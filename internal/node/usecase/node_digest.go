package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// NodeDigest is what the Nodes list shows for one node beyond its live stats:
// today's traffic, the last day's throughput, 30 days of uptime and the
// signals that flag a node as needing a look. DB and panel memory only, so it
// answers while the node is down.
type NodeDigest struct {
	Today NodeOverviewToday `json:"today"`
	// Yesterday is the previous UTC day's up+down bytes.
	Yesterday   int64  `json:"yesterday"`
	ConfigState string `json:"config_state"`
	// CPURecent is the trailing run of one-minute CPU averages, oldest first,
	// up to ten; a missing minute ends the run.
	CPURecent []float64 `json:"cpu_recent"`
	// Series is the last 24 hours in 15-minute buckets.
	Series []NodeDigestPoint `json:"series"`
	Uptime NodeDigestUptime  `json:"uptime"`
	// Last holds the last known readings of a node that is down.
	Last *NodeStats `json:"last,omitempty"`

	online bool
}

type NodeDigestPoint struct {
	T int64 `json:"t"`
	// Bps is the bucket's average up+down rate, bytes/s.
	Bps   float64 `json:"bps"`
	Users float64 `json:"users"`
}

type NodeDigestUptime struct {
	Days    []NodeDigestDay `json:"days"`
	Pct     float64         `json:"pct"`
	Outages int             `json:"outages"`
}

type NodeDigestDay struct {
	Date        string `json:"date"`
	ObservedSec int    `json:"observed_sec"`
	DowntimeSec int    `json:"downtime_sec"`
	PlannedSec  int    `json:"planned_sec"`
	Outages     int    `json:"outages"`
}

const (
	// Outlives the list's one-minute refetch, so each poll is served from here.
	digestCacheFor   = 75 * time.Second
	digestStepSec    = 15 * 60
	digestUptimeDays = 30
	cpuRecentMax     = 10
)

type digestCacheEntry struct {
	d         *NodeDigest
	expiresAt time.Time
}

// digestCache: the list refetches every minute per open tab, and nothing in
// a digest moves faster than that except what SSE already carries.
type digestCache struct {
	mu sync.Mutex
	m  map[uint]digestCacheEntry
}

func (c *digestCache) get(id uint) *NodeDigest {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.d
}

func (c *digestCache) put(id uint, d *NodeDigest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[uint]digestCacheEntry)
	}
	c.m[id] = digestCacheEntry{d: d, expiresAt: time.Now().Add(digestCacheFor)}
}

// GetNodesDigest returns a digest per node; empty ids means every node.
func (u *nodeUsecase) GetNodesDigest(ctx context.Context, ids []uint) (map[uint]*NodeDigest, error) {
	var nodes []*domain.Node
	if len(ids) == 0 {
		all, err := u.nodeRepo.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		nodes = all
	} else {
		for _, id := range ids {
			if n, err := u.nodeRepo.GetNode(ctx, id); err == nil {
				nodes = append(nodes, n)
			}
		}
	}

	out := make(map[uint]*NodeDigest, len(nodes))
	for _, n := range nodes {
		// Online/offline flips invalidate the panel's copy; a cached digest
		// must not keep last readings for a node that came back, or drop
		// them for one that just went down.
		if d := u.digestCache.get(n.ID); d != nil && d.online == n.IsOnline {
			out[n.ID] = d
			continue
		}
		d := u.nodeDigest(ctx, n)
		u.digestCache.put(n.ID, d)
		out[n.ID] = d
	}
	return out, nil
}

// nodeDigest never fails as a whole: a part that can't be read stays empty,
// so the list still shows the rest instead of a node stuck loading.
func (u *nodeUsecase) nodeDigest(ctx context.Context, node *domain.Node) *NodeDigest {
	warn := func(part string, err error) {
		logger.GetLogger().WithError(err).Warnf("[nodes-digest] node %d: %s", node.ID, part)
	}
	d := &NodeDigest{ConfigState: u.configState(node), CPURecent: []float64{}, Series: []NodeDigestPoint{}, online: node.IsOnline}
	now := time.Now().UTC()

	todayStart := now.Truncate(24 * time.Hour)
	if rows, err := u.nodeRepo.GetNodeDailyTraffic(ctx, node.ID, 2); err == nil {
		for _, r := range rows {
			day := r.Date.UTC().Truncate(24 * time.Hour)
			switch {
			case day.Equal(todayStart):
				d.Today.Uplink, d.Today.Downlink = r.Uplink, r.Downlink
			case day.Equal(todayStart.AddDate(0, 0, -1)):
				d.Yesterday = r.Uplink + r.Downlink
			}
		}
	}
	if peak, err := u.nodeRepo.GetNodeStatsPeakThroughput(ctx, node.ID, now.Add(-24*time.Hour)); err == nil {
		d.Today.PeakBps = peak
	}

	from := now.Add(-24 * time.Hour).Truncate(digestStepSec * time.Second)
	points, err := u.nodeRepo.GetNodeStatsSeries(ctx, node.ID, from, now, digestStepSec)
	if err != nil {
		warn("series", err)
	}
	for _, p := range points {
		d.Series = append(d.Series, NodeDigestPoint{T: p.T, Bps: p.UpAvg + p.DownAvg, Users: p.UsersMax})
	}

	if node.IsOnline {
		if hour, err := u.GetNodeStatsSeries(ctx, node.ID, "1h"); err == nil {
			d.CPURecent = trailingCPU(hour.Points, hour.StepSec, now.Unix())
		}
	} else {
		d.Last = u.lastKnownStats(ctx, node)
	}

	// The summary counts back from today, so 29 days plus today fill the
	// list's 30 marks exactly.
	d.Uptime.Days = []NodeDigestDay{}
	summary, err := u.GetNodeUptimeSummary(ctx, node.ID, digestUptimeDays-1)
	if err != nil {
		warn("uptime", err)
		return d
	}
	d.Uptime.Pct, d.Uptime.Outages = summary.UptimePct, summary.OutageCount
	for _, r := range summary.Days {
		d.Uptime.Days = append(d.Uptime.Days, NodeDigestDay{
			Date:        r.Date.UTC().Format("2006-01-02"),
			ObservedSec: r.ObservedSec,
			DowntimeSec: r.DowntimeSec,
			PlannedSec:  r.PlannedSec,
			Outages:     r.OutageCount,
		})
	}
	return d
}

// trailingCPU walks back from the newest one-minute bucket while the minutes
// stay contiguous. The newest bucket must be recent, or the run is stale.
func trailingCPU(points []domain.NodeStatBucket, stepSec int, nowUnix int64) []float64 {
	if len(points) == 0 || nowUnix-points[len(points)-1].T > int64(2*stepSec) {
		return []float64{}
	}
	run := []float64{}
	next := points[len(points)-1].T
	for i := len(points) - 1; i >= 0 && len(run) < cpuRecentMax; i-- {
		if points[i].T != next {
			break
		}
		run = append([]float64{points[i].CPUAvg}, run...)
		next -= int64(stepSec)
	}
	return run
}

// lastKnownStats is a down node's last readings, without dialing it.
func (u *nodeUsecase) lastKnownStats(ctx context.Context, node *domain.Node) *NodeStats {
	if snap, _, ok := u.liveStats.get(node.ID); ok {
		snap.StatsUnavailable = true
		snap.XrayStatus, snap.XrayPID, snap.ProcessUptime = "", 0, 0
		return &snap
	}
	s, err := u.lastRecordedStats(ctx, node)
	if err != nil {
		return nil
	}
	return s
}
