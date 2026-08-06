package agent

import (
	"context"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// NodeClient defines the full set of operations available on a node agent.
// Both direct (*Client) and reverse-tunnel clients implement this interface,
// allowing the rest of the codebase to be agnostic about connection mode.
type NodeClient interface {
	Close() error
	Target() string

	// Lifecycle
	GetStatus(ctx context.Context) (*pb.NodeStatus, error)
	StartXray(ctx context.Context) error
	StopXray(ctx context.Context, gracefulTimeout time.Duration) error
	RestartXray(ctx context.Context, validateConfig bool) error

	// File management
	WriteFile(ctx context.Context, path string, content []byte, perm uint32) (string, error)

	// Configuration
	PushConfig(ctx context.Context, configJSON string) error
	UpdateXrayAPIConfig(ctx context.Context, apiAddr string) error
	GetCurrentConfig(ctx context.Context) (string, string, error)
	ValidateConfig(ctx context.Context, configJSON string) (bool, []string, []string, error)
	PushConfigAndRestart(ctx context.Context, configJSON string, _ bool) error

	// User management
	AddUser(ctx context.Context, inboundTag, email, uuid, protocol, flow, encryption string, level int32) error
	RemoveUser(ctx context.Context, inboundTag, email string) error
	ListUsers(ctx context.Context, inboundTag string) ([]*pb.UserInfo, error)

	// Statistics
	GetSystemStats(ctx context.Context) (*SystemStats, error)
	GetXrayStats(ctx context.Context, reset bool) (*XrayStats, error)

	// Streaming
	StreamLogs(ctx context.Context, tail int, follow bool) (pb.NodeAgent_StreamLogsClient, error)
	OpenTerminal(ctx context.Context) (pb.NodeAgent_OpenTerminalClient, error)

	// Host info
	GetHostInfo(ctx context.Context) (*HostInfo, error)

	// ListInterfaces enumerates the box's NICs (Required by Router mode)
	ListInterfaces(ctx context.Context) ([]NetInterface, error)

	// Health
	HealthCheck(ctx context.Context) (*HealthResult, error)

	// Version
	GetVersion(ctx context.Context) (*VersionInfo, error)

	// Connectivity
	Ping(ctx context.Context) (int64, error)
	Heartbeat(ctx context.Context) (pb.NodeAgent_HeartbeatClient, error)

	// SSH
	GetSSHStatus(ctx context.Context) (*pb.SSHStatus, error)
	UpdateSSHConfig(ctx context.Context, enabled bool, port int) error
	ClearSSHLogs(ctx context.Context) error
	RestartSSH(ctx context.Context) error

	// Self-update
	SelfUpdate(ctx context.Context, binaryContent []byte, checksum, version string, restartAfter bool, signature []byte, force bool) (*UpdateResult, error)
	GetSelfChecksum(ctx context.Context) (*ChecksumResult, error)

	// Certificate denylist
	UpdateCertDenylist(ctx context.Context, serialNumbers []string, denylistHash string) error

	// Xray version management
	UpdateXrayBinary(ctx context.Context, version string, restartAfter bool, downloadURL, downloadToken, checksum string) error

	// Outbound testing
	TestOutbound(ctx context.Context, configLink, testURL string, timeout time.Duration) (*OutboundTestResult, error)

	// Online user detection
	GetUserOnlineIPs(ctx context.Context, email string) (map[string]int64, error)
	GetAllUsersOnlineIPs(ctx context.Context) (map[string]map[string]int64, error)

	// Buffered traffic
	GetBufferedTraffic(ctx context.Context) (*BufferedTrafficStats, error)
	AckBufferedTraffic(ctx context.Context, throughTimestamp int64) error

	// Tools
	GenerateVLESSKeys(ctx context.Context) ([]VLESSKeyPair, error)

	// Bandwidth shaping
	SetupBandwidth(ctx context.Context, iface string, totalBWMbps int) error
	TeardownBandwidth(ctx context.Context) error

	// Access logs
	GetAccessLogs(ctx context.Context, email string, limit int32) ([]*pb.AccessLogEntry, error)
	GetBufferedAccessLogSummary(ctx context.Context) (*pb.BufferedAccessLogSummary, error)
	AckBufferedAccessLogSummary(ctx context.Context, upToTimestamp int64, cfg AccessLogAckConfig) error

	// Starlink monitoring
	GetStarlinkStatus(ctx context.Context, dishAddr string) (*StarlinkStatus, error)
	GetStarlinkObstructionMap(ctx context.Context, dishAddr string) (*StarlinkObstructionMap, error)

	// Command execution
	ExecuteCommand(ctx context.Context, command string, timeoutSecs int) (*pb.ExecuteCommandResponse, error)

	// Cleanup
	Uninstall(ctx context.Context) error

	// Nuke / Wipe
	Wipe(ctx context.Context, req *pb.NukeRequest) (*pb.NukeReport, error)
	Nuke(ctx context.Context, req *pb.NukeRequest) (pb.NodeAgent_NukeClient, error)
}

// AccessLogAckConfig carries hub-side runtime settings the agent's
// aggregator should adopt on the next sweep. Zero values mean "use
// agent default" — keeps the wire format backward-compatible.
type AccessLogAckConfig struct {
	GracePeriod               time.Duration
	MaxDomainsPerHour         int32
	MaxRejectedDomainsPerHour int32
	MaxSourceIPsPerHour       int32
}
