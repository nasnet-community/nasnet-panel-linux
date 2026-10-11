package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// one SSH tunnel on node 1 with one user
type oneTunnel struct{ repository.CoreRepository }

var tunnel = &domain.CoreInbound{ID: 4, NodeID: 1, Protocol: domain.CoreProtoSSH, Port: 2222, EgressMode: domain.EgressDirect}

func (oneTunnel) ListInboundsByNode(context.Context, uint) ([]*domain.CoreInbound, error) {
	return []*domain.CoreInbound{tunnel}, nil
}
func (oneTunnel) DisabledProtocolsForNode(context.Context, uint) (map[domain.CoreProtocol]bool, error) {
	return nil, nil
}
func (oneTunnel) ListUsersByInbound(context.Context, uint) ([]*domain.CoreUser, error) {
	return []*domain.CoreUser{{ID: 9, CoreInboundID: 4, Username: "u", Secret: "s", Enabled: true}}, nil
}
func (oneTunnel) FindUsersBySubscription(context.Context, uint) ([]*domain.CoreUser, error) {
	return []*domain.CoreUser{{ID: 9, CoreInboundID: 4, Username: "u", Secret: "s", Enabled: true}}, nil
}
func (oneTunnel) FindUserByID(context.Context, uint) (*domain.CoreUser, error) {
	return &domain.CoreUser{ID: 9, CoreInboundID: 4, Username: "u", Secret: "s", Enabled: true}, nil
}
func (oneTunnel) FindInboundByID(context.Context, uint) (*domain.CoreInbound, error) {
	return tunnel, nil
}

type recordingPusher struct{ got []*pb.CoreStatePayload }

func (p *recordingPusher) ApplyCoreState(_ context.Context, payload *pb.CoreStatePayload) error {
	p.got = append(p.got, payload)
	return nil
}
func (p *recordingPusher) GetCoreStatus(context.Context, string) (*pb.CoreStatusResponse, error) {
	return &pb.CoreStatusResponse{}, nil
}

func inService(on bool) Option {
	return WithNodeInService(func(context.Context, uint) bool { return on })
}

func TestTunnelUsersFollowTheNodeService(t *testing.T) {
	for _, on := range []bool{true, false} {
		p := &recordingPusher{}
		uc := NewCoreUsecase(oneTunnel{}, inService(on))
		if err := uc.ReconcileNode(context.Background(), p, 1); err != nil {
			t.Fatal(err)
		}
		if len(p.got) != 1 || !p.got[0].Enabled || len(p.got[0].Users) != 1 {
			t.Fatalf("in service %v: the tunnel itself must keep running, got %+v", on, p.got)
		}
		if p.got[0].Users[0].Enabled != on {
			t.Fatalf("in service %v: user enabled = %v", on, p.got[0].Users[0].Enabled)
		}
	}
}

func TestTunnelLinksHiddenWhileOutOfService(t *testing.T) {
	host := WithNodeHostResolver(func(context.Context, uint) (string, error) { return "198.51.100.4", nil })
	for _, on := range []bool{true, false} {
		arts, err := NewCoreUsecase(oneTunnel{}, host, inService(on)).GenerateSubscriptionArtifacts(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 0}[on]; len(arts) != want {
			t.Fatalf("in service %v: %d links, want %d", on, len(arts), want)
		}
	}
}
