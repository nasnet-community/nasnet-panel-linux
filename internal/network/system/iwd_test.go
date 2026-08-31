package system

import (
	"context"
	"testing"
)

func TestFakeStationClient_SatisfiesTheSeam(t *testing.T) {
	var _ StationClient = NewFakeStationClient()
}

func TestFakeStationClient_ConnectRecordsTheSSID(t *testing.T) {
	ctx := context.Background()
	f := NewFakeStationClient()
	f.Nets = []WifiNetwork{{SSID: "home", Security: "WPA2", SignalDBm: -52}}

	if err := f.Scan(ctx, "wlp3s0"); err != nil {
		t.Fatal(err)
	}
	if len(f.Scanned) != 1 || f.Scanned[0] != "wlp3s0" {
		t.Errorf("Scanned = %v", f.Scanned)
	}

	nets, err := f.Networks(ctx, "wlp3s0")
	if err != nil || len(nets) != 1 {
		t.Fatalf("Networks = %+v, %v", nets, err)
	}
	if err := f.Connect(ctx, "wlp3s0", "home", "hunter2hunter2"); err != nil {
		t.Fatal(err)
	}
	if f.Connected["wlp3s0"] != "home" {
		t.Errorf("Connected = %v", f.Connected)
	}
	if st, _ := f.State(ctx, "wlp3s0"); st != "connected" {
		t.Errorf("State = %q", st)
	}
	if err := f.Disconnect(ctx, "wlp3s0"); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.State(ctx, "wlp3s0"); st != "disconnected" {
		t.Errorf("State after disconnect = %q", st)
	}
}

// iwd reports a failed association asynchronously with no error back, so
// refuse what cannot possibly associate before it sees it
func TestStationConnect_RefusesAWeakPSK(t *testing.T) {
	if err := NewFakeStationClient().Connect(context.Background(), "wlp3s0", "home", "short"); err == nil {
		t.Error("Connect accepted a PSK shorter than 8 characters")
	}
}

func TestStationConnect_RefusesAnEmptySSID(t *testing.T) {
	if err := NewFakeStationClient().Connect(context.Background(), "wlp3s0", "", "hunter2hunter2"); err == nil {
		t.Error("Connect accepted an empty SSID")
	}
}

// An open network has no passphrase at all
func TestStationConnect_AllowsAnEmptyPSK(t *testing.T) {
	if err := NewFakeStationClient().Connect(context.Background(), "wlp3s0", "open-net", ""); err != nil {
		t.Errorf("Connect refused an open network: %v", err)
	}
}

func TestForget_DropsTheAssociation(t *testing.T) {
	ctx := context.Background()
	f := NewFakeStationClient()
	if err := f.Connect(ctx, "wlp3s0", "home", "hunter2hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := f.Forget(ctx, "wlp3s0", "home"); err != nil {
		t.Fatal(err)
	}
	if f.Connected["wlp3s0"] != "" {
		t.Errorf("Connected = %v", f.Connected)
	}
}

// Bars, not a negative number nobody reads
func TestSignalPercent(t *testing.T) {
	cases := map[int]int{-30: 100, -50: 100, -75: 50, -100: 0, -120: 0}
	for dbm, want := range cases {
		if got := SignalPercent(dbm); got != want {
			t.Errorf("SignalPercent(%d) = %d, want %d", dbm, got, want)
		}
	}
	prev := -1
	for dbm := -100; dbm <= -30; dbm += 5 {
		p := SignalPercent(dbm)
		if p < prev {
			t.Errorf("SignalPercent is not monotonic at %d dBm", dbm)
		}
		prev = p
	}
}

func TestIWDSecurityLabel(t *testing.T) {
	cases := map[string]string{
		"psk":   "WPA2",
		"open":  "Open",
		"8021x": "Enterprise",
		"wep":   "WEP (insecure)",
		"":      "Unknown",
	}
	for raw, want := range cases {
		if got := iwdSecurityLabel(raw); got != want {
			t.Errorf("iwdSecurityLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}

// iwd hex-escapes an SSID that is not plain printable ASCII
func TestIWDEscapeSSID(t *testing.T) {
	if got := iwdEscapeSSID("home-net"); got != "home-net" {
		t.Errorf("plain SSID escaped: %q", got)
	}
	for _, ssid := range []string{"کافه", "a/b", "a=b", "tab\there"} {
		got := iwdEscapeSSID(ssid)
		if got == ssid || got[0] != '=' {
			t.Errorf("iwdEscapeSSID(%q) = %q, want a hex-wrapped form", ssid, got)
		}
	}
}
