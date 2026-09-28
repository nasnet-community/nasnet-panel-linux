package domain

import "time"

// NodeCertSighting is the certificate a node last presented to the hub in a
// TLS handshake.
type NodeCertSighting struct {
	NodeID     uint      `gorm:"primaryKey;autoIncrement:false" json:"node_id"`
	Serial     string    `gorm:"size:100;not null" json:"serial"`
	CommonName string    `gorm:"size:255" json:"common_name"`
	NotAfter   time.Time `json:"not_after"`
	Mode       string    `gorm:"size:10" json:"mode"` // "direct" | "reverse"
	SeenAt     time.Time `json:"seen_at"`
}

func (NodeCertSighting) TableName() string {
	return "node_cert_sightings"
}
