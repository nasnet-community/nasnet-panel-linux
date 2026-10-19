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
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
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
		// The sweep read this node moments ago; reuse that instead of four
		// more agent RPCs per viewer.
		if snap, _, ok := u.liveStats.get(n.ID); ok && snapshotIsFresh(snap) {
			results[n.ID] = &NodeStatsResult{Stats: &snap}
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

	stored := func(err error) (*domain.HostInfo, error) {
		if node.LastHostInfo == nil {
			return nil, err
		}
		cp := *node.LastHostInfo
		cp.Stale = true
		return &cp, nil
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return stored(err)
	}
	defer closeAgentClient(client)

	info, err := client.GetHostInfo(ctx)
	if err != nil {
		return stored(err)
	}

	now := time.Now()
	out := &domain.HostInfo{
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
		FetchedAt:            &now,
	}
	// Keep the copy fresh enough to describe the machine when it's offline,
	// without a write on every read.
	if prev := node.LastHostInfo; prev == nil || prev.FetchedAt == nil || hostInfoChanged(prev, out) || now.Sub(*prev.FetchedAt) > 6*time.Hour {
		if err := u.nodeRepo.SaveNodeHostInfo(ctx, node.ID, out); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", node.ID).Warn("[GetNodeHostInfo] Failed to store host info")
		}
	}
	return out, nil
}

func hostInfoChanged(a, b *domain.HostInfo) bool {
	x, y := *a, *b
	x.FetchedAt, y.FetchedAt, x.Stale, y.Stale = nil, nil, false, false
	return x != y
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

	client, clientErr := u.getAgentClientForStats(ctx, node)
	if clientErr != nil {
		return nil, fmt.Errorf("node stats unavailable: %w", clientErr)
	}
	if client == nil {
		return nil, fmt.Errorf("node stats unavailable: no agent client")
	}
	defer closeAgentClient(client)

	// Four independent RPCs — fan out so the round-trip cost is
	// paid once, not four times. System and status reads must succeed;
	// versions and the live traffic delta are best-effort.
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

	// A failed read is unavailable, never a successful zero sample. In
	// particular, bulk requests can exhaust their deadline here.
	if sysErr != nil {
		return nil, fmt.Errorf("system stats unavailable: %w", sysErr)
	}
	if sysStats == nil {
		return nil, fmt.Errorf("system stats unavailable: empty agent response")
	}
	if statusErr != nil {
		return nil, fmt.Errorf("node status unavailable: %w", statusErr)
	}
	if status == nil {
		return nil, fmt.Errorf("node status unavailable: empty agent response")
	}

	stats.CPUPercent = sysStats.CPUUsagePercent
	stats.MemoryPercent = sysStats.MemoryUsagePercent
	stats.MemoryUsedMB = sysStats.MemoryUsedBytes / (1024 * 1024)
	stats.MemoryTotalMB = sysStats.MemoryTotalBytes / (1024 * 1024)
	stats.DiskPercent = sysStats.DiskUsagePercent
	stats.DiskUsedGB = sysStats.DiskUsedBytes / (1024 * 1024 * 1024)
	stats.DiskTotalGB = sysStats.DiskTotalBytes / (1024 * 1024 * 1024)
	stats.UpSpeed = sysStats.NetworkSentRate
	stats.DownSpeed = sysStats.NetworkRecvRate
	stats.TcpCount = sysStats.TcpCount
	stats.UdpCount = sysStats.UdpCount
	stats.FdCount = sysStats.FdCount
	stats.SystemUptime = sysStats.SystemUptimeSeconds
	stats.LoadAvg1 = sysStats.LoadAvg1
	stats.LoadAvg5 = sysStats.LoadAvg5
	stats.LoadAvg15 = sysStats.LoadAvg15

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

	sampledAt := telemetrySampleTime(sysStats.CollectedAtUnixMs, time.Now())
	stats.SampledAt = &sampledAt
	var age int64
	stats.ReadingAgeMs = &age
	u.statsCache.Put(node.ID, stats)
	counts := &accountRepo.NodeAccountCount{Total: stats.TotalAccounts, Active: stats.ActiveAccounts}
	u.liveStats.record(node.ID, liveSample{stats: *stats, hasSystem: true, sampledAt: sampledAt, hasStatus: true, configHash: status.ConfigHash, counts: counts})
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

// agentVersionCacheTTL: agent/xray versions only change on deploys, so
// the stats sweep refreshes them at this cadence instead of every tick.
const agentVersionCacheTTL = 10 * time.Minute

// agentVersionCacheEntry holds one node's cached version pair.
type agentVersionCacheEntry struct {
	agentVersion string
	xrayVersion  string
	build        string // SHA-256 of the running agent binary; dev builds share a version
	fetchedAt    time.Time
}

// agentVersions returns (agentVersion, xrayVersion) for a node, hitting
// the agent's GetVersion RPC only when the cache is stale or forceRefresh
// is set (reconnect path). On a failed refresh the stale pair is served —
// a wrong-for-minutes version beats an empty one in the panel.
// forgetAgentVersion drops a node's cached versions so the next sweep asks
// the agent again (after an agent update).
func (u *nodeUsecase) forgetAgentVersion(nodeID uint) {
	u.versionCacheMu.Lock()
	delete(u.versionCache, nodeID)
	u.versionCacheMu.Unlock()
}

// notePushedAgent: the node now runs the build just pushed. The next sweep asks
// it again; until then the panel doesn't offer the same update twice.
func (u *nodeUsecase) notePushedAgent(nodeID uint, build string) {
	u.versionCacheMu.Lock()
	defer u.versionCacheMu.Unlock()
	if u.versionCache == nil {
		u.versionCache = make(map[uint]agentVersionCacheEntry)
	}
	u.versionCache[nodeID] = agentVersionCacheEntry{build: build} // a zero fetchedAt is stale at once
}

// agentBuild is the build the node last said it runs ("" when unknown).
func (u *nodeUsecase) agentBuild(nodeID uint) string {
	u.versionCacheMu.Lock()
	defer u.versionCacheMu.Unlock()
	return u.versionCache[nodeID].build
}

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
	if sum, err := client.GetSelfChecksum(ctx); err == nil && sum != nil {
		entry.build = sum.Checksum
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
	// onlineIPsSyncInterval: the panel's online-users view refreshes at
	// ~15s, so sweeping IPs faster than that is wasted work.
	onlineIPsSyncInterval = 15 * time.Second
	// accessLogSyncInterval: summaries are hourly buckets; 60s keeps the
	// panel fresh at 1/12th of the old per-tick fetch rate.
	accessLogSyncInterval = 60 * time.Second
	// heartbeatStatusFreshness: heartbeat pongs arrive every ~2s; a status
	// older than this means the stream is struggling — ask the agent then.
	heartbeatStatusFreshness = 10 * time.Second
	// bundleUnsupportedRetryTTL: how long to keep using individual RPCs
	// after an agent answered GetStatsBundle with Unimplemented, before
	// re-probing (picks up agents upgraded in place).
	bundleUnsupportedRetryTTL = 10 * time.Minute
	// cadenceSlack absorbs the sub-tick drift between the scheduler's 5s
	// ticker and the cadence stamps taken inside a sync pass. Without it an
	// interval that is a whole multiple of the tick (15s on a 5s grid) is a
	// coin flip each cycle: lose it and the cadence slips a whole tick.
	cadenceSlack = 1 * time.Second
)

// statsCadenceDue reports whether interval (less cadenceSlack) has elapsed
// since the node's last stamped run in m, stamping now when due. m points at
// one of the usecase's cadence maps (lazy-initialised here so test fixtures
// built as bare struct literals keep working); access is serialized by
// statsCadenceMu.
func (u *nodeUsecase) statsCadenceDue(m *map[uint]time.Time, nodeID uint, interval time.Duration) bool {
	u.statsCadenceMu.Lock()
	defer u.statsCadenceMu.Unlock()
	if *m == nil {
		*m = make(map[uint]time.Time)
	}
	if interval > cadenceSlack {
		interval -= cadenceSlack
	}
	if time.Since((*m)[nodeID]) < interval {
		return false
	}
	(*m)[nodeID] = time.Now()
	return true
}

// tryStatsBundle attempts the aggregated stats RPC. Returns nil when the
// agent predates the RPC (backing off bundleUnsupportedRetryTTL before
// re-probing) or on a transient error (falls back for this cycle only).
func (u *nodeUsecase) tryStatsBundle(ctx context.Context, client agent.NodeClient, nodeID uint, includeOnlineIPs bool) *agent.StatsBundle {
	u.statsCadenceMu.Lock()
	lastFail, failed := u.bundleUnsupportedAt[nodeID]
	u.statsCadenceMu.Unlock()
	if failed && time.Since(lastFail) < bundleUnsupportedRetryTTL {
		return nil
	}

	bundle, err := client.GetStatsBundle(ctx, includeOnlineIPs)
	if err != nil {
		if isUnimplementedErr(err) {
			u.statsCadenceMu.Lock()
			if u.bundleUnsupportedAt == nil {
				u.bundleUnsupportedAt = make(map[uint]time.Time)
			}
			u.bundleUnsupportedAt[nodeID] = time.Now()
			u.statsCadenceMu.Unlock()
		}
		logger.GetLogger().WithField("node_id", nodeID).WithError(err).
			Debug("[SyncNodeStats] stats bundle unavailable, using individual RPCs")
		return nil
	}
	return bundle
}

// isUnimplementedErr matches both transports' "agent too old for this RPC"
// signals: gRPC Unimplemented (direct mode), the reverse handler's
// unknown-method error string, and an Unimplemented status the reverse
// tunnel relays as text (the handler dispatches the name, the server
// predates it).
func isUnimplementedErr(err error) bool {
	if grpcstatus.Code(err) == codes.Unimplemented {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unknown method") || strings.Contains(msg, "code = Unimplemented")
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
	nodes = withoutLite(nodes) // MTProto nodes book their own usage (IngestTraffic)

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

// syncSingleNode collects + persists stats for one node. Errors are
// swallowed — a single bad node must not abort the batch. accountCounts
// may be nil (single-node path); totals publish as zero then.
func (u *nodeUsecase) syncSingleNode(ctx context.Context, node *domain.Node, accountCounts map[uint]accountRepo.NodeAccountCount) {
	log := logger.GetLogger().WithField("node", node.Name)

	release, err := u.acquireStatsSync(ctx, node.ID)
	if err != nil {
		log.WithError(err).Debug("Stats sync canceled while waiting for node")
		return
	}
	defer release()

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

	client, err := u.getAgentClientForStats(ctx, node)
	if err != nil {
		log.Warn("Failed to connect to agent for stats collection")
		u.ObserveNodeHealth(ctx, node, false, domain.NodeOfflineReasonAgentUnreachable, err.Error())
		return
	}
	// One client serves the whole pass (traffic, online IPs, acks, access
	// log summary) — direct mode multiplexes over the pooled HTTP/2 conn.
	defer client.Close()

	// Detect offline→online transition for config re-push
	wasOffline := !node.IsOnline

	// Reachability is decided below, by an RPC that actually answered —
	// never by the fact that we hold a client. In direct mode the pool
	// hands back a cached conn for up to its TTL after the agent dies, so
	// "got a client" proves nothing. Reporting online here also cleared
	// the offline candidate the heartbeat loop had just set, and since
	// this sweep runs every 5s against a 30s debounce, the window could
	// never elapse: a dead node stayed pinned online until the pooled
	// conn expired.
	reachable := false
	var unreachableErr error

	// Cadence gates: online IPs feed a panel refreshing at 15s; access-log
	// summaries are hourly buckets, so 60s keeps them fresh at a fraction
	// of the old per-tick cost. First pass after startup is always due.
	collectOnlineIPs := u.statsCadenceDue(&u.lastOnlineIPsAt, node.ID, onlineIPsSyncInterval)
	collectAccessLog := !node.IsStealth && u.statsCadenceDue(&u.lastAccessLogAt, node.ID, accessLogSyncInterval)

	// Status source №1: the heartbeat pong already carries a NodeStatus
	// every ~2s — reuse it when fresh instead of asking the agent again.
	var status *pb.NodeStatus
	if u.heartbeatMgr != nil {
		if st, at, ok := u.heartbeatMgr.GetLastStatus(node.ID); ok && time.Since(at) < heartbeatStatusFreshness {
			status = st
		}
	}

	// Collect system stats + status + buffered traffic (+ online IPs when
	// due) — one bundle RPC on current agents, individual RPCs on older ones.
	var agentSysStats *agent.SystemStats
	var bufferedStats *agent.BufferedTrafficStats
	var bulkIPs map[string]map[string]int64 // nil = not collected this cycle
	var onlineIPsCollectedAt int64

	if bundle := u.tryStatsBundle(ctx, client, node.ID, collectOnlineIPs); bundle != nil {
		reachable = true
		agentSysStats = bundle.System
		// The bundle's status is fresher than the heartbeat copy; the
		// heartbeat copy remains the fallback when the agent's status
		// collector failed inside the bundle.
		if bundle.Status != nil {
			status = bundle.Status
		}
		bufferedStats = bundle.Buffered
		if collectOnlineIPs {
			bulkIPs = bundle.OnlineIPs
			onlineIPsCollectedAt = bundle.OnlineIPsCollectedAtUnixMs
		}
	} else {
		// A. System Stats
		agentSysStats, err = client.GetSystemStats(ctx)
		if err != nil {
			log.Warnf("Failed to get system stats: %v", err)
			agentSysStats = nil
			unreachableErr = err
		} else {
			reachable = true
		}
		// B. Xray Status (only when the heartbeat copy was stale/missing)
		if status == nil {
			status, err = client.GetStatus(ctx)
			if err != nil {
				log.Debugf("Failed to get xray status: %v", err)
				status = nil
				if unreachableErr == nil {
					unreachableErr = err
				}
			} else {
				reachable = true
			}
		}
		// Buffered traffic: pull unconditionally whenever the agent is
		// reachable — this stream also carries non-Xray core (VPN tunnel)
		// per-email deltas, so gating it on xrayStatus starved usage/quota
		// on cores-only nodes where Xray is stopped. The agent returns an
		// empty set when buffering is disabled.
		bufferedStats, err = client.GetBufferedTraffic(ctx)
		if err != nil {
			log.Debugf("Failed to get buffered traffic (non-critical): %v", err)
			bufferedStats = nil
			if unreachableErr == nil {
				unreachableErr = err
			}
		} else {
			reachable = true
		}
		if collectOnlineIPs {
			var snapshot *agent.OnlineIPSnapshot
			snapshot, err = getOnlineIPSnapshot(ctx, client)
			if err != nil {
				log.WithError(err).Debug("GetAllUsersOnlineIPs failed; leaving online cache untouched this cycle")
				bulkIPs = nil
			} else if snapshot != nil {
				bulkIPs, onlineIPsCollectedAt = snapshot.Users, snapshot.CollectedAtUnixMs
			}
		}
	}

	// Now we know whether anything on the other end answered. An agent that
	// took none of these calls is unreachable however healthy its cached
	// connection looks; with nothing collected, the rest of this pass would
	// only publish zeroes.
	if !reachable {
		detail := "agent answered no stats RPC"
		if unreachableErr != nil {
			detail = unreachableErr.Error()
		}
		u.ObserveNodeHealth(ctx, node, false, domain.NodeOfflineReasonAgentUnreachable, detail)
		return
	}
	u.ObserveNodeHealth(ctx, node, true, domain.NodeOnlineReasonAgentReachable, "")

	if agentSysStats != nil {
		sysStats = &domain.NodeStat{
			NodeID: node.ID,
			// UTC: SQLite compares times as text (see GetNodeStatsSeries).
			CreatedAt:   telemetrySampleTime(agentSysStats.CollectedAtUnixMs, time.Now()).UTC(),
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

	// Xray status → SSE fields + config drift detection.
	if status != nil {
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
	// change on deploys, so the previous per-tick GetVersion RPC was waste.
	agentVer, cachedXrayVer := u.agentVersions(ctx, client, node.ID, wasOffline)
	if xrayVer == "" {
		xrayVer = cachedXrayVer
	}

	// Usage writes share one transaction. Acknowledge immediately after commit,
	// before optional stats/IP/log work can exhaust the remaining RPC deadline.
	if bufferedStats != nil && len(bufferedStats.Records) > 0 {
		through, err := u.persistBufferedTraffic(ctx, node, bufferedStats.Records)
		if err != nil {
			log.WithError(err).Warn("Traffic batch failed; leaving agent buffer unacknowledged")
		} else if through > 0 {
			if err := client.AckBufferedTraffic(ctx, through); err != nil {
				log.WithError(err).Warn("Traffic batch committed but acknowledgement failed")
			}
		}
	}
	u.syncStarlinkStats(ctx, node, client)
	if sysStats != nil {
		if err := u.nodeRepo.CreateNodeStat(ctx, sysStats); err != nil {
			log.WithError(err).Warn("Failed to save node stats")
		}
	}

	// Online detection from the online-IP sweep (bundle or fallback RPC,
	// collected above on the 15s cadence). Empty IP list = XHTTP ghost
	// session (Xray's online counter lingers after disconnect); clear those
	// from nodeUsers so per-node counts match the dashboard's userIPs-derived
	// count. nil = skipped or failed this cycle → leave the cache untouched.
	{
		if bulkIPs != nil {
			observedAt := telemetrySampleTime(onlineIPsCollectedAt, time.Now())
			// Hidden reverse users are tunnels, not customers online.
			u.observeReverseTunnels(ctx, node, bulkIPs, observedAt)
			observedAt = cache.ApplyNodeOnlineIPSnapshot(node.ID, bulkIPs, observedAt, onlineIPsCollectedAt)
			emails := make([]string, 0, len(bulkIPs))
			for email, ips := range bulkIPs {
				if len(ips) == 0 {
					continue
				}
				emails = append(emails, email)
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
							SeenAt:         observedAt,
						})
					}
				}
			}
			if u.subIPRepo != nil && len(ipRecords) > 0 {
				if err := u.subIPRepo.BulkUpsertSubscriptionIPs(ctx, ipRecords); err != nil {
					log.WithError(err).Warnf("Failed to bulk upsert %d IPs", len(ipRecords))
				}
			}
		}
	}

	// 4. Access Log Summary: fetch buffered hourly summaries and persist.
	// Runs on the accessLogSyncInterval cadence (entries are hourly buckets;
	// the old per-tick fetch was 12× the useful rate).
	if collectAccessLog {
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
				if domain.IsReverseEmail(entry.Email) {
					// A tunnel's hidden user isn't a customer: no history, but
					// the hour still counts as handled for the ack.
					persistedAny = true
					maxTS = max(maxTS, entry.HourTimestamp)
					continue
				}
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
	}

	// Re-read node to get updated cumulative traffic totals for SSE payload
	totalUp, totalDown := node.TotalUplink, node.TotalDownlink
	if freshNode, err := u.nodeRepo.GetNode(ctx, node.ID); err == nil {
		totalUp = freshNode.TotalUplink
		totalDown = freshNode.TotalDownlink
	}

	// Keep the latest reading for panel reads (GetNodeLiveStats) — the same
	// data the event below carries, merged with the previous pass.
	sample := liveSample{
		stats: NodeStats{
			TotalUplink:   totalUp,
			TotalDownlink: totalDown,
			OnlineUsers:   cache.GetNodeOnlineCount(node.ID),
			CPUPercent:    cpu,
			MemoryPercent: mem,
			MemoryUsedMB:  memUsed,
			MemoryTotalMB: memTotal,
			DiskPercent:   disk,
			DiskUsedGB:    diskUsed,
			DiskTotalGB:   diskTotal,
			UpSpeed:       up,
			DownSpeed:     down,
			TcpCount:      tcp,
			UdpCount:      udp,
			FdCount:       fd,
			XrayStatus:    xrayStatus,
			XrayRunning:   xrayStatus == "running",
			XrayPID:       xrayPID,
			ProcessUptime: procUptime,
			SystemUptime:  sysUp,
			LoadAvg1:      loadAvg1,
			LoadAvg5:      loadAvg5,
			LoadAvg15:     loadAvg15,
			AgentVersion:  agentVer,
			XrayVersion:   xrayVer,
		},
		hasSystem: agentSysStats != nil,
		hasStatus: status != nil,
	}
	var sampledAt *time.Time
	if sysStats != nil {
		sample.sampledAt = sysStats.CreatedAt
		at := sysStats.CreatedAt
		sampledAt = &at
	}
	if status != nil {
		sample.configHash = status.ConfigHash
	}
	if accountCounts != nil {
		c := accountCounts[node.ID]
		sample.counts = &c
	}
	u.liveStats.record(node.ID, sample)

	// Always publish node stats event (even when xray is stopped or no traffic)
	u.eventBus.Publish(events.Event{
		Type:      events.EventNodeStatsUpdated,
		Timestamp: time.Now(),
		Payload: events.NodeStatsPayload{
			NodeID:           node.ID,
			NodeName:         node.Name,
			StatsUnavailable: agentSysStats == nil,
			OnlineUsers:      cache.GetNodeOnlineCount(node.ID),
			TotalUplink:      totalUp,
			TotalDownlink:    totalDown,
			CPUPercent:       cpu,
			MemoryPercent:    mem,
			DiskPercent:      disk,
			MemoryUsedMB:     memUsed,
			MemoryTotalMB:    memTotal,
			DiskUsedGB:       diskUsed,
			DiskTotalGB:      diskTotal,
			UpSpeed:          up,
			DownSpeed:        down,
			TcpCount:         tcp,
			UdpCount:         udp,
			FdCount:          fd,
			Uptime:           uint64(procUptime),
			SystemUptime:     sysUp,
			XrayStatus:       xrayStatus,
			XrayPID:          xrayPID,
			AgentVersion:     agentVer,
			XrayVersion:      xrayVer,
			LoadAvg1:         loadAvg1,
			LoadAvg5:         loadAvg5,
			LoadAvg15:        loadAvg15,
			TotalAccounts:    accountCounts[node.ID].Total,
			ActiveAccounts:   accountCounts[node.ID].Active,
			SampledAt:        sampledAt,
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
