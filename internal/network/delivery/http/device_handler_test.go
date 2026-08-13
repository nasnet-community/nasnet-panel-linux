package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

func TestListDevices_ReturnsTheList(t *testing.T) {
	uc := &stubUsecase{devices: &usecase.LANDeviceList{
		Devices: []usecase.LANDevice{{MAC: "b8:27:eb:aa:bb:01", Online: true}},
		Enabled: true, LeasesOK: true, NeighboursOK: true,
		OfflineAfterSeconds: 300,
	}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/lan/devices", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	// The envelope, not just the payload: the frontend unwraps {success, data}
	// and treats a bare body as a failure with no message.
	var env struct {
		Success bool                  `json:"success"`
		Data    usecase.LANDeviceList `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success {
		t.Fatalf("no success envelope: %s", w.Body.String())
	}
	if len(env.Data.Devices) != 1 || env.Data.OfflineAfterSeconds != 300 {
		t.Errorf("got %+v", env.Data)
	}
}

// Losing the bridge means the answer is unknown, not empty.
func TestListDevices_BridgeFailureIsAnError(t *testing.T) {
	uc := &stubUsecase{devicesErr: errors.New("bridge fdb: no such device")}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/lan/devices", "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
}

// A colon-bearing MAC has to survive the path segment.
func TestSetDeviceLabel_PassesTheMACThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "PUT",
		"/api/v1/network/lan/devices/b8:27:eb:aa:bb:01/label", `{"label":"the NAS"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.labelledMAC != "b8:27:eb:aa:bb:01" || uc.labelledAs != "the NAS" {
		t.Errorf("got mac=%q label=%q", uc.labelledMAC, uc.labelledAs)
	}
}

func TestSetDeviceLabel_RejectionIs400(t *testing.T) {
	uc := &stubUsecase{setLabelErr: errors.New("randomized")}
	w := do(t, newRouter(t, uc, true), "PUT",
		"/api/v1/network/lan/devices/b2:27:eb:aa:bb:02/label", `{"label":"phone"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// Clearing a name is a blank label, not a DELETE.
func TestSetDeviceLabel_BlankClears(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "PUT",
		"/api/v1/network/lan/devices/b8:27:eb:aa:bb:01/label", `{"label":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if uc.labelledMAC != "b8:27:eb:aa:bb:01" || uc.labelledAs != "" {
		t.Errorf("got mac=%q label=%q", uc.labelledMAC, uc.labelledAs)
	}
}

func TestDeviceRoutes_RequireRouterMode(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/network/lan/devices", ""},
		{"PUT", "/api/v1/network/lan/devices/b8:27:eb:aa:bb:01/label", `{"label":"x"}`},
	} {
		w := do(t, newRouter(t, &stubUsecase{}, false), tc.method, tc.path, tc.body)
		if w.Code == http.StatusOK {
			t.Errorf("%s %s answered outside router mode", tc.method, tc.path)
		}
	}
}
