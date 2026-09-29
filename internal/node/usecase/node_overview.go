package usecase

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// ── Ranged history ───────────────────────────────────────────────────

// statsSeriesRange is one selectable chart range: its span and bucket width.
type statsSeriesRange struct {
	span     time.Duration
	step     time.Duration
	cacheFor time.Duration
}

var statsSeriesRanges = map[string]statsSeriesRange{
	"1h":  {span: time.Hour, step: time.Minute, cacheFor: 30 * time.Second},
	"6h":  {span: 6 * time.Hour, step: 5 * time.Minute, cacheFor: 30 * time.Second},
	"24h": {span: 24 * time.Hour, step: 5 * time.Minute, cacheFor: 30 * time.Second},
	"7d":  {span: 7 * 24 * time.Hour, step: time.Hour, cacheFor: 5 * time.Minute},
	"30d": {span: 30 * 24 * time.Hour, step: 4 * time.Hour, cacheFor: 5 * time.Minute},
}

// ErrUnknownStatsRange is returned for a range the series endpoint doesn't offer.
var ErrUnknownStatsRange = fmt.Errorf("unknown range: use 1h, 6h, 24h, 7d or 30d")

// NodeStatsSeries is a node's stats averaged into fixed-width buckets.
// From and To are unix seconds; a bucket with no samples is absent, and
// the client draws a gap there.
type NodeStatsSeries struct {
	Range   string                  `json:"range"`
	StepSec int                     `json:"step_sec"`
	From    int64                   `json:"from"`
	To      int64                   `json:"to"`
	Points  []domain.NodeStatBucket `json:"points"`
}

type statsSeriesCacheEntry struct {
	series    *NodeStatsSeries
	expiresAt time.Time
}

// statsSeriesCache: every open Overview refetches its range on an interval;
// the aggregate is the same for all of them until the next bucket fills.
type statsSeriesCache struct {
	mu sync.Mutex
	m  map[string]statsSeriesCacheEntry
}

func (c *statsSeriesCache) get(key string) *NodeStatsSeries {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.series
}

func (c *statsSeriesCache) put(key string, s *NodeStatsSeries, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]statsSeriesCacheEntry)
	}
	// Expired entries are dropped on write so the map stays bounded by the
	// nodes×ranges actually being viewed.
	now := time.Now()
	for k, e := range c.m {
		if now.After(e.expiresAt) {
			delete(c.m, k)
		}
	}
	c.m[key] = statsSeriesCacheEntry{series: s, expiresAt: now.Add(ttl)}
}

// GetNodeStatsSeries returns the node's recorded stats for one chart range.
func (u *nodeUsecase) GetNodeStatsSeries(ctx context.Context, nodeID uint, rangeKey string) (*NodeStatsSeries, error) {
	r, ok := statsSeriesRanges[rangeKey]
	if !ok {
		return nil, ErrUnknownStatsRange
	}
	key := fmt.Sprintf("%d:%s", nodeID, rangeKey)
	if cached := u.seriesCache.get(key); cached != nil {
		return cached, nil
	}
	stepSec := int(r.step / time.Second)
	now := time.Now()
	// Start on a bucket boundary so the first bucket isn't a partial one.
	from := now.Add(-r.span).Truncate(r.step)
	points, err := u.nodeRepo.GetNodeStatsSeries(ctx, nodeID, from, now, stepSec)
	if err != nil {
		return nil, err
	}
	if points == nil {
		points = []domain.NodeStatBucket{}
	}
	s := &NodeStatsSeries{Range: rangeKey, StepSec: stepSec, From: from.Unix(), To: now.Unix(), Points: points}
	u.seriesCache.put(key, s, r.cacheFor)
	return s, nil
}

// ── Overview facts ───────────────────────────────────────────────────

// NodeOverview is what the Overview tab shows beyond the live stats. All of
// it comes from the DB and hub memory, so it answers while the node is down.
type NodeOverview struct {
	Connection NodeOverviewConnection `json:"connection"`
	Agent      NodeOverviewAgent      `json:"agent"`
	Config     NodeOverviewConfig     `json:"config"`
	Today      NodeOverviewToday      `json:"today"`
}

type NodeOverviewConnection struct {
	// Mode is "local", "direct", "reverse" or "stealth".
	Mode string `json:"mode"`
	// RTTMs is the latest heartbeat latency; nil when there is none recent.
	RTTMs *int64 `json:"rtt_ms"`
}

type NodeOverviewAgent struct {
	Version string `json:"version"`
	// BundledVersion is the agent this hub ships; empty when unknown.
	BundledVersion  string `json:"bundled_version"`
	UpdateAvailable bool   `json:"update_available"`
}

type NodeOverviewConfig struct {
	// State is "in_sync", "drifted" or "unknown" — the agent's running config
	// against the last one the hub pushed.
	State string `json:"state"`
}

type NodeOverviewToday struct {
	Uplink   int64 `json:"uplink"`
	Downlink int64 `json:"downlink"`
	// PeakBps is the highest up+down rate over the last 24 hours, bytes/s.
	PeakBps float64 `json:"peak_bps"`
}

// rttFreshFor: an RTT older than this belongs to a session that has gone quiet.
const rttFreshFor = 2 * time.Minute

func (u *nodeUsecase) GetNodeOverview(ctx context.Context, nodeID uint) (*NodeOverview, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	snap, _, hasSnap := u.liveStats.get(nodeID)
	agentHash := u.liveStats.configHash(nodeID)

	out := &NodeOverview{}
	if node.IsLocal() {
		// The built-in node runs in-process: no link to time, and its agent
		// is the panel itself, which updates with the panel.
		out.Connection.Mode = domain.ConnectModeLocal
		out.Agent.Version = agentserver.Version
	} else {
		out.Connection.Mode = node.ConnectMode
		if node.IsStealth {
			out.Connection.Mode = "stealth"
		}
		if node.IsOnline {
			out.Connection.RTTMs = u.nodeRTT(node)
		}

		out.Agent.Version = node.AgentVersion
		if hasSnap && snap.AgentVersion != "" {
			out.Agent.Version = snap.AgentVersion
		}
		out.Agent.BundledVersion = bundledAgentVersion()
		out.Agent.UpdateAvailable = versionLess(out.Agent.Version, out.Agent.BundledVersion)
	}

	out.Config.State = "unknown"
	if node.IsOnline {
		if agentHash == "" && u.heartbeatMgr != nil {
			if _, h, ok := u.heartbeatMgr.GetSessionInfo(nodeID); ok {
				agentHash = h
			}
		}
		u.configHashMu.RLock()
		pushed := u.lastPushedConfigHash[nodeID]
		u.configHashMu.RUnlock()
		switch {
		case pushed == "" || agentHash == "":
		case pushed == agentHash:
			out.Config.State = "in_sync"
		default:
			out.Config.State = "drifted"
		}
	}

	now := time.Now().UTC()
	if rows, err := u.nodeRepo.GetNodeDailyTraffic(ctx, nodeID, 1); err == nil {
		for _, r := range rows {
			d := r.Date.UTC()
			if d.Year() == now.Year() && d.YearDay() == now.YearDay() {
				out.Today.Uplink, out.Today.Downlink = r.Uplink, r.Downlink
			}
		}
	}
	if peak, err := u.nodeRepo.GetNodeStatsPeakThroughput(ctx, nodeID, now.Add(-24*time.Hour)); err == nil {
		out.Today.PeakBps = peak
	}
	return out, nil
}

// nodeRTT reads the latest heartbeat latency. The HeartbeatManager sees
// pongs in both modes; the reverse session is the fallback. The agent
// computes it as now minus the ping's send time, so a negative value is
// clock skew, not a measurement.
func (u *nodeUsecase) nodeRTT(node *domain.Node) *int64 {
	fresh := func(rtt int64, at time.Time) *int64 {
		if at.IsZero() || time.Since(at) > rttFreshFor || rtt < 0 {
			return nil
		}
		return &rtt
	}
	if u.heartbeatMgr != nil {
		if rtt, at, ok := u.heartbeatMgr.GetLastPong(node.ID); ok {
			if v := fresh(rtt, at); v != nil {
				return v
			}
		}
	}
	if u.reverseRegistry != nil {
		if sess := u.reverseRegistry.GetByNodeID(node.ID); sess != nil {
			return fresh(sess.LastRTT())
		}
	}
	return nil
}

// ── Shipped agent version ────────────────────────────────────────────

// bundledAgentVersionPaths: build-agent.sh writes bin/agent/VERSION next to
// the binaries the hub serves; a source checkout also has internal/agent/VERSION.
var bundledAgentVersionPaths = []string{"bin/agent/VERSION", "internal/agent/VERSION"}

var bundledVersion struct {
	mu     sync.Mutex
	value  string
	readAt time.Time
}

// bundledAgentVersion is the agent version this hub deploys, re-read every
// five minutes so a rebuilt agent shows up without a hub restart.
func bundledAgentVersion() string {
	bundledVersion.mu.Lock()
	defer bundledVersion.mu.Unlock()
	if !bundledVersion.readAt.IsZero() && time.Since(bundledVersion.readAt) < 5*time.Minute {
		return bundledVersion.value
	}
	bundledVersion.value = ""
	for _, p := range bundledAgentVersionPaths {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				bundledVersion.value = v
				break
			}
		}
	}
	bundledVersion.readAt = time.Now()
	return bundledVersion.value
}

// versionLess reports a < b for dotted numeric versions ("0.0.69" < "0.0.71").
// Anything unparsable compares as not-less, so no false "update available".
func versionLess(a, b string) bool {
	pa, okA := parseDotted(a)
	pb, okB := parseDotted(b)
	if !okA || !okB {
		return false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func parseDotted(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
