package agent

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// PoolClient is optional so older clients can report an upgrade requirement.
type PoolClient interface {
	FetchPoolSource(context.Context, *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error)
	TestPoolCandidate(context.Context, *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error)
}

func (c *Client) FetchPoolSource(ctx context.Context, req *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	return c.client.FetchPoolSource(ctx, req)
}
func (c *Client) TestPoolCandidate(ctx context.Context, req *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	return c.client.TestPoolCandidate(ctx, req)
}
func (c *ReverseClient) FetchPoolSource(ctx context.Context, req *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	var resp pb.PoolSourceResponse
	err := c.sendUnary(ctx, pb.NodeAgent_FetchPoolSource_FullMethodName, req, &resp)
	return &resp, err
}
func (c *ReverseClient) TestPoolCandidate(ctx context.Context, req *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	var resp pb.PoolCandidateResponse
	err := c.sendUnary(ctx, pb.NodeAgent_TestPoolCandidate_FullMethodName, req, &resp)
	return &resp, err
}
