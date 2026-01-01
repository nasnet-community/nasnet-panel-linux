package http

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/config"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	authMiddleware "github.com/nasnet-community/nasnet-panel-linux/internal/auth/middleware"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/ratelimit"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userUsecase    usecase.UserUsecase
	jwtManager     *jwt.Manager
	adminConfig    config.AdminConfig
	settingUsecase settingDomain.SettingUsecase
	loginLimiter   *ratelimit.LoginLimiter
	auditUsecase   auditDomain.AuditLogUsecase
	botToken       string
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(userUsecase usecase.UserUsecase, jwtManager *jwt.Manager, settingUsecase settingDomain.SettingUsecase, auditUsecase auditDomain.AuditLogUsecase, botToken string, adminConfig ...config.AdminConfig) *AuthHandler {
	h := &AuthHandler{
		userUsecase:    userUsecase,
		jwtManager:     jwtManager,
		settingUsecase: settingUsecase,
		loginLimiter:   ratelimit.NewLoginLimiter(),
		auditUsecase:   auditUsecase,
		botToken:       botToken,
	}
	if len(adminConfig) > 0 {
		h.adminConfig = adminConfig[0]
	}
	return h
}

// Constants for cookie names
const (
	AccessTokenCookie  = "access_token"
	RefreshTokenCookie = "refresh_token"
)

// RegisterRoutes registers auth routes
func (h *AuthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	{
		auth.POST("/admin-login", h.AdminLogin)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", h.Logout)
		auth.GET("/me", h.GetCurrentUser)
	}
}

// LoginResponse represents login response
type LoginResponse struct {
	User        UserInfo  `json:"user"`
	AccessToken string    `json:"access_token,omitempty"` // Only included if not using cookies
	ExpiresAt   time.Time `json:"expires_at"`
}

// UserInfo represents user information in responses
type UserInfo struct {
	ID         uint   `json:"id"`
	TelegramID int64  `json:"telegram_id"`
	Username   string `json:"username"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	IsAdmin    bool   `json:"is_admin"`
}

// AdminLoginRequest represents admin panel login request
type AdminLoginRequest struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	RememberMe bool   `json:"remember_me"`
}

// AdminLogin authenticates an admin user with username/password for web panel access
func (h *AuthHandler) AdminLogin(c *gin.Context) {
	var req AdminLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "username and password are required")
		return
	}

	ip := c.ClientIP()

	// Rate limit check
	if allowed, retryAfter := h.loginLimiter.Check(ip, req.Username); !allowed {
		httputil.Error(c, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, try again in %d seconds", int(retryAfter.Seconds())))
		return
	}

	// Validate admin credentials against config
	if h.adminConfig.Username == "" || h.adminConfig.PasswordHash == "" {
		httputil.Error(c, http.StatusInternalServerError, "admin login not configured")
		return
	}

	// Check username
	if req.Username != h.adminConfig.Username {
		h.loginLimiter.RecordFailure(ip, req.Username)
		h.auditLoginAttempt(c, req.Username, "invalid_username")
		httputil.Error(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Get password hash — try DB setting first (changed via settings page), fall back to config
	passwordHash := h.adminConfig.PasswordHash
	if val, err := h.settingUsecase.GetByKey(c.Request.Context(), "admin_password_hash"); err == nil && val != "" {
		passwordHash = val
	}

	// Verify password with bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		h.loginLimiter.RecordFailure(ip, req.Username)
		h.auditLoginAttempt(c, req.Username, "invalid_password")
		httputil.Error(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Successful credential check — reset rate limiter
	h.loginLimiter.Reset(ip, req.Username)

	// Resolve token expiry from DB settings (with fallback to config)
	accessExpiry, refreshExpiry := h.resolveTokenExpiry(c)

	// Find the admin user by username (or create a pseudo-admin record)
	user, err := h.userUsecase.GetByUsername(c.Request.Context(), req.Username)
	if err != nil {
		// Admin doesn't exist as a user - create a synthetic admin response
		// Generate token with admin privileges
		tokenPair, err := h.jwtManager.GenerateTokenPairWithExpiry(0, 0, req.Username, true, req.RememberMe, accessExpiry, refreshExpiry)
		if err != nil {
			httputil.Error(c, http.StatusInternalServerError, "failed to generate tokens")
			return
		}

		h.setTokenCookies(c, tokenPair, req.RememberMe, accessExpiry, refreshExpiry)
		h.auditLoginAttempt(c, req.Username, "success")

		httputil.OK(c, LoginResponse{
			User: UserInfo{
				ID:       0,
				Username: req.Username,
				IsAdmin:  true,
			},
			ExpiresAt: tokenPair.ExpiresAt,
		})
		return
	}

	// Verify the user is actually an admin
	if !user.IsAdmin {
		httputil.Error(c, http.StatusForbidden, "user is not an admin")
		return
	}

	// Generate token pair for the admin user
	tokenPair, err := h.jwtManager.GenerateTokenPairWithExpiry(user.ID, user.TelegramID, user.Username, user.IsAdmin, req.RememberMe, accessExpiry, refreshExpiry)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	h.setTokenCookies(c, tokenPair, req.RememberMe, accessExpiry, refreshExpiry)
	h.auditLoginAttempt(c, req.Username, "success")

	httputil.OK(c, LoginResponse{
		User: UserInfo{
			ID:         user.ID,
			TelegramID: user.TelegramID,
			Username:   user.Username,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			IsAdmin:    user.IsAdmin,
		},
		ExpiresAt: tokenPair.ExpiresAt,
	})
}

// auditLoginAttempt logs admin login attempts via the audit usecase.
func (h *AuthHandler) auditLoginAttempt(c *gin.Context, username, result string) {
	if h.auditUsecase == nil {
		return
	}
	requestID := ""
	if id, exists := c.Get(httpMiddleware.RequestIDKey); exists {
		requestID, _ = id.(string)
	}
	h.auditUsecase.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:    string(auditDomain.AuditAdminLogin),
		ActorName: username,
		IPAddress: c.ClientIP(),
		RequestID: requestID,
		Source:    "web",
		NewValues: fmt.Sprintf(`{"result":"%s"}`, result),
	})
}

// Refresh refreshes the access token using the refresh token
func (h *AuthHandler) Refresh(c *gin.Context) {
	// Try to get refresh token from cookie first
	refreshToken, err := c.Cookie(RefreshTokenCookie)
	if err != nil {
		// Try from request body
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
			httputil.Error(c, http.StatusUnauthorized, "refresh token required")
			return
		}
		refreshToken = req.RefreshToken
	}

	// Validate refresh token
	claims, err := h.jwtManager.ValidateRefreshToken(refreshToken)
	if err != nil {
		h.clearTokenCookies(c)
		httputil.Error(c, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	// Resolve token expiry from DB settings (with fallback to config)
	accessExpiry, refreshExpiry := h.resolveTokenExpiry(c)

	// Synthetic admin (UserID 0) — skip DB lookup, just re-issue tokens
	if claims.UserID == 0 && claims.IsAdmin {
		tokenPair, err := h.jwtManager.GenerateTokenPairWithExpiry(0, 0, claims.Username, true, claims.RememberMe, accessExpiry, refreshExpiry)
		if err != nil {
			httputil.Error(c, http.StatusInternalServerError, "failed to generate tokens")
			return
		}

		h.setTokenCookies(c, tokenPair, claims.RememberMe, accessExpiry, refreshExpiry)

		httputil.OK(c, LoginResponse{
			User: UserInfo{
				ID:       0,
				Username: claims.Username,
				IsAdmin:  true,
			},
			ExpiresAt: tokenPair.ExpiresAt,
		})
		return
	}

	// Get user to check if still valid
	user, err := h.userUsecase.GetByID(c.Request.Context(), claims.UserID)
	if err != nil {
		h.clearTokenCookies(c)
		httputil.Error(c, http.StatusUnauthorized, "user not found")
		return
	}

	// Check if user is banned
	if user.IsBanned {
		h.clearTokenCookies(c)
		httputil.Error(c, http.StatusForbidden, "user is banned")
		return
	}

	// Generate new token pair with the resolved expiry durations
	tokenPair, err := h.jwtManager.GenerateTokenPairWithExpiry(user.ID, user.TelegramID, user.Username, user.IsAdmin, claims.RememberMe, accessExpiry, refreshExpiry)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	// Set new cookies using the same expiry durations
	h.setTokenCookies(c, tokenPair, claims.RememberMe, accessExpiry, refreshExpiry)

	httputil.OK(c, LoginResponse{
		User: UserInfo{
			ID:         user.ID,
			TelegramID: user.TelegramID,
			Username:   user.Username,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			IsAdmin:    user.IsAdmin,
		},
		ExpiresAt: tokenPair.ExpiresAt,
	})
}

// Logout revokes the current access token and clears authentication cookies
func (h *AuthHandler) Logout(c *gin.Context) {
	// If the auth middleware already validated the token (e.g. future re-wiring),
	// use claims from context; otherwise fall back to extracting from cookie.
	if claims, ok := authMiddleware.GetUserClaims(c); ok && claims.ID != "" {
		_ = h.jwtManager.RevokeToken(c.Request.Context(), claims.ID, claims.ExpiresAt.Time)
	} else if token, err := c.Cookie(AccessTokenCookie); err == nil && token != "" {
		if claims, err := h.jwtManager.ValidateAccessToken(token); err == nil && claims.ID != "" {
			_ = h.jwtManager.RevokeToken(c.Request.Context(), claims.ID, claims.ExpiresAt.Time)
		}
	}

	h.clearTokenCookies(c)

	httputil.OK(c, map[string]string{"message": "logged out successfully"})
}

// GetCurrentUser returns the current authenticated user
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	// Extract token from cookie (auth routes are public, so we need to validate manually)
	token, err := c.Cookie(AccessTokenCookie)
	if err != nil || token == "" {
		httputil.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Validate the token
	claims, err := h.jwtManager.ValidateAccessToken(token)
	if err != nil {
		httputil.Error(c, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	// For admin-only login (synthetic user with ID 0), return minimal info
	if claims.UserID == 0 {
		httputil.OK(c, UserInfo{
			ID:       0,
			Username: claims.Username,
			IsAdmin:  claims.IsAdmin,
		})
		return
	}

	// Get full user details for regular users
	user, err := h.userUsecase.GetByID(c.Request.Context(), claims.UserID)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "user not found")
		return
	}

	httputil.OK(c, UserInfo{
		ID:         user.ID,
		TelegramID: user.TelegramID,
		Username:   user.Username,
		FirstName:  user.FirstName,
		LastName:   user.LastName,
		IsAdmin:    user.IsAdmin,
	})
}

// resolveTokenExpiry reads JWT expiry durations from DB settings, falling back
// to the JWT manager's static config. The returned values are used for BOTH
// token generation and cookie MaxAge so they never diverge.
func (h *AuthHandler) resolveTokenExpiry(c *gin.Context) (accessExpiry, refreshExpiry time.Duration) {
	cfg := h.jwtManager.GetConfig()
	accessExpiry = cfg.AccessTokenExpiry
	refreshExpiry = cfg.RefreshTokenExpiry

	ctx := c.Request.Context()
	if val, err := h.settingUsecase.GetByKey(ctx, "jwt_access_expiry"); err == nil && val != "" {
		if min, err := strconv.Atoi(val); err == nil {
			accessExpiry = time.Duration(min) * time.Minute
		}
	}
	if val, err := h.settingUsecase.GetByKey(ctx, "jwt_refresh_expiry"); err == nil && val != "" {
		if hours, err := strconv.Atoi(val); err == nil {
			refreshExpiry = time.Duration(hours) * time.Hour
		}
	}
	return
}

// setTokenCookies sets access and refresh tokens as HTTP-only cookies.
// The accessExpiry and refreshExpiry must be the same values used when
// generating the token pair so that cookie lifetime matches the JWT claims.
func (h *AuthHandler) setTokenCookies(c *gin.Context, tokenPair *jwt.TokenPair, rememberMe bool, accessExpiry, refreshExpiry time.Duration) {
	cfg := h.jwtManager.GetConfig()

	// Determine SameSite based on whether it's cross-origin
	sameSite := http.SameSiteLaxMode
	if cfg.CookieSecure {
		// For cross-origin (production), use SameSite=None
		sameSite = http.SameSiteNoneMode
	}

	// Refresh token specific logic
	// Go's http.Cookie: MaxAge < 0 means delete, > 0 means max-age in seconds, 0 means no Max-Age attribute (session cookie)
	refreshMaxAge := 0
	if rememberMe {
		refreshMaxAge = int(refreshExpiry.Seconds())
	}

	// Access token cookie (lifetime matches the JWT exp claim)
	accessCookie := &http.Cookie{
		Name:     AccessTokenCookie,
		Value:    tokenPair.AccessToken,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   int(accessExpiry.Seconds()),
		Secure:   cfg.CookieSecure,
		HttpOnly: true,
		SameSite: sameSite,
	}
	http.SetCookie(c.Writer, accessCookie)

	// Refresh token cookie (lifetime matches the JWT exp claim)
	refreshCookie := &http.Cookie{
		Name:     RefreshTokenCookie,
		Value:    tokenPair.RefreshToken,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   refreshMaxAge,
		Secure:   cfg.CookieSecure,
		HttpOnly: true,
		SameSite: sameSite,
	}
	http.SetCookie(c.Writer, refreshCookie)
}

// clearTokenCookies removes the authentication cookies
func (h *AuthHandler) clearTokenCookies(c *gin.Context) {
	config := h.jwtManager.GetConfig()

	c.SetCookie(
		AccessTokenCookie,
		"",
		-1,
		"/",
		config.CookieDomain,
		config.CookieSecure,
		true,
	)

	c.SetCookie(
		RefreshTokenCookie,
		"",
		-1,
		"/",
		config.CookieDomain,
		config.CookieSecure,
		true,
	)
}
