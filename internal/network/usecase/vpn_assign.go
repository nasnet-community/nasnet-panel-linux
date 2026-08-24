package usecase

import (
	"context"
	"errors"
	"sort"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// assignTransport deals the pool across the secondaries. Pure, so the kernel,
// the UI and the API cannot drift apart on who rides what.
//
// A pin rides its WAN even when that WAN is down — one that silently moves is
// not a pin. With no healthy WAN the deal still names one: a tunnel with no
// mark has no route out, and its recovery would be unobservable.
func assignTransport(pool vpnPool, secondaries []Uplink, healthyByIf map[string]bool) map[string]Uplink {
	out := make(map[string]Uplink, len(pool.Tunnels))
	if len(secondaries) == 0 {
		return out
	}

	ordered := append([]Uplink(nil), secondaries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })

	byKey := make(map[string]Uplink, len(ordered))
	var alive []Uplink
	for _, s := range ordered {
		byKey[s.Key] = s
		if healthyByIf[s.IfName] {
			alive = append(alive, s)
		}
	}
	if len(alive) == 0 {
		alive = ordered
	}

	// vpnPoolRead sorts by wg slot, which is what makes the deal reproducible.
	next := 0
	for _, t := range pool.Tunnels {
		if key := t.Profile.TransportUplink; key != "" {
			if wan, ok := byKey[key]; ok {
				out[t.IfName] = wan
				continue
			}
			// Pinned row is gone; auto is the only honest fallback.
		}
		out[t.IfName] = alive[next%len(alive)]
		next++
	}
	return out
}

func secondariesOf(uplinks []Uplink) []Uplink {
	var out []Uplink
	for _, up := range uplinks {
		if up.Slot.IsSecondary() {
			out = append(out, up)
		}
	}
	return out
}

// healthySecondaries reads the same dampers the pool trusts. No damper yet
// means healthy: the damper starts optimistic.
func (u *networkUsecase) healthySecondaries(uplinks []Uplink) map[string]bool {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	u.ensureHealthMaps()
	out := map[string]bool{}
	for _, up := range uplinks {
		if !up.Slot.IsSecondary() {
			continue
		}
		healthy := true
		if s, ok := u.inetStates[up.IfName]; ok {
			down, _ := s.snapshot()
			healthy = !down
		}
		out[up.IfName] = healthy
	}
	return out
}

// applyTransportAssignments re-deals after the dampers move and re-marks only
// what changed. Ensure is idempotent, so a moved tunnel keeps its device and
// its escape hatch and merely re-handshakes out the new WAN.
func (u *networkUsecase) applyTransportAssignments(ctx context.Context) error {
	pool, err := u.vpnPoolRead(ctx)
	if err != nil || !pool.Active() {
		return err
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	deal := assignTransport(pool, secondariesOf(uplinks), u.healthySecondaries(uplinks))

	u.healthMu.Lock()
	if u.lastTransport == nil {
		u.lastTransport = map[string]string{}
	}
	prev := make(map[string]string, len(u.lastTransport))
	for k, v := range u.lastTransport {
		prev[k] = v
	}
	for ifName, wan := range deal {
		u.lastTransport[ifName] = wan.IfName
	}
	u.healthMu.Unlock()

	var errs []error
	for _, t := range pool.Tunnels {
		wan := deal[t.IfName]
		if prev[t.IfName] == wan.IfName {
			continue
		}
		apply, err := wgApplyConfig(t.Config, transportMark(wan))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := u.wg().Ensure(ctx, t.IfName, apply); err != nil {
			errs = append(errs, err)
			continue
		}
		// Nothing to announce the first time: that is the boot, not a move.
		if prev[t.IfName] != "" {
			u.emit(events.EventVPNTunnelRehomed, map[string]any{
				"name": t.Profile.Name, "if_name": t.IfName,
				"from": prev[t.IfName], "to": wan.IfName,
			})
		}
	}
	return errors.Join(errs...)
}

// rememberTransport records a deal applyVPNDevices already wrote, so the next
// tick sees no change and leaves the tunnels alone.
func (u *networkUsecase) rememberTransport(deal map[string]Uplink) {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	if u.lastTransport == nil {
		u.lastTransport = map[string]string{}
	}
	for ifName, wan := range deal {
		u.lastTransport[ifName] = wan.IfName
	}
}
