package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"gorm.io/gorm"
)

// RoutingRule represents a routing rule that determines how traffic is routed
type RoutingRule struct {
	ID     uint  `gorm:"primaryKey" json:"id"`
	NodeID uint  `gorm:"not null;index" json:"node_id"`
	Node   *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	// Rule identification
	RuleTag  string `gorm:"size:100;not null" json:"rule_tag"` // Unique tag for this rule
	Remark   string `gorm:"size:200" json:"remark"`            // Human-readable description
	Priority int    `gorm:"default:0" json:"priority"`         // Lower = higher priority (processed first)
	Enabled  bool   `gorm:"default:true" json:"enabled"`

	// Target (where to send matched traffic) - only one should be set
	OutboundTag  string `gorm:"size:100" json:"outbound_tag"`  // Target outbound tag (e.g., "direct", "proxy", "block")
	BalancingTag string `gorm:"size:100" json:"balancing_tag"` // OR target balancer tag (for load balancing)

	// Destination Matchers (stored as JSONB for type safety and query capabilities)
	DomainRules   DomainMatcherSlice   `gorm:"serializer:json;type:jsonb" json:"domain_rules"`
	GeoIPRules    jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"geoip_rules"`
	IPCIDRRules   jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"ipcidr_rules"`
	PortRules     jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"port_rules"`
	NetworkRules  jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"network_rules"`
	ProtocolRules jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"protocol_rules"`
	InboundTags   jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"inbound_tags"`
	UserEmails    jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"user_emails"`

	// Source Matchers (matching source traffic)
	SourceIPs   jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"source_ips"`
	SourcePorts jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"source_ports"`

	// Advanced Matchers (xray-core parity)
	Attributes   jsontype.StringMap   `gorm:"serializer:json;type:jsonb" json:"attributes"`    // HTTP header attribute matching (key->regex)
	ProcessNames jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"process_names"` // Match by process name
	LocalIPs     jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"local_ips"`     // Match local/bind IPs
	LocalPorts   jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"local_ports"`   // Match local/bind ports

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// DomainMatcher defines a domain matching pattern
type DomainMatcher struct {
	Type  string `json:"type"`  // "plain", "regex", "domain", "full"
	Value string `json:"value"` // e.g., "google.com", "geosite:cn", "regexp:.*\\.google\\.com$"
}

// Domain matcher types
const (
	DomainTypePlain  = "plain"  // Contains substring (e.g., "google" matches "google.com")
	DomainTypeRegex  = "regex"  // Regular expression pattern
	DomainTypeDomain = "domain" // Domain and all subdomains (e.g., "google.com" matches "www.google.com")
	DomainTypeFull   = "full"   // Exact match only
)

// DomainMatcherSlice is a typed wrapper for []DomainMatcher stored as JSONB.
type DomainMatcherSlice []DomainMatcher

// Value implements driver.Valuer interface for database writes
func (d DomainMatcherSlice) Value() (driver.Value, error) {
	if d == nil {
		return nil, nil
	}
	return json.Marshal(d)
}

// Scan implements sql.Scanner interface for database reads
func (d *DomainMatcherSlice) Scan(value interface{}) error {
	if value == nil {
		*d = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("DomainMatcherSlice.Scan: expected []byte or string, got %T", value)
	}

	return json.Unmarshal(bytes, d)
}

// GetMatcherSummary returns a human-readable summary of all matchers
func (r *RoutingRule) GetMatcherSummary() string {
	var parts []string

	if len(r.DomainRules) > 0 {
		parts = append(parts, "domains")
	}
	if len(r.GeoIPRules) > 0 {
		parts = append(parts, "geoip")
	}
	if len(r.IPCIDRRules) > 0 {
		parts = append(parts, "ip")
	}
	if len(r.PortRules) > 0 {
		parts = append(parts, "ports")
	}
	if len(r.NetworkRules) > 0 {
		parts = append(parts, "network")
	}
	if len(r.ProtocolRules) > 0 {
		parts = append(parts, "protocol")
	}
	if len(r.InboundTags) > 0 {
		parts = append(parts, "inbound")
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}
