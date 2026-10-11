// Package mtprotonode drives MTProto (lite) nodes, whose Telegram traffic leaves through the panel.
package mtprotonode

import (
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
)

// LiteNode is what a lite node needs beyond its nodes row.
type LiteNode struct {
	NodeID      uint   `gorm:"primaryKey" json:"node_id"`
	AgentID     string `gorm:"size:36;not null" json:"agent_id"`
	HubPort     int    `gorm:"not null;default:8443" json:"hub_port"`
	CapturePort int    `gorm:"not null;default:9443" json:"capture_port"`
	SSHPort     int    `gorm:"not null;default:22" json:"ssh_port"`
	SSHUser     string `gorm:"size:64;not null;default:'root'" json:"ssh_user"`
	HostKey     string `gorm:"size:100" json:"host_key"`

	// Link credentials. The REALITY private key lives only on the agent.
	RealityPub string `gorm:"size:64;not null" json:"-"`  // hex, 32 bytes
	ShortID    string `gorm:"size:16;not null" json:"-"`  // hex, 8 bytes
	Token      string `gorm:"size:128;not null" json:"-"` // hex, 32 bytes: a secret
	// NextSeed: an unconfirmed rotation the agent may hold (hex, secret); empty when none
	NextSeed string `gorm:"size:64" json:"-"`
	// KeysRotatedAt: when the credentials last changed; nil means the ones from the install
	KeysRotatedAt *time.Time `json:"keys_rotated_at,omitempty"`

	// Disguise is the cover site in use; our SNI must match it.
	Disguise   string `gorm:"size:255;not null" json:"disguise"`
	MaxStreams int    `gorm:"not null;default:5000" json:"max_streams"`
	// ProxyStopped: the operator stopped telemt; it stays off until started again
	ProxyStopped bool `gorm:"not null;default:false" json:"proxy_stopped"`

	Arch          string     `gorm:"size:10" json:"arch"` // amd64 | arm64, from the install; updates need it
	AgentVersion  string     `gorm:"size:40" json:"agent_version"`
	TelemtVersion string     `gorm:"size:40" json:"telemt_version"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (LiteNode) TableName() string { return "lite_nodes" }

// UsageBaseline survives a restart, so traffic is neither lost nor repeated.
type UsageBaseline struct {
	NodeID uint   `gorm:"primaryKey"`
	Name   string `gorm:"primaryKey;size:190"`
	Up     uint64
	Down   uint64
}

func (UsageBaseline) TableName() string { return "lite_usage_baselines" }

func (l LiteNode) record() relayops.Record {
	return relayops.Record{
		NodeID: l.NodeID, AgentID: l.AgentID, HubPort: l.HubPort, CapturePort: l.CapturePort,
		SSHPort: l.SSHPort, SSHUser: l.SSHUser, HostKey: l.HostKey,
		RealityPub: l.RealityPub, ShortID: l.ShortID, Token: l.Token, NextSeed: l.NextSeed, KeysRotatedAt: l.KeysRotatedAt,
		Disguise: l.Disguise, MaxStreams: l.MaxStreams, Arch: l.Arch,
		AgentVersion: l.AgentVersion, TelemtVersion: l.TelemtVersion, LastSeenAt: l.LastSeenAt,
	}
}

func (l *LiteNode) apply(r relayops.Record) {
	l.NodeID, l.AgentID, l.HubPort, l.CapturePort = r.NodeID, r.AgentID, r.HubPort, r.CapturePort
	l.SSHPort, l.SSHUser, l.HostKey = r.SSHPort, r.SSHUser, r.HostKey
	l.RealityPub, l.ShortID, l.Token, l.NextSeed = r.RealityPub, r.ShortID, r.Token, r.NextSeed
	l.KeysRotatedAt = r.KeysRotatedAt
	l.Disguise, l.MaxStreams, l.Arch = r.Disguise, r.MaxStreams, r.Arch
	l.AgentVersion, l.TelemtVersion, l.LastSeenAt = r.AgentVersion, r.TelemtVersion, r.LastSeenAt
}
