package usecase

import (
	"context"
	"fmt"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// DepAgent is the slice of a node's agent client the installer drives. The
// install itself runs on the node; the built-in node reaches the same code in
// process.
type DepAgent interface {
	ProbeDependencies(ctx context.Context, coreTypes []string) (*pb.ProbeDependenciesResponse, error)
	StartDependencyInstall(ctx context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error)
	GetInstallProgress(ctx context.Context, jobID string, sinceCursor int32) (*pb.InstallProgressResponse, error)
}

// DepAgentResolver returns the agent for a node.
type DepAgentResolver func(ctx context.Context, nodeID uint) (DepAgent, error)

// InstallUsecase probes and installs tunnel binaries on nodes. Jobs live on
// the node; the hub only remembers which node a job id belongs to.
type InstallUsecase struct {
	resolve DepAgentResolver

	mu   sync.Mutex
	jobs map[string]uint // job id → node id
}

// NewInstallUsecase builds the installer over resolve.
func NewInstallUsecase(resolve DepAgentResolver) *InstallUsecase {
	return &InstallUsecase{resolve: resolve, jobs: make(map[string]uint)}
}

// Probe reports, per requested core (or all), which binaries the node has.
func (u *InstallUsecase) Probe(ctx context.Context, nodeID uint, cores []string) ([]*pb.CoreDependencyReport, error) {
	agent, err := u.resolve(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	resp, err := agent.ProbeDependencies(ctx, cores)
	if err != nil {
		return nil, err
	}
	return resp.GetReports(), nil
}

// StartInstall launches an install (or removal) job on the node.
func (u *InstallUsecase) StartInstall(ctx context.Context, nodeID uint, cores []string, uninstall bool) (string, error) {
	if len(cores) == 0 {
		return "", fmt.Errorf("no cores specified")
	}
	for _, c := range cores {
		if !coredeps.Protocol(c).Valid() {
			return "", fmt.Errorf("unknown core protocol %q", c)
		}
	}
	agent, err := u.resolve(ctx, nodeID)
	if err != nil {
		return "", err
	}
	resp, err := agent.StartDependencyInstall(ctx, &pb.StartInstallRequest{CoreTypes: cores, Uninstall: uninstall})
	if err != nil {
		return "", err
	}
	u.mu.Lock()
	u.jobs[resp.GetJobId()] = nodeID
	u.mu.Unlock()
	return resp.GetJobId(), nil
}

// Progress returns a job's status, steps and log lines since sinceCursor.
func (u *InstallUsecase) Progress(ctx context.Context, jobID string, sinceCursor int32) (*pb.InstallProgressResponse, error) {
	u.mu.Lock()
	nodeID, ok := u.jobs[jobID]
	u.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("install job %q not found", jobID)
	}
	agent, err := u.resolve(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return agent.GetInstallProgress(ctx, jobID, sinceCursor)
}
