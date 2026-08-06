// Package domain holds the network models
// .network files, ip rules, routes and the nft table are rendered from these
// rows by one reconciler.
//
// SCHEMA IS ADDITIVE ONLY. No migration framework — AutoMigrate plus ad-hoc
// db.Exec for indexes, and AutoMigrate will not rename, drop or backfill.
// Every change must be a new column with an app-level default.
package domain

import (
	"time"

	"gorm.io/gorm"
)

// InterfaceRole is what an interface is for. Typed column, not a substring of
// a free-text comment.
type InterfaceRole string

const (
	RoleUnassigned InterfaceRole = "unassigned" // networkd leaves it alone (Default)
	RoleWAN        InterfaceRole = "wan"        // uplink, belongs to exactly one group
	RoleLAN        InterfaceRole = "lan"        // client facing L3 segment. Singleton.
	RoleLANMember  InterfaceRole = "lan_member" // enslaved to the LAN bridge, no L3 of its own
	RoleMgmt       InterfaceRole = "mgmt"       // reserved recovery port. Singleton.
)

func AllRoles() []InterfaceRole {
	return []InterfaceRole{RoleUnassigned, RoleWAN, RoleLAN, RoleLANMember, RoleMgmt}
}

func (r InterfaceRole) Valid() bool {
	switch r {
	case RoleUnassigned, RoleWAN, RoleLAN, RoleLANMember, RoleMgmt:
		return true
	}
	return false
}

// IsSingleton must agree with ux_netif_singleton_role, or app validation and
// the DB disagree.
func (r InterfaceRole) IsSingleton() bool {
	return r == RoleLAN || r == RoleMgmt
}

// UplinkSlot is the operator-facing name for an uplink. Stage 1 has two groups
// of one member, so exposing groups buys nothing. WANGroupMember rows sit
// behind these unchanged, so multi-member groups later need no migration.
type UplinkSlot string

const (
	SlotNone      UplinkSlot = ""
	SlotDomestic  UplinkSlot = "domestic"
	SlotSecondary UplinkSlot = "secondary"
)

// AddressMethod is how an uplink gets its address.
type AddressMethod string

const (
	MethodDHCP4  AddressMethod = "dhcp4"
	MethodStatic AddressMethod = "static"
	MethodRawIP  AddressMethod = "rawip" // cellular, networkd cannot DHCP these
)

// NetworkInterface is one NIC's persisted role and config. Rows survive the
// device vanishing (Present=false plus LastSeenAt, so a dongle keeps its role across a replug)
type NetworkInterface struct {
	ID uint `gorm:"primarykey" json:"id"`

	// NodeID scopes the singleton index and matches every other model here.
	// Single-node deployments use 1.
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	// Key is the stable identity. Anything but KeyKind "permaddr" ties the role
	// to a port, not a device.
	Key     string `gorm:"index;not null" json:"key"`
	KeyKind string `gorm:"not null;default:'ifname'" json:"key_kind"`

	IfName  string `gorm:"not null" json:"if_name"`
	PermMAC string `json:"perm_mac"`
	IDPath  string `json:"id_path"`

	// Label is operator text shown over the kernel name. Never parsed.
	Label string `json:"label"`

	Source           string `gorm:"not null;default:'unknown'" json:"source"`
	SourceOverride   string `json:"source_override"`
	SourceConfidence int    `gorm:"not null;default:0" json:"source_confidence"`

	Role InterfaceRole `gorm:"index;not null;default:'unassigned'" json:"role"`
	Slot UplinkSlot    `gorm:"index;not null;default:''" json:"slot"`

	// MasterID points at the RoleLAN row when Role is RoleLANMember.
	MasterID *uint `json:"master_id"`

	Method        AddressMethod `gorm:"not null;default:'dhcp4'" json:"method"`
	StaticAddress string        `json:"static_address"` // CIDR, e.g. 192.168.1.34/24
	StaticGateway string        `json:"static_gateway"`
	DNSServer     string        `json:"dns_server"`  // per link DNS= for resolved
	DNSDomains    string        `json:"dns_domains"` // per link Domains=, e.g. "~ir" or "~."
	RouteTable    int           `gorm:"not null;default:0" json:"route_table"`

	// Ephemeral sources (phone tethers) get a high metric, no RequiredForOnline,
	// and do not satisfy the two-uplink minimum.
	Ephemeral bool `gorm:"not null;default:false" json:"ephemeral"`

	// Health, maintained by the probe loop. Never written to xray's config.
	Healthy    bool   `gorm:"not null;default:false" json:"healthy"`
	ForceState string `gorm:"not null;default:''" json:"force_state"` // "", "up", "down"

	Present    bool       `gorm:"not null;default:true" json:"present"`
	LastSeenAt *time.Time `json:"last_seen_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
