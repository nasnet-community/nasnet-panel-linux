package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func dom1() Uplink {
	return Uplink{IfName: "adsl0", Table: 201, UplinkIndex: 1, Slot: domain.SlotDomestic, GroupIndex: 1}
}

func TestTick_ForcedDownPrimarySurvivesRestartAndCanRecover(t *testing.T) {
	for _, c := range []struct {
		name   string
		metric int
	}{{"managed default", 0}, {"DHCP default", probeRouteMetric}} {
		t.Run(c.name, func(t *testing.T) {
			u, _ := twoDomesticFixture(t)
			u.healthCfg = DefaultHealthConfig()
			u.healthCfg.FailoverToVPN = false
			repo := u.IfRepo.(*flowIfRepo)
			repo.rows[0].ForceState = "down"
			be := u.Backend.(*system.FakeBackend)
			for i := range be.Routes {
				if be.Routes[i].Table == 201 {
					be.Routes[i].Metric = c.metric
				}
			}

			tick(u, 30)
			if u.health.EverUp("eth0") {
				t.Fatal("forced-down primary unexpectedly became healthy")
			}
			if ds := defaultsIn(t, be, 201); len(ds) != 0 {
				t.Fatalf("disabled primary kept defaults: %+v", ds)
			}
			ups, _ := u.uplinks(t.Context())
			_, route, _ := walkPolicy(AllRules(flowGroups(), ups, VPNRouteState{}), routesByTable(be.Routes),
				"203.0.113.10", netmark.GroupMark(netmark.GroupDomestic))
			if route == nil || route.OifName != "eth2" {
				t.Fatalf("healthy backup did not carry domestic traffic: %+v", route)
			}

			// Removing the override still allows gateway damping and normal failback.
			repo.rows[0].ForceState = ""
			now := time.Now().Add(DefaultDamping().FailbackDwell + time.Second)
			u.health.Now = func() time.Time { return now }
			tick(u, 7)
			ds := defaultsIn(t, be, 201)
			if len(ds) != 1 || ds[0].OifName != "eth0" {
				t.Fatalf("primary failed to recover: %+v", ds)
			}
		})
	}
}

func TestForcedDownDomesticCanWithdrawWithoutAKnownGateway(t *testing.T) {
	u, _ := twoDomesticFixture(t)
	be := u.Backend.(*system.FakeBackend)
	be.Routes = []system.Route{
		{Table: 201, Dest: "default", OifName: "eth2"},
		{Table: 201, Dest: "default", OifName: "eth0", Metric: probeRouteMetric},
	}
	err := u.applyDomesticRoute(t.Context(), domesticRoute{Up: Uplink{IfName: "eth0", Table: 201}, Via: viaNone, ForcedDown: true})
	if err != nil {
		t.Fatal(err)
	}
	if ds := defaultsIn(t, be, 201); len(ds) != 0 {
		t.Fatalf("stale defaults survived force-down: %+v", ds)
	}
}

func TestForcedDownDomesticCanUsePoolBeforeEverUp(t *testing.T) {
	u, _ := twoDomesticFixture(t)
	repo := u.IfRepo.(*flowIfRepo)
	repo.rows[0].ForceState, repo.rows[1].ForceState = "down", "down"
	tick(u, 1)
	if u.viaOf("eth0") != "pool" || u.viaOf("eth2") != "pool" {
		t.Fatalf("forced-down domestic lines did not use available pool: %v", u.viaByIf)
	}
}

func dom2() Uplink {
	return Uplink{IfName: "fiber0", Table: 211, UplinkIndex: 6, Slot: domain.SlotDomestic2, GroupIndex: 1}
}

func planFor(t *testing.T, obs []domesticObs, failoverOn, vpnUp bool) map[string]domesticRoute {
	t.Helper()
	out := map[string]domesticRoute{}
	for _, r := range domesticRoutePlan(obs, failoverOn, vpnUp) {
		out[r.Up.IfName] = r
	}
	return out
}

func TestDomesticPlan_HealthyLinesKeepTheirOwnDefault(t *testing.T) {
	p := planFor(t, []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: true},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true},
	}, true, true)
	for _, name := range []string{"adsl0", "fiber0"} {
		if p[name].Via != viaOwn {
			t.Errorf("%s via %v, want its own gateway", name, p[name].Via)
		}
	}
}

// Internet dead, gateway alive: the table follows the sibling, the probe keeps
// its own path underneath.
func TestDomesticPlan_DeadInternetRidesTheFirstHealthySibling(t *testing.T) {
	p := planFor(t, []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: false},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true},
	}, true, true)
	r := p["adsl0"]
	if r.Via != viaSibling || r.Sibling.Up.IfName != "fiber0" || r.Sibling.Gateway != "198.51.100.1" {
		t.Fatalf("adsl0 = %+v, want a mirror of fiber0", r)
	}
	if r.Gateway != "192.0.2.1" {
		t.Fatalf("adsl0 lost its own gateway: %+v", r)
	}
	if p["fiber0"].Via != viaOwn {
		t.Fatalf("the healthy sibling must keep its own default: %+v", p["fiber0"])
	}
}

// A lower-numbered line is a sibling too: priority picks who carries when
// several are healthy, not who may be carried.
func TestDomesticPlan_BackupLineRidesThePrimaryWhenItDies(t *testing.T) {
	p := planFor(t, []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: true},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: true, InternetUp: false},
	}, true, true)
	if r := p["fiber0"]; r.Via != viaSibling || r.Sibling.Up.IfName != "adsl0" {
		t.Fatalf("fiber0 = %+v, want a mirror of adsl0", r)
	}
}

// No gateway means no probe path to protect, so the table empties and the
// group rule walks on to the sibling by itself.
func TestDomesticPlan_DeadGatewayWithdrawsWhenASiblingIsHealthy(t *testing.T) {
	p := planFor(t, []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: false, InternetUp: false},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true},
	}, true, true)
	if p["adsl0"].Via != viaNone {
		t.Fatalf("adsl0 = %+v, want withdrawn", p["adsl0"])
	}
}

func TestDomesticPlan_EveryLineDeadRidesThePoolWhenAllowed(t *testing.T) {
	obs := []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: false},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: false, InternetUp: false},
	}
	p := planFor(t, obs, true, true)
	for _, name := range []string{"adsl0", "fiber0"} {
		if p[name].Via != viaPool {
			t.Errorf("%s via %v, want the pool", name, p[name].Via)
		}
	}
	// Toggle off, or no tunnel up: a live gateway keeps its route so the probe
	// can see the recovery; a dead one withdraws.
	for _, c := range []struct{ on, vpn bool }{{false, true}, {true, false}} {
		p := planFor(t, obs, c.on, c.vpn)
		if p["adsl0"].Via != viaOwn {
			t.Errorf("on=%v vpn=%v: adsl0 via %v, want its own route kept", c.on, c.vpn, p["adsl0"].Via)
		}
		if p["fiber0"].Via != viaNone {
			t.Errorf("on=%v vpn=%v: fiber0 via %v, want withdrawn", c.on, c.vpn, p["fiber0"].Via)
		}
	}
}

// A sibling whose gateway was never learned cannot be mirrored: there is no
// nexthop to write.
func TestDomesticPlan_UnknownGatewayIsNeverASibling(t *testing.T) {
	p := planFor(t, []domesticObs{
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: false},
		{Up: dom2(), Gateway: "", GatewayUp: true, InternetUp: true},
	}, true, true)
	if p["adsl0"].Via != viaPool {
		t.Fatalf("adsl0 = %+v, want the pool since the sibling has no gateway", p["adsl0"])
	}
}

// The single-domestic box must behave exactly as before this feature.
func TestDomesticPlan_SingleLineMatchesTheOldPolicy(t *testing.T) {
	cases := []struct {
		name            string
		gwUp, inetUp    bool
		failoverOn, vpn bool
		want            domesticVia
	}{
		{"healthy", true, true, true, true, viaOwn},
		{"internet dead, pool allowed", true, false, true, true, viaPool},
		{"gateway dead, pool allowed", false, false, true, true, viaPool},
		{"internet dead, toggle off", true, false, false, true, viaOwn},
		{"internet dead, no tunnel", true, false, true, false, viaOwn},
		{"gateway dead, no tunnel", false, false, true, false, viaNone},
	}
	for _, c := range cases {
		p := planFor(t, []domesticObs{{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: c.gwUp, InternetUp: c.inetUp}}, c.failoverOn, c.vpn)
		if p["adsl0"].Via != c.want {
			t.Errorf("%s: via %v, want %v", c.name, p["adsl0"].Via, c.want)
		}
	}
}

func TestDomesticPlan_OrderIsBySlotNotByInput(t *testing.T) {
	three := Uplink{IfName: "lte0", Table: 212, UplinkIndex: 7, Slot: domain.SlotDomestic3, GroupIndex: 1}
	p := domesticRoutePlan([]domesticObs{
		{Up: three, Gateway: "203.0.113.1", GatewayUp: true, InternetUp: true},
		{Up: dom2(), Gateway: "198.51.100.1", GatewayUp: true, InternetUp: true},
		{Up: dom1(), Gateway: "192.0.2.1", GatewayUp: true, InternetUp: false},
	}, true, true)
	if p[0].Up.IfName != "adsl0" || p[1].Up.IfName != "fiber0" || p[2].Up.IfName != "lte0" {
		t.Fatalf("plan order %s %s %s, want slot order", p[0].Up.IfName, p[1].Up.IfName, p[2].Up.IfName)
	}
	if p[0].Via != viaSibling || p[0].Sibling.Up.IfName != "fiber0" {
		t.Fatalf("the first healthy sibling in slot order is fiber0, got %+v", p[0])
	}
}

// Two domestic lines and a tunnel, driven through the real tick.
func twoDomesticFixture(t *testing.T) (*networkUsecase, *fakeProber) {
	t.Helper()
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	rows := []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", Key: "eth0", Role: domain.RoleWAN, Slot: domain.SlotDomestic,
			Present: true, Healthy: true, LearnedGateway: "192.0.2.1"},
		{ID: 2, IfName: "eth2", Key: "eth2", Role: domain.RoleWAN, Slot: domain.SlotDomestic2,
			Present: true, Healthy: true, LearnedGateway: "198.51.100.1"},
		{ID: 3, IfName: "eth1", Key: "eth1", Role: domain.RoleWAN, Slot: domain.SlotSecondary,
			Present: true, Healthy: true, LearnedGateway: "100.64.0.1"},
	}
	u.IfRepo = &flowIfRepo{stubIfRepo{rows: rows}}
	be := u.Backend.(*system.FakeBackend)
	be.Routes = append(be.Routes, system.Route{Table: 211, Dest: "default", Gateway: "198.51.100.1", OifName: "eth2"})
	return u, u.Prober.(*fakeProber)
}

func tick(u *networkUsecase, n int) {
	for i := 0; i < n; i++ {
		u.probeOnce(context.Background())
	}
}

func TestTick_DeadDomesticInternetRidesTheSiblingThenThePool(t *testing.T) {
	u, prober := twoDomesticFixture(t)
	be := u.Backend.(*system.FakeBackend)
	ctx := context.Background()
	// Gateway damper needs six clean ticks before the internet layer counts.
	tick(u, 7)
	for _, name := range []string{"eth0", "eth2"} {
		if u.viaOf(name) != "" {
			t.Fatalf("%s riding %q while healthy", name, u.viaOf(name))
		}
	}

	prober.setDown("eth0", true)
	tick(u, 6) // five fails trip the internet damper
	if u.viaOf("eth0") != "eth2" {
		t.Fatalf("eth0 via %q, want eth2", u.viaOf("eth0"))
	}
	if u.poolFailoverActive() {
		t.Fatal("a sibling ride is not a pool failover")
	}
	var mirror, probe bool
	for _, r := range defaultsIn(t, be, 201) {
		if r.Metric == 0 && r.OifName == "eth2" && r.Gateway == "198.51.100.1" {
			mirror = true
		}
		if r.Metric == probeRouteMetric && r.OifName == "eth0" && r.Gateway == "192.0.2.1" {
			probe = true
		}
	}
	if !mirror || !probe {
		t.Fatalf("table 201 = %+v", defaultsIn(t, be, 201))
	}
	if ds := defaultsIn(t, be, 211); len(ds) != 1 || ds[0].OifName != "eth2" {
		t.Fatalf("the healthy sibling's table changed: %+v", ds)
	}

	prober.setDown("eth2", true)
	tick(u, 6)
	for _, name := range []string{"eth0", "eth2"} {
		if u.viaOf(name) != "pool" {
			t.Fatalf("%s via %q, want pool once every line is dead", name, u.viaOf(name))
		}
	}
	if !u.poolFailoverActive() {
		t.Fatal("pool failover not reported")
	}
	for _, table := range []int{201, 211} {
		var pooled bool
		for _, r := range defaultsIn(t, be, table) {
			for _, nh := range r.Nexthops {
				if nh.OifName == system.WGLinkName {
					pooled = true
				}
			}
		}
		if !pooled {
			t.Fatalf("table %d does not mirror the pool: %+v", table, defaultsIn(t, be, table))
		}
	}
	view, err := u.HealthState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !view.FailoverActive {
		t.Fatal("health view must report the pool failover")
	}
}

// Names run backwards against the slots and the rows are listed out of slot
// order, so neither a sort on if_name nor no sort at all can pass this.
func TestIngressUplinkIfNames_EveryDomesticInSlotOrder(t *testing.T) {
	u, _ := twoDomesticFixture(t)
	u.IfRepo = &flowIfRepo{stubIfRepo{rows: []domain.NetworkInterface{
		{ID: 2, IfName: "eth1", Key: "eth1", Role: domain.RoleWAN, Slot: domain.SlotDomestic2, Present: true},
		{ID: 3, IfName: "eth5", Key: "eth5", Role: domain.RoleWAN, Slot: domain.SlotSecondary, Present: true},
		{ID: 1, IfName: "eth9", Key: "eth9", Role: domain.RoleWAN, Slot: domain.SlotDomestic, Present: true},
	}}}
	u.RouterMode = true
	got := u.ingressUplinkIfNames(context.Background())
	if len(got) != 2 || got[0] != "eth9" || got[1] != "eth1" {
		t.Fatalf("ingress uplinks = %v, want [eth9 eth1] in slot order", got)
	}
	if u.IngressUplinkIfName() != "eth9" {
		t.Fatalf("the single-name consumer must get the first domestic, got %q", u.IngressUplinkIfName())
	}
	u.RouterMode = false
	if u.IngressUplinkIfName() != "" || len(u.ingressUplinkIfNames(context.Background())) != 0 {
		t.Fatal("no ingress uplink outside router mode")
	}
}
