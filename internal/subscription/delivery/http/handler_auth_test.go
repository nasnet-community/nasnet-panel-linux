package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// stubSubscriptionUsecase is a no-op implementation of SubscriptionUsecase
// used only to prevent nil-pointer panics in the tests that pass the ownership
// check and reach the usecase call. All methods return an error so the handler
// returns 500 (not 403), which is all we need to assert.
type stubSubscriptionUsecase struct{}

var errStub = errors.New("stub")

func (s *stubSubscriptionUsecase) Subscribe(_ context.Context, _, _ uint) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetByID(_ context.Context, _ uint) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetByConfigID(_ context.Context, _ string) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) ListByUserID(_ context.Context, _ uint, _, _ int) ([]*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetActiveByUserID(_ context.Context, _ uint) ([]*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) ListAllSubscriptions(_ context.Context, _ string, _, _ int) ([]*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) Cancel(_ context.Context, _ uint) error { return errStub }
func (s *stubSubscriptionUsecase) Renew(_ context.Context, _ uint) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) RenewByAdmin(_ context.Context, _ uint) error { return errStub }
func (s *stubSubscriptionUsecase) UpdateDataUsage(_ context.Context, _ uint, _ int64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) CheckAndExpireSubscriptions(_ context.Context) error {
	return errStub
}
func (s *stubSubscriptionUsecase) CheckAndExpireByDataLimit(_ context.Context) error {
	return errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionLink(_ context.Context, _ uint) (string, error) {
	return "", errStub
}
func (s *stubSubscriptionUsecase) SyncUsageFromXray(_ context.Context, _ uint) error {
	return errStub
}
func (s *stubSubscriptionUsecase) RenameSubscription(_ context.Context, _ uint, _ string) error {
	return errStub
}
func (s *stubSubscriptionUsecase) UpdateTelegramChatIDByConfigID(_ context.Context, _ string, _ int64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) ReconcileUsers(_ context.Context) (*usecase.ReconcileStats, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) RegenerateUUID(_ context.Context, _ uint) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) RegenerateSubscriptionKey(_ context.Context, _ uint, _ string) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) ActivateTrial(_ context.Context, _ uint) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionConfig(_ context.Context, _ string) (*usecase.SubscriptionConfigResult, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionServers(_ context.Context, _ uint) ([]usecase.SubServer, error) {
	return nil, nil
}
func (s *stubSubscriptionUsecase) GetByConfigEmail(_ context.Context, _ string) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) CreateDirect(_ context.Context, _ *domain.Subscription) error {
	return errStub
}
func (s *stubSubscriptionUsecase) AssignToUser(_ context.Context, _, _ uint) error { return errStub }
func (s *stubSubscriptionUsecase) AssignToInbound(_ context.Context, _, _ uint) error {
	return errStub
}
func (s *stubSubscriptionUsecase) SetCustomDataLimit(_ context.Context, _ uint, _ *float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) SetCustomEndDate(_ context.Context, _ uint, _ *time.Time, _ bool) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) SetCustomBandwidthLimit(_ context.Context, _ uint, _ *int) error {
	return errStub
}
func (s *stubSubscriptionUsecase) SetMaxDevices(_ context.Context, _ uint, _ int) error {
	return errStub
}
func (s *stubSubscriptionUsecase) AddData(_ context.Context, _ uint, _ float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) SubscribeMetered(_ context.Context, _, _ uint, _ float64) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) TopUpDataWithBalance(_ context.Context, _, _ uint, _ float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) GrantTopUp(_ context.Context, _ uint, _ float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) RenewMetered(_ context.Context, _ uint, _ float64) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) RenewMeteredByAdmin(_ context.Context, _ uint, _ float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) ResetDataUsed(_ context.Context, _ uint) error { return errStub }
func (s *stubSubscriptionUsecase) GetUsageDetails(_ context.Context, _ uint) (*usecase.SubscriptionUsageDetails, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) CheckAndSendDataWarnings(_ context.Context) ([]*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) ListAllFilteredSubscriptions(_ context.Context, _ repository.SubscriptionFilter) ([]*domain.Subscription, int64, error) {
	return nil, 0, errStub
}
func (s *stubSubscriptionUsecase) ReconcilePlanInbounds(_ context.Context, _ uint) error {
	return errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionUsageHistory(_ context.Context, _ uint, _ int) ([]usecase.UsageHistoryPoint, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) ListDailyUsageRecords(_ context.Context, _ uint, _ int) ([]*domain.SubscriptionDailyUsage, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionUsagePattern(_ context.Context, _ string, _ int) ([]usecase.HourlyUsagePoint, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) SetPanelPassword(_ context.Context, _ uint, _, _ string) error {
	return errStub
}
func (s *stubSubscriptionUsecase) GetPanelPasswordHash(_ context.Context, _ uint) (string, string, error) {
	return "", "", errStub
}
func (s *stubSubscriptionUsecase) HasUserPurchasedPlan(_ context.Context, _, _ uint) (bool, error) {
	return false, errStub
}
func (s *stubSubscriptionUsecase) CreateManual(_ context.Context, _ *usecase.ManualSubscriptionRequest) (*domain.Subscription, error) {
	return nil, errStub
}
func (s *stubSubscriptionUsecase) SetRenewalConfig(_ context.Context, _ uint, _ *int, _ *float64) error {
	return errStub
}
func (s *stubSubscriptionUsecase) SetAutoRenew(_ context.Context, _ uint, _ bool, _ int) error {
	return errStub
}
func (s *stubSubscriptionUsecase) RenewFromConfig(_ context.Context, _ uint) error {
	return errStub
}
func (s *stubSubscriptionUsecase) GetSubscriptionUsageTrend(_ context.Context, _ uint, _ int) (*domain.UsageTrend, error) {
	return nil, errStub
}

// newTestHandler returns a Handler backed by the stub usecase.
func newTestHandler() *Handler {
	return &Handler{subUsecase: &stubSubscriptionUsecase{}}
}

// buildContext creates a Gin context backed by a response recorder, sets
// the named URL param to paramValue, and populates the auth context keys.
func buildContext(authUserID uint, isAdmin bool, paramValue string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)

	// Simulate auth middleware
	c.Set("user_id", authUserID)
	c.Set("is_admin", isAdmin)

	// Set URL param
	c.Params = gin.Params{
		{Key: "user_id", Value: paramValue},
	}
	return c, w
}

// TestListByUserID_BlocksCrossUserAccess verifies that a non-admin user
// authenticated as user 1 cannot list subscriptions for user 2.
func TestListByUserID_BlocksCrossUserAccess(t *testing.T) {
	h := newTestHandler()
	c, w := buildContext(1, false, "2")

	h.ListByUserID(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}

// TestListByUserID_AllowsOwnData verifies that a non-admin user can list
// their own subscriptions (user_id in URL matches auth user_id).
// The response will not be 403 (it may be 500 due to stub usecase).
func TestListByUserID_AllowsOwnData(t *testing.T) {
	h := newTestHandler()
	c, w := buildContext(1, false, "1")

	h.ListByUserID(c)

	if w.Code == http.StatusForbidden {
		t.Errorf("expected NOT 403 Forbidden for own data, got %d", w.Code)
	}
}

// TestListByUserID_AllowsAdmin verifies that an admin user can list
// subscriptions belonging to any user.
// The response will not be 403 (it may be 500 due to stub usecase).
func TestListByUserID_AllowsAdmin(t *testing.T) {
	h := newTestHandler()
	c, w := buildContext(99, true, "2")

	h.ListByUserID(c)

	if w.Code == http.StatusForbidden {
		t.Errorf("expected NOT 403 Forbidden for admin, got %d", w.Code)
	}
}

// TestGetSubUsageTrend_BadRange verifies that an unrecognised range param returns 400.
func TestGetSubUsageTrend_BadRange(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/public/sub/aaaaaaaa/usage-trend?range=14d", nil)
	c.Params = gin.Params{{Key: "uuid", Value: "aaaaaaaa"}}

	h.GetSubUsageTrend(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestGetActiveByUserID_BlocksCrossUserAccess verifies that a non-admin user
// authenticated as user 1 cannot retrieve active subscriptions for user 2.
func TestGetActiveByUserID_BlocksCrossUserAccess(t *testing.T) {
	h := newTestHandler()
	c, w := buildContext(1, false, "2")

	h.GetActiveByUserID(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}
