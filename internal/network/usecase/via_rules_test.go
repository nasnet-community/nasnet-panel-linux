package usecase

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// Domestic + secondary (idx 2) + secondary3 (idx 4); secondary2 and 4 unassigned.
func viaTestUplinks() []Uplink {
	return []Uplink{
		{IfName: "enp1s0", Table: 201, UplinkIndex: 1, Slot: domain.SlotDomestic, GroupIndex: 1},
		{IfName: "enp2s0", Table: 202, UplinkIndex: 2, Slot: domain.SlotSecondary, GroupIndex: 2},
		{IfName: "enp4s0", Table: 205, UplinkIndex: 4, Slot: domain.SlotSecondary3, GroupIndex: 2},
	}
}

func TestViaRules_ExactPreferencesTablesAndMasks(t *testing.T) {
	rules := ViaRules(viaTestUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})

	want := map[int]struct {
		mark      uint32
		table     int
		blackhole bool
	}{
		200: {netmark.GroupMark(netmark.GroupForeignVia(2)), 207, false},
		209: {netmark.GroupMark(netmark.GroupForeignVia(2)), 0, true},
		219: {netmark.GroupMark(netmark.GroupForeignVia(3)), 0, true},
		220: {netmark.GroupMark(netmark.GroupForeignVia(4)), 209, false},
		229: {netmark.GroupMark(netmark.GroupForeignVia(4)), 0, true},
		239: {netmark.GroupMark(netmark.GroupForeignVia(5)), 0, true},
	}
	if len(rules) != len(want) {
		t.Fatalf("got %d rules, want %d: %+v", len(rules), len(want), rules)
	}
	for _, r := range rules {
		w, ok := want[r.Pref]
		if !ok {
			t.Errorf("unexpected rule at pref %d: %+v", r.Pref, r)
			continue
		}
		if r.FwMark != w.mark || r.FwMask != netmark.MaskGroup ||
			r.Table != w.table || r.Blackhole != w.blackhole {
			t.Errorf("pref %d = %+v, want mark 0x%x table %d blackhole %v",
				r.Pref, r, w.mark, w.table, w.blackhole)
		}
	}
}

// The terminators are what stops a stale via mark after a role change: they
// exist for every slot, assigned or not, pool or no pool.
func TestViaRules_TerminatorsSurviveEverything(t *testing.T) {
	for _, tc := range []struct {
		name    string
		uplinks []Uplink
		vpn     VPNRouteState
	}{
		{"no pool", viaTestUplinks(), VPNRouteState{}},
		{"no secondaries", viaTestUplinks()[:1], VPNRouteState{IfNames: []string{system.WGLinkName}}},
		{"nothing at all", nil, VPNRouteState{}},
	} {
		rules := ViaRules(tc.uplinks, tc.vpn)
		blackholes := 0
		for _, r := range rules {
			if !r.Blackhole {
				t.Errorf("%s: lookup rule at pref %d should not exist", tc.name, r.Pref)
			} else {
				blackholes++
			}
		}
		if blackholes != 4 {
			t.Errorf("%s: %d terminators, want 4", tc.name, blackholes)
		}
	}
}

func TestAllRules_ViaBlockSitsBetweenGroupsAndFallback(t *testing.T) {
	all := AllRules(twoGroups(), viaTestUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})
	var seen int
	for _, r := range all {
		if r.Pref >= RulePrefViaBase && r.Pref < RulePrefFallbackBase {
			seen++
		}
	}
	if seen == 0 {
		t.Fatal("AllRules emits no via rules")
	}
}
