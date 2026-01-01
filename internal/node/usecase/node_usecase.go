package usecase

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	sniUC "github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

var (
	ErrNodeNotFound      = errors.New("node not found")
	ErrInboundNotFound   = errors.New("inbound not found")
	ErrOutboundNotFound  = errors.New("outbound not found")
	ErrNodeHasChildren   = errors.New("node has active inbounds or accounts")
	ErrInvalidTargetNode = errors.New("invalid target node for migration")
)

// SyncResult holds the result of a sync operation
type SyncResult struct {
	Restored int // Inbounds pushed to Xray (Recovery)
	Imported int // New inbounds imported from Xray to DB
	Kept     int // Inbounds already matching
	Errors   int
}

// InboundMigrationResult holds the result of an inbound migration operation
type InboundMigrationResult struct {
	MigratedAccounts  int    `json:"migrated_accounts"`
	SkippedAccounts   int    `json:"skipped_accounts"`
	FailedAccounts    int    `json:"failed_accounts"`
	SourceDeactivated bool   `json:"source_deactivated"`
	ProtocolWarning   string `json:"protocol_warning,omitempty"`
}

// NodeStats holds aggregated statistics for a node
type NodeStats struct {
	TotalUplink   int64 `json:"total_uplink"`   // Total upload bytes
	TotalDownlink int64 `json:"total_downlink"` // Total download bytes
	OnlineUsers   int   `json:"online_users"`   // Number of users with active traffic
	XrayRunning   bool  `json:"-"`              // Internal helper
	// System metrics (from agent)
	CPUPercent    float64 `json:"cpu_percent"`     // CPU usage percentage
	MemoryPercent float64 `json:"memory_percent"`  // Memory usage percentage
	MemoryUsedMB  uint64  `json:"memory_used_mb"`  // Memory used in MB
	MemoryTotalMB uint64  `json:"memory_total_mb"` // Total memory in MB
	DiskPercent   float64 `json:"disk_percent"`    // Disk usage percentage
	DiskUsedGB    uint64  `json:"disk_used_gb"`    // Disk used in GB
	DiskTotalGB   uint64  `json:"disk_total_gb"`   // Total disk in GB
	// Network rates (from agent)
	UpSpeed   uint64 `json:"up_speed"`   // Upload speed in bytes/sec
	DownSpeed uint64 `json:"down_speed"` // Download speed in bytes/sec
	// Network details
	TcpCount uint64 `json:"tcp_count"`
	UdpCount uint64 `json:"udp_count"`
	FdCount  uint64 `json:"fd_count"`
	// Xray Process Stats
	XrayStatus    string `json:"xray_status"`    // "running", "stopped"
	XrayPID       int64  `json:"xray_pid"`       // Process ID
	ProcessUptime int64  `json:"process_uptime"` // Xray process uptime in seconds
	SystemUptime  int64  `json:"system_uptime"`  // Host system uptime in seconds
	// Load averages
	LoadAvg1  float64 `json:"load_avg_1"`
	LoadAvg5  float64 `json:"load_avg_5"`
	LoadAvg15 float64 `json:"load_avg_15"`
	// Version info
	AgentVersion string `json:"agent_version,omitempty"`
	XrayVersion  string `json:"xray_version,omitempty"`
	// Account counts
	TotalAccounts  int64 `json:"total_accounts"`
	ActiveAccounts int64 `json:"active_accounts"`
}

// InboundStats holds traffic statistics for a specific inbound
type InboundStats struct {
	TotalUplink   int64 // Total upload bytes for this inbound
	TotalDownlink int64 // Total download bytes for this inbound
	ActiveUsers   int   // Number of users on this inbound
}

// UpdateProgress represents the status of an agent update operation
type UpdateProgress struct {
	Step    string `json:"step"`    // init, connect, check_arch, prep_binary, upload, install, verify
	Message string `json:"message"` // User-friendly message
	Status  string `json:"status"`  // pending, running, success, error
	Error   string `json:"error,omitempty"`
}

type NodeUsecase interface {
	// Node Management
	CreateNode(ctx context.Context, name, ip, country, datacenter string, apiPort, agentPort int, connectMode string, isStealth, isPersistentStealth bool) (*domain.Node, error)
	ListNodes(ctx context.Context) ([]*domain.Node, error)
	GetNode(ctx context.Context, id uint) (*domain.Node, error)
	DeleteNode(ctx context.Context, id uint, force bool) error
	RepushForSNI(ctx context.Context, sniID uint)
	MigrateNodeAccounts(ctx context.Context, sourceNodeID, targetNodeID, targetInboundID uint) error
	UpdateNode(ctx context.Context, node *domain.Node) error
	UpdateNodeDNSSettings(ctx context.Context, nodeID uint, settings *domain.DNSSettings) error
	ClearNodeDNSSettings(ctx context.Context, nodeID uint) error
	UpdateNodeFakeDNSSettings(ctx context.Context, nodeID uint, pools []domain.FakeDNSPool) error
	CheckNodeHealth(ctx context.Context, id uint) (*domain.NodeHealth, error)

	// Inbound Management
	AddInbound(ctx context.Context, inbound *domain.Inbound) error

	ListInbounds(ctx context.Context, nodeID uint) ([]*domain.Inbound, error)
	ToggleInboundDisabled(ctx context.Context, id uint) (*domain.Inbound, error)
	DeleteInbound(ctx context.Context, id uint) error
	GetInbound(ctx context.Context, id uint) (*domain.Inbound, error)
	UpdateInbound(ctx context.Context, inbound *domain.Inbound) error

	// Inbound Discovery & Sync
	DiscoverInbounds(ctx context.Context, nodeID uint) ([]*domain.Inbound, error)
	SyncInbounds(ctx context.Context, nodeID uint) (*SyncResult, error)

	// Certificate Sync
	SyncCertificatesFromNodes(ctx context.Context) (int, error) // Returns count of imported certs

	// Statistics
	GetNodeStats(ctx context.Context, nodeID uint) (*NodeStats, error)
	GetNodesStatsBulk(ctx context.Context, ids []uint) (map[uint]*NodeStatsResult, error)
	GetNodeHostInfo(ctx context.Context, nodeID uint) (*domain.HostInfo, error)
	GetInboundStats(ctx context.Context, inboundID uint) (*InboundStats, error)

	// Outbound Management
	AddOutbound(ctx context.Context, outbound *domain.Outbound) error
	ListOutbounds(ctx context.Context, nodeID uint) ([]*domain.Outbound, error)
	GetOutbound(ctx context.Context, id uint) (*domain.Outbound, error)
	DeleteOutbound(ctx context.Context, id uint) error
	UpdateOutbound(ctx context.Context, outbound *domain.Outbound) error
	ToggleOutboundDisabled(ctx context.Context, id uint) (*domain.Outbound, error)

	// Outbound Testing
	TestOutbound(ctx context.Context, outboundID uint, testURL string) (*agent.OutboundTestResult, error)

	// Outbound Discovery & Sync
	DiscoverOutbounds(ctx context.Context, nodeID uint) ([]*domain.Outbound, error)
	SyncOutbounds(ctx context.Context, nodeID uint) (*SyncResult, error)

	// Routing Rule Management
	AddRoutingRule(ctx context.Context, rule *domain.RoutingRule) error
	ListRoutingRules(ctx context.Context, nodeID uint) ([]*domain.RoutingRule, error)
	GetRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error)
	DeleteRoutingRule(ctx context.Context, id uint) error
	UpdateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error
	ToggleRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error)
	SyncRoutingRules(ctx context.Context, nodeID uint) (*SyncResult, error)
	MoveRoutingRule(ctx context.Context, id uint, moveUp bool) error
	ReorderRoutingRules(ctx context.Context, nodeID uint, ruleIDs []uint) error
	SyncPresetRoutingRules(ctx context.Context, nodeID uint, rs *domain.RoutingSettings) error

	// Balancing Rule Management
	AddBalancingRule(ctx context.Context, rule *domain.BalancingRule) error
	ListBalancingRules(ctx context.Context, nodeID uint) ([]*domain.BalancingRule, error)
	UpdateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error
	DeleteBalancingRule(ctx context.Context, id uint) error

	// Reverse Proxy Management
	ListReverseProxies(ctx context.Context, nodeID uint) ([]*domain.ReverseProxy, error)
	GetReverseProxy(ctx context.Context, id uint) (*domain.ReverseProxy, error)
	AddReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error
	UpdateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error
	DeleteReverseProxy(ctx context.Context, id uint) error

	// User Management (Agent Aware)
	AddUserViaAgent(ctx context.Context, node *domain.Node, inboundTag, email, uuid, protocol, flow, encryption string) error
	RemoveUserViaAgent(ctx context.Context, node *domain.Node, inboundTag, email string) error

	// Agent Management
	PushConfigViaAgent(ctx context.Context, nodeID uint) error
	PushFullConfig(ctx context.Context, nodeID uint) error
	GetNodeSystemStats(ctx context.Context, nodeID uint) (*agent.SystemStats, error)
	CheckAgentHealth(ctx context.Context, id uint) (*domain.NodeHealth, error)
	GetNodeWithSystemStats(ctx context.Context, id uint) (*domain.Node, error)
	ListNodesWithSystemStats(ctx context.Context) ([]*domain.Node, error)
	UpdateAgentBinary(ctx context.Context, nodeID uint, binaryContent []byte, checksum, version string, signature []byte) error
	AutoUpdateAgent(ctx context.Context, nodeID uint, progress chan<- UpdateProgress) error

	// Stats History
	SyncNodeStats(ctx context.Context) error
	// SyncSingleNodeByID: manual single-node sweep. Use this instead of
	// xray reset=true (which double-counts vs the agent collector).
	SyncSingleNodeByID(ctx context.Context, nodeID uint) error
	GetNodeStatsHistory(ctx context.Context, nodeID uint, limit int) ([]*domain.NodeStat, error)
	GetNodesStatsHistoryBulk(ctx context.Context, nodeIDs []uint, limit int) (map[uint][]*domain.NodeStat, error)

	// Daily Traffic
	GetNodeDailyTraffic(ctx context.Context, nodeID uint, days int) ([]*domain.NodeDailyTraffic, error)

	// Uptime Events
	GetNodeUptimeEvents(ctx context.Context, nodeID uint, hours int) ([]*domain.NodeUptimeEvent, error)

	// Agent Process Control
	StartXray(ctx context.Context, nodeID uint) error
	StopXray(ctx context.Context, nodeID uint) error
	RestartXray(ctx context.Context, nodeID uint) error

	// Bulk Node Actions
	BulkRestartXray(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult
	BulkPushConfig(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult
	BulkCheckHealth(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult
	BulkUpdateXrayVersion(ctx context.Context, ids []uint, version string) map[uint]*NodeBulkActionResult

	// Xray Version Management
	UpdateXrayVersion(ctx context.Context, nodeID uint, version string) error
	SetXrayDeps(bm interface {
		GetChecksum(version, arch string) (string, error)
		EnsureCached(version, arch string) error
	}, tm interface {
		GenerateDeploymentToken(nodeID uint, duration time.Duration) (string, error)
	}, baseURL string)
	SetHTTPClientFactory(f *httpclient.Factory)
	SetEmbeddedServer(srv *agentserver.Server)
	SetAuditUsecase(a auditDomain.AuditLogUsecase)
	SetWGPeerSource(s WGPeerSource)

	// Node Nuke / Wipe
	Nuke(ctx context.Context, nodeID uint, opts NukeOptions, emit NukeEmitter) (*pb.NukeReport, error)

	// Geofile Management
	UpdateGeoFiles(ctx context.Context, nodeID uint, region string, customGeoIPURL, customGeoSiteURL string) error

	// SSH Management
	GetNodeSSHStatus(ctx context.Context, nodeID uint) (*domain.SSHStatus, error)
	UpdateNodeSSHConfig(ctx context.Context, nodeID uint, enabled bool, port int) error
	ClearNodeSSHLogs(ctx context.Context, nodeID uint) error
	RestartNodeSSH(ctx context.Context, nodeID uint) error

	// GetNodeClient returns an agent.NodeClient for the given node ID.
	GetNodeClient(ctx context.Context, nodeID uint) (agent.NodeClient, error)

	// Certificate Denylist
	PushCertDenylistToNode(ctx context.Context, nodeID uint) error
	PushCertDenylistToAllNodes(ctx context.Context) error

	// Realtime Data
	GetRealtimeUsers(ctx context.Context, nodeID uint) ([]*domain.InboundUsers, error)

	// Xray Config Management
	GetNodeXrayConfig(ctx context.Context, nodeID uint) (string, error)
	UpdateNodeXrayConfig(ctx context.Context, nodeID uint, content string) error
	ValidateNodeXrayConfig(ctx context.Context, nodeID uint, content string) (bool, []string, []string, error)
	GetNodeXrayConfigDiff(ctx context.Context, nodeID uint) (*XrayConfigDiff, error)
	StreamNodeLogs(ctx context.Context, nodeID uint, tail int, follow bool) (<-chan LogEntryDTO, <-chan error, error)
	GetNodeRecentLogs(ctx context.Context, nodeID uint, lines int) ([]LogEntryDTO, error)

	// Terminal Access
	OpenTerminal(ctx context.Context, nodeID uint) (pb.NodeAgent_OpenTerminalClient, func(), error)

	// Heartbeat
	StartHeartbeats(ctx context.Context)
	StopHeartbeats()
	GetHeartbeatManager() *HeartbeatManager

	// Tools
	GenerateVLESSKeys(ctx context.Context) ([]map[string]string, error)
	GenerateXrayConfig(ctx context.Context, nodeID uint) (string, error)

	// Host Management (presentation-layer templates)
	AddHost(ctx context.Context, host *domain.Host) error
	BulkCreateInfoHosts(ctx context.Context, host *domain.Host, planIDs []uint) ([]*domain.Host, error)
	ListHosts(ctx context.Context, inboundID uint) ([]*domain.Host, error)
	ListHostsByPlan(ctx context.Context, planID uint) ([]*domain.Host, error)
	ListAllHosts(ctx context.Context, search string, nodeID, inboundID, planID uint, isDisabled *bool, hostType string, tag string, sortBy string, sortOrder string, offset, limit int) ([]*domain.Host, int64, error)
	GetHost(ctx context.Context, id uint) (*domain.Host, error)
	UpdateHost(ctx context.Context, host *domain.Host) error
	DeleteHost(ctx context.Context, id uint) error
	DuplicateHost(ctx context.Context, id uint) (*domain.Host, error)
	BulkUpdateHosts(ctx context.Context, ids []uint, fields map[string]any) (int64, error)
	ListHostTags(ctx context.Context) ([]string, error)

	// Host Template Management
	CreateHostTemplate(ctx context.Context, template *domain.HostTemplate) error
	GetHostTemplate(ctx context.Context, id uint) (*domain.HostTemplate, error)
	UpdateHostTemplate(ctx context.Context, template *domain.HostTemplate) error
	DeleteHostTemplate(ctx context.Context, id uint) error
	ListHostTemplates(ctx context.Context) ([]*domain.HostTemplate, error)
	ApplyHostTemplate(ctx context.Context, templateID uint, hostIDs []uint) (int64, error)

	// Access Logs (per-subscription DNS logs)
	GetAccessLogs(ctx context.Context, nodeID uint, email string, limit int32) ([]*pb.AccessLogEntry, error)
	GetAggregatedAccessLogs(ctx context.Context, nodeIDs []uint, email string, limit int32) ([]AggregatedAccessLogEntry, error)

	// Access Log Analytics (historical summaries from DB)
	GetAccessLogAnalytics(ctx context.Context, filter repository.AccessLogSummaryFilter) ([]*domain.AccessLogSummary, int64, error)
	GetAccessLogTopDomains(ctx context.Context, filter repository.AccessLogSummaryFilter, topN int) ([]DomainCount, error)

	// Starlink Monitoring
	GetStarlinkStatus(ctx context.Context, nodeID uint) (*agent.StarlinkStatus, error)
	GetStarlinkObstructionMap(ctx context.Context, nodeID uint) (*agent.StarlinkObstructionMap, error)
	GetStarlinkHistory(ctx context.Context, nodeID uint, limit int, since *time.Time) ([]*domain.StarlinkStat, error)

	// Inbound Migration
	MigrateInbound(ctx context.Context, sourceInboundID, targetInboundID uint) (*InboundMigrationResult, error)
}

// AggregatedAccessLogEntry wraps a proto access log entry with node metadata.
type AggregatedAccessLogEntry struct {
	NodeID      uint   `json:"node_id"`
	NodeName    string `json:"node_name"`
	NodeCountry string `json:"node_country"`
	Timestamp   int64  `json:"timestamp"`
	SourceIP    string `json:"source_ip"`
	Status      string `json:"status"`
	Network     string `json:"network"`
	Domain      string `json:"domain"`
	Port        int32  `json:"port"`
	InboundTag  string `json:"inbound_tag"`
	OutboundTag string `json:"outbound_tag"`
	Email       string `json:"email"`
}

// configPushState tracks per-node config push state to implement
// rate limiting and exponential backoff for drift-triggered pushes.
type configPushState struct {
	mu          sync.Mutex
	inProgress  bool
	failures    int
	lastFailure time.Time
}

type nodeUsecase struct {
	nodeRepo    repository.NodeRepository
	subRepo     subRepo.SubscriptionRepository
	subIPRepo   subRepo.SubscriptionIPRepository
	accountRepo accountRepo.AccountRepository
	sniUsecase  sniUC.SNIUsecase
	certUC      CertificateUsecase // For mTLS certificate loading
	provService provisioning.ProvisioningService
	eventBus    *events.EventBus
	settingUC   settingDomain.SettingUsecase
	tm          database.TransactionManager

	// Config drift detection: tracks the config hash reported by each agent after the last successful push.
	// Used to detect when an agent's config drifts from what the master last pushed.
	configHashMu         sync.RWMutex
	lastPushedConfigHash map[uint]string // nodeID -> config hash after last successful push

	// Heartbeat manager for the local node's status loop
	heartbeatMgr *HeartbeatManager

	// Stats cache — short-lived memoization of GetNodeStats results.
	// Shields repeat-refresh + multiple panel tabs from fan-out.
	statsCache *nodeStatsCache

	// Per-node config push rate limiting to prevent infinite retry loops
	pushStateMu sync.Mutex
	pushState   map[uint]*configPushState

	// In process node-agent server used by getAgentClient to bypass grpc in single bin mode
	embeddedSrv *agentserver.Server

	// Audit logging (optional — set via SetAuditUsecase after construction to
	// avoid a cyclic dependency between the node and audit usecases).
	auditUC auditDomain.AuditLogUsecase

	// Managed WireGuard peer source (optional, set via SetWGPeerSource).
	wgPeerSource WGPeerSource

	// nukeAgentClientFactory: test override; nil → getAgentClient.
	nukeAgentClientFactory func(context.Context, *domain.Node) (agent.NodeClient, error)

	// statsAgentClientFactory: test override; nil → getAgentClient.
	statsAgentClientFactory func(context.Context, *domain.Node) (agent.NodeClient, error)

	// nukesInFlight tracks nodes with an active Nuke/Wipe so a second call
	// can reject cleanly instead of racing. Per-instance (not package-level)
	// so tests and any multi-hub future stay isolated.
	nukesInFlight   map[uint]struct{}
	nukesInFlightMu sync.Mutex

	// Xray binary distribution (set via SetXrayDeps). Hub pre-caches so
	// GitHub is hit once by the hub, not per-agent (most agents lack a
	// direct path to GitHub).
	xrayBM interface {
		GetChecksum(version, arch string) (string, error)
		EnsureCached(version, arch string) error
	}
	tokenManager interface {
		GenerateDeploymentToken(nodeID uint, duration time.Duration) (string, error)
	}
	baseURL string

	// Outbound HTTP factory for hub-side fetches (geofiles, etc). Optional —
	// set via SetHTTPClientFactory. nil falls back to a direct-internet
	// http.DefaultClient.
	httpFactory *httpclient.Factory
}

// nodeUsecase implements the SNI usecase's re-push hook (RepushForSNI).
var _ sniUC.Repusher = (*nodeUsecase)(nil)

func NewNodeUsecase(nodeRepo repository.NodeRepository, subRepo subRepo.SubscriptionRepository, subIPRepo subRepo.SubscriptionIPRepository, accountRepo accountRepo.AccountRepository, sniUsecase sniUC.SNIUsecase, certUC CertificateUsecase, provService provisioning.ProvisioningService, eventBus *events.EventBus, settingUC settingDomain.SettingUsecase, tm database.TransactionManager) NodeUsecase {
	return &nodeUsecase{
		nodeRepo:             nodeRepo,
		subRepo:              subRepo,
		subIPRepo:            subIPRepo,
		accountRepo:          accountRepo,
		sniUsecase:           sniUsecase,
		certUC:               certUC,
		provService:          provService,
		eventBus:             eventBus,
		settingUC:            settingUC,
		tm:                   tm,
		lastPushedConfigHash: make(map[uint]string),
		pushState:            make(map[uint]*configPushState),
		nukesInFlight:        map[uint]struct{}{},
		statsCache:           newNodeStatsCache(nodeStatsCacheTTL),
	}
}

// getOrCreatePushState returns the configPushState for a node, creating one if needed.
func (u *nodeUsecase) getOrCreatePushState(nodeID uint) *configPushState {
	u.pushStateMu.Lock()
	defer u.pushStateMu.Unlock()
	state, ok := u.pushState[nodeID]
	if !ok {
		state = &configPushState{}
		u.pushState[nodeID] = state
	}
	return state
}

// pushBackoffDuration calculates exponential backoff for config push retries.
// Base 5s, doubles each failure, capped at 5 minutes (max 6 consecutive failures tracked).
func pushBackoffDuration(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	exp := failures
	if exp > 6 {
		exp = 6
	}
	d := time.Duration(5*math.Pow(2, float64(exp-1))) * time.Second
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// tryScheduleConfigPush attempts to schedule a config push for a node with
// deduplication and exponential backoff. It is safe to call from multiple
// goroutines — concurrent/rapid calls for the same node are collapsed.
func (u *nodeUsecase) tryScheduleConfigPush(node *domain.Node) {
	log := logger.GetLogger().WithField("node_id", node.ID)
	state := u.getOrCreatePushState(node.ID)

	state.mu.Lock()
	if state.inProgress {
		state.mu.Unlock()
		log.Debug("[tryScheduleConfigPush] Push already in progress, skipping")
		return
	}
	if state.failures > 0 {
		backoff := pushBackoffDuration(state.failures)
		if time.Since(state.lastFailure) < backoff {
			state.mu.Unlock()
			log.WithField("backoff", backoff).Debug("[tryScheduleConfigPush] Within backoff window, skipping")
			return
		}
	}
	state.inProgress = true
	state.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := u.pushConfigToAgent(ctx, node); err != nil {
			log.WithError(err).Error("[tryScheduleConfigPush] Config push failed")
			state.mu.Lock()
			state.failures++
			state.lastFailure = time.Now()
			state.inProgress = false
			state.mu.Unlock()
		} else {
			log.Info("[tryScheduleConfigPush] Config pushed successfully")
			state.mu.Lock()
			state.failures = 0
			state.lastFailure = time.Time{}
			state.inProgress = false
			state.mu.Unlock()
		}
	}()
}

// resetPushState resets the config push backoff state for a node after a
// successful explicit push (e.g. from AddInbound, UpdateInbound, etc.).
func (u *nodeUsecase) resetPushState(nodeID uint) {
	state := u.getOrCreatePushState(nodeID)
	state.mu.Lock()
	state.failures = 0
	state.lastFailure = time.Time{}
	state.mu.Unlock()
}

// SetEmbeddedServer injects the in-process node-agent server. Once set,
// getAgentClient routes node operations through an in-process EmbeddedClient.
func (u *nodeUsecase) SetEmbeddedServer(srv *agentserver.Server) {
	u.embeddedSrv = srv
}

// SetAuditUsecase injects the audit usecase. Called after construction because
// auditUsecase is built later in the bootstrap sequence (cycle-breaking).
func (u *nodeUsecase) SetAuditUsecase(a auditDomain.AuditLogUsecase) {
	u.auditUC = a
}
