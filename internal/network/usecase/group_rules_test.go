package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func twoGroups() []domain.WANGroup {
	return []domain.WANGroup{
		{Name: "domestic", GroupIndex: netmark.GroupDomestic, RuleBase: 110, RuleBlackhole: 149},
		{Name: "foreign", GroupIndex: netmark.GroupForeign, RuleBase: 150, RuleBlackhole: 199},
	}
}

// Pins sit above group rules; both fields set means pinned.
func TestPinRules_AboveGroupRulesAndTerminatePerInterface(t *testing.T) {
	pins := PinRules(twoUplinks())
	groups := GroupRules(twoGroups(), twoUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})

	for _, p := range pins {
		for _, g := range groups {
			if p.Pref >= g.Pref {
				t.Fatalf("pin rule at pref %d is not above group rule at pref %d", p.Pref, g.Pref)
			}
		}
	}

	// Own lookup AND terminator, so it can't spill onto a sibling.
	for _, u := range twoUplinks() {
		mark := netmark.PinMark(u.UplinkIndex)
		var lookup, blackhole bool
		for _, r := range pins {
			if r.FwMark != mark || r.FwMask != netmark.MaskPin {
				continue
			}
			if r.Blackhole {
				blackhole = true
			} else if r.Table == u.Table {
				lookup = true
			}
		}
		if !lookup {
			t.Errorf("uplink %d has no pin lookup rule", u.UplinkIndex)
		}
		if !blackhole {
			t.Errorf("uplink %d has no pin terminator — a dead uplink would re-emit "+
				"its pinned replies through a sibling with the wrong source address",
				u.UplinkIndex)
		}
	}
}

func TestPinRules_ExactPreferencesAndMasks(t *testing.T) {
	pins := PinRules(twoUplinks())
	want := map[int]struct {
		mark      uint32
		table     int
		blackhole bool
	}{
		50: {netmark.PinMark(1), 201, false},
		51: {netmark.PinMark(1), 0, true},
		52: {netmark.PinMark(2), 202, false},
		53: {netmark.PinMark(2), 0, true},
	}
	if len(pins) != len(want) {
		t.Fatalf("got %d pin rules, want %d", len(pins), len(want))
	}
	for _, r := range pins {
		w, ok := want[r.Pref]
		if !ok {
			t.Errorf("unexpected pin rule at pref %d", r.Pref)
			continue
		}
		if r.FwMark != w.mark || r.FwMask != netmark.MaskPin ||
			r.Table != w.table || r.Blackhole != w.blackhole {
			t.Errorf("pin rule at pref %d = %+v, want %+v", r.Pref, r, w)
		}
	}
}

// A foreign spill onto the domestic ISP discloses the real address.
func TestGroupRules_FailClosedSymmetrically(t *testing.T) {
	rs := GroupRules(twoGroups(), twoUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})

	for _, g := range twoGroups() {
		mark := netmark.GroupMark(g.GroupIndex)
		var member, terminator bool
		for _, r := range rs {
			if r.FwMark != mark || r.FwMask != netmark.MaskGroup {
				continue
			}
			if r.Blackhole && r.Pref == g.RuleBlackhole {
				terminator = true
			}
			if !r.Blackhole && r.Pref >= g.RuleBase && r.Pref < g.RuleBlackhole {
				member = true
			}
		}
		if !member {
			t.Errorf("group %q has no member rule", g.Name)
		}
		if !terminator {
			t.Errorf("group %q does not fail closed", g.Name)
		}
	}
}

// The foreign group's egress is the tunnel, never the uplink underneath it.
func TestGroupRules_ForeignGroupLeavesByTheTunnel(t *testing.T) {
	rs := GroupRules(twoGroups(), twoUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})

	mark := netmark.GroupMark(netmark.GroupForeign)
	var lookups []system.Rule
	for _, r := range rs {
		if r.FwMark == mark && r.FwMask == netmark.MaskGroup && !r.Blackhole {
			lookups = append(lookups, r)
		}
	}
	if len(lookups) != 1 {
		t.Fatalf("foreign lookups = %+v, want exactly one", lookups)
	}
	if lookups[0].Pref != 150 || lookups[0].Table != system.WGTable {
		t.Errorf("foreign lookup = %+v, want pref 150 into table %d", lookups[0], system.WGTable)
	}
}

// The kill switch: no tunnel, so foreign traffic dies instead of falling through.
func TestGroupRules_ForeignGroupBlackholesWithoutATunnel(t *testing.T) {
	rs := GroupRules(twoGroups(), twoUplinks(), VPNRouteState{})

	mark := netmark.GroupMark(netmark.GroupForeign)
	var terminator bool
	for _, r := range rs {
		if r.FwMark != mark || r.FwMask != netmark.MaskGroup {
			continue
		}
		if r.Blackhole {
			terminator = r.Pref == 199
			continue
		}
		t.Errorf("foreign traffic still has an exit at pref %d into table %d", r.Pref, r.Table)
	}
	if !terminator {
		t.Error("the foreign group does not fail closed")
	}

	// The domestic group is untouched by any of this.
	domestic := netmark.GroupMark(netmark.GroupDomestic)
	var domesticLookup bool
	for _, r := range rs {
		if r.FwMark == domestic && r.FwMask == netmark.MaskGroup && !r.Blackhole && r.Table == 201 {
			domesticLookup = true
		}
	}
	if !domesticLookup {
		t.Error("the domestic group lost its exit")
	}
}

// A group mark naming an uplink would make failover restart xray.
func TestGroupRules_MarkNamesTheGroupNotTheUplink(t *testing.T) {
	rs := GroupRules(twoGroups(), twoUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})
	for _, r := range rs {
		if r.FwMask == netmark.MaskGroup && netmark.Pin(r.FwMark) != 0 {
			t.Errorf("group rule at pref %d carries a pin field: 0x%08x", r.Pref, r.FwMark)
		}
		if r.OifName != "" {
			t.Errorf("group rule at pref %d matches an interface: %q", r.Pref, r.OifName)
		}
	}
}

func TestAllRules_NoPreferenceCollisions(t *testing.T) {
	rs := AllRules(twoGroups(), twoUplinks(), VPNRouteState{IfNames: []string{system.WGLinkName}})
	seen := map[int]bool{}
	for _, r := range rs {
		if seen[r.Pref] && !r.Blackhole {
			t.Errorf("duplicate non-blackhole rule at pref %d", r.Pref)
		}
		seen[r.Pref] = true
	}
	for _, pref := range []int{20, 21, 30, 50, 51, 52, 53, 110, 149, 150, 199, 32000, 32001, 32002} {
		if !seen[pref] {
			t.Errorf("pref %d missing from the complete rule set", pref)
		}
	}
}

func TestApplyNftState_StampsThePinPerUplink(t *testing.T) {
	fa := &nft.FakeApplier{}
	m := nft.NewManager(fa)
	if err := ApplyNftState(context.Background(), m, twoUplinks()); err != nil {
		t.Fatal(err)
	}
	rs := m.Snapshot()
	if !rs.Connmark {
		t.Error("connmark must stay enabled — download shaping depends on it")
	}
	if len(rs.IngressPins) != 2 {
		t.Fatalf("got %d pins, want 2", len(rs.IngressPins))
	}
	byName := map[string]uint32{}
	for _, p := range rs.IngressPins {
		byName[p.IfName] = p.Index
	}
	if byName["enp1s0"] != 1 || byName["enp2s0"] != 2 {
		t.Errorf("pins = %+v", rs.IngressPins)
	}
}

func TestApplySysctls_SetsBothAllAndPerInterfaceRPFilter(t *testing.T) {
	ctx := context.Background()
	be := system.NewFakeBackend()
	if err := ApplySysctls(ctx, be, twoUplinks(), false, ""); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"net.ipv4.conf.all.rp_filter":    "2",
		"net.ipv4.conf.enp1s0.rp_filter": "2",
		"net.ipv4.conf.enp2s0.rp_filter": "2",
		"net.ipv4.tcp_fwmark_accept":     "1",
		"net.ipv4.fwmark_reflect":        "1",
		// L4 hash keeps a flow on one pool member.
		"net.ipv4.fib_multipath_hash_policy": "1",
		"net.ipv4.conf.enp1s0.arp_ignore":    "1",
		"net.ipv4.conf.enp1s0.arp_announce":  "2",
		"net.ipv4.conf.enp2s0.arp_ignore":    "1",
		"net.ipv4.conf.enp2s0.arp_announce":  "2",
	} {
		got, err := be.SysctlGet(ctx, key)
		if err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", key, got, err, want)
		}
	}
	if _, err := be.SysctlGet(ctx, "net.ipv4.conf.enp1s0.arp_filter"); err == nil {
		t.Error("arp_filter was set; it decides by route lookup and RouteTable= breaks that")
	}
	// Written as 0, not left alone: forwarding without the LAN rules is open.
	if got, err := be.SysctlGet(ctx, "net.ipv4.ip_forward"); err != nil || got != "0" {
		t.Errorf("ip_forward = %q (%v), want \"0\" with no LAN", got, err)
	}
}
