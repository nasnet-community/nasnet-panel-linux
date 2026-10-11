package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

// CoreAssigner is the tunnel-subsystem port for SSH/MTProto tunnels assigned
// to a subscription. Implemented by *coreusecase.CoreUsecase and wired via
// SetCoreAssigner (after construction, to keep the DI graph acyclic).
type CoreAssigner interface {
	// GetInbound looks up one tunnel (manual create validates before saving).
	GetInbound(ctx context.Context, id uint) (*coreDomain.CoreInbound, error)
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
	// SetKLimitBySubscription applies the subscription's device limit to its
	// tunnel users.
	SetKLimitBySubscription(ctx context.Context, subID uint, kLimit int) error
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
	return pendingOK(err, "AssignToCoreInbound", subscriptionID)
}

// pendingOK treats a node push that failed after the tunnel change was saved
// as success: the node's next reconcile applies it.
func pendingOK(err error, op string, subscriptionID uint) error {
	if errors.Is(err, coreUC.ErrReconcilePending) {
		logger.GetLogger().WithError(err).WithField("subscription_id", subscriptionID).Warnf("[%s] Node not updated yet", op)
		return nil
	}
	return err
}

// RemoveFromCoreInbound removes this subscription's tunnel user on an inbound.
func (u *subscriptionUsecase) RemoveFromCoreInbound(ctx context.Context, subscriptionID, coreInboundID uint) error {
	if u.coreAssigner == nil {
		return fmt.Errorf("tunnel assignment is not available")
	}
	return pendingOK(u.coreAssigner.RemoveSubscriptionFromCoreInbound(ctx, subscriptionID, coreInboundID), "RemoveFromCoreInbound", subscriptionID)
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
	return pendingOK(u.coreAssigner.DeleteUsersBySubscription(ctx, subscriptionID), "DeleteCoreTunnels", subscriptionID)
}

// manualTunnel is a validated tunnel waiting for the new subscription.
type manualTunnel struct {
	inbound  *coreDomain.CoreInbound
	nodeName string
}

// label names the tunnel the way the panel does, e.g. "MTProto tunnel on port 443 (VPS 1)".
func (t manualTunnel) label() string {
	proto := map[coreDomain.CoreProtocol]string{
		coreDomain.CoreProtoMTProto: "MTProto",
		coreDomain.CoreProtoSSH:     "SSH",
	}[t.inbound.Protocol]
	if proto == "" {
		proto = string(t.inbound.Protocol)
	}
	return fmt.Sprintf("%s tunnel on port %d (%s)", proto, t.inbound.Port, t.nodeName)
}

// resolveManualTunnels checks every tunnel before anything is created.
func (u *subscriptionUsecase) resolveManualTunnels(ctx context.Context, ids []uint) ([]manualTunnel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if u.coreAssigner == nil {
		return nil, fmt.Errorf("tunnel assignment is not available")
	}
	seen := make(map[uint]bool, len(ids))
	var out []manualTunnel
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		in, err := u.coreAssigner.GetInbound(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && in == nil) {
			return nil, badRequest(ErrInvalidTunnel, "tunnel %d not found", id)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tunnel %d: %w", id, err)
		}
		if in.IsDisabled {
			return nil, badRequest(ErrInvalidTunnel, "tunnel %d is disabled", id)
		}
		node, err := u.nodeRepo.GetNode(ctx, in.NodeID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to fetch node %d: %w", in.NodeID, err)
		}
		if node == nil || !node.IsActive {
			return nil, badRequest(ErrInvalidTunnel, "tunnel %d belongs to an inactive or missing node", id)
		}
		out = append(out, manualTunnel{inbound: in, nodeName: node.Name})
	}
	return out, nil
}

// attachManualTunnels gives the new subscription its tunnels; failures come
// back as warnings because the subscription already exists.
func (u *subscriptionUsecase) attachManualTunnels(ctx context.Context, subID uint, tunnels []manualTunnel) []string {
	var warnings []string
	for _, t := range tunnels {
		if err := u.AssignToCoreInbound(ctx, subID, t.inbound.ID); err != nil {
			logger.GetLogger().WithError(err).WithField("subscription_id", subID).Warnf("[CreateManual] Tunnel %d not attached", t.inbound.ID)
			warnings = append(warnings, fmt.Sprintf("%s was not attached: %v", t.label(), err))
		}
	}
	return warnings
}

// tunnelOnlyConfig is the raw /sub body of a sub with no Xray configs: its tunnel links, one per line.
func (u *subscriptionUsecase) tunnelOnlyConfig(ctx context.Context, sub *domain.Subscription) (*SubscriptionConfigResult, error) {
	// Like the sub page: no tunnel links once access has run out.
	if u.coreAssigner == nil || !sub.GrantsAccess() {
		return nil, ErrNoNodesAvailable
	}
	arts, err := u.GetCoreArtifacts(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, a := range arts {
		if a == nil {
			continue
		}
		for _, line := range strings.Split(a.Content, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				links = append(links, line)
			}
		}
	}
	if len(links) == 0 {
		return nil, ErrNoNodesAvailable
	}

	planName := sub.Label
	if planName == "" {
		planName = "Manual"
	}
	result := &SubscriptionConfigResult{
		Config:    strings.Join(links, "\n"),
		DataUsed:  sub.DataUsed,
		DataLimit: sub.GetEffectiveDataLimit(),
		PlanName:  planName,
	}
	if effectiveEnd := sub.GetEffectiveEndDate(); effectiveEnd != nil {
		result.ExpiresAt = effectiveEnd
	}
	return result, nil
}
