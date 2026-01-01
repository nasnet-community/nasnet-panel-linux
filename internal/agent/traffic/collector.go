package traffic

import (
	"context"
	"sync"
	"time"

	agentxray "github.com/nasnet-community/nasnet-panel-linux/internal/agent/xray"
	"github.com/sirupsen/logrus"
)

// Collector drains Xray cumulative counters into a Store via baseline
// diffing (reset=false). Avoids the reset=true atomicity hazard where a
// crash between Xray's reset and our store would lose the interval.
// Per-key restart detection: current < baseline → treat as fresh.
// Baseline is persisted so agent restarts don't double-count.
type Collector struct {
	xrayClient *agentxray.LocalClient
	store      Store
	interval   time.Duration
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	baselineMu sync.Mutex
	baseline   *XrayStatsSnapshot
}

// NewCollector creates a traffic collector.
// interval is typically 5 seconds.
func NewCollector(xrayClient *agentxray.LocalClient, store Store, interval time.Duration) *Collector {
	return &Collector{
		xrayClient: xrayClient,
		store:      store,
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

// Start begins the background collection loop.
func (c *Collector) Start() {
	// Restore persisted baseline so we resume delta computation without
	// double-counting the last flushed interval.
	c.baselineMu.Lock()
	c.baseline = c.store.GetBaseline()
	c.baselineMu.Unlock()

	c.wg.Add(1)
	go c.loop()
	logrus.WithField("interval", c.interval).Info("[traffic] Collector started")
}

// Stop halts the collection loop and waits for it to finish.
// Idempotent — the signal handler and SelfUpdate both reach here during
// a restart, and a double close(c.stopCh) would panic.
func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.wg.Wait()
		logrus.Info("[traffic] Collector stopped")
	})
}

func (c *Collector) loop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.collect()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Collector) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Read cumulative counters. reset=false so we own the delta computation
	// and any crash between RPC return and store write is harmless — we just
	// re-read the same cumulative counter on the next tick.
	stats, err := c.xrayClient.QueryStats(ctx, "", false)
	if err != nil {
		// Xray might be down or restarting — skip silently
		return
	}

	current := &XrayStatsSnapshot{
		UserUplink:       stats.UserUplink,
		UserDownlink:     stats.UserDownlink,
		InboundUplink:    stats.InboundUplink,
		InboundDownlink:  stats.InboundDownlink,
		OutboundUplink:   stats.OutboundUplink,
		OutboundDownlink: stats.OutboundDownlink,
		TotalUplink:      stats.TotalUplink,
		TotalDownlink:    stats.TotalDownlink,
	}

	c.baselineMu.Lock()
	delta := diffSnapshot(c.baseline, current)
	c.baseline = current
	c.baselineMu.Unlock()

	if !snapshotIsEmpty(delta) {
		c.store.Accumulate(delta, time.Now())
	}

	// Persist the cumulative snapshot so the next process restart
	// resumes delta computation from the same baseline. Safe to call
	// outside the lock because Store.SetBaseline copies internally.
	c.store.SetBaseline(current)
}

// diffSnapshot returns curr - prev applied per-key. For any key where the
// current value is less than the previous (e.g. xray restart zeroed the
// counter), the current value itself is used as the delta.
func diffSnapshot(prev, curr *XrayStatsSnapshot) *XrayStatsSnapshot {
	if curr == nil {
		return &XrayStatsSnapshot{
			UserUplink:       map[string]int64{},
			UserDownlink:     map[string]int64{},
			InboundUplink:    map[string]int64{},
			InboundDownlink:  map[string]int64{},
			OutboundUplink:   map[string]int64{},
			OutboundDownlink: map[string]int64{},
		}
	}
	if prev == nil {
		// No baseline yet: treat the full cumulative as the delta. First
		// collect after agent startup with no persisted baseline.
		return &XrayStatsSnapshot{
			UserUplink:       copyMap(curr.UserUplink),
			UserDownlink:     copyMap(curr.UserDownlink),
			InboundUplink:    copyMap(curr.InboundUplink),
			InboundDownlink:  copyMap(curr.InboundDownlink),
			OutboundUplink:   copyMap(curr.OutboundUplink),
			OutboundDownlink: copyMap(curr.OutboundDownlink),
			TotalUplink:      curr.TotalUplink,
			TotalDownlink:    curr.TotalDownlink,
		}
	}

	return &XrayStatsSnapshot{
		UserUplink:       diffMap(prev.UserUplink, curr.UserUplink),
		UserDownlink:     diffMap(prev.UserDownlink, curr.UserDownlink),
		InboundUplink:    diffMap(prev.InboundUplink, curr.InboundUplink),
		InboundDownlink:  diffMap(prev.InboundDownlink, curr.InboundDownlink),
		OutboundUplink:   diffMap(prev.OutboundUplink, curr.OutboundUplink),
		OutboundDownlink: diffMap(prev.OutboundDownlink, curr.OutboundDownlink),
		TotalUplink:      diffInt64(prev.TotalUplink, curr.TotalUplink),
		TotalDownlink:    diffInt64(prev.TotalDownlink, curr.TotalDownlink),
	}
}

func diffMap(prev, curr map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(curr))
	for k, c := range curr {
		if c <= 0 {
			continue
		}
		p := prev[k]
		if c < p {
			// Counter went backwards — xray restart or key reset. Use the
			// full current value as the delta since the prior baseline is
			// no longer meaningful for this key.
			out[k] = c
		} else if c > p {
			out[k] = c - p
		}
	}
	return out
}

func diffInt64(prev, curr int64) int64 {
	if curr < prev {
		return curr
	}
	return curr - prev
}

func snapshotIsEmpty(s *XrayStatsSnapshot) bool {
	if s == nil {
		return true
	}
	if s.TotalUplink != 0 || s.TotalDownlink != 0 {
		return false
	}
	return len(s.UserUplink) == 0 && len(s.UserDownlink) == 0 &&
		len(s.InboundUplink) == 0 && len(s.InboundDownlink) == 0 &&
		len(s.OutboundUplink) == 0 && len(s.OutboundDownlink) == 0
}
