package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	"gorm.io/gorm"
)

// OnlineUsersSnapshotRepository persists global online-user count snapshots.
type OnlineUsersSnapshotRepository interface {
	Create(ctx context.Context, snapshot *domain.OnlineUsersSnapshot) error
	ListSince(ctx context.Context, since time.Time) ([]*domain.OnlineUsersSnapshot, error)
	CleanupOlderThan(ctx context.Context, olderThanDays int) (int64, error)
}

type onlineUsersSnapshotRepository struct {
	db *gorm.DB
}

func NewOnlineUsersSnapshotRepository(db *gorm.DB) OnlineUsersSnapshotRepository {
	return &onlineUsersSnapshotRepository{db: db}
}

func (r *onlineUsersSnapshotRepository) Create(ctx context.Context, snapshot *domain.OnlineUsersSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *onlineUsersSnapshotRepository) ListSince(ctx context.Context, since time.Time) ([]*domain.OnlineUsersSnapshot, error) {
	var rows []*domain.OnlineUsersSnapshot
	err := r.db.WithContext(ctx).
		Where("created_at >= ?", since).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *onlineUsersSnapshotRepository) CleanupOlderThan(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&domain.OnlineUsersSnapshot{})
	return result.RowsAffected, result.Error
}
