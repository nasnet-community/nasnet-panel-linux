// Package domain defines the hub-side model for the tunnel cores (SSH and
// MTProto). These live in their own schema (core_inbounds + core_users) rather
// than the Xray inbounds table, because they are a distinct subsystem with
// their own agent Core.
package domain

import (
	"encoding/json"
	"time"
)

// CoreProtocol identifies a tunnel protocol.
type CoreProtocol string

const (
	CoreProtoSSH     CoreProtocol = "ssh"
	CoreProtoMTProto CoreProtocol = "mtproto"
)

var validCoreProtocols = map[CoreProtocol]bool{
	CoreProtoSSH: true, CoreProtoMTProto: true,
}

// IsValid reports whether p is a known core protocol.
func (p CoreProtocol) IsValid() bool { return validCoreProtocols[p] }

// orderedCoreProtocols is the stable UI/display order.
var orderedCoreProtocols = []CoreProtocol{CoreProtoSSH, CoreProtoMTProto}

// AllCoreProtocols returns every known core protocol in a stable order.
func AllCoreProtocols() []CoreProtocol {
	out := make([]CoreProtocol, len(orderedCoreProtocols))
	copy(out, orderedCoreProtocols)
	return out
}

// EgressMode selects how a core's traffic leaves the node.
type EgressMode string

const (
	// EgressDirect: the core dials out through the node's filtered egress
	// dialer (the foreign pool in router mode).
	EgressDirect EgressMode = "direct"
	// EgressXray: the core dials out through a loopback SOCKS inbound in the
	// node's Xray config, so Xray's routing rules and outbounds apply.
	EgressXray EgressMode = "xray"
	// EgressHub: an MTProto node's Telegram traffic leaves through the panel; MTProto nodes use nothing else.
	EgressHub EgressMode = "hub"
)

// IsValid reports whether e is a known egress mode.
func (e EgressMode) IsValid() bool { return e == EgressDirect || e == EgressXray || e == EgressHub }

// CoreInbound is one protocol listener on one node.
type CoreInbound struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	NodeID uint `gorm:"not null;index" json:"node_id"`

	Protocol   CoreProtocol `gorm:"size:20;not null;index" json:"protocol"`
	Listen     string       `gorm:"size:50;default:''" json:"listen"` // bind addr, e.g. "0.0.0.0"
	Port       int          `gorm:"not null;index" json:"port"`
	EgressMode EgressMode   `gorm:"size:10;not null;default:'direct'" json:"egress_mode"`

	// Settings is the per-protocol JSONB blob, decoded by the matching
	// protocol package / agent Core. Protocol-agnostic at this layer.
	Settings json.RawMessage `gorm:"serializer:json;type:jsonb" json:"settings"`

	Remark     string `gorm:"size:100;default:''" json:"remark"`
	IsDisabled bool   `gorm:"not null;default:false" json:"is_disabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Users []CoreUser `gorm:"foreignKey:CoreInboundID;constraint:OnDelete:CASCADE" json:"users,omitempty"`
}

func (CoreInbound) TableName() string { return "core_inbounds" }

// CoreUser is one credential on one core_inbound. The full active set is pushed
// to the agent, which validates against it (the agent has no database).
type CoreUser struct {
	ID             uint  `gorm:"primaryKey" json:"id"`
	CoreInboundID  uint  `gorm:"not null;index;uniqueIndex:idx_core_user_inbound_email;uniqueIndex:idx_core_user_inbound_username" json:"core_inbound_id"`
	SubscriptionID *uint `gorm:"index" json:"subscription_id,omitempty"`

	// One email and one username per inbound: a duplicate email writes a
	// duplicate key into telemt's [access.users]; a duplicate username shadows
	// an SSH account.
	Username string `gorm:"size:64;not null;uniqueIndex:idx_core_user_inbound_username" json:"username"`
	Secret   string `gorm:"size:128;not null" json:"-"`                                                   // password / MTProto secret; never serialized
	Email    string `gorm:"size:128;not null;index;uniqueIndex:idx_core_user_inbound_email" json:"email"` // routing + accounting identity

	KLimit int `gorm:"not null;default:1" json:"k_limit"` // max concurrent devices

	// DataUsed is per-tunnel consumed bytes, attributed by the hub's per-node
	// traffic fold. Display-only (sub.DataUsed stays authoritative); resets
	// whenever sub.DataUsed resets. Starts at 0 — no historical backfill.
	DataUsed int64 `gorm:"default:0" json:"data_used"`
	// Enabled is always set explicitly by the hub (subscription active + within
	// quota). No GORM default: a default:true would make GORM omit a zero-value
	// false on Create, silently persisting a disabled user as enabled.
	Enabled bool `gorm:"not null" json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CoreUser) TableName() string { return "core_users" }

// NodeCoreSetting is a per-node override for whether a core protocol may run on
// that node. Absent row => enabled (pre-feature behavior; no backfill). A row is
// written only when an admin toggles the protocol. A protocol with zero inbounds
// can still be pre-emptively disabled, which blocks future inbound creation.
type NodeCoreSetting struct {
	NodeID   uint         `gorm:"primaryKey;autoIncrement:false" json:"node_id"`
	Protocol CoreProtocol `gorm:"primaryKey;size:20" json:"protocol"`
	// Enabled has no gorm default: an explicit value is always written on upsert,
	// so a zero-value false is never silently persisted as enabled (same rationale
	// as CoreUser.Enabled).
	Enabled bool `gorm:"not null" json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (NodeCoreSetting) TableName() string { return "node_core_settings" }
