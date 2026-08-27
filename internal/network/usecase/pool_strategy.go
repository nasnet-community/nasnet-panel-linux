package usecase

import (
	"sort"
)

// PoolStrategy is how traffic uses the pool. One choice for the whole pool.
type PoolStrategy string

const (
	// Every healthy tunnel at once; per-flow hashing keeps a connection put.
	StrategySpread PoolStrategy = "spread"
	// A failover chain in the operator's order. Failback is automatic.
	StrategyOrder PoolStrategy = "order"
	// The lowest-RTT tunnel, re-elected as the measurements move.
	StrategyFastest PoolStrategy = "fastest"
)

// Router-prefixed, so saving it reloads the health config.
const PoolStrategyKey = "router_vpn_pool_strategy"

// An unconfigured pool spreads: nothing the operator enabled sits idle.
const DefaultPoolStrategy = StrategySpread

// False for anything else, including the empty string a fresh install stores.
func ParsePoolStrategy(s string) (PoolStrategy, bool) {
	switch PoolStrategy(s) {
	case StrategySpread, StrategyOrder, StrategyFastest:
		return PoolStrategy(s), true
	}
	return DefaultPoolStrategy, false
}

// Only these can announce a carrier move; spread has no carrier to move.
func (s PoolStrategy) SingleCarrier() bool {
	return s == StrategyOrder || s == StrategyFastest
}

// How much better a challenger must be, and for how long, before traffic moves.
const (
	fastestMarginMs  = 20
	fastestMarginPct = 25
	fastestHoldTicks = 3
)

// Both bars or neither: 5 ms off a satellite link is noise, and so is 30% of 3 ms.
func betterByMargin(challenger, carrier int) bool {
	if challenger <= 0 || carrier <= 0 {
		return false
	}
	return carrier-challenger >= fastestMarginMs &&
		(carrier-challenger)*100 >= carrier*fastestMarginPct
}

// Which members carry right now. sticky is the elected carrier, which only the
// fastest strategy reads. With nothing healthy it still names one: a dead
// tunnel beats a blackhole, and the probes need the path to see the recovery.
func carriersFor(members []poolMember, strategy PoolStrategy, sticky string) []poolMember {
	if len(members) == 0 {
		return nil
	}
	ordered := append([]poolMember(nil), members...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return ordered[i].Slot < ordered[j].Slot
	})

	var healthy []poolMember
	for _, m := range ordered {
		if m.Healthy {
			healthy = append(healthy, m)
		}
	}

	switch strategy {
	case StrategyOrder:
		if len(healthy) > 0 {
			return healthy[:1]
		}
		return ordered[:1]

	case StrategyFastest:
		if len(healthy) == 0 {
			return ordered[:1]
		}
		for _, m := range healthy {
			if m.IfName == sticky {
				return []poolMember{m}
			}
		}
		return []poolMember{fastestOf(healthy)}

	default: // StrategySpread
		if len(healthy) > 0 {
			return healthy
		}
		return ordered
	}
}

// First in order when nothing is measured yet: a pool carries before it is timed.
func fastestOf(healthy []poolMember) poolMember {
	best := healthy[0]
	for _, m := range healthy[1:] {
		if m.RTTms <= 0 {
			continue
		}
		if best.RTTms <= 0 || m.RTTms < best.RTTms {
			best = m
		}
	}
	return best
}

// One announced move. The three reasons read very differently to an operator.
type carrierSwitch struct {
	From    string
	To      string
	Reason  string // "failover", "failback" or "faster"
	FromRTT int
	ToRTT   int
}

// Decides who carries next, holding the fastest strategy's choice until a
// challenger earns it. Nil when nothing changed; a first election is the boot,
// not a failover.
func (u *networkUsecase) electCarrier(members []poolMember, strategy PoolStrategy) *carrierSwitch {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()

	if !strategy.SingleCarrier() || len(members) == 0 {
		u.poolCarrier, u.poolChallenger, u.poolChallengeTicks = "", "", 0
		return nil
	}

	prev := u.poolCarrier
	prevRTT := 0
	byName := make(map[string]poolMember, len(members))
	for _, m := range members {
		byName[m.IfName] = m
	}
	if m, ok := byName[prev]; ok {
		prevRTT = m.RTTms
	}

	var want poolMember
	reason := "failover"
	switch strategy {
	case StrategyOrder:
		want = carriersFor(members, strategy, "")[0]
		if cur, ok := byName[prev]; ok && want.Priority < cur.Priority {
			reason = "failback"
		}
	case StrategyFastest:
		want = u.electFastest(members, byName, prev)
		if want.IfName == prev {
			return nil
		}
		if cur, ok := byName[prev]; ok && cur.Healthy {
			reason = "faster"
		}
	}

	if want.IfName == prev {
		return nil
	}
	u.poolCarrier = want.IfName
	u.poolChallenger, u.poolChallengeTicks = "", 0
	if prev == "" {
		return nil
	}
	return &carrierSwitch{
		From: prev, To: want.IfName, Reason: reason,
		FromRTT: prevRTT, ToRTT: want.RTTms,
	}
}

// Holds the current carrier unless it is gone or beaten. Callers hold healthMu.
func (u *networkUsecase) electFastest(members []poolMember,
	byName map[string]poolMember, prev string) poolMember {

	cur, held := byName[prev]
	if !held || !cur.Healthy {
		// Nothing to defend, so take the best there is.
		u.poolChallenger, u.poolChallengeTicks = "", 0
		return carriersFor(members, StrategyFastest, "")[0]
	}

	var healthy []poolMember
	for _, m := range members {
		if m.Healthy && m.IfName != prev {
			healthy = append(healthy, m)
		}
	}
	if len(healthy) == 0 || cur.RTTms <= 0 {
		// An unmeasured carrier is not evidence of a slow one.
		u.poolChallenger, u.poolChallengeTicks = "", 0
		return cur
	}

	best := fastestOf(healthy)
	if !betterByMargin(best.RTTms, cur.RTTms) {
		u.poolChallenger, u.poolChallengeTicks = "", 0
		return cur
	}
	if u.poolChallenger != best.IfName {
		u.poolChallenger, u.poolChallengeTicks = best.IfName, 1
		return cur
	}
	u.poolChallengeTicks++
	if u.poolChallengeTicks < fastestHoldTicks {
		return cur
	}
	return best
}

// Empty under spread, where every healthy member carries.
func (u *networkUsecase) poolCarrierNow() string {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	return u.poolCarrier
}

// Rides in the health config with the router's other settings-backed knobs.
func (u *networkUsecase) poolStrategyNow() PoolStrategy {
	return u.healthConfigSnapshot().PoolStrategy
}
