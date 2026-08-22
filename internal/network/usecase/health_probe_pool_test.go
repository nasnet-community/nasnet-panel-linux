package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// poolProber answers per interface, which is the whole point of the pool.
type poolProber struct {
	mu sync.Mutex
	up map[string]bool
}

func (f *poolProber) set(ifName string, up bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.up == nil {
		f.up = map[string]bool{}
	}
	f.up[ifName] = up
}

func (f *poolProber) ProbeTarget(_ context.Context, ifName string, _ uint32, t ProbeTarget) ProbeResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ProbeResult{Target: t, OK: f.up[ifName], RTT: 10 * time.Millisecond}
}

type poolProbeFixture struct {
	uc     *networkUsecase
	prober *poolProber
	be     *system.FakeBackend
	bus    *events.EventBus
	mu     sync.Mutex
	seen   []string
}

func newPoolProbeFixture(t *testing.T, members int) *poolProbeFixture {
	t.Helper()
	f := &poolProbeFixture{
		prober: &poolProber{},
		be:     system.NewFakeBackend(),
		bus:    events.NewEventBus(),
	}
	t.Cleanup(f.bus.Close)
	f.bus.OnPublish = func(eventType string) {
		f.mu.Lock()
		f.seen = append(f.seen, eventType)
		f.mu.Unlock()
	}

	repo := &fakeVPNRepo{}
	weights := []int{3, 1, 1}
	for i := 0; i < members; i++ {
		slot := i
		addr := "10.66.0.2/32"
		if i > 0 {
			addr = "10.66.1.2/32"
		}
		repo.rows = append(repo.rows, domain.VPNProfile{
			ID: uint(i + 1), Name: string(rune('a' + i)), Enabled: true,
			Priority: 0, Weight: weights[i], WGSlot: &slot,
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
				c.Address = addr
				c.PinnedEndpointIP = "185.65.135.1"
			}),
		})
	}
	f.uc = &networkUsecase{Deps: Deps{
		VPNRepo:  repo,
		WG:       &system.FakeWGDevice{},
		Backend:  f.be,
		IfRepo:   &stubIfRepo{},
		EventBus: f.bus,
		Prober:   f.prober,
	}}
	return f
}

func (f *poolProbeFixture) tickPool(ctx context.Context) {
	f.uc.probePool(ctx, DefaultHealthConfig())
}

func (f *poolProbeFixture) poolDefault(t *testing.T) []system.Nexthop {
	t.Helper()
	routes, err := f.be.RouteList(context.Background(), system.WGTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if r.Dest == "default" && r.Metric == 0 {
			return r.Nexthops
		}
	}
	return nil
}

func (f *poolProbeFixture) sawEvent(want events.EventType) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.seen {
		if s == string(want) {
			return true
		}
	}
	return false
}

// pastTheDwell backdates a member's outage so the damper's dwell is over.
func (f *poolProbeFixture) pastTheDwell(ifName string) {
	f.uc.healthMu.Lock()
	defer f.uc.healthMu.Unlock()
	if s, ok := f.uc.inetStates[ifName]; ok {
		s.mu.Lock()
		s.lastDownAt = time.Now().Add(-3 * time.Minute)
		s.mu.Unlock()
	}
}

func TestProbePool_DeadMemberLeavesTheNexthopsAndComesBack(t *testing.T) {
	f := newPoolProbeFixture(t, 2)
	ctx := context.Background()

	f.prober.set("nasnet-wg0", true)
	f.prober.set("nasnet-wg1", true)
	f.tickPool(ctx)
	if nh := f.poolDefault(t); len(nh) != 2 {
		t.Fatalf("nexthops = %v", nh)
	}

	f.prober.set("nasnet-wg1", false)
	for i := 0; i < defaultInternetLimits().FailsToDown; i++ {
		f.tickPool(ctx)
	}
	if nh := f.poolDefault(t); len(nh) != 1 || nh[0].OifName != "nasnet-wg0" {
		t.Fatalf("dead member still routed: %v", nh)
	}
	if !f.sawEvent(events.EventVPNPoolChanged) {
		t.Fatal("no vpn.pool_changed emitted")
	}

	f.prober.set("nasnet-wg1", true)
	f.pastTheDwell("nasnet-wg1")
	for i := 0; i < defaultInternetLimits().SuccsToUp; i++ {
		f.tickPool(ctx)
	}
	if nh := f.poolDefault(t); len(nh) != 2 {
		t.Fatalf("recovered member never rejoined: %v", nh)
	}
}

func TestProbePool_LastMemberIsNeverEjected(t *testing.T) {
	f := newPoolProbeFixture(t, 1)
	ctx := context.Background()
	f.prober.set("nasnet-wg0", false)
	for i := 0; i < 10; i++ {
		f.tickPool(ctx)
	}
	if nh := f.poolDefault(t); len(nh) != 1 {
		t.Fatalf("last member ejected: %v", nh)
	}
	// The ladder still tells the truth even though the route stays.
	f.uc.healthMu.Lock()
	verdict := f.uc.ladders["nasnet-wg0"].Verdict
	f.uc.healthMu.Unlock()
	if verdict != "no-internet" {
		t.Errorf("verdict = %q, want no-internet", verdict)
	}
}

// A pool reshuffle has to reach the failover mirror in the domestic table too.
func TestApplyPoolRoutes_MirrorsIntoTheDomesticTableDuringFailover(t *testing.T) {
	f := newPoolProbeFixture(t, 2)
	ctx := context.Background()
	f.uc.IfRepo = &stubIfRepo{rows: []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", Key: "eth0", Role: domain.RoleWAN, Slot: domain.SlotDomestic, Present: true},
	}}
	f.uc.failoverActive = true

	f.prober.set("nasnet-wg0", true)
	f.prober.set("nasnet-wg1", false)
	for i := 0; i < defaultInternetLimits().FailsToDown; i++ {
		f.tickPool(ctx)
	}

	routes, _ := f.be.RouteList(ctx, 201)
	for _, r := range routes {
		if r.Dest == "default" && r.Metric == 0 {
			if len(r.Nexthops) != 1 || r.Nexthops[0].OifName != "nasnet-wg0" {
				t.Fatalf("mirror = %+v, want the surviving member only", r.Nexthops)
			}
			return
		}
	}
	t.Fatal("no failover mirror written into table 201")
}

// Disabled members' readings die with them, or the cards freeze.
func TestProbePool_DropsReadingsForMembersThatLeft(t *testing.T) {
	f := newPoolProbeFixture(t, 2)
	ctx := context.Background()
	f.prober.set("nasnet-wg0", true)
	f.prober.set("nasnet-wg1", true)
	f.tickPool(ctx)

	repo := f.uc.VPNRepo.(*fakeVPNRepo)
	if err := repo.SetEnabled(ctx, 2, false); err != nil {
		t.Fatal(err)
	}
	f.tickPool(ctx)

	f.uc.healthMu.Lock()
	_, hasLadder := f.uc.ladders["nasnet-wg1"]
	_, hasState := f.uc.inetStates["nasnet-wg1"]
	f.uc.healthMu.Unlock()
	if hasLadder || hasState {
		t.Error("a disabled member's readings survived")
	}
}
