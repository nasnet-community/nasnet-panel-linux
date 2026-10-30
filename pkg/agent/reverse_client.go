package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Compile-time check: *ReverseClient must satisfy NodeClient.
var _ NodeClient = (*ReverseClient)(nil)

// SendCommandFunc dispatches a unary RPC over the reverse command channel.
type SendCommandFunc func(nodeUUID, method string, payload []byte, timeout time.Duration) ([]byte, error)

// SendControlFunc sends a control envelope over the reverse command channel.
type SendControlFunc func(nodeUUID string, env *hubpb.HubEnvelope) error

// ReverseClientDeps provides streaming-related waiter registration for the
// reverse channel.  These are used by OpenTerminal and StreamLogs (Tasks 14/15).
type ReverseClientDeps interface {
	RegisterTerminalWaiter(sessionID string) <-chan hubpb.AgentHub_TerminalChannelServer
	CleanupTerminalWaiter(sessionID string)
	RegisterLogStreamWaiter(streamID string) <-chan hubpb.AgentHub_LogChannelServer
	CleanupLogStreamWaiter(streamID string)
}

// ReverseClient implements NodeClient by dispatching every call through the
// reverse command channel instead of a direct gRPC connection.
type ReverseClient struct {
	nodeUUID    string
	sendCommand SendCommandFunc
	sendControl SendControlFunc
	deps        ReverseClientDeps
}

// NewReverseClient creates a new ReverseClient.
func NewReverseClient(nodeUUID string, sendCommand SendCommandFunc, sendControl SendControlFunc, deps ReverseClientDeps) *ReverseClient {
	return &ReverseClient{
		nodeUUID:    nodeUUID,
		sendCommand: sendCommand,
		sendControl: sendControl,
		deps:        deps,
	}
}

// timeoutFor returns the remaining time on the caller's context when it has
// a deadline, otherwise falls back to the per-method deadline from the
// rpcDeadlines table. method may be a full gRPC path or bare name.
func (rc *ReverseClient) timeoutFor(ctx context.Context, method string) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		d := time.Until(dl)
		if d <= 0 {
			return time.Millisecond // already expired; let sendCommand handle it
		}
		return d
	}
	return DeadlineFor(method)
}

// sendUnary marshals req, calls sendCommand, then unmarshals the response bytes
// into resp.
func (rc *ReverseClient) sendUnary(ctx context.Context, method string, req proto.Message, resp proto.Message) error {
	payload, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("reverse_client: marshal request for %s: %w", method, err)
	}
	respBytes, err := rc.sendCommand(rc.nodeUUID, method, payload, rc.timeoutFor(ctx, method))
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(respBytes, resp); err != nil {
		return fmt.Errorf("reverse_client: unmarshal response for %s: %w", method, err)
	}
	return nil
}

// sendUnaryCmd calls sendUnary expecting a CommandResponse and checks its
// Success field, converting a failure to an error.
func (rc *ReverseClient) sendUnaryCmd(ctx context.Context, method string, req proto.Message) error {
	var resp pb.CommandResponse
	if err := rc.sendUnary(ctx, method, req, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s failed: %s", method, resp.Message)
	}
	return nil
}

// ===== NodeClient: identity =====

// Close is a no-op for reverse clients — there is no dedicated connection to
// close.
func (rc *ReverseClient) Close() error { return nil }

// Target returns the node UUID used to route commands.
func (rc *ReverseClient) Target() string { return rc.nodeUUID }

// ===== Lifecycle =====

func (rc *ReverseClient) GetStatus(ctx context.Context) (*pb.NodeStatus, error) {
	var resp pb.NodeStatus
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetStatus_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (rc *ReverseClient) StartXray(ctx context.Context) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_StartXray_FullMethodName, &pb.Empty{})
}

func (rc *ReverseClient) StopXray(ctx context.Context, gracefulTimeout time.Duration) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_StopXray_FullMethodName, &pb.StopRequest{
		GracefulTimeoutSeconds: int32(gracefulTimeout.Seconds()),
	})
}

func (rc *ReverseClient) RestartXray(ctx context.Context, validateConfig bool) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_RestartXray_FullMethodName, &pb.RestartRequest{
		ValidateConfig: validateConfig,
	})
}

// ===== File management =====

func (rc *ReverseClient) WriteFile(ctx context.Context, path string, content []byte, perm uint32) (string, error) {
	var resp pb.CommandResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_WriteFile_FullMethodName, &pb.FilePayload{
		Path:    path,
		Content: content,
		Perm:    int32(perm),
	}, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("failed to write file: %s", resp.Message)
	}
	return resp.Message, nil
}

// ===== Configuration =====

func (rc *ReverseClient) PushConfig(ctx context.Context, configJSON string) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_PushConfig_FullMethodName, &pb.ConfigPayload{
		JsonContent: configJSON,
	})
}

// ApplyCoreState reconciles one non-Xray protocol core on the node.
func (rc *ReverseClient) ApplyCoreState(ctx context.Context, payload *pb.CoreStatePayload) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_ApplyCoreState_FullMethodName, payload)
}

// GetCoreStatus reports the runtime state of one non-Xray protocol core.
func (rc *ReverseClient) GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error) {
	var resp pb.CoreStatusResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetCoreStatus_FullMethodName, &pb.CoreStatusRequest{CoreType: coreType}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ProbeDependencies reports which declared dependencies are present per core.
func (rc *ReverseClient) ProbeDependencies(ctx context.Context, coreTypes []string) (*pb.ProbeDependenciesResponse, error) {
	var resp pb.ProbeDependenciesResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_ProbeDependencies_FullMethodName, &pb.ProbeDependenciesRequest{CoreTypes: coreTypes}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StartDependencyInstall launches an async install job on the node.
func (rc *ReverseClient) StartDependencyInstall(ctx context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error) {
	var resp pb.StartInstallResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_StartDependencyInstall_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetInstallProgress polls an install job's status/steps/log lines.
func (rc *ReverseClient) GetInstallProgress(ctx context.Context, jobID string, sinceCursor int32) (*pb.InstallProgressResponse, error) {
	var resp pb.InstallProgressResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetInstallProgress_FullMethodName, &pb.InstallProgressRequest{JobId: jobID, SinceCursor: sinceCursor}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (rc *ReverseClient) UpdateXrayAPIConfig(ctx context.Context, apiAddr string) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_UpdateXrayAPIConfig_FullMethodName, &pb.XrayAPIConfigPayload{
		ApiAddr: apiAddr,
	})
}

func (rc *ReverseClient) GetCurrentConfig(ctx context.Context) (string, string, error) {
	var resp pb.ConfigPayload
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetCurrentConfig_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return "", "", err
	}
	return resp.JsonContent, resp.Checksum, nil
}

func (rc *ReverseClient) ValidateConfig(ctx context.Context, configJSON string) (bool, []string, []string, error) {
	var resp pb.ValidationResult
	if err := rc.sendUnary(ctx, pb.NodeAgent_ValidateConfig_FullMethodName, &pb.ConfigPayload{
		JsonContent: configJSON,
	}, &resp); err != nil {
		return false, nil, nil, err
	}
	return resp.Valid, resp.Errors, resp.Warnings, nil
}

func (rc *ReverseClient) PushConfigAndRestart(ctx context.Context, configJSON string, _ bool) error {
	return rc.PushConfig(ctx, configJSON)
}

// ===== User management =====

func (rc *ReverseClient) AddUser(ctx context.Context, inboundTag, email, uuid, protocol, flow, encryption string, level int32) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_AddUser_FullMethodName, &pb.UserPayload{
		InboundTag: inboundTag,
		Email:      email,
		Uuid:       uuid,
		Protocol:   protocol,
		Flow:       flow,
		Encryption: encryption,
		Level:      level,
	})
}

func (rc *ReverseClient) RemoveUser(ctx context.Context, inboundTag, email string) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_RemoveUser_FullMethodName, &pb.UserPayload{
		InboundTag: inboundTag,
		Email:      email,
	})
}

func (rc *ReverseClient) ListUsers(ctx context.Context, inboundTag string) ([]*pb.UserInfo, error) {
	var resp pb.UserList
	if err := rc.sendUnary(ctx, pb.NodeAgent_ListUsers_FullMethodName, &pb.InboundSelector{
		InboundTag: inboundTag,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.Users, nil
}

// ===== Statistics =====

func (rc *ReverseClient) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	var resp pb.SystemStats
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetSystemStats_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return SystemStatsFromPB(&resp), nil
}

func (rc *ReverseClient) GetXrayStats(ctx context.Context, reset bool) (*XrayStats, error) {
	var resp pb.XrayStats
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetXrayStats_FullMethodName, &pb.StatsRequest{
		Reset_: reset,
	}, &resp); err != nil {
		return nil, err
	}
	return &XrayStats{
		UserUplink:       resp.UserUplink,
		UserDownlink:     resp.UserDownlink,
		InboundUplink:    resp.InboundUplink,
		InboundDownlink:  resp.InboundDownlink,
		OutboundUplink:   resp.OutboundUplink,
		OutboundDownlink: resp.OutboundDownlink,
		TotalUplink:      resp.TotalUplink,
		TotalDownlink:    resp.TotalDownlink,
	}, nil
}

// ===== Streaming (placeholders — implemented in Tasks 14 and 15) =====

func (rc *ReverseClient) StreamLogs(_ context.Context, _ int, _ bool) (pb.NodeAgent_StreamLogsClient, error) {
	return nil, fmt.Errorf("log streaming is not yet supported for reverse-mode nodes")
}

func (rc *ReverseClient) OpenTerminal(ctx context.Context) (pb.NodeAgent_OpenTerminalClient, error) {
	// 1. Generate a unique session ID.
	sessionID := uuid.New().String()

	// 2. Register a waiter so the hub knows to route the agent's TerminalChannel
	//    stream back to us once it arrives.
	waiterCh := rc.deps.RegisterTerminalWaiter(sessionID)
	defer rc.deps.CleanupTerminalWaiter(sessionID)

	// 3. Send an OpenTerminalSignal to the agent via the command channel.
	if err := rc.sendControl(rc.nodeUUID, &hubpb.HubEnvelope{
		Control: &hubpb.HubEnvelope_OpenTerminal{
			OpenTerminal: &hubpb.OpenTerminalSignal{
				SessionId: sessionID,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("reverse_client: send OpenTerminal signal: %w", err)
	}

	// 4. Wait for the agent to call back with its TerminalChannel stream (10s timeout).
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()

	var hubStream hubpb.AgentHub_TerminalChannelServer
	select {
	case s, ok := <-waiterCh:
		if !ok {
			return nil, fmt.Errorf("reverse_client: terminal waiter channel closed")
		}
		hubStream = s
	case <-waitCtx.Done():
		return nil, fmt.Errorf("reverse_client: timeout waiting for agent terminal stream")
	}

	// 5. Return the stream wrapped in an adapter that converts between
	//    hubpb.TerminalFrame and pb.TerminalInput / pb.TerminalOutput.
	adapter := &terminalStreamAdapter{stream: hubStream}
	// The hub owns a server RPC, so cancelling ctx cannot cancel the gRPC
	// transport automatically as it does for a direct client stream.
	if owner, ok := hubStream.(interface{ Close() }); ok {
		adapter.stopCancel = context.AfterFunc(ctx, owner.Close)
	}
	return adapter, nil
}

// terminalStreamAdapter wraps an AgentHub_TerminalChannelServer (which
// exchanges hubpb.TerminalFrame) to satisfy pb.NodeAgent_OpenTerminalClient
// (which exchanges pb.TerminalInput / pb.TerminalOutput).
type terminalStreamAdapter struct {
	stream     hubpb.AgentHub_TerminalChannelServer
	stopCancel func() bool
}

// Send converts a pb.TerminalInput to a hubpb.TerminalFrame and sends it.
func (a *terminalStreamAdapter) Send(in *pb.TerminalInput) error {
	frame := &hubpb.TerminalFrame{}
	switch p := in.Payload.(type) {
	case *pb.TerminalInput_Data:
		frame.Payload = &hubpb.TerminalFrame_Data{Data: p.Data}
	case *pb.TerminalInput_Resize:
		if p.Resize != nil {
			frame.Payload = &hubpb.TerminalFrame_Resize{
				Resize: &hubpb.TerminalResizeEvent{
					Cols: p.Resize.Cols,
					Rows: p.Resize.Rows,
				},
			}
		}
	case *pb.TerminalInput_Close:
		frame.Payload = &hubpb.TerminalFrame_Close{Close: true}
	}
	return a.stream.Send(frame)
}

// Recv receives a hubpb.TerminalFrame from the agent and converts it to a
// pb.TerminalOutput.
func (a *terminalStreamAdapter) Recv() (*pb.TerminalOutput, error) {
	frame, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}

	out := &pb.TerminalOutput{}
	switch p := frame.GetPayload().(type) {
	case *hubpb.TerminalFrame_Data:
		out.Payload = &pb.TerminalOutput_Data{Data: p.Data}
	case *hubpb.TerminalFrame_ExitCode:
		out.Payload = &pb.TerminalOutput_ExitCode{ExitCode: p.ExitCode}
	case *hubpb.TerminalFrame_Error:
		out.Payload = &pb.TerminalOutput_Error{Error: p.Error}
	case *hubpb.TerminalFrame_Close:
		return nil, io.EOF
	default:
		// Identification-only or unknown frame — skip.
		return a.Recv()
	}
	return out, nil
}

// CloseSend signals the agent that no more input is coming.
func (a *terminalStreamAdapter) CloseSend() error {
	if owner, ok := a.stream.(interface{ Close() }); ok {
		if a.stopCancel != nil {
			a.stopCancel()
		}
		owner.Close()
		return nil
	}
	// Compatibility for stream implementations without local ownership.
	// Send a close frame so the agent can tear down the PTY.
	return a.stream.Send(&hubpb.TerminalFrame{
		Payload: &hubpb.TerminalFrame_Close{Close: true},
	})
}

// The remaining methods satisfy the grpc.ClientStream portion of
// grpc.BidiStreamingClient. They delegate to the underlying server stream's
// context/metadata where possible and are no-ops otherwise, because the actual
// transport is the hub's server stream, not a client-initiated gRPC call.

func (a *terminalStreamAdapter) Header() (metadata.MD, error) { return nil, nil }
func (a *terminalStreamAdapter) Trailer() metadata.MD         { return nil }
func (a *terminalStreamAdapter) Context() context.Context     { return a.stream.Context() }
func (a *terminalStreamAdapter) SendMsg(m any) error          { return nil }
func (a *terminalStreamAdapter) RecvMsg(m any) error          { return nil }

// Compile-time check: terminalStreamAdapter must satisfy NodeAgent_OpenTerminalClient.
var _ pb.NodeAgent_OpenTerminalClient = (*terminalStreamAdapter)(nil)

// ===== Host info =====

func (rc *ReverseClient) GetHostInfo(ctx context.Context) (*HostInfo, error) {
	var resp pb.HostInfo
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetHostInfo_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &HostInfo{
		Hostname:             resp.Hostname,
		OS:                   resp.Os,
		Platform:             resp.Platform,
		PlatformFamily:       resp.PlatformFamily,
		PlatformVersion:      resp.PlatformVersion,
		KernelVersion:        resp.KernelVersion,
		Arch:                 resp.Arch,
		VirtualizationSystem: resp.VirtualizationSystem,
		VirtualizationRole:   resp.VirtualizationRole,
		CPUModelName:         resp.CpuModelName,
		CPUCores:             resp.CpuCores,
		TotalMemory:          resp.TotalMemory,
		TotalSwap:            resp.TotalSwap,
		BootTime:             uint64(resp.BootTime),
	}, nil
}

// ===== Health =====

func (rc *ReverseClient) HealthCheck(ctx context.Context) (*HealthResult, error) {
	var resp pb.HealthResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_HealthCheck_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	components := make(map[string]ComponentHealth)
	for name, comp := range resp.Components {
		components[name] = ComponentHealth{
			Status:  HealthStatus(comp.Status),
			Message: comp.Message,
		}
	}
	return &HealthResult{
		Status:     HealthStatus(resp.Status),
		Message:    resp.Message,
		Components: components,
	}, nil
}

// ===== Version =====

func (rc *ReverseClient) GetVersion(ctx context.Context) (*VersionInfo, error) {
	var resp pb.VersionInfo
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetVersion_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &VersionInfo{
		AgentVersion:   resp.AgentVersion,
		AgentCommit:    resp.AgentCommit,
		AgentBuildTime: resp.AgentBuildTime,
		XrayVersion:    resp.XrayVersion,
		GoVersion:      resp.GoVersion,
		OS:             resp.Os,
		Arch:           resp.Arch,
	}, nil
}

// ===== Connectivity =====

func (rc *ReverseClient) Ping(ctx context.Context) (int64, error) {
	start := time.Now()
	var resp pb.NodeStatus
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetStatus_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

func (rc *ReverseClient) Heartbeat(ctx context.Context) (pb.NodeAgent_HeartbeatClient, error) {
	return nil, fmt.Errorf("heartbeat is handled natively by the reverse command channel")
}

// ===== SSH =====

func (rc *ReverseClient) GetSSHStatus(ctx context.Context) (*pb.SSHStatus, error) {
	var resp pb.SSHStatus
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetSSHStatus_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (rc *ReverseClient) UpdateSSHConfig(ctx context.Context, enabled bool, port int) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_UpdateSSHConfig_FullMethodName, &pb.SSHConfigPayload{
		Enabled: enabled,
		Port:    int32(port),
	})
}

func (rc *ReverseClient) ClearSSHLogs(ctx context.Context) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_ClearSSHLogs_FullMethodName, &pb.Empty{})
}

func (rc *ReverseClient) RestartSSH(ctx context.Context) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_RestartSSH_FullMethodName, &pb.Empty{})
}

// ===== Self-update =====

func (rc *ReverseClient) SelfUpdate(ctx context.Context, binaryContent []byte, checksum, version string, restartAfter bool, signature []byte, force bool) (*UpdateResult, error) {
	var resp pb.UpdateResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_SelfUpdate_FullMethodName, &pb.UpdateRequest{
		BinaryContent: binaryContent,
		Checksum:      checksum,
		Version:       version,
		RestartAfter:  restartAfter,
		Signature:     signature,
		Force:         force,
	}, &resp); err != nil {
		return nil, fmt.Errorf("failed to call SelfUpdate: %w", err)
	}
	return &UpdateResult{
		Success:    resp.Success,
		Message:    resp.Message,
		OldVersion: resp.OldVersion,
		NewVersion: resp.NewVersion,
	}, nil
}

func (rc *ReverseClient) GetSelfChecksum(ctx context.Context) (*ChecksumResult, error) {
	var resp pb.ChecksumResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetSelfChecksum_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &ChecksumResult{
		Checksum: resp.Checksum,
		Path:     resp.Path,
	}, nil
}

// ===== Certificate denylist =====

func (rc *ReverseClient) UpdateCertDenylist(ctx context.Context, serialNumbers []string, denylistHash string) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_UpdateCertDenylist_FullMethodName, &pb.CertDenylist{
		RevokedSerialNumbers: serialNumbers,
		DenylistHash:         denylistHash,
	})
}

// ===== Xray version management =====

func (rc *ReverseClient) UpdateXrayBinary(ctx context.Context, version string, restartAfter bool, downloadURL, downloadToken, checksum string) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_UpdateXrayBinary_FullMethodName, &pb.UpdateXrayRequest{
		Version:       version,
		RestartAfter:  restartAfter,
		DownloadUrl:   downloadURL,
		DownloadToken: downloadToken,
		Checksum:      checksum,
	})
}

// ===== Outbound testing =====

func (rc *ReverseClient) TestOutbound(ctx context.Context, spec *OutboundTestSpec) (*OutboundTestResult, error) {
	var resp pb.OutboundTestResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_TestOutbound_FullMethodName, OutboundTestRequestToPB(spec), &resp); err != nil {
		return nil, err
	}
	return OutboundTestResultFromPB(&resp), nil
}

// ===== Online user detection =====

func (rc *ReverseClient) GetUserOnlineIPs(ctx context.Context, email string) (map[string]int64, error) {
	var resp pb.OnlineIPsResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetUserOnlineIPs_FullMethodName, &pb.UserEmailRequest{Email: email}, &resp); err != nil {
		return nil, err
	}
	return resp.Ips, nil
}

func (rc *ReverseClient) GetAllUsersOnlineIPs(ctx context.Context) (map[string]map[string]int64, error) {
	snapshot, err := rc.GetOnlineIPSnapshot(ctx)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return snapshot.Users, nil
}

func (rc *ReverseClient) GetOnlineIPSnapshot(ctx context.Context) (*OnlineIPSnapshot, error) {
	var resp pb.AllOnlineIPsResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetAllUsersOnlineIPs_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &OnlineIPSnapshot{Users: OnlineIPsFromPB(&resp), CollectedAtUnixMs: resp.CollectedAtUnixMs}, nil
}

// ===== Buffered traffic =====

func (rc *ReverseClient) GetBufferedTraffic(ctx context.Context) (*BufferedTrafficStats, error) {
	var resp pb.BufferedTrafficStats
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetBufferedTraffic_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	// Shared mapper also carries CoreUsage — the old inline mapping dropped
	// it, starving per-tunnel core attribution on reverse-mode nodes.
	return BufferedTrafficFromPB(&resp), nil
}

// GetStatsBundle mirrors (*Client).GetStatsBundle over the reverse channel.
// Pre-bundle agents answer "unknown method"; callers fall back.
func (rc *ReverseClient) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*StatsBundle, error) {
	var resp pb.StatsBundle
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetStatsBundle_FullMethodName, &pb.StatsBundleRequest{IncludeOnlineIps: includeOnlineIPs}, &resp); err != nil {
		return nil, err
	}
	return &StatsBundle{
		System:                     SystemStatsFromPB(resp.System),
		Status:                     resp.Status,
		Buffered:                   BufferedTrafficFromPB(resp.BufferedTraffic),
		OnlineIPs:                  OnlineIPsFromPB(resp.OnlineIps),
		OnlineIPsCollectedAtUnixMs: resp.OnlineIps.GetCollectedAtUnixMs(),
	}, nil
}

func (rc *ReverseClient) AckBufferedTraffic(ctx context.Context, throughTimestamp int64) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_AckBufferedTraffic_FullMethodName, &pb.AckTrafficRequest{
		AckedThroughTimestamp: throughTimestamp,
	})
}

// ===== Tools =====

func (rc *ReverseClient) GenerateVLESSKeys(ctx context.Context) ([]VLESSKeyPair, error) {
	var resp pb.VLESSKeysResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GenerateVLESSKeys_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	keys := make([]VLESSKeyPair, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		keys = append(keys, VLESSKeyPair{
			Label:      k.Label,
			Decryption: k.Decryption,
			Encryption: k.Encryption,
		})
	}
	return keys, nil
}

// ===== Bandwidth shaping =====

func (rc *ReverseClient) SetupBandwidth(ctx context.Context, iface string, totalBWMbps int) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_SetupBandwidth_FullMethodName, &pb.BandwidthRequest{
		InterfaceName: iface,
		TotalBwMbps:   int32(totalBWMbps),
	})
}

func (rc *ReverseClient) TeardownBandwidth(ctx context.Context) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_TeardownBandwidth_FullMethodName, &pb.Empty{})
}

// ===== Access logs =====

func (rc *ReverseClient) GetAccessLogs(ctx context.Context, email string, limit int32) ([]*pb.AccessLogEntry, error) {
	var resp pb.AccessLogResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetAccessLogs_FullMethodName, &pb.AccessLogRequest{
		Email: email,
		Limit: limit,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (rc *ReverseClient) GetBufferedAccessLogSummary(ctx context.Context) (*pb.BufferedAccessLogSummary, error) {
	var resp pb.BufferedAccessLogSummary
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetBufferedAccessLogSummary_FullMethodName, &pb.Empty{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (rc *ReverseClient) AckBufferedAccessLogSummary(ctx context.Context, upToTimestamp int64, cfg AccessLogAckConfig) error {
	graceSec := int32(0)
	if cfg.GracePeriod > 0 {
		graceSec = int32(cfg.GracePeriod / time.Second)
	}
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_AckBufferedAccessLogSummary_FullMethodName, &pb.AckAccessLogSummaryRequest{
		UpToTimestamp:             upToTimestamp,
		GracePeriodSeconds:        graceSec,
		MaxDomainsPerHour:         cfg.MaxDomainsPerHour,
		MaxRejectedDomainsPerHour: cfg.MaxRejectedDomainsPerHour,
		MaxSourceIpsPerHour:       cfg.MaxSourceIPsPerHour,
	})
}

// ===== Starlink monitoring =====

func (rc *ReverseClient) GetStarlinkStatus(ctx context.Context, dishAddr string) (*StarlinkStatus, error) {
	var resp pb.StarlinkStatusResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetStarlinkStatus_FullMethodName, &pb.StarlinkRequest{DishAddress: dishAddr}, &resp); err != nil {
		return nil, err
	}
	return &StarlinkStatus{
		Available:                        resp.Available,
		DownlinkThroughputBps:            float64(resp.DownlinkThroughputBps),
		UplinkThroughputBps:              float64(resp.UplinkThroughputBps),
		PopPingLatencyMs:                 float64(resp.PopPingLatencyMs),
		PopPingDropRate:                  float64(resp.PopPingDropRate),
		EthSpeedMbps:                     resp.EthSpeedMbps,
		ObstructionFraction:              float64(resp.ObstructionFraction),
		CurrentlyObstructed:              resp.CurrentlyObstructed,
		AvgProlongedObstructionDurationS: float64(resp.AvgProlongedObstructionDurationS),
		AvgProlongedObstructionIntervalS: float64(resp.AvgProlongedObstructionIntervalS),
		AlertThermalThrottle:             resp.AlertThermalThrottle,
		AlertThermalShutdown:             resp.AlertThermalShutdown,
		AlertIsHeating:                   resp.AlertIsHeating,
		AlertSlowEthernet:                resp.AlertSlowEthernet,
		AlertPowerSaveIdle:               resp.AlertPowerSaveIdle,
		AlertMotorsStuck:                 resp.AlertMotorsStuck,
		AlertNoEthernetLink:              resp.AlertNoEthernetLink,
		AlertUnexpectedLocation:          resp.AlertUnexpectedLocation,
		AlertRoaming:                     resp.AlertRoaming,
		AlertMastNotNearVert:             resp.AlertMastNotNearVertical,
		AlertInstallPending:              resp.AlertInstallPending,
		HardwareVersion:                  resp.HardwareVersion,
		SoftwareVersion:                  resp.SoftwareVersion,
		CountryCode:                      resp.CountryCode,
		UptimeS:                          resp.UptimeS,
		BootCount:                        resp.BootCount,
		GpsValid:                         resp.GpsValid,
		GpsSats:                          resp.GpsSats,
		TiltAngleDeg:                     float64(resp.TiltAngleDeg),
		BoresightAzimuthDeg:              float64(resp.BoresightAzimuthDeg),
		BoresightElevationDeg:            float64(resp.BoresightElevationDeg),
		SoftwareUpdateState:              resp.SoftwareUpdateState,
		SoftwareUpdateProgress:           float64(resp.SoftwareUpdateProgress),
		OutageCause:                      resp.OutageCause,
		OutageDurationNs:                 resp.OutageDurationNs,
		DisablementCode:                  resp.DisablementCode,
		MobilityClass:                    resp.MobilityClass,
		ClassOfService:                   resp.ClassOfService,
		CellID:                           resp.CellId,
		SatelliteID:                      resp.SatelliteId,
		GatewayID:                        resp.GatewayId,
		OnBackupBeam:                     resp.OnBackupBeam,
		Latitude:                         resp.Latitude,
		Longitude:                        resp.Longitude,
		Altitude:                         resp.Altitude,
		IsSnrAboveNoiseFloor:             resp.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:             resp.IsSnrPersistentlyLow,
	}, nil
}

func (rc *ReverseClient) GetStarlinkObstructionMap(ctx context.Context, dishAddr string) (*StarlinkObstructionMap, error) {
	var resp pb.StarlinkObstructionMapResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_GetStarlinkObstructionMap_FullMethodName, &pb.StarlinkRequest{DishAddress: dishAddr}, &resp); err != nil {
		return nil, err
	}
	return &StarlinkObstructionMap{
		NumRows:        resp.NumRows,
		NumCols:        resp.NumCols,
		SNR:            resp.Snr,
		MaxThetaDeg:    resp.MaxThetaDeg,
		ReferenceFrame: resp.ReferenceFrame,
	}, nil
}

// ===== Command execution =====

func (rc *ReverseClient) ExecuteCommand(ctx context.Context, command string, timeoutSecs int) (*pb.ExecuteCommandResponse, error) {
	var resp pb.ExecuteCommandResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_ExecuteCommand_FullMethodName, &pb.ExecuteCommandRequest{
		Command:        command,
		TimeoutSeconds: int32(timeoutSecs),
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ===== Moving the agent =====

func (rc *ReverseClient) MoveListenPort(ctx context.Context, req *pb.MoveListenPortRequest) (*pb.CommandResponse, error) {
	var resp pb.CommandResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_MoveListenPort_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (rc *ReverseClient) SwitchConnectMode(ctx context.Context, req *pb.SwitchConnectModeRequest) (*pb.CommandResponse, error) {
	var resp pb.CommandResponse
	if err := rc.sendUnary(ctx, pb.NodeAgent_SwitchConnectMode_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ===== Cleanup =====

func (rc *ReverseClient) Uninstall(ctx context.Context) error {
	return rc.sendUnaryCmd(ctx, pb.NodeAgent_Uninstall_FullMethodName, &pb.Empty{})
}

// ===== Nuke / Wipe =====

// Wipe invokes the Wipe RPC over the reverse channel as a unary command.
// Wipe returns a NukeReport rather than a CommandResponse, so we use the
// lower-level sendUnary helper (same pattern as GetStatus).
func (rc *ReverseClient) Wipe(ctx context.Context, req *pb.NukeRequest) (*pb.NukeReport, error) {
	var resp pb.NukeReport
	if err := rc.sendUnary(ctx, pb.NodeAgent_Wipe_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Nuke (server-streaming) is not supported over the reverse channel today.
// Hub orchestration must detect reverse-mode nodes and fall back to Wipe.
func (rc *ReverseClient) Nuke(_ context.Context, _ *pb.NukeRequest) (pb.NodeAgent_NukeClient, error) {
	return nil, status.Error(codes.Unimplemented,
		"nuke streaming is not supported over the reverse channel; use Wipe instead")
}
