package usecase

import (
	"encoding/json"
	"net"
	"net/netip"
	"strconv"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// Swapped whole under the mutex so a mid-tick save can't tear it.
type HealthConfig struct {
	TargetsDomestic []ProbeTarget
	TargetsForeign  []ProbeTarget
	// Per-line overrides. A slot missing here uses its group's list above.
	TargetsBySlot           map[domain.UplinkSlot][]ProbeTarget
	DegradedLossPct         int // legacy default, inherited by groups without an override
	DegradedLossPctDomestic int
	DegradedLossPctForeign  int
	// Per-line override; a slot missing here uses its group's threshold.
	DegradedLossPctBySlot map[domain.UplinkSlot]int
	DegradedRTTms         map[domain.UplinkSlot]int
	FailoverToVPN         bool
	// PortMapEnabled turns the upstream port mapper on. Off by default: it
	// transmits to the upstream router and opens inbound ports.
	PortMapEnabled bool
	// Settings-backed like the rest, so one reload covers it.
	PoolStrategy PoolStrategy
}

func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		TargetsDomestic: []ProbeTarget{
			{Address: "217.218.155.155:53", Proto: "dns"},
			{Address: "178.22.122.100:53", Proto: "dns"},
		},
		TargetsForeign: []ProbeTarget{
			{Address: "1.1.1.1:443", Proto: "tcp"},
			{Address: "8.8.8.8:443", Proto: "tcp"},
		},
		TargetsBySlot:         map[domain.UplinkSlot][]ProbeTarget{},
		DegradedLossPct:       25,
		DegradedLossPctBySlot: map[domain.UplinkSlot]int{},
		// Starlink's RTT floor is high, and every secondary may be a dish.
		DegradedRTTms: degradedRTTDefaults(),
		FailoverToVPN: true,
		PoolStrategy:  DefaultPoolStrategy,
	}
}

func (c HealthConfig) targetsFor(slot domain.UplinkSlot) []ProbeTarget {
	if own := c.TargetsBySlot[slot]; len(own) > 0 {
		return own
	}
	if slot.IsDomestic() {
		return c.TargetsDomestic
	}
	return c.TargetsForeign
}

func (c HealthConfig) degradedLossFor(slot domain.UplinkSlot) int {
	if n, ok := c.DegradedLossPctBySlot[slot]; ok && n > 0 {
		return n
	}
	return c.degradedGroupLossFor(slot)
}

func (c HealthConfig) degradedGroupLossFor(slot domain.UplinkSlot) int {
	n := c.DegradedLossPctForeign
	if slot.IsDomestic() {
		n = c.DegradedLossPctDomestic
	}
	if n > 0 {
		return n
	}
	return c.DegradedLossPct
}

// Only probes out a secondary uplink ever meet the kill switch, and each leg
// now has its own list, so the exemption is per slot rather than one set.
func (c HealthConfig) probeExemptIPsBySlot() map[domain.UplinkSlot][]string {
	out := map[domain.UplinkSlot][]string{}
	for _, s := range domain.SecondarySlots() {
		var ips []string
		for _, t := range c.targetsFor(s) {
			if host, _, err := net.SplitHostPort(t.Address); err == nil {
				ips = append(ips, host)
			}
		}
		out[s] = ips
	}
	return out
}

// Settings keys for one line's own checks. The group keys keep their old
// names, so these carry a slot_ segment to stay clear of them.
func ProbeTargetsSlotKey(slot domain.UplinkSlot) string {
	return "router_probe_targets_slot_" + string(slot)
}

func DegradedRTTSlotKey(slot domain.UplinkSlot) string {
	return "router_degraded_rtt_ms_slot_" + string(slot)
}

func DegradedLossSlotKey(slot domain.UplinkSlot) string {
	return "router_degraded_loss_pct_slot_" + string(slot)
}

func allUplinkSlots() []domain.UplinkSlot {
	return append(domain.DomesticSlots(), domain.SecondarySlots()...)
}

// One bad set element aborts the whole nft table load, so v4 literals only.
func validTarget(t ProbeTarget) bool {
	if t.Proto != "tcp" && t.Proto != "dns" {
		return false
	}
	host, port, err := net.SplitHostPort(t.Address)
	if err != nil {
		return false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Is4()
}

func parseTargets(blob string) []ProbeTarget {
	var ts []ProbeTarget
	if json.Unmarshal([]byte(blob), &ts) != nil {
		return nil
	}
	out := ts[:0]
	for _, t := range ts {
		if validTarget(t) {
			out = append(out, t)
		}
	}
	return out
}

// Per-key fallback: a corrupt blob must not kill probing.
func ParseHealthConfig(get func(string) (string, error)) HealthConfig {
	cfg := DefaultHealthConfig()
	if v, err := get("router_probe_targets_domestic"); err == nil && v != "" {
		if ts := parseTargets(v); len(ts) > 0 {
			cfg.TargetsDomestic = ts
		}
	}
	if v, err := get("router_probe_targets_foreign"); err == nil && v != "" {
		if ts := parseTargets(v); len(ts) > 0 {
			cfg.TargetsForeign = ts
		}
	}
	if v, err := get("router_degraded_loss_pct"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			cfg.DegradedLossPct = n
		}
	}
	for _, group := range []struct {
		key       string
		threshold *int
	}{
		{"router_degraded_loss_pct_domestic", &cfg.DegradedLossPctDomestic},
		{"router_degraded_loss_pct_foreign", &cfg.DegradedLossPctForeign},
	} {
		if v, err := get(group.key); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				*group.threshold = n
			}
		}
	}
	if v, err := get("router_degraded_rtt_ms_domestic"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			for _, s := range domain.DomesticSlots() {
				cfg.DegradedRTTms[s] = n
			}
		}
	}
	if v, err := get("router_degraded_rtt_ms_foreign"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			for _, s := range domain.SecondarySlots() {
				cfg.DegradedRTTms[s] = n
			}
		}
	}
	for _, slot := range allUplinkSlots() {
		if v, err := get(ProbeTargetsSlotKey(slot)); err == nil && v != "" {
			if ts := parseTargets(v); len(ts) > 0 {
				cfg.TargetsBySlot[slot] = ts
			}
		}
		if v, err := get(DegradedRTTSlotKey(slot)); err == nil && v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.DegradedRTTms[slot] = n
			}
		}
		if v, err := get(DegradedLossSlotKey(slot)); err == nil && v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				cfg.DegradedLossPctBySlot[slot] = n
			}
		}
	}
	if v, err := get("router_failover_domestic_to_vpn"); err == nil && v != "" {
		cfg.FailoverToVPN = v == "true"
	}
	if v, err := get("router_portmap_enabled"); err == nil && v != "" {
		cfg.PortMapEnabled = v == "true"
	}
	// Missing or invalid values retain the default strategy.
	if v, err := get(PoolStrategyKey); err == nil {
		if s, ok := ParsePoolStrategy(v); ok {
			cfg.PoolStrategy = s
		}
	}
	return cfg
}

type routeState int

const (
	routeUp routeState = iota
	routeWithdraw
)

// A secondary carries tunnels only, so its policy is one line: a dead gateway
// withdraws, anything else keeps the route so the probe can see a recovery.
func secondaryRouteState(gatewayUp bool) routeState {
	if gatewayUp {
		return routeUp
	}
	return routeWithdraw
}

// The pool ladder has one rung. everAnswered is the evidence: the damper starts
// optimistic, so without it a tunnel that never replied reads "up". Ever, not
// this tick — one lost probe on a long-up tunnel is not a warm-up.
func tunnelVerdict(everAnswered, up, degraded bool) string {
	switch {
	case !up:
		return "no-internet"
	case !everAnswered:
		return "" // warm-up, not a claim
	case degraded:
		return "degraded"
	default:
		return "up"
	}
}

type uplinkLadder struct {
	Carrier  string
	Gateway  string
	Internet string
	Degraded bool
	Verdict  string
	Results  []ProbeResult
}

// Collapses the ladder to one word. gwKnown false = boot warm-up, not an outage.
func verdictFor(force string, carrier, gwKnown, gatewayUp, inetUp, inetKnown, degraded bool) string {
	switch force {
	case "up":
		return "forced-up"
	case "down":
		return "forced-down"
	}
	switch {
	case !carrier:
		return "no-carrier"
	case !gwKnown:
		return ""
	case !gatewayUp:
		return "no-gateway"
	case inetKnown && !inetUp:
		return "no-internet"
	case degraded:
		return "degraded"
	default:
		return "up"
	}
}

func degradedRTTDefaults() map[domain.UplinkSlot]int {
	out := map[domain.UplinkSlot]int{}
	for _, s := range domain.DomesticSlots() {
		out[s] = 300
	}
	for _, s := range domain.SecondarySlots() {
		out[s] = 800
	}
	return out
}
