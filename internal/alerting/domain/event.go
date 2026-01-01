package domain

import "time"

// EventStatus is the state transition recorded in the audit log.
type EventStatus string

const (
	EventStatusFired    EventStatus = "fired"
	EventStatusResolved EventStatus = "resolved"
)

// Event is an immutable audit row: "this rule fired for this entity at
// this time". Retained indefinitely for now; add a retention policy if
// the table grows past a few million rows.
type Event struct {
	ID        uint        `gorm:"primaryKey" json:"id"`
	RuleID    uint        `gorm:"not null;index" json:"rule_id"`
	EntityKey string      `gorm:"type:varchar(128);index" json:"entity_key"`
	Status    EventStatus `gorm:"type:varchar(16);not null" json:"status"`
	Title     string      `gorm:"size:200" json:"title"`
	Message   string      `gorm:"type:text" json:"message"`
	ValueJSON string      `gorm:"type:text" json:"value_json,omitempty"`
	CreatedAt time.Time   `gorm:"index:,sort:desc" json:"created_at"`
}

func (Event) TableName() string { return "alert_events" }
