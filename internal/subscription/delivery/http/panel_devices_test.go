package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
)

// fakeDeviceUC is a configurable DeviceUsecase for panel handler tests.
type fakeDeviceUC struct {
	devices    []*wgDomain.WGPeer
	servers    []wireguardUC.WGServerOption
	maxDevices int
	createErr  error
	configErr  error
	created    *wireguardUC.DeviceConfig
}

func (f *fakeDeviceUC) ListServers(_ context.Context, _ uint) ([]wireguardUC.WGServerOption, error) {
	return f.servers, nil
}
func (f *fakeDeviceUC) ListDevices(_ context.Context, _ uint) ([]*wgDomain.WGPeer, error) {
	return f.devices, nil
}
func (f *fakeDeviceUC) MaxDevices(_ context.Context, _ uint) (int, error) { return f.maxDevices, nil }
func (f *fakeDeviceUC) CreateDevice(_ context.Context, _ uint, _ wireguardUC.CreateDeviceInput) (*wireguardUC.DeviceConfig, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.created, nil
}
func (f *fakeDeviceUC) DeviceConfig(_ context.Context, _, _ uint) (*wireguardUC.DeviceConfig, error) {
	if f.configErr != nil {
		return nil, f.configErr
	}
	return f.created, nil
}
func (f *fakeDeviceUC) RotateDevice(_ context.Context, _, _ uint) (*wireguardUC.DeviceConfig, error) {
	return f.created, nil
}
func (f *fakeDeviceUC) RemoveDevice(_ context.Context, _, _ uint) error        { return nil }
func (f *fakeDeviceUC) DeactivateSubscription(_ context.Context, _ uint) error { return nil }
func (f *fakeDeviceUC) ActivateSubscription(_ context.Context, _ uint) error   { return nil }

// subReturningUC reuses the no-op stub but returns a fixed subscription from
// GetByConfigID so the UUID→sub resolution in authedPanelSub succeeds.
type subReturningUC struct {
	*stubSubscriptionUsecase
	sub *domain.Subscription
}

func (s *subReturningUC) GetByConfigID(_ context.Context, _ string) (*domain.Subscription, error) {
	return s.sub, nil
}

func panelDeviceCtx(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var r *http.Request
	if body != "" {
		r, _ = http.NewRequest(method, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r, _ = http.NewRequest(method, "/", nil)
	}
	c.Request = r
	c.Params = gin.Params{{Key: "uuid", Value: "testuuid1"}, {Key: "deviceId", Value: "7"}}
	return c, w
}

func handlerWithDevice(sub *domain.Subscription, dev *fakeDeviceUC) *Handler {
	return &Handler{
		subUsecase: &subReturningUC{stubSubscriptionUsecase: &stubSubscriptionUsecase{}, sub: sub},
		deviceUC:   dev,
	}
}

// TestPanelDevices_OK returns the device list plus the cap so the UI can show used/max.
func TestPanelDevices_OK(t *testing.T) {
	h := handlerWithDevice(&domain.Subscription{}, &fakeDeviceUC{
		devices:    []*wgDomain.WGPeer{{}, {}},
		maxDevices: 5,
	})
	c, w := panelDeviceCtx(http.MethodGet, "")

	h.PanelDevices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"max_devices":5`) || !strings.Contains(body, `"used":2`) {
		t.Fatalf("expected cap fields in body, got %s", body)
	}
}

// TestPanelAddDevice_CapReached maps the usecase cap error to 409.
func TestPanelAddDevice_CapReached(t *testing.T) {
	h := handlerWithDevice(&domain.Subscription{}, &fakeDeviceUC{
		createErr: wireguardUC.ErrDeviceCapReached,
	})
	c, w := panelDeviceCtx(http.MethodPost, "{}")

	h.PanelAddDevice(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "device_limit_reached") {
		t.Fatalf("expected device_limit_reached, got %s", w.Body.String())
	}
}

// TestPanelAddDevice_OK returns the freshly created device + its once-shown config.
func TestPanelAddDevice_OK(t *testing.T) {
	h := handlerWithDevice(&domain.Subscription{}, &fakeDeviceUC{
		created: &wireguardUC.DeviceConfig{Peer: &wgDomain.WGPeer{}, Conf: "[Interface]\nPrivateKey=x"},
	})
	c, w := panelDeviceCtx(http.MethodPost, `{"label":"My Phone"}`)

	h.PanelAddDevice(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PrivateKey") {
		t.Fatalf("expected config in body, got %s", w.Body.String())
	}
}

// TestPanelDeviceRoutes_RegisterAndResolve exercises real gin route
// registration — catching any wildcard conflict that would panic on boot — and
// confirms a request routes through to the handler with path params parsed.
func TestPanelDeviceRoutes_RegisterAndResolve(t *testing.T) {
	h := handlerWithDevice(&domain.Subscription{}, &fakeDeviceUC{maxDevices: 3})

	engine := gin.New()
	root := engine.Group("")
	// Must not panic on conflicting wildcards alongside the existing routes.
	h.RegisterPublicRoutes(root)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/public/sub/abcd1234/devices", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from registered /devices route, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"max_devices":3`) {
		t.Fatalf("expected device payload, got %s", w.Body.String())
	}
}

// TestPanelDeviceRoutes_NotRegisteredWhenNil verifies the routes are gated on a
// wired deviceUC, matching the mini-app's behavior.
func TestPanelDeviceRoutes_NotRegisteredWhenNil(t *testing.T) {
	h := &Handler{subUsecase: &subReturningUC{stubSubscriptionUsecase: &stubSubscriptionUsecase{}, sub: &domain.Subscription{}}}

	engine := gin.New()
	h.RegisterPublicRoutes(engine.Group(""))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/public/sub/abcd1234/devices", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (route absent) when deviceUC nil, got %d", w.Code)
	}
}

// TestPanelDevices_PasswordGate ensures device routes honor the panel password
// gate (custom mode with a hash, no cookie → 403) just like read endpoints.
func TestPanelDevices_PasswordGate(t *testing.T) {
	sub := &domain.Subscription{PanelPasswordMode: "custom", PanelPasswordHash: "$2a$10$abcdefghijklmnopqrstuv"}
	h := handlerWithDevice(sub, &fakeDeviceUC{})
	c, w := panelDeviceCtx(http.MethodGet, "")

	h.PanelDevices(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when password gate active, got %d", w.Code)
	}
}
