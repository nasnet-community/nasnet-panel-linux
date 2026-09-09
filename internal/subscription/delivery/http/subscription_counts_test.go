package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	authMiddleware "github.com/nasnet-community/nasnet-panel-linux/internal/auth/middleware"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
)

type expiryCountUsecase struct {
	stubSubscriptionUsecase
	days  int
	calls int
	err   error
}

func (s *expiryCountUsecase) CountExpiringSubscriptions(_ context.Context, days int) (int64, error) {
	s.calls++
	s.days = days
	return 1005, s.err
}

func TestCountExpiringAdminRoute(t *testing.T) {
	mgr := jwt.NewManager(jwt.Config{SecretKey: "expiry-count-test-secret", Issuer: "expiry-count-test"})
	mw := authMiddleware.NewJWTMiddleware(mgr)
	admin, err := mgr.GenerateTokenPairWithExpiry(1, 0, "admin", true, false, time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	user, err := mgr.GenerateTokenPairWithExpiry(2, 0, "user", false, false, time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	const route = "/api/v1/admin/subscriptions/expiring-soon/count"
	for _, tc := range []struct {
		name   string
		query  string
		token  string
		status int
		days   int
		err    error
	}{
		{name: "default", token: admin.AccessToken, status: http.StatusOK, days: 7},
		{name: "custom days", query: "?days=14", token: admin.AccessToken, status: http.StatusOK, days: 14},
		{name: "zero", query: "?days=0", token: admin.AccessToken, status: http.StatusBadRequest},
		{name: "negative", query: "?days=-1", token: admin.AccessToken, status: http.StatusBadRequest},
		{name: "too large", query: "?days=366", token: admin.AccessToken, status: http.StatusBadRequest},
		{name: "invalid", query: "?days=abc", token: admin.AccessToken, status: http.StatusBadRequest},
		{name: "no auth", status: http.StatusUnauthorized},
		{name: "ordinary user", token: user.AccessToken, status: http.StatusForbidden},
		{name: "database failure", token: admin.AccessToken, status: http.StatusInternalServerError, days: 7, err: errors.New("query failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := &expiryCountUsecase{err: tc.err}
			h := &Handler{subUsecase: uc}
			router := gin.New()
			h.RegisterAdminRoutes(router.Group("/api/v1", mw.RequireAuth(), mw.RequireAdmin()))
			req := httptest.NewRequest(http.MethodGet, route+tc.query, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if uc.days != tc.days || (tc.days == 0 && uc.calls != 0) {
				t.Fatalf("unauthorized/invalid request reached query or wrong days: calls=%d days=%d", uc.calls, uc.days)
			}
			if tc.status == http.StatusOK {
				var response struct {
					Success bool `json:"success"`
					Data    struct {
						Count int64 `json:"count"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !response.Success || response.Data.Count != 1005 {
					t.Fatalf("unexpected count response: %s (error: %v)", w.Body.String(), err)
				}
			}
		})
	}
}

func (s *stubSubscriptionUsecase) CountExpiringSubscriptions(_ context.Context, _ int) (int64, error) {
	return 0, errStub
}
