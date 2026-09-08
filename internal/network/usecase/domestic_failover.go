package usecase

import "sort"

// Where one domestic table's default points this tick.
type domesticVia int

const (
	viaOwn     domesticVia = iota // its own gateway
	viaSibling                    // a healthy domestic's gateway, own kept for the probe
	viaPool                       // the VPN pool's nexthop set
	viaNone                       // withdrawn; the group rule walks on
)

// One domestic line as the tick measured it.
type domesticObs struct {
	Up         Uplink
	Gateway    string // own gateway, "" while unknown
	GatewayUp  bool
	InternetUp bool
	ForcedDown bool // persisted operator intent, including before the first healthy tick
}

type domesticRoute struct {
	Up         Uplink
	Gateway    string
	Via        domesticVia
	ForcedDown bool
	// Sibling is set only for viaSibling.
	Sibling domesticObs
}

func (o domesticObs) healthy() bool { return o.GatewayUp && o.InternetUp }

// The whole domestic failover policy, pure so tests can enumerate it. Slot
// order is priority: the first healthy line carries every dead one, the pool
// carries them all when none is healthy.
func domesticRoutePlan(obs []domesticObs, failoverOn, vpnUp bool) []domesticRoute {
	ordered := append([]domesticObs(nil), obs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Up.UplinkIndex < ordered[j].Up.UplinkIndex })

	// A mirror needs a nexthop to write, so an unlearned gateway disqualifies.
	var best *domesticObs
	for i := range ordered {
		if ordered[i].healthy() && ordered[i].Gateway != "" {
			best = &ordered[i]
			break
		}
	}

	out := make([]domesticRoute, 0, len(ordered))
	for _, o := range ordered {
		r := domesticRoute{Up: o.Up, Gateway: o.Gateway, ForcedDown: o.ForcedDown}
		switch {
		case o.healthy():
			r.Via = viaOwn
		case best != nil && o.GatewayUp:
			r.Via, r.Sibling = viaSibling, *best
		case best != nil:
			// No gateway means no probe path to keep; withdraw and let the
			// group rule find the sibling's table itself.
			r.Via = viaNone
		case failoverOn && vpnUp:
			r.Via = viaPool
		case o.GatewayUp:
			// Gateway alive, nothing better: keep the route, or the probe
			// can't see the recovery.
			r.Via = viaOwn
		default:
			r.Via = viaNone
		}
		out = append(out, r)
	}
	return out
}
