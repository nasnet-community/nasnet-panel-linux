package domain

import "time"

// SubscriptionDailyUsage: bytes per sub per UTC day. DataUsed is a delta
// (NOT cumulative), written only by the node stats sweep via
// AddDailyUsageSplit. Upload and download are always recorded together.
type SubscriptionDailyUsage struct {
	ID             uint      `gorm:"primaryKey"`
	SubscriptionID uint      `gorm:"uniqueIndex:idx_sub_date;not null"`
	Date           time.Time `gorm:"uniqueIndex:idx_sub_date;not null;type:date"`
	DataUsed       int64     `gorm:"default:0"`                               // bytes used on Date (combined)
	DataUpload     int64     `gorm:"column:data_upload;not null;default:0"`   // bytes uploaded on Date
	DataDownload   int64     `gorm:"column:data_download;not null;default:0"` // bytes downloaded on Date
	CreatedAt      time.Time
}

func (SubscriptionDailyUsage) TableName() string {
	return "subscription_daily_usage"
}
