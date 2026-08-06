package domain

import (
	"time"

	"gorm.io/gorm"
)

// LANConfig is the client facing segment
type LANConfig struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	BridgeName    string `gorm:"not null;default:'lan0'" json:"bridge_name"`
	CIDR          string `gorm:"not null;default:'10.77.0.1/24'" json:"cidr"` // 10.77.0.0/24 by default
	DHCPRangeLow  string `gorm:"not null;default:'10.77.0.100'" json:"dhcp_range_low"`
	DHCPRangeHigh string `gorm:"not null;default:'10.77.0.200'" json:"dhcp_range_high"`
	LeaseHours    int    `gorm:"not null;default:12" json:"lease_hours"`
	Enabled       bool   `gorm:"not null;default:false" json:"enabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// WifiConfig is an AP or station config
type WifiConfig struct {
	ID          uint `gorm:"primarykey" json:"id"`
	InterfaceID uint `gorm:"index;not null" json:"interface_id"`

	Mode string `gorm:"not null;default:'ap'" json:"mode"` // "ap" | "station"
	SSID string `json:"ssid"`
	// PSK is stored as written. Treat like other secrets: never logged, never
	// returned by a list endpoint.
	PSK string `json:"-"`
	// CountryCode is mandatory before hostapd starts: the default regdomain
	// "00" marks nearly all 5 GHz NO_IR and hostapd dies with "Channel N
	// (primary) not allowed for AP mode".
	CountryCode string `json:"country_code"`
	Band        string `gorm:"not null;default:'2g'" json:"band"`
	Channel     int    `gorm:"not null;default:0" json:"channel"`
	Hidden      bool   `gorm:"not null;default:false" json:"hidden"`
	Enabled     bool   `gorm:"not null;default:false" json:"enabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
