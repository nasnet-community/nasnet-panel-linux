package domain

import "time"

// InboundSNI is the queryable link between an inbound and the SNI domain whose
// certificate it serves. The authoritative copy still lives inside the inbound's
// TLSSettings JSON; this table mirrors it so we can answer "which nodes use this
// cert" without scanning every inbound — needed for renewal re-push, the
// delete-guard, and the "used by N" UI.
type InboundSNI struct {
	InboundID uint      `gorm:"primaryKey" json:"inbound_id"`
	SNIID     uint      `gorm:"index;not null" json:"sni_id"`
	NodeID    uint      `gorm:"index;not null" json:"node_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (InboundSNI) TableName() string { return "inbound_sni" }
