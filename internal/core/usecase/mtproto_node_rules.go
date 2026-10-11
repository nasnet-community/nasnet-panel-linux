package usecase

import (
	"context"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	mtprotoproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/mtproto"
)

// kindLite is node.Kind for an MTProto node (kept as a string so this package does
// not import the node domain).
const kindLite = "lite"

// validateForNode applies the rules that depend on where the tunnel lives. A
// MTProto node runs one MTProto tunnel whose Telegram connections are carried
// back through the panel; a full node never uses hub egress.
func (u *CoreUsecase) validateForNode(ctx context.Context, in *domain.CoreInbound) error {
	kind := ""
	if u.nodeKind != nil {
		k, err := u.nodeKind(ctx, in.NodeID)
		if err != nil {
			return err
		}
		kind = k
	}
	if kind != kindLite {
		if in.EgressMode == domain.EgressHub {
			return ErrHubEgressNotHere
		}
		return nil
	}
	if in.EgressMode != domain.EgressHub {
		return ErrMTProtoNodeEgress
	}
	if in.Protocol != domain.CoreProtoMTProto {
		return ErrMTProtoNodeProtocol
	}
	existing, err := u.repo.ListInboundsByNode(ctx, in.NodeID)
	if err != nil {
		return err
	}
	for _, other := range existing {
		if other.ID != in.ID {
			return ErrMTProtoNodeSingle
		}
	}
	s, err := mtprotoproto.ParseSettings(in.Settings)
	if err != nil {
		return err
	}
	// the agent runs telemt and its REALITY on tls_domain: extras alone won't do
	primary := strings.ToLower(strings.TrimSpace(s.TLSDomain))
	if primary == "" {
		return ErrMTProtoNodeDomain
	}
	if s.MiddleProxy || s.AdtagDefault != "" {
		return ErrMTProtoNodeNoMiddle
	}
	if u.coverCheck != nil {
		return u.coverCheck(ctx, in.NodeID, primary)
	}
	return nil
}
