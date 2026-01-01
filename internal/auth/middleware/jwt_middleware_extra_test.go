package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractToken_CookiePrecedesBearer verifies cookie wins when both
// cookie and Authorization header are present. Browser-first precedence
// is important: cookie is authenticated + httpOnly in prod, while a
// bearer header could come from a CSRF'd XHR on a different origin.
func TestExtractToken_CookiePrecedesBearer(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	cookieToken := generateValidToken(t, mgr, 11, 111, "cookie_user", false)
	headerToken := generateValidToken(t, mgr, 22, 222, "header_user", true)

	r := gin.New()
	r.GET("/test", mw.RequireAuth(), func(c *gin.Context) {
		username, _ := c.Get("username")
		c.JSON(http.StatusOK, gin.H{"user": username})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: cookieToken})
	req.Header.Set("Authorization", "Bearer "+headerToken)

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Body must reflect the COOKIE user — header identity is ignored.
	assert.Contains(t, w.Body.String(), `"user":"cookie_user"`,
		"cookie token must take precedence over Authorization header")
}

// TestExtractToken_RawAuthHeader covers the non-Bearer branch in
// extractToken: some clients send the raw JWT in Authorization.
func TestExtractToken_RawAuthHeader(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 33, 333, "raw_user", false)

	r := newTestRouter(mw.RequireAuth())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", token) // no "Bearer " prefix

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestExtractToken_BearerCaseInsensitive verifies the scheme match is
// case-insensitive, since both "Bearer" and "bearer" appear in the wild.
func TestExtractToken_BearerCaseInsensitive(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	token := generateValidToken(t, mgr, 44, 444, "bearer_case", false)

	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", scheme+" "+token)

		r := newTestRouter(mw.RequireAuth())
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "scheme %q should be accepted", scheme)
	}
}

// TestRequireAdmin_WithoutAuth exercises the defensive branch in
// RequireAdmin: if someone wires RequireAdmin without RequireAuth, the
// middleware should reject with 401 (no identity known at all), not
// 403 (identity known but insufficient privileges).
func TestRequireAdmin_WithoutAuth(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	r := gin.New()
	// RequireAdmin directly — no RequireAuth before.
	r.GET("/test", mw.RequireAdmin(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"RequireAdmin without RequireAuth must return 401, not 403")
}

// TestRequireAuth_MalformedCookie verifies the validation rejects
// non-JWT strings cleanly.
func TestRequireAuth_MalformedCookie(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	r := newTestRouter(mw.RequireAuth())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "not.a.real.token"})

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAuth_RefreshTokenRejected: a refresh token must not be
// accepted on the access-token path. Type confusion would let refresh
// tokens (with their longer TTL) authorize full API access.
func TestRequireAuth_RefreshTokenRejected(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	pair, err := mgr.GenerateTokenPair(1, 1, "refresh_user", false, false)
	require.NoError(t, err)

	r := newTestRouter(mw.RequireAuth())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: pair.RefreshToken})

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"refresh token on access path must be rejected")
}

// TestRequireAuth_EmptyBearer verifies that "Authorization: Bearer "
// with an empty token falls back to no-token behaviour.
func TestRequireAuth_EmptyBearer(t *testing.T) {
	mgr := setupTestManager()
	mw := NewJWTMiddleware(mgr)

	r := newTestRouter(mw.RequireAuth())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestIsAdmin_WithoutContext covers the path where IsAdmin is called
// on a bare context (no RequireAuth upstream). Must return false, not
// panic.
func TestIsAdmin_WithoutContext(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if IsAdmin(c) {
		t.Error("IsAdmin on empty context must be false")
	}
}

// TestGetUserID_WrongType defends the type assertion in GetUserID
// against future refactors that might set user_id as a different
// numeric type.
func TestGetUserID_WrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", "string-not-uint")
	id, ok := GetUserID(c)
	if ok {
		t.Errorf("expected ok=false for wrong type, got id=%d ok=true", id)
	}
}
