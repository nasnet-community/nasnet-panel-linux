package agent

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc/metadata"
)

var _ NodeClient = (*EmbeddedClient)(nil)

// EmbeddedClient talks to the local agent server in-process (no gRPC/TLS).
// Same shape as the gRPC Client, just calls c.srv.X directly.
type EmbeddedClient struct {
	srv *server.Server
}

func NewEmbeddedClient(srv *server.Server) *EmbeddedClient {
	return &EmbeddedClient{srv: srv}
}

// Close is a no-op — the server is owned by cmd, not the client.
func (c *EmbeddedClient) Close() error { return nil }

func (c *EmbeddedClient) Target() string { return "embedded" }

// ===== Lifecycle Methods =====

func (c *EmbeddedClient) GetStatus(ctx context.Context) (*pb.NodeStatus, error) {
	return c.srv.GetStatus(ctx, &pb.Empty{})
}

func (c *EmbeddedClient) StartXray(ctx context.Context) error {
	resp, err := c.srv.StartXray(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("start xray", resp)
	}
	return nil
}

func (c *EmbeddedClient) StopXray(ctx context.Context, gracefulTimeout time.Duration) error {
	resp, err := c.srv.StopXray(ctx, &pb.StopRequest{
		GracefulTimeoutSeconds: int32(gracefulTimeout.Seconds()),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("stop xray", resp)
	}
	return nil
}

func (c *EmbeddedClient) RestartXray(ctx context.Context, validateConfig bool) error {
	resp, err := c.srv.RestartXray(ctx, &pb.RestartRequest{
		ValidateConfig: validateConfig,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("restart xray", resp)
	}
	return nil
}

// ===== File Management Methods =====

func (c *EmbeddedClient) WriteFile(ctx context.Context, path string, content []byte, perm uint32) (string, error) {
	resp, err := c.srv.WriteFile(ctx, &pb.FilePayload{
		Path:    path,
		Content: content,
		Perm:    int32(perm),
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", errFromResp("write file", resp)
	}
	// On success, Message contains the absolute path.
	return resp.Message, nil
}

// ===== Configuration Methods =====

func (c *EmbeddedClient) PushConfig(ctx context.Context, configJSON string) error {
	resp, err := c.srv.PushConfig(ctx, &pb.ConfigPayload{
		JsonContent: configJSON,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("push config", resp)
	}
	return nil
}

func (c *EmbeddedClient) UpdateXrayAPIConfig(ctx context.Context, apiAddr string) error {
	resp, err := c.srv.UpdateXrayAPIConfig(ctx, &pb.XrayAPIConfigPayload{
		ApiAddr: apiAddr,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("update xray api config", resp)
	}
	return nil
}

func (c *EmbeddedClient) GetCurrentConfig(ctx context.Context) (string, string, error) {
	resp, err := c.srv.GetCurrentConfig(ctx, &pb.Empty{})
	if err != nil {
		return "", "", err
	}
	return resp.JsonContent, resp.Checksum, nil
}

func (c *EmbeddedClient) ValidateConfig(ctx context.Context, configJSON string) (bool, []string, []string, error) {
	resp, err := c.srv.ValidateConfig(ctx, &pb.ConfigPayload{
		JsonContent: configJSON,
	})
	if err != nil {
		return false, nil, nil, err
	}
	return resp.Valid, resp.Errors, resp.Warnings, nil
}

// PushConfig already restarts xray, so this just pushes.
func (c *EmbeddedClient) PushConfigAndRestart(ctx context.Context, configJSON string, _ bool) error {
	return c.PushConfig(ctx, configJSON)
}

// ===== User Management Methods =====

func (c *EmbeddedClient) AddUser(ctx context.Context, inboundTag, email, uuid, protocol, flow, encryption string, level int32) error {
	resp, err := c.srv.AddUser(ctx, &pb.UserPayload{
		InboundTag: inboundTag,
		Email:      email,
		Uuid:       uuid,
		Protocol:   protocol,
		Flow:       flow,
		Level:      level,
		Encryption: encryption,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("add user", resp)
	}
	return nil
}

func (c *EmbeddedClient) RemoveUser(ctx context.Context, inboundTag, email string) error {
	resp, err := c.srv.RemoveUser(ctx, &pb.UserPayload{
		InboundTag: inboundTag,
		Email:      email,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("remove user", resp)
	}
	return nil
}

func (c *EmbeddedClient) ListUsers(ctx context.Context, inboundTag string) ([]*pb.UserInfo, error) {
	resp, err := c.srv.ListUsers(ctx, &pb.InboundSelector{
		InboundTag: inboundTag,
	})
	if err != nil {
		return nil, err
	}
	return resp.Users, nil
}

// ===== Statistics Methods =====

func (c *EmbeddedClient) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	resp, err := c.srv.GetSystemStats(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return &SystemStats{
		CPUUsagePercent:     resp.CpuUsagePercent,
		MemoryTotalBytes:    resp.MemoryTotalBytes,
		MemoryUsedBytes:     resp.MemoryUsedBytes,
		MemoryUsagePercent:  resp.MemoryUsagePercent,
		DiskTotalBytes:      resp.DiskTotalBytes,
		DiskUsedBytes:       resp.DiskUsedBytes,
		DiskUsagePercent:    resp.DiskUsagePercent,
		NetworkRecvRate:     resp.NetworkRecvRate,
		NetworkSentRate:     resp.NetworkSentRate,
		LoadAvg1:            resp.LoadAvg_1,
		LoadAvg5:            resp.LoadAvg_5,
		LoadAvg15:           resp.LoadAvg_15,
		SystemUptimeSeconds: resp.SystemUptimeSeconds,
		TcpCount:            resp.TcpConns,
		UdpCount:            resp.UdpConns,
		FdCount:             resp.FdCount,
	}, nil
}

func (c *EmbeddedClient) GetXrayStats(ctx context.Context, reset bool) (*XrayStats, error) {
	resp, err := c.srv.GetXrayStats(ctx, &pb.StatsRequest{
		Reset_: reset,
	})
	if err != nil {
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

// ===== Streaming Methods =====

// StreamLogs runs the handler in a goroutine, returns the read end of the pipe.
func (c *EmbeddedClient) StreamLogs(ctx context.Context, tail int, follow bool) (pb.NodeAgent_StreamLogsClient, error) {
	pipe := newServerStreamPipe[pb.LogEntry](ctx)
	go func() {
		pipe.finish(c.srv.StreamLogs(&pb.LogRequest{
			TailLines: int32(tail),
			Follow:    follow,
			LogType:   pb.LogType_LOG_TYPE_ALL,
		}, pipe))
	}()
	return pipe, nil
}

// OpenTerminal runs the bidi PTY handler over an in-memory pipe.
func (c *EmbeddedClient) OpenTerminal(ctx context.Context) (pb.NodeAgent_OpenTerminalClient, error) {
	pipe := newBidiStreamPipe[pb.TerminalInput, pb.TerminalOutput](ctx)
	go func() {
		pipe.finish(c.srv.OpenTerminal(bidiServerStream[pb.TerminalInput, pb.TerminalOutput]{p: pipe}))
	}()
	return bidiClientStream[pb.TerminalInput, pb.TerminalOutput]{p: pipe}, nil
}

// ===== Host info =====

func (c *EmbeddedClient) GetHostInfo(ctx context.Context) (*HostInfo, error) {
	resp, err := c.srv.GetHostInfo(ctx, &pb.Empty{})
	if err != nil {
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

func (c *EmbeddedClient) ListInterfaces(ctx context.Context) ([]NetInterface, error) {
	ifs, addrs, err := c.srv.ListNetInterfaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NetInterface, 0, len(ifs))
	for _, in := range ifs {
		out = append(out, NetInterface{
			IfName:       in.IfName,
			PermMAC:      in.PermMAC,
			MAC:          in.MAC,
			IDPath:       in.IDPath,
			KeyKind:      string(in.KeyKind),
			Key:          in.Key,
			Source:       string(in.Source),
			Confidence:   in.Confidence,
			Driver:       in.Driver,
			Carrier:      in.Carrier,
			OperState:    in.OperState,
			SpeedMbit:    in.SpeedMbit,
			MTU:          in.MTU,
			Phy:          in.Phy,
			USBSpeedMbit: in.USBSpeedMbit,
			Assignable:   in.Assignable,
			Addrs:        addrs[in.IfName],
		})
	}
	return out, nil
}

// ===== Health =====

func (c *EmbeddedClient) HealthCheck(ctx context.Context) (*HealthResult, error) {
	resp, err := c.srv.HealthCheck(ctx, &pb.Empty{})
	if err != nil {
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

func (c *EmbeddedClient) GetVersion(ctx context.Context) (*VersionInfo, error) {
	resp, err := c.srv.GetVersion(ctx, &pb.Empty{})
	if err != nil {
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

func (c *EmbeddedClient) Ping(ctx context.Context) (int64, error) {
	start := time.Now()
	if _, err := c.srv.GetStatus(ctx, &pb.Empty{}); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// Heartbeat runs the bidi heartbeat handler over an in-memory pipe.
func (c *EmbeddedClient) Heartbeat(ctx context.Context) (pb.NodeAgent_HeartbeatClient, error) {
	pipe := newBidiStreamPipe[pb.HeartbeatPing, pb.HeartbeatPong](ctx)
	go func() {
		pipe.finish(c.srv.Heartbeat(bidiServerStream[pb.HeartbeatPing, pb.HeartbeatPong]{p: pipe}))
	}()
	return bidiClientStream[pb.HeartbeatPing, pb.HeartbeatPong]{p: pipe}, nil
}

// ===== SSH =====

func (c *EmbeddedClient) GetSSHStatus(ctx context.Context) (*pb.SSHStatus, error) {
	return c.srv.GetSSHStatus(ctx, &pb.Empty{})
}

func (c *EmbeddedClient) UpdateSSHConfig(ctx context.Context, enabled bool, port int) error {
	resp, err := c.srv.UpdateSSHConfig(ctx, &pb.SSHConfigPayload{
		Enabled: enabled,
		Port:    int32(port),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("update ssh config", resp)
	}
	return nil
}

func (c *EmbeddedClient) ClearSSHLogs(ctx context.Context) error {
	resp, err := c.srv.ClearSSHLogs(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("clear ssh logs", resp)
	}
	return nil
}

func (c *EmbeddedClient) RestartSSH(ctx context.Context) error {
	resp, err := c.srv.RestartSSH(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("restart ssh service", resp)
	}
	return nil
}

// ===== Self-Update =====

func (c *EmbeddedClient) SelfUpdate(ctx context.Context, binaryContent []byte, checksum, version string, restartAfter bool, signature []byte, force bool) (*UpdateResult, error) {
	resp, err := c.srv.SelfUpdate(ctx, &pb.UpdateRequest{
		BinaryContent: binaryContent,
		Checksum:      checksum,
		Version:       version,
		RestartAfter:  restartAfter,
		Signature:     signature,
		Force:         force,
	})
	if err != nil {
		return nil, err
	}
	return &UpdateResult{
		Success:    resp.Success,
		Message:    resp.Message,
		OldVersion: resp.OldVersion,
		NewVersion: resp.NewVersion,
	}, nil
}

func (c *EmbeddedClient) GetSelfChecksum(ctx context.Context) (*ChecksumResult, error) {
	resp, err := c.srv.GetSelfChecksum(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return &ChecksumResult{
		Checksum: resp.Checksum,
		Path:     resp.Path,
	}, nil
}

// ===== Certificate Denylist =====

func (c *EmbeddedClient) UpdateCertDenylist(ctx context.Context, serialNumbers []string, denylistHash string) error {
	resp, err := c.srv.UpdateCertDenylist(ctx, &pb.CertDenylist{
		RevokedSerialNumbers: serialNumbers,
		DenylistHash:         denylistHash,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("update cert denylist", resp)
	}
	return nil
}

// ===== Xray Version Management =====

func (c *EmbeddedClient) UpdateXrayBinary(ctx context.Context, version string, restartAfter bool, downloadURL, downloadToken, checksum string) error {
	resp, err := c.srv.UpdateXrayBinary(ctx, &pb.UpdateXrayRequest{
		Version:       version,
		RestartAfter:  restartAfter,
		DownloadUrl:   downloadURL,
		DownloadToken: downloadToken,
		Checksum:      checksum,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("update xray binary", resp)
	}
	return nil
}

// ===== Outbound Testing =====

func (c *EmbeddedClient) TestOutbound(ctx context.Context, configLink, testURL string, timeout time.Duration) (*OutboundTestResult, error) {
	timeoutSecs := int32(timeout.Seconds())
	if timeoutSecs == 0 {
		timeoutSecs = 10
	}
	resp, err := c.srv.TestOutbound(ctx, &pb.OutboundTestRequest{
		ConfigLink:     configLink,
		TestUrl:        testURL,
		TimeoutSeconds: timeoutSecs,
	})
	if err != nil {
		return nil, err
	}
	return &OutboundTestResult{
		Success:    resp.Success,
		LatencyMs:  resp.LatencyMs,
		StatusCode: resp.StatusCode,
		IP:         resp.Ip,
		Country:    resp.Country,
		Error:      resp.Error,
		Message:    resp.Message,
	}, nil
}

// ===== Online User Detection =====

func (c *EmbeddedClient) GetUserOnlineIPs(ctx context.Context, email string) (map[string]int64, error) {
	resp, err := c.srv.GetUserOnlineIPs(ctx, &pb.UserEmailRequest{Email: email})
	if err != nil {
		return nil, err
	}
	return resp.Ips, nil
}

func (c *EmbeddedClient) GetAllUsersOnlineIPs(ctx context.Context) (map[string]map[string]int64, error) {
	resp, err := c.srv.GetAllUsersOnlineIPs(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]int64, len(resp.Users))
	for email, entry := range resp.Users {
		if entry == nil || entry.Ips == nil {
			out[email] = map[string]int64{}
			continue
		}
		out[email] = entry.Ips
	}
	return out, nil
}

// ===== Buffered Traffic =====

func (c *EmbeddedClient) GetBufferedTraffic(ctx context.Context) (*BufferedTrafficStats, error) {
	resp, err := c.srv.GetBufferedTraffic(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	records := make([]*TrafficRecord, 0, len(resp.Records))
	for _, r := range resp.Records {
		records = append(records, &TrafficRecord{
			Timestamp:        r.Timestamp,
			UserUplink:       r.UserUplink,
			UserDownlink:     r.UserDownlink,
			OutboundUplink:   r.OutboundUplink,
			OutboundDownlink: r.OutboundDownlink,
			InboundUplink:    r.InboundUplink,
			InboundDownlink:  r.InboundDownlink,
			TotalUplink:      r.TotalUplink,
			TotalDownlink:    r.TotalDownlink,
		})
	}
	return &BufferedTrafficStats{
		Records:         records,
		BufferStartTime: resp.BufferStartTime,
		BufferEndTime:   resp.BufferEndTime,
	}, nil
}

func (c *EmbeddedClient) AckBufferedTraffic(ctx context.Context, throughTimestamp int64) error {
	resp, err := c.srv.AckBufferedTraffic(ctx, &pb.AckTrafficRequest{
		AckedThroughTimestamp: throughTimestamp,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("ack buffered traffic", resp)
	}
	return nil
}

// ===== Tools =====

func (c *EmbeddedClient) GenerateVLESSKeys(ctx context.Context) ([]VLESSKeyPair, error) {
	resp, err := c.srv.GenerateVLESSKeys(ctx, &pb.Empty{})
	if err != nil {
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

// ===== Bandwidth Shaping =====

func (c *EmbeddedClient) SetupBandwidth(ctx context.Context, iface string, totalBWMbps int) error {
	resp, err := c.srv.SetupBandwidth(ctx, &pb.BandwidthRequest{
		InterfaceName: iface,
		TotalBwMbps:   int32(totalBWMbps),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("setup bandwidth", resp)
	}
	return nil
}

func (c *EmbeddedClient) TeardownBandwidth(ctx context.Context) error {
	resp, err := c.srv.TeardownBandwidth(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("teardown bandwidth", resp)
	}
	return nil
}

// ===== Access Logs =====

func (c *EmbeddedClient) GetAccessLogs(ctx context.Context, email string, limit int32) ([]*pb.AccessLogEntry, error) {
	resp, err := c.srv.GetAccessLogs(ctx, &pb.AccessLogRequest{
		Email: email,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (c *EmbeddedClient) GetBufferedAccessLogSummary(ctx context.Context) (*pb.BufferedAccessLogSummary, error) {
	return c.srv.GetBufferedAccessLogSummary(ctx, &pb.Empty{})
}

func (c *EmbeddedClient) AckBufferedAccessLogSummary(ctx context.Context, upToTimestamp int64, cfg AccessLogAckConfig) error {
	graceSec := int32(0)
	if cfg.GracePeriod > 0 {
		graceSec = int32(cfg.GracePeriod / time.Second)
	}
	_, err := c.srv.AckBufferedAccessLogSummary(ctx, &pb.AckAccessLogSummaryRequest{
		UpToTimestamp:             upToTimestamp,
		GracePeriodSeconds:        graceSec,
		MaxDomainsPerHour:         cfg.MaxDomainsPerHour,
		MaxRejectedDomainsPerHour: cfg.MaxRejectedDomainsPerHour,
		MaxSourceIpsPerHour:       cfg.MaxSourceIPsPerHour,
	})
	return err
}

// ===== Starlink Monitoring =====

func (c *EmbeddedClient) GetStarlinkStatus(ctx context.Context, dishAddr string) (*StarlinkStatus, error) {
	resp, err := c.srv.GetStarlinkStatus(ctx, &pb.StarlinkRequest{DishAddress: dishAddr})
	if err != nil {
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

func (c *EmbeddedClient) GetStarlinkObstructionMap(ctx context.Context, dishAddr string) (*StarlinkObstructionMap, error) {
	resp, err := c.srv.GetStarlinkObstructionMap(ctx, &pb.StarlinkRequest{DishAddress: dishAddr})
	if err != nil {
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

// ===== Command Execution =====

func (c *EmbeddedClient) ExecuteCommand(ctx context.Context, command string, timeoutSecs int) (*pb.ExecuteCommandResponse, error) {
	return c.srv.ExecuteCommand(ctx, &pb.ExecuteCommandRequest{
		Command:        command,
		TimeoutSeconds: int32(timeoutSecs),
	})
}

// ===== Cleanup =====

func (c *EmbeddedClient) Uninstall(ctx context.Context) error {
	resp, err := c.srv.Uninstall(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errFromResp("uninstall agent", resp)
	}
	return nil
}

// ===== Nuke / Wipe =====

func (c *EmbeddedClient) Wipe(ctx context.Context, req *pb.NukeRequest) (*pb.NukeReport, error) {
	return c.srv.Wipe(ctx, req)
}

// Nuke runs the streaming handler in a goroutine; caller reads per-phase progress.
func (c *EmbeddedClient) Nuke(ctx context.Context, req *pb.NukeRequest) (pb.NodeAgent_NukeClient, error) {
	pipe := newServerStreamPipe[pb.NukeProgress](ctx)
	go func() {
		pipe.finish(c.srv.Nuke(req, pipe))
	}()
	return pipe, nil
}

// errFromResp turns a failed CommandResponse into a "failed to X: msg" error.
func errFromResp(action string, resp *pb.CommandResponse) error {
	return &commandError{action: action, message: resp.GetMessage()}
}

type commandError struct {
	action  string
	message string
}

func (e *commandError) Error() string {
	return "failed to " + e.action + ": " + e.message
}

// ===== In-process stream pipes =====
//
// Fake gRPC streams over channels: the handler runs in a goroutine and Sends,
// the caller Recvs on the other end. Unbuffered, so nothing is lost when the
// handler returns.

// serverStreamPipe is one end of a server-streaming RPC: handler Sends, caller Recvs.
type serverStreamPipe[T any] struct {
	ctx  context.Context
	ch   chan *T
	done chan struct{} // closed when the handler returns
	mu   sync.Mutex
	err  error // handler's return error, read after done
}

func newServerStreamPipe[T any](ctx context.Context) *serverStreamPipe[T] {
	return &serverStreamPipe[T]{
		ctx:  ctx,
		ch:   make(chan *T),
		done: make(chan struct{}),
	}
}

// finish records the handler's error and wakes any pending Recv.
func (p *serverStreamPipe[T]) finish(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	close(p.done)
}

func (p *serverStreamPipe[T]) Send(m *T) error {
	select {
	case p.ch <- m:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

func (p *serverStreamPipe[T]) Recv() (*T, error) {
	select {
	case m := <-p.ch:
		return m, nil
	case <-p.done:
		p.mu.Lock()
		err := p.err
		p.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
}

func (p *serverStreamPipe[T]) Context() context.Context     { return p.ctx }
func (p *serverStreamPipe[T]) SetHeader(metadata.MD) error  { return nil }
func (p *serverStreamPipe[T]) SendHeader(metadata.MD) error { return nil }
func (p *serverStreamPipe[T]) SetTrailer(metadata.MD)       {}
func (p *serverStreamPipe[T]) Header() (metadata.MD, error) { return nil, nil }
func (p *serverStreamPipe[T]) Trailer() metadata.MD         { return nil }
func (p *serverStreamPipe[T]) CloseSend() error             { return nil }
func (p *serverStreamPipe[T]) SendMsg(any) error            { return nil }
func (p *serverStreamPipe[T]) RecvMsg(any) error            { return nil }

// bidiStreamPipe is the shared core of a bidi stream: Req goes client→server,
// Res goes server→client.
type bidiStreamPipe[Req any, Res any] struct {
	ctx        context.Context
	toServer   chan *Req
	toClient   chan *Res
	sendClosed chan struct{} // closed when the client calls CloseSend
	closeOnce  sync.Once
	done       chan struct{} // closed when the handler returns
	mu         sync.Mutex
	err        error
}

func newBidiStreamPipe[Req any, Res any](ctx context.Context) *bidiStreamPipe[Req, Res] {
	return &bidiStreamPipe[Req, Res]{
		ctx:        ctx,
		toServer:   make(chan *Req),
		toClient:   make(chan *Res),
		sendClosed: make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (p *bidiStreamPipe[Req, Res]) finish(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	close(p.done)
}

// bidiServerStream is the handler end: sends Res, receives Req.
type bidiServerStream[Req any, Res any] struct{ p *bidiStreamPipe[Req, Res] }

func (s bidiServerStream[Req, Res]) Send(m *Res) error {
	select {
	case s.p.toClient <- m:
		return nil
	case <-s.p.ctx.Done():
		return s.p.ctx.Err()
	}
}

func (s bidiServerStream[Req, Res]) Recv() (*Req, error) {
	select {
	case m := <-s.p.toServer:
		return m, nil
	case <-s.p.sendClosed:
		return nil, io.EOF
	case <-s.p.ctx.Done():
		return nil, s.p.ctx.Err()
	}
}

func (s bidiServerStream[Req, Res]) Context() context.Context     { return s.p.ctx }
func (s bidiServerStream[Req, Res]) SetHeader(metadata.MD) error  { return nil }
func (s bidiServerStream[Req, Res]) SendHeader(metadata.MD) error { return nil }
func (s bidiServerStream[Req, Res]) SetTrailer(metadata.MD)       {}
func (s bidiServerStream[Req, Res]) SendMsg(any) error            { return nil }
func (s bidiServerStream[Req, Res]) RecvMsg(any) error            { return nil }

// bidiClientStream is the caller end: sends Req, receives Res.
type bidiClientStream[Req any, Res any] struct{ p *bidiStreamPipe[Req, Res] }

func (c bidiClientStream[Req, Res]) Send(m *Req) error {
	select {
	case c.p.toServer <- m:
		return nil
	case <-c.p.done:
		return io.EOF
	case <-c.p.ctx.Done():
		return c.p.ctx.Err()
	}
}

func (c bidiClientStream[Req, Res]) Recv() (*Res, error) {
	select {
	case m := <-c.p.toClient:
		return m, nil
	case <-c.p.done:
		c.p.mu.Lock()
		err := c.p.err
		c.p.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	case <-c.p.ctx.Done():
		return nil, c.p.ctx.Err()
	}
}

func (c bidiClientStream[Req, Res]) CloseSend() error {
	c.p.closeOnce.Do(func() { close(c.p.sendClosed) })
	return nil
}

func (c bidiClientStream[Req, Res]) Context() context.Context     { return c.p.ctx }
func (c bidiClientStream[Req, Res]) Header() (metadata.MD, error) { return nil, nil }
func (c bidiClientStream[Req, Res]) Trailer() metadata.MD         { return nil }
func (c bidiClientStream[Req, Res]) SendMsg(any) error            { return nil }
func (c bidiClientStream[Req, Res]) RecvMsg(any) error            { return nil }
