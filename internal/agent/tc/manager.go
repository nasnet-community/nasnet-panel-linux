package tc

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/sirupsen/logrus"
)

// Manager shapes the bandwidth tiers on a node. Every tier is one pipe shared
// by all of its users, across all of the node's interfaces.
//
// Tier traffic leaves through outbound sockets that carry the tier's
// SO_PRIORITY (b0:N, set by Xray). On every eligible interface a clsact
// filter re-tags those packets b1:N and redirects them into the upload IFB,
// whose HTB holds one class per tier. Their packets also store the tier in the
// conntrack mark (nftables), so replies get it back from a ctinfo action and
// are redirected into the download IFB.
//
// Nothing else is touched: interfaces keep their own root qdiscs, packet marks
// used for dual-WAN routing are left alone, and untiered traffic never meets
// HTB. The tier sits in the conntrack mark's reserved nibble (pkg/netmark),
// outside what the router's own save/restore carries. Re-tagging on redirect
// keeps a tunnel's outer packets, or a stacked device, from counting the same
// bytes twice.
type Manager struct {
	iface   string // configured uplink, shaped whatever its kind
	totalBW int    // ceiling for all tiered traffic combined, in Mbps
	mu      sync.Mutex
	warned  map[string]bool // interfaces already reported as unshapeable
	log     *logrus.Entry
}

const (
	upIFB   = "nasnet-bw-up"
	downIFB = "nasnet-bw-down"
	// Filter preferences on interface hooks, well away from common ones.
	prioTag      = "41001"
	prioRedirect = "41002"
	nftTable     = "nasnet_bw"
)

// Link kinds that are never shaped unless configured explicitly: container
// plumbing, and devices stacked on a lower device that is shaped already.
var skippedKinds = map[string]bool{
	"bridge": true, "veth": true, "dummy": true, "ifb": true, "nlmon": true,
	"vlan": true, "macvlan": true, "macvtap": true, "ipvlan": true, "ipvtap": true,
	"vxlan": true, "geneve": true,
}

// NewManager creates a TC manager for the given uplink and link speed.
func NewManager(iface string, totalBW int) *Manager {
	if totalBW <= 0 {
		totalBW = 1000 // default 1 Gbps
	}
	return &Manager{
		iface:   iface,
		totalBW: totalBW,
		warned:  make(map[string]bool),
		log:     logrus.WithField("component", "tc"),
	}
}

// Iface returns the configured uplink.
func (m *Manager) Iface() string { return m.iface }

// TotalBW returns the configured ceiling in Mbps.
func (m *Manager) TotalBW() int { return m.totalBW }

// Ensure brings the shaping up to date and is cheap when it already is, so it
// can run on every hub push and on a timer. That timer also picks up
// interfaces created later: tunnels brought up after the agent, or the TUN
// devices Xray recreates when it restarts.
func (m *Manager) Ensure() error {
	if runtime.GOOS != "linux" {
		m.log.Warn("TC bandwidth shaping is only supported on Linux, skipping")
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	qdiscs := m.output("tc", "qdisc", "show")
	m.removeLegacy(qdiscs)

	links, err := listLinks()
	if err != nil {
		return err
	}
	for _, ifb := range []string{upIFB, downIFB} {
		if err := m.ensureIFB(ifb, links); err != nil {
			return err
		}
	}

	var problems []string
	if err := m.applyNft(); err != nil {
		problems = append(problems, fmt.Sprintf("download shaping unavailable: %v", err))
	}

	targets := m.targets(links)
	if !targets[m.iface] {
		problems = append(problems, fmt.Sprintf("interface %s not found", m.iface))
	}
	for dev := range targets {
		err := m.ensureHooks(dev)
		switch {
		case err == nil:
		case dev == m.iface:
			problems = append(problems, fmt.Sprintf("%s: %v", dev, err))
		case !m.warned[dev]:
			m.warned[dev] = true
			m.log.WithError(err).Warnf("Traffic through %s stays unshaped", dev)
		}
	}
	// An interface can stop qualifying (the uplink setting moved); take our
	// filters back off it.
	for _, dev := range clsactDevices(qdiscs) {
		if !targets[dev] && m.hasFilters(dev) {
			m.clearHooks(dev)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// Teardown removes everything Ensure created, plus shaping left by agents that
// predate it.
func (m *Manager) Teardown() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	qdiscs := m.output("tc", "qdisc", "show")
	m.removeLegacy(qdiscs)
	_ = m.run("nft", "delete", "table", "inet", nftTable)
	for _, dev := range clsactDevices(qdiscs) {
		if m.hasFilters(dev) {
			m.clearHooks(dev)
		}
	}
	var errs []string
	for _, ifb := range []string{upIFB, downIFB} {
		if exec.Command("ip", "link", "show", ifb).Run() != nil {
			continue
		}
		if err := m.run("ip", "link", "del", ifb); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("teardown errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

type link struct {
	Name     string   `json:"ifname"`
	Flags    []string `json:"flags"`
	LinkInfo struct {
		Kind      string `json:"info_kind"`
		SlaveKind string `json:"info_slave_kind"`
	} `json:"linkinfo"`
}

func (l link) up() bool { return slices.Contains(l.Flags, "UP") }

func listLinks() ([]link, error) {
	out, err := exec.Command("ip", "-j", "-d", "link", "show").Output()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	var links []link
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, fmt.Errorf("parse interfaces: %w", err)
	}
	return links, nil
}

// targets picks the interfaces whose traffic is shaped: the configured uplink,
// and every other interface that is up and not container plumbing, a stacked
// device, or a bridge/bond port. Tunnels qualify: their inner packets are the
// only place tier traffic is still recognizable.
func (m *Manager) targets(links []link) map[string]bool {
	set := make(map[string]bool)
	for _, l := range links {
		switch {
		case l.Name == m.iface:
			set[l.Name] = true
		case l.Name == "lo" || l.Name == upIFB || l.Name == downIFB || !l.up():
		case skippedKinds[l.LinkInfo.Kind]:
		case l.LinkInfo.SlaveKind != "" && l.LinkInfo.SlaveKind != "vrf":
		default:
			set[l.Name] = true
		}
	}
	return set
}

func (m *Manager) ensureIFB(name string, links []link) error {
	if !slices.ContainsFunc(links, func(l link) bool { return l.Name == name }) {
		_ = m.run("modprobe", "ifb", "numifbs=0")
		if err := m.run("ip", "link", "add", name, "type", "ifb"); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	if err := m.run("ip", "link", "set", name, "up"); err != nil {
		return err
	}
	return m.ensureClasses(name)
}

// ensureClasses builds the HTB on an IFB unless the classes there already
// match. Rebuilding drops queued tier packets, so neither the periodic Ensure
// nor a restarted agent may do it for nothing.
func (m *Manager) ensureClasses(ifb string) error {
	if m.classesMatch(ifb) {
		return nil
	}
	tiers := bandwidth.RateLimitedTiers()
	root := fmt.Sprintf("%x:", bandwidth.ClassMajor)
	top := fmt.Sprintf("%x:ff", bandwidth.ClassMajor)
	rate := fmt.Sprintf("%dmbit", m.totalBW)
	// HTB can't be changed in place. Unclassified packets (there should be
	// none) bypass it as direct traffic.
	_ = m.run("tc", "qdisc", "del", "dev", ifb, "root")
	if err := m.run("tc", "qdisc", "add", "dev", ifb, "root", "handle", root, "htb", "r2q", "100"); err != nil {
		return fmt.Errorf("%s root qdisc: %w", ifb, err)
	}
	if err := m.run("tc", "class", "add", "dev", ifb, "parent", root, "classid", top,
		"htb", "rate", rate, "burst", m.computeRootBurst()); err != nil {
		return fmt.Errorf("%s root class: %w", ifb, err)
	}
	for _, tier := range tiers {
		if err := m.run("tc", "class", "add", "dev", ifb, "parent", top, "classid", tier.ClassID(), "htb",
			"rate", fmt.Sprintf("%dmbit", tier.RateMbit), "ceil", fmt.Sprintf("%dmbit", tier.CeilMbit),
			"burst", tier.Burst); err != nil {
			return fmt.Errorf("%s class %s: %w", ifb, tier.ClassID(), err)
		}
		// fq_codel keeps flows within the tier fair and its queue short.
		leaf := fmt.Sprintf("c%x:", tier.Level)
		if err := m.run("tc", "qdisc", "add", "dev", ifb, "parent", tier.ClassID(), "handle", leaf, "fq_codel"); err != nil {
			m.log.WithError(err).Warnf("Failed to add fq_codel under %s on %s (non-fatal)", tier.ClassID(), ifb)
		}
	}
	return nil
}

// listClasses reads an IFB's classes, from tc's JSON where it has one and
// from its text output on older iproute2.
func (m *Manager) listClasses(ifb string) ([]tcClass, bool) {
	if out, err := exec.Command("tc", "-j", "class", "show", "dev", ifb).Output(); err == nil {
		var classes []tcClass
		if json.Unmarshal(out, &classes) == nil {
			return classes, true
		}
	}
	out, err := exec.Command("tc", "class", "show", "dev", ifb).Output()
	if err != nil {
		return nil, false
	}
	// class htb b1:1 parent b1:ff leaf c1: prio 0 rate 10Mbit ceil 12Mbit burst ...
	var classes []tcClass
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "class" {
			continue
		}
		c := tcClass{Class: f[1], Handle: f[2]}
		for i := 3; i+1 < len(f); i++ {
			switch f[i] {
			case "rate":
				c.Rate = parseRate(f[i+1])
			case "ceil":
				c.Ceil = parseRate(f[i+1])
			}
		}
		classes = append(classes, c)
	}
	return classes, true
}

// parseRate turns tc's "12Mbit" into bytes/s, the unit of its JSON output.
func parseRate(s string) float64 {
	units := []struct {
		suffix string
		bits   float64
	}{{"Gbit", 1e9}, {"Mbit", 1e6}, {"Kbit", 1e3}, {"bit", 1}}
	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return -1
			}
			return v * u.bits / 8
		}
	}
	return -1
}

// tcClass is a class as `tc -j class show` prints it; rates in bytes/s.
type tcClass struct {
	Class  string  `json:"class"`
	Handle string  `json:"handle"`
	Rate   float64 `json:"rate"`
	Ceil   float64 `json:"ceil"`
}

// classesMatch reports whether the IFB's HTB classes have the wanted rates.
// Output it can't read counts as a mismatch.
func (m *Manager) classesMatch(ifb string) bool {
	classes, ok := m.listClasses(ifb)
	if !ok {
		return false
	}
	// fq_codel lists its active flows as classes too.
	classes = slices.DeleteFunc(classes, func(c tcClass) bool { return c.Class != "htb" })
	const bytesPerMbit = 125000
	want := map[string][2]float64{
		fmt.Sprintf("%x:ff", bandwidth.ClassMajor): {float64(m.totalBW) * bytesPerMbit, float64(m.totalBW) * bytesPerMbit},
	}
	for _, tier := range bandwidth.RateLimitedTiers() {
		want[tier.ClassID()] = [2]float64{float64(tier.RateMbit) * bytesPerMbit, float64(tier.CeilMbit) * bytesPerMbit}
	}
	if len(classes) != len(want) {
		return false
	}
	for _, c := range classes {
		if rates, ok := want[c.Handle]; !ok || rates != [2]float64{c.Rate, c.Ceil} {
			return false
		}
	}
	return true
}

// applyNft (re)writes the table that records the tier of each tier connection
// in the conntrack mark, unless it is already in place. Priority 300 runs after
// the router's mangle_post, which copies the packet mark over the conntrack
// mark and so clears the tier; every packet sets it again, and the and/or
// keeps the other fields.
func (m *Manager) applyNft() error {
	if strings.Count(m.output("nft", "list", "table", "inet", nftTable), "ct mark set") == len(bandwidth.RateLimitedTiers()) {
		return nil
	}
	keep := fmt.Sprintf("0x%08x", ^bandwidth.ConnMarkMask)
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\ntable inet %s {\n", nftTable, nftTable, nftTable)
	b.WriteString("\tchain postrouting {\n\t\ttype filter hook postrouting priority 300; policy accept;\n")
	for _, tier := range bandwidth.RateLimitedTiers() {
		fmt.Fprintf(&b, "\t\tmeta priority %x:%x ct mark set ct mark and %s or 0x%08x\n",
			bandwidth.PriorityMajor, tier.Level, keep, tier.ConnMark())
	}
	b.WriteString("\t}\n}\n")

	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureHooks installs the redirect filters on one interface unless they are
// all there already.
func (m *Manager) ensureHooks(dev string) error {
	tiers := bandwidth.RateLimitedTiers()
	egress := m.output("tc", "filter", "show", "dev", dev, "egress")
	ingress := m.output("tc", "filter", "show", "dev", dev, "ingress")
	if strings.Count(egress, "device "+upIFB) == len(tiers) &&
		strings.Count(ingress, "device "+downIFB) == len(tiers) &&
		strings.Contains(ingress, "ctinfo") {
		return nil
	}

	qdiscs := m.output("tc", "qdisc", "show", "dev", dev)
	switch {
	case strings.Contains(qdiscs, "qdisc clsact"):
		m.deleteFilters(dev)
	case strings.Contains(qdiscs, "qdisc ingress"):
		// clsact and ingress can't coexist, and ingress has no egress hook.
		return fmt.Errorf("has an ingress qdisc of its own, not shaped")
	default:
		if err := m.run("tc", "qdisc", "add", "dev", dev, "clsact"); err != nil {
			return err
		}
	}

	for _, tier := range tiers {
		if err := m.run("tc", "filter", "add", "dev", dev, "egress", "protocol", "all", "prio", prioTag,
			"handle", fmt.Sprint(tier.Level), "basic", "match", fmt.Sprintf("meta(priority eq 0x%x)", tier.SocketPriority()),
			"action", "skbedit", "priority", tier.ClassID(), "pipe",
			"action", "mirred", "egress", "redirect", "dev", upIFB); err != nil {
			return err
		}
	}

	// ctinfo (kernel 5.3+) copies only the tier bits of the conntrack mark,
	// and only onto packets nothing has marked yet, so other marks survive.
	mask := fmt.Sprintf("0x%08x", bandwidth.ConnMarkMask)
	if err := m.run("tc", "filter", "add", "dev", dev, "ingress", "protocol", "all", "prio", prioTag,
		"basic", "match", "meta(nf_mark eq 0)", "action", "ctinfo", "cpmark", mask, "continue"); err != nil {
		return err
	}
	for _, tier := range tiers {
		if err := m.run("tc", "filter", "add", "dev", dev, "ingress", "protocol", "all", "prio", prioRedirect,
			"handle", fmt.Sprintf("0x%08x/%s", tier.ConnMark(), mask), "fw",
			"action", "skbedit", "priority", tier.ClassID(), "pipe",
			"action", "mirred", "egress", "redirect", "dev", downIFB); err != nil {
			return err
		}
	}
	return nil
}

// clearHooks removes our filters from an interface, and its clsact qdisc once
// nobody else's filters are left on it.
func (m *Manager) clearHooks(dev string) {
	m.deleteFilters(dev)
	if strings.TrimSpace(m.output("tc", "filter", "show", "dev", dev, "egress")) == "" &&
		strings.TrimSpace(m.output("tc", "filter", "show", "dev", dev, "ingress")) == "" {
		_ = m.run("tc", "qdisc", "del", "dev", dev, "clsact")
	}
}

func (m *Manager) hasFilters(dev string) bool {
	return strings.Contains(m.output("tc", "filter", "show", "dev", dev, "egress")+
		m.output("tc", "filter", "show", "dev", dev, "ingress"), "device nasnet-bw-")
}

func (m *Manager) deleteFilters(dev string) {
	for _, hook := range []string{"egress", "ingress"} {
		for _, prio := range []string{prioTag, prioRedirect} {
			_ = m.run("tc", "filter", "del", "dev", dev, hook, "prio", prio)
		}
	}
}

// clsactDevices lists the interfaces that have a clsact qdisc.
func clsactDevices(qdiscs string) []string {
	var devs []string
	for line := range strings.SplitSeq(qdiscs, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[1] == "clsact" && fields[3] == "dev" {
			devs = append(devs, fields[4])
		}
	}
	return devs
}

// removeLegacy undoes the shaping of earlier agents: HTB as the root qdisc of
// the uplink and of ifb0, an ingress qdisc redirecting to ifb0, and the
// CONNMARK rules in the iptables mangle table that came before the owned
// nftables table.
func (m *Manager) removeLegacy(qdiscs string) {
	found := false
	for line := range strings.SplitSeq(qdiscs, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[3] != "dev" {
			continue
		}
		dev := fields[4]
		switch {
		case fields[1] == "htb" && fields[2] == "1:" && strings.Contains(line, " root ") && strings.Contains(line, "default 0x99"):
			_ = m.run("tc", "qdisc", "del", "dev", dev, "root")
			found = true
		case fields[1] == "ingress" &&
			strings.Contains(m.output("tc", "filter", "show", "dev", dev, "parent", "ffff:"), "Redirect to device ifb0"):
			_ = m.run("tc", "qdisc", "del", "dev", dev, "ingress")
			found = true
		}
	}
	if !found {
		return
	}
	m.log.Info("Removed bandwidth shaping left by an older agent")
	for range 5 {
		save := m.run("iptables", "-t", "mangle", "-D", "POSTROUTING",
			"-m", "mark", "!", "--mark", "0", "-j", "CONNMARK", "--save-mark")
		restore := m.run("iptables", "-t", "mangle", "-D", "PREROUTING",
			"-j", "CONNMARK", "--restore-mark")
		if save != nil && restore != nil {
			break
		}
	}
}

// computeRootBurst calculates the burst for the class all tiers borrow from.
// HTB requires burst >= rate_bytes_per_sec / kernel_HZ; a smaller one makes
// the bucket drain instantly and the class stall in short bursts. It must also
// cover the largest tier burst (4m for the 500 Mbps tier).
func (m *Manager) computeRootBurst() string {
	// rate_bytes / HZ with HZ=250 (conservative) and 2x safety margin
	burstBytes := max(m.totalBW*1000000/8/250*2, 4*1024*1024)
	return fmt.Sprintf("%dk", burstBytes/1024)
}

func (m *Manager) run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (output: %s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// output runs a read-only command; failures read as empty output.
func (m *Manager) output(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return string(out)
}
