package usecase

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Optional RPCs must survive the shared connection wrapper.
func (p *pooledAgentClient) FetchPoolSource(ctx context.Context, req *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	client, ok := p.NodeClient.(agent.PoolClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "pool sources require an updated agent")
	}
	return client.FetchPoolSource(ctx, req)
}
func (p *pooledAgentClient) TestPoolCandidate(ctx context.Context, req *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	client, ok := p.NodeClient.(agent.PoolClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "pool scans require an updated agent")
	}
	return client.TestPoolCandidate(ctx, req)
}

func (p *pooledAgentClient) ProbeRealityDest(ctx context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	client, ok := p.NodeClient.(agent.RealityDestClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "REALITY destination checks require an updated agent")
	}
	return client.ProbeRealityDest(ctx, req)
}

var _ agent.PoolClient = (*pooledAgentClient)(nil)
var _ agent.RealityDestClient = (*pooledAgentClient)(nil)

func (p *pooledAgentClient) StarlinkHandle(ctx context.Context, req *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	client, ok := p.NodeClient.(agent.StarlinkPassthroughClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "dish passthrough requires an updated agent")
	}
	return client.StarlinkHandle(ctx, req)
}

var _ agent.StarlinkPassthroughClient = (*pooledAgentClient)(nil)

func (p *pooledAgentClient) InstallCertificate(ctx context.Context, req *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	client, ok := p.NodeClient.(agent.CertInstallClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "certificate rotation requires an updated agent")
	}
	return client.InstallCertificate(ctx, req)
}

func (p *pooledAgentClient) InspectCertFile(ctx context.Context, req *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	client, ok := p.NodeClient.(agent.CertInstallClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "reading certificate files requires an updated agent")
	}
	return client.InspectCertFile(ctx, req)
}

var _ agent.CertInstallClient = (*pooledAgentClient)(nil)

// GetOnlineIPSnapshot keeps the collection time the wrapper would hide;
// ReverseClient has no snapshot, so it falls back to the plain map.
func (p *pooledAgentClient) GetOnlineIPSnapshot(ctx context.Context) (*agent.OnlineIPSnapshot, error) {
	return getOnlineIPSnapshot(ctx, p.NodeClient)
}
