package nft

// Package nft owns `table inet nasnet` — the entire firewall and mangle surface this panel installs.

import (
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

const (
	TableFamily = "inet"
	TableName   = "nasnet"
)

// Pin records that flows arriving on IfName belong to uplink Index, so their
// replies leave by the same uplink. Index is an uplink index.
type Pin struct {
	IfName string
	Index  uint32
}

// Set is a named nftables set. Interval sets hold geoip CIDRs
type Set struct {
	Name     string
	Family   string // "ipv4_addr" | "ipv6_addr"
	Interval bool
	Timeout  string // nftables duration
	Elements []string
}

// LANClassify marks forwarded LAN traffic by destination
type LANClassify struct {
	BridgeName    string
	DomesticV4Set string
	DomesticV6Set string
	DomainV4Set   string
	DomainV6Set   string
}

// FilterForward is LAN isolation
type FilterForward struct {
	BridgeName  string
	UplinkNames []string
}

// PortForward is one rendered DNAT rule
type PortForward struct {
	IfName  string
	Proto   string
	DPort   int
	ToAddr  string
	ToPort  int
	Comment string
}

// InputAccept is one rendered accept
type InputAccept struct {
	IfNames []string
	Proto   string
	Port    int
	Comment string
}

// FilterInput is the box's own exposure. Policy drop.
type FilterInput struct {
	LocalIfNames []string
	Accepts      []InputAccept
}

// Resolvers the kill switch lets through, so an endpoint hostname can be
// resolved before the tunnel exists.
const SetDoHBootstrap = "doh_bootstrap"

// Targets the health probe may reach past the kill switch.
const SetProbe = "probe_v4"

// Named counters the flow page reads. Declared as a block so a chain
// referencing one can never abort the transaction.
const (
	CounterDomestic   = "cnt_domestic"
	CounterForeign    = "cnt_foreign"
	CounterKillSwitch = "cnt_killswitch"
)

// KillSwitch drops everything leaving the secondary uplink in the clear.
// Rendered whenever a secondary uplink exists, tunnel or not, and unlike the
// input firewall it is not a setting.
type KillSwitch struct {
	SecondaryIfName string
	// The health probe has to reach the gateway or the uplink reads as down
	// forever. Empty before the first lease.
	GatewayIP string
	// Keeps the Starlink dish's own API reachable. Empty omits it.
	DishSubnet string
	// Match the tunnel's transport, the one thing allowed out in the clear.
	MarkMask  uint32
	MarkValue uint32
	// The DoH resolvers. Empty renders no exemption at all.
	BootstrapIPs []string
	// The health probe's own traffic. Empty renders no exemption.
	ProbeMark uint32
	ProbeIPs  []string
}

// Ruleset is the complete desired state of the owned table. Zero value renders
// an empty table, which is a valid "everything off" state
type Ruleset struct {
	// Connmark enables the masked save/restore pair that download shaping,
	// forwarded reply-path pinning and the DNAT ingress pin all depend on.
	Connmark bool

	// Counters enables the named traffic counters the flow page reads.
	Counters bool

	// IngressPins stamp the arrival uplink into ct mark. Requires Connmark.
	IngressPins []Pin

	// Sets are the named sets the classification rules reference
	Sets []Set
	// LANClassify marks forwarded LAN traffic by destination.
	LANClassify *LANClassify
	// Masquerade lists the uplink kernel names to source-NAT on egress.
	Masquerade []string
	// PortForwards render the nat_pre chain.
	PortForwards []PortForward
	// FilterForward is LAN isolation.
	FilterForward *FilterForward
	// FilterInput is the box's own exposure.
	FilterInput *FilterInput
	// KillSwitch stops anything leaving the secondary uplink in the clear.
	KillSwitch *KillSwitch
}

// IsZero reports whether this renders an empty table
func (r Ruleset) IsZero() bool {
	return !r.Connmark && !r.Counters && len(r.IngressPins) == 0 && len(r.Sets) == 0 &&
		r.LANClassify == nil && len(r.Masquerade) == 0 && len(r.PortForwards) == 0 &&
		r.FilterForward == nil && r.FilterInput == nil && r.KillSwitch == nil
}

// Render returns a complete `nft -f -` script that atomically replaces the owned
// table, chains in the order a forwarded packet meets them.
func (r Ruleset) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "table %s %s\n", TableFamily, TableName)
	fmt.Fprintf(&b, "delete table %s %s\n", TableFamily, TableName)
	fmt.Fprintf(&b, "table %s %s {\n", TableFamily, TableName)

	sections := []string{
		r.renderSets(),
		r.renderCountersDecl(),
		r.renderManglePre(),
		r.renderNatPre(),
		r.renderFilterInput(),
		r.renderFilterForward(),
		r.renderKillSwitch(),
		r.renderManglePostSection(),
		r.renderNatPost(),
	}
	first := true
	for _, s := range sections {
		if s == "" {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		b.WriteString(s)
		first = false
	}

	b.WriteString("}\n")
	return b.String()
}

// Pins stamp the ct mark, so they imply the save/restore pair
func (r Ruleset) connmark() bool { return r.Connmark || len(r.IngressPins) > 0 }

func (r Ruleset) renderSets() string {
	sets := r.Sets
	// Carries its own set: a chain referencing a missing one aborts the whole
	// transaction.
	if k := r.KillSwitch; k != nil && len(k.BootstrapIPs) > 0 {
		sets = append(append([]Set(nil), sets...), Set{
			Name:     SetDoHBootstrap,
			Family:   "ipv4_addr",
			Elements: k.BootstrapIPs,
		})
	}
	if k := r.KillSwitch; k != nil && k.ProbeMark != 0 && len(k.ProbeIPs) > 0 {
		sets = append(append([]Set(nil), sets...), Set{
			Name:     SetProbe,
			Family:   "ipv4_addr",
			Elements: k.ProbeIPs,
		})
	}
	if len(sets) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range sets {
		// Declared even when empty, so a rule referencing it cannot abort the
		// whole transaction.
		fmt.Fprintf(&b, "\tset %s {\n", s.Name)
		fmt.Fprintf(&b, "\t\ttype %s\n", s.Family)
		if s.Interval {
			b.WriteString("\t\tflags interval\n")
			// One overlapping prefix upstream would abort the whole load.
			b.WriteString("\t\tauto-merge\n")
		}
		if s.Timeout != "" {
			b.WriteString("\t\tflags timeout\n")
			fmt.Fprintf(&b, "\t\ttimeout %s\n", s.Timeout)
		}
		if len(s.Elements) > 0 {
			fmt.Fprintf(&b, "\t\telements = { %s }\n", strings.Join(s.Elements, ", "))
		}
		b.WriteString("\t}\n")
	}
	return b.String()
}

func (r Ruleset) renderCountersDecl() string {
	if !r.Counters {
		return ""
	}
	var b strings.Builder
	for _, n := range []string{CounterDomestic, CounterForeign, CounterKillSwitch} {
		fmt.Fprintf(&b, "\tcounter %s {\n\t}\n", n)
	}
	return b.String()
}

// priority -150: after conntrack, before dstnat, so iifname is still the real one
func (r Ruleset) renderManglePre() string {
	if !r.connmark() && r.LANClassify == nil {
		return ""
	}
	all := netmark.Hex(netmark.MaskAll)
	keep := netmark.Hex(^netmark.MaskPin) // clear the pin field, keep the rest

	var b strings.Builder
	b.WriteString("\tchain mangle_pre {\n")
	b.WriteString("\t\ttype filter hook prerouting priority mangle; policy accept;\n")

	for _, p := range r.IngressPins {
		fmt.Fprintf(&b, "\t\tiifname %q ct state new ct mark set ct mark and %s or %s\n",
			p.IfName, keep, netmark.Hex(netmark.PinMark(p.Index)))
	}
	if r.connmark() {
		fmt.Fprintf(&b, "\t\tct mark != 0x0 meta mark set ct mark and %s\n", all)
	}
	b.WriteString(r.renderLANClassify())

	b.WriteString("\t}\n")
	return b.String()
}

// renderLANClassify runs after the restore rule, so it wins over whatever
// conntrack carried.
func (r Ruleset) renderLANClassify() string {
	c := r.LANClassify
	if c == nil {
		return ""
	}
	keep := netmark.Hex(^netmark.MaskGroup)
	domestic := netmark.Hex(netmark.GroupMark(netmark.GroupDomestic))
	foreign := netmark.Hex(netmark.GroupMark(netmark.GroupForeign))

	var b strings.Builder
	b.WriteString("\n\t\t# LAN classification — kernel-only, so it survives an xray\n")
	b.WriteString("\t\t# restart. Two layers: geoip prefixes as the floor, then\n")
	b.WriteString("\t\t# dnsmasq-populated domain sets, then a foreign catch-all.\n")
	for _, m := range []struct{ set, match string }{
		{c.DomesticV4Set, "ip daddr"},
		{c.DomainV4Set, "ip daddr"},
		{c.DomesticV6Set, "ip6 daddr"},
		{c.DomainV6Set, "ip6 daddr"},
	} {
		if m.set == "" {
			continue
		}
		fmt.Fprintf(&b, "\t\tiifname %q %s @%s meta mark set mark and %s or %s\n",
			c.BridgeName, m.match, m.set, keep, domestic)
	}
	// Foreign-by-default is the fail-safe direction: an unmatched destination
	// must not leave via the domestic ISP.
	fmt.Fprintf(&b, "\t\tiifname %q meta mark and %s == 0x0 meta mark set mark and %s or %s\n",
		c.BridgeName, netmark.Hex(netmark.MaskGroup), keep, foreign)
	return b.String()
}

func (r Ruleset) renderNatPre() string {
	if len(r.PortForwards) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\tchain nat_pre {\n")
	b.WriteString("\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	b.WriteString("\n\t\t# `fib daddr type local` means \"addressed to this box\", which is\n")
	b.WriteString("\t\t# exactly what a port forward is, and it survives a DHCP renewal.\n")
	b.WriteString("\t\t# Paired with iifname it also refuses to fire on transit traffic.\n")
	for _, pf := range r.PortForwards {
		if pf.Comment != "" {
			fmt.Fprintf(&b, "\t\t# %s\n", pf.Comment)
		}
		fmt.Fprintf(&b, "\t\tfib daddr type local iifname %q %s dport %d dnat ip to %s:%d\n",
			pf.IfName, pf.Proto, pf.DPort, pf.ToAddr, pf.ToPort)
	}
	b.WriteString("\t}\n")
	return b.String()
}

func (r Ruleset) renderFilterForward() string {
	f := r.FilterForward
	if f == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\tchain filter_fwd {\n")
	b.WriteString("\t\ttype filter hook forward priority filter; policy drop;\n")
	b.WriteString("\n\t\t# LAN isolation. Closes the hole where any host on the domestic\n")
	b.WriteString("\t\t# segment can add a route and reach the LAN directly — DNAT is not\n")
	b.WriteString("\t\t# the only way in.\n")
	b.WriteString("\t\tct state established,related accept\n")
	fmt.Fprintf(&b, "\t\tiifname %q oifname { %s } accept\n", f.BridgeName, quoteList(f.UplinkNames))
	b.WriteString("\t\tct status dnat accept\n")
	b.WriteString("\t}\n")
	return b.String()
}

func (r Ruleset) renderFilterInput() string {
	f := r.FilterInput
	if f == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\tchain filter_in {\n")
	b.WriteString("\t\ttype filter hook input priority filter; policy drop;\n")
	b.WriteString("\n\t\tct state established,related accept\n")
	if len(f.LocalIfNames) > 0 {
		fmt.Fprintf(&b, "\t\tiifname { %s } accept\n", quoteList(f.LocalIfNames))
	}
	b.WriteString("\t\ticmp type { echo-request, destination-unreachable, time-exceeded } \\\n")
	b.WriteString("\t\t\tlimit rate 10/second accept\n")
	b.WriteString("\n\t\t# Every accept below is RENDERED from the same rows that generate\n")
	b.WriteString("\t\t# the xray config and the port forwards. A hand-maintained port list\n")
	b.WriteString("\t\t# would silently kill somebody's VPN.\n")
	for _, a := range f.Accepts {
		if a.Comment != "" {
			fmt.Fprintf(&b, "\t\t# %s\n", a.Comment)
		}
		if len(a.IfNames) == 1 {
			fmt.Fprintf(&b, "\t\tiifname %q %s dport %d accept\n", a.IfNames[0], a.Proto, a.Port)
			continue
		}
		fmt.Fprintf(&b, "\t\tiifname { %s } %s dport %d accept\n",
			quoteList(a.IfNames), a.Proto, a.Port)
	}
	b.WriteString("\t}\n")
	return b.String()
}

// renderKillSwitch writes the two chains. Both are policy accept with an
// explicit drop last — a drop policy would take the box off the network if a
// rule ever failed to render.
func (r Ruleset) renderKillSwitch() string {
	k := r.KillSwitch
	if k == nil || k.SecondaryIfName == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\tchain killswitch_out {\n")
	b.WriteString("\t\ttype filter hook output priority filter + 10; policy accept;\n")
	b.WriteString("\n\t\t# Nothing this box originates may leave the secondary uplink in the\n")
	b.WriteString("\t\t# clear. Everything accepted below is either the tunnel's own crypto\n")
	b.WriteString("\t\t# or link-scoped, so none of it can carry payload to the internet.\n")
	fmt.Fprintf(&b, "\t\toifname != %q accept\n", k.SecondaryIfName)
	b.WriteString("\n\t\t# Replies to flows that arrived here — an inbound VPN user over the\n")
	b.WriteString("\t\t# dish. An outbound flow's first packet never leaves, so it can\n")
	b.WriteString("\t\t# never become established through this.\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString(k.exemptions())
	b.WriteString(r.renderDrop())
	b.WriteString("\t}\n")

	b.WriteString("\n\tchain killswitch_fwd {\n")
	b.WriteString("\t\ttype filter hook forward priority filter + 10; policy accept;\n")
	b.WriteString("\n\t\t# The same rule for traffic passing through: a LAN host must not\n")
	b.WriteString("\t\t# reach the internet over the raw uplink either.\n")
	fmt.Fprintf(&b, "\t\toifname != %q accept\n", k.SecondaryIfName)
	b.WriteString("\t\tct state established,related accept\n")
	if k.DishSubnet != "" {
		fmt.Fprintf(&b, "\t\tip daddr %s accept\n", k.DishSubnet)
	}
	b.WriteString(r.renderDrop())
	b.WriteString("\t}\n")
	return b.String()
}

// renderDrop counts kills when counters are on; the drop itself never changes.
func (r Ruleset) renderDrop() string {
	if r.Counters {
		return fmt.Sprintf("\t\tcounter name %s drop\n", CounterKillSwitch)
	}
	return "\t\tdrop\n"
}

// exemptions is the entire allowlist, in the order a packet meets it.
func (k *KillSwitch) exemptions() string {
	var b strings.Builder
	if k.MarkMask != 0 {
		mask, value := netmark.Hex(k.MarkMask), netmark.Hex(k.MarkValue)
		b.WriteString("\n\t\t# The tunnel's own transport. This is the whole point.\n")
		fmt.Fprintf(&b, "\t\tmeta mark and %s == %s meta l4proto udp accept\n", mask, value)
		if len(k.BootstrapIPs) > 0 {
			b.WriteString("\n\t\t# Resolving the endpoint hostname needs a resolver, and the only\n")
			b.WriteString("\t\t# one reachable before the tunnel is up is out here. Fixed\n")
			b.WriteString("\t\t# addresses over DoH: an editable list would be an editable hole.\n")
			fmt.Fprintf(&b, "\t\tmeta mark and %s == %s tcp dport 443 ip daddr @%s accept\n",
				mask, value, SetDoHBootstrap)
		}
	}
	if k.ProbeMark != 0 && len(k.ProbeIPs) > 0 {
		b.WriteString("\n\t\t# The health probe measuring this uplink.\n")
		fmt.Fprintf(&b, "\t\tmeta mark and %s == %s ip daddr @%s accept\n",
			netmark.Hex(netmark.MaskPin), netmark.Hex(k.ProbeMark), SetProbe)
	}

	b.WriteString("\n\t\t# Link-scoped only: a lease, the gateway the health probe pings,\n")
	b.WriteString("\t\t# and the dish's own management address.\n")
	b.WriteString("\t\tudp sport 68 udp dport 67 accept\n")
	if k.GatewayIP != "" {
		fmt.Fprintf(&b, "\t\tip daddr %s accept\n", k.GatewayIP)
	}
	if k.DishSubnet != "" {
		fmt.Fprintf(&b, "\t\tip daddr %s accept\n", k.DishSubnet)
	}
	return b.String()
}

// renderManglePostSection wraps the postrouting mangle chain so Render can treat
// every section uniformly.
func (r Ruleset) renderManglePostSection() string {
	if !r.connmark() && !r.Counters {
		return ""
	}
	return r.renderManglePost()
}

// renderManglePost writes the working skb mark back to conntrack, and counts
// what left under each group mark.
func (r Ruleset) renderManglePost() string {
	all := netmark.Hex(netmark.MaskAll)
	var b strings.Builder
	b.WriteString("\tchain mangle_post {\n")
	b.WriteString("\t\ttype filter hook postrouting priority mangle; policy accept;\n")
	if r.connmark() {
		fmt.Fprintf(&b, "\t\tmeta mark and %s != 0x0 ct mark set meta mark and %s\n", all, all)
	}
	if r.Counters {
		group := netmark.Hex(netmark.MaskGroup)
		fmt.Fprintf(&b, "\t\tmeta mark and %s == %s counter name %s\n",
			group, netmark.Hex(netmark.GroupMark(netmark.GroupDomestic)), CounterDomestic)
		fmt.Fprintf(&b, "\t\tmeta mark and %s == %s counter name %s\n",
			group, netmark.Hex(netmark.GroupMark(netmark.GroupForeign)), CounterForeign)
	}
	b.WriteString("\t}\n")
	return b.String()
}

func (r Ruleset) renderNatPost() string {
	if len(r.Masquerade) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\tchain nat_post {\n")
	b.WriteString("\t\ttype nat hook postrouting priority srcnat; policy accept;\n")
	fmt.Fprintf(&b, "\t\toifname { %s } masquerade\n", quoteList(r.Masquerade))
	b.WriteString("\t}\n")
	return b.String()
}

// ChainNames lists the chains Render will emit, for intended-vs-actual checks.
func (r Ruleset) ChainNames() []string {
	var out []string
	if r.connmark() || r.LANClassify != nil {
		out = append(out, "mangle_pre")
	}
	if len(r.PortForwards) > 0 {
		out = append(out, "nat_pre")
	}
	if r.FilterInput != nil {
		out = append(out, "filter_in")
	}
	if r.FilterForward != nil {
		out = append(out, "filter_fwd")
	}
	if r.KillSwitch != nil && r.KillSwitch.SecondaryIfName != "" {
		out = append(out, "killswitch_out", "killswitch_fwd")
	}
	if r.connmark() || r.Counters {
		out = append(out, "mangle_post")
	}
	if len(r.Masquerade) > 0 {
		out = append(out, "nat_post")
	}
	return out
}

// SetNames mirrors renderSets, bootstrap set included.
func (r Ruleset) SetNames() []string {
	var out []string
	for _, s := range r.Sets {
		out = append(out, s.Name)
	}
	if k := r.KillSwitch; k != nil && len(k.BootstrapIPs) > 0 {
		out = append(out, SetDoHBootstrap)
	}
	if k := r.KillSwitch; k != nil && k.ProbeMark != 0 && len(k.ProbeIPs) > 0 {
		out = append(out, SetProbe)
	}
	return out
}

func (r Ruleset) CounterNames() []string {
	if !r.Counters {
		return nil
	}
	return []string{CounterDomestic, CounterForeign, CounterKillSwitch}
}

// quoteList renders a set of interface names as nftables expects them.
func quoteList(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("%q", n))
	}
	return strings.Join(out, ", ")
}
