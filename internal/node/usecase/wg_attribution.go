package usecase

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
