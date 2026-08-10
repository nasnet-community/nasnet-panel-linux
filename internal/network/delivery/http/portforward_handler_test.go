package http

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// 400 must carry the verdicts, so the UI can name the rule.
func TestPostPortForward_RejectionCarriesTheVerdicts(t *testing.T) {
	uc := &stubUsecase{portForwardVerdicts: []domain.Verdict{
		{Rule: "V27", Level: domain.LevelReject, Message: "tcp/443 is already used by an enabled xray inbound"},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/port-forwards",
		`{"proto":"tcp","dport":443,"to_addr":"10.77.0.5","to_port":443,"enabled":true}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "V27") {
		t.Errorf("response does not name the rule: %s", w.Body.String())
	}
}

func TestPostPortForward_ConfirmRequiredWithoutTheFlag(t *testing.T) {
	uc := &stubUsecase{portForwardVerdicts: []domain.Verdict{
		{Rule: "V28", Level: domain.LevelConfirm, Message: "this forward exposes the panel"},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/port-forwards",
		`{"proto":"tcp","dport":9761,"to_addr":"10.77.0.5","to_port":9761,"enabled":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 so the UI can prompt: %s", w.Code, w.Body.String())
	}

	w = do(t, newRouter(t, uc, true), "POST", "/api/v1/network/port-forwards",
		`{"proto":"tcp","dport":9761,"to_addr":"10.77.0.5","to_port":9761,"enabled":true,"confirmed":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("confirmed request = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestPortForwardRoutes_404WhenRouterModeIsOff(t *testing.T) {
	r := newRouter(t, &stubUsecase{}, false)
	for _, path := range []string{"/api/v1/network/port-forwards", "/api/v1/network/lan"} {
		if w := do(t, r, "GET", path, ""); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
}

// A network change, so it arms the dead-man instead of taking effect.
func TestPutLAN_ReturnsAConfirmDeadline(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "PUT", "/api/v1/network/lan",
		`{"enabled":true,"cidr":"10.77.0.1/24"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "confirm_deadline_unix") {
		t.Errorf("enabling the LAN did not arm the dead-man: %s", w.Body.String())
	}
}

// A colliding CIDR is a 400 naming the rule, like every validated change.
func TestPutLAN_RejectionCarriesTheVerdicts(t *testing.T) {
	uc := &stubUsecase{lanVerdicts: []domain.Verdict{
		{Rule: "V14", Level: domain.LevelReject, Message: "LAN 100.64.0.1/24 overlaps 100.64.0.0/10"},
	}}
	w := do(t, newRouter(t, uc, true), "PUT", "/api/v1/network/lan",
		`{"enabled":true,"cidr":"100.64.0.1/24"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "V14") {
		t.Errorf("response does not name the rule: %s", w.Body.String())
	}
}

// Deleting a forward re-renders the whole chain; a bad id must not 500.
func TestDeletePortForward_BadIDIsABadRequest(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "DELETE", "/api/v1/network/port-forwards/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400: %s", w.Code, w.Body.String())
	}
}
