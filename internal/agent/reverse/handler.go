package reverse

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/protobuf/proto"
)

// CommandHandler wraps a pb.NodeAgentServer and dispatches commands by method name.
type CommandHandler struct {
	srv pb.NodeAgentServer
}

// NewCommandHandler creates a new CommandHandler wrapping the given server.
func NewCommandHandler(srv pb.NodeAgentServer) *CommandHandler {
	return &CommandHandler{srv: srv}
}

// call is a generic helper that unmarshals payload into req, calls handler, and marshals the response.
func (h *CommandHandler) call(ctx context.Context, payload []byte, req proto.Message, handler func(context.Context, proto.Message) (proto.Message, error)) ([]byte, error) {
	if len(payload) > 0 {
		if err := proto.Unmarshal(payload, req); err != nil {
			return nil, fmt.Errorf("unmarshal request: %w", err)
		}
	}
	resp, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(resp)
}

// GetNodeStatus calls GetStatus on the underlying server.
func (h *CommandHandler) GetNodeStatus(ctx context.Context) (*pb.NodeStatus, error) {
	return h.srv.GetStatus(ctx, &pb.Empty{})
}

// StreamLogs passes through to the underlying server's StreamLogs method.
func (h *CommandHandler) StreamLogs(req *pb.LogRequest, stream pb.NodeAgent_StreamLogsServer) error {
	return h.srv.StreamLogs(req, stream)
}

// HandleCommand dispatches a method name and serialized payload to the appropriate server handler.
func (h *CommandHandler) HandleCommand(ctx context.Context, method string, payload []byte) ([]byte, error) {
	// Strip gRPC full method path prefix (e.g., "/nodeagent.NodeAgent/GetStatus" → "GetStatus")
	if i := strings.LastIndex(method, "/"); i >= 0 {
		method = method[i+1:]
	}

	switch method {
	case "FetchPoolSource":
		return h.call(ctx, payload, &pb.PoolSourceRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.FetchPoolSource(ctx, req.(*pb.PoolSourceRequest))
		})
	case "TestPoolCandidate":
		return h.call(ctx, payload, &pb.PoolCandidateRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.TestPoolCandidate(ctx, req.(*pb.PoolCandidateRequest))
		})
	case "ProbeRealityDest":
		return h.call(ctx, payload, &pb.RealityDestProbeRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ProbeRealityDest(ctx, req.(*pb.RealityDestProbeRequest))
		})
	case "GetStatus":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetStatus(ctx, req.(*pb.Empty))
		})
	case "StartXray":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.StartXray(ctx, req.(*pb.Empty))
		})
	case "StopXray":
		return h.call(ctx, payload, &pb.StopRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.StopXray(ctx, req.(*pb.StopRequest))
		})
	case "RestartXray":
		return h.call(ctx, payload, &pb.RestartRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.RestartXray(ctx, req.(*pb.RestartRequest))
		})
	case "WriteFile":
		return h.call(ctx, payload, &pb.FilePayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.WriteFile(ctx, req.(*pb.FilePayload))
		})
	case "PushConfig":
		return h.call(ctx, payload, &pb.ConfigPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.PushConfig(ctx, req.(*pb.ConfigPayload))
		})
	case "UpdateXrayAPIConfig":
		return h.call(ctx, payload, &pb.XrayAPIConfigPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.UpdateXrayAPIConfig(ctx, req.(*pb.XrayAPIConfigPayload))
		})
	case "GetCurrentConfig":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetCurrentConfig(ctx, req.(*pb.Empty))
		})
	case "ValidateConfig":
		return h.call(ctx, payload, &pb.ConfigPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ValidateConfig(ctx, req.(*pb.ConfigPayload))
		})
	case "AddUser":
		return h.call(ctx, payload, &pb.UserPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.AddUser(ctx, req.(*pb.UserPayload))
		})
	case "RemoveUser":
		return h.call(ctx, payload, &pb.UserPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.RemoveUser(ctx, req.(*pb.UserPayload))
		})
	case "ListUsers":
		return h.call(ctx, payload, &pb.InboundSelector{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ListUsers(ctx, req.(*pb.InboundSelector))
		})
	case "GetSystemStats":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetSystemStats(ctx, req.(*pb.Empty))
		})
	case "GetHostInfo":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetHostInfo(ctx, req.(*pb.Empty))
		})
	case "GetXrayStats":
		return h.call(ctx, payload, &pb.StatsRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetXrayStats(ctx, req.(*pb.StatsRequest))
		})
	case "HealthCheck":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.HealthCheck(ctx, req.(*pb.Empty))
		})
	case "GetVersion":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetVersion(ctx, req.(*pb.Empty))
		})
	case "GetSSHStatus":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetSSHStatus(ctx, req.(*pb.Empty))
		})
	case "UpdateSSHConfig":
		return h.call(ctx, payload, &pb.SSHConfigPayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.UpdateSSHConfig(ctx, req.(*pb.SSHConfigPayload))
		})
	case "ClearSSHLogs":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ClearSSHLogs(ctx, req.(*pb.Empty))
		})
	case "RestartSSH":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.RestartSSH(ctx, req.(*pb.Empty))
		})
	case "SelfUpdate":
		return h.call(ctx, payload, &pb.UpdateRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.SelfUpdate(ctx, req.(*pb.UpdateRequest))
		})
	case "GetSelfChecksum":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetSelfChecksum(ctx, req.(*pb.Empty))
		})
	case "UpdateCertDenylist":
		return h.call(ctx, payload, &pb.CertDenylist{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.UpdateCertDenylist(ctx, req.(*pb.CertDenylist))
		})
	case "UpdateXrayBinary":
		return h.call(ctx, payload, &pb.UpdateXrayRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.UpdateXrayBinary(ctx, req.(*pb.UpdateXrayRequest))
		})
	case "TestOutbound":
		return h.call(ctx, payload, &pb.OutboundTestRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.TestOutbound(ctx, req.(*pb.OutboundTestRequest))
		})
	case "GetUserOnlineIPs":
		return h.call(ctx, payload, &pb.UserEmailRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetUserOnlineIPs(ctx, req.(*pb.UserEmailRequest))
		})
	case "GetAllUsersOnlineIPs":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetAllUsersOnlineIPs(ctx, req.(*pb.Empty))
		})
	case "GetBufferedTraffic":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetBufferedTraffic(ctx, req.(*pb.Empty))
		})
	case "GetStatsBundle":
		return h.call(ctx, payload, &pb.StatsBundleRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetStatsBundle(ctx, req.(*pb.StatsBundleRequest))
		})
	case "AckBufferedTraffic":
		return h.call(ctx, payload, &pb.AckTrafficRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.AckBufferedTraffic(ctx, req.(*pb.AckTrafficRequest))
		})
	case "GenerateVLESSKeys":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GenerateVLESSKeys(ctx, req.(*pb.Empty))
		})
	case "SetupBandwidth":
		return h.call(ctx, payload, &pb.BandwidthRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.SetupBandwidth(ctx, req.(*pb.BandwidthRequest))
		})
	case "TeardownBandwidth":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.TeardownBandwidth(ctx, req.(*pb.Empty))
		})
	case "GetAccessLogs":
		return h.call(ctx, payload, &pb.AccessLogRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetAccessLogs(ctx, req.(*pb.AccessLogRequest))
		})
	case "GetBufferedAccessLogSummary":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetBufferedAccessLogSummary(ctx, req.(*pb.Empty))
		})
	case "AckBufferedAccessLogSummary":
		return h.call(ctx, payload, &pb.AckAccessLogSummaryRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.AckBufferedAccessLogSummary(ctx, req.(*pb.AckAccessLogSummaryRequest))
		})
	case "GetStarlinkStatus":
		return h.call(ctx, payload, &pb.StarlinkRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetStarlinkStatus(ctx, req.(*pb.StarlinkRequest))
		})
	case "GetStarlinkObstructionMap":
		return h.call(ctx, payload, &pb.StarlinkRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetStarlinkObstructionMap(ctx, req.(*pb.StarlinkRequest))
		})
	case "StarlinkHandle":
		return h.call(ctx, payload, &pb.StarlinkHandleRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.StarlinkHandle(ctx, req.(*pb.StarlinkHandleRequest))
		})
	case "InstallCertificate":
		return h.call(ctx, payload, &pb.InstallCertificateRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.InstallCertificate(ctx, req.(*pb.InstallCertificateRequest))
		})
	case "InspectCertFile":
		return h.call(ctx, payload, &pb.InspectCertFileRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.InspectCertFile(ctx, req.(*pb.InspectCertFileRequest))
		})
	case "MoveListenPort":
		return h.call(ctx, payload, &pb.MoveListenPortRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.MoveListenPort(ctx, req.(*pb.MoveListenPortRequest))
		})
	case "SwitchConnectMode":
		return h.call(ctx, payload, &pb.SwitchConnectModeRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.SwitchConnectMode(ctx, req.(*pb.SwitchConnectModeRequest))
		})
	case "ExecuteCommand":
		return h.call(ctx, payload, &pb.ExecuteCommandRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ExecuteCommand(ctx, req.(*pb.ExecuteCommandRequest))
		})
	case "Uninstall":
		return h.call(ctx, payload, &pb.Empty{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.Uninstall(ctx, req.(*pb.Empty))
		})
	case "Wipe":
		// The hub falls back to Wipe for reverse nodes: Nuke is a server stream.
		return h.call(ctx, payload, &pb.NukeRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.Wipe(ctx, req.(*pb.NukeRequest))
		})
	case "ApplyCoreState":
		return h.call(ctx, payload, &pb.CoreStatePayload{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ApplyCoreState(ctx, req.(*pb.CoreStatePayload))
		})
	case "GetCoreStatus":
		return h.call(ctx, payload, &pb.CoreStatusRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetCoreStatus(ctx, req.(*pb.CoreStatusRequest))
		})
	case "ProbeDependencies":
		return h.call(ctx, payload, &pb.ProbeDependenciesRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.ProbeDependencies(ctx, req.(*pb.ProbeDependenciesRequest))
		})
	case "StartDependencyInstall":
		return h.call(ctx, payload, &pb.StartInstallRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.StartDependencyInstall(ctx, req.(*pb.StartInstallRequest))
		})
	case "GetInstallProgress":
		return h.call(ctx, payload, &pb.InstallProgressRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return h.srv.GetInstallProgress(ctx, req.(*pb.InstallProgressRequest))
		})
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}
