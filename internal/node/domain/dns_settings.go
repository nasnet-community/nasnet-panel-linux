package domain

// DNSServer represents a single DNS server entry in xray-core configuration.
type DNSServer struct {
	Address         string   `json:"address"`
	Port            int      `json:"port,omitempty"`
	Domains         []string `json:"domains,omitempty"`
	ExpectedIPs     []string `json:"expected_ips,omitempty"`
	UnexpectedIPs   []string `json:"unexpected_ips,omitempty"`
	SkipFallback    bool     `json:"skip_fallback"`
	QueryStrategy   string   `json:"query_strategy,omitempty"`
	Tag             string   `json:"tag,omitempty"`
	ClientIP        string   `json:"client_ip,omitempty"`
	TimeoutMs       uint64   `json:"timeout_ms,omitempty"`
	DisableCache    *bool    `json:"disable_cache,omitempty"`
	ServeStale      *bool    `json:"serve_stale,omitempty"`
	ServeExpiredTTL *uint32  `json:"serve_expired_ttl,omitempty"`
	FinalQuery      bool     `json:"final_query"`
}

// DNSSettings holds per-node DNS configuration.
// Stored as JSONB on the Node model. When nil, DNS section is omitted from xray config.
type DNSSettings struct {
	Servers                []DNSServer    `json:"servers,omitempty"`
	Hosts                  map[string]any `json:"hosts,omitempty"`
	ClientIP               string         `json:"client_ip,omitempty"`
	QueryStrategy          string         `json:"query_strategy,omitempty"` // UseIP, UseIPv4, UseIPv6, UseSystem
	DisableCache           bool           `json:"disable_cache"`
	DisableFallback        bool           `json:"disable_fallback"`
	DisableFallbackIfMatch bool           `json:"disable_fallback_if_match"`
	Tag                    string         `json:"tag,omitempty"`
	ServeStale             bool           `json:"serve_stale"`
	ServeExpiredTTL        *uint32        `json:"serve_expired_ttl,omitempty"` // pointer: nil=omit, 0=unlimited
	EnableParallelQuery    bool           `json:"enable_parallel_query"`
	UseSystemHosts         bool           `json:"use_system_hosts"`
}

// FakeDNSPool configures a FakeDNS IP pool for xray-core.
type FakeDNSPool struct {
	IPPool  string `json:"ip_pool,omitempty"`  // CIDR, e.g. "198.18.0.0/15"
	LRUSize int64  `json:"lru_size,omitempty"` // LRU cache size for domain-to-fake-IP mappings
}
