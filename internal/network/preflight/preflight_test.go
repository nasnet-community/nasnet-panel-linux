package preflight

import (
	"strings"
	"testing"
)

func ok() Env {
	return Env{
		OSID: "ubuntu", OSVersionID: "24.04",
		InContainer: false, HasNetAdmin: true, NetworkdActive: true,
		NMMasked: true, TakeoverDone: true, AssignableNICs: 2,
	}
}

func TestCheck_HappyPath(t *testing.T) {
	r := Check(ok())
	if !r.OK() {
		t.Fatalf("healthy env rejected: %+v", r)
	}
	if len(r.Warn) != 0 {
		t.Errorf("unexpected warnings: %v", r.Warn)
	}
}

func TestCheck_FatalCases(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Env)
		wantMsg string
	}{
		{"not ubuntu", func(e *Env) { e.OSID = "debian" }, "Ubuntu 24.04"},
		{"wrong ubuntu", func(e *Env) { e.OSVersionID = "22.04" }, "Ubuntu 24.04"},
		{"container", func(e *Env) { e.InContainer = true }, "container"},
		{"no cap", func(e *Env) { e.HasNetAdmin = false }, "CAP_NET_ADMIN"},
		{"networkd down", func(e *Env) { e.NetworkdActive = false }, "systemd-networkd"},
		{"nm unmasked after takeover", func(e *Env) { e.NMMasked = false }, "NetworkManager"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := ok()
			c.mutate(&e)
			r := Check(e)
			if r.OK() {
				t.Fatalf("%s was accepted", c.name)
			}
			joined := strings.Join(r.Fatal, " | ")
			if !strings.Contains(joined, c.wantMsg) {
				t.Errorf("fatal messages %q do not mention %q", joined, c.wantMsg)
			}
		})
	}
}

// Pre-takeover, netplan and NM still own the network legitimately. Fatal here
// would stop the box booting into the screen needed to fix it.
func TestCheck_UnmaskedNMIsNotFatalBeforeTakeover(t *testing.T) {
	e := ok()
	e.NMMasked = false
	e.TakeoverDone = false
	r := Check(e)
	if !r.OK() {
		t.Fatalf("pre-takeover NM should not be fatal: %+v", r)
	}
	if len(r.Warn) == 0 {
		t.Error("pre-takeover state should still warn so the UI can show the finish-setup banner")
	}
}

// Single uplink is a degraded state, not an error.
func TestCheck_FewerThanTwoNICsWarnsOnly(t *testing.T) {
	e := ok()
	e.AssignableNICs = 1
	r := Check(e)
	if !r.OK() {
		t.Fatalf("single NIC must not be fatal: %+v", r)
	}
	if len(r.Warn) == 0 {
		t.Error("single NIC must warn")
	}
}
