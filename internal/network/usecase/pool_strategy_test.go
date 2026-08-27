package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// poolOf names each member after its slot, the way the kernel does.
func poolOf(specs ...poolMember) []poolMember {
	out := make([]poolMember, 0, len(specs))
	for _, s := range specs {
		if s.IfName == "" {
			s.IfName = system.WGLinkNameFor(s.Slot)
		}
		out = append(out, s)
	}
	return out
}

func TestParsePoolStrategy_RefusesAnythingElse(t *testing.T) {
	for _, s := range []string{"spread", "order", "fastest"} {
		if got, ok := ParsePoolStrategy(s); !ok || string(got) != s {
			t.Errorf("%q parsed as %q, ok=%v", s, got, ok)
		}
	}
	for _, s := range []string{"", "SPREAD", "weighted", "round-robin"} {
		if got, ok := ParsePoolStrategy(s); ok || got != DefaultPoolStrategy {
			t.Errorf("%q parsed as %q, ok=%v - want the default and a refusal", s, got, ok)
		}
	}
}

// The margin is what stops a blinking probe moving live traffic.
func TestBetterByMargin_NeedsBothAbsoluteAndRelative(t *testing.T) {
	cases := []struct {
		challenger, carrier int
		want                bool
	}{
		{90, 300, true},   // half the latency, way past both bars
		{285, 300, false}, // 15 ms: under the absolute bar
		{270, 300, false}, // 30 ms but only 10%: under the relative bar
		{2, 30, true},     // 28 ms and 93%
		{0, 300, false},   // nothing measured is not a win
		{100, 0, false},   // an unmeasured carrier is not a loss
	}
	for _, c := range cases {
		if got := betterByMargin(c.challenger, c.carrier); got != c.want {
			t.Errorf("betterByMargin(%d, %d) = %v, want %v",
				c.challenger, c.carrier, got, c.want)
		}
	}
}

func TestElectCarrier_ChainFailsOverAndBackByPosition(t *testing.T) {
	u := &networkUsecase{}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, Healthy: true},
		poolMember{Slot: 1, Priority: 1, Healthy: true},
	)
	// First election is the boot, not a move.
	if sw := u.electCarrier(members, StrategyOrder); sw != nil {
		t.Fatalf("boot announced a move: %+v", sw)
	}
	if u.poolCarrier != "nasnet-wg0" {
		t.Fatalf("carrier = %q, want the first in the chain", u.poolCarrier)
	}

	members[0].Healthy = false
	sw := u.electCarrier(members, StrategyOrder)
	if sw == nil || sw.To != "nasnet-wg1" || sw.Reason != "failover" {
		t.Fatalf("failover = %+v", sw)
	}

	members[0].Healthy = true
	sw = u.electCarrier(members, StrategyOrder)
	if sw == nil || sw.To != "nasnet-wg0" || sw.Reason != "failback" {
		t.Fatalf("failback = %+v, want the first to take it back", sw)
	}
	if again := u.electCarrier(members, StrategyOrder); again != nil {
		t.Fatalf("a settled chain announced %+v", again)
	}
}

func TestElectCarrier_FastestHoldsUntilAChallengerEarnsIt(t *testing.T) {
	u := &networkUsecase{}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, RTTms: 300, Healthy: true},
		poolMember{Slot: 1, Priority: 1, RTTms: 290, Healthy: true},
	)
	u.electCarrier(members, StrategyFastest) // boot: wg0 is not faster, wg1 wins on RTT
	if u.poolCarrier != "nasnet-wg1" {
		t.Fatalf("carrier = %q, want the quickest at boot", u.poolCarrier)
	}

	// wg0 improves, but only inside the margin: nothing moves, ever.
	members[0].RTTms = 275
	for i := 0; i < fastestHoldTicks+2; i++ {
		if sw := u.electCarrier(members, StrategyFastest); sw != nil {
			t.Fatalf("tick %d moved traffic on a %d ms win: %+v", i, 290-275, sw)
		}
	}

	// Now it is properly faster, but the hold still has to pass.
	members[0].RTTms = 90
	for i := 1; i < fastestHoldTicks; i++ {
		if sw := u.electCarrier(members, StrategyFastest); sw != nil {
			t.Fatalf("moved after %d ticks, want %d", i, fastestHoldTicks)
		}
	}
	sw := u.electCarrier(members, StrategyFastest)
	if sw == nil || sw.To != "nasnet-wg0" || sw.Reason != "faster" {
		t.Fatalf("switch = %+v, want wg0 on the third tick", sw)
	}
	if sw.FromRTT != 290 || sw.ToRTT != 90 {
		t.Errorf("switch carries %d -> %d ms, want the numbers it acted on", sw.FromRTT, sw.ToRTT)
	}
}

// A challenger that keeps changing never accumulates a hold.
func TestElectCarrier_FastestResetsWhenTheChallengerChanges(t *testing.T) {
	u := &networkUsecase{}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, RTTms: 300, Healthy: true},
		poolMember{Slot: 1, Priority: 1, RTTms: 50, Healthy: true},
		poolMember{Slot: 2, Priority: 2, RTTms: 60, Healthy: true},
	)
	u.poolCarrier = "nasnet-wg0"
	for i := 0; i < 6; i++ {
		// The two rivals trade places every tick.
		members[1].RTTms, members[2].RTTms = members[2].RTTms, members[1].RTTms
		if sw := u.electCarrier(members, StrategyFastest); sw != nil {
			t.Fatalf("tick %d moved to a challenger that never held: %+v", i, sw)
		}
	}
}

func TestElectCarrier_FastestTakesOverWhenTheCarrierDies(t *testing.T) {
	u := &networkUsecase{poolCarrier: "nasnet-wg0"}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, RTTms: 90, Healthy: false},
		poolMember{Slot: 1, Priority: 1, RTTms: 400, Healthy: true},
	)
	sw := u.electCarrier(members, StrategyFastest)
	if sw == nil || sw.To != "nasnet-wg1" || sw.Reason != "failover" {
		t.Fatalf("switch = %+v, want an immediate failover with no hold", sw)
	}
}

// Nothing measured yet is not evidence of a slow carrier.
func TestElectCarrier_FastestHoldsWithNoSamples(t *testing.T) {
	u := &networkUsecase{poolCarrier: "nasnet-wg0"}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, RTTms: 0, Healthy: true},
		poolMember{Slot: 1, Priority: 1, RTTms: 40, Healthy: true},
	)
	for i := 0; i < fastestHoldTicks+1; i++ {
		if sw := u.electCarrier(members, StrategyFastest); sw != nil {
			t.Fatalf("moved off an unmeasured carrier: %+v", sw)
		}
	}
}

func TestElectCarrier_SpreadHasNoCarrierToMove(t *testing.T) {
	u := &networkUsecase{poolCarrier: "nasnet-wg1", poolChallengeTicks: 2}
	members := poolOf(
		poolMember{Slot: 0, Priority: 0, RTTms: 90, Healthy: true},
		poolMember{Slot: 1, Priority: 1, RTTms: 400, Healthy: true},
	)
	if sw := u.electCarrier(members, StrategySpread); sw != nil {
		t.Fatalf("spread announced %+v", sw)
	}
	if u.poolCarrier != "" || u.poolChallengeTicks != 0 {
		t.Errorf("spread kept carrier %q and %d challenge ticks",
			u.poolCarrier, u.poolChallengeTicks)
	}
}

// ---------------------------------------------------------------- the writes

type fakePoolSettings struct {
	rows map[string]string
	err  error
}

func (f *fakePoolSettings) Get(_ context.Context, key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.rows[key], nil
}

func (f *fakePoolSettings) Set(_ context.Context, key, value string) error {
	if f.err != nil {
		return f.err
	}
	if f.rows == nil {
		f.rows = map[string]string{}
	}
	f.rows[key] = value
	return nil
}

func TestSetPoolStrategy_StoresAndAppliesAtOnce(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	store := &fakePoolSettings{}
	f.uc.PoolSettings = store
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Enabled: true, Priority: 1, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}

	if err := f.uc.SetPoolStrategy(ctx, "order"); err != nil {
		t.Fatal(err)
	}
	if store.rows[PoolStrategyKey] != "order" {
		t.Errorf("stored %q, want the choice to survive a reboot", store.rows[PoolStrategyKey])
	}
	nh := f.uc.currentPoolNexthops()
	if len(nh) != 1 || nh[0].OifName != "nasnet-wg0" {
		t.Fatalf("nexthops = %+v, want the chain applied on the spot", nh)
	}

	if err := f.uc.SetPoolStrategy(ctx, "spread"); err != nil {
		t.Fatal(err)
	}
	if nh := f.uc.currentPoolNexthops(); len(nh) != 2 {
		t.Fatalf("nexthops = %+v, want both back in", nh)
	}
}

func TestSetPoolStrategy_RefusesAnInventedOne(t *testing.T) {
	f := newVPNFixture(t)
	err := f.uc.SetPoolStrategy(context.Background(), "least-connections")
	if err == nil {
		t.Fatal("an invented strategy was accepted")
	}
}

func TestSetPoolOrder_WritesPositionsAndReroutes(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.healthCfg = DefaultHealthConfig()
	f.uc.healthCfg.PoolStrategy = StrategyOrder
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Enabled: true, Priority: 1, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}

	if err := f.uc.SetPoolOrder(ctx, []uint{2, 1}); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[1].Priority != 0 || f.repo.rows[0].Priority != 1 {
		t.Fatalf("positions = %d, %d; want the dragged order",
			f.repo.rows[0].Priority, f.repo.rows[1].Priority)
	}
	nh := f.uc.currentPoolNexthops()
	if len(nh) != 1 || nh[0].OifName != "nasnet-wg1" {
		t.Fatalf("nexthops = %+v, want the new first tunnel carrying", nh)
	}
}

func TestSetPoolOrder_RefusesAPartialOrHalfKnownOrder(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Enabled: true, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}
	for _, tc := range []struct {
		name string
		ids  []uint
	}{
		{"one of two", []uint{1}},
		{"a stranger", []uint{1, 99}},
		{"the same one twice", []uint{1, 1}},
	} {
		if err := f.uc.SetPoolOrder(ctx, tc.ids); err == nil {
			t.Errorf("%s was accepted as an order", tc.name)
		}
	}
}

// An upgraded box has tiers and no strategy. Tiers meant "these carry, that one
// waits", and the chain is the only thing that still says it.
func TestMigratePoolStrategy_ReadsTheOldTiers(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		rows  []domain.VPNProfile
		want  PoolStrategy
		order []uint
	}{
		{"one flat tier stays a spread",
			[]domain.VPNProfile{
				{ID: 1, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0)},
				{ID: 2, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(1)},
			}, StrategySpread, []uint{1, 2}},
		{"tiers become a chain, heaviest first inside a tier",
			[]domain.VPNProfile{
				{ID: 1, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0)},
				{ID: 2, Enabled: true, Priority: 1, Weight: 1, WGSlot: slotOf(1)},
				{ID: 3, Enabled: true, Priority: 0, Weight: 5, WGSlot: slotOf(2)},
			}, StrategyOrder, []uint{3, 1, 2}},
		{"an empty pool is a spread", nil, StrategySpread, nil},
	}
	for _, tc := range cases {
		f := newVPNFixture(t)
		store := &fakePoolSettings{}
		f.uc.PoolSettings = store
		f.repo.rows = tc.rows

		if err := f.uc.MigratePoolStrategy(ctx); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := store.rows[PoolStrategyKey]; got != string(tc.want) {
			t.Errorf("%s: stored %q, want %q", tc.name, got, tc.want)
		}
		if f.uc.poolStrategyNow() != tc.want {
			t.Errorf("%s: in memory %q, want %q", tc.name, f.uc.poolStrategyNow(), tc.want)
		}
		for pos, id := range tc.order {
			for i := range f.repo.rows {
				if f.repo.rows[i].ID == id && f.repo.rows[i].Priority != pos {
					t.Errorf("%s: profile %d at position %d, want %d",
						tc.name, id, f.repo.rows[i].Priority, pos)
				}
			}
		}
	}
}

func TestMigratePoolStrategy_LeavesAChosenStrategyAlone(t *testing.T) {
	f := newVPNFixture(t)
	store := &fakePoolSettings{rows: map[string]string{PoolStrategyKey: "fastest"}}
	f.uc.PoolSettings = store
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0)},
		{ID: 2, Enabled: true, Priority: 3, Weight: 1, WGSlot: slotOf(1)},
	}
	if err := f.uc.MigratePoolStrategy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.rows[PoolStrategyKey] != "fastest" {
		t.Errorf("migration overwrote a chosen strategy with %q", store.rows[PoolStrategyKey])
	}
	if f.repo.rows[1].Priority != 3 {
		t.Errorf("migration renumbered a pool it should not have touched")
	}
}

func TestAppendToChain_PutsANewTunnelLast(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0)},
		{ID: 2, Enabled: true, Priority: 1, Weight: 1, WGSlot: slotOf(1)},
		{ID: 3, Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(2)},
	}
	if err := f.uc.appendToChain(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[2].Priority != 2 {
		t.Fatalf("the new tunnel landed at position %d, want last", f.repo.rows[2].Priority)
	}
}

// The move is the one automatic act an operator notices, so it has to reach
// the feed with the numbers behind it.
func TestAnnounceCarrier_NamesBothTunnelsAndTheReason(t *testing.T) {
	f := newVPNFixture(t)
	bus := events.NewEventBus()
	defer bus.Close()
	rec := events.NewRecorder(bus, "carrier-test", 10, events.IsNetworkEvent)
	f.uc.EventBus, f.uc.Events = bus, rec
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "Oslo", Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "Berlin", Enabled: true, Priority: 1, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}
	pool := f.uc.vpnPoolNow(context.Background())
	f.uc.announceCarrier(&carrierSwitch{
		From: "nasnet-wg1", To: "nasnet-wg0", Reason: "faster", FromRTT: 160, ToRTT: 95,
	}, pool)

	var got []events.Event
	for range 200 {
		if got = rec.Recent(); len(got) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(got) != 1 || got[0].Type != events.EventVPNCarrierSwitched {
		t.Fatalf("recorded %+v", got)
	}
	payload, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %T", got[0].Payload)
	}
	if payload["from"] != "Berlin" || payload["to"] != "Oslo" {
		t.Errorf("payload names %v -> %v, want the tunnels' own names",
			payload["from"], payload["to"])
	}
	if payload["reason"] != "faster" || payload["to_rtt_ms"] != 95 {
		t.Errorf("payload = %v, want the reason and the numbers it acted on", payload)
	}
}
