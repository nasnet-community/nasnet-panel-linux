package usecase

// The what-if tracer: replays the routing decision for one destination without
// sending a packet, then asks the kernel whether it agrees.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

var ErrBadTraceInput = errors.New("trace input")

type TraceRequest struct {
	Dest   string `json:"dest"`
	Source string `json:"source"`
}

type TraceStep struct {
	Title    string   `json:"title"`
	Evidence []string `json:"evidence"`
	Verdict  string   `json:"verdict"`
}

type TraceView struct {
	Dest         string      `json:"dest"`
	ResolvedIP   string      `json:"resolved_ip"`
	Source       string      `json:"source"`
	Steps        []TraceStep `json:"steps"`
	PathNodes    []string    `json:"path_nodes"`
	PathEdges    []string    `json:"path_edges"`
	FinalVerdict string      `json:"final_verdict"`
}

// Source kinds differ in where their mark comes from, which is the only thing
// that changes the answer today.
var traceSources = map[string]string{
	"lan":           "src-lan",
	"xray-foreign":  "src-xray",
	"xray-domestic": "src-xray",
	"router":        "src-router",
}

func (u *networkUsecase) TraceFlow(ctx context.Context, req TraceRequest) (*TraceView, error) {
	startNode, ok := traceSources[req.Source]
	if !ok {
		return nil, fmt.Errorf("%w: unknown source %q", ErrBadTraceInput, req.Source)
	}
	host := strings.TrimSpace(req.Dest)
	if host == "" || !validTraceHost(host) {
		return nil, fmt.Errorf("%w: the destination must be an IP address or a hostname",
			ErrBadTraceInput)
	}

	v := &TraceView{Dest: host, Source: req.Source}

	ip, step, err := u.traceResolve(ctx, host)
	if err != nil {
		return nil, err
	}
	if step != nil {
		v.Steps = append(v.Steps, *step)
	}
	v.ResolvedIP = ip

	mark, markStep := u.traceMark(ctx, req.Source, ip)
	v.Steps = append(v.Steps, markStep...)

	rules, err := u.Backend.RuleList(ctx)
	if err != nil {
		return nil, fmt.Errorf("read routing rules: %w", err)
	}
	routes := map[int][]system.Route{}
	for _, t := range []int{201, 202, system.WGTable, 254} {
		if rs, rerr := u.Backend.RouteList(ctx, t); rerr == nil && len(rs) > 0 {
			routes[t] = rs
		}
	}

	walk, route, matched := walkPolicy(rules, routes, ip, mark)
	v.Steps = append(v.Steps, walk)
	v.Steps = append(v.Steps, u.traceKernelCheck(ctx, ip, mark, route))

	u.finishTrace(ctx, v, startNode, mark, route, matched)
	return v, nil
}

func (u *networkUsecase) traceResolve(ctx context.Context, host string) (string, *TraceStep, error) {
	if net.ParseIP(host) != nil {
		return host, nil, nil
	}
	addr, err := u.doh().Resolve(ctx, host)
	if err != nil {
		return "", nil, fmt.Errorf("%w: could not resolve %q: %v", ErrBadTraceInput, host, err)
	}
	return addr.String(), &TraceStep{
		Title:   "Resolve",
		Verdict: "info",
		Evidence: []string{
			fmt.Sprintf("%s → %s", host, addr),
			"resolved here over DoH; a LAN client would ask dnsmasq and could get a different address",
		},
	}, nil
}

// traceMark reproduces what stamps the group mark: nft set lookups for LAN
// traffic, the managed outbound's own mark for xray, nothing for the router.
func (u *networkUsecase) traceMark(ctx context.Context, source, ip string) (uint32, []TraceStep) {
	switch source {
	case "xray-foreign":
		mark := netmark.GroupMark(netmark.GroupForeign)
		return mark, []TraceStep{{
			Title: "Mark", Verdict: "ok",
			Evidence: []string{
				"outbound direct-foreign stamps " + netmark.Hex(mark) + " on its own sockets",
				"the destination is not consulted: the outbound already decided",
			},
		}}
	case "xray-domestic":
		mark := netmark.GroupMark(netmark.GroupDomestic)
		return mark, []TraceStep{{
			Title: "Mark", Verdict: "ok",
			Evidence: []string{"outbound direct-domestic stamps " + netmark.Hex(mark)},
		}}
	case "router":
		return 0, []TraceStep{{
			Title: "Mark", Verdict: "info",
			Evidence: []string{"traffic the box originates carries no group mark; it takes the fallback"},
		}}
	}

	classify := TraceStep{Title: "Classify", Verdict: "ok"}
	domestic := false
	for _, set := range []string{DomesticSetV4, DomainSetV4} {
		in, err := u.nftReader().SetContains(ctx, set, ip)
		if err != nil {
			classify.Evidence = append(classify.Evidence,
				fmt.Sprintf("could not read @%s: %v", set, err))
			classify.Verdict = "warn"
			continue
		}
		if in {
			classify.Evidence = append(classify.Evidence, fmt.Sprintf("%s is in @%s", ip, set))
			domestic = true
			break
		}
		classify.Evidence = append(classify.Evidence, fmt.Sprintf("%s is not in @%s", ip, set))
	}

	group := netmark.GroupForeign
	if domestic {
		group = netmark.GroupDomestic
	} else {
		classify.Evidence = append(classify.Evidence,
			"no set matched, so the foreign catch-all applies")
	}
	mark := netmark.GroupMark(group)
	return mark, []TraceStep{classify, {
		Title: "Mark", Verdict: "ok",
		Evidence: []string{fmt.Sprintf("chain mangle_pre sets mark %s", netmark.Hex(mark))},
	}}
}

// walkPolicy replays the kernel's rule walk for a forwarded packet: oif rules
// never match (no oif exists before the route is chosen), stock rules are
// skipped, a blackhole ends it, and a suppressed match lets the walk continue.
func walkPolicy(rules []system.Rule, routes map[int][]system.Route, dst string, mark uint32) (
	TraceStep, *system.Route, *system.Rule) {

	sorted := append([]system.Rule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Pref < sorted[j].Pref })

	step := TraceStep{Title: "Policy walk", Verdict: "ok"}
	for i := range sorted {
		r := sorted[i]
		if r.OifName != "" {
			continue // socket-bound; a forwarded packet has no oif yet
		}
		if isStockRule(r.Pref) {
			continue
		}
		if r.FwMask != 0 && (mark&r.FwMask) != r.FwMark {
			continue
		}
		if r.Blackhole {
			step.Evidence = append(step.Evidence, fmt.Sprintf(
				"pref %d: blackhole — matched fwmark 0x%x/0x%x", r.Pref, r.FwMark, r.FwMask))
			step.Verdict = "drop"
			return step, nil, &r
		}
		best, plen, ok := lookupTable(routes[r.Table], dst)
		if !ok {
			step.Evidence = append(step.Evidence, fmt.Sprintf(
				"pref %d: lookup table %d — no route, the walk continues", r.Pref, r.Table))
			continue
		}
		if r.SuppressSet && plen <= r.SuppressPrefixLen {
			step.Evidence = append(step.Evidence, fmt.Sprintf(
				"pref %d: table %d matched /%d but suppress_prefixlength %d suppresses it",
				r.Pref, r.Table, plen, r.SuppressPrefixLen))
			continue
		}
		step.Evidence = append(step.Evidence, fmt.Sprintf(
			"pref %d: table %d → %s dev %s", r.Pref, r.Table, best.Dest, best.OifName))
		return step, best, &r
	}
	step.Evidence = append(step.Evidence, "no rule produced a route")
	step.Verdict = "drop"
	return step, nil, nil
}

// lookupTable is longest-prefix match; "default" is 0.0.0.0/0.
func lookupTable(routes []system.Route, dst string) (*system.Route, int, bool) {
	ip := net.ParseIP(dst)
	if ip == nil {
		return nil, 0, false
	}
	best, bestLen := -1, -1
	for i, r := range routes {
		cidr := r.Dest
		if cidr == "" || cidr == "default" {
			cidr = "0.0.0.0/0"
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil || !n.Contains(ip) {
			continue
		}
		if l, _ := n.Mask.Size(); l > bestLen {
			best, bestLen = i, l
		}
	}
	if best < 0 {
		return nil, 0, false
	}
	return &routes[best], bestLen, true
}

// traceKernelCheck asks the kernel the same question. The kernel is the
// authority: a disagreement means the walk is wrong, and that is worth saying.
func (u *networkUsecase) traceKernelCheck(ctx context.Context, ip string, mark uint32,
	route *system.Route) TraceStep {

	step := TraceStep{Title: "Kernel check", Verdict: "info"}
	got, err := u.Backend.RouteGet(ctx, ip, mark)
	if err != nil {
		step.Evidence = append(step.Evidence, "ip route get: "+err.Error())
		if route != nil {
			step.Verdict = "warn"
			step.Evidence = append(step.Evidence,
				"the walk found a route but the kernel did not")
		} else {
			step.Evidence = append(step.Evidence, "which confirms nothing routes here")
		}
		return step
	}
	step.Evidence = append(step.Evidence, fmt.Sprintf(
		"ip route get %s mark 0x%x → dev %s table %d", ip, mark, got.OifName, got.Table))
	switch {
	case route == nil:
		// The dangerous direction: we are about to call this dropped.
		step.Verdict = "warn"
		step.Evidence = append(step.Evidence,
			"the walk found nothing but the kernel routes it — trust the kernel")
	case got.OifName != "" && got.OifName != route.OifName:
		step.Verdict = "warn"
		step.Evidence = append(step.Evidence, fmt.Sprintf(
			"the walk said dev %s — trust the kernel and report the difference", route.OifName))
	}
	return step
}

// finishTrace turns the route into the path the graph highlights.
func (u *networkUsecase) finishTrace(ctx context.Context, v *TraceView, startNode string,
	mark uint32, route *system.Route, matched *system.Rule) {

	nodes := []string{startNode}
	switch netmark.Group(mark) {
	case netmark.GroupDomestic:
		nodes = append(nodes, "mark-domestic")
	case netmark.GroupForeign:
		nodes = append(nodes, "mark-foreign")
	}

	if route == nil {
		v.PathNodes = append(nodes, "killswitch")
		v.PathEdges = edgesFor(v.PathNodes)
		v.FinalVerdict = "dropped"
		v.Steps = append(v.Steps, blackholeStep(matched))
		return
	}

	table := 0
	if matched != nil {
		table = matched.Table
	}
	switch {
	// A multipath default only ever lives in the pool's table.
	case system.IsWGLink(route.OifName) || len(route.Nexthops) > 0:
		nodes = append(nodes, "table-203", "wg", "table-202", "uplink-secondary", "world-foreign")
		v.FinalVerdict = "delivered-vpn"
	case table == 202:
		// Out the secondary uplink in the clear: the kill switch stops this.
		nodes = append(nodes, "table-202", "killswitch")
		v.Steps = append(v.Steps, TraceStep{
			Title: "Kill switch", Verdict: "drop",
			Evidence: []string{
				fmt.Sprintf("chain killswitch_fwd drops anything leaving %s without the transport mark",
					route.OifName),
				"only the tunnel's own crypto, DHCP, the gateway and the dish are exempt",
			},
		})
		v.FinalVerdict = "dropped"
	default:
		if table > 0 {
			nodes = append(nodes, fmt.Sprintf("table-%d", table))
		}
		// By the interface the route actually names, not by assuming domestic.
		if u.isSecondaryIf(ctx, route) {
			nodes = append(nodes, "uplink-secondary", "world-foreign")
			v.FinalVerdict = "delivered-secondary"
		} else {
			nodes = append(nodes, "uplink-domestic", "world-domestic")
			v.FinalVerdict = "delivered-domestic"
		}
	}
	v.PathNodes = nodes
	v.PathEdges = edgesFor(nodes)
}

// isSecondaryIf says whether a route leaves by the secondary uplink.
func (u *networkUsecase) isSecondaryIf(ctx context.Context, route *system.Route) bool {
	if route == nil || route.OifName == "" {
		return false
	}
	ups, err := u.uplinks(ctx)
	if err != nil {
		return false
	}
	for _, up := range ups {
		if up.IfName == route.OifName {
			return up.Slot == domain.SlotSecondary
		}
	}
	return false
}

// blackholeStep ends the story where the packet does. Without it the path
// stops at the kill switch with nothing saying why.
func blackholeStep(matched *system.Rule) TraceStep {
	s := TraceStep{Title: "Dropped", Verdict: "drop"}
	if matched != nil && matched.Blackhole {
		s.Evidence = append(s.Evidence, fmt.Sprintf(
			"the blackhole rule at pref %d has the last word", matched.Pref))
	}
	s.Evidence = append(s.Evidence,
		"nothing routes this traffic: with no tunnel the foreign group has nowhere to go",
		"activate a VPN profile, or this destination stays unreachable")
	return s
}

// edgesFor names the drawn edge between each consecutive pair.
func edgesFor(nodes []string) []string {
	between := map[string]string{
		"src-lan>mark-domestic":          "e-lan-dom",
		"src-lan>mark-foreign":           "e-lan-for",
		"src-xray>mark-domestic":         "e-xray-dom",
		"src-xray>mark-foreign":          "e-xray-for",
		"src-router>table-201":           "e-router-fb",
		"src-router>table-203":           "e-router-fb",
		"mark-domestic>table-201":        "e-dom-201",
		"mark-foreign>table-203":         "e-for-203",
		"mark-foreign>killswitch":        "e-for-ks",
		"table-201>uplink-domestic":      "e-201-updom",
		"table-203>wg":                   "e-203-wg",
		"wg>table-202":                   "e-wg-202",
		"table-202>uplink-secondary":     "e-202-upsec",
		"uplink-domestic>world-domestic": "e-updom-world",
		"uplink-secondary>world-foreign": "e-upsec-world",
	}
	var out []string
	for i := 0; i+1 < len(nodes); i++ {
		if id, ok := between[nodes[i]+">"+nodes[i+1]]; ok {
			out = append(out, id)
		}
	}
	return out
}

func validTraceHost(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	for _, r := range h {
		switch {
		case r == '.' || r == '-' || r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
