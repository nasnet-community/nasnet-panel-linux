package domain

import "time"

// State tracks whether a (rule, entity) pair is currently firing and
// when it last notified. Primary key is composite so one rule can fire
// independently per entity (e.g. high_cpu fires per node).
type State struct {
	RuleID           uint       `gorm:"primaryKey" json:"rule_id"`
	EntityKey        string     `gorm:"primaryKey;type:varchar(128)" json:"entity_key"`
	Firing           bool       `gorm:"default:false" json:"firing"`
	FirstTriggeredAt *time.Time `json:"first_triggered_at,omitempty"`
	LastNotifiedAt   *time.Time `json:"last_notified_at,omitempty"`
	LastSeenValue    string     `gorm:"type:text" json:"last_seen_value,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (State) TableName() string { return "alert_state" }
