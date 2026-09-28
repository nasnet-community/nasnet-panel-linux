package agent

import (
	"context"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// StarlinkPassthroughClient forwards raw dish requests through the agent.
type StarlinkPassthroughClient interface {
	StarlinkHandle(context.Context, *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error)
}

func (c *Client) StarlinkHandle(ctx context.Context, req *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	return c.client.StarlinkHandle(ctx, req)
}

func (c *ReverseClient) StarlinkHandle(ctx context.Context, req *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	var resp pb.StarlinkHandleResponse
	if err := c.sendUnary(ctx, pb.NodeAgent_StarlinkHandle_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
