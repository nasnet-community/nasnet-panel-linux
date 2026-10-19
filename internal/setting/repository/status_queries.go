package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

// StatusQueries answers the counts the settings status endpoint shows.
type StatusQueries struct {
	db *gorm.DB
}

func NewStatusQueries(db *gorm.DB) *StatusQueries {
	return &StatusQueries{db: db}
}

// ActiveReach counts users with an active subscription the way the
// maintenance broadcast selects them, and how many of those have Telegram.
func (q *StatusQueries) ActiveReach(ctx context.Context) (total, telegram int64, err error) {
	err = q.db.WithContext(ctx).Raw(`
		SELECT COUNT(DISTINCT u.id), COUNT(DISTINCT CASE WHEN u.telegram_id > 0 THEN u.id END)
		FROM users u JOIN subscriptions s ON s.user_id = u.id
		WHERE s.status = 'active' AND u.is_banned = ? AND u.deleted_at IS NULL`, false).
		Row().Scan(&total, &telegram)
	return total, telegram, err
}

// DBSize is the database's size on disk.
func (q *StatusQueries) DBSize(ctx context.Context) (int64, error) {
	db := q.db.WithContext(ctx)
	var n int64
	if database.DriverName == "postgres" {
		err := db.Raw("SELECT pg_database_size(current_database())").Row().Scan(&n)
		return n, err
	}
	var pages, size int64
	if err := db.Raw("PRAGMA page_count").Row().Scan(&pages); err != nil {
		return 0, err
	}
	if err := db.Raw("PRAGMA page_size").Row().Scan(&size); err != nil {
		return 0, err
	}
	return pages * size, nil
}
