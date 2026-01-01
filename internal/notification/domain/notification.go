package domain

import (
	"time"

	"gorm.io/gorm"
)

// NotificationType represents different types of subscription notifications
type NotificationType string

const (
	NotificationTypeExpiry7Days   NotificationType = "expiry_7d"
	NotificationTypeExpiry3Days   NotificationType = "expiry_3d"
	NotificationTypeExpiry1Day    NotificationType = "expiry_1d"
	NotificationTypeExpired       NotificationType = "expired"
	NotificationTypeDataWarning   NotificationType = "data_warning" // 80% data used
	NotificationTypeDataExhausted NotificationType = "data_exhausted"
)

// NotificationLog tracks sent notifications to prevent duplicates
type NotificationLog struct {
	ID             uint             `gorm:"primaryKey"`
	UserID         uint             `gorm:"not null;index"`
	SubscriptionID uint             `gorm:"not null;index"`
	Type           NotificationType `gorm:"type:varchar(50);not null;index"`
	SentAt         time.Time        `gorm:"not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

func (NotificationLog) TableName() string {
	return "notification_logs"
}
