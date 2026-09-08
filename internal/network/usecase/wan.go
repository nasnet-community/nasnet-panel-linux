package usecase

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

func gatewayOf(row domain.NetworkInterface) string {
	if row.Method == domain.MethodStatic {
		return row.StaticGateway
	}
	return row.LearnedGateway
}

// Health routes are foreign to networkd. Remove routes through the edited
// link before reload so a former static default cannot be mistaken for a new
// DHCP lease. The snapshot covers these tables and restores them on failure.
func (u *networkUsecase) clearWANRoutes(ctx context.Context, ifName string) error {
	slots := append(domain.DomesticSlots(), domain.SecondarySlots()...)
	for _, slot := range slots {
		routes, err := u.Backend.RouteList(ctx, tableFor(slot))
		if err != nil {
			return err
		}
		for _, r := range routes {
			if r.OifName == ifName {
				if err := u.Backend.RouteDel(ctx, r); err != nil {
					return fmt.Errorf("clear previous WAN route: %w", err)
				}
			}
		}
	}
	return nil
}

// A timer rollback happens in another process. Exclude health mutations while
// it is restoring files, database intent and routes.
func (u *networkUsecase) lockHealthNetwork() (func(), error) {
	u.planMu.Lock()
	if u.Paths.StateDir == "" {
		return u.planMu.Unlock, nil
	}
	unlock, err := system.TryNetworkLock(u.Paths)
	if err != nil {
		u.planMu.Unlock()
		return nil, err
	}
	return func() { unlock(); u.planMu.Unlock() }, nil
}
