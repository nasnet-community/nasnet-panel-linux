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
	uc := &stubUsecase{vpnProfiles: []usecase.VPNProfileView{{ID: 1, Name: "berlin", Active: true}}}
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

func TestActivateVPN_ArmsTheDeadManAndCarriesVerdicts(t *testing.T) {
	uc := &stubUsecase{
		vpnApplyView: &usecase.ApplyView{PlanID: 12, ConfirmDeadlineUnix: 99},
		vpnVerdicts:  []domain.Verdict{{Rule: "V33", Level: domain.LevelWarn, Message: "no uplink"}},
	}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/activate", `{"profile_id":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.vpnActivateID != 7 {
		t.Errorf("activated %d, want 7", uc.vpnActivateID)
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
func TestActivateVPN_RejectIs400WithVerdicts(t *testing.T) {
	uc := &stubUsecase{vpnVerdicts: []domain.Verdict{
		{Rule: "V34", Level: domain.LevelReject, Message: "could not resolve"},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/activate", `{"profile_id":1}`)
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

func TestActivateVPN_FailureIs500(t *testing.T) {
	uc := &stubUsecase{vpnApplyErr: errors.New("netlink is unhappy")}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/activate", `{"profile_id":1}`)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
}

// Turning it off with nothing on is a no-op, not an error.
func TestDeactivateVPN_NothingActiveStillSucceeds(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/vpn/deactivate", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !uc.vpnDeactived {
		t.Error("the usecase was never called")
	}
}

func TestVPNStatus_ReturnsTheEnvelope(t *testing.T) {
	uc := &stubUsecase{vpnStatus: &usecase.VPNStatusView{Connected: true, KillSwitch: true, MTU: 1420}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/vpn/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool                   `json:"success"`
		Data    *usecase.VPNStatusView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data == nil || !env.Data.Connected || !env.Data.KillSwitch {
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
		{"POST", "/api/v1/network/vpn/activate", `{"profile_id":1}`},
		{"POST", "/api/v1/network/vpn/deactivate", ""},
		{"GET", "/api/v1/network/vpn/status", ""},
	} {
		w := do(t, newRouter(t, &stubUsecase{}, false), tc.method, tc.path, tc.body)
		if w.Code == http.StatusOK {
			t.Errorf("%s %s answered outside router mode", tc.method, tc.path)
		}
	}
}
