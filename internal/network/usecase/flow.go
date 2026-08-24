package usecase

// The flow page's graph: what the kernel is doing right now, drawn honestly.
// Ghost nodes stay in the picture so "why is X missing" answers itself.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/dohboot"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// FlowDetailSection is one block of raw kernel truth behind a node.
type FlowDetailSection struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

type FlowNode struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Label    string              `json:"label"`
	Sublabel string              `json:"sublabel,omitempty"`
	Status   string              `json:"status"`
	Hint     string              `json:"hint,omitempty"`
	Detail   []FlowDetailSection `json:"detail,omitempty"`
}

type FlowEdge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Label      string `json:"label,omitempty"`
	CounterKey string `json:"counter_key,omitempty"`
}

// FlowCounter is cumulative, never a rate: the page diffs consecutive polls so
// the backend can stay stateless and serve many viewers.
type FlowCounter struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	Packets uint64 `json:"packets,omitempty"`
}

type FlowMismatch struct {
	NodeID   string `json:"node_id"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

type FlowView struct {
	GeneratedUnix int64                  `json:"generated_unix"`
	Nodes         []FlowNode             `json:"nodes"`
	Edges         []FlowEdge             `json:"edges"`
	Mismatches    []FlowMismatch         `json:"mismatches"`
	Counters      map[string]FlowCounter `json:"counters"`
}

// flowState is everything the node builders read, gathered once.
type flowState struct {
	lan       *domain.LANConfig
	pool      vpnPool
	uplinks   []Uplink
	domestic  *Uplink
	secondary *Uplink
	// Every secondary, in slot order. secondary is the first of these.
	secondaries []Uplink
	healthy     map[string]bool
	routes      map[int][]system.Route
	routeErrs   map[int]error
	wgStatus    map[string]*system.WGStatus
	wgLinks     []string
	nftText     string
	nftObj      *system.NftObjects
	dnsUp       bool
}

func (u *networkUsecase) FlowGraph(ctx context.Context) (*FlowView, error) {
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, err
	}
	lan := u.lanConfig(ctx)
	pool := u.vpnPoolNow(ctx)
	vpn := VPNRouteState{IfNames: pool.IfNames()}

	st := flowState{
		lan: lan, pool: pool, uplinks: uplinks,
		healthy:   u.healthyKeys(ctx),
		routes:    map[int][]system.Route{},
		routeErrs: map[int]error{},
		wgStatus:  map[string]*system.WGStatus{},
	}
	for i := range uplinks {
		switch uplinks[i].Slot {
		case domain.SlotDomestic:
			st.domestic = &uplinks[i]
		default:
			if uplinks[i].Slot.IsSecondary() {
				st.secondaries = append(st.secondaries, uplinks[i])
			}
		}
	}
	sort.Slice(st.secondaries, func(a, b int) bool {
		return st.secondaries[a].UplinkIndex < st.secondaries[b].UplinkIndex
	})
	if len(st.secondaries) > 0 {
		st.secondary = &st.secondaries[0]
	}

	// Live reads. Each may fail without killing the page: a broken read IS a
	// finding, so it lands in Mismatches rather than a 500.
	liveRules, rulesErr := u.Backend.RuleList(ctx)
	tables := []int{201, 202, system.WGTable}
	for _, s := range st.secondaries {
		if s.Table != 202 {
			tables = append(tables, s.Table)
		}
		tables = append(tables, vpnViaTableFor(s.UplinkIndex))
	}
	for _, t := range tables {
		rs, rerr := u.Backend.RouteList(ctx, t)
		if rerr != nil {
			st.routeErrs[t] = rerr
			continue
		}
		st.routes[t] = rs
	}
	nftObj, nftErr := u.nftReader().LiveObjects(ctx)
	st.nftObj = nftObj
	st.nftText, _ = u.nftReader().ListRuleset(ctx)
	stats, _ := u.flowSource().LinkStats(ctx)
	for _, t := range pool.Tunnels {
		if s, err := u.wg().Status(ctx, t.IfName); err == nil {
			st.wgStatus[t.IfName] = s
		}
	}
	st.wgLinks, _ = u.wg().List(ctx)
	// Both are subprocesses and this page polls every 3s, so probe once.
	st.dnsUp = u.dnsmasqStatus(ctx).Running

	view := &FlowView{
		GeneratedUnix: time.Now().Unix(),
		Counters:      map[string]FlowCounter{},
	}
	for _, up := range uplinks {
		if s, ok := stats[up.IfName]; ok {
			view.Counters["if:"+up.IfName] = FlowCounter{RxBytes: s.RxBytes, TxBytes: s.TxBytes}
		}
	}
	var wgRx, wgTx uint64
	for _, s := range st.wgStatus {
		wgRx += uint64(s.RxBytes)
		wgTx += uint64(s.TxBytes)
	}
	if len(st.wgStatus) > 0 {
		view.Counters["wg"] = FlowCounter{RxBytes: wgRx, TxBytes: wgTx}
	}
	if nftObj != nil {
		for key, name := range map[string]string{
			"nft:domestic":   nft.CounterDomestic,
			"nft:foreign":    nft.CounterForeign,
			"nft:killswitch": nft.CounterKillSwitch,
		} {
			if c, ok := nftObj.Counters[name]; ok {
				view.Counters[key] = FlowCounter{TxBytes: c.Bytes, Packets: c.Packets}
			}
		}
	}

	view.Nodes = u.flowNodes(ctx, st)
	view.Edges = flowEdges(vpn, st)
	view.Mismatches = u.flowMismatches(ctx, flowMismatchInput{
		uplinks: uplinks, vpn: vpn, pool: pool,
		liveRules: liveRules, rulesErr: rulesErr,
		routes: st.routes, routeErrs: st.routeErrs,
		nftObj: nftObj, nftErr: nftErr, lan: lan,
		wgStatus: st.wgStatus, wgLinks: st.wgLinks, dnsUp: st.dnsUp,
	})
	return view, nil
}

func (u *networkUsecase) flowNodes(ctx context.Context, st flowState) []FlowNode {
	return []FlowNode{
		u.lanNode(ctx, st),
		u.xrayNode(ctx),
		routerNode(st),
		u.markNode(st, netmark.GroupDomestic),
		u.markNode(st, netmark.GroupForeign),
		tableNode(st, 201, "nasnet-domestic", ""),
		tableNode(st, system.WGTable, system.WGTableName, ""),
		tableNode(st, 202, "nasnet-secondary", "transport only"),
		u.wgNode(st),
		killSwitchNode(st),
		u.uplinkNode(st, domain.SlotDomestic),
		u.uplinkNode(st, domain.SlotSecondary),
		worldNode(st, domain.SlotDomestic),
		worldNode(st, domain.SlotSecondary),
		u.dnsNode(ctx, st),
	}
}

func (u *networkUsecase) lanNode(ctx context.Context, st flowState) FlowNode {
	n := FlowNode{ID: "src-lan", Kind: "source", Label: "LAN clients"}
	if st.lan == nil || !st.lan.Enabled {
		n.Status = "ghost"
		n.Hint = "The local network is off — no clients to route."
		return n
	}
	n.Status = "ok"
	n.Sublabel = st.lan.BridgeName

	detail := []string{
		"bridge: " + st.lan.BridgeName,
		"address: " + st.lan.CIDR,
		fmt.Sprintf("dhcp: %s – %s (%dh lease)", st.lan.DHCPRangeLow, st.lan.DHCPRangeHigh, st.lan.LeaseHours),
	}
	if list, err := u.ListDevices(ctx); err == nil && list != nil {
		online := 0
		lines := make([]string, 0, len(list.Devices))
		for _, d := range list.Devices {
			if d.Online {
				online++
			}
			name := d.Label
			if name == "" {
				name = d.Hostname
			}
			ip := ""
			if len(d.IPs) > 0 {
				ip = d.IPs[0]
			}
			if len(lines) < 30 {
				lines = append(lines, strings.TrimSpace(fmt.Sprintf("%s  %s  %s", d.MAC, ip, name)))
			}
		}
		n.Sublabel = fmt.Sprintf("%d online", online)
		if len(lines) > 0 {
			return withDetail(n, section("Bridge", detail), section("Devices", lines))
		}
	}
	return withDetail(n, section("Bridge", detail))
}

func (u *networkUsecase) xrayNode(ctx context.Context) FlowNode {
	n := FlowNode{ID: "src-xray", Kind: "source", Label: "xray clients"}
	outbounds := []string{
		"direct-foreign  → mark " + netmark.Hex(netmark.GroupMark(netmark.GroupForeign)),
		"direct-domestic → mark " + netmark.Hex(netmark.GroupMark(netmark.GroupDomestic)),
	}
	if u.Inbounds == nil {
		n.Status = "ghost"
		n.Hint = "No inbound source configured."
		return withDetail(n, section("Managed outbounds", outbounds))
	}
	rows, err := u.Inbounds.EnabledInbounds(ctx)
	if err != nil || len(rows) == 0 {
		n.Status = "ghost"
		n.Hint = "No xray inbounds — nothing arrives this way."
		return withDetail(n, section("Managed outbounds", outbounds))
	}
	n.Status = "ok"
	n.Sublabel = fmt.Sprintf("%d inbounds", len(rows))
	lines := make([]string, 0, len(rows))
	for _, in := range rows {
		lines = append(lines, fmt.Sprintf("%s  %s/%d", in.Tag, in.Proto, in.Port))
	}
	return withDetail(n, section("Inbounds", lines), section("Managed outbounds", outbounds))
}

func routerNode(st flowState) FlowNode {
	n := FlowNode{ID: "src-router", Kind: "source", Label: "the router itself", Status: "ok",
		Sublabel: "unmarked"}
	lines := []string{}
	if st.pool.Active() {
		lines = append(lines, fmt.Sprintf("pref %d: lookup %d (tunnel)", RulePrefFallbackBase, system.WGTable))
		lines = append(lines, fmt.Sprintf("pref %d: lookup 201 (domestic)", RulePrefFallbackBase+1))
	} else {
		lines = append(lines, fmt.Sprintf("pref %d: lookup 201 (domestic)", RulePrefFallbackBase))
	}
	lines = append(lines, fmt.Sprintf("pref %d: blackhole", RulePrefFallbackBlackhole))
	return withDetail(n, section("Fallback for unmarked traffic", lines))
}

func (u *networkUsecase) markNode(st flowState, group uint32) FlowNode {
	n := FlowNode{Kind: "classify"}
	if group == netmark.GroupDomestic {
		n.ID, n.Label = "mark-domestic", "domestic"
	} else {
		n.ID, n.Label = "mark-foreign", "foreign"
	}
	n.Sublabel = "mark " + netmark.Hex(netmark.GroupMark(group))

	switch {
	case st.nftObj == nil:
		n.Status = "warn"
		n.Hint = "Could not read the live firewall."
	case !contains(st.nftObj.Chains, "mangle_pre"):
		n.Status = "down"
		n.Hint = "The classification chain is missing — nothing is being marked."
	default:
		n.Status = "ok"
	}
	if lines := extractChain(st.nftText, "mangle_pre"); len(lines) > 0 {
		return withDetail(n, section("chain mangle_pre", lines))
	}
	return n
}

func tableNode(st flowState, table int, name, sublabel string) FlowNode {
	n := FlowNode{
		ID:       fmt.Sprintf("table-%d", table),
		Kind:     "route",
		Label:    fmt.Sprintf("table %d", table),
		Sublabel: name,
	}
	if sublabel != "" {
		n.Sublabel = name + " · " + sublabel
	}
	routes := st.routes[table]
	if err := st.routeErrs[table]; err != nil {
		n.Status = "unknown"
		n.Hint = "Could not read this table: " + err.Error()
		return n
	}

	if table == system.WGTable && !st.pool.Active() {
		n.Status = "ghost"
		n.Hint = "No VPN is active — nothing routes here."
		return withDetail(n, section("routes", routeLines(routes)))
	}
	present, want := hasDefault(routes), "No default route in this table."
	if table == system.WGTable {
		present, want = hasPoolDefault(routes), "No weighted default across the pool members."
	}
	if present {
		n.Status = "ok"
	} else {
		n.Status = "down"
		n.Hint = want
	}
	return withDetail(n, section("routes", routeLines(routes)))
}

func (u *networkUsecase) wgNode(st flowState) FlowNode {
	n := FlowNode{ID: "wg", Kind: "tunnel", Label: "VPN pool"}
	if !st.pool.Active() {
		n.Status = "ghost"
		n.Hint = "No enabled VPN — turn a profile on in the VPN tab."
		return n
	}
	n.Sublabel = fmt.Sprintf("%d tunnels", len(st.pool.Tunnels))
	if len(st.pool.Tunnels) == 1 {
		n.Sublabel = st.pool.Tunnels[0].Profile.Name
	}

	worst := "ok"
	var details []FlowDetailSection
	for _, t := range st.pool.Tunnels {
		lines := []string{
			"endpoint: " + t.Config.Peer.Endpoint,
			"address: " + t.Config.Address,
			fmt.Sprintf("tier %d · weight %d", t.Profile.Priority, t.Profile.Weight),
		}
		status := st.wgStatus[t.IfName]
		switch {
		case status == nil:
			worst = "down"
			n.Hint = fmt.Sprintf("%s is not present.", t.IfName)
			lines = append(lines, "interface missing")
		case !status.Connected():
			if worst == "ok" {
				worst = "warn"
				n.Hint = fmt.Sprintf("%s has no recent handshake.", t.IfName)
			}
			lines = append(lines, "no recent handshake")
		default:
			lines = append(lines, fmt.Sprintf("transfer: %d rx / %d tx bytes",
				status.RxBytes, status.TxBytes))
		}
		if status != nil && !status.LastHandshake.IsZero() {
			lines = append(lines, fmt.Sprintf("last handshake: %ds ago",
				int(time.Since(status.LastHandshake).Seconds())))
		}
		u.healthMu.Lock()
		if _, ok := u.ladders[t.IfName]; ok {
			samples := u.rings[t.IfName].snapshot()
			lines = append(lines, fmt.Sprintf("probe: %d%% loss, median %dms",
				lossPct(samples, 20), medianRTT(samples, 20)))
		}
		u.healthMu.Unlock()
		details = append(details, section(t.IfName+" — "+t.Profile.Name, lines))
	}
	n.Status = worst
	deal := assignTransport(st.pool, secondariesOf(st.uplinks), u.healthySecondaries(st.uplinks))
	var marks []string
	for _, t := range st.pool.Tunnels {
		wan := deal[t.IfName]
		marks = append(marks, fmt.Sprintf("%s rides %s, mark %s",
			t.IfName, wan.IfName, netmark.Hex(transportMark(wan))))
	}
	details = append(details, section("pool", marks))
	return withDetail(n, details...)
}

func killSwitchNode(st flowState) FlowNode {
	n := FlowNode{ID: "killswitch", Kind: "drop", Label: "kill switch"}
	if st.secondary == nil {
		n.Status = "ghost"
		n.Hint = "No secondary uplink — nothing to guard."
		return n
	}
	n.Sublabel = "always armed"
	if st.pool.Active() {
		n.Status = "ok"
	} else {
		n.Status = "warn"
		n.Hint = "Foreign traffic dies here while the VPN is down."
	}
	details := []FlowDetailSection{}
	for _, chain := range []string{"killswitch_out", "killswitch_fwd"} {
		if lines := extractChain(st.nftText, chain); len(lines) > 0 {
			details = append(details, section("chain "+chain, lines))
		}
	}
	if st.nftObj != nil {
		if c, ok := st.nftObj.Counters[nft.CounterKillSwitch]; ok {
			details = append(details, section("dropped",
				[]string{fmt.Sprintf("%d packets, %d bytes", c.Packets, c.Bytes)}))
		}
	}
	return withDetail(n, details...)
}

func (u *networkUsecase) uplinkNode(st flowState, slot domain.UplinkSlot) FlowNode {
	n := FlowNode{Kind: "uplink"}
	up := st.domestic
	if slot == domain.SlotDomestic {
		n.ID, n.Label = "uplink-domestic", "domestic WAN"
	} else {
		n.ID, n.Label = "uplink-secondary", "secondary WAN"
		up = st.secondary
	}
	if up == nil {
		n.Status = "ghost"
		n.Hint = fmt.Sprintf("No %s uplink assigned — assign one on the Ports tab.", slot)
		return n
	}
	n.Sublabel = up.IfName

	u.healthMu.Lock()
	l := u.ladders[up.IfName]
	failover := u.failoverActive
	u.healthMu.Unlock()

	switch l.Verdict {
	case "":
		// No tick yet; fall back to the persisted flag.
		if st.healthy[up.Key] {
			n.Status = "ok"
		} else {
			n.Status = "down"
			n.Hint = "The health probe cannot reach this uplink's gateway."
		}
	case "up":
		n.Status = "ok"
	case "degraded":
		n.Status = "warn"
		n.Hint = "Reachable but lossy or slow — see the health strip."
	case "no-internet":
		n.Status = "down"
		n.Hint = "Gateway answers but nothing past it does — the ISP's upstream looks dead."
	case "no-gateway":
		n.Status = "down"
		n.Hint = "The health probe cannot reach this uplink's gateway."
	case "no-carrier":
		n.Status = "down"
		n.Hint = "No signal on the wire."
	case "forced-down":
		n.Status = "down"
		n.Hint = "Held down by the operator."
	case "forced-up":
		n.Status = "warn"
		n.Hint = "Held up by the operator; the probe is not consulted."
	default:
		n.Status = "down"
	}
	if failover && slot == domain.SlotDomestic {
		n.Status = "warn"
		n.Hint = "Domestic internet is down — traffic is riding the tunnel until it recovers."
	}

	lines := []string{
		"interface: " + up.IfName,
		fmt.Sprintf("table: %d", up.Table),
		fmt.Sprintf("pin mark: %s", netmark.Hex(netmark.PinMark(up.UplinkIndex))),
	}
	// One node stands for every secondary, so name the rest here rather than
	// letting the page imply this box has one.
	for _, s := range st.secondaries {
		if s.IfName == up.IfName {
			continue
		}
		lines = append(lines, fmt.Sprintf("also: %s, table %d, pin mark %s",
			s.IfName, s.Table, netmark.Hex(netmark.PinMark(s.UplinkIndex))))
	}
	var health []string
	for _, r := range l.Results {
		state := "unreachable"
		if r.OK {
			state = fmt.Sprintf("%dms", r.RTT.Milliseconds())
		}
		health = append(health, fmt.Sprintf("%s %s: %s", r.Target.Proto, r.Target.Address, state))
	}
	return withDetail(n, section("uplink", lines), section("health", health),
		section("routes", routeLines(st.routes[up.Table])))
}

func worldNode(st flowState, slot domain.UplinkSlot) FlowNode {
	n := FlowNode{Kind: "world"}
	up := st.domestic
	if slot == domain.SlotDomestic {
		n.ID, n.Label = "world-domestic", "domestic sites"
	} else {
		n.ID, n.Label = "world-foreign", "foreign sites"
		up = st.secondary
		// A live uplink is not reachability: with no pool the kill switch
		// stops every foreign packet, so saying "ok" here would be a lie.
		if !st.pool.Active() {
			n.Status = "ghost"
			n.Hint = "Unreachable while the VPN is down — the kill switch drops this traffic."
			return n
		}
	}
	switch {
	case up == nil:
		n.Status = "ghost"
	case st.healthy[up.Key]:
		n.Status = "ok"
	default:
		n.Status = "down"
	}
	return n
}

func (u *networkUsecase) dnsNode(ctx context.Context, st flowState) FlowNode {
	n := FlowNode{ID: "dns", Kind: "dns", Label: "DNS", Sublabel: "split resolver"}
	if st.lan == nil || !st.lan.Enabled {
		n.Status = "ghost"
		n.Hint = "The resolver only runs with the local network on."
		return n
	}
	if st.dnsUp {
		n.Status = "ok"
	} else {
		n.Status = "down"
		n.Hint = "dnsmasq is not running — clients cannot resolve anything."
	}
	fdns := poolForeignDNS(st.pool)
	lines := []string{
		fmt.Sprintf("domestic: %s via %s (%s)", system.DefaultDomesticDNS,
			ifNameOr(st.domestic, "no uplink"), DomesticSuffix),
	}
	if len(fdns) == 0 {
		lines = append(lines, "foreign: none — no tunnel to send queries through")
	}
	for _, f := range fdns {
		lines = append(lines, fmt.Sprintf("foreign: %s via %s", f.Server, f.IfName))
	}
	lines = append(lines, "DoH bootstrap: "+strings.Join(dohboot.BootstrapIPs(), ", "))
	return withDetail(n, section("resolvers", lines))
}

// flowEdges wires the fixed topology. Statuses say what is live, not what is
// configured: a ghosted edge is a path packets cannot take right now.
func flowEdges(vpn VPNRouteState, st flowState) []FlowEdge {
	lanOK := st.lan != nil && st.lan.Enabled
	ghostIf := func(ok bool) string {
		if ok {
			return "ok"
		}
		return "ghost"
	}
	vpnStatus := ghostIf(vpn.Active())
	edges := []FlowEdge{
		{ID: "e-lan-dom", From: "src-lan", To: "mark-domestic", Kind: "data", Status: ghostIf(lanOK)},
		{ID: "e-lan-for", From: "src-lan", To: "mark-foreign", Kind: "data", Status: ghostIf(lanOK)},
		{ID: "e-xray-dom", From: "src-xray", To: "mark-domestic", Kind: "data", Status: "ok"},
		{ID: "e-xray-for", From: "src-xray", To: "mark-foreign", Kind: "data", Status: "ok"},
		{ID: "e-dom-201", From: "mark-domestic", To: "table-201", Kind: "data", Status: "ok"},
		{ID: "e-for-203", From: "mark-foreign", To: "table-203", Kind: "data", Status: vpnStatus},
		// Where foreign traffic dies with no tunnel. Live exactly when the VPN is not.
		{ID: "e-for-ks", From: "mark-foreign", To: "killswitch", Kind: "drop", Status: ghostIf(!vpn.Active())},
		{ID: "e-201-updom", From: "table-201", To: "uplink-domestic", Kind: "data",
			Status: "ok", CounterKey: "nft:domestic"},
		{ID: "e-203-wg", From: "table-203", To: "wg", Kind: "data",
			Status: vpnStatus, CounterKey: "nft:foreign"},
		{ID: "e-wg-202", From: "wg", To: "table-202", Kind: "transport", Status: vpnStatus,
			Label: "encrypted", CounterKey: "wg"},
		{ID: "e-202-upsec", From: "table-202", To: "uplink-secondary", Kind: "transport", Status: vpnStatus},
		{ID: "e-updom-world", From: "uplink-domestic", To: "world-domestic", Kind: "data", Status: "ok"},
		{ID: "e-upsec-world", From: "uplink-secondary", To: "world-foreign", Kind: "data", Status: vpnStatus},
	}
	setCounter := func(id, key string) {
		for i := range edges {
			if edges[i].ID == id {
				edges[i].CounterKey = key
				return
			}
		}
	}
	if st.domestic != nil {
		setCounter("e-updom-world", "if:"+st.domestic.IfName)
	}
	if st.secondary != nil {
		setCounter("e-upsec-world", "if:"+st.secondary.IfName)
	}
	if st.pool.Active() {
		edges = append(edges, FlowEdge{ID: "e-router-fb", From: "src-router", To: "table-203",
			Kind: "data", Status: "ok", Label: "fallback"})
	} else {
		edges = append(edges, FlowEdge{ID: "e-router-fb", From: "src-router", To: "table-201",
			Kind: "data", Status: "ok", Label: "fallback"})
	}
	edges = append(edges,
		FlowEdge{ID: "e-dns-lan", From: "src-lan", To: "dns", Kind: "dns", Status: ghostIf(lanOK)},
		FlowEdge{ID: "e-dns-updom", From: "dns", To: "uplink-domestic", Kind: "dns",
			Status: ghostIf(lanOK), Label: DomesticSuffix},
		FlowEdge{ID: "e-dns-wg", From: "dns", To: "wg", Kind: "dns",
			Status: vpnStatus, Label: "foreign queries"},
		FlowEdge{ID: "e-dns-doh", From: "dns", To: "uplink-secondary", Kind: "dns",
			Status: ghostIf(st.secondary != nil), Label: "DoH bootstrap"},
	)
	return edges
}

func section(title string, lines []string) FlowDetailSection {
	return FlowDetailSection{Title: title, Lines: lines}
}

func withDetail(n FlowNode, s ...FlowDetailSection) FlowNode {
	for _, sec := range s {
		if len(sec.Lines) > 0 {
			n.Detail = append(n.Detail, sec)
		}
	}
	return n
}

func routeLines(routes []system.Route) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		line := r.Dest
		if r.Gateway != "" {
			line += " via " + r.Gateway
		}
		if r.OifName != "" {
			line += " dev " + r.OifName
		}
		if r.Scope != "" {
			line += " scope " + r.Scope
		}
		for _, nh := range r.Nexthops {
			line += fmt.Sprintf(" nexthop %s weight %d", nh.OifName, nh.Weight)
		}
		if r.Metric != 0 {
			line += fmt.Sprintf(" metric %d", r.Metric)
		}
		out = append(out, line)
	}
	return out
}

func hasDefault(routes []system.Route) bool {
	for _, r := range routes {
		if r.Dest == "default" {
			return true
		}
	}
	return false
}

// The pool's escape hatches are defaults too, so hasDefault would report the
// weighted one present when only they survive. Metric zero is the real thing.
func hasPoolDefault(routes []system.Route) bool {
	for _, r := range routes {
		if r.Dest == "default" && r.Metric == 0 {
			return true
		}
	}
	return false
}

// extractChain pulls one chain's body out of `nft list table` output, so a
// detail panel can show the rules the kernel really holds.
func extractChain(text, name string) []string {
	var out []string
	inChain := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inChain {
			if strings.HasPrefix(trimmed, "chain "+name+" ") || trimmed == "chain "+name+" {" {
				inChain = true
			}
			continue
		}
		if trimmed == "}" {
			break
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func ifNameOr(u *Uplink, fallback string) string {
	if u == nil {
		return fallback
	}
	return u.IfName
}
