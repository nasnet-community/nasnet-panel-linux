// Package nodebridge translates hub-side core_inbounds / core_users into the
// gRPC CoreStatePayload the agent reconciles. It is the core-subsystem analogue
// of internal/wireguard/nodebridge.
package nodebridge

import (
	"context"
	"strconv"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// UserPolicy is the subscription-derived enforcement pushed alongside a
// core_user, for cores that can hold it natively (mtproto/telemt).
type UserPolicy struct {
	// Active is whether the backing subscription should be carrying traffic at
	// all (not paused / cancelled / expired / out of quota). A core_user whose
	// subscription resolves to Active=false is pushed disabled no matter what
	// its own enabled column says — the column is bookkeeping that a missed
	// lifecycle hook can leave stale, the subscription's status is the truth.
	Active           bool
	ExpiresAt        time.Time // zero => no expiry
	RateLimitUpBps   int64     // 0 => unlimited
	RateLimitDownBps int64
}

// UserPolicyResolver maps subscription IDs to their enforcement policy. It is
// optional: without one, core_users are pushed with no expiry or rate cap (the
// hub's own enable/disable remains the primary control). Declared here rather
// than imported so the core subsystem does not depend on the subscription one.
//
// A subscription the resolver cannot read (a transient DB error) must simply be
// left out of the returned map rather than reported inactive, so a blip does not
// cut every user off; one it reads and finds gone or dead belongs in the map
// with Active=false.
type UserPolicyResolver func(ctx context.Context, subscriptionIDs []uint) (map[uint]UserPolicy, error)

// BuildCoreStatePayloads returns one CoreStatePayload per core_inbound on the
// node. Disabled inbounds are still emitted with enabled=false so the agent
// tears them down; disabled users are still emitted with enabled=false so the
// agent denies new dials and the sweeper kills their live sessions.
func BuildCoreStatePayloads(ctx context.Context, repo repository.CoreRepository, nodeID uint, policies UserPolicyResolver) ([]*pb.CoreStatePayload, error) {
	inbounds, err := repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	// Protocols disabled on this node have their listeners torn down: the payload
	// is still emitted (so the agent reconciles it away) but with enabled=false.
	// The core_inbound row is left untouched, so re-enabling is lossless.
	disabled, err := repo.DisabledProtocolsForNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	// Resolve every subscription referenced on this node in one pass, so the
	// per-user loop below does not fan out into N queries.
	policyBySub, err := resolvePolicies(ctx, repo, inbounds, policies)
	if err != nil {
		return nil, err
	}
	payloads := make([]*pb.CoreStatePayload, 0, len(inbounds))
	for _, in := range inbounds {
		users, err := repo.ListUsersByInbound(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		pbUsers := make([]*pb.CoreUser, 0, len(users))
		for _, u := range users {
			pu := &pb.CoreUser{
				Username: u.Username,
				Secret:   u.Secret,
				Email:    u.Email,
				KLimit:   int32(u.KLimit),
				Enabled:  u.Enabled,
			}
			if u.SubscriptionID != nil {
				if pol, ok := policyBySub[*u.SubscriptionID]; ok {
					// Subscription state can only ever take access away, never
					// grant it: an admin-disabled tunnel stays disabled.
					if !pol.Active {
						pu.Enabled = false
					}
					if !pol.ExpiresAt.IsZero() {
						pu.ExpiresAtUnix = pol.ExpiresAt.Unix()
					}
					pu.RateLimitUpBps = pol.RateLimitUpBps
					pu.RateLimitDownBps = pol.RateLimitDownBps
				}
			}
			pbUsers = append(pbUsers, pu)
		}
		settings, err := withSocksAddr(in)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, &pb.CoreStatePayload{
			CoreType:   string(in.Protocol),
			InboundId:  strconv.FormatUint(uint64(in.ID), 10),
			Enabled:    !in.IsDisabled && !disabled[in.Protocol],
			Listen:     in.Listen,
			Port:       int32(in.Port),
			EgressMode: string(in.EgressMode),
			Settings:   settings,
			Users:      pbUsers,
		})
	}
	return payloads, nil
}

// resolvePolicies collects the distinct subscription IDs backing this node's
// core_users and resolves each one's enforcement policy. A resolver failure is
// non-fatal: the push proceeds without policy rather than leaving the node
// unreconciled (the hub's enable/disable still governs access).
func resolvePolicies(
	ctx context.Context,
	repo repository.CoreRepository,
	inbounds []*domain.CoreInbound,
	policies UserPolicyResolver,
) (map[uint]UserPolicy, error) {
	if policies == nil {
		return nil, nil
	}
	seen := map[uint]struct{}{}
	var ids []uint
	for _, in := range inbounds {
		users, err := repo.ListUsersByInbound(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.SubscriptionID == nil {
				continue
			}
			if _, ok := seen[*u.SubscriptionID]; ok {
				continue
			}
			seen[*u.SubscriptionID] = struct{}{}
			ids = append(ids, *u.SubscriptionID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := policies(ctx, ids)
	if err != nil {
		return nil, nil
	}
	return out, nil
}
