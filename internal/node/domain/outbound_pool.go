package domain

import (
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"time"
)

// MaxPoolScanThreads caps a remote node's concurrent candidate tests.
const MaxPoolScanThreads = 2000

// The built-in node scans inside the router's own panel process, next to
// the router's traffic, so it gets a far smaller budget (D7).
const (
	LocalPoolScanThreads        = 32
	LocalPoolDefaultConcurrency = 16
)

// PoolScanThreads is the node-wide scan budget for a node.
func PoolScanThreads(n *Node) int {
	if n.IsLocal() {
		return LocalPoolScanThreads
	}
	return MaxPoolScanThreads
}

type OutboundPool struct {
	ActiveRunID      uint                 `json:"active_run_id"`
	ID               uint                 `gorm:"primaryKey" json:"id"`
	NodeID           uint                 `gorm:"not null;index" json:"node_id"`
	Name             string               `gorm:"size:100;not null" json:"name"`
	BalancerID       uint                 `gorm:"not null;index" json:"balancer_id"`
	Enabled          bool                 `json:"enabled"`
	StopWhenFull     bool                 `json:"stop_when_full"`
	MaxActive        int                  `json:"max_active"`
	RefreshSeconds   int                  `json:"refresh_seconds"`
	RescanSeconds    int                  `json:"rescan_seconds"`
	Concurrency      int                  `json:"concurrency"`
	MaxDelayMs       int                  `json:"max_delay_ms"`
	Retries          int                  `json:"retries"`
	FailureThreshold int                  `json:"failure_threshold"`
	MaxSourceLinks   int                  `json:"max_source_links"`
	MaxSourceBytes   int64                `json:"max_source_bytes"`
	MaxCandidates    int                  `json:"max_candidates"`
	TestURLs         jsontype.StringSlice `gorm:"serializer:json;type:text" json:"test_urls"`
	Revision         uint64               `json:"revision"`
	AppliedRevision  uint64               `json:"applied_revision"`
	NextRefreshAt    time.Time            `json:"next_refresh_at"`
	NextScanAt       time.Time            `json:"next_scan_at"`
	LastRunID        uint                 `json:"last_run_id"`
	Sources          []OutboundPoolSource `gorm:"foreignKey:PoolID" json:"sources,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type OutboundPoolSource struct {
	ID           uint                 `gorm:"primaryKey" json:"id"`
	PoolID       uint                 `gorm:"not null;index" json:"pool_id"`
	Name         string               `gorm:"size:100" json:"name"`
	Kind         string               `gorm:"size:16" json:"kind"`
	URL          string               `gorm:"type:text" json:"url,omitempty"`
	Links        string               `gorm:"serializer:encoded_string;type:text" json:"links,omitempty"`
	UserAgent    string               `json:"user_agent,omitempty"`
	AllowPrivate bool                 `json:"allow_private"`
	Snapshot     jsontype.StringSlice `gorm:"serializer:json;type:text" json:"-"`
	ETag         string               `json:"-"`
	LastModified string               `json:"-"`
	FetchedAt    *time.Time           `json:"fetched_at"`
	LastError    string               `json:"last_error,omitempty"`
	LinkCount    int                  `json:"link_count"`
}

type OutboundPoolCandidate struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	PoolID       uint       `gorm:"uniqueIndex:idx_pool_identity;not null" json:"pool_id"`
	Fingerprint  string     `gorm:"size:100;uniqueIndex:idx_pool_identity;not null" json:"fingerprint"`
	Link         string     `gorm:"serializer:encoded_string;type:text" json:"link"`
	Remark       string     `json:"remark"`
	Protocol     string     `json:"protocol"`
	Address      string     `json:"address"`
	SourceIDs    []uint     `gorm:"serializer:json;type:text" json:"source_ids"`
	Present      bool       `gorm:"index" json:"present"`
	Excluded     bool       `json:"excluded"`
	Status       string     `gorm:"size:32" json:"status"`
	LastError    string     `json:"last_error,omitempty"`
	Failures     int        `json:"failures"`
	LatencyMs    int64      `json:"latency_ms"`
	TestedAt     *time.Time `json:"tested_at"`
	SeenAt       time.Time  `json:"seen_at"`
	OutboundJSON string     `gorm:"type:text" json:"-"`
}

type OutboundPoolRun struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	PoolID         uint       `gorm:"not null;index" json:"pool_id"`
	Revision       uint64     `json:"revision"`
	State          string     `gorm:"size:32;index" json:"state"`
	Refresh        bool       `json:"refresh"`
	RawCount       int        `json:"raw_count"`
	UniqueCount    int        `json:"unique_count"`
	DuplicateCount int        `json:"duplicate_count"`
	Tested         int        `json:"tested"`
	Healthy        int        `json:"healthy"`
	Active         int        `json:"active"`
	Error          string     `gorm:"type:text" json:"error,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	LeaseUntil     time.Time  `gorm:"index" json:"-"`
}
