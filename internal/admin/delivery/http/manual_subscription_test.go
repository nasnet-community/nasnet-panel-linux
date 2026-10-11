package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
)

type manualAdmin struct {
	usecase.AdminUsecase
	got      *subUC.ManualSubscriptionRequest
	warnings []string
	err      error
}

func (a *manualAdmin) CreateManualSubscription(_ context.Context, req *subUC.ManualSubscriptionRequest) (*subDomain.Subscription, []string, error) {
	a.got = req
	if a.err != nil {
		return nil, nil, a.err
	}
	return &subDomain.Subscription{ID: 7, LinkKey: "k"}, a.warnings, nil
}

func postManual(t *testing.T, admin *manualAdmin, body string) (int, map[string]any) {
	t.Helper()
	handler := &Handler{adminUsecase: admin, baseURL: "https://panel.test"}
	router := gin.New()
	router.POST("/manual", handler.CreateManualSubscription)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/manual", strings.NewReader(body)))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, out
}

func TestCreateManualSubscription_NeedsInboundOrTunnel(t *testing.T) {
	for _, body := range []string{`{}`, `{"inbound_ids":[],"core_inbound_ids":[]}`} {
		admin := &manualAdmin{}
		code, out := postManual(t, admin, body)
		if code != 400 || admin.got != nil {
			t.Fatalf("%s: code=%d called=%v", body, code, admin.got != nil)
		}
		if msg, _ := out["error"].(string); !strings.Contains(msg, "inbound or tunnel") {
			t.Errorf("%s: error = %q", body, msg)
		}
	}
}

func TestCreateManualSubscription_TunnelOnly(t *testing.T) {
	admin := &manualAdmin{}
	code, out := postManual(t, admin, `{"label":"x","core_inbound_ids":[20,21]}`)
	if code != 200 {
		t.Fatalf("code = %d, body = %v", code, out)
	}
	if len(admin.got.InboundIDs) != 0 || fmt.Sprint(admin.got.CoreInboundIDs) != "[20 21]" {
		t.Errorf("request = %+v", admin.got)
	}
	if _, present := out["warnings"]; present {
		t.Errorf("no warnings expected: %v", out)
	}
}

func TestCreateManualSubscription_BadTunnelIs400(t *testing.T) {
	admin := &manualAdmin{err: fmt.Errorf("%w: tunnel 99 not found", subUC.ErrInvalidTunnel)}
	code, out := postManual(t, admin, `{"core_inbound_ids":[99]}`)
	if code != 400 {
		t.Fatalf("code = %d", code)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "tunnel 99 not found") {
		t.Errorf("error = %q", msg)
	}
}

func TestCreateManualSubscription_ReturnsWarnings(t *testing.T) {
	admin := &manualAdmin{warnings: []string{"SSH tunnel on port 22 (n1) was not attached: boom"}}
	code, out := postManual(t, admin, `{"inbound_ids":[1],"core_inbound_ids":[22]}`)
	if code != 200 {
		t.Fatalf("code = %d", code)
	}
	if fmt.Sprint(out["warnings"]) != "[SSH tunnel on port 22 (n1) was not attached: boom]" {
		t.Errorf("warnings = %v", out["warnings"])
	}
	data, _ := out["data"].(map[string]any)
	if data["subscription_url"] != "https://panel.test/sub/k" {
		t.Errorf("data = %v", data)
	}
}

// Xray-only requests keep the old response shape.
func TestCreateManualSubscription_XrayOnlyUnchanged(t *testing.T) {
	admin := &manualAdmin{}
	code, out := postManual(t, admin, `{"inbound_ids":[1,2]}`)
	if code != 200 || fmt.Sprint(admin.got.InboundIDs) != "[1 2]" || len(admin.got.CoreInboundIDs) != 0 {
		t.Fatalf("code=%d request=%+v", code, admin.got)
	}
	if _, present := out["warnings"]; present {
		t.Errorf("response = %v", out)
	}

}

// Bad Xray inbounds are the caller's problem; anything else stays a 500.
func TestCreateManualSubscription_StatusCodes(t *testing.T) {
	bad := fmt.Errorf("%w", subUC.ErrInvalidInbound)
	for _, tc := range []struct {
		err  error
		code int
		msg  string
	}{
		{&inputErr{subUC.ErrInvalidInbound, "inbound 1 is disabled"}, 400, "inbound 1 is disabled"},
		{&inputErr{subUC.ErrInvalidInbound, "no valid inbounds found for the given IDs"}, 400, "no valid inbounds found for the given IDs"},
		{bad, 400, "invalid inbound"},
		{fmt.Errorf("failed to fetch inbounds: db down"), 500, "failed to fetch inbounds: db down"},
		{fmt.Errorf("provisioning failed: node unreachable"), 500, "provisioning failed: node unreachable"},
	} {
		code, out := postManual(t, &manualAdmin{err: tc.err}, `{"inbound_ids":[1]}`)
		if code != tc.code || out["error"] != tc.msg {
			t.Errorf("%v: code=%d error=%v, want %d %q", tc.err, code, out["error"], tc.code, tc.msg)
		}
	}
}

// inputErr mimics the usecase's message-preserving sentinel wrapper.
type inputErr struct {
	kind error
	msg  string
}

func (e *inputErr) Error() string { return e.msg }
func (e *inputErr) Unwrap() error { return e.kind }
