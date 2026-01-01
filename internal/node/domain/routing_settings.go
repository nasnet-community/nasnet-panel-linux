package domain

// RoutingSettings holds per-node routing configuration presets.
// Stored as JSONB on the Node model. When nil, defaults are applied.
type RoutingSettings struct {
	// General
	DomainStrategy string `json:"domain_strategy"` // AsIs, IPIfNonMatch, IPOnDemand

	// Basic Routing
	BlockBitTorrent bool     `json:"block_bittorrent"`
	BlockIPs        []string `json:"block_ips"`      // geoip:private, geoip:ir, etc.
	BlockDomains    []string `json:"block_domains"`  // geosite:category-ads-all, geosite:ir, etc.
	DirectIPs       []string `json:"direct_ips"`     // geoip:private, geoip:cn, etc.
	DirectDomains   []string `json:"direct_domains"` // geosite:telegram, geosite:reddit, etc.
	IPv4Routing     []string `json:"ipv4_routing"`   // geosite entries to route via IPv4
	WARPEnabled     bool     `json:"warp_enabled"`
	WARPDomains     []string `json:"warp_domains"`
	WARPIPs         []string `json:"warp_ips"`

	// Outbound Testing
	OutboundTestURL string `json:"outbound_test_url"` // Default URL for outbound connectivity tests
}

// GetDefaultRoutingSettings returns sensible defaults that preserve existing behavior.
func GetDefaultRoutingSettings() *RoutingSettings {
	return &RoutingSettings{
		DomainStrategy: "IPIfNonMatch",
	}
}
