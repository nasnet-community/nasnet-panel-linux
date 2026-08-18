package domain

import (
	"encoding/json"
	"time"
)

// RuleType enumerates the built-in alert kinds. Keep this closed-set —
// adding a new kind requires an evaluator implementation + seed entry.
type RuleType string

const (
	RuleTypeNodeOffline   RuleType = "node_offline"
	RuleTypeNodeCrashLoop RuleType = "node_crash_loop"
	RuleTypeHighCPU       RuleType = "high_cpu"
	RuleTypeHighDisk      RuleType = "high_disk"
)

// ScopeKind controls which entities a rule applies to. v1 ships `global`
// and `node_ids`; `tag` is reserved for when node tagging lands.
type ScopeKind string

const (
	ScopeGlobal  ScopeKind = "global"
	ScopeNodeIDs ScopeKind = "node_ids"
	ScopeTag     ScopeKind = "tag"
)

// Threshold is the serialised threshold payload. Field usage varies per
// RuleType — evaluators interpret what they need and ignore the rest:
//   - node_offline:     DurationSec (grace before firing)
//   - node_crash_loop:  Count + WindowSec
//   - high_cpu/disk:    Value (%) + DurationSec (sustain)
type Threshold struct {
	Value       float64 `json:"value,omitempty"`
	Count       int     `json:"count,omitempty"`
	WindowSec   int     `json:"window_sec,omitempty"`
	DurationSec int     `json:"duration_sec,omitempty"`
}

// Rule is the persisted alert definition. Sinks is reserved for per-rule
// channel overrides (phase B); v1 leaves it empty and relies on the
// global notification settings to decide routing.
type Rule struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	RuleType    RuleType  `gorm:"type:varchar(64);not null;index" json:"rule_type"`
	Scope       ScopeKind `gorm:"type:varchar(32);not null;default:'global'" json:"scope"`
	ScopeValue  string    `gorm:"type:text" json:"scope_value,omitempty"` // JSON-encoded scope payload
	Threshold   Threshold `gorm:"serializer:json;type:jsonb" json:"threshold"`
	CooldownSec int       `gorm:"default:900" json:"cooldown_sec"`
	Enabled     bool      `gorm:"default:false;index" json:"enabled"`
	Sinks       string    `gorm:"type:text" json:"sinks,omitempty"` // JSON — reserved, phase B
	// Free text straight from the rule form, length-unchecked at the API, so
	// it cannot carry a varchar limit into PostgreSQL.
	Description string     `gorm:"type:text" json:"description"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Rule) TableName() string { return "alert_rules" }

// ScopedNodeIDs decodes ScopeValue as a JSON array of node IDs.
// Returns nil slice (global) when scope != node_ids.
func (r *Rule) ScopedNodeIDs() []uint {
	if r.Scope != ScopeNodeIDs || r.ScopeValue == "" {
		return nil
	}
	var ids []uint
	if err := json.Unmarshal([]byte(r.ScopeValue), &ids); err != nil {
		return nil
	}
	return ids
}
