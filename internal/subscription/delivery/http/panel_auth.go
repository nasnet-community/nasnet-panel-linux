package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
	"golang.org/x/crypto/bcrypt"
)

const (
	subAuthCookiePrefix  = "sub_auth_"
	subAuthDefaultExpiry = 24 * time.Hour
	subAuthRememberMe    = 30 * 24 * time.Hour
)

// passwordInfo holds resolved password authentication details for a subscription.
type passwordInfo struct {
	required   bool
	bcryptHash string
}

// resolvePasswordAuth checks whether password authentication is required for a subscription.
func (h *Handler) resolvePasswordAuth(c *gin.Context, sub *domain.Subscription) passwordInfo {
	mode := sub.PanelPasswordMode
	if mode == "" {
		mode = "default"
	}

	switch mode {
	case "disabled":
		return passwordInfo{}
	case "custom":
		if sub.PanelPasswordHash == "" {
			return passwordInfo{}
		}
		return passwordInfo{required: true, bcryptHash: sub.PanelPasswordHash}
	default: // "default"
		if h.settingUC == nil {
			return passwordInfo{}
		}
		enabled, _ := h.settingUC.GetByKey(c.Request.Context(), "sub_panel_auth_enabled")
		if enabled != "true" {
			return passwordInfo{}
		}
		globalPw, _ := h.settingUC.GetByKey(c.Request.Context(), "sub_panel_password")
		if globalPw == "" {
			return passwordInfo{}
		}
		return passwordInfo{required: true, bcryptHash: globalPw}
	}
}

// verifyPassword checks a candidate password against the resolved password info.
func (pi passwordInfo) verifyPassword(candidate string) bool {
	if pi.bcryptHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(pi.bcryptHash), []byte(candidate)) == nil
	}
	return false
}

// checkSubAuth validates the auth cookie/query param for a subscription.
// Returns true if access is allowed, false if a 403 was sent.
func (h *Handler) checkSubAuth(c *gin.Context, sub *domain.Subscription) bool {
	pi := h.resolvePasswordAuth(c, sub)
	if !pi.required {
		return true
	}

	// Check cookie
	linkKey := sub.GetLinkKey()
	cookieName := subAuthCookieName(linkKey)
	if token, err := c.Cookie(cookieName); err == nil && token != "" {
		if h.validateSubAuthToken(token, linkKey) {
			return true
		}
	}

	// Check query param (for SSE EventSource which cannot set cookies on connect)
	if token := c.Query("auth"); token != "" {
		if h.validateSubAuthToken(token, linkKey) {
			return true
		}
	}

	// Return 403 with minimal info
	c.JSON(http.StatusForbidden, httputil.Response{
		Success: false,
		Error:   "password_required",
		Data: map[string]interface{}{
			"auth_required": true,
			"label":         sub.GetDisplayName(),
		},
	})
	return false
}

// generateSubAuthToken creates a signed token for subscription panel access.
func (h *Handler) generateSubAuthToken(configID string, expiry time.Duration) string {
	exp := time.Now().Add(expiry).Unix()
	payload := fmt.Sprintf("%s:%d", configID, exp)
	sig := h.hmacSign(payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

// validateSubAuthToken validates a sub auth token.
func (h *Handler) validateSubAuthToken(token string, configID string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(payloadBytes)

	// Verify HMAC
	expectedSig := h.hmacSign(payload)
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return false
	}

	// Parse payload
	sepIdx := strings.LastIndex(payload, ":")
	if sepIdx < 0 {
		return false
	}
	tokenConfigID := payload[:sepIdx]
	expStr := payload[sepIdx+1:]

	// Verify configID matches
	if tokenConfigID != configID {
		return false
	}

	// Verify expiry
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > exp {
		return false
	}

	return true
}

// hmacSign creates an HMAC-SHA256 signature using the handler's auth secret.
func (h *Handler) hmacSign(data string) string {
	mac := hmac.New(sha256.New, []byte(h.authSecret+"_sub_panel"))
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// subAuthCookieName returns the cookie name for a subscription's auth token.
func subAuthCookieName(configID string) string {
	if len(configID) > 8 {
		return subAuthCookiePrefix + configID[:8]
	}
	return subAuthCookiePrefix + configID
}

type verifySubPasswordRequest struct {
	Password string `json:"password" binding:"required"`
	Remember bool   `json:"remember"`
}

// VerifySubPassword handles POST /api/v1/public/sub/:uuid/auth
func (h *Handler) VerifySubPassword(c *gin.Context) {
	uuid := c.Param("uuid")

	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}

	pi := h.resolvePasswordAuth(c, sub)
	if !pi.required {
		httputil.OK(c, map[string]string{"message": "no password required"})
		return
	}

	var req verifySubPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "password is required")
		return
	}

	if !pi.verifyPassword(req.Password) {
		httputil.Error(c, http.StatusUnauthorized, "invalid_password")
		return
	}

	// Generate token
	expiry := subAuthDefaultExpiry
	if req.Remember {
		expiry = subAuthRememberMe
	}
	token := h.generateSubAuthToken(sub.GetLinkKey(), expiry)

	// Set cookie
	cookieName := subAuthCookieName(sub.GetLinkKey())
	maxAge := int(expiry.Seconds())
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, token, maxAge, "/", "", false, true)

	httputil.OK(c, map[string]string{
		"token": token,
	})
}

// LogoutSub handles DELETE /api/v1/public/sub/:uuid/auth
func (h *Handler) LogoutSub(c *gin.Context) {
	uuid := c.Param("uuid")
	cookieName := subAuthCookieName(uuid)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, "", -1, "/", "", false, true)
	httputil.OK(c, map[string]string{"message": "logged out"})
}

type setPanelPasswordRequest struct {
	Mode     string `json:"mode" binding:"required"` // "default", "custom", "disabled"
	Password string `json:"password"`                // required when mode="custom"
}

// SetPanelPassword handles PUT /api/v1/subscriptions/:id/panel-password
func (h *Handler) SetPanelPassword(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req setPanelPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.SetPanelPassword(c.Request.Context(), id, req.Mode, req.Password); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	httputil.OK(c, map[string]string{"message": "panel password updated"})
}
