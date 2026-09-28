package agent

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// RealityDestClient is optional so callers can fall back when an older agent
// does not know the RPC.
type RealityDestClient interface {
	ProbeRealityDest(context.Context, *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error)
}

func (c *Client) ProbeRealityDest(ctx context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	return c.client.ProbeRealityDest(ctx, req)
}

func (c *ReverseClient) ProbeRealityDest(ctx context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	var resp pb.RealityDestProbeResponse
	err := c.sendUnary(ctx, pb.NodeAgent_ProbeRealityDest_FullMethodName, req, &resp)
	return &resp, err
}
