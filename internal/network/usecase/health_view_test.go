package usecase

import (
	"context"
	"strings"
	"testing"
)

func healthFixture(t *testing.T, o flowOpts, targetsUp bool) *networkUsecase {
	t.Helper()
	u := newFlowFixture(t, o)
	u.health = NewHealthMonitor(u.Backend, &scriptedProbe{carrier: true, gateway: true}, DefaultDamping())
	up := map[string]bool{}
	if targetsUp {
		for _, tg := range DefaultHealthConfig().TargetsDomestic {
			up[tg.Address] = true
		}
		for _, tg := range DefaultHealthConfig().TargetsForeign {
			up[tg.Address] = true
		}
	}
	u.Prober = &fakeProber{up: up}
	return u
}

func TestHealthStateReportsTheLadder(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	// The gateway damper needs six clean ticks before the internet layer counts.
	for i := 0; i < 7; i++ {
		u.probeOnce(context.Background())
	}
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Uplinks) != 2 {
		t.Fatalf("want both uplinks, got %d", len(view.Uplinks))
	}
	for _, up := range view.Uplinks {
		if up.Verdict == "" || up.Internet == "" || up.Gateway == "" {
			t.Fatalf("empty ladder for %s: %+v", up.IfName, up)
		}
		if len(up.History) == 0 {
			t.Fatalf("no history for %s after a tick", up.IfName)
		}
		if len(up.Targets) == 0 {
			t.Fatalf("no target results for %s", up.IfName)
		}
	}
	if view.VPN == nil || !view.VPN.Present {
		t.Fatal("active tunnel must report a VPN health block")
	}
}

func TestHealthStateNoVPNMeansNoVPNBlock(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	u.probeOnce(context.Background())
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.VPN != nil {
		t.Fatal("no tunnel, but a VPN block appeared")
	}
}

func TestFlowUplinkNodeNamesTheBrokenLayer(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, false)
	// Six ticks bring the gateway up; five more drop the internet damper.
	for i := 0; i < 12; i++ {
		u.probeOnce(context.Background())
	}
	view, err := u.FlowGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := nodeByID(t, view, "uplink-domestic")
	if n.Status == "ok" {
		t.Fatal("internet-dead uplink still reads ok on the flow page")
	}
	low := strings.ToLower(n.Hint)
	if !strings.Contains(low, "internet") && !strings.Contains(low, "tunnel") {
		t.Fatalf("hint does not name the failure: %q", n.Hint)
	}
}

func TestFlowUplinkNodeShowsProbeDetail(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	for i := 0; i < 7; i++ {
		u.probeOnce(context.Background())
	}
	view, err := u.FlowGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := nodeByID(t, view, "uplink-domestic")
	if n.Status != "ok" {
		t.Fatalf("clean uplink must be ok, got %s (%s)", n.Status, n.Hint)
	}
	found := false
	for _, d := range n.Detail {
		if d.Title == "health" && len(d.Lines) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("no health detail section on the uplink node")
	}
}

func TestVPNHealthClearsWhenTheTunnelGoesAway(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	u.probeOnce(context.Background())
	if view, _ := u.HealthState(context.Background()); view.VPN == nil {
		t.Fatal("active tunnel must have a VPN block first")
	}
	// Profile deactivated: readings must go with it, not freeze.
	u.VPNRepo = &fakeVPNRepo{}
	u.probeOnce(context.Background())
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.VPN != nil {
		t.Fatal("stale tunnel readings survived deactivation")
	}
}

// A captive portal upstream of a station uplink reads as "gateway up, internet
// down" forever, which looks like a broken probe list unless it is named.
func TestUplinkNote_NamesTheCaptivePortalShape(t *testing.T) {
	got := uplinkNote("wifi_pci", "up", "down", false)
	if !strings.Contains(got, "captive portal") {
		t.Fatalf("note = %q", got)
	}

	cases := map[string]struct {
		source, gateway, internet string
		everUp                    bool
	}{
		"ethernet uplink":       {"eth_onboard", "up", "down", false},
		"internet has been up":  {"wifi_pci", "up", "down", true},
		"internet currently up": {"wifi_pci", "up", "up", true},
		"gateway down":          {"wifi_pci", "down", "down", false},
		"internet unknown":      {"wifi_pci", "up", "unknown", false},
		"nothing measured yet":  {"wifi_pci", "", "", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := uplinkNote(c.source, c.gateway, c.internet, c.everUp); got != "" {
				t.Errorf("unexpected note %q", got)
			}
		})
	}
}
