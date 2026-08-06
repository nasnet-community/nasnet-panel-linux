package domain

import (
	"time"

	"gorm.io/gorm"
)

// FailoverPolicy is how a group picks among members
type FailoverPolicy string

const (
	PolicyFailover FailoverPolicy = "failover"
)

// WANGroup is a policy group
type WANGroup struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	Name string `gorm:"uniqueIndex:ux_wangroup_node_name;not null" json:"name"` // "domestic" | "foreign"

	// GroupIndex goes in the mark's 0x00FF0000 field. 1 = domestic, 2 = foreign.
	GroupIndex uint32 `gorm:"not null" json:"group_index"`

	// RuleBase/RuleBlackhole are this group's ip rule preferences: one member
	// rule per priority from RuleBase up, terminated by a blackhole so the
	// group fails closed.
	RuleBase      int `gorm:"not null" json:"rule_base"`
	RuleBlackhole int `gorm:"not null" json:"rule_blackhole"`

	Policy FailoverPolicy `gorm:"not null;default:'failover'" json:"policy"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// WANGroupMember binds an uplink to a group at a priority.
type WANGroupMember struct {
	ID      uint `gorm:"primarykey" json:"id"`
	GroupID uint `gorm:"index;not null" json:"group_id"`

	InterfaceID uint `gorm:"index;not null" json:"interface_id"`

	// Priority orders member rules within the group; lower wins. The kernel
	// walks to the next member itself when a table yields no route.
	Priority int `gorm:"not null;default:0" json:"priority"`

	// UplinkIndex goes in the mark's 0x0F000000 ingress-pin field. Separate
	// from the group on purpose: a group falls through to a sibling, a pin
	// must never.
	UplinkIndex uint32 `gorm:"not null" json:"uplink_index"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
