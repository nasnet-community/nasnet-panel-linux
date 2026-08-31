package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

// json:"-" on the model is the only thing keeping the passphrase out of the
// radio list. This pins it against a refactor.
func TestListRadios_NeverCarriesAPSK(t *testing.T) {
	uc := &stubUsecase{radios: []usecase.RadioView{{
		Phy: "phy0", IfName: "wlp3s0", Key: "k2", Role: "lan", Mode: "ap",
		SupportsAP: true, CountryCode: "IR", CountryCodeSet: true,
		Config: &domain.WifiConfig{InterfaceID: 2, Mode: "ap", SSID: "nasnet",
			PSK: "sekritsekrit", CountryCode: "IR", Band: "2g", Enabled: true},
	}}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/wifi/radios", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sekritsekrit") {
		t.Fatalf("the PSK leaked into the radio list: %s", w.Body.String())
	}

	var env struct {
		Success bool                `json:"success"`
		Data    []usecase.RadioView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || len(env.Data) != 1 {
		t.Fatalf("envelope = %s", w.Body.String())
	}
	if env.Data[0].Config == nil || env.Data[0].Config.SSID != "nasnet" {
		t.Errorf("the rest of the intent was lost: %+v", env.Data[0].Config)
	}
}

func TestListRadios_ProbeFailureIs500(t *testing.T) {
	uc := &stubUsecase{radiosErr: errors.New("probe radios: no such phy")}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/wifi/radios", "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestEnableAP_PassesTheFormThrough(t *testing.T) {
	uc := &stubUsecase{}
	body := `{"interface_id":2,"ssid":"nasnet","psk":"hunter2hunter2",
		"country_code":"IR","band":"5g","channel":36,"hidden":true}`
	w := do(t, newRouter(t, uc, true), "PUT", "/api/v1/network/wifi/ap", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	got := uc.wifiEnabled
	if got == nil {
		t.Fatal("the usecase never saw the config")
	}
	if got.InterfaceID != 2 || got.SSID != "nasnet" || got.PSK != "hunter2hunter2" {
		t.Errorf("form lost: %+v", got)
	}
	if got.Band != "5g" || got.Channel != 36 || !got.Hidden {
		t.Errorf("radio settings lost: %+v", got)
	}
	// The apply view is what arms the countdown in the UI
	if !strings.Contains(w.Body.String(), "confirm_deadline_unix") {
		t.Errorf("no apply view: %s", w.Body.String())
	}
}

// Same protocol as every other apply: 400 with verdicts the UI renders
func TestEnableAP_RejectionCarriesVerdicts(t *testing.T) {
	uc := &stubUsecase{wifiVerdicts: []domain.Verdict{
		{Rule: "V39", Level: domain.LevelReject, Message: "the passphrase must be 8-63 characters"},
	}}
	body := `{"interface_id":2,"ssid":"nasnet","psk":"short","country_code":"IR","band":"2g"}`
	w := do(t, newRouter(t, uc, true), "PUT", "/api/v1/network/wifi/ap", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env struct {
		Success  bool             `json:"success"`
		Verdicts []domain.Verdict `json:"verdicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Success || len(env.Verdicts) != 1 || env.Verdicts[0].Rule != "V39" {
		t.Errorf("verdicts lost: %s", w.Body.String())
	}
}

func TestEnableAP_MissingFieldsAre400(t *testing.T) {
	uc := &stubUsecase{}
	for _, body := range []string{
		`{"ssid":"nasnet","country_code":"IR","band":"2g"}`,      // no interface
		`{"interface_id":2,"country_code":"IR","band":"2g"}`,     // no ssid
		`{"interface_id":2,"ssid":"nasnet","band":"2g"}`,         // no country
		`{"interface_id":2,"ssid":"nasnet","country_code":"IR"}`, // no band
	} {
		w := do(t, newRouter(t, uc, true), "PUT", "/api/v1/network/wifi/ap", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s gave status %d, want 400", body, w.Code)
		}
	}
}

// A role refusal is the operator's input, not our fault
func TestEnableAP_RoleRefusalIs400(t *testing.T) {
	uc := &stubUsecase{wifiEnableErr: errors.New("wlp3s0 is a station uplink, so it cannot beacon")}
	body := `{"interface_id":2,"ssid":"nasnet","psk":"hunter2hunter2","country_code":"IR","band":"2g"}`
	w := do(t, newRouter(t, uc, true), "PUT", "/api/v1/network/wifi/ap", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "station uplink") {
		t.Errorf("the reason was dropped: %s", w.Body.String())
	}
}

func TestScanWifi_PassesTheKeyThrough(t *testing.T) {
	uc := &stubUsecase{wifiNets: []system.WifiNetwork{
		{SSID: "upstream-net", Security: "WPA2", SignalDBm: -48, Known: true},
	}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/wifi/scan/k2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.wifiScanned != "k2" {
		t.Errorf("key = %q", uc.wifiScanned)
	}
	if !strings.Contains(w.Body.String(), "upstream-net") {
		t.Errorf("results lost: %s", w.Body.String())
	}
}

func TestConnectWifi_PassesCredentialsThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/wifi/connect/k2",
		`{"ssid":"upstream-net","psk":"hunter2hunter2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.wifiConnected != [3]string{"k2", "upstream-net", "hunter2hunter2"} {
		t.Errorf("got %v", uc.wifiConnected)
	}
	// The passphrase went in; it must not come back out
	if strings.Contains(w.Body.String(), "hunter2hunter2") {
		t.Errorf("the PSK was echoed: %s", w.Body.String())
	}
}

func TestConnectWifi_NoSSIDIs400(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "POST",
		"/api/v1/network/wifi/connect/k2", `{"psk":"hunter2hunter2"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDisableWifi_PassesTheKeyThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "DELETE", "/api/v1/network/wifi/k2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.wifiDisabled != "k2" {
		t.Errorf("key = %q", uc.wifiDisabled)
	}
}

// Router mode off 404s the whole subtree
func TestWifiRoutes_404WithRouterModeOff(t *testing.T) {
	r := newRouter(t, &stubUsecase{}, false)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/network/wifi/radios"},
		{"PUT", "/api/v1/network/wifi/ap"},
		{"POST", "/api/v1/network/wifi/scan/k2"},
	} {
		if w := do(t, r, tc.method, tc.path, "{}"); w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, w.Code)
		}
	}
}
