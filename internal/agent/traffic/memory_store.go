package traffic

import (
	"sort"
	"sync"
	"time"
)

// MemoryStore is an in-memory Store implementation with configurable bucket duration and retention.
type MemoryStore struct {
	mu             sync.RWMutex
	buckets        map[int64]*TrafficBucket
	baseline       *XrayStatsSnapshot
	bucketDuration time.Duration
	retention      time.Duration
}

// NewMemoryStore creates a new in-memory traffic store.
// bucketDuration is typically 1 hour. retention is the max age of buckets (e.g., 7 days).
func NewMemoryStore(bucketDuration, retention time.Duration) *MemoryStore {
	return &MemoryStore{
		buckets:        make(map[int64]*TrafficBucket),
		bucketDuration: bucketDuration,
		retention:      retention,
	}
}

func (s *MemoryStore) truncateTime(t time.Time) int64 {
	secs := int64(s.bucketDuration.Seconds())
	return (t.Unix() / secs) * secs
}

func (s *MemoryStore) Accumulate(snapshot *XrayStatsSnapshot, now time.Time) {
	if snapshot == nil {
		return
	}

	ts := s.truncateTime(now)

	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, ok := s.buckets[ts]
	if !ok {
		bucket = &TrafficBucket{
			Timestamp:        ts,
			UserUplink:       make(map[string]int64),
			UserDownlink:     make(map[string]int64),
			InboundUplink:    make(map[string]int64),
			InboundDownlink:  make(map[string]int64),
			OutboundUplink:   make(map[string]int64),
			OutboundDownlink: make(map[string]int64),
		}
		s.buckets[ts] = bucket
	}

	// Merge user traffic
	for k, v := range snapshot.UserUplink {
		bucket.UserUplink[k] += v
	}
	for k, v := range snapshot.UserDownlink {
		bucket.UserDownlink[k] += v
	}
	for k, v := range snapshot.InboundUplink {
		bucket.InboundUplink[k] += v
	}
	for k, v := range snapshot.InboundDownlink {
		bucket.InboundDownlink[k] += v
	}

	// Merge outbound traffic
	for k, v := range snapshot.OutboundUplink {
		bucket.OutboundUplink[k] += v
	}
	for k, v := range snapshot.OutboundDownlink {
		bucket.OutboundDownlink[k] += v
	}

	// Merge totals
	bucket.TotalUplink += snapshot.TotalUplink
	bucket.TotalDownlink += snapshot.TotalDownlink

	// Evict expired buckets
	cutoff := now.Add(-s.retention).Unix()
	for ts := range s.buckets {
		if ts < cutoff {
			delete(s.buckets, ts)
		}
	}
}

func (s *MemoryStore) GetAll() []*TrafficBucket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*TrafficBucket, 0, len(s.buckets))
	for _, b := range s.buckets {
		result = append(result, deepCopyBucket(b))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	return result
}

func (s *MemoryStore) Drain(throughTime int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for ts := range s.buckets {
		if ts <= throughTime {
			delete(s.buckets, ts)
		}
	}
}

func (s *MemoryStore) Close() error {
	return nil
}

// GetBaseline returns a deep copy of the stored baseline, or nil if none.
func (s *MemoryStore) GetBaseline() *XrayStatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.baseline)
}

// SetBaseline stores a deep copy of the provided snapshot.
func (s *MemoryStore) SetBaseline(snapshot *XrayStatsSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseline = cloneSnapshot(snapshot)
}

func cloneSnapshot(src *XrayStatsSnapshot) *XrayStatsSnapshot {
	if src == nil {
		return nil
	}
	return &XrayStatsSnapshot{
		UserUplink:       copyMap(src.UserUplink),
		UserDownlink:     copyMap(src.UserDownlink),
		InboundUplink:    copyMap(src.InboundUplink),
		InboundDownlink:  copyMap(src.InboundDownlink),
		OutboundUplink:   copyMap(src.OutboundUplink),
		OutboundDownlink: copyMap(src.OutboundDownlink),
		TotalUplink:      src.TotalUplink,
		TotalDownlink:    src.TotalDownlink,
	}
}

// Aggregate returns a single combined snapshot of all buckets, useful for legacy GetXrayStats.
func (s *MemoryStore) Aggregate() *XrayStatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agg := &XrayStatsSnapshot{
		UserUplink:       make(map[string]int64),
		UserDownlink:     make(map[string]int64),
		InboundUplink:    make(map[string]int64),
		InboundDownlink:  make(map[string]int64),
		OutboundUplink:   make(map[string]int64),
		OutboundDownlink: make(map[string]int64),
	}

	for _, b := range s.buckets {
		for k, v := range b.UserUplink {
			agg.UserUplink[k] += v
		}
		for k, v := range b.UserDownlink {
			agg.UserDownlink[k] += v
		}
		for k, v := range b.InboundUplink {
			agg.InboundUplink[k] += v
		}
		for k, v := range b.InboundDownlink {
			agg.InboundDownlink[k] += v
		}
		for k, v := range b.OutboundUplink {
			agg.OutboundUplink[k] += v
		}
		for k, v := range b.OutboundDownlink {
			agg.OutboundDownlink[k] += v
		}
		agg.TotalUplink += b.TotalUplink
		agg.TotalDownlink += b.TotalDownlink
	}

	return agg
}

// DrainAll removes all buckets and returns the aggregated snapshot.
func (s *MemoryStore) DrainAll() *XrayStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	agg := &XrayStatsSnapshot{
		UserUplink:       make(map[string]int64),
		UserDownlink:     make(map[string]int64),
		InboundUplink:    make(map[string]int64),
		InboundDownlink:  make(map[string]int64),
		OutboundUplink:   make(map[string]int64),
		OutboundDownlink: make(map[string]int64),
	}

	for _, b := range s.buckets {
		for k, v := range b.UserUplink {
			agg.UserUplink[k] += v
		}
		for k, v := range b.UserDownlink {
			agg.UserDownlink[k] += v
		}
		for k, v := range b.InboundUplink {
			agg.InboundUplink[k] += v
		}
		for k, v := range b.InboundDownlink {
			agg.InboundDownlink[k] += v
		}
		for k, v := range b.OutboundUplink {
			agg.OutboundUplink[k] += v
		}
		for k, v := range b.OutboundDownlink {
			agg.OutboundDownlink[k] += v
		}
		agg.TotalUplink += b.TotalUplink
		agg.TotalDownlink += b.TotalDownlink
	}

	s.buckets = make(map[int64]*TrafficBucket)
	return agg
}

func deepCopyBucket(b *TrafficBucket) *TrafficBucket {
	cp := &TrafficBucket{
		Timestamp:        b.Timestamp,
		UserUplink:       copyMap(b.UserUplink),
		UserDownlink:     copyMap(b.UserDownlink),
		InboundUplink:    copyMap(b.InboundUplink),
		InboundDownlink:  copyMap(b.InboundDownlink),
		OutboundUplink:   copyMap(b.OutboundUplink),
		OutboundDownlink: copyMap(b.OutboundDownlink),
		TotalUplink:      b.TotalUplink,
		TotalDownlink:    b.TotalDownlink,
	}
	return cp
}

func copyMap(m map[string]int64) map[string]int64 {
	cp := make(map[string]int64, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
