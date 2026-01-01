package usecase

import (
	"testing"
	"time"
)

func TestStatsCache_PutGetRoundTrip(t *testing.T) {
	c := newNodeStatsCache(time.Second)
	stats := &NodeStats{CPUPercent: 42.5, XrayStatus: "running"}
	c.Put(1, stats)

	got := c.Get(1)
	if got == nil || got.CPUPercent != 42.5 {
		t.Errorf("expected cached stats back, got %+v", got)
	}
}

func TestStatsCache_MissOnUnknown(t *testing.T) {
	c := newNodeStatsCache(time.Second)
	if got := c.Get(99); got != nil {
		t.Errorf("expected nil for unknown node, got %+v", got)
	}
}

func TestStatsCache_ExpiresAfterTTL(t *testing.T) {
	c := newNodeStatsCache(10 * time.Millisecond)
	c.Put(1, &NodeStats{CPUPercent: 1})
	time.Sleep(20 * time.Millisecond)
	if got := c.Get(1); got != nil {
		t.Errorf("expired entry should return nil, got %+v", got)
	}
}

func TestStatsCache_InvalidateDrops(t *testing.T) {
	c := newNodeStatsCache(time.Hour)
	c.Put(1, &NodeStats{CPUPercent: 1})
	c.Invalidate(1)
	if got := c.Get(1); got != nil {
		t.Errorf("invalidate must drop entry, got %+v", got)
	}
}

func TestStatsCache_NilPutIsSkipped(t *testing.T) {
	// RPC-failure paths pass nil up; we must not poison the cache with
	// an empty stats object that would mask the real value for the TTL.
	c := newNodeStatsCache(time.Hour)
	c.Put(1, nil)
	if got := c.Get(1); got != nil {
		t.Errorf("nil put must not cache, got %+v", got)
	}
}
