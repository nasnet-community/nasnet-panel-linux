package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
	"gorm.io/gorm"
)

type NotificationRepository interface {
	// Create records a new notification
	Create(ctx context.Context, log *domain.NotificationLog) error

	// HasSentNotification checks if a notification of given type was already sent for this subscription
	HasSentNotification(ctx context.Context, subscriptionID uint, notifType domain.NotificationType) (bool, error)

	// HasSentNotificationToday checks if notification was sent today (for daily reset)
	HasSentNotificationToday(ctx context.Context, subscriptionID uint, notifType domain.NotificationType) (bool, error)

	// GetRecentNotifications gets recent notifications for a user
	GetRecentNotifications(ctx context.Context, userID uint, limit int) ([]*domain.NotificationLog, error)

	// CleanupOldNotifications removes notifications older than N days
	CleanupOldNotifications(ctx context.Context, olderThanDays int) error

	// DeleteBySubscriptionAndTypes removes the log rows of the given types for a
	// subscription, re-arming one-shot notifications (data-exhausted, expiry
	// reminders) for the next cycle after a renewal/reset/extension.
	DeleteBySubscriptionAndTypes(ctx context.Context, subscriptionID uint, notifTypes ...domain.NotificationType) error
}

type notificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Create(ctx context.Context, log *domain.NotificationLog) error {
	log.SentAt = time.Now()
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *notificationRepository) HasSentNotification(ctx context.Context, subscriptionID uint, notifType domain.NotificationType) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.NotificationLog{}).
		Where("subscription_id = ? AND type = ?", subscriptionID, notifType).
		Count(&count).Error
	return count > 0, err
}

func (r *notificationRepository) HasSentNotificationToday(ctx context.Context, subscriptionID uint, notifType domain.NotificationType) (bool, error) {
	today := time.Now().Truncate(24 * time.Hour)
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.NotificationLog{}).
		Where("subscription_id = ? AND type = ? AND sent_at >= ?", subscriptionID, notifType, today).
		Count(&count).Error
	return count > 0, err
}

func (r *notificationRepository) GetRecentNotifications(ctx context.Context, userID uint, limit int) ([]*domain.NotificationLog, error) {
	var logs []*domain.NotificationLog
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("sent_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

func (r *notificationRepository) CleanupOldNotifications(ctx context.Context, olderThanDays int) error {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	return r.db.WithContext(ctx).
		Where("sent_at < ?", cutoff).
		Delete(&domain.NotificationLog{}).Error
}

func (r *notificationRepository) DeleteBySubscriptionAndTypes(ctx context.Context, subscriptionID uint, notifTypes ...domain.NotificationType) error {
	if len(notifTypes) == 0 {
		return nil
	}
	types := make([]string, len(notifTypes))
	for i, t := range notifTypes {
		types[i] = string(t)
	}
	return r.db.WithContext(ctx).
		Where("subscription_id = ? AND type IN ?", subscriptionID, types).
		Delete(&domain.NotificationLog{}).Error
}
