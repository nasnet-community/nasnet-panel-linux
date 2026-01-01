package domain

import (
	"time"

	"gorm.io/gorm"
)

// AgentCertificate stores certificates for agent mTLS authentication
type AgentCertificate struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// Certificate type: "ca", "master", "agent"
	Type string `gorm:"size:20;not null;index" json:"type"`

	// For agent certs, link to the node
	NodeID *uint `gorm:"index" json:"node_id,omitempty"`
	Node   *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	// Certificate details
	CommonName   string `gorm:"size:255;not null" json:"common_name"`
	SerialNumber string `gorm:"size:100;uniqueIndex" json:"serial_number"`

	// PEM-encoded certificate (stored encrypted in production)
	Certificate []byte `gorm:"not null" json:"-"`
	PrivateKey  []byte `json:"-"` // Only stored for CA, not for agent certs

	// Validity period
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`

	// Status
	IsRevoked bool       `gorm:"default:false" json:"is_revoked"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	// Auto-renew (for public certs)
	AutoRenew bool `gorm:"default:false" json:"auto_renew"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// IsExpired checks if the certificate has expired
func (c *AgentCertificate) IsExpired() bool {
	return time.Now().After(c.NotAfter)
}

// IsValid checks if the certificate is currently valid
func (c *AgentCertificate) IsValid() bool {
	now := time.Now()
	return !c.IsRevoked && now.After(c.NotBefore) && now.Before(c.NotAfter)
}

// DaysUntilExpiry returns the number of days until the certificate expires
func (c *AgentCertificate) DaysUntilExpiry() int {
	return int(time.Until(c.NotAfter).Hours() / 24)
}

func (AgentCertificate) TableName() string {
	return "agent_certificates"
}

// Certificate types
const (
	CertTypeCA     = "ca"
	CertTypeMaster = "master"
	CertTypeAgent  = "agent"
	CertTypePublic = "public"
)
