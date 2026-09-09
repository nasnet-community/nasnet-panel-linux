package usecase

import (
	"context"
	"errors"
	"fmt"
)

// Nil storage still takes a strategy; it just forgets it at the next boot.
type PoolSettings interface {
	Set(ctx context.Context, key, value string) error
}

// Instant like a pin: the worst it does is move traffic between trusted tunnels.
func (u *networkUsecase) SetPoolStrategy(ctx context.Context, strategy string) error {
	s, ok := ParsePoolStrategy(strategy)
	if !ok {
		return fmt.Errorf("%w: %q is not a way to use the pool", ErrValidationFailed, strategy)
	}
	if u.PoolSettings != nil {
		if err := u.PoolSettings.Set(ctx, PoolStrategyKey, string(s)); err != nil {
			return err
		}
	}
	// The settings callback is boot-time wiring; a control that waits reads broken.
	u.healthMu.Lock()
	if u.healthCfg.DegradedRTTms == nil {
		u.healthCfg = DefaultHealthConfig()
	}
	u.healthCfg.PoolStrategy = s
	// The old carrier belongs to the old strategy.
	u.poolCarrier, u.poolChallenger, u.poolChallengeTicks = "", "", 0
	u.healthMu.Unlock()

	pool := u.vpnPoolNow(ctx)
	u.announceCarrier(u.electCarrier(u.poolMembers(pool), s), pool)
	return u.applyPoolRoutes(ctx)
}

// The drag order, first to last. Stored under every strategy, so switching to
// the chain finds the order the operator left behind.
func (u *networkUsecase) SetPoolOrder(ctx context.Context, ids []uint) error {
	if u.VPNRepo == nil {
		return errors.New("no VPN storage configured")
	}
	enabled, err := u.VPNRepo.Enabled(ctx)
	if err != nil {
		return err
	}
	known := make(map[uint]bool, len(enabled))
	for i := range enabled {
		known[enabled[i].ID] = true
	}
	seen := make(map[uint]bool, len(ids))
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("%w: %d is not in the pool", ErrValidationFailed, id)
		}
		if seen[id] {
			return fmt.Errorf("%w: %d is listed twice", ErrValidationFailed, id)
		}
		seen[id] = true
	}
	if len(ids) != len(enabled) {
		// A partial order leaves the rest sharing a position.
		return fmt.Errorf("%w: the order must name every tunnel in the pool", ErrValidationFailed)
	}
	if err := u.VPNRepo.SetOrder(ctx, ids); err != nil {
		return err
	}
	return u.applyPoolRoutes(ctx)
}

// Puts one profile last, where a tunnel joining the pool belongs.
func (u *networkUsecase) appendToChain(ctx context.Context, id uint) error {
	enabled, err := u.VPNRepo.Enabled(ctx)
	if err != nil {
		return err
	}
	last := -1
	for i := range enabled {
		if enabled[i].ID != id && enabled[i].Priority > last {
			last = enabled[i].Priority
		}
	}
	return u.VPNRepo.SetRole(ctx, id, last+1, 1)
}
