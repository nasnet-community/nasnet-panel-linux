package embedded

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func (c *Client) ApplyCoreState(ctx context.Context, p *pb.CoreStatePayload) error {
	resp, err := c.srv.ApplyCoreState(ctx, p)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("apply core state", resp)
	}
	return nil
}
func (c *Client) GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error) {
	return c.srv.GetCoreStatus(ctx, &pb.CoreStatusRequest{CoreType: coreType})
}
func (c *Client) ProbeDependencies(ctx context.Context, coreTypes []string) (*pb.ProbeDependenciesResponse, error) {
	return c.srv.ProbeDependencies(ctx, &pb.ProbeDependenciesRequest{CoreTypes: coreTypes})
}
func (c *Client) StartDependencyInstall(ctx context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error) {
	return c.srv.StartDependencyInstall(ctx, req)
}
func (c *Client) GetInstallProgress(ctx context.Context, jobID string, since int32) (*pb.InstallProgressResponse, error) {
	return c.srv.GetInstallProgress(ctx, &pb.InstallProgressRequest{JobId: jobID, SinceCursor: since})
}
func (c *Client) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*agent.StatsBundle, error) {
	resp, err := c.srv.GetStatsBundle(ctx, &pb.StatsBundleRequest{IncludeOnlineIps: includeOnlineIPs})
	if err != nil {
		return nil, err
	}
	return &agent.StatsBundle{
		System:                     agent.SystemStatsFromPB(resp.System),
		Status:                     resp.Status,
		Buffered:                   agent.BufferedTrafficFromPB(resp.BufferedTraffic),
		OnlineIPs:                  agent.OnlineIPsFromPB(resp.OnlineIps),
		OnlineIPsCollectedAtUnixMs: resp.OnlineIps.GetCollectedAtUnixMs(),
	}, nil
}
func (c *Client) FetchPoolSource(ctx context.Context, r *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	return c.srv.FetchPoolSource(ctx, r)
}
func (c *Client) TestPoolCandidate(ctx context.Context, r *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	return c.srv.TestPoolCandidate(ctx, r)
}
func (c *Client) ProbeRealityDest(ctx context.Context, r *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	return c.srv.ProbeRealityDest(ctx, r)
}
func (c *Client) StarlinkHandle(ctx context.Context, r *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	return c.srv.StarlinkHandle(ctx, r)
}

var (
	_ agent.PoolClient                = (*Client)(nil)
	_ agent.RealityDestClient         = (*Client)(nil)
	_ agent.StarlinkPassthroughClient = (*Client)(nil)
)

// InstallCertificate is refused: the built-in node shares the panel's
// process and has no agent TLS certificate of its own to rotate.
func (c *Client) InstallCertificate(context.Context, *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	return nil, status.Error(codes.FailedPrecondition, "the built-in node has no agent certificate")
}

func (c *Client) InspectCertFile(ctx context.Context, r *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	return c.srv.InspectCertFile(ctx, r)
}

var _ agent.CertInstallClient = (*Client)(nil)
