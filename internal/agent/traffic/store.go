package traffic

import "time"

// XrayStatsSnapshot is a decoupled snapshot of Xray traffic counters.
type XrayStatsSnapshot struct {
	UserUplink       map[string]int64
	UserDownlink     map[string]int64
	InboundUplink    map[string]int64
	InboundDownlink  map[string]int64
	OutboundUplink   map[string]int64
	OutboundDownlink map[string]int64
	TotalUplink      int64
	TotalDownlink    int64
}

// TrafficBucket holds aggregated traffic for a single time bucket (typically 1 hour).
type TrafficBucket struct {
	Timestamp        int64            `json:"timestamp"` // Unix seconds, start of bucket
	UserUplink       map[string]int64 `json:"user_uplink"`
	UserDownlink     map[string]int64 `json:"user_downlink"`
	InboundUplink    map[string]int64 `json:"inbound_uplink"`
	InboundDownlink  map[string]int64 `json:"inbound_downlink"`
	OutboundUplink   map[string]int64 `json:"outbound_uplink"`
	OutboundDownlink map[string]int64 `json:"outbound_downlink"`
	TotalUplink      int64            `json:"total_uplink"`
	TotalDownlink    int64            `json:"total_downlink"`
}

// Store defines the interface for buffered traffic storage.
type Store interface {
	// Accumulate merges a snapshot into the appropriate time bucket.
	Accumulate(snapshot *XrayStatsSnapshot, now time.Time)
	// GetAll returns a deep copy of all buckets sorted by timestamp ascending.
	GetAll() []*TrafficBucket
	// Drain removes all buckets with timestamp <= throughTime.
	Drain(throughTime int64)
	// GetBaseline returns the last-persisted cumulative counter baseline
	// (used by the collector for delta computation). Nil if none saved yet.
	GetBaseline() *XrayStatsSnapshot
	// SetBaseline stores the cumulative counter baseline. Callers pass a
	// snapshot they may continue to read from; implementations copy the data.
	SetBaseline(snapshot *XrayStatsSnapshot)
	// Close performs cleanup (e.g., final flush for file-backed stores).
	Close() error
}
