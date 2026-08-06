package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// Routing policy preferences
const (
	// A socket bound to an interface gets that interface's table
	RulePrefOifBase = 20

	// Every specific route still resolves from main; a default is refused.
	// Load-bearing: without it marked LAN traffic dies in the blackhole below.
	RulePrefMainSuppress = 30

	// Unmarked traffic
	RulePrefFallbackBase = 32000
	// Terminates the fallback list.
	RulePrefFallbackBlackhole = 32002

	tableMain = 254
)

// Uplink is one uplink's routing identity.
type Uplink struct {
	IfName      string
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

	// pref 30 — consult main for every prefix above /0, refuse a default, so it
	// can't leak even if a default ever appears in main.
	rules = append(rules, system.Rule{
		Pref:              RulePrefMainSuppress,
		Table:             tableMain,
		SuppressSet:       true,
		SuppressPrefixLen: 0,
	})

	// pref 32000+ — ordered fallback, not a chosen group: failover already adds
	// and removes these defaults, so the kernel walks the list itself.
	//
	// The only exception to fail-closed, safe only because pkg/httpclient makes
	// the egress group a required parameter, so "unmarked" can't come to mean
	// "we forgot to classify this".
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
	// The terminator sits at RulePrefFallbackBlackhole, so only this many
	// uplinks fit ahead of it.
	if limit := RulePrefFallbackBlackhole - RulePrefFallbackBase; len(out) > limit {
		out = out[:limit]
	}
	return out
}

// isStockRule reports whether the kernel owns a preference
func isStockRule(pref int) bool { return pref == 0 || pref >= 32766 }

// ReconcileRules makes the kernel's rule set equal want, leaving stock rules
// alone. Adds before deletes, so there's no window with no policy.
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
