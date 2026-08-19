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
	DegradedLossPct int
	DegradedRTTms   map[domain.UplinkSlot]int
	FailoverToVPN   bool
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
		DegradedLossPct: 25,
		DegradedRTTms: map[domain.UplinkSlot]int{
			domain.SlotDomestic: 300,
			// Starlink's RTT floor is high.
			domain.SlotSecondary: 800,
		},
		FailoverToVPN: true,
	}
}

func (c HealthConfig) targetsFor(slot domain.UplinkSlot) []ProbeTarget {
	if slot == domain.SlotDomestic {
		return c.TargetsDomestic
	}
	return c.TargetsForeign
}

// Only probes out the secondary uplink ever meet the kill switch.
func (c HealthConfig) probeExemptIPs() []string {
	var out []string
	for _, t := range c.TargetsForeign {
		if host, _, err := net.SplitHostPort(t.Address); err == nil {
			out = append(out, host)
		}
	}
	return out
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
	if v, err := get("router_degraded_rtt_ms_domestic"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.DegradedRTTms[domain.SlotDomestic] = n
		}
	}
	if v, err := get("router_degraded_rtt_ms_foreign"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.DegradedRTTms[domain.SlotSecondary] = n
		}
	}
	if v, err := get("router_failover_domestic_to_vpn"); err == nil && v != "" {
		cfg.FailoverToVPN = v == "true"
	}
	return cfg
}

type routeState int

const (
	routeUp routeState = iota
	routeFailover
	routeWithdraw
)

type routeInputs struct {
	Slot       domain.UplinkSlot
	GatewayUp  bool
	InternetUp bool
	FailoverOn bool
	VPNUp      bool
}

// The whole failover policy, pure so tests can enumerate it.
func routeStateFor(in routeInputs) routeState {
	if in.GatewayUp && in.InternetUp {
		return routeUp
	}
	if in.Slot == domain.SlotDomestic && in.FailoverOn && in.VPNUp {
		return routeFailover
	}
	if !in.GatewayUp {
		return routeWithdraw
	}
	// Gateway alive: keep the route, or the probe can't see the recovery.
	return routeUp
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
