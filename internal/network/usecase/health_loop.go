package usecase

import (
	"context"
	"errors"
	"fmt"
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

func (u *networkUsecase) vpnConnectedNow(ctx context.Context) bool {
	if !u.vpnPlaneNow(ctx).Active() {
		return false
	}
	st, err := u.wg().Status(ctx)
	return err == nil && st.Connected()
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
		if err := u.Backend.RouteReplace(ctx, system.Route{
			Table: up.Table, Dest: "default", OifName: system.WGLinkName,
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
				"if_name": up.IfName, "to": system.WGLinkName})
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

// Foreign group mark, so the socket walks the same path real traffic takes.
func (u *networkUsecase) probeTunnel(ctx context.Context, cfg HealthConfig) {
	if !u.vpnPlaneNow(ctx).Active() || len(cfg.TargetsForeign) == 0 {
		// Tunnel gone: drop the readings or the card freezes.
		u.healthMu.Lock()
		u.ensureHealthMaps()
		delete(u.ladders, system.WGLinkName)
		delete(u.rings, system.WGLinkName)
		u.healthMu.Unlock()
		return
	}
	results := probeAll(ctx, u.targetProber(), system.WGLinkName,
		netmark.GroupMark(netmark.GroupForeign), cfg.TargetsForeign)
	u.ring(system.WGLinkName).push(tickSample(time.Now(), results))
	u.healthMu.Lock()
	u.ensureHealthMaps()
	u.ladders[system.WGLinkName] = uplinkLadder{Results: results}
	u.healthMu.Unlock()
}
