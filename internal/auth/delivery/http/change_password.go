package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
	"golang.org/x/crypto/bcrypt"
)

// ChangePasswordRequest represents the change password request body
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

// RegisterAdminRoutes registers admin-protected auth routes
func (h *AuthHandler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	{
		auth.POST("/change-password", h.ChangePassword)
	}
}

// ChangePassword allows the admin to change their password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "all fields are required")
		return
	}

	// Validate passwords match
	if req.NewPassword != req.ConfirmPassword {
		httputil.Error(c, http.StatusBadRequest, "new password and confirmation do not match")
		return
	}

	// Validate minimum length
	if len(req.NewPassword) < 8 {
		httputil.Error(c, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	// Get current password hash — try DB setting first, fall back to config
	currentHash := ""
	if val, err := h.settingUsecase.GetByKey(c.Request.Context(), "admin_password_hash"); err == nil && val != "" {
		currentHash = val
	} else {
		currentHash = h.adminConfig.PasswordHash
	}

	if currentHash == "" {
		httputil.Error(c, http.StatusInternalServerError, "admin password not configured")
		return
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		httputil.Error(c, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to hash new password")
		return
	}

	// Save as setting in the admin category
	setting := &settingDomain.Setting{
		Key:         "admin_password_hash",
		Value:       string(newHash),
		Type:        "string",
		Category:    "admin",
		Description: "Hashed admin password (managed by change-password)",
		Label:       "Admin Password Hash",
	}

	if err := h.settingUsecase.UpdateMany(c.Request.Context(), []*settingDomain.Setting{setting}); err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to save new password")
		return
	}

	// Audit log for password change
	if h.auditUsecase != nil {
		requestID := ""
		if id, exists := c.Get(httpMiddleware.RequestIDKey); exists {
			requestID, _ = id.(string)
		}
		actorName := ""
		if username, exists := c.Get("username"); exists {
			actorName, _ = username.(string)
		}
		h.auditUsecase.Log(c.Request.Context(), &auditDomain.AuditLog{
			Action:    string(auditDomain.AuditPasswordChange),
			ActorName: actorName,
			IPAddress: c.ClientIP(),
			RequestID: requestID,
			Source:    "web",
			NewValues: `{"result":"success"}`,
		})
	}

	httputil.OK(c, map[string]string{"message": "password changed successfully"})
}
