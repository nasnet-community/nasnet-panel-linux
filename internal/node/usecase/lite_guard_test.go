package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// An MTProto node has no gRPC agent, so every path that would dial one must fail
// closed (or skip it) instead of recording a false outage.

func TestGetAgentClientFailsClosedForMTProtoNodes(t *testing.T) {
	u := &nodeUsecase{}
	client, err := u.getAgentClient(&domain.Node{ID: 3, Kind: domain.NodeKindLite, ConnectMode: domain.ConnectModeLink})
	if client != nil || !errors.Is(err, ErrLiteNode) {
		t.Fatalf("client=%v err=%v, want ErrLiteNode", client, err)
	}
}

func TestRemoteNodesDropsTheBuiltInNodeAndMTProtoNodes(t *testing.T) {
	remote := &domain.Node{ID: 1, ConnectMode: domain.ConnectModeDirect}
	local := &domain.Node{ID: 2, ConnectMode: domain.ConnectModeLocal}
	lite := &domain.Node{ID: 3, Kind: domain.NodeKindLite, ConnectMode: domain.ConnectModeLink}
	got := remoteNodes([]*domain.Node{remote, local, lite})
	if len(got) != 1 || got[0] != remote {
		t.Fatalf("got %+v, want only the remote node", got)
	}
}

// The repo fake panics on any call it was not given, so reaching ObserveNodeHealth
// (which writes through the repo) would fail this test.
func TestCheckAgentHealthDoesNotProbeAnMTProtoNode(t *testing.T) {
	node := &domain.Node{ID: 3, Name: "vps", Kind: domain.NodeKindLite, ConnectMode: domain.ConnectModeLink, IsOnline: true}
	u := newSyncSingleNodeTestUsecase(newFakeStatsNodeRepo(node), nil)
	h, err := u.CheckAgentHealth(context.Background(), node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Healthy || !strings.Contains(h.Message, "MTProto node") {
		t.Fatalf("health = %+v", h)
	}
}

// deleteRepo answers what a forced delete touches and remembers the delete.
type deleteRepo struct {
	*fakeStatsNodeRepo
	deleted bool
	updated *domain.Node
}

func (r *deleteRepo) ListInboundsByNode(context.Context, uint) ([]*domain.Inbound, error) {
	return nil, nil
}
func (r *deleteRepo) DeleteReverseProxiesByNode(context.Context, uint) error { return nil }
func (r *deleteRepo) DeleteOutboundsByNode(context.Context, uint) error      { return nil }
func (r *deleteRepo) DeleteRoutingRulesByNode(context.Context, uint) error   { return nil }
func (r *deleteRepo) DeleteBalancingRulesByNode(context.Context, uint) error { return nil }
func (r *deleteRepo) DeleteNodeStatsByNode(context.Context, uint) error      { return nil }
func (r *deleteRepo) DeleteNode(context.Context, uint) error                 { r.deleted = true; return nil }
func (r *deleteRepo) UpdateNode(_ context.Context, n *domain.Node) error     { r.updated = n; return nil }

func mtprotoNodeUsecase() (*nodeUsecase, *deleteRepo) {
	node := &domain.Node{ID: 3, Name: "vps", IP: "203.0.113.9", Kind: domain.NodeKindLite, ConnectMode: domain.ConnectModeLink, IsActive: true}
	repo := &deleteRepo{fakeStatsNodeRepo: newFakeStatsNodeRepo(node)}
	u := newSyncSingleNodeTestUsecase(repo.fakeStatsNodeRepo, nil)
	u.nodeRepo = repo
	return u, repo
}

// Deleting an MTProto node anywhere but its own page would leave its agent relaying
// with nothing metering or revoking it.
func TestDeleteNodeRefusesAnMTProtoNodeOutsideTheMTProtoService(t *testing.T) {
	for _, force := range []bool{false, true} {
		u, repo := mtprotoNodeUsecase()
		if err := u.DeleteNode(context.Background(), 3, force); !errors.Is(err, ErrMTProtoNode) {
			t.Fatalf("force=%v: err = %v, want ErrMTProtoNode", force, err)
		}
		if repo.deleted {
			t.Fatalf("force=%v: the row was deleted anyway", force)
		}
	}
	u, repo := mtprotoNodeUsecase()
	if err := u.DeleteNode(FromMTProtoService(context.Background()), 3, true); err != nil {
		t.Fatal(err)
	}
	if !repo.deleted {
		t.Fatal("the MTProto node service could not delete its own node")
	}
}

// The agent is installed at one address; only disabling and renaming go through
// the generic edit.
func TestUpdateNodeRefusesToMoveAnMTProtoNode(t *testing.T) {
	u, repo := mtprotoNodeUsecase()
	moved := *repo.nodes[3]
	moved.IP = "198.51.100.1"
	if err := u.UpdateNode(context.Background(), &moved); !errors.Is(err, ErrMTProtoNodeAddress) {
		t.Fatalf("err = %v, want ErrMTProtoNodeAddress", err)
	}
	if repo.updated != nil {
		t.Fatal("the new address was saved")
	}
	full := *repo.nodes[3]
	full.Kind = domain.NodeKindFull
	if err := u.UpdateNode(context.Background(), &full); !errors.Is(err, ErrMTProtoNodeAddress) {
		t.Fatalf("turning an MTProto node into a full one: err = %v", err)
	}
	renamed := *repo.nodes[3]
	renamed.Name = "vps-2"
	if err := u.UpdateNode(context.Background(), &renamed); err != nil {
		t.Fatal(err)
	}
	if repo.updated == nil || repo.updated.Name != "vps-2" {
		t.Fatal("a rename was refused")
	}
}
