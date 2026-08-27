package usecase

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// wgRef links a synthetic WG stat email back to its peer + subscription.
type wgRef struct {
	SubID     uint
	PeerID    uint
	InboundID uint
}

// wgEmailKey must match the per-peer email the config builder stamps onto each
// WireGuard peer ("wg:" + inboundTag + ":" + tunnelIP)
func wgEmailKey(inboundTag, ip string) string {
	return "wg:" + inboundTag + ":" + ip
}

// buildWGIndex maps stat email -> wgRef for every active peer, by inbound tag.
func buildWGIndex(peersByInboundTag map[string][]WGRenderPeer) map[string]wgRef {
	idx := make(map[string]wgRef)
	for tag, peers := range peersByInboundTag {
		for _, p := range peers {
			idx[wgEmailKey(tag, p.AllowedIP)] = wgRef{SubID: p.SubscriptionID, PeerID: p.PeerID, InboundID: p.InboundID}
		}
	}
	return idx
}

// persistWGPeerTraffic feeds a peer's cycle traffic into its subscription's
// usage (same quota path as normal users) and the per-device counters.
func (u *nodeUsecase) persistWGPeerTraffic(ctx context.Context, ref wgRef, total, up, down int64, log logrus.FieldLogger) {
	if err := u.subRepo.AddDataUsed(ctx, ref.SubID, total); err != nil {
		log.WithError(err).Warnf("WG: AddDataUsed sub=%d", ref.SubID)
	}
	if err := u.subRepo.AddLifetimeDataUsed(ctx, ref.SubID, total); err != nil {
		log.WithError(err).Warnf("WG: AddLifetimeDataUsed sub=%d", ref.SubID)
	}
	if up > 0 {
		_ = u.subRepo.AddDataUpload(ctx, ref.SubID, up)
		_ = u.subRepo.AddLifetimeDataUpload(ctx, ref.SubID, up)
	}
	if down > 0 {
		_ = u.subRepo.AddDataDownload(ctx, ref.SubID, down)
		_ = u.subRepo.AddLifetimeDataDownload(ctx, ref.SubID, down)
	}
	now := time.Now()
	_ = u.subRepo.UpdateLastActive(ctx, ref.SubID, now)
	if err := u.wgPeerSource.AddPeerUsage(ctx, ref.PeerID, up, down); err != nil {
		log.WithError(err).Warnf("WG: AddPeerUsage peer=%d", ref.PeerID)
	}
	_ = u.wgPeerSource.TouchPeerLastSeen(ctx, ref.PeerID, now)

	// Credit the manual WG account for this inbound so the panel's per-server
	// (SERVERS) view reflects WG usage, like other protocols do.
	if ref.InboundID != 0 {
		if accts, err := u.accountRepo.ListBySubscriptionID(ctx, ref.SubID); err == nil {
			for _, a := range accts {
				if a.InboundID == ref.InboundID {
					_ = u.accountRepo.AddDataUsed(ctx, a.ID, total)
					_ = u.accountRepo.UpdateLastActive(ctx, a.ID, now)
					break
				}
			}
		}
	}
}
