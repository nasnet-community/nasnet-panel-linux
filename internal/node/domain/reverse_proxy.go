package domain

import (
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"gorm.io/gorm"
)

// ReverseProxy represents a reverse proxy entry (bridge or portal) on a Node.
type ReverseProxy struct {
	ID     uint  `gorm:"primaryKey" json:"id"`
	NodeID uint  `gorm:"not null;uniqueIndex:idx_rp_node_tag" json:"node_id"`
	Node   *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	Type   string `gorm:"size:20;not null" json:"type"` // "bridge" or "portal"
	Tag    string `gorm:"size:100;not null;uniqueIndex:idx_rp_node_tag" json:"tag"`
	Domain string `gorm:"size:200;not null" json:"domain"` // Legacy metadata; VLESS Reverse has no control domain.

	// Bridge mode: outbound tag selections
	InterconnectionTag string `gorm:"size:100" json:"interconnection_tag"`
	OutboundTag        string `gorm:"size:100" json:"outbound_tag"`

	// Portal mode: inbound tag selections
	InterconnectionTags jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"interconnection_tags"`
	InboundTags         jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"inbound_tags"`

	// Rule1ID is an obsolete legacy control rule, removed on edit.
	// Rule2ID references the managed VLESS Reverse traffic rule (not a GORM FK).
	Rule1ID *uint `json:"rule1_id"`
	Rule2ID *uint `json:"rule2_id"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReverseProxy) TableName() string { return "reverse_proxies" }
