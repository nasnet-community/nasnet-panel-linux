package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	utlstransport "github.com/nasnet-community/nasnet-panel-linux/pkg/transport/utls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

// Client provides a high-level API for communicating with a Node Agent
type Client struct {
	conn   *grpc.ClientConn
	client pb.NodeAgentClient
	target string
}

// TLSConfig holds mTLS credentials for connecting to an agent
type TLSConfig struct {
	CACert      string // CA certificate PEM content
	ClientCert  string // Client certificate PEM content
	ClientKey   string // Client private key PEM content
	UseUTLS     bool   // Enable uTLS fingerprinting to bypass DPI
	Fingerprint string // TLS fingerprint: "chrome", "firefox", "safari", "edge", "ios", "android", "random"
	SNIOverride string // Camouflage SNI domain (e.g. "www.google.com"); empty = use actual server address
	// VerifyLeaf, when set, sees the agent's leaf certificate after chain
	// verification and may refuse it.
	VerifyLeaf func(leaf *x509.Certificate) error
	// ServerName is the name the agent's certificate must carry; empty means
	// the address dialled. An address move dials the new address as the old name.
	ServerName string
}

// NewClient creates a new agent client with mandatory mTLS. extra dial
// options go last (router mode passes a marked dialer).
func NewClient(target string, tlsCfg *TLSConfig, timeout time.Duration, extra ...grpc.DialOption) (*Client, error) {
	if tlsCfg == nil || tlsCfg.CACert == "" {
		return nil, fmt.Errorf("TLS configuration is required: refusing to connect without mTLS credentials")
	}

	if timeout == 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var opts []grpc.DialOption

	creds, err := loadTLSCredentials(tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
	}
	opts = append(opts, grpc.WithTransportCredentials(creds))

	opts = append(opts, grpc.WithBlock())

	// Increase max message size to 100MB
	opts = append(opts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(100*1024*1024),
		grpc.MaxCallSendMsgSize(100*1024*1024),
	))

	// Apply per-method deadline fallback so a slow agent cannot hang any
	// caller that forgot to set its own context timeout.
	opts = append(opts, grpc.WithUnaryInterceptor(DeadlineUnaryInterceptor()))
	opts = append(opts, extra...)

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent at %s: %w", target, err)
	}

	return &Client{
		conn:   conn,
		client: pb.NewNodeAgentClient(conn),
		target: target,
	}, nil
}

// loadTLSCredentials loads mTLS credentials from PEM content
// If UseUTLS is enabled, returns uTLS credentials with fingerprint mimicry
func loadTLSCredentials(cfg *TLSConfig) (credentials.TransportCredentials, error) {
	// Parse CA certificate from PEM content
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM([]byte(cfg.CACert)) {
		return nil, fmt.Errorf("failed to parse CA cert PEM")
	}

	// Load client certificate and key from PEM content
	clientCert, err := tls.X509KeyPair([]byte(cfg.ClientCert), []byte(cfg.ClientKey))
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert from PEM: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
		ServerName:   cfg.ServerName,
	}
	if cfg.VerifyLeaf != nil {
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
			if len(chains) > 0 && len(chains[0]) > 0 {
				return cfg.VerifyLeaf(chains[0][0])
			}
			if len(rawCerts) == 0 {
				return fmt.Errorf("agent sent no certificate")
			}
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("parse agent certificate: %w", err)
			}
			return cfg.VerifyLeaf(leaf)
		}
	}

	// Use uTLS if enabled for fingerprint camouflage
	if cfg.UseUTLS {
		fingerprint := utlstransport.Fingerprint(cfg.Fingerprint)
		if cfg.Fingerprint == "" {
			fingerprint = utlstransport.FingerprintChrome // Default to Chrome
		}

		var opts []utlstransport.CredentialOption

		// SNI camouflage: override ServerName in ClientHello with a benign domain.
		// Because the cert won't match the camouflage domain, we skip standard
		// hostname verification and instead validate the cert chain against our
		// private CA manually.
		if cfg.SNIOverride != "" {
			opts = append(opts, utlstransport.WithSNIOverride(cfg.SNIOverride))
			tlsConfig.InsecureSkipVerify = true
			caCertPool := certPool // capture for closure
			tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				certs := make([]*x509.Certificate, len(rawCerts))
				for i, raw := range rawCerts {
					c, err := x509.ParseCertificate(raw)
					if err != nil {
						return fmt.Errorf("failed to parse server certificate: %w", err)
					}
					certs[i] = c
				}
				verifyOpts := x509.VerifyOptions{
					Roots:         caCertPool,
					Intermediates: x509.NewCertPool(),
				}
				for _, c := range certs[1:] {
					verifyOpts.Intermediates.AddCert(c)
				}
				if _, err := certs[0].Verify(verifyOpts); err != nil {
					return fmt.Errorf("server certificate not signed by trusted CA: %w", err)
				}
				if cfg.VerifyLeaf != nil {
					return cfg.VerifyLeaf(certs[0])
				}
				return nil
			}
		}

		// Session ticket cache for TLS session resumption (avoids full handshake every time)
		opts = append(opts, utlstransport.WithSessionCache(utlstransport.NewLRUSessionCache(32)))

		return utlstransport.NewCredentials(tlsConfig, fingerprint, opts...), nil
	}

	return credentials.NewTLS(tlsConfig), nil
}

// Close closes the connection to the agent
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ConnBroken reports whether gRPC has already observed the connection fail.
// The ClientConn object stays valid after the peer dies — it just parks in
// TransientFailure while it retries — so a cached client keeps looking usable
// for as long as something caches it. Only a state gRPC itself calls failed
// counts: Idle is a healthy conn between calls, and an unknown state (no conn
// yet) is no evidence of breakage. Callers must still treat a real RPC as the
// authority on whether the agent is reachable.
func (c *Client) ConnBroken() bool {
	if c == nil || c.conn == nil {
		return false
	}
	switch c.conn.GetState() {
	case connectivity.TransientFailure, connectivity.Shutdown:
		return true
	default:
		return false
	}
}

// Target returns the agent target address
func (c *Client) Target() string {
	return c.target
}

// ===== Lifecycle Methods =====

// GetStatus returns the current status of the node
func (c *Client) GetStatus(ctx context.Context) (*pb.NodeStatus, error) {
	return c.client.GetStatus(ctx, &pb.Empty{})
}

// StartXray starts the xray process on the remote node
func (c *Client) StartXray(ctx context.Context) error {
	resp, err := c.client.StartXray(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to start xray: %s", resp.Message)
	}
	return nil
}

// StopXray stops the xray process on the remote node
func (c *Client) StopXray(ctx context.Context, gracefulTimeout time.Duration) error {
	resp, err := c.client.StopXray(ctx, &pb.StopRequest{
		GracefulTimeoutSeconds: int32(gracefulTimeout.Seconds()),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to stop xray: %s", resp.Message)
	}
	return nil
}

// RestartXray restarts the xray process on the remote node
func (c *Client) RestartXray(ctx context.Context, validateConfig bool) error {
	resp, err := c.client.RestartXray(ctx, &pb.RestartRequest{
		ValidateConfig: validateConfig,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to restart xray: %s", resp.Message)
	}
	return nil
}

// ===== File Management Methods =====

// WriteFile writes a file to the remote node and returns the absolute path where it was written
func (c *Client) WriteFile(ctx context.Context, path string, content []byte, perm uint32) (string, error) {
	resp, err := c.client.WriteFile(ctx, &pb.FilePayload{
		Path:    path,
		Content: content,
		Perm:    int32(perm),
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("failed to write file: %s", resp.Message)
	}
	// On success, Message contains the absolute path
	return resp.Message, nil
}

// ===== Configuration Methods =====

// PushConfig sends a new configuration to the agent
func (c *Client) PushConfig(ctx context.Context, configJSON string) error {
	resp, err := c.client.PushConfig(ctx, &pb.ConfigPayload{
		JsonContent: configJSON,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to push config: %s", resp.Message)
	}
	return nil
}

// ApplyCoreState reconciles one non-Xray protocol core on the node.
func (c *Client) ApplyCoreState(ctx context.Context, payload *pb.CoreStatePayload) error {
	resp, err := c.client.ApplyCoreState(ctx, payload)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("apply core state (%s): %s", payload.GetCoreType(), resp.Message)
	}
	return nil
}

// GetCoreStatus reports the runtime state of one non-Xray protocol core.
func (c *Client) GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error) {
	return c.client.GetCoreStatus(ctx, &pb.CoreStatusRequest{CoreType: coreType})
}

// ProbeDependencies reports which declared dependencies are present per core.
func (c *Client) ProbeDependencies(ctx context.Context, coreTypes []string) (*pb.ProbeDependenciesResponse, error) {
	return c.client.ProbeDependencies(ctx, &pb.ProbeDependenciesRequest{CoreTypes: coreTypes})
}

// StartDependencyInstall launches an async install job on the node.
func (c *Client) StartDependencyInstall(ctx context.Context, req *pb.StartInstallRequest) (*pb.StartInstallResponse, error) {
	return c.client.StartDependencyInstall(ctx, req)
}

// GetInstallProgress polls an install job's status/steps/log lines.
func (c *Client) GetInstallProgress(ctx context.Context, jobID string, sinceCursor int32) (*pb.InstallProgressResponse, error) {
	return c.client.GetInstallProgress(ctx, &pb.InstallProgressRequest{JobId: jobID, SinceCursor: sinceCursor})
}

// UpdateXrayAPIConfig updates the Xray API address in the agent config
func (c *Client) UpdateXrayAPIConfig(ctx context.Context, apiAddr string) error {
	resp, err := c.client.UpdateXrayAPIConfig(ctx, &pb.XrayAPIConfigPayload{
		ApiAddr: apiAddr,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to update xray api config: %s", resp.Message)
	}
	return nil
}

// GetCurrentConfig retrieves the current configuration from the agent
func (c *Client) GetCurrentConfig(ctx context.Context) (string, string, error) {
	resp, err := c.client.GetCurrentConfig(ctx, &pb.Empty{})
	if err != nil {
		return "", "", err
	}
	return resp.JsonContent, resp.Checksum, nil
}

// ValidateConfig validates a configuration on the agent without applying it
func (c *Client) ValidateConfig(ctx context.Context, configJSON string) (bool, []string, []string, error) {
	resp, err := c.client.ValidateConfig(ctx, &pb.ConfigPayload{
		JsonContent: configJSON,
	})
	if err != nil {
		return false, nil, nil, err
	}
	return resp.Valid, resp.Errors, resp.Warnings, nil
}

// PushConfigAndRestart pushes config and restarts xray in one operation.
// PushConfig on the agent already validates and restarts Xray, so no
// separate RestartXray call is needed (a double restart can cause
// "port in use" errors when the OS hasn't released sockets yet).
func (c *Client) PushConfigAndRestart(ctx context.Context, configJSON string, _ bool) error {
	if err := c.PushConfig(ctx, configJSON); err != nil {
		return fmt.Errorf("failed to push config: %w", err)
	}
	return nil
}

// ===== User Management Methods =====

// AddUser adds a user to an inbound on the remote node
func (c *Client) AddUser(ctx context.Context, inboundTag, email, uuid, protocol, flow, encryption string, level int32) error {
	resp, err := c.client.AddUser(ctx, &pb.UserPayload{
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
		return fmt.Errorf("failed to add user: %s", resp.Message)
	}
	return nil
}

// RemoveUser removes a user from an inbound on the remote node
func (c *Client) RemoveUser(ctx context.Context, inboundTag, email string) error {
	resp, err := c.client.RemoveUser(ctx, &pb.UserPayload{
		InboundTag: inboundTag,
		Email:      email,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to remove user: %s", resp.Message)
	}
	return nil
}

// ListUsers returns all users on a specific inbound
func (c *Client) ListUsers(ctx context.Context, inboundTag string) ([]*pb.UserInfo, error) {
	resp, err := c.client.ListUsers(ctx, &pb.InboundSelector{
		InboundTag: inboundTag,
	})
	if err != nil {
		return nil, err
	}
	return resp.Users, nil
}

// ===== Statistics Methods =====

// GetSystemStats retrieves system statistics from the remote node
func (c *Client) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	resp, err := c.client.GetSystemStats(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return systemStatsFromPB(resp), nil
}

// GetXrayStats retrieves Xray traffic statistics from the remote node
func (c *Client) GetXrayStats(ctx context.Context, reset bool) (*XrayStats, error) {
	resp, err := c.client.GetXrayStats(ctx, &pb.StatsRequest{
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

// StreamLogs returns a stream of logs from the agent
func (c *Client) StreamLogs(ctx context.Context, tail int, follow bool) (pb.NodeAgent_StreamLogsClient, error) {
	stream, err := c.client.StreamLogs(ctx, &pb.LogRequest{
		TailLines: int32(tail),
		Follow:    follow,
		LogType:   pb.LogType_LOG_TYPE_ALL,
	})
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// OpenTerminal opens a bidirectional PTY terminal stream
func (c *Client) OpenTerminal(ctx context.Context) (pb.NodeAgent_OpenTerminalClient, error) {
	return c.client.OpenTerminal(ctx)
}

// GetHostInfo retrieves static host information from the remote node
func (c *Client) GetHostInfo(ctx context.Context) (*HostInfo, error) {
	resp, err := c.client.GetHostInfo(ctx, &pb.Empty{})
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

// ===== Health Methods =====

// HealthCheck performs a health check on the remote agent
func (c *Client) HealthCheck(ctx context.Context) (*HealthResult, error) {
	resp, err := c.client.HealthCheck(ctx, &pb.Empty{})
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

// GetVersion retrieves version information from the agent
func (c *Client) GetVersion(ctx context.Context) (*VersionInfo, error) {
	resp, err := c.client.GetVersion(ctx, &pb.Empty{})
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

// Ping checks if the agent is reachable
func (c *Client) Ping(ctx context.Context) (int64, error) {
	start := time.Now()
	_, err := c.client.GetStatus(ctx, &pb.Empty{})
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// Heartbeat opens the bidirectional ping/pong stream.
func (c *Client) Heartbeat(ctx context.Context) (pb.NodeAgent_HeartbeatClient, error) {
	return c.client.Heartbeat(ctx)
}

// ===== SSH Methods =====

// GetSSHStatus returns the current status of the SSH service
func (c *Client) GetSSHStatus(ctx context.Context) (*pb.SSHStatus, error) {
	return c.client.GetSSHStatus(ctx, &pb.Empty{})
}

// UpdateSSHConfig updates the SSH configuration (port, enabled)
func (c *Client) UpdateSSHConfig(ctx context.Context, enabled bool, port int) error {
	resp, err := c.client.UpdateSSHConfig(ctx, &pb.SSHConfigPayload{
		Enabled: enabled,
		Port:    int32(port),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to update ssh config: %s", resp.Message)
	}
	return nil
}

// ClearSSHLogs clears the SSH auth logs
func (c *Client) ClearSSHLogs(ctx context.Context) error {
	resp, err := c.client.ClearSSHLogs(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to clear ssh logs: %s", resp.Message)
	}
	return nil
}

// RestartSSH restarts the SSH service
func (c *Client) RestartSSH(ctx context.Context) error {
	resp, err := c.client.RestartSSH(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to restart ssh service: %s", resp.Message)
	}
	return nil
}

// ===== Self-Update Methods =====

// SelfUpdate sends a new binary to the agent for self-update
func (c *Client) SelfUpdate(ctx context.Context, binaryContent []byte, checksum, version string, restartAfter bool, signature []byte, force bool) (*UpdateResult, error) {
	resp, err := c.client.SelfUpdate(ctx, &pb.UpdateRequest{
		BinaryContent: binaryContent,
		Checksum:      checksum,
		Version:       version,
		RestartAfter:  restartAfter,
		Signature:     signature,
		Force:         force,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call SelfUpdate: %w", err)
	}

	return &UpdateResult{
		Success:    resp.Success,
		Message:    resp.Message,
		OldVersion: resp.OldVersion,
		NewVersion: resp.NewVersion,
	}, nil
}

// GetSelfChecksum retrieves the checksum of the running binary from the agent
func (c *Client) GetSelfChecksum(ctx context.Context) (*ChecksumResult, error) {
	resp, err := c.client.GetSelfChecksum(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return &ChecksumResult{
		Checksum: resp.Checksum,
		Path:     resp.Path,
	}, nil
}

// ===== Certificate Denylist Methods =====

// UpdateCertDenylist pushes a list of revoked certificate serial numbers to the agent
func (c *Client) UpdateCertDenylist(ctx context.Context, serialNumbers []string, denylistHash string) error {
	resp, err := c.client.UpdateCertDenylist(ctx, &pb.CertDenylist{
		RevokedSerialNumbers: serialNumbers,
		DenylistHash:         denylistHash,
	})
	if err != nil {
		return fmt.Errorf("failed to push cert denylist: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("cert denylist update failed: %s", resp.Message)
	}
	return nil
}

// ===== Xray Version Management Methods =====

// UpdateXrayBinary tells the agent to download and install a specific xray-core version
func (c *Client) UpdateXrayBinary(ctx context.Context, version string, restartAfter bool, downloadURL, downloadToken, checksum string) error {
	resp, err := c.client.UpdateXrayBinary(ctx, &pb.UpdateXrayRequest{
		Version:       version,
		RestartAfter:  restartAfter,
		DownloadUrl:   downloadURL,
		DownloadToken: downloadToken,
		Checksum:      checksum,
	})
	if err != nil {
		return fmt.Errorf("failed to update xray binary: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("xray update failed: %s", resp.Message)
	}
	return nil
}

// ===== Outbound Testing Methods =====

// TestOutbound tests outbound connectivity via the agent using xray-knife
func (c *Client) TestOutbound(ctx context.Context, spec *OutboundTestSpec) (*OutboundTestResult, error) {
	resp, err := c.client.TestOutbound(ctx, outboundTestRequest(spec))
	if err != nil {
		return nil, err
	}
	return outboundTestResult(resp), nil
}

// ===== Online User Detection Methods =====

// GetUserOnlineIPs returns the IPs currently connected for a specific user
func (c *Client) GetUserOnlineIPs(ctx context.Context, email string) (map[string]int64, error) {
	resp, err := c.client.GetUserOnlineIPs(ctx, &pb.UserEmailRequest{Email: email})
	if err != nil {
		return nil, err
	}
	return resp.Ips, nil
}

// GetAllUsersOnlineIPs returns email → IP → first-seen unix timestamp
// in one round-trip. Both maps non-nil even when empty.
func (c *Client) GetAllUsersOnlineIPs(ctx context.Context) (map[string]map[string]int64, error) {
	snapshot, err := c.GetOnlineIPSnapshot(ctx)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return snapshot.Users, nil
}

// GetOnlineIPSnapshot is optional on NodeClient so older clients and fakes
// can keep their map-only method while current transports preserve freshness.
func (c *Client) GetOnlineIPSnapshot(ctx context.Context) (*OnlineIPSnapshot, error) {
	resp, err := c.client.GetAllUsersOnlineIPs(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return &OnlineIPSnapshot{Users: onlineIPsFromPB(resp), CollectedAtUnixMs: resp.GetCollectedAtUnixMs()}, nil
}

// ===== Buffered Traffic Methods =====

// GetBufferedTraffic retrieves time-bucketed traffic records from the agent's local buffer
func (c *Client) GetBufferedTraffic(ctx context.Context) (*BufferedTrafficStats, error) {
	resp, err := c.client.GetBufferedTraffic(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return bufferedTrafficFromPB(resp), nil
}

// GetStatsBundle fetches system stats, node status, buffered traffic and
// (optionally) online IPs in one RPC. Errors (including Unimplemented from
// pre-bundle agents) surface to the caller, which falls back to the
// individual RPCs.
func (c *Client) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*StatsBundle, error) {
	resp, err := c.client.GetStatsBundle(ctx, &pb.StatsBundleRequest{IncludeOnlineIps: includeOnlineIPs})
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

// AckBufferedTraffic acknowledges receipt of traffic records up to a given timestamp
func (c *Client) AckBufferedTraffic(ctx context.Context, throughTimestamp int64) error {
	resp, err := c.client.AckBufferedTraffic(ctx, &pb.AckTrafficRequest{
		AckedThroughTimestamp: throughTimestamp,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("ack failed: %s", resp.Message)
	}
	return nil
}

// ===== Tools =====

// GenerateVLESSKeys asks the agent to run `xray vlessenc` and return the generated keys.
func (c *Client) GenerateVLESSKeys(ctx context.Context) ([]VLESSKeyPair, error) {
	resp, err := c.client.GenerateVLESSKeys(ctx, &pb.Empty{})
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

// ===== Bandwidth Shaping Methods =====

// SetupBandwidth configures TC bandwidth shaping on the agent node
func (c *Client) SetupBandwidth(ctx context.Context, iface string, totalBWMbps int) error {
	resp, err := c.client.SetupBandwidth(ctx, &pb.BandwidthRequest{
		InterfaceName: iface,
		TotalBwMbps:   int32(totalBWMbps),
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to setup bandwidth: %s", resp.Message)
	}
	return nil
}

// TeardownBandwidth removes TC bandwidth shaping from the agent node
func (c *Client) TeardownBandwidth(ctx context.Context) error {
	resp, err := c.client.TeardownBandwidth(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to teardown bandwidth: %s", resp.Message)
	}
	return nil
}

// ===== Access Log Methods =====

// GetAccessLogs retrieves parsed access log entries from the agent, optionally filtered by email.
func (c *Client) GetAccessLogs(ctx context.Context, email string, limit int32) ([]*pb.AccessLogEntry, error) {
	resp, err := c.client.GetAccessLogs(ctx, &pb.AccessLogRequest{
		Email: email,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// GetBufferedAccessLogSummary retrieves hourly aggregated access log summaries.
func (c *Client) GetBufferedAccessLogSummary(ctx context.Context) (*pb.BufferedAccessLogSummary, error) {
	return c.client.GetBufferedAccessLogSummary(ctx, &pb.Empty{})
}

// AckBufferedAccessLogSummary acknowledges processing of summaries up
// to a timestamp and pushes the current hub-side runtime config to the
// agent (grace window + per-row top-N caps). Zero fields on cfg mean
// "agent keeps its default for that field".
func (c *Client) AckBufferedAccessLogSummary(ctx context.Context, upToTimestamp int64, cfg AccessLogAckConfig) error {
	graceSec := int32(0)
	if cfg.GracePeriod > 0 {
		graceSec = int32(cfg.GracePeriod / time.Second)
	}
	_, err := c.client.AckBufferedAccessLogSummary(ctx, &pb.AckAccessLogSummaryRequest{
		UpToTimestamp:             upToTimestamp,
		GracePeriodSeconds:        graceSec,
		MaxDomainsPerHour:         cfg.MaxDomainsPerHour,
		MaxRejectedDomainsPerHour: cfg.MaxRejectedDomainsPerHour,
		MaxSourceIpsPerHour:       cfg.MaxSourceIPsPerHour,
	})
	return err
}

// ===== Starlink Monitoring Methods =====

// GetStarlinkStatus retrieves Starlink dish status via the agent.
func (c *Client) GetStarlinkStatus(ctx context.Context, dishAddr string) (*StarlinkStatus, error) {
	resp, err := c.client.GetStarlinkStatus(ctx, &pb.StarlinkRequest{DishAddress: dishAddr})
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
		AttitudeUncertaintyDeg:           float64(resp.AttitudeUncertaintyDeg),
		AttitudeEstimationState:          resp.AttitudeEstimationState,
		DesiredBoresightAzimuthDeg:       float64(resp.DesiredBoresightAzimuthDeg),
		DesiredBoresightElevationDeg:     float64(resp.DesiredBoresightElevationDeg),
		ActuatorState:                    resp.ActuatorState,
		HasActuators:                     resp.HasActuators,
	}, nil
}

// GetStarlinkObstructionMap retrieves the Starlink dish obstruction map via the agent.
func (c *Client) GetStarlinkObstructionMap(ctx context.Context, dishAddr string) (*StarlinkObstructionMap, error) {
	resp, err := c.client.GetStarlinkObstructionMap(ctx, &pb.StarlinkRequest{DishAddress: dishAddr})
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

// ===== Command Execution Methods =====

// ExecuteCommand runs a shell command on the remote node and returns the result.
func (c *Client) ExecuteCommand(ctx context.Context, command string, timeoutSecs int) (*pb.ExecuteCommandResponse, error) {
	return c.client.ExecuteCommand(ctx, &pb.ExecuteCommandRequest{
		Command:        command,
		TimeoutSeconds: int32(timeoutSecs),
	})
}

// ===== Moving the agent =====

func (c *Client) MoveListenPort(ctx context.Context, req *pb.MoveListenPortRequest) (*pb.CommandResponse, error) {
	return c.client.MoveListenPort(ctx, req)
}

func (c *Client) SwitchConnectMode(ctx context.Context, req *pb.SwitchConnectModeRequest) (*pb.CommandResponse, error) {
	return c.client.SwitchConnectMode(ctx, req)
}

// ===== Cleanup Methods =====

// Uninstall tells the agent to remove itself and Xray. Irreversible.
func (c *Client) Uninstall(ctx context.Context) error {
	resp, err := c.client.Uninstall(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to uninstall agent: %s", resp.Message)
	}
	return nil
}

// Wipe runs the Wipe/Nuke phase list on the agent and returns the final report.
// Use this variant for WIPE mode, and as a fallback for NUKE mode on
// reverse-connected nodes (which don't yet support server-streaming RPCs).
func (c *Client) Wipe(ctx context.Context, req *pb.NukeRequest) (*pb.NukeReport, error) {
	return c.client.Wipe(ctx, req)
}

// Nuke is the server-streaming variant: the caller receives a NukePhaseResult
// for each phase as it executes, then a terminal NukeReport frame, then the
// stream closes as the agent self-destructs.
func (c *Client) Nuke(ctx context.Context, req *pb.NukeRequest) (pb.NodeAgent_NukeClient, error) {
	return c.client.Nuke(ctx, req)
}
