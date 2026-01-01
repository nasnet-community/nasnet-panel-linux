package domain

import (
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"gorm.io/gorm"
)

// BalancingRule configures load balancing between outbounds in xray-core routing.
type BalancingRule struct {
	ID     uint  `gorm:"primaryKey" json:"id"`
	NodeID uint  `gorm:"not null;index" json:"node_id"`
	Node   *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	Tag               string               `gorm:"size:100;not null" json:"tag"`
	OutboundSelectors jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"outbound_selectors"`
	Strategy          string               `gorm:"size:50;default:random" json:"strategy"` // random, leastping, roundrobin, leastload
	FallbackTag       string               `gorm:"size:100" json:"fallback_tag"`
	Enabled           bool                 `gorm:"default:true" json:"enabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// ValidBalancingStrategies lists all valid strategy names.
var ValidBalancingStrategies = map[string]bool{
	"random": true, "leastping": true, "roundrobin": true, "leastload": true,
}
