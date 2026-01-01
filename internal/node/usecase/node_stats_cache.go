package usecase

import (
	"sync"
	"time"
)

// nodeStatsCacheTTL: 5s — dedupes multiple panel tabs polling at 15s
// while still feeling live.
const nodeStatsCacheTTL = 5 * time.Second

// nodeStatsCache memoizes GetNodeStats results per node so a burst
// of concurrent bulk requests (e.g. several admin tabs open) all
// share one backend fan-out.
type nodeStatsCache struct {
	mu      sync.RWMutex
	entries map[uint]nodeStatsCacheEntry
	ttl     time.Duration
}

type nodeStatsCacheEntry struct {
	stats     *NodeStats
	expiresAt time.Time
}

func newNodeStatsCache(ttl time.Duration) *nodeStatsCache {
	return &nodeStatsCache{
		entries: make(map[uint]nodeStatsCacheEntry),
		ttl:     ttl,
	}
}

// Get returns the cached stats if still fresh, else nil. Callers
// should treat nil as "not in cache" and fall through to fetch.
func (c *nodeStatsCache) Get(nodeID uint) *NodeStats {
	c.mu.RLock()
	entry, ok := c.entries[nodeID]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.stats
}

// Put stores stats with TTL. Skips empty payloads so an RPC failure
// doesn't poison the cache.
func (c *nodeStatsCache) Put(nodeID uint, stats *NodeStats) {
	if stats == nil {
		return
	}
	c.mu.Lock()
	c.entries[nodeID] = nodeStatsCacheEntry{
		stats:     stats,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Invalidate drops any cached stats for the given node. Called from
// mutation paths (PushConfig, RestartXray, DeleteNode, …) so the
// panel sees post-action numbers immediately instead of waiting out
// the TTL.
func (c *nodeStatsCache) Invalidate(nodeID uint) {
	c.mu.Lock()
	delete(c.entries, nodeID)
	c.mu.Unlock()
}
