package accesslog

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Default per-row top-N caps. Overridden at runtime via hub settings
// pushed in AckBufferedAccessLogSummary (Aggregator.SetTopNCaps).
const (
	defaultMaxDomainsPerSummary         = 100
	defaultMaxRejectedDomainsPerSummary = 20
	defaultMaxSourceIPsPerSummary       = 100
)

// defaultGracePeriod is the fallback wall-clock margin past an hour's
// END before GetBuffered will return that hour. The hub setting
// access_log_grace_minutes overrides this at runtime via the
// AckBufferedAccessLogSummary RPC (Aggregator.SetGracePeriod).
const defaultGracePeriod = 90 * time.Minute

// maxBuckets caps in-memory hourly buckets at 7 days. Panel-side
// retention covers anything older; on-agent retention beyond this
// just grows memory without giving the panel anything it can't already
// pull during normal sweeps.
const maxBuckets = 24 * 7

// HourlySummary holds aggregated access log stats for one email in one hour.
type HourlySummary struct {
	Email           string           `json:"email"`
	HourTimestamp   int64            `json:"hour_timestamp"` // Unix seconds, start of hour
	AcceptedCount   int64            `json:"accepted_count"`
	RejectedCount   int64            `json:"rejected_count"`
	TcpCount        int64            `json:"tcp_count"`
	UdpCount        int64            `json:"udp_count"`
	Domains         map[string]int64 `json:"domains"`          // domain → count (accepted)
	RejectedDomains map[string]int64 `json:"rejected_domains"` // domain → count (rejected)
	SourceIPs       map[string]int64 `json:"source_ips"`       // IP → count
	fromDisk        bool             `json:"-"`                // loaded from persistent storage
}

// Aggregator accumulates parsed access log entries into hourly summary buckets.
// It follows the same buffered+ack pattern as the traffic store.
type Aggregator struct {
	mu             sync.Mutex
	buckets        map[int64]map[string]*HourlySummary // hourTs → email → summary
	acked          int64                               // last acked hour timestamp
	filePath       string
	droppedBuckets int64 // monotonic count of buckets evicted by the cap
	// graceNanos is the runtime-configured grace window in nanoseconds.
	// Set via SetGracePeriod from the hub. Zero = use defaultGracePeriod.
	// Read on the hot GetBuffered path so updates go through atomic.
	graceNanos int64
	// Per-row top-N caps. Zero = use the corresponding default const.
	// Updated via SetTopNCaps from the hub, read in GetBuffered.
	maxDomains         int32
	maxRejectedDomains int32
	maxSourceIPs       int32
}

// NewAggregator creates a new access log aggregator.
// filePath is used for persistence across restarts (empty = no persistence).
func NewAggregator(filePath string) *Aggregator {
	a := &Aggregator{
		buckets:  make(map[int64]map[string]*HourlySummary),
		filePath: filePath,
	}
	if filePath != "" {
		a.load()
	}
	return a
}

func hourTimestamp(t time.Time) int64 {
	return (t.Unix() / 3600) * 3600
}

// Record accumulates a single parsed entry into the aggregator.
func (a *Aggregator) Record(entry Entry) {
	if entry.Email == "" {
		return
	}

	hourTs := hourTimestamp(entry.Timestamp)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Skip if already acked
	if hourTs <= a.acked {
		return
	}

	emailMap, ok := a.buckets[hourTs]
	if !ok {
		// Cap in-memory buckets at maxBuckets. When at capacity, evict
		// the oldest hour (lowest hourTs) only if the new bucket would
		// actually be newer — never drop a newer bucket for an older one.
		if len(a.buckets) >= maxBuckets {
			var oldest int64 = math.MaxInt64
			for ts := range a.buckets {
				if ts < oldest {
					oldest = ts
				}
			}
			if oldest != math.MaxInt64 && oldest < hourTs {
				delete(a.buckets, oldest)
				a.droppedBuckets++
				logrus.WithFields(logrus.Fields{
					"evicted_hour_ts":   oldest,
					"dropped_total":     a.droppedBuckets,
					"buckets_in_memory": len(a.buckets),
				}).Warn("[accesslog] Bucket cap reached, evicting oldest hour")
			} else {
				// New bucket is older than the oldest in memory — refuse
				// to insert so we don't trade newer data for older.
				a.droppedBuckets++
				return
			}
		}
		emailMap = make(map[string]*HourlySummary)
		a.buckets[hourTs] = emailMap
	}

	s, ok := emailMap[entry.Email]
	if !ok {
		s = &HourlySummary{
			Email:           entry.Email,
			HourTimestamp:   hourTs,
			Domains:         make(map[string]int64),
			RejectedDomains: make(map[string]int64),
			SourceIPs:       make(map[string]int64),
		}
		emailMap[entry.Email] = s
	} else if s.fromDisk {
		// Reset: collector is re-reading the log file after restart,
		// so rebuild counts from scratch to avoid double-counting.
		s.AcceptedCount = 0
		s.RejectedCount = 0
		s.TcpCount = 0
		s.UdpCount = 0
		s.Domains = make(map[string]int64)
		s.RejectedDomains = make(map[string]int64)
		s.SourceIPs = make(map[string]int64)
		s.fromDisk = false
	}

	if entry.Status == "accepted" {
		s.AcceptedCount++
	} else {
		s.RejectedCount++
	}
	if entry.Network == "tcp" {
		s.TcpCount++
	} else {
		s.UdpCount++
	}
	if entry.Domain != "" {
		if entry.Status == "accepted" {
			s.Domains[entry.Domain]++
		} else {
			s.RejectedDomains[entry.Domain]++
		}
	}
	if entry.SourceIP != "" {
		s.SourceIPs[entry.SourceIP]++
	}
}

// GetBuffered returns hourly summaries whose hour ended at least
// gracePeriod() ago. Hours still inside the grace window are held
// back so late-flushed log lines aren't dropped after Ack(). The
// current hour is implicitly excluded because its end is still in
// the future.
func (a *Aggregator) GetBuffered() []*HourlySummary {
	cutoff := time.Now().Add(-a.gracePeriod()).Unix()

	a.mu.Lock()
	defer a.mu.Unlock()

	var result []*HourlySummary
	for hourTs, emailMap := range a.buckets {
		// Bucket H covers [H, H+3600). Eligible once H+3600 <= cutoff.
		if hourTs+3600 > cutoff {
			continue
		}
		for _, s := range emailMap {
			// Deep copy to avoid data races with concurrent Record() calls
			trimmed := *s
			trimmed.Domains = topN(s.Domains, a.maxDomainsCap())
			trimmed.RejectedDomains = topN(s.RejectedDomains, a.maxRejectedDomainsCap())
			trimmed.SourceIPs = topN(s.SourceIPs, a.maxSourceIPsCap())
			result = append(result, &trimmed)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].HourTimestamp != result[j].HourTimestamp {
			return result[i].HourTimestamp < result[j].HourTimestamp
		}
		return result[i].Email < result[j].Email
	})

	return result
}

// Ack removes all summaries with hour_timestamp <= upTo.
func (a *Aggregator) Ack(upTo int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for hourTs := range a.buckets {
		if hourTs <= upTo {
			delete(a.buckets, hourTs)
		}
	}
	if upTo > a.acked {
		a.acked = upTo
	}

	a.persistLocked()
}

// Persist writes current state to disk.
func (a *Aggregator) Persist() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.persistLocked()
}

// AggregatorStats is a snapshot of the aggregator's runtime counters.
type AggregatorStats struct {
	BucketsInMemory int   `json:"buckets_in_memory"`
	DroppedBuckets  int64 `json:"dropped_buckets"`
	AckedHour       int64 `json:"acked_hour"`
}

// Stats returns a snapshot of the aggregator's counters. Cheap lockless
// readers should call this rather than peeking at fields directly.
func (a *Aggregator) Stats() AggregatorStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AggregatorStats{
		BucketsInMemory: len(a.buckets),
		DroppedBuckets:  a.droppedBuckets,
		AckedHour:       a.acked,
	}
}

// SetGracePeriod updates the grace window applied on the next
// GetBuffered call. Non-positive resets to defaultGracePeriod.
// Values >24h are clamped. Concurrency-safe; no locks.
func (a *Aggregator) SetGracePeriod(d time.Duration) {
	const maxGrace = 24 * time.Hour
	if d <= 0 {
		atomic.StoreInt64(&a.graceNanos, 0)
		return
	}
	if d > maxGrace {
		d = maxGrace
	}
	atomic.StoreInt64(&a.graceNanos, int64(d))
}

// GracePeriod returns the effective grace window — configured value
// when set, otherwise defaultGracePeriod.
func (a *Aggregator) GracePeriod() time.Duration {
	return a.gracePeriod()
}

func (a *Aggregator) gracePeriod() time.Duration {
	g := atomic.LoadInt64(&a.graceNanos)
	if g <= 0 {
		return defaultGracePeriod
	}
	return time.Duration(g)
}

// SetTopNCaps updates the per-row top-N limits applied on the next
// GetBuffered call. Non-positive values reset that cap to its default.
// Values >maxAllowedCap are clamped. Concurrency-safe; lock-free.
func (a *Aggregator) SetTopNCaps(maxDomains, maxRejectedDomains, maxSourceIPs int32) {
	const maxAllowedCap int32 = 500
	clamp := func(v int32) int32 {
		if v <= 0 {
			return 0
		}
		if v > maxAllowedCap {
			return maxAllowedCap
		}
		return v
	}
	atomic.StoreInt32(&a.maxDomains, clamp(maxDomains))
	atomic.StoreInt32(&a.maxRejectedDomains, clamp(maxRejectedDomains))
	atomic.StoreInt32(&a.maxSourceIPs, clamp(maxSourceIPs))
}

func (a *Aggregator) maxDomainsCap() int {
	v := atomic.LoadInt32(&a.maxDomains)
	if v <= 0 {
		return defaultMaxDomainsPerSummary
	}
	return int(v)
}

func (a *Aggregator) maxRejectedDomainsCap() int {
	v := atomic.LoadInt32(&a.maxRejectedDomains)
	if v <= 0 {
		return defaultMaxRejectedDomainsPerSummary
	}
	return int(v)
}

func (a *Aggregator) maxSourceIPsCap() int {
	v := atomic.LoadInt32(&a.maxSourceIPs)
	if v <= 0 {
		return defaultMaxSourceIPsPerSummary
	}
	return int(v)
}

type aggregatorState struct {
	Acked   int64                               `json:"acked"`
	Buckets map[int64]map[string]*HourlySummary `json:"buckets"`
}

func (a *Aggregator) persistLocked() {
	if a.filePath == "" {
		return
	}
	state := aggregatorState{
		Acked:   a.acked,
		Buckets: a.buckets,
	}
	data, err := json.Marshal(state)
	if err != nil {
		logrus.WithError(err).Warn("[accesslog] Failed to marshal aggregator state")
		return
	}
	if err := os.WriteFile(a.filePath+".tmp", data, 0644); err != nil {
		logrus.WithError(err).Warn("[accesslog] Failed to write aggregator state")
		return
	}
	os.Rename(a.filePath+".tmp", a.filePath)
}

func (a *Aggregator) load() {
	data, err := os.ReadFile(a.filePath)
	if err != nil {
		return // file doesn't exist yet — normal
	}
	var state aggregatorState
	if err := json.Unmarshal(data, &state); err != nil {
		logrus.WithError(err).Warn("[accesslog] Failed to load aggregator state, starting fresh")
		return
	}
	a.acked = state.Acked
	if state.Buckets != nil {
		a.buckets = state.Buckets
		// Mark all loaded summaries so Record() resets them on first entry,
		// avoiding double-counting when the collector re-reads the log file.
		for _, emailMap := range a.buckets {
			for _, s := range emailMap {
				s.fromDisk = true
			}
		}
	}
	logrus.WithFields(logrus.Fields{
		"acked":   a.acked,
		"buckets": len(a.buckets),
	}).Info("[accesslog] Loaded aggregator state from disk")
}

// topN returns the top N entries from a map by value (descending).
func topN(m map[string]int64, n int) map[string]int64 {
	if len(m) <= n {
		cp := make(map[string]int64, len(m))
		for k, v := range m {
			cp[k] = v
		}
		return cp
	}

	type kv struct {
		k string
		v int64
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })

	result := make(map[string]int64, n)
	for i := 0; i < n; i++ {
		result[pairs[i].k] = pairs[i].v
	}
	return result
}
