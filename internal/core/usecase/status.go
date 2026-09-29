package usecase

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// ErrStatusUnavailable is returned when no status resolver is wired (e.g. the
// node client factory isn't configured).
var ErrStatusUnavailable = errors.New("core: status resolver not configured")

// CoreStatusReader is the narrow slice of the agent client that status queries
// need. *agent.Client and *agent.ReverseClient both satisfy it.
type CoreStatusReader interface {
	GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error)
}

// NodeStatusResolver returns a status reader for a node's agent.
type NodeStatusResolver func(ctx context.Context, nodeID uint) (CoreStatusReader, error)

// WithNodeStatusResolver wires the agent-client lookup used to query live core
// status from a node's agent.
func WithNodeStatusResolver(fn NodeStatusResolver) Option {
	return func(u *CoreUsecase) { u.statusResolve = fn }
}

// CoreStatus is the hub-facing runtime status of one protocol core on a node.
// Status is reported per protocol (the agent runs one core per protocol type),
// so all inbounds of the same protocol on a node share these numbers.
type CoreStatus struct {
	Running         bool   `json:"running"`
	Version         string `json:"version"`
	LiveSessions    int    `json:"live_sessions"`
	DistinctDevices int    `json:"distinct_devices"`
	Detail          string `json:"detail"`
}

// GetInboundStatus resolves the node's agent and reports the runtime status of
// the inbound's protocol core.
func (u *CoreUsecase) GetInboundStatus(ctx context.Context, inboundID uint) (*CoreStatus, error) {
	in, err := u.repo.FindInboundByID(ctx, inboundID)
	if err != nil {
		return nil, err
	}
	if u.statusResolve == nil {
		return nil, ErrStatusUnavailable
	}
	reader, err := u.statusResolve(ctx, in.NodeID)
	if err != nil {
		return nil, fmt.Errorf("resolve node %d client: %w", in.NodeID, err)
	}
	resp, err := reader.GetCoreStatus(ctx, string(in.Protocol))
	if err != nil {
		return nil, err
	}
	return &CoreStatus{
		Running:         resp.GetRunning(),
		Version:         resp.GetVersion(),
		LiveSessions:    int(resp.GetLiveSessions()),
		DistinctDevices: int(resp.GetDistinctDevices()),
		Detail:          resp.GetDetail(),
	}, nil
}
