package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserDailyUsageRepository manages daily usage snapshots
type UserDailyUsageRepository interface {
	Upsert(ctx context.Context, entry *domain.UserDailyUsage) error
	ListByUserID(ctx context.Context, userID uint, from, to time.Time) ([]*domain.UserDailyUsage, error)
	DeleteOlderThan(ctx context.Context, before time.Time) error
}

type userDailyUsageRepository struct {
	db *gorm.DB
}

// NewUserDailyUsageRepository creates a new UserDailyUsageRepository
func NewUserDailyUsageRepository(db *gorm.DB) UserDailyUsageRepository {
	return &userDailyUsageRepository{db: db}
}

// Upsert inserts or updates a daily usage snapshot for (user_id, date)
func (r *userDailyUsageRepository) Upsert(ctx context.Context, entry *domain.UserDailyUsage) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "date"}},
			DoUpdates: clause.AssignmentColumns([]string{"data_used"}),
		}).
		Create(entry).Error
}

// ListByUserID returns usage records for a user within a date range, ordered by date ASC
func (r *userDailyUsageRepository) ListByUserID(ctx context.Context, userID uint, from, to time.Time) ([]*domain.UserDailyUsage, error) {
	var records []*domain.UserDailyUsage
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND date >= ? AND date <= ?", userID, from, to).
		Order("date ASC").
		Find(&records).Error
	return records, err
}

// DeleteOlderThan removes records older than the given timestamp
func (r *userDailyUsageRepository) DeleteOlderThan(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).
		Where("date < ?", before).
		Delete(&domain.UserDailyUsage{}).Error
}
