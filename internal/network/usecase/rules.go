package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// Routing policy preferences
const (
	// A socket bound to an interface gets that interface's table
	RulePrefOifBase = 20

	// Specific routes still resolve from main, a default is refused. At 45 so
	// five uplinks and eight tunnels all fit an oif rule under it.
	RulePrefMainSuppress = 45

	// Unmarked traffic
	RulePrefFallbackBase = 32000
	// Terminates the fallback list.
	RulePrefFallbackBlackhole = 32002

	tableMain = 254
)

// Uplink is one uplink's routing identity.
type Uplink struct {
	IfName string
	// Key is the interface's stable identity, which is what a port-forward row
	// stores — IfName can be reassigned to a different device.
	Key         string
	Table       int
	UplinkIndex uint32
	Slot        domain.UplinkSlot
	GroupIndex  uint32
}

// VPNRouteState is the pool as the rules need it. Empty IfNames and the
// foreign group blackholes — that's the kill switch, not a bug.
type VPNRouteState struct {
	IfNames []string
}

func (v VPNRouteState) Active() bool { return len(v.IfNames) > 0 }

// BaseRules builds what must exist the moment RouteTable= empties main
func BaseRules(uplinks []Uplink, vpn VPNRouteState) []system.Rule {
	var rules []system.Rule

	// pref 20, 21, … in a stable order so preferences don't shuffle per boot
	ordered := append([]Uplink(nil), uplinks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })
	oifNames := make([]string, 0, len(ordered)+1)
	oifTables := make([]int, 0, len(ordered)+1)
	for _, u := range ordered {
		oifNames = append(oifNames, u.IfName)
		oifTables = append(oifTables, u.Table)
	}
	// Each tunnel needs one too, or a socket bound to it finds no route.
	for _, name := range vpn.IfNames {
		oifNames = append(oifNames, name)
		oifTables = append(oifTables, system.WGTable)
	}
	if limit := RulePrefMainSuppress - RulePrefOifBase; len(oifNames) > limit {
		// Past this the next oif rule lands on the suppressor and replaces it.
		oifNames, oifTables = oifNames[:limit], oifTables[:limit]
	}
	for i := range oifNames {
		rules = append(rules, system.Rule{
			Pref:    RulePrefOifBase + i,
			OifName: oifNames[i],
			Table:   oifTables[i],
		})
	}

	// pref 30 — main for anything above /0, never a default.
	rules = append(rules, system.Rule{
		Pref:              RulePrefMainSuppress,
		Table:             tableMain,
		SuppressSet:       true,
		SuppressPrefixLen: 0,
	})

	// pref 32000+ — ordered fallback; failover moves these defaults, so the
	// kernel walks the list itself. Only exception to fail-closed.
	for i, table := range fallbackTables(ordered, vpn) {
		rules = append(rules, system.Rule{
			Pref:  RulePrefFallbackBase + i,
			Table: table,
		})
	}
	rules = append(rules, system.Rule{Pref: RulePrefFallbackBlackhole, Blackhole: true})

	return rules
}

// fallbackTables is what unmarked traffic walks: tunnel, then domestic, then the
// rest. Never the secondary's own table — that is the leak the kill switch stops.
// Domestic stays on so a box with no VPN can still fetch its updates.
func fallbackTables(uplinks []Uplink, vpn VPNRouteState) []int {
	var out []int
	if vpn.Active() {
		out = append(out, system.WGTable)
	}

	rank := func(u Uplink) int {
		if u.Slot == domain.SlotDomestic {
			return 0
		}
		return 1
	}
	rest := make([]Uplink, 0, len(uplinks))
	for _, u := range uplinks {
		if u.Slot.IsSecondary() {
			continue
		}
		rest = append(rest, u)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		ri, rj := rank(rest[i]), rank(rest[j])
		if ri != rj {
			return ri < rj
		}
		return rest[i].UplinkIndex < rest[j].UplinkIndex
	})
	for _, u := range rest {
		out = append(out, u.Table)
	}

	// Only this many fit ahead of the terminator.
	if limit := RulePrefFallbackBlackhole - RulePrefFallbackBase; len(out) > limit {
		out = out[:limit]
	}
	return out
}

// isStockRule reports whether the kernel owns a preference
func isStockRule(pref int) bool { return pref == 0 || pref >= 32766 }

// ReconcileRules makes the kernel match want. Adds before deletes.
func ReconcileRules(ctx context.Context, be system.Backend, want []system.Rule) error {
	for _, r := range want {
		if err := be.RuleAdd(ctx, r); err != nil {
			return fmt.Errorf("add rule pref %d: %w", r.Pref, err)
		}
	}

	have, err := be.RuleList(ctx)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}
	for _, r := range have {
		if isStockRule(r.Pref) {
			continue
		}
		keep := false
		for _, w := range want {
			if w.Equal(r) {
				keep = true
				break
			}
		}
		if !keep {
			if err := be.RuleDel(ctx, r); err != nil {
				return fmt.Errorf("delete stale rule pref %d: %w", r.Pref, err)
			}
		}
	}
	return nil
}

// RulePrefPinBase starts the pin block, two per uplink. Above the group rules:
// a flow carrying both fields is pinned, not policy-routed.
const RulePrefPinBase = 50

// PinRules gives each uplink a lookup and its own terminator. SNAT runs after
// the route lookup, so only conntrack knows which uplink a reply came in on.
func PinRules(uplinks []Uplink) []system.Rule {
	ordered := append([]Uplink(nil), uplinks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })

	var rules []system.Rule
	for i, u := range ordered {
		mark := netmark.PinMark(u.UplinkIndex)
		base := RulePrefPinBase + i*2
		rules = append(rules,
			system.Rule{Pref: base, FwMark: mark, FwMask: netmark.MaskPin, Table: u.Table},
			system.Rule{Pref: base + 1, FwMark: mark, FwMask: netmark.MaskPin, Blackhole: true},
		)
	}
	return rules
}

// GroupRules builds one rule per member, terminated by the group's blackhole.
//
// The foreign group is the exception: members ignored, egress is the tunnel or
// its own blackhole.
func GroupRules(groups []domain.WANGroup, uplinks []Uplink, vpn VPNRouteState) []system.Rule {
	byGroup := map[uint32][]Uplink{}
	for _, u := range uplinks {
		byGroup[u.GroupIndex] = append(byGroup[u.GroupIndex], u)
	}

	ordered := append([]domain.WANGroup(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].RuleBase < ordered[j].RuleBase })

	var rules []system.Rule
	for _, g := range ordered {
		mark := netmark.GroupMark(g.GroupIndex)

		if g.GroupIndex == netmark.GroupForeign {
			if vpn.Active() {
				rules = append(rules, system.Rule{
					Pref: g.RuleBase, FwMark: mark, FwMask: netmark.MaskGroup, Table: system.WGTable,
				})
			}
			rules = append(rules, system.Rule{
				Pref: g.RuleBlackhole, FwMark: mark, FwMask: netmark.MaskGroup, Blackhole: true,
			})
			continue
		}

		members := byGroup[g.GroupIndex]
		sort.Slice(members, func(i, j int) bool { return members[i].UplinkIndex < members[j].UplinkIndex })

		for i, m := range members {
			pref := g.RuleBase + i
			if pref >= g.RuleBlackhole {
				break // terminator stays last
			}
			rules = append(rules, system.Rule{
				Pref: pref, FwMark: mark, FwMask: netmark.MaskGroup, Table: m.Table,
			})
		}
		rules = append(rules, system.Rule{
			Pref: g.RuleBlackhole, FwMark: mark, FwMask: netmark.MaskGroup, Blackhole: true,
		})
	}
	return rules
}

// Ten preferences per secondary slot, in slot order, between the group block
// and the fallback.
const (
	RulePrefViaBase   = 200
	rulePrefViaStride = 10
)

// One secondary's slice of the pool; 203 stays the whole of it. Secondaries
// only (2-5 → 207-210), since index 1 would collide with a raw uplink table.
func vpnViaTableFor(uplinkIndex uint32) int { return 205 + int(uplinkIndex) }

// A lookup when there is something to look up, a terminator always: a stale via
// mark must die here rather than walk on and out the domestic line.
func ViaRules(uplinks []Uplink, vpn VPNRouteState) []system.Rule {
	assigned := map[uint32]bool{}
	for _, up := range uplinks {
		if up.Slot.IsSecondary() {
			assigned[up.UplinkIndex] = true
		}
	}

	var rules []system.Rule
	for _, slot := range domain.SecondarySlots() {
		idx := uplinkIndexFor(slot)
		mark := netmark.GroupMark(netmark.GroupForeignVia(idx))
		base := RulePrefViaBase + rulePrefViaStride*int(idx-2)
		if vpn.Active() && assigned[idx] {
			rules = append(rules, system.Rule{
				Pref: base, FwMark: mark, FwMask: netmark.MaskGroup,
				Table: vpnViaTableFor(idx),
			})
		}
		rules = append(rules, system.Rule{
			Pref: base + rulePrefViaStride - 1, FwMark: mark,
			FwMask: netmark.MaskGroup, Blackhole: true,
		})
	}
	return rules
}

// AllRules: oif, suppressor, pins, groups, unmarked fallback.
func AllRules(groups []domain.WANGroup, uplinks []Uplink, vpn VPNRouteState) []system.Rule {
	base := BaseRules(uplinks, vpn)

	// Pins and groups go between the suppressor and the fallback.
	var head, tail []system.Rule
	for _, r := range base {
		if r.Pref >= RulePrefFallbackBase {
			tail = append(tail, r)
		} else {
			head = append(head, r)
		}
	}

	out := append([]system.Rule(nil), head...)
	out = append(out, PinRules(uplinks)...)
	out = append(out, GroupRules(groups, uplinks, vpn)...)
	out = append(out, ViaRules(uplinks, vpn)...)
	return append(out, tail...)
}

// Fixed table numbers, so a snapshot from one build restores under another.
// 203 is the pool's, so the extra secondaries skip it.
func tableFor(slot domain.UplinkSlot) int {
	switch slot {
	case domain.SlotDomestic:
		return 201
	case domain.SlotSecondary:
		return 202
	case domain.SlotSecondary2:
		return 204
	case domain.SlotSecondary3:
		return 205
	case domain.SlotSecondary4:
		return 206
	}
	return 0
}

func uplinkIndexFor(slot domain.UplinkSlot) uint32 {
	switch slot {
	case domain.SlotDomestic:
		return 1
	case domain.SlotSecondary:
		return 2
	case domain.SlotSecondary2:
		return 3
	case domain.SlotSecondary3:
		return 4
	case domain.SlotSecondary4:
		return 5
	}
	return 0
}

func groupIndexFor(slot domain.UplinkSlot) uint32 {
	if slot == domain.SlotDomestic {
		return netmark.GroupDomestic
	}
	if slot.IsSecondary() {
		return netmark.GroupForeign
	}
	return 0
}
