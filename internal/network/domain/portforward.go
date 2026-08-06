package domain

import (
	"time"

	"gorm.io/gorm"
)

// PortForward is one DNAT rule. The whole nat_pre chain is regenerated from
// these rows on every change, so no uplink address ever appears in a rule.
// Used by (LAN)
type PortForward struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	// UplinkKey names the interface the forward is accepted on (empty means any)
	UplinkKey string `gorm:"not null;default:''" json:"uplink_key"`

	Proto   string `gorm:"not null" json:"proto"` // "tcp" | "udp"
	DPort   int    `gorm:"not null" json:"dport"`
	ToAddr  string `gorm:"not null" json:"to_addr"`
	ToPort  int    `gorm:"not null" json:"to_port"`
	Comment string `json:"comment"`
	Enabled bool   `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
