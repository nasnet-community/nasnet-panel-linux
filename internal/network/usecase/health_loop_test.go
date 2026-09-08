package usecase

import (
	"context"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func TestVerdictLadderCollapsesTopDown(t *testing.T) {
	cases := []struct {
		name                                                string
		force                                               string
		carrier, gwKnown, gwUp, inetUp, inetKnown, degraded bool
		want                                                string
	}{
		{"forced down wins over everything", "down", true, true, true, true, true, false, "forced-down"},
		{"forced up wins too", "up", false, true, false, false, true, false, "forced-up"},
		{"no carrier", "", false, true, true, true, true, false, "no-carrier"},
		{"cold damper is warm-up, not an outage", "", true, false, false, true, true, false, ""},
		{"gateway dead", "", true, true, false, true, true, false, "no-gateway"},
		{"internet dead", "", true, true, true, false, true, false, "no-internet"},
		{"internet unknown falls back to gateway", "", true, true, true, false, false, false, "up"},
		{"degraded", "", true, true, true, true, true, true, "degraded"},
		{"clean", "", true, true, true, true, true, false, "up"},
	}
	for _, c := range cases {
		if got := verdictFor(c.force, c.carrier, c.gwKnown, c.gwUp, c.inetUp, c.inetKnown, c.degraded); got != c.want {
			t.Fatalf("%s: want %q got %q", c.name, c.want, got)
		}
	}
}

// The damper starts optimistic, so "up" without a reply would be a lie.
func TestTunnelVerdictNeedsEvidence(t *testing.T) {
	cases := []struct {
		name                   string
		answered, up, degraded bool
		want                   string
	}{
		{"warm-up: optimistic damper, nothing answered yet", false, true, false, ""},
		{"damper gave up", false, false, false, "no-internet"},
		{"answering but lossy", true, true, true, "degraded"},
		{"answering cleanly", true, true, false, "up"},
	}
	for _, c := range cases {
		if got := tunnelVerdict(c.answered, c.up, c.degraded); got != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, got)
		}
	}
}

func TestSetUplinkForceRejectsGarbage(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	if err := u.SetUplinkForce(context.Background(), "eth0", "sideways"); err == nil {
		t.Fatal("junk state must be rejected")
	}
	if err := u.SetUplinkForce(context.Background(), "nosuch", "up"); err == nil {
		t.Fatal("unknown key must be rejected")
	}
}

func TestSetUplinkForceAcceptsTheThreeStates(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	for _, state := range []string{"up", "down", ""} {
		if err := u.SetUplinkForce(context.Background(), "eth0", state); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
}

// A secondary carries tunnels only, so its policy is one line.
func TestSecondaryRouteStateOnlyAGatewayDeathWithdraws(t *testing.T) {
	if got := secondaryRouteState(true); got != routeUp {
		t.Fatalf("gateway up: want routeUp, got %v", got)
	}
	if got := secondaryRouteState(false); got != routeWithdraw {
		t.Fatalf("gateway dead: want routeWithdraw, got %v", got)
	}
}

func domesticRouteFixture(t *testing.T) (*networkUsecase, *system.FakeBackend, Uplink) {
	t.Helper()
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	be := u.Backend.(*system.FakeBackend)
	up := Uplink{IfName: "eth0", Key: "eth0", Table: 201, UplinkIndex: 1, Slot: domain.SlotDomestic}
	u.health.Observe(context.Background(), up, "192.0.2.1", "up") // mark EverUp
	return u, be, up
}

func defaultsIn(t *testing.T, be *system.FakeBackend, table int) []system.Route {
	t.Helper()
	routes, _ := be.RouteList(context.Background(), table)
	var out []system.Route
	for _, r := range routes {
		if r.Dest == "default" {
			out = append(out, r)
		}
	}
	return out
}

// Found on the VM: failover replaced the only route the domestic probe could
// use, so recovery became unobservable. The gateway path must stay alive.
func TestPoolMirrorKeepsAProbeRouteOutTheRealUplink(t *testing.T) {
	u, be, up := domesticRouteFixture(t)
	if err := u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaPool}); err != nil {
		t.Fatal(err)
	}
	var viaTunnel, viaGateway bool
	for _, r := range defaultsIn(t, be, 201) {
		for _, nh := range r.Nexthops {
			if nh.OifName == system.WGLinkName {
				viaTunnel = true
			}
		}
		if r.Gateway == "192.0.2.1" && r.Metric > 0 {
			viaGateway = true
		}
	}
	if !viaTunnel {
		t.Fatal("failover did not route into the pool")
	}
	if !viaGateway {
		t.Fatal("failover starved the probe: no gateway route left in the table")
	}
	if u.viaOf("eth0") != "pool" || !u.poolFailoverActive() {
		t.Fatalf("via = %q, pool active = %v", u.viaOf("eth0"), u.poolFailoverActive())
	}
}

// The mirror is the sibling's own default; the probe path sits under it at
// metric 100 so the oif-bound socket still dials the real ISP.
func TestSiblingMirrorKeepsAProbeRouteOutTheRealUplink(t *testing.T) {
	u, be, up := domesticRouteFixture(t)
	sib := domesticObs{Up: Uplink{IfName: "eth2", Table: 211, UplinkIndex: 6, Slot: domain.SlotDomestic2},
		Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true}
	if err := u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaSibling, Sibling: sib}); err != nil {
		t.Fatal(err)
	}
	var mirror, probe bool
	for _, r := range defaultsIn(t, be, 201) {
		if r.Metric == 0 && r.Gateway == "198.51.100.1" && r.OifName == "eth2" {
			mirror = true
		}
		if r.Metric == probeRouteMetric && r.Gateway == "192.0.2.1" && r.OifName == "eth0" {
			probe = true
		}
	}
	if !mirror || !probe {
		t.Fatalf("mirror=%v probe=%v in %+v", mirror, probe, defaultsIn(t, be, 201))
	}
	if u.viaOf("eth0") != "eth2" || u.poolFailoverActive() {
		t.Fatalf("via = %q, pool active = %v", u.viaOf("eth0"), u.poolFailoverActive())
	}
}

// The kernel deletes only the lowest-metric default per call, so a withdraw
// after a mirror must clear the probe helper route too.
func TestWithdrawAfterMirrorClearsBothDefaults(t *testing.T) {
	u, be, up := domesticRouteFixture(t)
	_ = u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaPool})
	_ = u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaNone})
	if ds := defaultsIn(t, be, 201); len(ds) != 0 {
		t.Fatalf("a default survived the withdraw: %+v", ds)
	}
	if u.viaOf("eth0") != "" {
		t.Fatalf("via = %q after withdraw", u.viaOf("eth0"))
	}
}

// Coming home drops the helper: two defaults via the same gateway are not a
// bug, but they are a stale mirror waiting to confuse the next reader.
func TestReturningHomeLeavesOneDefault(t *testing.T) {
	u, be, up := domesticRouteFixture(t)
	sib := domesticObs{Up: Uplink{IfName: "eth2", Table: 211, UplinkIndex: 6}, Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true}
	_ = u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaSibling, Sibling: sib})
	_ = u.applyDomesticRoute(context.Background(), domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaOwn})
	ds := defaultsIn(t, be, 201)
	if len(ds) != 1 || ds[0].Gateway != "192.0.2.1" || ds[0].Metric != 0 {
		t.Fatalf("defaults after return = %+v, want one via the own gateway", ds)
	}
}

// The via map is the event source: enter, move, come home, lose everything.
func TestViaChangesAreTheFailoverEvents(t *testing.T) {
	u, _, up := domesticRouteFixture(t)
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	var seen []string
	bus.OnPublish = func(eventType string) {
		mu.Lock()
		seen = append(seen, eventType)
		mu.Unlock()
	}
	u.EventBus = bus
	sib := domesticObs{Up: Uplink{IfName: "eth2", Table: 211, UplinkIndex: 6}, Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true}
	ctx := context.Background()
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaOwn})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaSibling, Sibling: sib})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaSibling, Sibling: sib})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaPool})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaOwn})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaPool})
	_ = u.applyDomesticRoute(ctx, domesticRoute{Up: up, Gateway: "192.0.2.1", Via: viaNone})
	want := []string{
		string(events.EventWANFailover),         // own -> sibling
		string(events.EventWANFailover),         // sibling -> pool (moved)
		string(events.EventWANFailoverRestored), // pool -> own
		string(events.EventWANFailover),         // own -> pool
		string(events.EventWANFailoverLost),     // pool -> none
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != len(want) {
		t.Fatalf("events %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("event %d = %s, want %s (all: %v)", i, seen[i], want[i], seen)
		}
	}
}

// A port un-assigned mid-failover would otherwise pin the banner on forever.
func TestTickForgetsAVanishedPortsVia(t *testing.T) {
	u, _, _ := domesticRouteFixture(t)
	u.recordVia("eth9", "pool", false)
	if !u.poolFailoverActive() {
		t.Fatal("the stale pool entry never armed")
	}
	u.probeOnce(context.Background())
	if got := u.viaOf("eth9"); got != "" {
		t.Fatalf("eth9 still riding %q after a tick", got)
	}
	if u.poolFailoverActive() {
		t.Fatal("the banner is still lit for a port that is gone")
	}
}

// Saving a per-line check list has to rewrite the firewall, or the new
// destination is dropped and the line reads dead forever.
func TestSetHealthConfigRearmsEachLegsProbeSet(t *testing.T) {
	rows := []domain.NetworkInterface{
		{ID: 1, IfName: "dish0", Key: "k-dish", Role: domain.RoleWAN, Slot: domain.SlotSecondary,
			Present: true, StaticGateway: "100.64.0.1"},
		{ID: 2, IfName: "lte0", Key: "k-lte", Role: domain.RoleWAN, Slot: domain.SlotSecondary2,
			Present: true, StaticGateway: "10.0.0.1"},
	}
	m := nft.NewManager(&nft.FakeApplier{})
	u := NewNetworkUsecase(Deps{
		RouterMode: true, Nft: m, IfRepo: &stubIfRepo{rows: rows},
	}).(*networkUsecase)

	u.SetHealthConfig(DefaultHealthConfig())
	before := probeIPsByIf(m.Snapshot().KillSwitch)
	if got := before["lte0"]; len(got) != 2 || got[0] != "1.1.1.1" {
		t.Fatalf("lte0 starts on the shared list, got %v", got)
	}

	// The operator gives lte0 its own check.
	cfg := ParseHealthConfig(func(k string) (string, error) {
		if k == ProbeTargetsSlotKey(domain.SlotSecondary2) {
			return `[{"address":"9.9.9.9:443","proto":"tcp","label":"Quad9"}]`, nil
		}
		return "", nil
	})
	u.SetHealthConfig(cfg)

	after := probeIPsByIf(m.Snapshot().KillSwitch)
	if got := after["lte0"]; len(got) != 1 || got[0] != "9.9.9.9" {
		t.Errorf("lte0 exemption was not re-armed, got %v", got)
	}
	if got := after["dish0"]; len(got) != 2 || got[0] != "1.1.1.1" {
		t.Errorf("dish0 must keep the shared list, got %v", got)
	}
}

func probeIPsByIf(k *nft.KillSwitch) map[string][]string {
	out := map[string][]string{}
	if k == nil {
		return out
	}
	for _, leg := range k.Legs {
		out[leg.IfName] = leg.ProbeIPs
	}
	return out
}
