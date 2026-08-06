package domain

import (
	"time"

	"gorm.io/gorm"
)

// ApplyPhase tracks a two phase network change
type ApplyPhase string

const (
	PhasePlanned    ApplyPhase = "planned"
	PhaseApplied    ApplyPhase = "applied" // dead hang is enabled
	PhaseConfirmed  ApplyPhase = "confirmed"
	PhaseRolledBack ApplyPhase = "rolled_back"
	PhaseFailed     ApplyPhase = "failed"
)

// ApplyRecord is the audit trail and the takeover flag.
type ApplyRecord struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	Phase ApplyPhase `gorm:"index;not null" json:"phase"`
	// Ops is the human readable operation list shown before applying
	Ops []string `gorm:"serializer:json" json:"ops"`

	SnapshotPath string     `json:"snapshot_path"`
	Deadline     *time.Time `json:"deadline"`
	// PerformedTakeover records that this apply moved netplan aside, disabled cloud-init networking and masked NetworkManager
	PerformedTakeover bool   `gorm:"not null;default:false" json:"performed_takeover"`
	Error             string `json:"error"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
