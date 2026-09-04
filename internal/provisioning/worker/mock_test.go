package worker

import (
	"context"
	"sync"
	"time"

	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// ============================================================
// mockProvisioningRepo — implements repository.ProvisioningRepository
// ============================================================

type mockProvisioningRepo struct {
	mu    sync.Mutex
	tasks map[uint]*domain.ProvisioningTask

	// Track calls for assertions
	updateStatusCalls []updateStatusCall
	markSuccessCalls  []uint
	markFailedCalls   []markFailedCall

	// Configurable responses
	fetchPendingResult []*domain.ProvisioningTask
	fetchPendingErr    error
}

type updateStatusCall struct {
	ID     uint
	Status domain.TaskStatus
}

type markFailedCall struct {
	ID     uint
	ErrStr string
	NextAt time.Time
	IsDead bool
}

func newMockProvisioningRepo() *mockProvisioningRepo {
	return &mockProvisioningRepo{
		tasks: make(map[uint]*domain.ProvisioningTask),
	}
}

func (m *mockProvisioningRepo) Enqueue(_ context.Context, task *domain.ProvisioningTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	return nil
}

func (m *mockProvisioningRepo) FetchPending(_ context.Context, _ int) ([]*domain.ProvisioningTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fetchPendingErr != nil {
		return nil, m.fetchPendingErr
	}
	return m.fetchPendingResult, nil
}

func (m *mockProvisioningRepo) UpdateStatus(_ context.Context, id uint, status domain.TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateStatusCalls = append(m.updateStatusCalls, updateStatusCall{ID: id, Status: status})
	if t, ok := m.tasks[id]; ok {
		t.Status = status
	}
	return nil
}

func (m *mockProvisioningRepo) MarkSuccess(_ context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markSuccessCalls = append(m.markSuccessCalls, id)
	if t, ok := m.tasks[id]; ok {
		t.Status = domain.StatusCompleted
	}
	return nil
}

func (m *mockProvisioningRepo) MarkFailed(_ context.Context, id uint, errStr string, nextRetry time.Time, isDead bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markFailedCalls = append(m.markFailedCalls, markFailedCall{
		ID:     id,
		ErrStr: errStr,
		NextAt: nextRetry,
		IsDead: isDead,
	})
	if t, ok := m.tasks[id]; ok {
		t.LastError = errStr
		t.NextRetryAt = nextRetry
		t.RetryCount++
		if isDead {
			t.Status = domain.StatusDead
		} else {
			t.Status = domain.StatusFailed
		}
	}
	return nil
}

func (m *mockProvisioningRepo) CountPending(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockProvisioningRepo) CancelTasksForNode(_ context.Context, _ uint) error {
	return nil
}

func (m *mockProvisioningRepo) CleanupCompletedTasks(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

// ============================================================
// mockNodeUsecase — implements nodeUC.NodeUsecase
// ============================================================

type mockNodeUsecase struct {
	// GetNode behaviour
	nodes      map[uint]*nodeDomain.Node
	getNodeErr error

	// AddUserViaAgent behaviour
	addUserErr error

	// RemoveUserViaAgent behaviour
	removeUserErr error

	// Track calls
	addUserCalls    []addUserCall
	removeUserCalls []removeUserCall
}

type addUserCall struct {
	NodeID     uint
	InboundTag string
	Email      string
	UUID       string
	Protocol   string
	Flow       string
	Encryption string
}

type removeUserCall struct {
	NodeID     uint
	InboundTag string
	Email      string
}

func newMockNodeUsecase() *mockNodeUsecase {
	return &mockNodeUsecase{
		nodes: make(map[uint]*nodeDomain.Node),
	}
}

func (m *mockNodeUsecase) GetNode(_ context.Context, id uint) (*nodeDomain.Node, error) {
	if m.getNodeErr != nil {
		return nil, m.getNodeErr
	}
	node, ok := m.nodes[id]
	if !ok {
		return nil, nodeUC.ErrNodeNotFound
	}
	return node, nil
}

func (m *mockNodeUsecase) AddUserViaAgent(_ context.Context, node *nodeDomain.Node, inboundTag, email, uuid, protocol, flow, encryption string) error {
	m.addUserCalls = append(m.addUserCalls, addUserCall{
		NodeID:     node.ID,
		InboundTag: inboundTag,
		Email:      email,
		UUID:       uuid,
		Protocol:   protocol,
		Flow:       flow,
		Encryption: encryption,
	})
	return m.addUserErr
}

func (m *mockNodeUsecase) RemoveUserViaAgent(_ context.Context, node *nodeDomain.Node, inboundTag, email string) error {
	m.removeUserCalls = append(m.removeUserCalls, removeUserCall{
		NodeID:     node.ID,
		InboundTag: inboundTag,
		Email:      email,
	})
	return m.removeUserErr
}

// ---- Stubs for remaining NodeUsecase interface methods ----

func (m *mockNodeUsecase) CreateNode(context.Context, string, string, string, string, int, int, string, bool, bool) (*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ListNodes(context.Context) ([]*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeUsecase) DeleteNode(context.Context, uint, bool) error { return nil }
func (m *mockNodeUsecase) RepushForSNI(context.Context, uint)           {}
func (m *mockNodeUsecase) MigrateNodeAccounts(context.Context, uint, uint, uint) error {
	return nil
}
func (m *mockNodeUsecase) UpdateNode(context.Context, *nodeDomain.Node) error { return nil }
func (m *mockNodeUsecase) UpdateNodeDNSSettings(context.Context, uint, *nodeDomain.DNSSettings) error {
	return nil
}
func (m *mockNodeUsecase) ClearNodeDNSSettings(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) UpdateNodeFakeDNSSettings(context.Context, uint, []nodeDomain.FakeDNSPool) error {
	return nil
}
func (m *mockNodeUsecase) CheckNodeHealth(context.Context, uint) (*nodeDomain.NodeHealth, error) {
	return nil, nil
}

func (m *mockNodeUsecase) BackfillNodeUUIDs(context.Context) error { return nil }

// Inbound Management
func (m *mockNodeUsecase) AddInbound(context.Context, *nodeDomain.Inbound) error { return nil }
func (m *mockNodeUsecase) ListInbounds(context.Context, uint) ([]*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ToggleInboundDisabled(context.Context, uint) (*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) DeleteInbound(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) GetInbound(context.Context, uint) (*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateInbound(context.Context, *nodeDomain.Inbound) error { return nil }

// Inbound Discovery & Sync
func (m *mockNodeUsecase) DiscoverInbounds(context.Context, uint) ([]*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) SyncInbounds(context.Context, uint) (*nodeUC.SyncResult, error) {
	return nil, nil
}

// Certificate Sync
func (m *mockNodeUsecase) SyncCertificatesFromNodes(context.Context) (int, error) { return 0, nil }

// Statistics
func (m *mockNodeUsecase) GetNodeStats(context.Context, uint) (*nodeUC.NodeStats, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetNodesStatsBulk(context.Context, []uint) (map[uint]*nodeUC.NodeStatsResult, error) {
	return map[uint]*nodeUC.NodeStatsResult{}, nil
}
func (m *mockNodeUsecase) GetNodeHostInfo(context.Context, uint) (*nodeDomain.HostInfo, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetInboundStats(context.Context, uint) (*nodeUC.InboundStats, error) {
	return nil, nil
}

// Outbound Management
func (m *mockNodeUsecase) AddOutbound(context.Context, *nodeDomain.Outbound) error { return nil }
func (m *mockNodeUsecase) ListOutbounds(context.Context, uint) ([]*nodeDomain.Outbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetOutbound(context.Context, uint) (*nodeDomain.Outbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) DeleteOutbound(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) UpdateOutbound(context.Context, *nodeDomain.Outbound) error {
	return nil
}
func (m *mockNodeUsecase) ToggleOutboundDisabled(context.Context, uint) (*nodeDomain.Outbound, error) {
	return nil, nil
}

// Outbound Testing
func (m *mockNodeUsecase) TestOutbound(context.Context, uint, nodeUC.OutboundTestOptions) (*nodeUC.OutboundTestOutcome, error) {
	return nil, nil
}

// Outbound Discovery & Sync
func (m *mockNodeUsecase) DiscoverOutbounds(context.Context, uint) ([]*nodeDomain.Outbound, error) {
	return nil, nil
}
func (m *mockNodeUsecase) SyncOutbounds(context.Context, uint) (*nodeUC.SyncResult, error) {
	return nil, nil
}

// Routing Rule Management
func (m *mockNodeUsecase) AddRoutingRule(context.Context, *nodeDomain.RoutingRule) error {
	return nil
}
func (m *mockNodeUsecase) ListRoutingRules(context.Context, uint) ([]*nodeDomain.RoutingRule, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetRoutingRule(context.Context, uint) (*nodeDomain.RoutingRule, error) {
	return nil, nil
}
func (m *mockNodeUsecase) DeleteRoutingRule(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) UpdateRoutingRule(context.Context, *nodeDomain.RoutingRule) error {
	return nil
}
func (m *mockNodeUsecase) ToggleRoutingRule(context.Context, uint) (*nodeDomain.RoutingRule, error) {
	return nil, nil
}
func (m *mockNodeUsecase) SyncRoutingRules(context.Context, uint) (*nodeUC.SyncResult, error) {
	return nil, nil
}
func (m *mockNodeUsecase) MoveRoutingRule(context.Context, uint, bool) error { return nil }
func (m *mockNodeUsecase) ReorderRoutingRules(context.Context, uint, []uint) error {
	return nil
}
func (m *mockNodeUsecase) SyncPresetRoutingRules(context.Context, uint, *nodeDomain.RoutingSettings) error {
	return nil
}

// Balancing Rule Management
func (m *mockNodeUsecase) AddBalancingRule(context.Context, *nodeDomain.BalancingRule) error {
	return nil
}
func (m *mockNodeUsecase) ListBalancingRules(context.Context, uint) ([]*nodeDomain.BalancingRule, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateBalancingRule(context.Context, *nodeDomain.BalancingRule) error {
	return nil
}
func (m *mockNodeUsecase) DeleteBalancingRule(context.Context, uint) error { return nil }

// Reverse Proxy Management
func (m *mockNodeUsecase) ListReverseProxies(context.Context, uint) ([]*nodeDomain.ReverseProxy, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetReverseProxy(context.Context, uint) (*nodeDomain.ReverseProxy, error) {
	return nil, nil
}
func (m *mockNodeUsecase) AddReverseProxy(context.Context, *nodeDomain.ReverseProxy) error {
	return nil
}
func (m *mockNodeUsecase) UpdateReverseProxy(context.Context, *nodeDomain.ReverseProxy) error {
	return nil
}
func (m *mockNodeUsecase) DeleteReverseProxy(context.Context, uint) error { return nil }

// Agent Management
func (m *mockNodeUsecase) PushConfigViaAgent(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) PushFullConfig(context.Context, uint) error     { return nil }
func (m *mockNodeUsecase) GetNodeSystemStats(context.Context, uint) (*agent.SystemStats, error) {
	return nil, nil
}
func (m *mockNodeUsecase) CheckAgentHealth(context.Context, uint) (*nodeDomain.NodeHealth, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetNodeWithSystemStats(context.Context, uint) (*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ListNodesWithSystemStats(context.Context) ([]*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateAgentBinary(context.Context, uint, []byte, string, string, []byte) error {
	return nil
}
func (m *mockNodeUsecase) AutoUpdateAgent(context.Context, uint, chan<- nodeUC.UpdateProgress) error {
	return nil
}

// Stats History
func (m *mockNodeUsecase) SyncNodeStats(context.Context) error            { return nil }
func (m *mockNodeUsecase) SyncSingleNodeByID(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) GetNodeStatsHistory(context.Context, uint, int) ([]*nodeDomain.NodeStat, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetNodesStatsHistoryBulk(context.Context, []uint, int) (map[uint][]*nodeDomain.NodeStat, error) {
	return map[uint][]*nodeDomain.NodeStat{}, nil
}

// Daily Traffic
func (m *mockNodeUsecase) GetNodeDailyTraffic(context.Context, uint, int) ([]*nodeDomain.NodeDailyTraffic, error) {
	return nil, nil
}

// Uptime Events
func (m *mockNodeUsecase) GetNodeUptimeEvents(context.Context, uint, int) ([]*nodeDomain.NodeUptimeEvent, error) {
	return nil, nil
}

// Agent Process Control
func (m *mockNodeUsecase) StartXray(context.Context, uint) error   { return nil }
func (m *mockNodeUsecase) StopXray(context.Context, uint) error    { return nil }
func (m *mockNodeUsecase) RestartXray(context.Context, uint) error { return nil }

// Bulk Node Actions
func (m *mockNodeUsecase) BulkRestartXray(context.Context, []uint) map[uint]*nodeUC.NodeBulkActionResult {
	return map[uint]*nodeUC.NodeBulkActionResult{}
}
func (m *mockNodeUsecase) BulkPushConfig(context.Context, []uint) map[uint]*nodeUC.NodeBulkActionResult {
	return map[uint]*nodeUC.NodeBulkActionResult{}
}
func (m *mockNodeUsecase) BulkCheckHealth(context.Context, []uint) map[uint]*nodeUC.NodeBulkActionResult {
	return map[uint]*nodeUC.NodeBulkActionResult{}
}
func (m *mockNodeUsecase) BulkUpdateXrayVersion(context.Context, []uint, string) map[uint]*nodeUC.NodeBulkActionResult {
	return map[uint]*nodeUC.NodeBulkActionResult{}
}

// Xray Version Management
func (m *mockNodeUsecase) UpdateXrayVersion(context.Context, uint, string) error { return nil }
func (m *mockNodeUsecase) SetXrayDeps(interface {
	EnsureCached(version, arch string) error
	GetChecksum(version, arch string) (string, error)
}, interface {
	GenerateDeploymentToken(nodeID uint, duration time.Duration) (string, error)
}, string) {
}
func (m *mockNodeUsecase) SetHTTPClientFactory(*httpclient.Factory) {}
func (m *mockNodeUsecase) SetWGPeerSource(nodeUC.WGPeerSource)      {}
func (m *mockNodeUsecase) SetRouterMode(bool)                       {}
func (m *mockNodeUsecase) SetRouterWANSource(func(context.Context) []xray.RouterWAN) {
}
func (m *mockNodeUsecase) SetIngressUplinkSource(func() string)               {}
func (m *mockNodeUsecase) SetInboundsChangedHook(func(context.Context) error) {}
func (m *mockNodeUsecase) SetEmbeddedServer(*agentserver.Server)              {}

// Geofile Management
func (m *mockNodeUsecase) UpdateGeoFiles(context.Context, uint, string, string, string) error {
	return nil
}

// SSH Management
func (m *mockNodeUsecase) GetNodeSSHStatus(context.Context, uint) (*nodeDomain.SSHStatus, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateNodeSSHConfig(context.Context, uint, bool, int) error { return nil }
func (m *mockNodeUsecase) ClearNodeSSHLogs(context.Context, uint) error               { return nil }
func (m *mockNodeUsecase) RestartNodeSSH(context.Context, uint) error                 { return nil }

// GetNodeClient
func (m *mockNodeUsecase) GetNodeClient(context.Context, uint) (agent.NodeClient, error) {
	return nil, nil
}

// Certificate Denylist
func (m *mockNodeUsecase) PushCertDenylistToNode(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) PushCertDenylistToAllNodes(context.Context) error   { return nil }

// Realtime Data
func (m *mockNodeUsecase) GetRealtimeUsers(context.Context, uint) ([]*nodeDomain.InboundUsers, error) {
	return nil, nil
}

// Xray Config Management
func (m *mockNodeUsecase) GetNodeXrayConfig(context.Context, uint) (string, error) {
	return "", nil
}
func (m *mockNodeUsecase) UpdateNodeXrayConfig(context.Context, uint, string) error { return nil }
func (m *mockNodeUsecase) ValidateNodeXrayConfig(context.Context, uint, string) (bool, []string, []string, error) {
	return false, nil, nil, nil
}
func (m *mockNodeUsecase) GetNodeXrayConfigDiff(context.Context, uint) (*nodeUC.XrayConfigDiff, error) {
	return nil, nil
}
func (m *mockNodeUsecase) StreamNodeLogs(context.Context, uint, int, bool) (<-chan nodeUC.LogEntryDTO, <-chan error, error) {
	return nil, nil, nil
}
func (m *mockNodeUsecase) GetNodeRecentLogs(context.Context, uint, int) ([]nodeUC.LogEntryDTO, error) {
	return nil, nil
}

// Terminal Access
func (m *mockNodeUsecase) OpenTerminal(context.Context, uint) (pb.NodeAgent_OpenTerminalClient, func(), error) {
	return nil, nil, nil
}

// Heartbeat
func (m *mockNodeUsecase) StartHeartbeats(context.Context)               {}
func (m *mockNodeUsecase) StopHeartbeats()                               {}
func (m *mockNodeUsecase) GetHeartbeatManager() *nodeUC.HeartbeatManager { return nil }

// Tools
func (m *mockNodeUsecase) GenerateVLESSKeys(context.Context) ([]map[string]string, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GenerateXrayConfig(context.Context, uint) (string, error) {
	return "", nil
}

// Host Management
func (m *mockNodeUsecase) AddHost(context.Context, *nodeDomain.Host) error { return nil }
func (m *mockNodeUsecase) BulkCreateInfoHosts(context.Context, *nodeDomain.Host, []uint) ([]*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ListHosts(context.Context, uint) ([]*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ListHostsByPlan(context.Context, uint) ([]*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ListAllHosts(context.Context, string, uint, uint, uint, *bool, string, string, string, string, int, int) ([]*nodeDomain.Host, int64, error) {
	return nil, 0, nil
}
func (m *mockNodeUsecase) GetHost(context.Context, uint) (*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateHost(context.Context, *nodeDomain.Host) error { return nil }
func (m *mockNodeUsecase) DeleteHost(context.Context, uint) error             { return nil }
func (m *mockNodeUsecase) DuplicateHost(context.Context, uint) (*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeUsecase) BulkUpdateHosts(context.Context, []uint, map[string]any) (int64, error) {
	return 0, nil
}
func (m *mockNodeUsecase) ListHostTags(context.Context) ([]string, error) { return nil, nil }

// Host Template Management
func (m *mockNodeUsecase) CreateHostTemplate(context.Context, *nodeDomain.HostTemplate) error {
	return nil
}
func (m *mockNodeUsecase) GetHostTemplate(context.Context, uint) (*nodeDomain.HostTemplate, error) {
	return nil, nil
}
func (m *mockNodeUsecase) UpdateHostTemplate(context.Context, *nodeDomain.HostTemplate) error {
	return nil
}
func (m *mockNodeUsecase) DeleteHostTemplate(context.Context, uint) error { return nil }
func (m *mockNodeUsecase) ListHostTemplates(context.Context) ([]*nodeDomain.HostTemplate, error) {
	return nil, nil
}
func (m *mockNodeUsecase) ApplyHostTemplate(context.Context, uint, []uint) (int64, error) {
	return 0, nil
}

// Access Logs
func (m *mockNodeUsecase) GetAccessLogs(context.Context, uint, string, int32) ([]*pb.AccessLogEntry, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetAggregatedAccessLogs(context.Context, []uint, string, int32) ([]nodeUC.AggregatedAccessLogEntry, error) {
	return nil, nil
}

// Access Log Analytics
func (m *mockNodeUsecase) GetAccessLogAnalytics(context.Context, nodeRepo.AccessLogSummaryFilter) ([]*nodeDomain.AccessLogSummary, int64, error) {
	return nil, 0, nil
}
func (m *mockNodeUsecase) GetAccessLogTopDomains(context.Context, nodeRepo.AccessLogSummaryFilter, int) ([]nodeUC.DomainCount, error) {
	return nil, nil
}

// Starlink Monitoring
func (m *mockNodeUsecase) GetStarlinkStatus(context.Context, uint) (*agent.StarlinkStatus, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetStarlinkObstructionMap(context.Context, uint) (*agent.StarlinkObstructionMap, error) {
	return nil, nil
}
func (m *mockNodeUsecase) GetStarlinkHistory(context.Context, uint, int, *time.Time) ([]*nodeDomain.StarlinkStat, error) {
	return nil, nil
}

// Inbound Migration
func (m *mockNodeUsecase) MigrateInbound(context.Context, uint, uint) (*nodeUC.InboundMigrationResult, error) {
	return nil, nil
}

// Audit wiring (added for Node Nuke orchestration)
func (m *mockNodeUsecase) SetAuditUsecase(auditDomain.AuditLogUsecase) {}

// Node Nuke / Wipe
func (m *mockNodeUsecase) Nuke(context.Context, uint, nodeUC.NukeOptions, nodeUC.NukeEmitter) (*pb.NukeReport, error) {
	return nil, nil
}
