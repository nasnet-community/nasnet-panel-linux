package cmd

import (
	"context"
	"fmt"

	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	networkUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// inboundLister is the slice of the node repository the firewall needs.
type inboundLister interface {
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*nodeDomain.Inbound, error)
}

// tunnelLister is the slice of the tunnel repository the firewall needs.
type tunnelLister interface {
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*coreDomain.CoreInbound, error)
	DisabledProtocolsForNode(ctx context.Context, nodeID uint) (map[coreDomain.CoreProtocol]bool, error)
}

// inboundSource feeds the firewall the same rows that generate the xray config
// and the tunnel state, so an inbound cannot exist without its accept.
type inboundSource struct {
	repo    inboundLister
	tunnels tunnelLister
	// localNodeID is read on every call: the built-in node is resolved after
	// router mode starts. Router tables are pinned to node 1, so 1 stands in
	// until then.
	localNodeID func() uint
}

func (s inboundSource) EnabledInbounds(ctx context.Context) ([]networkUsecase.InboundSpec, error) {
	nodeID := uint(1)
	if s.localNodeID != nil {
		if id := s.localNodeID(); id != 0 {
			nodeID = id
		}
	}
	rows, err := s.repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]networkUsecase.InboundSpec, 0, len(rows))
	for _, ib := range rows {
		out = append(out, networkUsecase.InboundSpecsFor(
			ib.Tag, ib.Protocol, ib.Port, ib.PortRange, !ib.IsDisabled)...)
	}
	if s.tunnels == nil {
		return out, nil
	}
	tunnels, err := s.tunnels.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	disabled, err := s.tunnels.DisabledProtocolsForNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	for _, t := range tunnels {
		tag := fmt.Sprintf("core-%s-%d", t.Protocol, t.ID)
		out = append(out, networkUsecase.InboundSpecsFor(
			tag, "tcp", t.Port, "", !t.IsDisabled && !disabled[t.Protocol])...)
	}
	return out, nil
}
