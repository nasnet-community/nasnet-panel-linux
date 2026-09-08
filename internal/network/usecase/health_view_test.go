package usecase

import (
	"context"
	"strings"
	"testing"
	"time"
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

// The card says who is carrying a failed line; the banner only fires for the pool.
func TestHealthStateReportsWhereEachDomesticRides(t *testing.T) {
	u, prober := twoDomesticFixture(t)
	tick(u, 7)
	prober.setDown("eth0", true)
	tick(u, 6)
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byIf := map[string]UplinkHealthView{}
	for _, up := range view.Uplinks {
		byIf[up.IfName] = up
	}
	if byIf["eth0"].Via != "eth2" {
		t.Fatalf("eth0 via %q, want eth2", byIf["eth0"].Via)
	}
	if byIf["eth2"].Via != "" || byIf["eth1"].Via != "" {
		t.Fatalf("healthy lines must not ride anything: %+v", view.Uplinks)
	}
	if view.FailoverActive {
		t.Fatal("a sibling ride must not raise the pool banner")
	}
}

// A name the operator typed is worth nothing if the panel cannot read it back.
func TestTargetViewsCarryTheOperatorsName(t *testing.T) {
	got := targetViews([]ProbeResult{
		{Target: ProbeTarget{Address: "10.0.0.9:53", Proto: "dns", Label: "Head-end"}, OK: true},
		{Target: ProbeTarget{Address: "1.1.1.1:443", Proto: "tcp"}, OK: false, Err: "timeout"},
	})
	if len(got) != 2 {
		t.Fatalf("%d views, want 2", len(got))
	}
	if got[0].Label != "Head-end" {
		t.Errorf("label = %q, want the one that was stored", got[0].Label)
	}
	if got[1].Label != "" {
		t.Errorf("an unnamed target must stay unnamed, got %q", got[1].Label)
	}
}

// A line's card has to name the device it dials, and for a DHCP uplink the
// only place that address exists is the learned column.
func TestHealthStateReportsTheUpstreamAddress(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	u.probeOnce(context.Background())
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"eth0": "192.0.2.1", "eth1": "100.64.0.1"}
	for _, up := range view.Uplinks {
		if up.GatewayIP != want[up.IfName] {
			t.Errorf("%s gateway = %q, want %q", up.IfName, up.GatewayIP, want[up.IfName])
		}
	}
}

// "working for 3 days" needs a moment to count from.
func TestHealthStateReportsWhenTheLineLastChanged(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	for i := 0; i < 7; i++ {
		u.probeOnce(context.Background())
	}
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, up := range view.Uplinks {
		if up.SinceUnix == 0 {
			t.Errorf("%s has no transition time", up.IfName)
			continue
		}
		if up.SinceUnix > now || now-up.SinceUnix > 60 {
			t.Errorf("%s changed at %d, which is not just now (%d)", up.IfName, up.SinceUnix, now)
		}
	}
}

// Recovery is the one number that turns "waiting" into "nearly back".
func TestHealthStateRecoveryOnlyWhileTheLineIsDown(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	for i := 0; i < 7; i++ {
		u.probeOnce(context.Background())
	}
	view, _ := u.HealthState(context.Background())
	for _, up := range view.Uplinks {
		if up.Recovery != nil {
			t.Fatalf("%s is up, so it has nothing to recover from: %+v", up.IfName, up.Recovery)
		}
	}

	// Drop the internet: five failures put the damper down.
	u.Prober = &fakeProber{up: map[string]bool{}}
	for i := 0; i < 6; i++ {
		u.probeOnce(context.Background())
	}
	// Then one clean tick, which starts counting toward the return.
	u.Prober = &fakeProber{up: allDefaultTargets()}
	u.probeOnce(context.Background())

	view, _ = u.HealthState(context.Background())
	for _, up := range view.Uplinks {
		if up.Recovery == nil {
			t.Fatalf("%s lost the internet, so it needs a recovery count", up.IfName)
		}
		if up.Recovery.Needed != 12 {
			t.Errorf("%s needs %d passes, want 12", up.IfName, up.Recovery.Needed)
		}
		if up.Recovery.Passes != 1 {
			t.Errorf("%s has %d passes, want the one clean tick", up.IfName, up.Recovery.Passes)
		}
		if up.Recovery.DwellSecondsLeft <= 0 || up.Recovery.DwellSecondsLeft > 120 {
			t.Errorf("%s dwell left = %d, want inside the 120 s window",
				up.IfName, up.Recovery.DwellSecondsLeft)
		}
	}
}

func TestHealthStateReportsBytesAndItsOwnRoutes(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	u.probeOnce(context.Background())
	view, err := u.HealthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byIf := map[string]UplinkHealthView{}
	for _, up := range view.Uplinks {
		byIf[up.IfName] = up
	}
	if got := byIf["eth0"]; got.RxBytes != 1000 || got.TxBytes != 2000 {
		t.Errorf("eth0 bytes = %d/%d, want 1000/2000", got.RxBytes, got.TxBytes)
	}
	if got := byIf["eth1"]; got.RxBytes != 3000 || got.TxBytes != 4000 {
		t.Errorf("eth1 bytes = %d/%d, want 3000/4000", got.RxBytes, got.TxBytes)
	}
	// One line's routes, not the whole group's: table 201 is eth0's alone.
	eth0 := byIf["eth0"].Routes
	if len(eth0) == 0 {
		t.Fatal("eth0 reported no routes")
	}
	joined := strings.Join(eth0, "\n")
	if !strings.Contains(joined, "192.0.2.1") || !strings.Contains(joined, "eth0") {
		t.Errorf("eth0 routes do not describe its own table: %v", eth0)
	}
	if strings.Contains(joined, "100.64.0.1") {
		t.Errorf("eth0 routes leaked eth1's table: %v", eth0)
	}
}

func allDefaultTargets() map[string]bool {
	up := map[string]bool{}
	for _, tg := range DefaultHealthConfig().TargetsDomestic {
		up[tg.Address] = true
	}
	for _, tg := range DefaultHealthConfig().TargetsForeign {
		up[tg.Address] = true
	}
	return up
}
