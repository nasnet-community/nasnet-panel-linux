package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

func TestListVPNProfiles_ReturnsTheEnvelope(t *testing.T) {
	uc := &stubUsecase{vpnProfiles: []usecase.VPNProfileView{{ID: 1, Name: "berlin", Enabled: true}}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/vpn/profiles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	// The frontend unwraps {success, data} and reads a bare body as a failure
	// with no message.
	var env struct {
		Success bool                     `json:"success"`
		Data    []usecase.VPNProfileView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || len(env.Data) != 1 || env.Data[0].Name != "berlin" {
		t.Errorf("got %s", w.Body.String())
	}
}

func TestCreateVPNProfile_PassesThePastedText(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles",
		`{"name":"berlin","raw":"wireguard://x@1.2.3.4:51820"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnCreated == nil || uc.vpnCreated.Raw == "" || uc.vpnCreated.Name != "berlin" {
		t.Errorf("got %+v", uc.vpnCreated)
	}
}

// A config the parser refuses is bad input, not a server fault.
func TestCreateVPNProfile_RejectionIs400(t *testing.T) {
	uc := &stubUsecase{vpnCreateErr: domain.ErrScriptKey}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles",
		`{"name":"x","raw":"[Interface]\nPostUp = rm -rf /"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// Deleting the row under a live tunnel would leave nothing to turn it off with.
func TestDeleteVPNProfile_ActiveIs400(t *testing.T) {
	uc := &stubUsecase{vpnDeleteErr: domain.ErrProfileActive}
	w := do(t, newRouter(t, uc, true), "DELETE", "/api/v1/network/vpn/profiles/3", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if uc.vpnDeletedID != 3 {
		t.Errorf("deleted %d, want 3", uc.vpnDeletedID)
	}
}

// The preview must be a dry run: nothing stored until the operator says so.
func TestParseVPNInput_StoresNothing(t *testing.T) {
	uc := &stubUsecase{vpnVerdicts: []domain.Verdict{
		{Rule: "V32", Level: domain.LevelWarn, Message: "narrow"},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/parse",
		`{"raw":"wireguard://x@1.2.3.4:51820"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnCreated != nil {
		t.Error("a preview created a profile")
	}
	var env struct {
		Success  bool                    `json:"success"`
		Data     *domain.WireGuardConfig `json:"data"`
		Verdicts []domain.Verdict        `json:"verdicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data == nil || len(env.Verdicts) != 1 {
		t.Errorf("got %s", w.Body.String())
	}
}

func TestParseVPNInput_UnreadableIs400(t *testing.T) {
	uc := &stubUsecase{vpnParseErr: domain.ErrNotWireGuard}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/parse", `{"raw":"hello"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGenerateVPNKeypair_ReturnsBothHalves(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "POST", "/api/v1/network/vpn/keypair", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			PrivateKey string `json:"private_key"`
			PublicKey  string `json:"public_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data.PrivateKey == "" || env.Data.PublicKey == "" {
		t.Errorf("got %s", w.Body.String())
	}
}

func TestEnableVPNProfile_ArmsTheDeadManAndCarriesVerdicts(t *testing.T) {
	uc := &stubUsecase{
		vpnApplyView: &usecase.ApplyView{PlanID: 12, ConfirmDeadlineUnix: 99},
		vpnVerdicts:  []domain.Verdict{{Rule: "V33", Level: domain.LevelWarn, Message: "no uplink"}},
	}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles/7/enable", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnEnabledID != 7 {
		t.Errorf("enabled %d, want 7", uc.vpnEnabledID)
	}
	// Verdicts sit beside data, not inside it: the UI reads them either way.
	var env struct {
		Success  bool               `json:"success"`
		Data     *usecase.ApplyView `json:"data"`
		Verdicts []domain.Verdict   `json:"verdicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data == nil || env.Data.PlanID != 12 || len(env.Verdicts) != 1 {
		t.Errorf("got %s", w.Body.String())
	}
}

// A reject applies nothing, and the reason has to survive to the UI.
func TestEnableVPNProfile_RejectIs400WithVerdicts(t *testing.T) {
	uc := &stubUsecase{vpnVerdicts: []domain.Verdict{
		{Rule: "V34", Level: domain.LevelReject, Message: "could not resolve"},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles/1/enable", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env struct {
		Verdicts []domain.Verdict `json:"verdicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Verdicts) != 1 || env.Verdicts[0].Rule != "V34" {
		t.Errorf("the reason was lost: %s", w.Body.String())
	}
}

func TestEnableVPNProfile_FailureIs500(t *testing.T) {
	uc := &stubUsecase{vpnApplyErr: errors.New("netlink is unhappy")}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles/1/enable", "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
}

// Turning off a member that is already off is a no-op, not an error.
func TestDisableVPNProfile_NothingEnabledStillSucceeds(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/profiles/4/disable", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnDisabledID != 4 {
		t.Error("the usecase was never called")
	}
}

func TestSetVPNProfileRole_PassesTheRoleThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "PATCH", "/api/v1/network/vpn/profiles/5/role",
		`{"priority":2,"weight":40}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnRoleID != 5 || uc.vpnRolePrio != 2 || uc.vpnRoleWeight != 40 {
		t.Errorf("role call = id %d prio %d weight %d", uc.vpnRoleID, uc.vpnRolePrio, uc.vpnRoleWeight)
	}
}

func TestSetVPNProfileRole_BadRangeIs400(t *testing.T) {
	uc := &stubUsecase{vpnRoleErr: usecase.ErrValidationFailed}
	w := do(t, newRouter(t, uc, true), "PATCH", "/api/v1/network/vpn/profiles/5/role",
		`{"priority":9,"weight":1}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestVPNStatus_ReturnsTheEnvelope(t *testing.T) {
	uc := &stubUsecase{vpnStatus: &usecase.VPNPoolStatusView{
		Tunnels: []usecase.TunnelStatusView{{ProfileID: 1, Name: "berlin", Connected: true, MTU: 1420, InPool: true}},
		KillSwitch: true,
	}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/vpn/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool                       `json:"success"`
		Data    *usecase.VPNPoolStatusView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data == nil || len(env.Data.Tunnels) != 1 ||
		!env.Data.Tunnels[0].Connected || !env.Data.KillSwitch {
		t.Errorf("got %s", w.Body.String())
	}
}

func TestVPNRoutes_RequireRouterMode(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/network/vpn/profiles", ""},
		{"POST", "/api/v1/network/vpn/profiles", `{"name":"x","raw":"y"}`},
		{"PUT", "/api/v1/network/vpn/profiles/1", `{"name":"x","raw":"y"}`},
		{"DELETE", "/api/v1/network/vpn/profiles/1", ""},
		{"POST", "/api/v1/network/vpn/parse", `{"raw":"x"}`},
		{"POST", "/api/v1/network/vpn/keypair", ""},
		{"POST", "/api/v1/network/vpn/profiles/1/enable", ""},
		{"POST", "/api/v1/network/vpn/profiles/1/disable", ""},
		{"PATCH", "/api/v1/network/vpn/profiles/1/role", `{"priority":0,"weight":1}`},
		{"GET", "/api/v1/network/vpn/status", ""},
	} {
		w := do(t, newRouter(t, &stubUsecase{}, false), tc.method, tc.path, tc.body)
		if w.Code == http.StatusOK {
			t.Errorf("%s %s answered outside router mode", tc.method, tc.path)
		}
	}
}

func TestSetVPNProfileTransport_PassesThePinThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "PATCH", "/api/v1/network/vpn/profiles/5/transport",
		`{"uplink_key":"k-lte0"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnTransportID != 5 || uc.vpnTransportKey != "k-lte0" {
		t.Errorf("transport call = id %d key %q", uc.vpnTransportID, uc.vpnTransportKey)
	}
}

// An empty key is how the UI says "back to automatic", not a bad request.
func TestSetVPNProfileTransport_EmptyKeyClearsThePin(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "PATCH", "/api/v1/network/vpn/profiles/5/transport",
		`{"uplink_key":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnTransportKey != "" {
		t.Errorf("key = %q, want cleared", uc.vpnTransportKey)
	}
}

func TestSetVPNProfileTransport_UnknownUplinkIs400(t *testing.T) {
	uc := &stubUsecase{vpnTransportErr: usecase.ErrValidationFailed}
	w := do(t, newRouter(t, uc, true), "PATCH", "/api/v1/network/vpn/profiles/5/transport",
		`{"uplink_key":"k-nope"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
