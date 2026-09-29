package server

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/pool"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func (s *Server) FetchPoolSource(ctx context.Context, req *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	return pool.Fetch(ctx, req)
}
func (s *Server) TestPoolCandidate(ctx context.Context, req *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	return pool.Test(ctx, req)
}
