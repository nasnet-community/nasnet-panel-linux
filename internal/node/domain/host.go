package domain

import (
	"sort"
	"time"

	"gorm.io/gorm"
)

// HostFragmentSettings holds anti-censorship fragment configuration
type HostFragmentSettings struct {
	Packets  string `json:"packets,omitempty"`  // "tlshello", "1-3", etc.
	Length   string `json:"length,omitempty"`   // "100-200"
	Interval string `json:"interval,omitempty"` // "10-20"
}

// Host: presentation-layer template between an Inbound and the sub output.
// Controls how a config link appears; doesn't touch server-side xray.
// Inbound with 0 hosts = single link as before; 1+ hosts → one link per
// enabled host. Belongs to EITHER an Inbound (server host) OR a Plan
// (info-only placeholder VLESS link), never both.
type Host struct {
	ID        uint     `gorm:"primaryKey" json:"id"`
	InboundID *uint    `gorm:"index" json:"inbound_id"`
	Inbound   *Inbound `gorm:"foreignKey:InboundID" json:"inbound,omitempty"`
	PlanID    *uint    `gorm:"index" json:"plan_id"`

	// Display name template with variables:
	// {country}, {node}, {port}, {protocol}, {network}, {security},
	// {data_left}, {days_left}, {data_limit}, {usage_percent}
	Remark string `gorm:"size:255" json:"remark"`

	// Connection overrides (empty/nil = use Inbound's value)
	Address       string `gorm:"size:255" json:"address"`
	Port          *int   `json:"port"` // nil = inbound port
	SNI           string `gorm:"size:255" json:"sni"`
	Host          string `gorm:"size:255" json:"host"`
	Path          string `gorm:"size:255" json:"path"`
	ALPN          string `gorm:"size:100" json:"alpn"` // comma-separated
	Fingerprint   string `gorm:"size:50" json:"fingerprint"`
	Security      string `gorm:"size:20" json:"security"` // "", "tls", "reality", "none"
	AllowInsecure *bool  `json:"allow_insecure"`

	// Network-level overrides
	Mode       string `gorm:"size:30" json:"mode"`        // xhttp/splithttp mode: auto, packet-up, stream-up, stream-one
	HeaderType string `gorm:"size:20" json:"header_type"` // tcp header type: none, http

	// Fragment settings (anti-censorship)
	FragmentSettings *HostFragmentSettings `gorm:"serializer:json;type:jsonb" json:"fragment_settings"`

	// Tags for categorization
	Tags []string `gorm:"serializer:json;type:jsonb;default:'[]'" json:"tags"`

	Priority   int  `gorm:"default:0" json:"priority"`
	IsDisabled bool `gorm:"default:false" json:"is_disabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Host) TableName() string {
	return "hosts"
}

// IsInfoOnly returns true if this host is a plan-level info-only host
// (no inbound, just displays subscription stats in the client).
func (h *Host) IsInfoOnly() bool {
	return h.InboundID == nil && h.PlanID != nil
}

// HostTemplate is a reusable preset for quickly creating hosts with pre-filled connection overrides.
type HostTemplate struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:100;not null;uniqueIndex" json:"name"`
	Description string `gorm:"size:255" json:"description"`

	// Template fields (same as Host connection overrides)
	Remark           string                `gorm:"size:255" json:"remark"`
	Address          string                `gorm:"size:255" json:"address"`
	Port             *int                  `json:"port"`
	SNI              string                `gorm:"size:255" json:"sni"`
	Host             string                `gorm:"size:255" json:"host"`
	Path             string                `gorm:"size:255" json:"path"`
	ALPN             string                `gorm:"size:100" json:"alpn"`
	Fingerprint      string                `gorm:"size:50" json:"fingerprint"`
	Security         string                `gorm:"size:20" json:"security"`
	AllowInsecure    *bool                 `json:"allow_insecure"`
	Mode             string                `gorm:"size:30" json:"mode"`
	HeaderType       string                `gorm:"size:20" json:"header_type"`
	FragmentSettings *HostFragmentSettings `gorm:"serializer:json;type:jsonb" json:"fragment_settings"`
	Priority         *int                  `json:"priority"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (HostTemplate) TableName() string {
	return "host_templates"
}

// GetActiveHosts returns enabled hosts sorted by priority ASC, ID ASC.
// Returns nil if there are no active hosts.
func (i *Inbound) GetActiveHosts() []Host {
	if len(i.Hosts) == 0 {
		return nil
	}
	var active []Host
	for _, h := range i.Hosts {
		if !h.IsDisabled {
			active = append(active, h)
		}
	}
	if len(active) == 0 {
		return nil
	}
	sort.Slice(active, func(a, b int) bool {
		if active[a].Priority != active[b].Priority {
			return active[a].Priority < active[b].Priority
		}
		return active[a].ID < active[b].ID
	})
	return active
}
