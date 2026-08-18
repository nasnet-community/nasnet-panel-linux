package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"golang.org/x/sync/errgroup"
)

// NodeStatsResult is one entry in a bulk stats response.
// Stats is nil when Error is set.
type NodeStatsResult struct {
	Stats *NodeStats `json:"stats,omitempty"`
	Error string     `json:"error,omitempty"`
}

const (
	bulkStatsConcurrency = 10
	bulkStatsPerNodeTTL  = 5 * time.Second
)

// GetNodesStatsBulk fetches stats for the given node IDs in parallel
// (empty ids → all nodes). Per-node 5s deadline, failures returned inline
// as Error entries. Offline nodes short-circuit so they don't burn dial
// timeouts and starve the pool.
func (u *nodeUsecase) GetNodesStatsBulk(ctx context.Context, ids []uint) (map[uint]*NodeStatsResult, error) {
	// Pre-load nodes so we can classify online vs offline before fan-out.
	// When the caller supplied no explicit ids, we still end up pulling
	// every node (same as before), just from one place.
	var nodes []*domain.Node
	if len(ids) == 0 {
		all, err := u.nodeRepo.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		nodes = all
	} else {
		nodes = make([]*domain.Node, 0, len(ids))
		for _, id := range ids {
			n, err := u.nodeRepo.GetNode(ctx, id)
			if err != nil {
				// Missing node ≠ fatal — surface as per-entry error so
				// the rest of the batch still returns.
				continue
			}
			nodes = append(nodes, n)
		}
	}

	results := make(map[uint]*NodeStatsResult, len(nodes))
	online := make([]*domain.Node, 0, len(nodes))
	for _, n := range nodes {
		if !n.IsOnline {
			results[n.ID] = &NodeStatsResult{Error: "node is offline"}
			continue
		}
		online = append(online, n)
	}

	if len(online) == 0 {
		return results, nil
	}

	// Pre-fetch per-node account counts in one grouped query (else 2
	// Counts per goroutine). Error swallowed; counts fall back to zero.
	onlineIDs := make([]uint, 0, len(online))
	for _, n := range online {
		onlineIDs = append(onlineIDs, n.ID)
	}
	counts, err := u.accountRepo.CountByNodes(ctx, onlineIDs)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[GetNodesStatsBulk] CountByNodes failed; per-node counts will be zero")
		counts = nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, bulkStatsConcurrency)

	for _, n := range online {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(node *domain.Node) {
			defer wg.Done()
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, bulkStatsPerNodeTTL)
			defer cancel()

			// getNodeStatsForNode reuses the node record we already
			// loaded AND the preloaded counts map so it doesn't issue
			// per-node DB queries.
			stats, err := u.getNodeStatsForNodeWithCounts(callCtx, node, counts)
			entry := &NodeStatsResult{}
			if err != nil {
				entry.Error = err.Error()
			} else {
				entry.Stats = stats
			}

			mu.Lock()
			results[node.ID] = entry
			mu.Unlock()
		}(n)
	}

	wg.Wait()
	return results, nil
}

// GetNodeStats retrieves aggregated statistics for a node from Xray
// GetNodeHostInfo retrieves static host info
func (u *nodeUsecase) GetNodeHostInfo(ctx context.Context, nodeID uint) (*domain.HostInfo, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	info, err := client.GetHostInfo(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.HostInfo{
		Hostname:             info.Hostname,
		OS:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		Arch:                 info.Arch,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		CPUModelName:         info.CPUModelName,
		CPUCores:             info.CPUCores,
		TotalMemory:          info.TotalMemory,
		TotalSwap:            info.TotalSwap,
		BootTime:             info.BootTime,
	}, nil
}

// GetNodeStats: aggregated system + Xray stats. Short-lived cache
// dedupes multi-tab panel polls; miss path = getNodeStatsForNode.
func (u *nodeUsecase) GetNodeStats(ctx context.Context, nodeID uint) (*NodeStats, error) {
	if cached := u.statsCache.Get(nodeID); cached != nil {
		return cached, nil
	}

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return u.getNodeStatsForNode(ctx, node)
}

// getNodeStatsForNode: single-node entry path.
func (u *nodeUsecase) getNodeStatsForNode(ctx context.Context, node *domain.Node) (*NodeStats, error) {
	return u.getNodeStatsForNodeWithCounts(ctx, node, nil)
}

// getNodeStatsForNodeWithCounts runs the four agent RPCs concurrently via
// errgroup (HTTP/2 multiplexing collapses 4×RTT → ~1×RTT). preloadedCounts
// is non-nil from the bulk sweep to skip per-node Count queries.
func (u *nodeUsecase) getNodeStatsForNodeWithCounts(
	ctx context.Context,
	node *domain.Node,
	preloadedCounts map[uint]accountRepo.NodeAccountCount,
) (*NodeStats, error) {
	log := logger.GetLogger()
	stats := &NodeStats{}

	// Seed from persisted DB values; live delta from Xray added below.
	stats.TotalUplink = node.TotalUplink
	stats.TotalDownlink = node.TotalDownlink

	client, clientErr := u.getAgentClient(node)
	if clientErr == nil && client != nil {
		defer closeAgentClient(client)

		// Four independent RPCs — fan out so the round-trip cost is
		// paid once, not four times. Per-RPC errors are logged and
		// swallowed so partial data still reaches the panel.
		var (
			sysStats *agent.SystemStats
			status   *pb.NodeStatus
			xStats   *agent.XrayStats
			version  *agent.VersionInfo

			sysErr, statusErr, xErr, verErr error
		)

		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			sysStats, sysErr = client.GetSystemStats(gctx)
			return nil // soft errors — never cancel siblings
		})
		g.Go(func() error {
			status, statusErr = client.GetStatus(gctx)
			return nil
		})
		g.Go(func() error {
			xStats, xErr = client.GetXrayStats(gctx, false)
			return nil
		})
		g.Go(func() error {
			version, verErr = client.GetVersion(gctx)
			return nil
		})
		_ = g.Wait()

		if sysErr == nil && sysStats != nil {
			stats.CPUPercent = sysStats.CPUUsagePercent
			stats.MemoryPercent = sysStats.MemoryUsagePercent
			stats.MemoryUsedMB = sysStats.MemoryUsedBytes / (1024 * 1024)
			stats.MemoryTotalMB = sysStats.MemoryTotalBytes / (1024 * 1024)
			stats.DiskPercent = sysStats.DiskUsagePercent
			stats.DiskUsedGB = sysStats.DiskUsedBytes / (1024 * 1024 * 1024)
			stats.DiskTotalGB = sysStats.DiskTotalBytes / (1024 * 1024 * 1024)
			stats.DownSpeed = sysStats.NetworkRecvRate
			stats.TcpCount = sysStats.TcpCount
			stats.UdpCount = sysStats.UdpCount
			stats.FdCount = sysStats.FdCount
			stats.SystemUptime = sysStats.SystemUptimeSeconds
			stats.LoadAvg1 = sysStats.LoadAvg1
			stats.LoadAvg5 = sysStats.LoadAvg5
			stats.LoadAvg15 = sysStats.LoadAvg15
		}

		if statusErr == nil && status != nil {
			stats.XrayRunning = status.XrayRunning
			if status.XrayRunning {
				stats.XrayStatus = "running"
			} else {
				stats.XrayStatus = "stopped"
			}
			stats.XrayPID = status.XrayPid
			stats.ProcessUptime = status.UptimeSeconds
			if stats.XrayVersion == "" {
				stats.XrayVersion = status.XrayVersion
			}
		}

		// Only fold the Xray traffic delta in when the process is
		// actually running — a stale stats response from a stopped
		// process would double-count bytes on restart.
		if stats.XrayRunning && xErr == nil && xStats != nil {
			stats.TotalUplink += xStats.TotalUplink
			stats.TotalDownlink += xStats.TotalDownlink
		}

		if verErr == nil && version != nil {
			stats.AgentVersion = version.AgentVersion
			if stats.XrayVersion == "" {
				stats.XrayVersion = version.XrayVersion
			}
		}
	}

	// Online users come from the in-memory cache populated by the
	// subscription syncer — same source dashboard/accounts use.
	stats.OnlineUsers = cache.GetNodeOnlineCount(node.ID)

	// Total + active account counts. Bulk path passes these preloaded
	// from a single GROUP BY query; single-node path falls back to
	// two Count queries (fine — called rarely relative to bulk).
	if preloadedCounts != nil {
		if c, ok := preloadedCounts[node.ID]; ok {
			stats.TotalAccounts = c.Total
			stats.ActiveAccounts = c.Active
		}
	} else {
		nid := node.ID
		if total, err := u.accountRepo.Count(ctx, accountRepo.AccountFilter{NodeID: &nid}); err == nil {
			stats.TotalAccounts = total
		} else {
			log.WithError(err).WithField("node_id", nid).Warn("[GetNodeStats] Failed to count total accounts")
		}
		if active, err := u.accountRepo.Count(ctx, accountRepo.AccountFilter{NodeID: &nid, Status: "active"}); err == nil {
			stats.ActiveAccounts = active
		} else {
			log.WithError(err).WithField("node_id", nid).Warn("[GetNodeStats] Failed to count active accounts")
		}
	}

	u.statsCache.Put(node.ID, stats)
	return stats, nil
}

// GetInboundStats retrieves traffic statistics for a specific inbound
func (u *nodeUsecase) GetInboundStats(ctx context.Context, inboundID uint) (*InboundStats, error) {
	// Get inbound with node info
	inbound, err := u.nodeRepo.GetInboundWithNode(ctx, inboundID)
	if err != nil {
		return nil, err
	}

	if inbound.Node == nil {
		return nil, fmt.Errorf("node not found for inbound")
	}

	if inbound.Node == nil {
		return nil, fmt.Errorf("node not found for inbound")
	}

	stats := &InboundStats{}

	// Query inbound traffic stats
	// Use Agent API
	var client agent.NodeClient
	client, err = u.getAgentClient(inbound.Node)
	if err == nil {
		defer closeAgentClient(client)

		var xrayStats *agent.XrayStats
		xrayStats, err = client.GetXrayStats(ctx, false)
		if err == nil && xrayStats != nil {
			stats.TotalUplink = xrayStats.InboundUplink[inbound.Tag]
			stats.TotalDownlink = xrayStats.InboundDownlink[inbound.Tag]
		}
	}

	// Count active users on this inbound by querying user stats
	// Count active users (Last 10 minutes)
	activeCount, _ := u.subRepo.CountActiveByInbound(ctx, inboundID, time.Now().Add(-10*time.Minute))
	stats.ActiveUsers = int(activeCount)

	return stats, nil
}

// syncStatsConcurrency caps how many nodes are synced in parallel.
// HTTP/2-multiplexed agent RPCs don't serialize on the pool, so this
// ceiling exists mainly to bound DB write fan-in during a single pass.
const syncStatsConcurrency = 8

// syncStatsPerNodeTimeout bounds wall time for a single node's full
// stats sweep. One slow node must not stall the batch past the next
// scheduler tick.
const syncStatsPerNodeTimeout = 10 * time.Second

// SyncNodeStats syncs traffic usage from Xray to DB for every active
// node. Parallel up to syncStatsConcurrency; per-node syncStatsPerNodeTimeout
// so a hung agent can't block the pass. Offline nodes skipped (HeartbeatManager
// re-detects).
func (u *nodeUsecase) SyncNodeStats(ctx context.Context) error {
	nodes, err := u.nodeRepo.ListActiveNodes(ctx)
	if err != nil {
		return err
	}

	// Pre-fetch per-node account counts once so each goroutine's SSE
	// publish doesn't re-issue two Count queries. Soft error — worst
	// case per-node counts publish as zero this cycle.
	ids := make([]uint, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	counts, countsErr := u.accountRepo.CountByNodes(ctx, ids)
	if countsErr != nil {
		logger.GetLogger().WithError(countsErr).Warn("[SyncNodeStats] CountByNodes failed; per-node counts will be zero")
		counts = nil
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, syncStatsConcurrency)

	for _, node := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(n *domain.Node) {
			defer wg.Done()
			defer func() { <-sem }()

			nCtx, cancel := context.WithTimeout(ctx, syncStatsPerNodeTimeout)
			defer cancel()

			// Per-node panic guard so one bad apple doesn't take down
			// the whole sweep. Unlikely but cheap.
			defer func() {
				if r := recover(); r != nil {
					logger.GetLogger().WithField("node", n.Name).Errorf("[SyncNodeStats] panic: %v", r)
				}
			}()

			u.syncSingleNode(nCtx, n, counts)
		}(node)
	}

	wg.Wait()
	return nil
}

// SyncSingleNodeByID runs a manual sweep for one node, reusing the
// scheduled sweep's buffered path (no xray counter reset).
func (u *nodeUsecase) SyncSingleNodeByID(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("load node %d: %w", nodeID, err)
	}
	if !node.IsOnline {
		return fmt.Errorf("node %d is offline", nodeID)
	}
	nCtx, cancel := context.WithTimeout(ctx, syncStatsPerNodeTimeout)
	defer cancel()
	// Panic guard: misbehaving agent must not 500 the admin "Sync now" handler.
	defer func() {
		if r := recover(); r != nil {
			logger.GetLogger().WithField("node", node.Name).Errorf("[SyncSingleNodeByID] panic: %v", r)
		}
	}()
	u.syncSingleNode(nCtx, node, nil)
	return nil
}

// getAgentClientForStats: tests swap via statsAgentClientFactory.
func (u *nodeUsecase) getAgentClientForStats(ctx context.Context, node *domain.Node) (agent.NodeClient, error) {
	if u.statsAgentClientFactory != nil {
		return u.statsAgentClientFactory(ctx, node)
	}
	return u.getAgentClient(node)
}

// agentVersionCacheTTL: agent/xray versions only change on deploys, so
// the stats sweep refreshes them at this cadence instead of every tick.
const agentVersionCacheTTL = 10 * time.Minute

// agentVersionCacheEntry holds one node's cached version pair.
type agentVersionCacheEntry struct {
	agentVersion string
	xrayVersion  string
	fetchedAt    time.Time
}

// agentVersions returns (agentVersion, xrayVersion) for a node, hitting
// the agent's GetVersion RPC only when the cache is stale or forceRefresh
// is set (reconnect path). On a failed refresh the stale pair is served —
// a wrong-for-minutes version beats an empty one in the panel.
func (u *nodeUsecase) agentVersions(ctx context.Context, client agent.NodeClient, nodeID uint, forceRefresh bool) (string, string) {
	u.versionCacheMu.Lock()
	entry, ok := u.versionCache[nodeID]
	u.versionCacheMu.Unlock()

	if ok && !forceRefresh && time.Since(entry.fetchedAt) < agentVersionCacheTTL {
		return entry.agentVersion, entry.xrayVersion
	}

	ver, err := client.GetVersion(ctx)
	if err != nil || ver == nil {
		return entry.agentVersion, entry.xrayVersion
	}

	entry = agentVersionCacheEntry{
		agentVersion: ver.AgentVersion,
		xrayVersion:  ver.XrayVersion,
		fetchedAt:    time.Now(),
	}
	u.versionCacheMu.Lock()
	if u.versionCache == nil {
		u.versionCache = make(map[uint]agentVersionCacheEntry)
	}
	u.versionCache[nodeID] = entry
	u.versionCacheMu.Unlock()
	return entry.agentVersion, entry.xrayVersion
}

const (
	// onlineIPsSyncInterval: this sweep is what refreshes the online-user
	// cache, whose entries expire after 15s (cache.maxAge). Refreshing on
	// that same period would let them lapse between passes — the sweep
	// reaches this step only after its DB work — and the online counts
	// would flap to zero. Stay comfortably inside the window while still
	// skipping every other 5s tick.
	onlineIPsSyncInterval = 10 * time.Second
	// accessLogSyncInterval: summaries are hourly buckets; 60s keeps the
	// panel fresh at a fraction of the old per-tick fetch rate.
	accessLogSyncInterval = 60 * time.Second
)

// statsCadenceDue reports whether interval has elapsed since the node's last
// stamped run in m, stamping now when due. m points at one of the usecase's
// cadence maps (lazy-initialised here so test fixtures built as bare struct
// literals keep working); access is serialized by statsCadenceMu.
func (u *nodeUsecase) statsCadenceDue(m *map[uint]time.Time, nodeID uint, interval time.Duration) bool {
	u.statsCadenceMu.Lock()
	defer u.statsCadenceMu.Unlock()
	if *m == nil {
		*m = make(map[uint]time.Time)
	}
	if time.Since((*m)[nodeID]) < interval {
		return false
	}
	(*m)[nodeID] = time.Now()
	return true
}

// syncSingleNode collects + persists stats for one node. Errors are
// swallowed — a single bad node must not abort the batch. accountCounts
// may be nil (single-node path); totals publish as zero then.
func (u *nodeUsecase) syncSingleNode(ctx context.Context, node *domain.Node, accountCounts map[uint]accountRepo.NodeAccountCount) {
	log := logger.GetLogger().WithField("node", node.Name)

	// Variables for SSE payload
	var (
		cpu, mem, disk                                                 float64
		up, down, tcp, udp, fd, memUsed, memTotal, diskUsed, diskTotal uint64
		sysUp, xrayPID, procUptime                                     int64
		xrayStatus, agentVer, xrayVer                                  string
		loadAvg1, loadAvg5, loadAvg15                                  float64
	)

	// 1. Collect System Stats & Xray Traffic
	var sysStats *domain.NodeStat
	var userTraffic map[string]int64  // email -> total bytes (up + down)
	var userUplink map[string]int64   // email -> upload bytes
	var userDownlink map[string]int64 // email -> download bytes
	var lastRecordTimestamp int64     // timestamp of the last buffered record processed
	persistedOutboundTraffic := false
	persistedNodeTraffic := false
	persistError := false

	client, err := u.getAgentClientForStats(ctx, node)
	if err != nil {
		log.Warn("Failed to connect to agent for stats collection")
		// Mark node as offline if it was online
		if node.IsOnline {
			if statusErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, false, time.Now()); statusErr != nil {
				log.WithError(statusErr).Warn("[SyncNodeStats] Failed to update node offline status")
			}
			u.nodeRepo.CreateUptimeEvent(ctx, &domain.NodeUptimeEvent{
				NodeID: node.ID, Status: "offline", Timestamp: time.Now(),
			})
		}
		return
	}

	// Detect offline→online transition for config re-push
	wasOffline := !node.IsOnline

	// Update node as online since we connected successfully
	if wasOffline {
		if updateErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, true, time.Now()); updateErr != nil {
			log.Warnf("Failed to update node online status: %v", updateErr)
		}
		u.nodeRepo.CreateUptimeEvent(ctx, &domain.NodeUptimeEvent{
			NodeID: node.ID, Status: "online", Timestamp: time.Now(),
		})
	}

	// A. System Stats
	agentSysStats, err := client.GetSystemStats(ctx)
	if err != nil {
		log.Warnf("Failed to get system stats: %v", err)
	} else {
		sysStats = &domain.NodeStat{
			NodeID:      node.ID,
			CPU:         agentSysStats.CPUUsagePercent,
			Memory:      agentSysStats.MemoryUsagePercent,
			Disk:        agentSysStats.DiskUsagePercent,
			UpSpeed:     agentSysStats.NetworkSentRate,
			DownSpeed:   agentSysStats.NetworkRecvRate,
			TcpCount:    agentSysStats.TcpCount,
			UdpCount:    agentSysStats.UdpCount,
			FdCount:     agentSysStats.FdCount,
			LoadAvg1:    agentSysStats.LoadAvg1,
			OnlineUsers: cache.GetNodeOnlineCount(node.ID),
		}
		// Populate SSE variables
		cpu = agentSysStats.CPUUsagePercent
		mem = agentSysStats.MemoryUsagePercent
		disk = agentSysStats.DiskUsagePercent
		up = agentSysStats.NetworkSentRate
		down = agentSysStats.NetworkRecvRate
		tcp = agentSysStats.TcpCount
		udp = agentSysStats.UdpCount
		fd = agentSysStats.FdCount
		memUsed = agentSysStats.MemoryUsedBytes / (1024 * 1024)
		memTotal = agentSysStats.MemoryTotalBytes / (1024 * 1024)
		diskUsed = agentSysStats.DiskUsedBytes / (1024 * 1024 * 1024)
		diskTotal = agentSysStats.DiskTotalBytes / (1024 * 1024 * 1024)
		sysUp = agentSysStats.SystemUptimeSeconds
		loadAvg1 = agentSysStats.LoadAvg1
		loadAvg5 = agentSysStats.LoadAvg5
		loadAvg15 = agentSysStats.LoadAvg15
	}

	// B. Xray Status & Version
	status, err := client.GetStatus(ctx)
	if err != nil {
		log.Debugf("Failed to get xray status: %v", err)
	} else {
		if status.XrayRunning {
			xrayStatus = "running"
		} else {
			xrayStatus = "stopped"
		}
		xrayPID = status.XrayPid
		procUptime = status.UptimeSeconds
		xrayVer = status.XrayVersion

		// Config drift detection & automatic re-push on reconnect
		needsConfigPush := false
		if wasOffline {
			log.Info("Node came back online, scheduling config re-push")
			needsConfigPush = true
		} else if status.ConfigHash != "" {
			// Compare agent's current config hash with last pushed hash
			u.configHashMu.RLock()
			expectedHash := u.lastPushedConfigHash[node.ID]
			u.configHashMu.RUnlock()

			if expectedHash != "" && expectedHash != status.ConfigHash {
				log.WithFields(map[string]interface{}{
					"agent_hash":    status.ConfigHash,
					"expected_hash": expectedHash,
				}).Warn("Config drift detected, scheduling config re-push")
				needsConfigPush = true
			}
		}

		if needsConfigPush {
			u.tryScheduleConfigPush(node)
		}
	}

	// C. Versions — cached with a TTL and force-refreshed on reconnect
	// (the agent may have been updated while offline). Versions only
	// change on deploys, so the previous per-tick GetVersion was waste.
	agentVer, cachedXrayVer := u.agentVersions(ctx, client, node.ID, wasOffline)
	if xrayVer == "" {
		xrayVer = cachedXrayVer
	}

	// Buffered traffic: retrieve time-bucketed records from the agent
	var bufferedStats *agent.BufferedTrafficStats
	if xrayStatus == "running" || xrayStatus == "" {
		bufferedStats, err = client.GetBufferedTraffic(ctx)
		if err != nil {
			log.Debugf("Failed to get buffered traffic (non-critical): %v", err)
		}
	}

	// trafficSubs: per-email subscription cache, populated lazily once
	// userTraffic is known. Avoids N round-trips for the three per-email loops.
	var trafficSubs map[string]*subDomain.Subscription

	// Inbound tags with any traffic this cycle. Per-inbound totals
	// tell us which inbounds saw traffic; xray's per-email stats
	// can't attribute bytes to a specific inbound on multi-inbound emails.
	activeInboundTags := make(map[string]bool)

	if bufferedStats != nil && len(bufferedStats.Records) > 0 {
		userTraffic = make(map[string]int64)
		userUplink = make(map[string]int64)
		userDownlink = make(map[string]int64)

		// Per-day user traffic for retroactive daily usage attribution
		// dayUserTraffic[dateStr][email] = combined bytes (kept for fallback logging)
		dayUserTraffic := make(map[string]map[string]int64)
		dayUserUplink := make(map[string]map[string]int64)
		dayUserDownlink := make(map[string]map[string]int64)

		// Node/outbound totals aggregated across all buffered records so
		// persistence below is one UPDATE per target instead of one per record.
		var nodeUp, nodeDown int64
		dailyNodeTraffic := make(map[time.Time][2]int64) // UTC day -> {up, down}
		outboundTotals := make(map[string][2]int64)      // tag -> {up, down}

		for _, record := range bufferedStats.Records {
			for tag, bytes := range record.InboundUplink {
				if bytes > 0 {
					activeInboundTags[tag] = true
				}
			}
			for tag, bytes := range record.InboundDownlink {
				if bytes > 0 {
					activeInboundTags[tag] = true
				}
			}
			if record.Timestamp > lastRecordTimestamp {
				lastRecordTimestamp = record.Timestamp
			}

			recordDay := time.Unix(record.Timestamp, 0).UTC().Format("2006-01-02")

			// Aggregate user traffic
			for email, bytes := range record.UserUplink {
				userTraffic[email] += bytes
				userUplink[email] += bytes
				if dayUserTraffic[recordDay] == nil {
					dayUserTraffic[recordDay] = make(map[string]int64)
					dayUserUplink[recordDay] = make(map[string]int64)
					dayUserDownlink[recordDay] = make(map[string]int64)
				}
				dayUserTraffic[recordDay][email] += bytes
				dayUserUplink[recordDay][email] += bytes
			}
			for email, bytes := range record.UserDownlink {
				userTraffic[email] += bytes
				userDownlink[email] += bytes
				if dayUserTraffic[recordDay] == nil {
					dayUserTraffic[recordDay] = make(map[string]int64)
					dayUserUplink[recordDay] = make(map[string]int64)
					dayUserDownlink[recordDay] = make(map[string]int64)
				}
				dayUserTraffic[recordDay][email] += bytes
				dayUserDownlink[recordDay][email] += bytes
			}

			// Accumulate node-level and per-outbound traffic in memory;
			// flushed once after the record loop.
			if record.TotalUplink > 0 || record.TotalDownlink > 0 {
				nodeUp += record.TotalUplink
				nodeDown += record.TotalDownlink
				recordDate := time.Unix(record.Timestamp, 0).UTC().Truncate(24 * time.Hour)
				d := dailyNodeTraffic[recordDate]
				d[0] += record.TotalUplink
				d[1] += record.TotalDownlink
				dailyNodeTraffic[recordDate] = d
			}
			for tag, bytes := range record.OutboundUplink {
				if bytes > 0 {
					o := outboundTotals[tag]
					o[0] += bytes
					outboundTotals[tag] = o
				}
			}
			for tag, bytes := range record.OutboundDownlink {
				if bytes > 0 {
					o := outboundTotals[tag]
					o[1] += bytes
					outboundTotals[tag] = o
				}
			}
		}

		// Flush aggregated node totals: one AddNodeTraffic per pass, one
		// AddNodeDailyTraffic per distinct UTC day (normally one).
		if nodeUp > 0 || nodeDown > 0 {
			if err := u.nodeRepo.AddNodeTraffic(ctx, node.ID, nodeUp, nodeDown); err != nil {
				log.Warnf("Failed to accumulate node traffic: %v", err)
				persistError = true
			} else {
				persistedNodeTraffic = true
			}
			for recordDate, d := range dailyNodeTraffic {
				if err := u.nodeRepo.AddNodeDailyTraffic(ctx, node.ID, recordDate, d[0], d[1]); err != nil {
					log.Warnf("Failed to record daily traffic: %v", err)
					persistError = true
				} else {
					persistedNodeTraffic = true
				}
			}
		}

		// Flush aggregated outbound totals: one UPDATE per tag.
		for tag, o := range outboundTotals {
			if err := u.nodeRepo.AddOutboundTraffic(ctx, node.ID, tag, o[0], o[1]); err != nil {
				log.Warnf("Failed to accumulate outbound %s traffic: %v", tag, err)
				persistError = true
			} else {
				persistedOutboundTraffic = true
			}
		}

		// Pre-fetch subscriptions for all emails with traffic this
		// cycle; one IN(...) query serves daily-attribution + persist.
		if len(userTraffic) > 0 {
			emails := make([]string, 0, len(userTraffic))
			for email := range userTraffic {
				emails = append(emails, email)
			}
			if subs, err := u.subRepo.FindByConfigEmails(ctx, emails); err != nil {
				log.WithError(err).Warn("FindByConfigEmails batch failed; per-email DB fallback will still run")
			} else {
				trafficSubs = subs
			}
		}

		// Retroactive daily usage attribution: distribute traffic
		// to the correct days. Uses trafficSubs so each email
		// resolves via a map hit instead of a DB round-trip.
		for dateStr, emailTraffic := range dayUserTraffic {
			date, parseErr := time.Parse("2006-01-02", dateStr)
			if parseErr != nil {
				log.Warnf("Failed to parse date %s for daily usage: %v", dateStr, parseErr)
				continue
			}
			upByEmail := dayUserUplink[dateStr]
			dnByEmail := dayUserDownlink[dateStr]
			for email := range emailTraffic {
				sub, ok := trafficSubs[email]
				if !ok {
					continue
				}
				up := upByEmail[email]
				dn := dnByEmail[email]
				if up == 0 && dn == 0 {
					continue
				}
				if err := u.subRepo.AddDailyUsageSplit(ctx, sub.ID, date, up, dn); err != nil {
					log.WithField("email", email).Warnf("Failed to add daily usage split for %s: %v", dateStr, err)
					persistError = true
				}
			}
		}
	}

	// Starlink stats collection (if enabled on this node)
	u.syncStarlinkStats(ctx, node, client)

	client.Close()

	// 2. Save System Stats
	if sysStats != nil {
		if err := u.nodeRepo.CreateNodeStat(ctx, sysStats); err != nil {
			log.Warnf("Failed to save node stats: %v", err)
		}
	}

	// WG per-peer attribution: synthetic emails (wg:<tag>:<ip>) -> peer/sub.
	// Feeds the sub's quota + per-device counters. The main loop below skips
	// these (no config-email match), so no double count. No daily split yet.
	if u.wgPeerSource != nil && len(userTraffic) > 0 {
		peersByTag := map[string][]WGRenderPeer{}
		for _, in := range node.Inbounds {
			if !strings.EqualFold(in.Protocol, "wireguard") {
				continue
			}
			if ps, err := u.wgPeerSource.ActivePeersByInbound(ctx, in.ID); err == nil {
				peersByTag[in.Tag] = ps
			}
		}
		wgIndex := buildWGIndex(peersByTag)
		for email := range userTraffic {
			if ref, ok := wgIndex[email]; ok {
				u.persistWGPeerTraffic(ctx, ref, userTraffic[email], userUplink[email], userDownlink[email], log)
			}
		}
	}

	// 3. Process User Traffic
	persistedTraffic := false
	if len(userTraffic) > 0 {
		// Account attribution index: one projection query replaces the old
		// per-(email, inbound) FindByEmailAndInbound lookups. nil map (load
		// failure or no repo wired) skips account attribution this cycle;
		// subscription-level usage above still persists.
		var accountRefs map[string]map[uint]uint // email -> inboundID -> accountID
		if u.accountRepo != nil && len(node.Inbounds) > 0 {
			if refs, refErr := u.accountRepo.ListTrafficRefsByNode(ctx, node.ID); refErr != nil {
				log.WithError(refErr).Warn("ListTrafficRefsByNode failed; skipping account attribution this cycle")
			} else {
				accountRefs = make(map[string]map[uint]uint, len(refs))
				for _, ref := range refs {
					byInbound := accountRefs[ref.Email]
					if byInbound == nil {
						byInbound = make(map[uint]uint)
						accountRefs[ref.Email] = byInbound
					}
					byInbound[ref.InboundID] = ref.ID
				}
			}
		}

		for email, bytes := range userTraffic {
			if bytes <= 0 {
				continue
			}

			// Use pre-fetched batch; per-email fallback if batch failed.
			var sub *subDomain.Subscription
			if trafficSubs != nil {
				sub = trafficSubs[email]
				if sub == nil {
					log.WithField("email", email).Debug("Traffic for unknown user")
					continue
				}
			} else {
				var err error
				sub, err = u.subRepo.FindByConfigEmail(ctx, email)
				if err != nil {
					log.WithField("email", email).Debug("Traffic for unknown user")
					continue
				}
			}

			// All subscription counters (used/lifetime totals, up/down
			// splits, last_active_at) in one UPDATE.
			now := time.Now()
			if err := u.subRepo.AddUsageDelta(ctx, sub.ID, userUplink[email], userDownlink[email], now); err != nil {
				log.WithField("email", email).Warnf("Failed to add usage delta: %v", err)
				persistError = true
			}

			// Update account data usage. Equal-split bytes across accounts
			// on inbounds that saw traffic (xray's per-email stats can't
			// attribute bytes to a specific inbound).
			var matched []uint
			if byInbound := accountRefs[email]; byInbound != nil {
				for _, inbound := range node.Inbounds {
					if !activeInboundTags[inbound.Tag] {
						continue
					}
					if accountID, ok := byInbound[inbound.ID]; ok {
						matched = append(matched, accountID)
					}
				}
			}

			if len(matched) > 0 {
				share := bytes / int64(len(matched))
				for _, accountID := range matched {
					if err := u.accountRepo.AddDataUsed(ctx, accountID, share); err != nil {
						log.WithField("email", email).Warnf("Failed to update account data usage: %v", err)
						persistError = true
					}
					if err := u.accountRepo.UpdateLastActive(ctx, accountID, now); err != nil {
						log.WithField("email", email).Warnf("Failed to update account last active: %v", err)
					}
				}
			}
		}

		log.Infof("Synced traffic for %d users", len(userTraffic))
		if !persistError {
			persistedTraffic = true
		}
	}

	// Online detection via GetAllUsersOnlineIPs. Empty IP list = XHTTP
	// ghost session (Xray's online counter lingers after disconnect);
	// clear those from nodeUsers so per-node counts match the
	// dashboard's userIPs-derived count. Runs on its own cadence — the
	// panel's online view refreshes at ~15s, so a faster sweep is waste.
	if u.statsCadenceDue(&u.lastOnlineIPsAt, node.ID, onlineIPsSyncInterval) {
		client, err := u.getAgentClientForStats(ctx, node)
		if err == nil {
			if bulkIPs, bulkErr := client.GetAllUsersOnlineIPs(ctx); bulkErr == nil {
				emails := make([]string, 0, len(bulkIPs))
				for email, ips := range bulkIPs {
					if len(ips) == 0 {
						cache.ClearNodeOnlineUser(node.ID, email)
						continue
					}
					emails = append(emails, email)
				}
				if len(emails) > 0 {
					cache.SetNodeOnlineUsers(node.ID, emails)
					cache.SetOnlineUsers(emails)
				}

				// Pre-fetch subscriptions for every online email in
				// one query so the IP upsert loop below doesn't turn
				// into an N+1.
				onlineSubs, onlineSubsErr := u.subRepo.FindByConfigEmails(ctx, emails)
				if onlineSubsErr != nil {
					log.WithError(onlineSubsErr).Warn("FindByConfigEmails batch failed for online IP persistence")
					onlineSubs = nil
				}

				// Accumulate IP upserts into one bulk write to avoid
				// per-(email,IP) round-trips on busy nodes.
				var ipRecords []subRepo.SubscriptionIPRecord
				for email, ips := range bulkIPs {
					cache.SetUserOnlineIPs(email, ips)
					if u.subIPRepo != nil && len(ips) > 0 {
						var subID uint
						if onlineSubs != nil {
							sub, ok := onlineSubs[email]
							if !ok {
								continue
							}
							subID = sub.ID
						} else {
							sub, subErr := u.subRepo.FindByConfigEmail(ctx, email)
							if subErr != nil {
								continue
							}
							subID = sub.ID
						}
						for ip := range ips {
							ipRecords = append(ipRecords, subRepo.SubscriptionIPRecord{
								SubscriptionID: subID,
								IP:             ip,
								NodeID:         node.ID,
							})
						}
					}
				}
				if u.subIPRepo != nil && len(ipRecords) > 0 {
					if err := u.subIPRepo.BulkUpsertSubscriptionIPs(ctx, ipRecords); err != nil {
						log.WithError(err).Warnf("Failed to bulk upsert %d IPs", len(ipRecords))
					}
				}
			} else {
				log.WithError(bulkErr).Debug("GetAllUsersOnlineIPs failed; leaving online cache untouched this cycle")
			}
			client.Close()
		}
	}

	// Acknowledge buffered traffic after successful persist
	if !persistError && (persistedTraffic || persistedOutboundTraffic || persistedNodeTraffic) && lastRecordTimestamp > 0 {
		client, err := u.getAgentClientForStats(ctx, node)
		if err == nil {
			ackErr := client.AckBufferedTraffic(ctx, lastRecordTimestamp)
			if ackErr != nil {
				log.Warnf("Failed to ack buffered traffic (agent will re-send on next sync): %v", ackErr)
			}
			client.Close()
		}
	}

	// 4. Access Log Summary: fetch buffered hourly summaries and persist.
	// The summaries are hourly buckets, so this runs on the slower cadence
	// rather than once per tick. Braces scope the client/resp locals.
	if u.statsCadenceDue(&u.lastAccessLogAt, node.ID, accessLogSyncInterval) {
		client, err := u.getAgentClientForStats(ctx, node)
		if err == nil {
			resp, err := client.GetBufferedAccessLogSummary(ctx)
			if err != nil {
				log.Debugf("Failed to get buffered access log summary (non-critical): %v", err)
			} else if resp != nil && len(resp.Entries) > 0 {
				// Batch-resolve emails → subscription IDs so we can
				// denormalise subscription_id into the summary row.
				summaryEmails := make([]string, 0, len(resp.Entries))
				emailSeen := make(map[string]struct{}, len(resp.Entries))
				for _, entry := range resp.Entries {
					if entry.Email == "" {
						continue
					}
					if _, dup := emailSeen[entry.Email]; dup {
						continue
					}
					emailSeen[entry.Email] = struct{}{}
					summaryEmails = append(summaryEmails, entry.Email)
				}
				var emailToSub map[string]*subDomain.Subscription
				if len(summaryEmails) > 0 {
					if m, lookupErr := u.subRepo.FindByConfigEmails(ctx, summaryEmails); lookupErr == nil {
						emailToSub = m
					} else {
						log.WithError(lookupErr).Warn("FindByConfigEmails failed for access log summary; rows will have subscription_id=0")
					}
				}

				var maxTS int64
				var minFailedTS int64
				persistedAny := false
				hasFailure := false
				for _, entry := range resp.Entries {
					domainsJSON, _ := json.Marshal(entry.Domains)
					rejDomainsJSON, _ := json.Marshal(entry.RejectedDomains)
					ipsJSON, _ := json.Marshal(entry.SourceIps)
					var subID uint
					if emailToSub != nil {
						if sub, ok := emailToSub[entry.Email]; ok && sub != nil {
							subID = sub.ID
						}
					}
					summary := &domain.AccessLogSummary{
						NodeID:          node.ID,
						Email:           entry.Email,
						SubscriptionID:  subID,
						HourTime:        time.Unix(entry.HourTimestamp, 0).UTC(),
						AcceptedCount:   entry.AcceptedCount,
						RejectedCount:   entry.RejectedCount,
						TcpCount:        entry.TcpCount,
						UdpCount:        entry.UdpCount,
						TopDomains:      string(domainsJSON),
						RejectedDomains: string(rejDomainsJSON),
						SourceIPs:       string(ipsJSON),
					}
					if err := u.nodeRepo.UpsertAccessLogSummary(ctx, summary); err != nil {
						log.Warnf("Failed to upsert access log summary for %s: %v", entry.Email, err)
						if !hasFailure || entry.HourTimestamp < minFailedTS {
							minFailedTS = entry.HourTimestamp
							hasFailure = true
						}
					} else {
						persistedAny = true
						if entry.HourTimestamp > maxTS {
							maxTS = entry.HourTimestamp
						}
					}
				}
				if persistedAny && maxTS > 0 {
					ackTS := maxTS
					if hasFailure {
						// Only ack up to the hour before the first failure
						// to prevent data loss from non-contiguous persistence gaps.
						safeTS := minFailedTS - 3600
						if safeTS < ackTS {
							ackTS = safeTS
						}
					}
					if ackTS > 0 {
						// Push current hub-side runtime config alongside Ack
						// so the agent's aggregator stays in sync (grace
						// window + per-row top-N caps).
						ackCfg := u.readAccessLogAckConfig(ctx)
						if ackErr := client.AckBufferedAccessLogSummary(ctx, ackTS, ackCfg); ackErr != nil {
							log.Warnf("Failed to ack access log summary: %v", ackErr)
						} else {
							// Ack succeeded — stamp last-synced so the
							// freshness pill in the panel updates.
							if markErr := u.nodeRepo.MarkAccessLogSynced(ctx, node.ID, time.Now().UTC()); markErr != nil {
								log.WithError(markErr).Debug("Failed to stamp last_access_log_synced_at")
							}
						}
					}
				}
				log.Debugf("Synced %d access log summary entries", len(resp.Entries))
			}
			client.Close()
		}
	}

	// Re-read node to get updated cumulative traffic totals for SSE payload
	var totalUp, totalDown int64
	if freshNode, err := u.nodeRepo.GetNode(ctx, node.ID); err == nil {
		totalUp = freshNode.TotalUplink
		totalDown = freshNode.TotalDownlink
	}

	// Always publish node stats event (even when xray is stopped or no traffic)
	u.eventBus.Publish(events.Event{
		Type:      events.EventNodeStatsUpdated,
		Timestamp: time.Now(),
		Payload: events.NodeStatsPayload{
			NodeID:         node.ID,
			NodeName:       node.Name,
			OnlineUsers:    cache.GetNodeOnlineCount(node.ID),
			TotalUplink:    totalUp,
			TotalDownlink:  totalDown,
			CPUPercent:     cpu,
			MemoryPercent:  mem,
			DiskPercent:    disk,
			MemoryUsedMB:   memUsed,
			MemoryTotalMB:  memTotal,
			DiskUsedGB:     diskUsed,
			DiskTotalGB:    diskTotal,
			UpSpeed:        up,
			DownSpeed:      down,
			TcpCount:       tcp,
			UdpCount:       udp,
			FdCount:        fd,
			Uptime:         uint64(procUptime),
			SystemUptime:   sysUp,
			XrayStatus:     xrayStatus,
			XrayPID:        xrayPID,
			AgentVersion:   agentVer,
			XrayVersion:    xrayVer,
			LoadAvg1:       loadAvg1,
			LoadAvg5:       loadAvg5,
			LoadAvg15:      loadAvg15,
			TotalAccounts:  accountCounts[node.ID].Total,
			ActiveAccounts: accountCounts[node.ID].Active,
		},
	})
}

func (u *nodeUsecase) GetNodeStatsHistory(ctx context.Context, nodeID uint, limit int) ([]*domain.NodeStat, error) {
	return u.nodeRepo.GetNodeStatsHistory(ctx, nodeID, limit)
}

// GetNodesStatsHistoryBulk: per-node history for sparklines. Repo passthrough.
func (u *nodeUsecase) GetNodesStatsHistoryBulk(ctx context.Context, nodeIDs []uint, limit int) (map[uint][]*domain.NodeStat, error) {
	return u.nodeRepo.GetNodesStatsHistoryBulk(ctx, nodeIDs, limit)
}

func (u *nodeUsecase) GetNodeDailyTraffic(ctx context.Context, nodeID uint, days int) ([]*domain.NodeDailyTraffic, error) {
	return u.nodeRepo.GetNodeDailyTraffic(ctx, nodeID, days)
}

func (u *nodeUsecase) GetNodeUptimeEvents(ctx context.Context, nodeID uint, hours int) ([]*domain.NodeUptimeEvent, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	return u.nodeRepo.GetUptimeEvents(ctx, nodeID, since)
}

// readAccessLogAckConfig pulls all hub-configured aggregator knobs
// (grace window + per-row top-N caps) into one struct. Zero fields
// mean "agent keeps its built-in default for that field".
func (u *nodeUsecase) readAccessLogAckConfig(ctx context.Context) agent.AccessLogAckConfig {
	return agent.AccessLogAckConfig{
		GracePeriod:               u.readAccessLogGracePeriod(ctx),
		MaxDomainsPerHour:         u.readAccessLogCap(ctx, "access_log_max_domains_per_hour"),
		MaxRejectedDomainsPerHour: u.readAccessLogCap(ctx, "access_log_max_rejected_domains_per_hour"),
		MaxSourceIPsPerHour:       u.readAccessLogCap(ctx, "access_log_max_source_ips_per_hour"),
	}
}

// readAccessLogGracePeriod resolves the hub-configured grace window
// from settings. Missing / malformed / out-of-range → 0 (the agent
// keeps its built-in default). Clamps to [0, 1440] minutes.
func (u *nodeUsecase) readAccessLogGracePeriod(ctx context.Context) time.Duration {
	if u.settingUC == nil {
		return 0
	}
	raw, err := u.settingUC.GetByKey(ctx, "access_log_grace_minutes")
	if err != nil || raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	if n > 1440 {
		n = 1440
	}
	return time.Duration(n) * time.Minute
}

// readAccessLogCap resolves one of the per-row top-N caps. Missing /
// malformed / non-positive → 0 (agent keeps its default for that cap).
// Clamps to [1, 500].
func (u *nodeUsecase) readAccessLogCap(ctx context.Context, key string) int32 {
	if u.settingUC == nil {
		return 0
	}
	raw, err := u.settingUC.GetByKey(ctx, key)
	if err != nil || raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	if n > 500 {
		n = 500
	}
	return int32(n)
}
