package domain

import "time"

// UserDailyUsage stores cumulative data usage snapshots per user per day
type UserDailyUsage struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"uniqueIndex:idx_user_date;not null"`
	Date      time.Time `gorm:"uniqueIndex:idx_user_date;not null;type:date"`
	DataUsed  int64     `gorm:"default:0"` // cumulative bytes snapshot
	CreatedAt time.Time
}

func (UserDailyUsage) TableName() string {
	return "user_daily_usage"
}
