package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
)

// ErrHostUnavailable is returned when a node host resolver is needed (to build a
// subscription client artifact) but none was wired.
var ErrHostUnavailable = errors.New("core: node host resolver not configured")

// ErrNoPublicAddress is returned when a node has no public address to put in
// a link (the built-in node on loopback with no APP_BASE_URL).
var ErrNoPublicAddress = errors.New("core: node has no public address")

// ErrReconcilePending wraps a failed node push after the database change it
// followed was saved: the next reconcile of that node retries it.
var ErrReconcilePending = errors.New("core: saved, node not updated yet")

// NodeHostResolver returns a node's public host/address for client links.
type NodeHostResolver func(ctx context.Context, nodeID uint) (string, error)

// WithNodeHostResolver wires the node-host lookup used to render subscription
// client artifacts (mirrors the coreHostFn closure in the HTTP transport).
func WithNodeHostResolver(fn NodeHostResolver) Option {
	return func(u *CoreUsecase) { u.hostResolve = fn }
}

// ResolveNodeHost resolves a node's public host via the wired resolver.
func (u *CoreUsecase) ResolveNodeHost(ctx context.Context, nodeID uint) (string, error) {
	if u.hostResolve == nil {
		return "", ErrHostUnavailable
	}
	return u.hostResolve(ctx, nodeID)
}

// NodeNameResolver returns a node's display name for labeling client artifacts.
type NodeNameResolver func(ctx context.Context, nodeID uint) (string, error)

// WithNodeNameResolver wires the node display-name lookup used to label
// per-node config files in a multi-inbound subscription. Optional — when unset,
// artifacts fall back to the node host as the label.
func WithNodeNameResolver(fn NodeNameResolver) Option {
	return func(u *CoreUsecase) { u.nodeNameResolve = fn }
}

// resolveNodeLabel returns the node's display name if a resolver is wired and
// succeeds, otherwise the provided host fallback.
func (u *CoreUsecase) resolveNodeLabel(ctx context.Context, nodeID uint, hostFallback string) string {
	if u.nodeNameResolve != nil {
		if name, err := u.nodeNameResolve(ctx, nodeID); err == nil && name != "" {
			return name
		}
	}
	return hostFallback
}

// --- Subscription tie-in ----------------------------------------------------
//
// A subscription may span several inbounds on several nodes, so every mutating
// helper collects the distinct affected nodes and reconciles each once.

// ProvisionUserForSubscription creates (or reuses) one core_user per target
// inbound, stamped with the subscription's email, then reconciles the affected
// nodes. Idempotent: re-running reuses the existing user on an inbound
// (matched by email) instead of duplicating it. kLimit 0 keeps the default.
func (u *CoreUsecase) ProvisionUserForSubscription(ctx context.Context, subID uint, email string, kLimit int, inboundIDs []uint) ([]*domain.CoreUser, error) {
	if email == "" {
		return nil, fmt.Errorf("core: provision requires the subscription's email")
	}
	existing, err := u.repo.FindUsersByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	byInbound := make(map[uint]*domain.CoreUser, len(existing))
	for _, cu := range existing {
		byInbound[cu.CoreInboundID] = cu
	}

	var users []*domain.CoreUser
	nodeSet := map[uint]struct{}{}
	for _, inboundID := range inboundIDs {
		in, err := u.repo.FindInboundByID(ctx, inboundID)
		if err != nil {
			return nil, fmt.Errorf("core: placement inbound %d: %w", inboundID, err)
		}
		if cu, ok := byInbound[inboundID]; ok {
			users = append(users, cu) // already provisioned — reuse
			nodeSet[in.NodeID] = struct{}{}
			continue
		}
		cu := &domain.CoreUser{
			CoreInboundID: inboundID,
			Email:         email,
			KLimit:        kLimit,
		}
		if subID != 0 {
			subIDCopy := subID
			cu.SubscriptionID = &subIDCopy
		}
		if err := u.CreateUser(ctx, cu); err != nil {
			return nil, fmt.Errorf("core: provision user on inbound %d: %w", inboundID, err)
		}
		users = append(users, cu)
		nodeSet[in.NodeID] = struct{}{}
	}

	if err := u.reconcileNodeSet(ctx, nodeSet); err != nil {
		// Users are persisted; a reconcile failure is surfaced but non-fatal
		// (the background reconcile / next mutation will retry the push).
		return users, err
	}
	return users, nil
}

// BindUsersToSubscription stamps subscription_id onto the core_users provisioned
// under the subscription's email (a user an admin created by hand with that
// email is adopted). Only unbound rows are updated, so a re-run is a no-op.
// Reconcile is not needed — subscription_id is hub-side bookkeeping the agent
// never sees.
func (u *CoreUsecase) BindUsersToSubscription(ctx context.Context, email string, subID uint) error {
	if email == "" || subID == 0 {
		return fmt.Errorf("core: bind requires email and subscription id")
	}
	users, err := u.repo.FindUsersByEmail(ctx, email)
	if err != nil {
		return err
	}
	for _, cu := range users {
		if cu.SubscriptionID != nil && *cu.SubscriptionID == subID {
			continue
		}
		subIDCopy := subID
		cu.SubscriptionID = &subIDCopy
		if err := u.repo.UpdateUser(ctx, cu); err != nil {
			return err
		}
	}
	return nil
}

// ListUsersBySubscription returns the core_users provisioned for a subscription.
func (u *CoreUsecase) ListUsersBySubscription(ctx context.Context, subID uint) ([]*domain.CoreUser, error) {
	return u.repo.FindUsersBySubscription(ctx, subID)
}

// ResetData zeroes the per-tunnel DataUsed counter for all of a subscription's
// core_users. Called from subscriptionUsecase.ResetDataUsed so per-tunnel usage
// stays in step with sub.DataUsed across renewals and admin resets.
func (u *CoreUsecase) ResetData(ctx context.Context, subID uint) error {
	return u.repo.ResetDataBySubscription(ctx, subID)
}

// EnableUsersBySubscription enables all of a subscription's core_users (renewal /
// reactivation) and reconciles the affected nodes.
func (u *CoreUsecase) EnableUsersBySubscription(ctx context.Context, subID uint) error {
	return u.setUsersEnabledBySubscription(ctx, subID, true)
}

// DisableUsersBySubscription disables all of a subscription's core_users (expiry /
// quota / pause) and reconciles the affected nodes so the agent sweep-kills them.
func (u *CoreUsecase) DisableUsersBySubscription(ctx context.Context, subID uint) error {
	return u.setUsersEnabledBySubscription(ctx, subID, false)
}

func (u *CoreUsecase) setUsersEnabledBySubscription(ctx context.Context, subID uint, enabled bool) error {
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	for _, cu := range users {
		if cu.Enabled == enabled {
			continue
		}
		cu.Enabled = enabled
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

// DeleteUsersBySubscription removes all of a subscription's core_users (the
// subscription is being deleted) and reconciles the affected nodes. Node IDs
// are collected before deletion, since the rows vanish.
func (u *CoreUsecase) DeleteUsersBySubscription(ctx context.Context, subID uint) error {
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	nodeSet, err := u.nodeSetForUsers(ctx, users)
	if err != nil {
		return err
	}
	for _, cu := range users {
		if err := u.repo.DeleteUser(ctx, cu.ID); err != nil {
			return err
		}
	}
	return u.reconcileNodeSet(ctx, nodeSet)
}

// GenerateSubscriptionArtifacts renders the client artifact for every core_user
// of a subscription, resolving each user's node host via the wired resolver.
// Pure render — no provisioning, no enable/disable.
func (u *CoreUsecase) GenerateSubscriptionArtifacts(ctx context.Context, subID uint) ([]*ClientArtifact, error) {
	users, err := u.repo.FindUsersBySubscription(ctx, subID)
	if err != nil {
		return nil, err
	}
	var arts []*ClientArtifact
	disabledByNode := map[uint]map[domain.CoreProtocol]bool{}
	serving := map[uint]bool{}
	inboundCache := map[uint]*domain.CoreInbound{}
	for _, cu := range users {
		in, ok := inboundCache[cu.CoreInboundID]
		if !ok {
			in, err = u.repo.FindInboundByID(ctx, cu.CoreInboundID)
			if err != nil {
				// Stale user (inbound deleted): skip, don't sink the whole list.
				continue
			}
			inboundCache[cu.CoreInboundID] = in
		}
		// Hide tunnels whose protocol is disabled on their node — the listener is
		// down, so the config would be dead.
		dis, ok := disabledByNode[in.NodeID]
		if !ok {
			if dis, err = u.repo.DisabledProtocolsForNode(ctx, in.NodeID); err != nil {
				return nil, err
			}
			disabledByNode[in.NodeID] = dis
		}
		if dis[in.Protocol] {
			continue
		}
		on, ok := serving[in.NodeID]
		if !ok {
			on = u.servesUsers(ctx, in.NodeID)
			serving[in.NodeID] = on
		}
		if !on {
			continue // node out of service: its links would be dead
		}
		art, err := u.GenerateUserConfigAuto(ctx, cu.ID)
		if err != nil {
			// Partial-failure tolerant: one unreachable node/inbound must not
			// sink the whole subscription's config list.
			continue
		}
		art.DataUsed = cu.DataUsed // per-tunnel consumed bytes for the sub panel
		arts = append(arts, art)
	}
	return arts, nil
}

// GenerateUserConfigAuto renders a core_user's client artifact, resolving the
// node host internally (the plain GenerateUserConfig takes an explicit host).
func (u *CoreUsecase) GenerateUserConfigAuto(ctx context.Context, userID uint) (*ClientArtifact, error) {
	cu, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	in, err := u.repo.FindInboundByID(ctx, cu.CoreInboundID)
	if err != nil {
		return nil, err
	}
	host, err := u.ResolveNodeHost(ctx, in.NodeID)
	if err != nil {
		return nil, err
	}
	if host == "" {
		return nil, ErrNoPublicAddress
	}
	art, err := u.GenerateUserConfig(ctx, userID, host)
	if err != nil {
		return nil, err
	}
	art.NodeName = u.resolveNodeLabel(ctx, in.NodeID, host)
	return art, nil
}

// nodeSetForUsers collects the distinct node IDs a user set touches, caching
// inbound→node lookups so each inbound is fetched at most once.
func (u *CoreUsecase) nodeSetForUsers(ctx context.Context, users []*domain.CoreUser) (map[uint]struct{}, error) {
	nodeSet := map[uint]struct{}{}
	inboundNode := map[uint]uint{}
	for _, cu := range users {
		nodeID, ok := inboundNode[cu.CoreInboundID]
		if !ok {
			in, err := u.repo.FindInboundByID(ctx, cu.CoreInboundID)
			if err != nil {
				return nil, err
			}
			nodeID = in.NodeID
			inboundNode[cu.CoreInboundID] = nodeID
		}
		nodeSet[nodeID] = struct{}{}
	}
	return nodeSet, nil
}

// reconcileNodeSet pushes core state to every node in the set, joining any
// per-node reconcile errors (all pushes are attempted regardless of failures).
func (u *CoreUsecase) reconcileNodeSet(ctx context.Context, nodeSet map[uint]struct{}) error {
	var errs []error
	for nodeID := range nodeSet {
		if err := u.ReconcileNodeByID(ctx, nodeID); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrReconcilePending, errors.Join(errs...))
}
