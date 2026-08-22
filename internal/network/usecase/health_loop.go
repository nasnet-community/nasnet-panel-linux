package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// ErrBadInput maps to a 400 at the handler, like ErrBadTraceInput does.
var ErrBadInput = errors.New("bad input")

// Keeps the gateway path under the wg0 default; matches networkd's DHCP metric.
const probeRouteMetric = 100

// Operator override. Empty hands control back; addressed by if_name because
// that's the only identity the views expose.
func (u *networkUsecase) SetUplinkForce(ctx context.Context, ifName, state string) error {
	if state != "" && state != "up" && state != "down" {
		return fmt.Errorf("%w: force state must be up, down or empty", ErrBadInput)
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].IfName != ifName {
			continue
		}
		if err := u.IfRepo.SetForceState(ctx, rows[i].ID, state); err != nil {
			return err
		}
		u.emit(events.EventWANForceState, map[string]any{"if_name": ifName, "state": state})
		return nil
	}
	return fmt.Errorf("%w: no such uplink", ErrBadInput)
}

// Also re-syncs the kill-switch set: the exemption must match the new targets.
func (u *networkUsecase) SetHealthConfig(cfg HealthConfig) {
	u.healthMu.Lock()
	u.healthCfg = cfg
	u.healthMu.Unlock()
	ctx := context.Background()
	if uplinks, err := u.uplinks(ctx); err == nil && u.IfRepo != nil {
		rows, _ := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
		_ = ApplyKillSwitchState(ctx, u.Nft, uplinks, secondaryGateway(uplinks, rows),
			cfg.probeExemptIPs())
	}
}

func (u *networkUsecase) healthConfigSnapshot() HealthConfig {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	if u.healthCfg.DegradedRTTms == nil {
		u.healthCfg = DefaultHealthConfig()
	}
	return u.healthCfg
}

// Tests build the usecase as a bare literal. Callers hold healthMu.
func (u *networkUsecase) ensureHealthMaps() {
	if u.inetStates == nil {
		u.inetStates = map[string]*internetState{}
		u.bootTicks = map[string]int{}
		u.rings = map[string]*healthRing{}
		u.ladders = map[string]uplinkLadder{}
		u.degradedNow = map[string]bool{}
		u.lastEffective = map[string]bool{}
	}
	if u.tunnelWasUp == nil {
		u.tunnelWasUp = map[string]bool{}
		u.tunnelLastResolve = map[string]time.Time{}
	}
}

func (u *networkUsecase) targetProber() TargetProber {
	if u.Prober != nil {
		return u.Prober
	}
	return kernelProbe{}
}

func (u *networkUsecase) inetState(ifName string) *internetState {
	u.healthMu.Lock()
	u.ensureHealthMaps()
	defer u.healthMu.Unlock()
	s, ok := u.inetStates[ifName]
	if !ok {
		s = &internetState{}
		u.inetStates[ifName] = s
	}
	return s
}

func (u *networkUsecase) ring(ifName string) *healthRing {
	u.healthMu.Lock()
	u.ensureHealthMaps()
	defer u.healthMu.Unlock()
	r, ok := u.rings[ifName]
	if !ok {
		r = &healthRing{}
		u.rings[ifName] = r
	}
	return r
}

func (u *networkUsecase) effectiveChanged(ifName string, now bool) bool {
	u.healthMu.Lock()
	u.ensureHealthMaps()
	defer u.healthMu.Unlock()
	was, seen := u.lastEffective[ifName]
	u.lastEffective[ifName] = now
	return !seen || was != now
}

// poolConnectedNow: one member with a fresh handshake makes failover viable.
func (u *networkUsecase) poolConnectedNow(ctx context.Context) bool {
	for _, t := range u.vpnPoolNow(ctx).Tunnels {
		if st, err := u.wg().Status(ctx, t.IfName); err == nil && st.Connected() {
			return true
		}
	}
	return false
}

func (u *networkUsecase) currentPoolNexthops() []system.Nexthop {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	return append([]system.Nexthop(nil), u.poolNH...)
}

// publishPoolNH records the set table 203's default now holds and announces a
// move. Every writer of that route calls it, or a reader gets a stale set.
func (u *networkUsecase) publishPoolNH(nh []system.Nexthop) {
	var key strings.Builder
	for _, n := range nh {
		fmt.Fprintf(&key, "%s/%d ", n.OifName, n.Weight)
	}
	u.healthMu.Lock()
	u.ensureHealthMaps()
	// An empty previous key is boot, not a change.
	changed := key.String() != u.lastPoolKey && u.lastPoolKey != ""
	u.lastPoolKey = key.String()
	u.poolNH = append([]system.Nexthop(nil), nh...)
	u.healthMu.Unlock()

	if changed {
		names := make([]string, 0, len(nh))
		for _, n := range nh {
			names = append(names, n.OifName)
		}
		u.emit(events.EventVPNPoolChanged, map[string]any{"active": names})
	}
}

// One route op per uplink per tick. A kernel refusal bubbles up so the caller
// can't record a recovery that never happened.
func (u *networkUsecase) applyRouteState(ctx context.Context, up Uplink, gw string, st routeState) error {
	guarded := gw != "" && (st == routeUp || u.health.EverUp(up.IfName))
	if !guarded {
		return nil
	}
	u.healthMu.Lock()
	wasFailover := u.failoverActive && up.Slot == domain.SlotDomestic
	u.healthMu.Unlock()

	switch st {
	case routeUp:
		if err := u.Backend.RouteReplace(ctx, system.Route{
			Table: up.Table, Dest: "default", Gateway: gw, OifName: up.IfName,
		}); err != nil {
			return err
		}
		if wasFailover {
			u.setFailoverActive(false)
			u.emit(events.EventWANFailoverRestored, map[string]any{"if_name": up.IfName})
		}
	case routeFailover:
		nh := u.currentPoolNexthops()
		if len(nh) == 0 {
			// First tick after boot: the pool loop hasn't published a set yet.
			nh = poolNexthops(u.poolMembers(u.vpnPoolNow(ctx)))
		}
		if len(nh) == 0 {
			// The VPNUp gate should prevent this; never install an empty default.
			return nil
		}
		if err := u.Backend.RouteReplace(ctx, system.Route{
			Table: up.Table, Dest: "default", Nexthops: nh,
		}); err != nil {
			return err
		}
		// The probe still dials the real uplink; without this its failures
		// are self-fulfilling.
		_ = u.Backend.RouteReplace(ctx, system.Route{
			Table: up.Table, Dest: "default", Gateway: gw, OifName: up.IfName,
			Metric: probeRouteMetric,
		})
		if !wasFailover {
			u.setFailoverActive(true)
			u.emit(events.EventWANFailover, map[string]any{
				"if_name": up.IfName, "to": "pool"})
		}
	case routeWithdraw:
		_ = u.Backend.RouteDel(ctx, system.Route{Table: up.Table, Dest: "default"})
		// The kernel deletes only the lowest metric; the probe helper goes too.
		_ = u.Backend.RouteDel(ctx, system.Route{
			Table: up.Table, Dest: "default", Metric: probeRouteMetric,
		})
		if wasFailover {
			u.setFailoverActive(false)
			u.emit(events.EventWANFailoverLost, map[string]any{"if_name": up.IfName})
		}
	}
	return nil
}

func (u *networkUsecase) setFailoverActive(on bool) {
	u.healthMu.Lock()
	u.failoverActive = on
	u.healthMu.Unlock()
}

func (u *networkUsecase) observeDegraded(up Uplink, cfg HealthConfig, ring *healthRing) {
	samples := ring.snapshot()
	loss, rtt := lossPct(samples, 20), medianRTT(samples, 20)
	limit := cfg.DegradedRTTms[up.Slot]
	degraded := len(samples) >= 20 && (loss >= cfg.DegradedLossPct || (limit > 0 && rtt > limit))
	u.healthMu.Lock()
	u.ensureHealthMaps()
	was := u.degradedNow[up.IfName]
	u.degradedNow[up.IfName] = degraded
	u.healthMu.Unlock()
	if degraded != was {
		u.emit(events.EventWANDegraded, map[string]any{
			"if_name": up.IfName, "slot": string(up.Slot), "entered": degraded,
			"loss_pct": loss, "median_rtt_ms": rtt,
		})
	}
}

func (u *networkUsecase) storeLadder(ctx context.Context, up Uplink, force string, gatewayUp, inetUp, inetKnown bool, results []ProbeResult) {
	carrier := "unknown"
	hasCarrier := true
	if ok, err := u.health.Probe.Carrier(ctx, up.IfName); err == nil {
		hasCarrier = ok
		carrier = "down"
		if ok {
			carrier = "up"
		}
	}
	// A cold damper reads down through its first streak; warm-up, not an outage.
	gwKnown := gatewayUp || u.health.EverUp(up.IfName) ||
		u.tickCount(up.IfName) > u.health.Damping.SuccessesToUp
	layer := func(up bool) string {
		if up {
			return "up"
		}
		return "down"
	}
	gw := "unknown"
	if gwKnown {
		gw = layer(gatewayUp)
	}
	inet := "unknown"
	if inetKnown {
		inet = layer(inetUp)
	}
	u.healthMu.Lock()
	u.ensureHealthMaps()
	degraded := u.degradedNow[up.IfName]
	u.ladders[up.IfName] = uplinkLadder{
		Carrier:  carrier,
		Gateway:  gw,
		Internet: inet,
		Degraded: degraded,
		Verdict:  verdictFor(force, hasCarrier, gwKnown, gatewayUp, inetUp, inetKnown, degraded),
		Results:  results,
	}
	u.healthMu.Unlock()
}

func (u *networkUsecase) tickCount(ifName string) int {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	u.ensureHealthMaps()
	u.bootTicks[ifName]++
	return u.bootTicks[ifName]
}

// Foreign group mark, so each socket walks the same path real traffic takes.
func (u *networkUsecase) probePool(ctx context.Context, cfg HealthConfig) {
	pool := u.vpnPoolNow(ctx)
	want := map[string]bool{}
	for _, t := range pool.Tunnels {
		want[t.IfName] = true
	}
	// Members gone: drop the readings or their cards freeze.
	u.healthMu.Lock()
	u.ensureHealthMaps()
	for name := range u.ladders {
		if system.IsWGLink(name) && !want[name] {
			delete(u.ladders, name)
			delete(u.rings, name)
			delete(u.inetStates, name)
			delete(u.degradedNow, name)
		}
	}
	u.healthMu.Unlock()
	if !pool.Active() || len(cfg.TargetsForeign) == 0 {
		return
	}

	for _, t := range pool.Tunnels {
		results := probeAll(ctx, u.targetProber(), t.IfName,
			netmark.GroupMark(netmark.GroupForeign), cfg.TargetsForeign)
		answered := anyUp(results)
		state := u.inetState(t.IfName)
		up, _ := state.observe(answered, defaultInternetLimits(), time.Now())
		_, everAnswered := state.snapshot()
		u.ring(t.IfName).push(tickSample(time.Now(), results))

		samples := u.ring(t.IfName).snapshot()
		loss, rtt := lossPct(samples, 20), medianRTT(samples, 20)
		limit := cfg.DegradedRTTms[domain.SlotSecondary]
		degraded := len(samples) >= 20 && (loss >= cfg.DegradedLossPct || (limit > 0 && rtt > limit))

		inet := "down"
		if up {
			inet = "up"
		}
		u.healthMu.Lock()
		u.ensureHealthMaps()
		wasDegraded := u.degradedNow[t.IfName]
		u.degradedNow[t.IfName] = degraded
		u.ladders[t.IfName] = uplinkLadder{
			Internet: inet,
			Degraded: degraded,
			Verdict:  tunnelVerdict(everAnswered, up, degraded),
			Results:  results,
		}
		u.healthMu.Unlock()
		if degraded != wasDegraded {
			u.emit(events.EventVPNDegraded, map[string]any{
				"profile_id": t.Profile.ID, "name": t.Profile.Name, "entered": degraded,
				"loss_pct": loss, "median_rtt_ms": rtt,
			})
		}
	}
	_ = u.applyPoolRoutes(ctx)
}

// applyPoolRoutes rewrites the pool defaults from current dampers and roles and
// mirrors them into the domestic table while failover holds.
func (u *networkUsecase) applyPoolRoutes(ctx context.Context) error {
	pool := u.vpnPoolNow(ctx)
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	if err := u.applyVPNRoutes(ctx, pool, uplinks); err != nil {
		return err
	}
	nh := u.currentPoolNexthops()
	u.healthMu.Lock()
	failover := u.failoverActive
	u.healthMu.Unlock()

	// The failover route is a mirror; a pool reshuffle has to reach it too.
	if failover && len(nh) > 0 {
		for _, up := range uplinks {
			if up.Slot == domain.SlotDomestic {
				_ = u.Backend.RouteReplace(ctx, system.Route{
					Table: up.Table, Dest: "default", Nexthops: nh,
				})
			}
		}
	}
	return nil
}
