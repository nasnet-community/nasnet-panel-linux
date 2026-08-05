package tc

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"github.com/sirupsen/logrus"
)

// Manager handles Linux TC (Traffic Control) HTB setup for bandwidth rate
// limiting. It creates per-tier TC classes keyed by the firewall mark Xray
// sets via sockopt.mark.
//
// Marks are shared with dual-WAN routing (see pkg/netmark), so every handle
// here is masked to the tier field and the conntrack save/restore pair lives
// in the owned nftables table rather than in iptables.
type Manager struct {
	iface   string // ingress uplink
	totalBW int    // total link bandwidth in Mbps
	nft     *nft.Manager
	mu      sync.Mutex
	log     *logrus.Entry
}

// NewManager creates a TC manager for the given interface and link speed.
func NewManager(iface string, totalBW int, nftMgr *nft.Manager) (*Manager, error) {
	if nftMgr == nil {
		return nil, fmt.Errorf("tc: nft manager is required")
	}
	if totalBW <= 0 {
		totalBW = 1000 // default 1 Gbps
	}
	return &Manager{
		iface:   iface,
		totalBW: totalBW,
		nft:     nftMgr,
		log:     logrus.WithField("component", "tc"),
	}, nil
}

// FilterHandle returns the masked `tc ... fw` handle for a tier mark.
func (m *Manager) FilterHandle(tierMark uint32) string {
	return fmt.Sprintf("0x%x/%s", tierMark, netmark.Hex(netmark.MaskTier))
}

// Setup configures the full TC HTB qdisc + per-tier classes + nftables mark-to-class filters.
// Safe to call multiple times — it tears down existing config first.
func (m *Manager) Setup(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		m.log.Warn("TC bandwidth shaping is only supported on Linux, skipping")
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.WithFields(logrus.Fields{
		"interface": m.iface,
		"total_bw":  m.totalBW,
	}).Info("Setting up TC bandwidth shaping")

	// Tear down any existing qdisc (ignore errors — may not exist)
	_ = m.run("tc", "qdisc", "del", "dev", m.iface, "root")

	// 1. Root HTB qdisc
	if err := m.run("tc", "qdisc", "add", "dev", m.iface, "root", "handle", "1:", "htb", "default", "99"); err != nil {
		return fmt.Errorf("failed to add root qdisc: %w", err)
	}

	// 2. Root class (total link bandwidth)
	rate := fmt.Sprintf("%dmbit", m.totalBW)
	rootBurst := m.computeRootBurst()
	if err := m.run("tc", "class", "add", "dev", m.iface, "parent", "1:", "classid", "1:1", "htb", "rate", rate, "burst", rootBurst); err != nil {
		return fmt.Errorf("failed to add root class: %w", err)
	}

	// 3. Default class (unlimited — for unmarked traffic)
	if err := m.run("tc", "class", "add", "dev", m.iface, "parent", "1:1", "classid", "1:99", "htb", "rate", rate, "burst", rootBurst); err != nil {
		return fmt.Errorf("failed to add default class: %w", err)
	}

	// 4. Per-tier classes
	for _, tier := range bandwidth.RateLimitedTiers() {
		tierRate := fmt.Sprintf("%dmbit", tier.RateMbit)
		tierCeil := fmt.Sprintf("%dmbit", tier.CeilMbit)
		classID := tier.TCClassID()

		if err := m.run("tc", "class", "add", "dev", m.iface, "parent", "1:1", "classid", classID,
			"htb", "rate", tierRate, "ceil", tierCeil, "burst", tier.Burst); err != nil {
			return fmt.Errorf("failed to add class %s: %w", classID, err)
		}

		// Add fq_codel qdisc under each class for fair queuing with AQM.
		// fq_codel provides per-flow fairness (like SFQ) plus Controlled Delay
		// to prevent bufferbloat and keep latency low under load.
		handle := fmt.Sprintf("%d:", tier.Mark)
		if err := m.run("tc", "qdisc", "add", "dev", m.iface, "parent", classID, "handle", handle, "fq_codel"); err != nil {
			m.log.WithError(err).Warnf("Failed to add fq_codel qdisc for class %s (non-fatal)", classID)
		}

		m.log.WithFields(logrus.Fields{
			"class": classID,
			"rate":  tierRate,
			"ceil":  tierCeil,
			"mark":  tier.Mark,
		}).Debug("Added TC class for bandwidth tier")
	}

	// 5. Filters: match the tier field of the fwmark to a TC class.
	for _, tier := range bandwidth.RateLimitedTiers() {
		handle := m.FilterHandle(tier.Mark)
		classID := tier.TCClassID()
		if err := m.run("tc", "filter", "add", "dev", m.iface, "parent", "1:", "protocol", "ip",
			"prio", "1", "handle", handle, "fw", "classid", classID); err != nil {
			return fmt.Errorf("failed to add filter for handle %s: %w", handle, err)
		}
	}

	// 6. Connmark: save outgoing packet marks to conntrack, restore on inbound.
	if err := m.setupConnmark(ctx); err != nil {
		m.log.WithError(err).Warn("Connmark setup failed (non-fatal, download shaping may not work)")
	}

	// 7. Setup IFB for ingress (download) shaping
	if err := m.setupIngress(); err != nil {
		m.log.WithError(err).Warn("Ingress (download) shaping setup failed (non-fatal, upload-only shaping active)")
	}

	m.log.Info("TC bandwidth shaping setup complete")
	return nil
}

// setupConnmark enables the masked conntrack save/restore pair in the owned
// nftables table. Xray sets SO_MARK on outbound sockets; the save rule copies
// that mark into conntrack and the restore rule puts it back on inbound
// packets, which is what makes download shaping work.
//
// Masked to netmark.MaskAll, and in nftables rather than iptables, because the
// same conntrack mark word also carries the dual-WAN group selector and the
// inbound ingress pin. The old unmasked iptables restore overwrote both.
func (m *Manager) setupConnmark(ctx context.Context) error {
	if err := m.nft.Update(ctx, func(rs *nft.Ruleset) { rs.Connmark = true }); err != nil {
		return fmt.Errorf("failed to enable connmark chains: %w", err)
	}
	m.log.Info("Connmark nftables chains configured")
	return nil
}

// setupIngress configures IFB device for download traffic shaping
func (m *Manager) setupIngress() error {
	ifb := "ifb0"

	// Load IFB module
	_ = m.run("modprobe", "ifb", "numifbs=1")

	// Bring up IFB
	if err := m.run("ip", "link", "set", ifb, "up"); err != nil {
		return fmt.Errorf("failed to bring up %s: %w", ifb, err)
	}

	// Add ingress qdisc on real interface
	_ = m.run("tc", "qdisc", "del", "dev", m.iface, "ingress")
	if err := m.run("tc", "qdisc", "add", "dev", m.iface, "handle", "ffff:", "ingress"); err != nil {
		return fmt.Errorf("failed to add ingress qdisc: %w", err)
	}

	// Redirect ingress to IFB.
	// Use "action connmark" to restore the conntrack mark to the packet's skb->mark
	// BEFORE the redirect. The tc ingress qdisc runs ahead of every netfilter
	// prerouting hook, so the owned table's mangle_pre restore rule has not fired
	// yet at this point. Without the tc connmark action, packets arrive at IFB with
	// fwmark=0 and no HTB class matches.
	//
	// This action takes no mask — it copies the ct mark word whole. That is why
	// netmark keeps the reserved nibble permanently zero: nothing this action can
	// import is junk. See pkg/netmark for the layout.
	_ = m.run("modprobe", "act_connmark")
	if err := m.run("tc", "filter", "add", "dev", m.iface, "parent", "ffff:", "protocol", "ip",
		"u32", "match", "u32", "0", "0",
		"action", "connmark",
		"action", "mirred", "egress", "redirect", "dev", ifb); err != nil {
		return fmt.Errorf("failed to redirect ingress to IFB: %w", err)
	}

	// Setup HTB on IFB (same structure as egress)
	_ = m.run("tc", "qdisc", "del", "dev", ifb, "root")

	rate := fmt.Sprintf("%dmbit", m.totalBW)
	rootBurst := m.computeRootBurst()

	if err := m.run("tc", "qdisc", "add", "dev", ifb, "root", "handle", "1:", "htb", "default", "99"); err != nil {
		return fmt.Errorf("failed to add IFB root qdisc: %w", err)
	}
	if err := m.run("tc", "class", "add", "dev", ifb, "parent", "1:", "classid", "1:1", "htb", "rate", rate, "burst", rootBurst); err != nil {
		return fmt.Errorf("failed to add IFB root class: %w", err)
	}
	if err := m.run("tc", "class", "add", "dev", ifb, "parent", "1:1", "classid", "1:99", "htb", "rate", rate, "burst", rootBurst); err != nil {
		return fmt.Errorf("failed to add IFB default class: %w", err)
	}

	for _, tier := range bandwidth.RateLimitedTiers() {
		tierRate := fmt.Sprintf("%dmbit", tier.RateMbit)
		tierCeil := fmt.Sprintf("%dmbit", tier.CeilMbit)
		classID := tier.TCClassID()

		if err := m.run("tc", "class", "add", "dev", ifb, "parent", "1:1", "classid", classID,
			"htb", "rate", tierRate, "ceil", tierCeil, "burst", tier.Burst); err != nil {
			return fmt.Errorf("failed to add IFB class %s: %w", classID, err)
		}

		// Add fq_codel qdisc under each class (mirrors egress setup)
		handle := fmt.Sprintf("%d:", tier.Mark)
		if err := m.run("tc", "qdisc", "add", "dev", ifb, "parent", classID, "handle", handle, "fq_codel"); err != nil {
			m.log.WithError(err).Warnf("Failed to add fq_codel qdisc for IFB class %s (non-fatal)", classID)
		}
	}

	// Filters on IFB matching the tier field of the fwmark
	for _, tier := range bandwidth.RateLimitedTiers() {
		handle := m.FilterHandle(tier.Mark)
		classID := tier.TCClassID()
		if err := m.run("tc", "filter", "add", "dev", ifb, "parent", "1:", "protocol", "ip",
			"prio", "1", "handle", handle, "fw", "classid", classID); err != nil {
			return fmt.Errorf("failed to add IFB filter for handle %s: %w", handle, err)
		}
	}

	m.log.Info("IFB ingress shaping configured")
	return nil
}

// Teardown removes all TC configuration from the interface
func (m *Manager) Teardown(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.WithField("interface", m.iface).Info("Tearing down TC bandwidth shaping")

	// Drop the connmark chains from the owned table. This is a table replace,
	// not a rule-by-rule delete, so it cannot leave duplicates behind.
	if err := m.nft.Update(ctx, func(rs *nft.Ruleset) { rs.Connmark = false }); err != nil {
		m.log.WithError(err).Warn("Failed to remove connmark chains")
	}

	var errs []string
	if err := m.run("tc", "qdisc", "del", "dev", m.iface, "root"); err != nil {
		errs = append(errs, fmt.Sprintf("egress: %v", err))
	}
	if err := m.run("tc", "qdisc", "del", "dev", m.iface, "ingress"); err != nil {
		errs = append(errs, fmt.Sprintf("ingress: %v", err))
	}
	_ = m.run("tc", "qdisc", "del", "dev", "ifb0", "root")

	if len(errs) > 0 {
		return fmt.Errorf("teardown errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// computeRootBurst calculates the burst parameter for root and default HTB classes.
// HTB requires burst >= rate_bytes_per_sec / kernel_HZ. With a too-small burst, the
// token bucket drains instantly, causing micro-throttling (traffic for ~0.1ms then
// stalls for ~4ms), which produces extreme jitter, packet drops, and TCP collapse.
// The root burst must also be >= the largest child burst (4m for the 500 Mbps tier).
func (m *Manager) computeRootBurst() string {
	// rate_bytes / HZ with HZ=250 (conservative) and 2x safety margin
	burstBytes := m.totalBW * 1000000 / 8 / 250 * 2
	// Must be >= largest child burst (4MB for 500 Mbps tier)
	if burstBytes < 4*1024*1024 {
		burstBytes = 4 * 1024 * 1024
	}
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
