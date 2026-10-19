package xray

import (
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// TunnelLink builds the link a reverse tunnel's bridge dials: the same link a
// customer on this inbound gets, carrying the tunnel's own id.
func TunnelLink(detail product.InboundDetail, uuid, email, remark string) (string, error) {
	info := BuildInboundInfo(&detail)
	if detail.LinkFormat != "" {
		p := &Provider{linkBuilder: NewLinkBuilder()}
		raw := p.generateLink(detail.LinkFormat, uuid, email, detail.NodeIP, detail.PublicPort, remark)
		return p.enrichLinkWithInboundInfo(raw, info), nil
	}
	return xray.GenerateConfigLink(info, uuid, detail.NodeIP, remark)
}
