package usecase

import (
	"context"
	"errors"
	"fmt"

	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// CoreAssigner is the tunnel-subsystem port for SSH/MTProto tunnels assigned
// to a subscription. Implemented by *coreusecase.CoreUsecase and wired via
// SetCoreAssigner (after construction, to keep the DI graph acyclic).
type CoreAssigner interface {
	AssignSubscriptionToCoreInbound(ctx context.Context, subID uint, email string, kLimit int, coreInboundID uint) error
	RemoveSubscriptionFromCoreInbound(ctx context.Context, subID, coreInboundID uint) error
	ListSubscriptionCoreAccounts(ctx context.Context, subID uint) ([]coreUC.SubscriptionCoreAccount, error)
	// GenerateSubscriptionArtifacts renders the subscription's tunnels as
	// client links.
	GenerateSubscriptionArtifacts(ctx context.Context, subID uint) ([]*coreUC.ClientArtifact, error)
	// ResetData zeroes per-tunnel usage for the subscription (kept in step
	// with sub.DataUsed resets).
	ResetData(ctx context.Context, subID uint) error
	// Enable/DisableUsersBySubscription flip the subscription's tunnel users
	// and reconcile the affected nodes.
	EnableUsersBySubscription(ctx context.Context, subID uint) error
	DisableUsersBySubscription(ctx context.Context, subID uint) error
	// DeleteUsersBySubscription removes the subscription's tunnel users.
	DeleteUsersBySubscription(ctx context.Context, subID uint) error
	// RebindSubscription moves the subscription's tunnel users to a new email
	// and rotates their secrets.
	RebindSubscription(ctx context.Context, subID uint, email string) error
}

// SetCoreAssigner wires the tunnel usecase.
func (u *subscriptionUsecase) SetCoreAssigner(ca CoreAssigner) { u.coreAssigner = ca }

// AssignToCoreInbound provisions a tunnel user for this subscription on a
// tunnel inbound, keyed by the subscription's email, and reconciles the node.
// The device limit follows the subscription's when it sets one.
func (u *subscriptionUsecase) AssignToCoreInbound(ctx context.Context, subscriptionID, coreInboundID uint) error {
	if u.coreAssigner == nil {
		return fmt.Errorf("tunnel assignment is not available")
	}
	sub, err := u.GetByID(ctx, subscriptionID)
	if err != nil {
		return err
	}
	if sub.ConfigEmail == "" {
		return fmt.Errorf("subscription %d has no config email to bind the tunnel user", subscriptionID)
	}
	err = u.coreAssigner.AssignSubscriptionToCoreInbound(ctx, subscriptionID, sub.ConfigEmail, sub.MaxDevices, coreInboundID)
	// A new user is created enabled; a subscription without access must not
	// hand out a live tunnel.
	if (err == nil || errors.Is(err, coreUC.ErrReconcilePending)) && !sub.GrantsAccess() {
		err = u.coreAssigner.DisableUsersBySubscription(ctx, subscriptionID)
	}
	if errors.Is(err, coreUC.ErrReconcilePending) {
		// Saved; the node picks the tunnel up on its next reconcile.
		logger.GetLogger().WithError(err).WithField("subscription_id", subscriptionID).Warn("[AssignToCoreInbound] Node not updated yet")
		return nil
	}
	return err
}

// RemoveFromCoreInbound removes this subscription's tunnel user on an inbound.
func (u *subscriptionUsecase) RemoveFromCoreInbound(ctx context.Context, subscriptionID, coreInboundID uint) error {
	if u.coreAssigner == nil {
		return fmt.Errorf("tunnel assignment is not available")
	}
	return u.coreAssigner.RemoveSubscriptionFromCoreInbound(ctx, subscriptionID, coreInboundID)
}

// ListCoreAccounts returns the subscription's assigned tunnels.
func (u *subscriptionUsecase) ListCoreAccounts(ctx context.Context, subscriptionID uint) ([]coreUC.SubscriptionCoreAccount, error) {
	if u.coreAssigner == nil {
		return nil, nil
	}
	return u.coreAssigner.ListSubscriptionCoreAccounts(ctx, subscriptionID)
}

// GetCoreArtifacts returns the subscription's tunnels rendered as client links
// for the public sub page.
func (u *subscriptionUsecase) GetCoreArtifacts(ctx context.Context, subscriptionID uint) ([]*coreUC.ClientArtifact, error) {
	if u.coreAssigner == nil {
		return nil, nil
	}
	return u.coreAssigner.GenerateSubscriptionArtifacts(ctx, subscriptionID)
}

// DeleteCoreTunnels removes the subscription's tunnel users (the subscription
// is being deleted).
func (u *subscriptionUsecase) DeleteCoreTunnels(ctx context.Context, subscriptionID uint) error {
	if u.coreAssigner == nil {
		return nil
	}
	err := u.coreAssigner.DeleteUsersBySubscription(ctx, subscriptionID)
	if errors.Is(err, coreUC.ErrReconcilePending) {
		// Deleted; the node's next reconcile tears the listeners' users down.
		logger.GetLogger().WithError(err).WithField("subscription_id", subscriptionID).Warn("[DeleteCoreTunnels] Node not updated yet")
		return nil
	}
	return err
}
