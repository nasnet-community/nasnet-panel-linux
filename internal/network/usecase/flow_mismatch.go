package usecase

// Curated intended-vs-actual checks. A full text diff would cry wolf over
// ordering and whitespace; these each name one thing that is actually wrong.

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

type flowMismatchInput struct {
	uplinks   []Uplink
	vpn       VPNRouteState
	pool      vpnPool
	liveRules []system.Rule
	rulesErr  error
	routes    map[int][]system.Route
	routeErrs map[int]error
	wgStatus  map[string]*system.WGStatus
	wgLinks   []string
	dnsUp     bool
	nftObj    *system.NftObjects
	nftErr    error
	lan       *domain.LANConfig
}

func (u *networkUsecase) flowMismatches(ctx context.Context, in flowMismatchInput) []FlowMismatch {
	out := []FlowMismatch{}
	out = append(out, u.ruleMismatches(ctx, in)...)
	out = append(out, routeMismatches(in)...)
	out = append(out, u.nftMismatches(in)...)
	out = append(out, u.wgMismatches(ctx, in)...)
	out = append(out, u.dnsMismatches(ctx, in)...)
	return out
}

func (u *networkUsecase) ruleMismatches(ctx context.Context, in flowMismatchInput) []FlowMismatch {
	if in.rulesErr != nil {
		return []FlowMismatch{{
			NodeID: "src-router", Rule: "rules-unreadable", Severity: "error",
			Message: "Could not read the kernel's routing rules.",
			Actual:  in.rulesErr.Error(),
		}}
	}
	var groups []domain.WANGroup
	if u.GroupRepo != nil {
		groups, _ = u.GroupRepo.List(ctx)
	}
	want := AllRules(groups, in.uplinks, in.vpn)

	var out []FlowMismatch
	for _, w := range want {
		if ruleAmong(in.liveRules, w) {
			continue
		}
		out = append(out, FlowMismatch{
			NodeID: nodeForRulePref(w), Rule: "rule-missing", Severity: "error",
			Message:  fmt.Sprintf("Routing rule at pref %d is missing.", w.Pref),
			Expected: fmtRule(w), Actual: "absent",
		})
	}
	// Rules in our range that nothing asked for. Warn, not error: a leftover
	// from a half-finished apply is worth naming but rarely breaks traffic.
	for _, live := range in.liveRules {
		if isStockRule(live.Pref) || !ourPref(live.Pref) {
			continue
		}
		if ruleAmong(want, live) {
			continue
		}
		out = append(out, FlowMismatch{
			NodeID: "src-router", Rule: "rule-unexpected", Severity: "warn",
			Message: fmt.Sprintf("An unexpected rule sits at pref %d.", live.Pref),
			Actual:  fmtRule(live),
		})
	}
	return out
}

func routeMismatches(in flowMismatchInput) []FlowMismatch {
	var out []FlowMismatch
	for _, up := range in.uplinks {
		if err := in.routeErrs[up.Table]; err != nil {
			out = append(out, FlowMismatch{
				NodeID: fmt.Sprintf("table-%d", up.Table), Rule: "route-unreadable", Severity: "warn",
				Message:  fmt.Sprintf("Could not read table %d, so its routes are unknown.", up.Table),
				Expected: "default dev " + up.IfName, Actual: err.Error(),
			})
			continue
		}
		if hasDefault(in.routes[up.Table]) {
			continue
		}
		out = append(out, FlowMismatch{
			NodeID: fmt.Sprintf("table-%d", up.Table), Rule: "route-missing", Severity: "error",
			Message:  fmt.Sprintf("Table %d has no default route, so %s carries nothing.", up.Table, up.IfName),
			Expected: "default dev " + up.IfName,
			Actual:   "absent",
		})
	}
	if !in.vpn.Active() {
		return out
	}
	if err := in.routeErrs[system.WGTable]; err == nil && !hasPoolDefault(in.routes[system.WGTable]) {
		out = append(out, FlowMismatch{
			NodeID: "table-203", Rule: "route-missing", Severity: "error",
			Message:  "The pool's default route is missing from table 203.",
			Expected: "default across the pool members",
			Actual:   "absent",
		})
	}
	return out
}

func (u *networkUsecase) nftMismatches(in flowMismatchInput) []FlowMismatch {
	if in.nftErr != nil {
		return []FlowMismatch{{
			NodeID: "src-router", Rule: "nft-unreadable", Severity: "warn",
			Message: "Could not read the live firewall.",
			Actual:  in.nftErr.Error(),
		}}
	}
	if in.nftObj == nil || u.Nft == nil {
		return nil
	}
	desired := u.Nft.Snapshot()
	var out []FlowMismatch
	for _, chain := range desired.ChainNames() {
		if contains(in.nftObj.Chains, chain) {
			continue
		}
		out = append(out, FlowMismatch{
			NodeID: nodeForChain(chain), Rule: "nft-chain-missing", Severity: "error",
			Message:  fmt.Sprintf("Firewall chain %s is not in the kernel.", chain),
			Expected: "chain " + chain, Actual: "absent",
		})
	}
	for _, set := range desired.SetNames() {
		if contains(in.nftObj.Sets, set) {
			continue
		}
		out = append(out, FlowMismatch{
			NodeID: "mark-domestic", Rule: "nft-set-missing", Severity: "error",
			Message:  fmt.Sprintf("Address set %s is not in the kernel.", set),
			Expected: "set " + set, Actual: "absent",
		})
	}
	for _, c := range desired.CounterNames() {
		if _, ok := in.nftObj.Counters[c]; ok {
			continue
		}
		// Without it the graph draws blank rates and says nothing is wrong.
		out = append(out, FlowMismatch{
			NodeID: "src-router", Rule: "nft-counter-missing", Severity: "warn",
			Message:  fmt.Sprintf("Counter %s is not in the kernel, so its rate reads empty.", c),
			Expected: "counter " + c, Actual: "absent",
		})
	}
	return out
}

func (u *networkUsecase) wgMismatches(_ context.Context, in flowMismatchInput) []FlowMismatch {
	inPool := map[string]bool{}
	for _, t := range in.pool.Tunnels {
		inPool[t.IfName] = true
	}
	var out []FlowMismatch
	// A link with no enabled profile behind it must not keep answering.
	for _, name := range in.wgLinks {
		if !inPool[name] {
			out = append(out, FlowMismatch{
				NodeID: "wg", Rule: "wg-device-unexpected", Severity: "warn",
				Message: fmt.Sprintf("Tunnel interface %s exists with no enabled profile.", name),
				Actual:  name,
			})
		}
	}
	for _, t := range in.pool.Tunnels {
		st := in.wgStatus[t.IfName]
		if st == nil {
			continue // the node itself already says the device is missing
		}
		if st.PublicKey != "" && st.PublicKey != t.Config.Peer.PublicKey {
			out = append(out, FlowMismatch{
				NodeID: "wg", Rule: "wg-peer-mismatch", Severity: "error",
				Message:  fmt.Sprintf("The live peer on %s is not the one %q configures.", t.IfName, t.Profile.Name),
				Expected: t.Config.Peer.PublicKey, Actual: st.PublicKey,
			})
		}
	}
	return out
}

func (u *networkUsecase) dnsMismatches(_ context.Context, in flowMismatchInput) []FlowMismatch {
	if in.lan == nil || !in.lan.Enabled {
		return nil
	}
	if in.dnsUp {
		return nil
	}
	return []FlowMismatch{{
		NodeID: "dns", Rule: "dnsmasq-not-running", Severity: "error",
		Message: "dnsmasq is not running, so LAN clients cannot resolve anything.",
	}}
}

// ourPref is the pref ranges this panel writes: oif rules and the main
// suppressor, the ingress pins, the group rules, and the fallback list.
// Tailscale, libvirt and a hand-added rule all live outside them.
func ourPref(pref int) bool {
	switch {
	case pref >= RulePrefOifBase && pref <= RulePrefMainSuppress:
		return true
	case pref >= RulePrefPinBase && pref <= 199:
		return true
	case pref >= RulePrefFallbackBase && pref <= RulePrefFallbackBlackhole:
		return true
	}
	return false
}

// nodeForRulePref pins a policy-rule finding to the node whose stage it serves.
func nodeForRulePref(r system.Rule) string {
	switch {
	case r.Pref >= 110 && r.Pref <= 149:
		return "mark-domestic"
	case r.Pref >= 150 && r.Pref <= 199:
		return "mark-foreign"
	case r.Pref >= RulePrefPinBase && r.Pref < 110:
		if r.Table == 201 {
			return "uplink-domestic"
		}
		return "uplink-secondary"
	case r.Table == system.WGTable:
		return "table-203"
	case r.Table == 201:
		return "table-201"
	case r.Table == 202:
		return "table-202"
	default:
		return "src-router"
	}
}

func nodeForChain(chain string) string {
	switch chain {
	case "killswitch_out", "killswitch_fwd":
		return "killswitch"
	case "mangle_pre", "mangle_post":
		return "mark-foreign"
	case "filter_fwd":
		return "src-lan"
	case "nat_post", "nat_pre":
		return "uplink-domestic"
	default:
		return "src-router"
	}
}

func fmtRule(r system.Rule) string {
	s := fmt.Sprintf("pref %d", r.Pref)
	if r.FwMask != 0 {
		s += fmt.Sprintf(" fwmark 0x%x/0x%x", r.FwMark, r.FwMask)
	}
	if r.OifName != "" {
		s += " oif " + r.OifName
	}
	if r.SuppressSet {
		s += fmt.Sprintf(" suppress_prefixlength %d", r.SuppressPrefixLen)
	}
	switch {
	case r.Blackhole:
		s += " blackhole"
	default:
		s += fmt.Sprintf(" lookup %d", r.Table)
	}
	return s
}

func ruleAmong(list []system.Rule, want system.Rule) bool {
	for _, r := range list {
		if r.Equal(want) {
			return true
		}
	}
	return false
}

func routeAmong(list []system.Route, want system.Route) bool {
	for _, r := range list {
		if r.Dest == want.Dest && r.OifName == want.OifName {
			return true
		}
	}
	return false
}
