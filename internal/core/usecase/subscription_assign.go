package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
)

// SubscriptionCoreAccount is a view of one core_user provisioned for a
// subscription, joined with its inbound — for the panel's ACCOUNTS list.
type SubscriptionCoreAccount struct {
	CoreUserID    uint   `json:"core_user_id"`
	CoreInboundID uint   `json:"core_inbound_id"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	Remark        string `json:"remark"`
	NodeID        uint   `json:"node_id"`
	Email         string `json:"email"`
	Enabled       bool   `json:"enabled"`
}

// AssignSubscriptionToCoreInbound provisions a core_user for the subscription on
// one core inbound (idempotent — reuses an existing user matched by email) and
// reconciles the node so the core picks it up. kLimit 0 keeps the default.
func (u *CoreUsecase) AssignSubscriptionToCoreInbound(ctx context.Context, subID uint, email string, kLimit int, coreInboundID uint) error {
	if subID == 0 || email == "" {
		return fmt.Errorf("core: assign requires a subscription id and email")
	}
	_, provErr := u.ProvisionUserForSubscription(ctx, subID, email, kLimit, []uint{coreInboundID})
	if provErr != nil && !errors.Is(provErr, ErrReconcilePending) {
		return provErr
	}
	// Ensure subscription_id is stamped even if the user pre-existed by email.
	if err := u.BindUsersToSubscription(ctx, email, subID); err != nil {
		return err
	}
	return provErr
}

// RemoveSubscriptionFromCoreInbound deletes the subscription's core_user on one
// inbound and reconciles the node (sweep-kills any live session).
func (u *CoreUsecase) RemoveSubscriptionFromCoreInbound(ctx context.Context, subID, coreInboundID uint) error {
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	nodeSet := map[uint]struct{}{}
	removed := false
	for _, cu := range users {
		if cu.CoreInboundID != coreInboundID {
			continue
		}
		if in, ierr := u.repo.FindInboundByID(ctx, cu.CoreInboundID); ierr == nil {
			nodeSet[in.NodeID] = struct{}{}
		}
		if err := u.repo.DeleteUser(ctx, cu.ID); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return u.reconcileNodeSet(ctx, nodeSet)
}

// ListSubscriptionCoreAccounts returns the subscription's core_users joined with
// their inbounds, for the panel accounts view.
func (u *CoreUsecase) ListSubscriptionCoreAccounts(ctx context.Context, subID uint) ([]SubscriptionCoreAccount, error) {
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return nil, err
	}
	out := make([]SubscriptionCoreAccount, 0, len(users))
	for _, cu := range users {
		acc := SubscriptionCoreAccount{
			CoreUserID:    cu.ID,
			CoreInboundID: cu.CoreInboundID,
			Email:         cu.Email,
			Enabled:       cu.Enabled,
		}
		if in, ierr := u.repo.FindInboundByID(ctx, cu.CoreInboundID); ierr == nil {
			acc.Protocol = string(in.Protocol)
			acc.Port = in.Port
			acc.Remark = in.Remark
			acc.NodeID = in.NodeID
		}
		out = append(out, acc)
	}
	return out, nil
}

// RebindSubscription moves every tunnel user of a subscription to a new email
// (the subscription's ConfigEmail changed) and rotates each secret, so the
// old credentials stop working. The affected nodes are reconciled.
func (u *CoreUsecase) RebindSubscription(ctx context.Context, subID uint, email string) error {
	if email == "" {
		return fmt.Errorf("core: rebind requires an email")
	}
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	for _, cu := range users {
		cu.Email = email
		cu.Secret = randToken(16)
		if err := u.repo.UpdateUser(ctx, cu); err != nil {
			return err
		}
	}
	nodeSet, err := u.nodeSetForUsers(ctx, users)
	if err != nil {
		return err
	}
	return u.reconcileNodeSet(ctx, nodeSet)
}

// SetKLimitBySubscription applies a subscription's device limit to all of its
// tunnel users (0 keeps the default of one device) and reconciles the nodes.
func (u *CoreUsecase) SetKLimitBySubscription(ctx context.Context, subID uint, kLimit int) error {
	if kLimit < 1 {
		kLimit = 1
	}
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	var changed []*domain.CoreUser
	for _, cu := range users {
		if cu.KLimit == kLimit {
			continue
		}
		cu.KLimit = kLimit
		if err := u.repo.UpdateUser(ctx, cu); err != nil {
			return err
		}
		changed = append(changed, cu)
	}
	if len(changed) == 0 {
		return nil
	}
	nodeSet, err := u.nodeSetForUsers(ctx, changed)
	if err != nil {
		return err
	}
	return u.reconcileNodeSet(ctx, nodeSet)
}
