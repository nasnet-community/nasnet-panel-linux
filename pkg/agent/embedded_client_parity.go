package agent

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func (c *EmbeddedClient) ApplyCoreState(ctx context.Context, p *pb.CoreStatePayload) error {
	resp, err := c.srv.ApplyCoreState(ctx, p)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("apply core state", resp)
	}
	return nil
}
func (c *EmbeddedClient) GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error) {
	return c.srv.GetCoreStatus(ctx, &pb.CoreStatusRequest{CoreType: coreType})
}
func (c *EmbeddedClient) ProbeDependencies(ctx context.Context, coreTypes []string) (*pb.ProbeDependenciesResponse, error) {
	return c.srv.ProbeDependencies(ctx, &pb.ProbeDependenciesRequest{CoreTypes: coreTypes})
}
func (c *EmbeddedClient) StartDependencyInstall(ctx context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error) {
	return c.srv.StartDependencyInstall(ctx, req)
}
func (c *EmbeddedClient) GetInstallProgress(ctx context.Context, jobID string, since int32) (*pb.InstallProgressResponse, error) {
	return c.srv.GetInstallProgress(ctx, &pb.InstallProgressRequest{JobId: jobID, SinceCursor: since})
}
func (c *EmbeddedClient) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*StatsBundle, error) {
	resp, err := c.srv.GetStatsBundle(ctx, &pb.StatsBundleRequest{IncludeOnlineIps: includeOnlineIPs})
	if err != nil {
		return nil, err
	}
	return &StatsBundle{
		System:                     systemStatsFromPB(resp.System),
		Status:                     resp.Status,
		Buffered:                   bufferedTrafficFromPB(resp.BufferedTraffic),
		OnlineIPs:                  onlineIPsFromPB(resp.OnlineIps),
		OnlineIPsCollectedAtUnixMs: resp.OnlineIps.GetCollectedAtUnixMs(),
	}, nil
}
func (c *EmbeddedClient) FetchPoolSource(ctx context.Context, r *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	return c.srv.FetchPoolSource(ctx, r)
}
func (c *EmbeddedClient) TestPoolCandidate(ctx context.Context, r *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	return c.srv.TestPoolCandidate(ctx, r)
}
func (c *EmbeddedClient) ProbeRealityDest(ctx context.Context, r *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	return c.srv.ProbeRealityDest(ctx, r)
}
func (c *EmbeddedClient) StarlinkHandle(ctx context.Context, r *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	return c.srv.StarlinkHandle(ctx, r)
}

var (
	_ PoolClient                = (*EmbeddedClient)(nil)
	_ RealityDestClient         = (*EmbeddedClient)(nil)
	_ StarlinkPassthroughClient = (*EmbeddedClient)(nil)
)

// InstallCertificate is refused: the built-in node shares the panel's
// process and has no agent TLS certificate of its own to rotate.
func (c *EmbeddedClient) InstallCertificate(context.Context, *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	return nil, status.Error(codes.FailedPrecondition, "the built-in node has no agent certificate")
}

func (c *EmbeddedClient) InspectCertFile(ctx context.Context, r *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	return c.srv.InspectCertFile(ctx, r)
}

var _ CertInstallClient = (*EmbeddedClient)(nil)
