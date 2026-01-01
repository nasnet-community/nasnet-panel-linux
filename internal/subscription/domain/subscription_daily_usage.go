package domain

import "time"

// SubscriptionDailyUsage: bytes per sub per UTC day. DataUsed is a delta
// (NOT cumulative), written only by the node stats sweep via
// AddDailyUsageSplit. DataUpload/DataDownload populated post-split-migration.
type SubscriptionDailyUsage struct {
	ID             uint      `gorm:"primaryKey"`
	SubscriptionID uint      `gorm:"uniqueIndex:idx_sub_date;not null"`
	Date           time.Time `gorm:"uniqueIndex:idx_sub_date;not null;type:date"`
	DataUsed       int64     `gorm:"default:0"`            // bytes used on Date (combined)
	DataUpload     *int64    `gorm:"column:data_upload"`   // bytes uploaded on Date; nil on legacy rows
	DataDownload   *int64    `gorm:"column:data_download"` // bytes downloaded on Date; nil on legacy rows
	CreatedAt      time.Time
}

func (SubscriptionDailyUsage) TableName() string {
	return "subscription_daily_usage"
}
