package usecase

import (
	"context"
	"fmt"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

func twoUplinks() []Uplink {
	return []Uplink{
		{IfName: "enp1s0", Table: 201, UplinkIndex: 1, Slot: domain.SlotDomestic, GroupIndex: 1},
		{IfName: "enp2s0", Table: 202, UplinkIndex: 2, Slot: domain.SlotSecondary, GroupIndex: 2},
	}
}

func find(rs []system.Rule, pref int) *system.Rule {
	for i := range rs {
		if rs[i].Pref == pref {
			return &rs[i]
		}
	}
	return nil
}

// A socket bound to an interface only matches routes on that device. resolved's
// per-link DNS, health probes and `ping -I` all need this.
func TestBaseRules_OifRulesPerUplink(t *testing.T) {
	rs := BaseRules(twoUplinks(), VPNRouteState{})

	r20 := find(rs, 20)
	if r20 == nil || r20.OifName != "enp1s0" || r20.Table != 201 {
		t.Fatalf("pref 20 = %+v, want oif enp1s0 -> table 201", r20)
	}
	r21 := find(rs, 21)
	if r21 == nil || r21.OifName != "enp2s0" || r21.Table != 202 {
		t.Fatalf("pref 21 = %+v, want oif enp2s0 -> table 202", r21)
	}
	if r20.FwMask != 0 || r21.FwMask != 0 {
		t.Error("oif rules must not match on a mark")
	}
}

// pref 30 is load-bearing: without it a marked packet bound for the LAN or an
// uplink subnet finds nothing in its group table and dies in a blackhole.
func TestBaseRules_MainSuppressorConsultsMainButRefusesADefault(t *testing.T) {
	r := find(BaseRules(twoUplinks(), VPNRouteState{}), 30)
	if r == nil {
		t.Fatal("no pref 30 rule")
	}
	if !r.SuppressSet || r.SuppressPrefixLen != 0 {
		t.Errorf("pref 30 = %+v, want suppress_prefixlength 0", r)
	}
	if r.Table != 254 {
		t.Errorf("pref 30 table = %d, want 254 (main)", r.Table)
	}
	if r.Blackhole {
		t.Error("pref 30 must be a lookup, not a blackhole")
	}
}

// Tunnel first so the box's own traffic doesn't disclose the domestic address;
// domestic last so a box with no VPN can still fetch updates.
func TestBaseRules_OrderedFallbackTunnelThenDomesticThenDrop(t *testing.T) {
	rs := BaseRules(twoUplinks(), VPNRouteState{Active: true})

	r0 := find(rs, 32000)
	if r0 == nil || r0.Table != system.WGTable {
		t.Fatalf("pref 32000 = %+v, want the tunnel's table", r0)
	}
	r1 := find(rs, 32001)
	if r1 == nil || r1.Table != 201 {
		t.Fatalf("pref 32001 = %+v, want the domestic uplink's table", r1)
	}
	r2 := find(rs, 32002)
	if r2 == nil || !r2.Blackhole {
		t.Fatalf("pref 32002 = %+v, want a blackhole terminator", r2)
	}
	for _, r := range []*system.Rule{r0, r1, r2} {
		if r.FwMask != 0 {
			t.Errorf("fallback rule at pref %d matches a mark; it must match everything", r.Pref)
		}
	}
}

// The kill switch as a routing invariant: nothing unmarked reaches table 202.
func TestBaseRules_FallbackNeverReachesTheRawSecondaryUplink(t *testing.T) {
	for _, vpn := range []VPNRouteState{{Active: false}, {Active: true}} {
		for _, r := range BaseRules(twoUplinks(), vpn) {
			if r.Pref >= RulePrefFallbackBase && r.Table == 202 {
				t.Errorf("vpn active=%v: fallback rule at pref %d sends unmarked traffic "+
					"out the raw secondary uplink", vpn.Active, r.Pref)
			}
		}
	}
}

// No profile, so domestic is the whole list — the box still needs its updates.
func TestBaseRules_FallbackWithoutATunnelIsDomesticOnly(t *testing.T) {
	rs := BaseRules(twoUplinks(), VPNRouteState{})

	r0 := find(rs, 32000)
	if r0 == nil || r0.Table != 201 {
		t.Fatalf("pref 32000 = %+v, want the domestic uplink's table", r0)
	}
	if r := find(rs, 32001); r != nil && !r.Blackhole {
		t.Errorf("pref 32001 = %+v, want nothing between domestic and the terminator", r)
	}
	if r := find(rs, 32002); r == nil || !r.Blackhole {
		t.Error("no fallback terminator")
	}
}

// A socket bound to the tunnel finds no route without this.
func TestBaseRules_TunnelGetsAnOifRuleOnlyWhenActive(t *testing.T) {
	active := BaseRules(twoUplinks(), VPNRouteState{Active: true})
	var found *system.Rule
	for i := range active {
		if active[i].OifName == system.WGLinkName {
			found = &active[i]
		}
	}
	if found == nil || found.Table != system.WGTable {
		t.Fatalf("tunnel oif rule = %+v, want a lookup into table %d", found, system.WGTable)
	}
	if found.Pref >= RulePrefMainSuppress {
		t.Errorf("tunnel oif rule at pref %d, at or past the suppressor", found.Pref)
	}

	for _, r := range BaseRules(twoUplinks(), VPNRouteState{}) {
		if r.OifName == system.WGLinkName {
			t.Error("a tunnel oif rule with no active profile")
		}
	}
}

// A single-uplink box must still get a fallback, or it loses its own egress.
func TestBaseRules_SingleUplinkStillGetsAFallback(t *testing.T) {
	rs := BaseRules([]Uplink{
		{IfName: "enp1s0", Table: 201, UplinkIndex: 1, Slot: domain.SlotDomestic, GroupIndex: 1},
	}, VPNRouteState{})
	if find(rs, 32000) == nil {
		t.Error("no fallback rule on a single-uplink box")
	}
	if find(rs, 32002) == nil {
		t.Error("no fallback terminator on a single-uplink box")
	}
}

func TestBaseRules_NoUplinksEmitsOnlyTheSuppressorAndTerminator(t *testing.T) {
	rs := BaseRules(nil, VPNRouteState{})
	if find(rs, 30) == nil {
		t.Error("pref 30 must exist regardless")
	}
	if find(rs, 32002) == nil {
		t.Error("the terminator must exist regardless")
	}
	if find(rs, 20) != nil || find(rs, 32000) != nil {
		t.Error("per-uplink rules emitted with no uplinks")
	}
}

// oif rules count up from 20, so enough of them would land on 30 and silently
// replace the suppressor — the one rule the whole policy depends on.
func TestBaseRules_OifRulesNeverReachTheSuppressorPref(t *testing.T) {
	var many []Uplink
	for i := 1; i <= 15; i++ {
		many = append(many, Uplink{
			IfName: fmt.Sprintf("eth%d", i), Table: 200 + i, UplinkIndex: uint32(i),
		})
	}
	rs := BaseRules(many, VPNRouteState{})

	sup := find(rs, RulePrefMainSuppress)
	if sup == nil || !sup.SuppressSet {
		t.Fatalf("pref %d is not the suppressor: %+v", RulePrefMainSuppress, sup)
	}
	for _, r := range rs {
		if r.OifName != "" && r.Pref >= RulePrefMainSuppress {
			t.Errorf("oif rule for %s landed at pref %d, at or past the suppressor", r.OifName, r.Pref)
		}
	}
}

func TestReconcileRules_AddsWantedAndRemovesStale(t *testing.T) {
	ctx := context.Background()
	be := system.NewFakeBackend()

	// A stale rule left by a previous configuration.
	stale := system.Rule{Pref: 150, Table: 202}
	if err := be.RuleAdd(ctx, stale); err != nil {
		t.Fatal(err)
	}

	want := BaseRules(twoUplinks(), VPNRouteState{})
	if err := ReconcileRules(ctx, be, want); err != nil {
		t.Fatal(err)
	}

	got, _ := be.RuleList(ctx)
	if len(got) != len(want) {
		t.Fatalf("got %d rules, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		if find(got, w.Pref) == nil {
			t.Errorf("wanted rule at pref %d is missing", w.Pref)
		}
	}
	if find(got, 150) != nil {
		t.Error("stale rule survived reconciliation")
	}
}

// Runs at boot and after every change; twice must produce no churn.
func TestReconcileRules_Idempotent(t *testing.T) {
	ctx := context.Background()
	be := system.NewFakeBackend()
	want := BaseRules(twoUplinks(), VPNRouteState{})
	for i := 0; i < 3; i++ {
		if err := ReconcileRules(ctx, be, want); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	got, _ := be.RuleList(ctx)
	if len(got) != len(want) {
		t.Errorf("got %d rules after three passes, want %d", len(got), len(want))
	}
}

// Stock rules at pref 0 and >= 32766 belong to the kernel.
func TestReconcileRules_LeavesStockRulesAlone(t *testing.T) {
	ctx := context.Background()
	be := system.NewFakeBackend()
	for _, p := range []int{0, 32766, 32767} {
		if err := be.RuleAdd(ctx, system.Rule{Pref: p, Table: 254}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileRules(ctx, be, BaseRules(twoUplinks(), VPNRouteState{})); err != nil {
		t.Fatal(err)
	}
	got, _ := be.RuleList(ctx)
	for _, p := range []int{0, 32766, 32767} {
		if find(got, p) == nil {
			t.Errorf("stock rule at pref %d was deleted", p)
		}
	}
}
