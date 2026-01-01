package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
)

// JWTMiddleware handles JWT authentication
type JWTMiddleware struct {
	jwtManager *jwt.Manager
}

// NewJWTMiddleware creates a new JWT middleware
func NewJWTMiddleware(jwtManager *jwt.Manager) *JWTMiddleware {
	return &JWTMiddleware{jwtManager: jwtManager}
}

// jsonResponse for error responses
type jsonResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RequireAuth middleware ensures a valid JWT token is present
// It checks both cookies and Authorization header
func (m *JWTMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := m.extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, jsonResponse{
				Success: false,
				Error:   "authentication required",
			})
			return
		}

		claims, err := m.jwtManager.ValidateAccessToken(token)
		if err != nil {
			statusCode := http.StatusUnauthorized
			message := "invalid token"

			if err == jwt.ErrExpiredToken {
				message = "token expired"
			}

			c.AbortWithStatusJSON(statusCode, jsonResponse{
				Success: false,
				Error:   message,
			})
			return
		}

		// Set user info in context for handlers to use
		c.Set("user", claims)
		c.Set("user_id", claims.UserID)
		c.Set("telegram_id", claims.TelegramID)
		c.Set("username", claims.Username)
		c.Set("is_admin", claims.IsAdmin)

		c.Next()
	}
}

// RequireAdmin middleware ensures the user is an admin
// Must be used after RequireAuth
func (m *JWTMiddleware) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, exists := c.Get("is_admin")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, jsonResponse{
				Success: false,
				Error:   "authentication required",
			})
			return
		}

		if admin, ok := isAdmin.(bool); !ok || !admin {
			c.AbortWithStatusJSON(http.StatusForbidden, jsonResponse{
				Success: false,
				Error:   "admin access required",
			})
			return
		}

		c.Next()
	}
}

// OptionalAuth middleware extracts user info if a token is present, but doesn't require it
func (m *JWTMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := m.extractToken(c)
		if token == "" {
			c.Next()
			return
		}

		claims, err := m.jwtManager.ValidateAccessToken(token)
		if err != nil {
			// Token is invalid, but we don't block the request
			c.Next()
			return
		}

		// Set user info in context
		c.Set("user", claims)
		c.Set("user_id", claims.UserID)
		c.Set("telegram_id", claims.TelegramID)
		c.Set("username", claims.Username)
		c.Set("is_admin", claims.IsAdmin)

		c.Next()
	}
}

// extractToken attempts to extract the JWT token from the request
// Priority: 1. Cookie, 2. Authorization header
func (m *JWTMiddleware) extractToken(c *gin.Context) string {
	// Try cookie first (preferred for browser-based apps)
	if token, err := c.Cookie("access_token"); err == nil && token != "" {
		return token
	}

	// Try Authorization header (for API clients)
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	// Support "Bearer <token>" format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}

	// Also support raw token in Authorization header
	return authHeader
}

// GetUserID extracts the user ID from the context
func GetUserID(c *gin.Context) (uint, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := userID.(uint)
	return id, ok
}

// GetUserClaims extracts the full claims from the context
func GetUserClaims(c *gin.Context) (*jwt.Claims, bool) {
	user, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	claims, ok := user.(*jwt.Claims)
	return claims, ok
}

// IsAdmin checks if the current user is an admin
func IsAdmin(c *gin.Context) bool {
	isAdmin, exists := c.Get("is_admin")
	if !exists {
		return false
	}
	admin, ok := isAdmin.(bool)
	return ok && admin
}
