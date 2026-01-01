package domain

import "time"

// OnlineUsersSnapshot: deduped global online-user count from
// pkg/cache.GetOnlineCount() at each stats tick. Not derivable from
// NodeStat (per-node sum would double-count multi-node users).
type OnlineUsersSnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Count     int       `gorm:"not null" json:"count"`
	CreatedAt time.Time `gorm:"index;not null" json:"created_at"`
}
