package server

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/depinstall"
	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// depInstaller lazily builds the install manager (the manager holds no
// per-request state, so one per server is fine).
func (s *Server) depInstaller() *depinstall.Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.depMgr == nil {
		s.depMgr = depinstall.New(s.cfg.Cores.Download)
	}
	return s.depMgr
}

// ProbeDependencies reports which declared dependencies are present per core.
func (s *Server) ProbeDependencies(_ context.Context, req *pb.ProbeDependenciesRequest) (*pb.ProbeDependenciesResponse, error) {
	cores := make([]coredeps.Protocol, 0, len(req.GetCoreTypes()))
	for _, c := range req.GetCoreTypes() {
		cores = append(cores, coredeps.Protocol(c))
	}
	reports := s.depInstaller().Probe(cores)

	out := &pb.ProbeDependenciesResponse{Reports: make([]*pb.CoreDependencyReport, 0, len(reports))}
	for _, r := range reports {
		rep := &pb.CoreDependencyReport{CoreType: r.CoreType, Ready: r.Ready}
		for _, d := range r.Deps {
			rep.Deps = append(rep.Deps, &pb.DependencyStatus{
				Name:     d.Name,
				Kind:     string(d.Kind),
				Present:  d.Present,
				Version:  d.Version,
				Path:     d.Path,
				Optional: d.Optional,
			})
		}
		out.Reports = append(out.Reports, rep)
	}
	return out, nil
}

// StartDependencyInstall launches an async install job and returns its id. The
// download source is the pinned manifest; the request's proxy and artifact
// fields are not used.
func (s *Server) StartDependencyInstall(_ context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error) {
	if len(req.GetCoreTypes()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "core_types is required")
	}
	cores := make([]coredeps.Protocol, 0, len(req.GetCoreTypes()))
	for _, c := range req.GetCoreTypes() {
		p := coredeps.Protocol(c)
		if !p.Valid() {
			return nil, status.Errorf(codes.InvalidArgument, "unknown core type %q", c)
		}
		cores = append(cores, p)
	}
	jobID := s.depInstaller().StartInstall(depinstall.InstallRequest{
		Cores:     cores,
		Uninstall: req.GetUninstall(),
	})
	return &pb.StartInstallResponse{JobId: jobID}, nil
}

// GetInstallProgress returns the job status, step states, and new log lines.
func (s *Server) GetInstallProgress(_ context.Context, req *pb.InstallProgressRequest) (*pb.InstallProgressResponse, error) {
	snap, ok := s.depInstaller().Progress(req.GetJobId(), int(req.GetSinceCursor()))
	if !ok {
		return nil, status.Errorf(codes.NotFound, "install job %q not found", req.GetJobId())
	}
	out := &pb.InstallProgressResponse{
		Status:     snap.Status,
		LogLines:   snap.LogLines,
		NextCursor: int32(snap.NextCursor),
		Done:       snap.Done,
	}
	for _, st := range snap.Steps {
		out.Steps = append(out.Steps, &pb.InstallStep{
			Name:     st.Name,
			CoreType: st.CoreType,
			Kind:     st.Kind,
			State:    st.State,
			Detail:   st.Detail,
		})
	}
	return out, nil
}
