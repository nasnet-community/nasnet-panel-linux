// Package nodebridge adapts the wireguard repository to the node usecase's
// WGPeerSource, kept separate to avoid an import cycle.
package nodebridge

import (
	"context"
	"time"

	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	wgRepo "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/repository"
)

type PeerSource struct{ peers wgRepo.WGPeerRepository }

func New(peers wgRepo.WGPeerRepository) *PeerSource { return &PeerSource{peers: peers} }

func (s *PeerSource) ActivePeersByInbound(ctx context.Context, inboundID uint) ([]nodeUC.WGRenderPeer, error) {
	rows, err := s.peers.ListActiveByInbound(ctx, inboundID)
	if err != nil {
		return nil, err
	}
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

func (s *PeerSource) AddPeerUsage(ctx context.Context, peerID uint, up, down int64) error {
	return s.peers.AddUsage(ctx, peerID, up, down)
}

func (s *PeerSource) TouchPeerLastSeen(ctx context.Context, peerID uint, t time.Time) error {
	return s.peers.TouchLastSeen(ctx, peerID, t)
}
