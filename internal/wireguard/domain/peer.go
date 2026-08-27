package domain

import "time"

type WGPeerStatus string

const (
	WGPeerStatusActive   WGPeerStatus = "active"   // in the server config
	WGPeerStatusDisabled WGPeerStatus = "disabled" // kept but pulled from config (sub expired)
)

// WGPeer is one user device = one WireGuard peer on one inbound.
// No soft-delete: remove = hard delete to free the IP.
type WGPeer struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	SubscriptionID uint `gorm:"index;not null" json:"subscription_id"`
	InboundID      uint `gorm:"index;not null;uniqueIndex:idx_wgpeer_inbound_ip" json:"inbound_id"`

	HostID *uint `gorm:"index" json:"host_id,omitempty"`

	Label string `gorm:"size:64" json:"label"`

	PublicKey    string `gorm:"size:64;uniqueIndex;not null" json:"public_key"`
	PresharedKey string `gorm:"size:64;not null" json:"-"`
	PrivateKey   string `gorm:"size:64;not null;default:''" json:"-"`
	AssignedIP   string `gorm:"size:64;not null;uniqueIndex:idx_wgpeer_inbound_ip" json:"assigned_ip"` // bare IP, rendered as /32

	Status WGPeerStatus `gorm:"size:20;default:'active';index" json:"status"`

	// Cumulative traffic; last_seen = last stats cycle with traffic.
	UpBytes   int64      `gorm:"not null;default:0" json:"up_bytes"`
	DownBytes int64      `gorm:"not null;default:0" json:"down_bytes"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (WGPeer) TableName() string { return "wg_peers" }
