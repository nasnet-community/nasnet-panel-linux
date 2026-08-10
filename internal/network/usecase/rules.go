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

	// Specific routes still resolve from main, a default is refused.
	RulePrefMainSuppress = 30

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

// BaseRules builds what must exist the moment RouteTable= empties main
func BaseRules(uplinks []Uplink) []system.Rule {
	var rules []system.Rule

	// pref 20, 21, … in a stable order so preferences don't shuffle per boot
	ordered := append([]Uplink(nil), uplinks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })
	if limit := RulePrefMainSuppress - RulePrefOifBase; len(ordered) > limit {
		// Past this the next oif rule lands on the suppressor and replaces it.
		ordered = ordered[:limit]
	}
	for i, u := range ordered {
		rules = append(rules, system.Rule{
			Pref:    RulePrefOifBase + i,
			OifName: u.IfName,
			Table:   u.Table,
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
	for i, u := range fallbackOrder(ordered) {
		rules = append(rules, system.Rule{
			Pref:  RulePrefFallbackBase + i,
			Table: u.Table,
		})
	}
	rules = append(rules, system.Rule{Pref: RulePrefFallbackBlackhole, Blackhole: true})

	return rules
}

// fallbackOrder puts secondary first, then domestic, then the rest by index
func fallbackOrder(uplinks []Uplink) []Uplink {
	rank := func(u Uplink) int {
		switch u.Slot {
		case domain.SlotSecondary:
			return 0
		case domain.SlotDomestic:
			return 1
		default:
			return 2
		}
	}
	out := append([]Uplink(nil), uplinks...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i].UplinkIndex < out[j].UplinkIndex
	})
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
func GroupRules(groups []domain.WANGroup, uplinks []Uplink) []system.Rule {
	byGroup := map[uint32][]Uplink{}
	for _, u := range uplinks {
		byGroup[u.GroupIndex] = append(byGroup[u.GroupIndex], u)
	}

	ordered := append([]domain.WANGroup(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].RuleBase < ordered[j].RuleBase })

	var rules []system.Rule
	for _, g := range ordered {
		members := byGroup[g.GroupIndex]
		sort.Slice(members, func(i, j int) bool { return members[i].UplinkIndex < members[j].UplinkIndex })

		mark := netmark.GroupMark(g.GroupIndex)
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

// AllRules: oif, suppressor, pins, groups, unmarked fallback.
func AllRules(groups []domain.WANGroup, uplinks []Uplink) []system.Rule {
	base := BaseRules(uplinks)

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
	out = append(out, GroupRules(groups, uplinks)...)
	return append(out, tail...)
}

// Fixed table numbers, so a snapshot from one build restores under another
func tableFor(slot domain.UplinkSlot) int {
	switch slot {
	case domain.SlotDomestic:
		return 201
	case domain.SlotSecondary:
		return 202
	}
	return 0
}

func uplinkIndexFor(slot domain.UplinkSlot) uint32 {
	switch slot {
	case domain.SlotDomestic:
		return 1
	case domain.SlotSecondary:
		return 2
	}
	return 0
}

func groupIndexFor(slot domain.UplinkSlot) uint32 {
	switch slot {
	case domain.SlotDomestic:
		return netmark.GroupDomestic
	case domain.SlotSecondary:
		return netmark.GroupForeign
	}
	return 0
}
