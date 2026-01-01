package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { gin.SetMode(gin.TestMode) }

func setupTestManager() *jwt.Manager {
	return jwt.NewManager(jwt.Config{
		SecretKey:          "test-secret-key-that-is-long-enough",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "nasnet-panel-test",
	})
}

// generateValidToken generates a valid access token for testing.
func generateValidToken(t *testing.T, mgr *jwt.Manager, userID uint, telegramID int64, username string, isAdmin bool) string {
	t.Helper()
	pair, err := mgr.GenerateTokenPairWithExpiry(userID, telegramID, username, isAdmin, false, 15*time.Minute, 7*24*time.Hour)
	require.NoError(t, err)
	return pair.AccessToken
}

// newTestRouter creates a Gin router with the supplied handlers and a trivial
// terminal handler that returns 200 OK.
func newTestRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	handlers = append(handlers, func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/test", handlers...)
	return r
}

// TestRequireAuth_ValidToken ensures a request with a valid cookie token passes
// and that the expected context keys are populated.
func TestRequireAuth_ValidToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 42, 123456789, "testuser", false)

	called := false
	r := gin.New()
	r.GET("/test", mw.RequireAuth(), func(c *gin.Context) {
		called = true

		userID, ok := c.Get("user_id")
		assert.True(t, ok)
		assert.Equal(t, uint(42), userID.(uint))

		telegramID, ok := c.Get("telegram_id")
		assert.True(t, ok)
		assert.Equal(t, int64(123456789), telegramID.(int64))

		username, ok := c.Get("username")
		assert.True(t, ok)
		assert.Equal(t, "testuser", username.(string))

		isAdmin, ok := c.Get("is_admin")
		assert.True(t, ok)
		assert.Equal(t, false, isAdmin.(bool))

		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called, "handler should have been called")
}

// TestRequireAuth_NoToken ensures a request without any token is rejected with 401.
func TestRequireAuth_NoToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	r := newTestRouter(mw.RequireAuth())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAuth_ExpiredToken ensures a token past its expiry is rejected with 401.
func TestRequireAuth_ExpiredToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	// Use a negative duration so the token is immediately expired.
	pair, err := mgr.GenerateTokenPairWithExpiry(1, 111, "expired", false, false, -1*time.Second, -1*time.Second)
	require.NoError(t, err)

	r := newTestRouter(mw.RequireAuth())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: pair.AccessToken})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAuth_TamperedToken ensures a token with appended characters is rejected.
func TestRequireAuth_TamperedToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 1, 111, "user", false)
	tampered := token + "TAMPERED"

	r := newTestRouter(mw.RequireAuth())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: tampered})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAuth_BearerHeader ensures a token sent via Authorization: Bearer is accepted.
func TestRequireAuth_BearerHeader(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 7, 777, "bearer_user", false)

	r := newTestRouter(mw.RequireAuth())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAdmin_AdminUser ensures an admin token passes the admin guard.
func TestRequireAdmin_AdminUser(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 99, 999, "admin_user", true)

	r := newTestRouter(mw.RequireAuth(), mw.RequireAdmin())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAdmin_NonAdminUser ensures a non-admin token is rejected with 403.
func TestRequireAdmin_NonAdminUser(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 5, 555, "plain_user", false)

	r := newTestRouter(mw.RequireAuth(), mw.RequireAdmin())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestOptionalAuth_WithToken ensures that when a valid token is present the
// context keys are populated but the request is still allowed through.
func TestOptionalAuth_WithToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 10, 1010, "optional_user", false)

	called := false
	r := gin.New()
	r.GET("/test", mw.OptionalAuth(), func(c *gin.Context) {
		called = true
		_, ok := c.Get("user_id")
		assert.True(t, ok, "user_id should be set when a valid token is provided")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

// TestOptionalAuth_WithoutToken ensures a missing token still lets the request through.
func TestOptionalAuth_WithoutToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	called := false
	r := gin.New()
	r.GET("/test", mw.OptionalAuth(), func(c *gin.Context) {
		called = true
		_, ok := c.Get("user_id")
		assert.False(t, ok, "user_id should not be set when no token is provided")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

// TestOptionalAuth_InvalidToken ensures a bad token still lets the request through
// but does not populate context keys.
func TestOptionalAuth_InvalidToken(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	called := false
	r := gin.New()
	r.GET("/test", mw.OptionalAuth(), func(c *gin.Context) {
		called = true
		_, ok := c.Get("user_id")
		assert.False(t, ok, "user_id should not be set for an invalid token")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "this.is.not.a.valid.jwt"})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

// TestGetUserClaims verifies that all claim fields are correctly round-tripped
// through the context helpers.
func TestGetUserClaims(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	const (
		wantUserID     = uint(77)
		wantTelegramID = int64(7700000)
		wantUsername   = "claims_user"
		wantIsAdmin    = true
	)

	token := generateValidToken(t, mgr, wantUserID, wantTelegramID, wantUsername, wantIsAdmin)

	r := gin.New()
	r.GET("/test", mw.RequireAuth(), func(c *gin.Context) {
		claims, ok := GetUserClaims(c)
		require.True(t, ok, "GetUserClaims should return ok=true")
		require.NotNil(t, claims)

		assert.Equal(t, wantUserID, claims.UserID)
		assert.Equal(t, wantTelegramID, claims.TelegramID)
		assert.Equal(t, wantUsername, claims.Username)
		assert.Equal(t, wantIsAdmin, claims.IsAdmin)
		assert.Equal(t, "access", claims.TokenType)

		// Verify GetUserID helper
		uid, ok := GetUserID(c)
		assert.True(t, ok)
		assert.Equal(t, wantUserID, uid)

		// Verify IsAdmin helper
		assert.True(t, IsAdmin(c))

		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
