package usecase

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// WGAccess is the wireguard-subsystem port for revoking/restoring a
// subscription's peers. Implemented by the wireguard device usecase and wired
// via SetWGAccess (post-construction, to keep the DI graph acyclic).
type WGAccess interface {
	ActivateSubscription(ctx context.Context, subID uint) error
	DeactivateSubscription(ctx context.Context, subID uint) error
}

// SetWGAccess wires the wireguard peer enable/disable port.
func (u *subscriptionUsecase) SetWGAccess(w WGAccess) { u.wgAccess = w }

// setTunnelAccess flips every access artifact a subscription owns: its Xray
// accounts AND its WireGuard peers.
//
// The two live in different tables and used to be toggled by different code
// paths — Xray accounts directly through accountManager, WG peers only from
// inside the product provider's Activate/DeactivateUser. Anything that revoked
// a subscription without going through a provider therefore left its peers
// live, and an admin pause or revoke does exactly that. Every lifecycle
// transition now routes through here, so "revoke this subscription" means all
// of it.
//
// Both halves are attempted even if the first fails; the worst outcome of a
// partial failure is drift, which the node push re-derives from subscription
// status anyway (see the wireguard PeerSource access resolver and the account
// gate in the node usecase).
func (u *subscriptionUsecase) setTunnelAccess(ctx context.Context, subID uint, enabled bool) error {
	log := logger.GetLogger()
	var firstErr error
	fail := func(err error, what string) {
		if err == nil {
			return
		}
		log.WithError(err).WithFields(map[string]interface{}{
			"subscription_id": subID,
			"enabled":         enabled,
		}).Warnf("[setTunnelAccess] Failed to update %s", what)
		if firstErr == nil {
			firstErr = err
		}
	}

	if u.accountManager != nil {
		if enabled {
			fail(u.accountManager.EnableAccountsBySubscription(ctx, subID), "xray accounts")
		} else {
			fail(u.accountManager.DisableAccountsBySubscription(ctx, subID), "xray accounts")
		}
	}
	if u.wgAccess != nil {
		if enabled {
			fail(u.wgAccess.ActivateSubscription(ctx, subID), "wireguard peers")
		} else {
			fail(u.wgAccess.DeactivateSubscription(ctx, subID), "wireguard peers")
		}
	}
	return firstErr
}

// SetTunnelAccess is setTunnelAccess exposed for callers outside this package
// (the admin usecase's pause / resume / revoke), so they cannot accidentally
// revoke only half of a subscription's access.
func (u *subscriptionUsecase) SetTunnelAccess(ctx context.Context, subID uint, enabled bool) error {
	return u.setTunnelAccess(ctx, subID, enabled)
}
