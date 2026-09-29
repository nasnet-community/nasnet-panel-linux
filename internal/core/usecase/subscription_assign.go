package usecase

import (
	"context"
	"fmt"
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
	if _, err := u.ProvisionUserForSubscription(ctx, subID, email, kLimit, []uint{coreInboundID}); err != nil {
		return err
	}
	// Ensure subscription_id is stamped even if the user pre-existed by email.
	return u.BindUsersToSubscription(ctx, email, subID)
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
