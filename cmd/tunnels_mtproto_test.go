package cmd

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/mtprotonode"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

type localOnly struct {
	nodeUsecase.NodeUsecase
	id uint
}

func (l localOnly) LocalNodeID() uint { return l.id }

type countingNodes struct{ gets int }

func (c *countingNodes) GetNode(_ context.Context, id uint) (*nodeDomain.Node, error) {
	c.gets++
	return &nodeDomain.Node{ID: id, Kind: nodeDomain.NodeKindLite}, nil
}
func (c *countingNodes) ListInboundsByNode(context.Context, uint) ([]*nodeDomain.Inbound, error) {
	return nil, nil
}

type noMTProto struct{}

func (noMTProto) Pusher(uint) *mtprotonode.Pusher                { return nil }
func (noMTProto) ReservedPorts(context.Context, uint) []int      { return nil }
func (noMTProto) CheckCover(context.Context, uint, string) error { return nil }

// A reconcile inside a transaction must reach the built-in node without
// touching SQLite's one connection.
func TestTheBuiltInNodeIsResolvedWithoutADatabaseRead(t *testing.T) {
	nodes := &countingNodes{}
	d := tunnelDeps{MTProto: noMTProto{}, Nodes: nodes, NodeUC: localOnly{id: 1}}
	if d.isLite(context.Background(), 1) || nodes.gets != 0 {
		t.Fatalf("the built-in node cost %d node reads", nodes.gets)
	}
	if !d.isLite(context.Background(), 2) {
		t.Fatal("an MTProto node was not recognised")
	}
}
