// Package nodebridge adapts the wireguard repository to the node usecase's
// WGPeerSource, kept separate to avoid an import cycle.
package nodebridge

import (
	"context"
	"time"

	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wgRepo "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// AccessResolver reports which of the given subscriptions may currently carry
// traffic. A subscription left out of the returned map is treated as "no
// opinion" and its peers are rendered as-is, so a transient database error
// cannot cut every peer off at once. Declared here rather than imported so the
// wireguard subsystem does not depend on the subscription one.
type AccessResolver func(ctx context.Context, subIDs []uint) (map[uint]bool, error)

type PeerSource struct {
	peers  wgRepo.WGPeerRepository
	access AccessResolver
}

func New(peers wgRepo.WGPeerRepository) *PeerSource { return &PeerSource{peers: peers} }

// SetAccessResolver wires the subscription-status gate. Without it a peer row
// marked active is rendered onto the node even if the subscription behind it is
// paused, cancelled, expired or out of quota — the peer's own status column is
// only as correct as the last lifecycle hook that remembered to write it.
func (s *PeerSource) SetAccessResolver(r AccessResolver) { s.access = r }

func (s *PeerSource) ActivePeersByInbound(ctx context.Context, inboundID uint) ([]nodeUC.WGRenderPeer, error) {
	rows, err := s.peers.ListActiveByInbound(ctx, inboundID)
	if err != nil {
		return nil, err
	}
	rows = s.filterRevoked(ctx, rows)
	out := make([]nodeUC.WGRenderPeer, 0, len(rows))
	for _, p := range rows {
		out = append(out, nodeUC.WGRenderPeer{
			PublicKey:      p.PublicKey,
			PresharedKey:   p.PresharedKey,
			AllowedIP:      p.AssignedIP,
			SubscriptionID: p.SubscriptionID,
			PeerID:         p.ID,
			InboundID:      p.InboundID,
		})
	}
	return out, nil
}

// filterRevoked drops peers whose backing subscription no longer grants access.
// Peers with no subscription (manually added, not sold) are always kept.
func (s *PeerSource) filterRevoked(ctx context.Context, rows []*wgDomain.WGPeer) []*wgDomain.WGPeer {
	if s.access == nil || len(rows) == 0 {
		return rows
	}
	seen := map[uint]struct{}{}
	ids := make([]uint, 0, len(rows))
	for _, p := range rows {
		if p.SubscriptionID == 0 {
			continue
		}
		if _, ok := seen[p.SubscriptionID]; ok {
			continue
		}
		seen[p.SubscriptionID] = struct{}{}
		ids = append(ids, p.SubscriptionID)
	}
	if len(ids) == 0 {
		return rows
	}
	allowed, err := s.access(ctx, ids)
	if err != nil {
		// Render what we have rather than blanking the inbound on a lookup error.
		logger.GetLogger().WithError(err).Warn("[wireguard] access resolve failed; rendering peers unfiltered")
		return rows
	}
	kept := rows[:0:0]
	for _, p := range rows {
		if ok, known := allowed[p.SubscriptionID]; known && !ok {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func (s *PeerSource) AddPeerUsage(ctx context.Context, peerID uint, up, down int64) error {
	return s.peers.AddUsage(ctx, peerID, up, down)
}

func (s *PeerSource) TouchPeerLastSeen(ctx context.Context, peerID uint, t time.Time) error {
	return s.peers.TouchLastSeen(ctx, peerID, t)
}
