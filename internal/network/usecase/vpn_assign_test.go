package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func assignFixture(nTunnels int, pins map[int]string) (vpnPool, []Uplink) {
	var pool vpnPool
	for i := 0; i < nTunnels; i++ {
		slot := i
		p := &domain.VPNProfile{ID: uint(i + 1), Name: string(rune('a' + i)), WGSlot: &slot}
		if key, ok := pins[i]; ok {
			p.TransportUplink = key
		}
		pool.Tunnels = append(pool.Tunnels, tunnel{
			Profile: p, Config: &domain.WireGuardConfig{}, IfName: system.WGLinkNameFor(slot),
		})
	}
	ups := []Uplink{
		{IfName: "dish0", Key: "k-dish0", Slot: domain.SlotSecondary, UplinkIndex: 2, Table: 202},
		{IfName: "lte0", Key: "k-lte0", Slot: domain.SlotSecondary2, UplinkIndex: 3, Table: 204},
	}
	return pool, ups
}

func TestAssignDealsRoundRobinInSlotOrder(t *testing.T) {
	pool, ups := assignFixture(4, nil)
	healthy := map[string]bool{"dish0": true, "lte0": true}
	got := assignTransport(pool, ups, healthy)
	want := map[string]string{
		"nasnet-wg0": "dish0", "nasnet-wg1": "lte0",
		"nasnet-wg2": "dish0", "nasnet-wg3": "lte0",
	}
	for ifName, wan := range want {
		if got[ifName].IfName != wan {
			t.Fatalf("%s rides %q, want %q", ifName, got[ifName].IfName, wan)
		}
	}
}

func TestAssignIsDeterministic(t *testing.T) {
	pool, ups := assignFixture(5, nil)
	healthy := map[string]bool{"dish0": true, "lte0": true}
	a := assignTransport(pool, ups, healthy)
	for i := 0; i < 20; i++ {
		b := assignTransport(pool, ups, healthy)
		for k := range a {
			if a[k].IfName != b[k].IfName {
				t.Fatalf("run %d moved %s from %s to %s", i, k, a[k].IfName, b[k].IfName)
			}
		}
	}
}

func TestAssignHonoursThePinEvenOnADeadWAN(t *testing.T) {
	pool, ups := assignFixture(2, map[int]string{0: "k-dish0"})
	healthy := map[string]bool{"dish0": false, "lte0": true}
	got := assignTransport(pool, ups, healthy)
	if got["nasnet-wg0"].IfName != "dish0" {
		t.Fatal("a pin that silently moves is not a pin")
	}
	if got["nasnet-wg1"].IfName != "lte0" {
		t.Fatal("the auto tunnel must avoid the dead WAN")
	}
}

func TestAssignRehomesAutoTunnelsOffADeadWAN(t *testing.T) {
	pool, ups := assignFixture(4, nil)
	healthy := map[string]bool{"dish0": false, "lte0": true}
	got := assignTransport(pool, ups, healthy)
	for ifName, wan := range got {
		if wan.IfName != "lte0" {
			t.Fatalf("%s still rides the dead %s", ifName, wan.IfName)
		}
	}
}

func TestAssignPinToAMissingRowFallsBackToAuto(t *testing.T) {
	pool, ups := assignFixture(1, map[int]string{0: "k-gone"})
	healthy := map[string]bool{"dish0": true, "lte0": true}
	if got := assignTransport(pool, ups, healthy); got["nasnet-wg0"].IfName == "" {
		t.Fatal("a stale pin must not strand the tunnel")
	}
}

// Boot before the first probe, or a total outage: marks must still exist so
// recovery is observable the moment a WAN answers.
func TestAssignAllWANsDeadStillAssigns(t *testing.T) {
	pool, ups := assignFixture(2, nil)
	got := assignTransport(pool, ups, map[string]bool{"dish0": false, "lte0": false})
	if got["nasnet-wg0"].IfName == "" || got["nasnet-wg1"].IfName == "" {
		t.Fatal("an unassigned tunnel has no mark and can never come back")
	}
}

// seedSecondaries gives the fixture N secondary uplinks in slot order.
func seedSecondaries(t *testing.T, f *vpnFixture, ifNames ...string) {
	t.Helper()
	slots := domain.SecondarySlots()
	if len(ifNames) > len(slots) {
		t.Fatalf("%d secondaries asked for, %d exist", len(ifNames), len(slots))
	}
	rows := []domain.NetworkInterface{
		{ID: 1, Key: "k-eth0", IfName: "eth0", Role: domain.RoleWAN,
			Slot: domain.SlotDomestic, Present: true},
	}
	for i, name := range ifNames {
		rows = append(rows, domain.NetworkInterface{
			ID: uint(i + 2), Key: "k-" + name, IfName: name, Role: domain.RoleWAN,
			Slot: slots[i], Present: true, LearnedGateway: "10.0.0.1",
		})
	}
	f.uc.IfRepo = &stubIfRepo{rows: rows}
}

// seedEnabledTunnels puts n enabled profiles in slots 0..n-1.
func seedEnabledTunnels(t *testing.T, f *vpnFixture, n int) {
	t.Helper()
	f.repo.rows = nil
	for i := 0; i < n; i++ {
		slot := i
		f.repo.rows = append(f.repo.rows, domain.VPNProfile{
			ID: uint(i + 1), Name: fmt.Sprintf("tunnel-%c", 'a'+i),
			Type: domain.VPNTypeWireGuard, Enabled: true, WGSlot: &slot,
			Priority: 0, Weight: 1,
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
				c.Address = fmt.Sprintf("10.66.%d.2/32", i)
				c.PinnedEndpointIP = "185.65.135.1"
			}),
		})
	}
}

func TestEachTunnelCarriesItsOwnWANsMark(t *testing.T) {
	f := newVPNFixture(t)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 3)

	if err := f.uc.applyVPNDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	marks := map[string]uint32{
		"nasnet-wg0": netmark.PinMark(2), // dish0, dealt first
		"nasnet-wg1": netmark.PinMark(3), // lte0
		"nasnet-wg2": netmark.PinMark(2), // wraps
	}
	for ifName, want := range marks {
		st := f.wg.State(ifName)
		if st == nil || st.Applied == nil {
			t.Fatalf("%s was never brought up", ifName)
		}
		if got := st.Applied.FirewallMark; got != want {
			t.Fatalf("%s mark %#x, want %#x", ifName, got, want)
		}
	}
}

func TestKillSwitchStateBuildsALegPerSecondary(t *testing.T) {
	ups := []Uplink{
		{IfName: "eth0", Key: "k0", Slot: domain.SlotDomestic, UplinkIndex: 1},
		{IfName: "dish0", Key: "k1", Slot: domain.SlotSecondary, UplinkIndex: 2},
		{IfName: "lte0", Key: "k2", Slot: domain.SlotSecondary2, UplinkIndex: 3},
	}
	rows := []domain.NetworkInterface{
		{Key: "k1", LearnedGateway: "100.64.0.1"},
		{Key: "k2", StaticGateway: "10.0.0.1"},
	}
	gws := secondaryGateways(ups, rows)
	if gws["dish0"] != "100.64.0.1" || gws["lte0"] != "10.0.0.1" {
		t.Fatalf("gateways = %v", gws)
	}
	legs := killSwitchLegs(ups, gws)
	if len(legs) != 2 {
		t.Fatalf("%d legs, want 2 - domestic must not get one", len(legs))
	}
	if legs[0].PinValue != netmark.PinMark(2) || legs[1].PinValue != netmark.PinMark(3) {
		t.Fatal("each leg must carry its own wan's pin")
	}
}

// withBus wires an event recorder onto the fixture.
func withBus(t *testing.T, f *vpnFixture) *[]string {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	var seen []string
	bus.OnPublish = func(eventType string) {
		mu.Lock()
		seen = append(seen, eventType)
		mu.Unlock()
	}
	f.uc.EventBus = bus
	return &seen
}

// forceInetDown drives a damper past its fail limit.
func forceInetDown(t *testing.T, f *vpnFixture, ifName string) {
	t.Helper()
	lim := defaultInternetLimits()
	st := f.uc.inetState(ifName)
	for i := 0; i <= lim.FailsToDown; i++ {
		st.observe(false, lim, time.Now())
	}
	if down, _ := st.snapshot(); !down {
		t.Fatalf("%s damper never tripped", ifName)
	}
}

func TestRehomerMovesAutoTunnelsWhenAWANDies(t *testing.T) {
	f := newVPNFixture(t)
	seen := withBus(t, f)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 2)
	ctx := context.Background()
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}

	forceInetDown(t, f, "dish0")
	if err := f.uc.applyTransportAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.wg.State("nasnet-wg0").Applied.FirewallMark; got != netmark.PinMark(3) {
		t.Fatalf("wg0 still marked %#x, want lte0's pin", got)
	}
	found := false
	for _, e := range *seen {
		if e == string(events.EventVPNTunnelRehomed) {
			found = true
		}
	}
	if !found {
		t.Fatalf("a silent re-home is invisible; events were %v", *seen)
	}
}

func TestRehomerIsQuietWhenNothingChanged(t *testing.T) {
	f := newVPNFixture(t)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 2)
	ctx := context.Background()
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	n := f.wg.EnsureCalls()
	if err := f.uc.applyTransportAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	if f.wg.EnsureCalls() != n {
		t.Fatal("re-ensuring an unchanged tunnel churns handshakes for nothing")
	}
}

func TestVPNStatusJSONCarriesUplinksAndVia(t *testing.T) {
	f := newVPNFixture(t)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 2)
	f.repo.rows[1].TransportUplink = "k-lte0" // pin the second tunnel

	st, err := f.uc.VPNStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	if strings.Contains(s, "secondary_uplink_up") {
		t.Fatal("the single bool must be gone, not deprecated")
	}
	if !strings.Contains(s, `"uplinks":[`) {
		t.Fatalf("status must list the WANs: %s", s)
	}
	var via *TunnelVia
	for _, tv := range st.Tunnels {
		if tv.IfName == "nasnet-wg1" {
			via = tv.Via
		}
	}
	if via == nil || !via.Pinned || via.IfName != "lte0" {
		t.Fatalf("wg1 via = %+v, want pinned lte0", via)
	}
	if len(st.Uplinks) != 2 {
		t.Fatalf("%d uplinks listed, want 2", len(st.Uplinks))
	}
}

func TestSetTransportRejectsANonSecondaryKey(t *testing.T) {
	f := newVPNFixture(t)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 1)
	err := f.uc.SetVPNProfileTransport(context.Background(), 1, "k-eth0")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("a pin to the domestic uplink must be refused, got %v", err)
	}
}

func TestSetTransportRemarksTheTunnelImmediately(t *testing.T) {
	f := newVPNFixture(t)
	seedSecondaries(t, f, "dish0", "lte0")
	seedEnabledTunnels(t, f, 1) // auto: rides dish0
	ctx := context.Background()
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.uc.SetVPNProfileTransport(ctx, 1, "k-lte0"); err != nil {
		t.Fatal(err)
	}
	if got := f.wg.State("nasnet-wg0").Applied.FirewallMark; got != netmark.PinMark(3) {
		t.Fatalf("mark %#x after the pin, want lte0's", got)
	}
}

// LAN goes through the pool or nowhere. A secondary that is not slot one is
// still a raw uplink, and masquerading LAN out of it is the leak.
func TestLANNeverEgressesAnySecondary(t *testing.T) {
	var ups []Uplink
	for _, s := range append([]domain.UplinkSlot{domain.SlotDomestic}, domain.SecondarySlots()...) {
		ups = append(ups, Uplink{IfName: string(s), Slot: s, Table: tableFor(s),
			UplinkIndex: uplinkIndexFor(s)})
	}
	names := lanEgressNames(ups, VPNRouteState{IfNames: []string{"nasnet-wg0"}})
	for _, n := range names {
		for _, s := range domain.SecondarySlots() {
			if n == string(s) {
				t.Fatalf("LAN may egress %s in the clear", n)
			}
		}
	}
}

// Without a limit nothing is ever degraded, so a slow WAN reads perfect.
func TestEverySecondaryHasADegradedRTTLimit(t *testing.T) {
	cfg := DefaultHealthConfig()
	for _, s := range domain.SecondarySlots() {
		if cfg.DegradedRTTms[s] <= 0 {
			t.Fatalf("%s has no degraded RTT limit", s)
		}
	}
}

// Provisioning before the dish arrives warns; a box whose only WAN sits in a
// later slot is fully set up and must not be told otherwise. This is the
// predicate the V33 guard runs, not the whole enable pipeline.
func TestASecondaryInAnySlotCountsAsAssigned(t *testing.T) {
	for _, slot := range domain.SecondarySlots() {
		ups := []Uplink{
			{IfName: "eth0", Key: "k-eth0", Slot: domain.SlotDomestic, UplinkIndex: 1},
			{IfName: "wan1", Key: "k-wan1", Slot: slot, UplinkIndex: uplinkIndexFor(slot)},
		}
		if len(secondariesOf(ups)) == 0 {
			t.Fatalf("a uplink in %s reads as no secondary at all", slot)
		}
	}
	only := []Uplink{{IfName: "eth0", Slot: domain.SlotDomestic, UplinkIndex: 1}}
	if len(secondariesOf(only)) != 0 {
		t.Fatal("a domestic-only box must still warn")
	}
}
