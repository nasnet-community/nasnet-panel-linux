package usecase

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// Bypass-mode range. Not Starlink-specific (CGNAT uses it too)
const starlinkBypassCIDR = "100.64.0.0/10"

// GatewayReachable must go out a socket bound to that uplink (pref 20/21)
type Probe interface {
	Carrier(ctx context.Context, ifName string) (bool, error)
	GatewayReachable(ctx context.Context, ifName, gateway string) (bool, error)
}

// DampingConfig is live reloadable from settings
type DampingConfig struct {
	FailuresToDown int
	SuccessesToUp  int
	FailbackDwell  time.Duration
}

func DefaultDamping() DampingConfig {
	return DampingConfig{FailuresToDown: 3, SuccessesToUp: 6, FailbackDwell: 60 * time.Second}
}

type uplinkState struct {
	up           bool
	fails        int
	successes    int
	lastDownAt   time.Time
	everObserved bool
}

// HealthMonitor damps probe results into one route operation. Revisit at a
// third uplink: a false "up" would pin traffic to a dead group member.
type HealthMonitor struct {
	Backend  system.Backend
	Probe    Probe
	Damping  DampingConfig
	Now      func() time.Time
	OnChange func(ifName string, up bool)

	mu     sync.Mutex
	states map[string]*uplinkState
}

func NewHealthMonitor(be system.Backend, p Probe, d DampingConfig) *HealthMonitor {
	return &HealthMonitor{
		Backend: be, Probe: p, Damping: d,
		Now:    time.Now,
		states: map[string]*uplinkState{},
	}
}

func (h *HealthMonitor) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

// Observe runs one probe cycle. forceState is "up", "down" or "".
func (h *HealthMonitor) Observe(ctx context.Context, u Uplink, gateway, forceState string) (bool, bool, error) {
	h.mu.Lock()
	st, ok := h.states[u.IfName]
	if !ok {
		st = &uplinkState{}
		h.states[u.IfName] = st
	}
	h.mu.Unlock()

	switch forceState {
	case "up":
		return h.transition(st, u, true)
	case "down":
		return h.transition(st, u, false)
	}

	carrier, err := h.Probe.Carrier(ctx, u.IfName)
	if err != nil {
		return st.up, false, fmt.Errorf("carrier %s: %w", u.IfName, err)
	}
	if !carrier {
		// Damping smooths flaky reachability, not the physical layer.
		st.fails = h.Damping.FailuresToDown
		return h.transition(st, u, false)
	}

	reachable, err := h.Probe.GatewayReachable(ctx, u.IfName, gateway)
	if err != nil {
		reachable = false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if reachable {
		st.fails = 0
		st.successes++
		if !st.up && st.successes >= h.Damping.SuccessesToUp {
			// Dwell first, so a flapping uplink doesn't thrash.
			if !st.lastDownAt.IsZero() && h.now().Sub(st.lastDownAt) < h.Damping.FailbackDwell {
				return st.up, false, nil
			}
			return h.setLocked(st, u, true)
		}
		if !st.everObserved && st.up {
			st.everObserved = true
		}
		return st.up, false, nil
	}

	st.successes = 0
	st.fails++
	if st.up && st.fails >= h.Damping.FailuresToDown {
		return h.setLocked(st, u, false)
	}
	if !st.everObserved {
		st.everObserved = true
		return st.up, false, nil
	}
	return st.up, false, nil
}

func (h *HealthMonitor) transition(st *uplinkState, u Uplink, up bool) (bool, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.setLocked(st, u, up)
}

func (h *HealthMonitor) setLocked(st *uplinkState, u Uplink, up bool) (bool, bool, error) {
	changed := st.up != up || !st.everObserved
	st.up = up
	st.everObserved = true
	st.fails, st.successes = 0, 0
	if !up {
		st.lastDownAt = h.now()
	}
	if changed && h.OnChange != nil {
		h.OnChange(u.IfName, up)
	}
	return up, changed, nil
}

// ApplyRoute is the whole failover mechanism
func (h *HealthMonitor) ApplyRoute(ctx context.Context, u Uplink, gateway string, up bool) error {
	route := system.Route{Table: u.Table, Dest: "default", Gateway: gateway, OifName: u.IfName}
	if up {
		if err := h.Backend.RouteReplace(ctx, route); err != nil {
			return fmt.Errorf("bring up %s: %w", u.IfName, err)
		}
		return nil
	}
	if err := h.Backend.RouteDel(ctx, system.Route{Table: u.Table, Dest: "default"}); err != nil {
		return fmt.Errorf("bring down %s: %w", u.IfName, err)
	}
	return nil
}

// LeaseVerdicts runs the address dependent rules (at apply, and after every DHCP lease)
func LeaseVerdicts(addrs []system.Addr, uplinks []Uplink, dishReachable bool, dishLeaseCIDR string) []domain.Verdict {
	var vs []domain.Verdict

	isUplink := map[string]bool{}
	for _, u := range uplinks {
		isUplink[u.IfName] = true
	}

	type entry struct {
		ifName string
		prefix netip.Prefix
	}
	var entries []entry
	for _, a := range addrs {
		if !isUplink[a.IfName] {
			continue
		}
		p, err := netip.ParsePrefix(a.CIDR)
		if err != nil {
			continue
		}
		entries = append(entries, entry{ifName: a.IfName, prefix: p})
	}

	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a.ifName == b.ifName {
				continue
			}
			aIP, bIP := a.prefix.Addr(), b.prefix.Addr()
			switch {
			case aIP == bIP:
				vs = append(vs, domain.Verdict{
					Rule:  "V29",
					Level: domain.LevelReject,
					Message: fmt.Sprintf(
						"%s and %s both hold %s; source selection and conntrack both break and no sysctl helps",
						a.ifName, b.ifName, aIP),
				})
			case a.prefix.Contains(bIP) || b.prefix.Contains(aIP):
				vs = append(vs, domain.Verdict{
					Rule:  "V30",
					Level: domain.LevelWarn,
					Message: fmt.Sprintf(
						"%s (%s) and %s (%s) are on overlapping subnets; ARP hardening is applied, "+
							"but check that this is intended",
						a.ifName, aIP, b.ifName, bIP),
				})
			}
		}
	}

	// V31 — dish answers, lease outside bypass space. Cause of most V29/V30.
	if dishReachable && dishLeaseCIDR != "" {
		bypass, err := netip.ParsePrefix(starlinkBypassCIDR)
		lease, perr := netip.ParsePrefix(dishLeaseCIDR)
		if err == nil && perr == nil && !bypass.Contains(lease.Addr()) {
			vs = append(vs, domain.Verdict{
				Rule:  "V31",
				Level: domain.LevelWarn,
				Message: fmt.Sprintf(
					"Starlink is not in bypass mode: the dish answers but the lease %s is outside %s",
					dishLeaseCIDR, starlinkBypassCIDR),
			})
		}
	}

	return vs
}
