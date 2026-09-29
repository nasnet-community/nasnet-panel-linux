package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
)

// liveStatsFreshFor: a snapshot older than this no longer counts as a live
// reading. The sweep refreshes every 5s, so this allows two missed passes
// before reads go back to the agent (and the panel says "last reading").
const liveStatsFreshFor = 15 * time.Second

// liveStatsEntry is the latest sweep result for one node.
type liveStatsEntry struct {
	stats NodeStats
	// observedAt is when the hub received the system readings in stats, by
	// the hub's clock. Freshness is judged by this, never by SampledAt,
	// which comes from the agent's clock.
	observedAt time.Time
	// configHash is what the agent reported as its running config in the
	// latest sweep that had a status. Empty until one did.
	configHash string
}

// liveStatsStore keeps the stats sweep's latest result per node, so panel
// reads are served from memory instead of fresh agent RPCs per viewer. The
// sweep already collects the same data every tick and publishes it over SSE.
type liveStatsStore struct {
	mu sync.RWMutex
	m  map[uint]*liveStatsEntry
}

func newLiveStatsStore() *liveStatsStore {
	return &liveStatsStore{m: make(map[uint]*liveStatsEntry)}
}

// liveSample is one sweep's observation. Nil pointers mean "not collected
// this pass" and keep the previous value.
type liveSample struct {
	stats      NodeStats
	hasSystem  bool
	sampledAt  time.Time
	counts     *accountRepo.NodeAccountCount
	hasStatus  bool
	configHash string
}

// record merges a sample into the node's entry. A pass without system stats
// keeps the last good readings (and their sampled_at), so a flaky agent read
// never shows as zeroes.
func (s *liveStatsStore) record(nodeID uint, in liveSample) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.m[nodeID]
	// A sweep pass that started before a live read (e.g. the read right after
	// Stop) can finish after it; its older readings must not win.
	if prev != nil && in.hasSystem && prev.stats.SampledAt != nil && in.sampledAt.Before(*prev.stats.SampledAt) {
		return
	}
	next := in.stats
	if prev != nil {
		p := prev.stats
		if !in.hasSystem {
			copySystemFields(&next, &p)
			next.SampledAt = p.SampledAt
		}
		if in.counts == nil {
			next.TotalAccounts, next.ActiveAccounts = p.TotalAccounts, p.ActiveAccounts
		}
		if !in.hasStatus {
			next.XrayStatus, next.XrayRunning, next.XrayPID, next.ProcessUptime = p.XrayStatus, p.XrayRunning, p.XrayPID, p.ProcessUptime
		}
		if next.AgentVersion == "" {
			next.AgentVersion = p.AgentVersion
		}
		if next.XrayVersion == "" {
			next.XrayVersion = p.XrayVersion
		}
	}
	if in.hasSystem {
		at := in.sampledAt
		next.SampledAt = &at
	}
	if in.counts != nil {
		next.TotalAccounts, next.ActiveAccounts = in.counts.Total, in.counts.Active
	}
	next.StatsUnavailable = !in.hasSystem
	entry := &liveStatsEntry{stats: next}
	switch {
	case in.hasSystem:
		entry.observedAt = time.Now()
	case prev != nil:
		entry.observedAt = prev.observedAt
	}
	switch {
	case in.hasStatus:
		entry.configHash = in.configHash
	case prev != nil:
		entry.configHash = prev.configHash
	}
	s.m[nodeID] = entry
}

// get returns a copy of the node's latest stats and config hash. The copy
// carries ReadingAgeMs as of now; an entry that never had system readings
// (only status so far) counts as absent.
func (s *liveStatsStore) get(nodeID uint) (NodeStats, string, bool) {
	if s == nil {
		return NodeStats{}, "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[nodeID]
	if !ok || e.observedAt.IsZero() {
		return NodeStats{}, "", false
	}
	out := e.stats
	age := time.Since(e.observedAt).Milliseconds()
	out.ReadingAgeMs = &age
	return out, e.configHash, true
}

// configHash returns the agent's last reported config hash, even before any
// system readings arrived.
func (s *liveStatsStore) configHash(nodeID uint) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.m[nodeID]; ok {
		return e.configHash
	}
	return ""
}

func (s *liveStatsStore) forget(nodeID uint) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.m, nodeID)
	s.mu.Unlock()
}

func copySystemFields(dst, src *NodeStats) {
	dst.CPUPercent, dst.MemoryPercent, dst.DiskPercent = src.CPUPercent, src.MemoryPercent, src.DiskPercent
	dst.MemoryUsedMB, dst.MemoryTotalMB = src.MemoryUsedMB, src.MemoryTotalMB
	dst.DiskUsedGB, dst.DiskTotalGB = src.DiskUsedGB, src.DiskTotalGB
	dst.UpSpeed, dst.DownSpeed = src.UpSpeed, src.DownSpeed
	dst.TcpCount, dst.UdpCount, dst.FdCount = src.TcpCount, src.UdpCount, src.FdCount
	dst.SystemUptime = src.SystemUptime
	dst.LoadAvg1, dst.LoadAvg5, dst.LoadAvg15 = src.LoadAvg1, src.LoadAvg5, src.LoadAvg15
}

// snapshotIsFresh reports whether the snapshot holds a recent live reading,
// by the hub's own clock.
func snapshotIsFresh(s NodeStats) bool {
	return !s.StatsUnavailable && s.ReadingAgeMs != nil && time.Duration(*s.ReadingAgeMs)*time.Millisecond < liveStatsFreshFor
}

// GetNodeLiveStats serves the panel's stats read from the sweep's snapshot.
// An online node with a stale or missing snapshot gets one live read; when
// that fails too, the panel still gets the last known readings, flagged
// unavailable, instead of an error.
func (u *nodeUsecase) GetNodeLiveStats(ctx context.Context, nodeID uint) (*NodeStats, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	snap, _, ok := u.liveStats.get(nodeID)
	if ok && node.IsOnline && snapshotIsFresh(snap) {
		return &snap, nil
	}
	if node.IsOnline {
		if live, err := u.getNodeStatsForNode(ctx, node); err == nil {
			return live, nil
		}
	}
	if ok {
		snap.StatsUnavailable = true
		snap.OnlineUsers = cache.GetNodeOnlineCount(nodeID)
		if !node.IsOnline {
			snap.XrayStatus, snap.XrayPID, snap.ProcessUptime = "", 0, 0
		}
		return &snap, nil
	}
	return u.lastRecordedStats(ctx, node)
}

// lastRecordedStats rebuilds a partial reading from the newest node_stats
// row, for a node the hub hasn't swept since it started.
func (u *nodeUsecase) lastRecordedStats(ctx context.Context, node *domain.Node) (*NodeStats, error) {
	rows, err := u.nodeRepo.GetNodeStatsHistory(ctx, node.ID, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("node stats unavailable: nothing recorded yet")
	}
	r := rows[0]
	at := r.CreatedAt
	age := time.Since(at).Milliseconds()
	out := &NodeStats{
		StatsUnavailable: true,
		SampledAt:        &at,
		ReadingAgeMs:     &age,
		TotalUplink:      node.TotalUplink,
		TotalDownlink:    node.TotalDownlink,
		OnlineUsers:      cache.GetNodeOnlineCount(node.ID),
		CPUPercent:       r.CPU,
		MemoryPercent:    r.Memory,
		DiskPercent:      r.Disk,
		UpSpeed:          r.UpSpeed,
		DownSpeed:        r.DownSpeed,
		TcpCount:         r.TcpCount,
		UdpCount:         r.UdpCount,
		FdCount:          r.FdCount,
		LoadAvg1:         r.LoadAvg1,
		AgentVersion:     node.AgentVersion,
		XrayVersion:      node.XrayVersion,
	}
	nid := node.ID
	if total, err := u.accountRepo.Count(ctx, accountRepo.AccountFilter{NodeID: &nid}); err == nil {
		out.TotalAccounts = total
	}
	if active, err := u.accountRepo.Count(ctx, accountRepo.AccountFilter{NodeID: &nid, Status: "active"}); err == nil {
		out.ActiveAccounts = active
	}
	return out, nil
}
