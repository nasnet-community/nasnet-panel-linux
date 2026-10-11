package usecase

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// withoutLite drops MTProto nodes before a walk that makes gRPC agent calls.
func withoutLite(nodes []*domain.Node) []*domain.Node {
	out := make([]*domain.Node, 0, len(nodes))
	for _, n := range nodes {
		if !n.IsLite() {
			out = append(out, n)
		}
	}
	return out
}

type mtprotoServiceKey struct{}

// FromMTProtoService marks the one caller allowed to delete an MTProto node.
func FromMTProtoService(ctx context.Context) context.Context {
	return context.WithValue(ctx, mtprotoServiceKey{}, true)
}

func fromMTProtoService(ctx context.Context) bool {
	v, _ := ctx.Value(mtprotoServiceKey{}).(bool)
	return v
}
