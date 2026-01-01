package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

type ProvisioningRepository interface {
	// Enqueue adds a new task to the queue
	Enqueue(ctx context.Context, task *domain.ProvisioningTask) error

	// FetchPending gets tasks that are ready to be processed (Pending/Failed + Time)
	FetchPending(ctx context.Context, limit int) ([]*domain.ProvisioningTask, error)

	// UpdateStatus updates the status of a task (e.g. to PROCESSING)
	UpdateStatus(ctx context.Context, id uint, status domain.TaskStatus) error

	// MarkSuccess marks a task as completed
	MarkSuccess(ctx context.Context, id uint) error

	// MarkFailed increments retry count, updates error message, and sets next retry time
	MarkFailed(ctx context.Context, id uint, errStr string, nextRetry time.Time, isDead bool) error

	// CountPending returns the number of tasks waiting
	CountPending(ctx context.Context) (int64, error)

	// CancelTasksForNode marks all pending/processing/failed tasks for a node as DEAD/COMPLETED
	CancelTasksForNode(ctx context.Context, nodeID uint) error

	// CleanupCompletedTasks removes completed/dead tasks older than the specified days
	CleanupCompletedTasks(ctx context.Context, olderThanDays int) (int64, error)
}

type provisioningRepository struct {
	db *gorm.DB
}

func NewProvisioningRepository(db *gorm.DB) ProvisioningRepository {
	return &provisioningRepository{db: db}
}

func (r *provisioningRepository) Enqueue(ctx context.Context, task *domain.ProvisioningTask) error {
	// Default NextRetryAt to now if not set
	if task.NextRetryAt.IsZero() {
		task.NextRetryAt = time.Now()
	}
	return database.GetExecutor(r.db, ctx).Create(task).Error
}

func (r *provisioningRepository) FetchPending(ctx context.Context, limit int) ([]*domain.ProvisioningTask, error) {
	var tasks []*domain.ProvisioningTask

	// Fetch tasks that are PENDING or FAILED (retries), where NextRetryAt is in the past
	err := r.db.WithContext(ctx).
		Where("status IN ? AND next_retry_at <= ?", []domain.TaskStatus{domain.StatusPending, domain.StatusFailed}, time.Now()).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&tasks).Error

	return tasks, err
}

func (r *provisioningRepository) UpdateStatus(ctx context.Context, id uint, status domain.TaskStatus) error {
	return r.db.WithContext(ctx).
		Model(&domain.ProvisioningTask{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *provisioningRepository) MarkSuccess(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&domain.ProvisioningTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     domain.StatusCompleted,
			"last_error": "", // Clear error on success
		}).Error
}

func (r *provisioningRepository) MarkFailed(ctx context.Context, id uint, errStr string, nextRetry time.Time, isDead bool) error {
	updates := map[string]interface{}{
		"last_error":    errStr,
		"next_retry_at": nextRetry,
		"retry_count":   gorm.Expr("retry_count + ?", 1),
	}

	if isDead {
		updates["status"] = domain.StatusDead
	} else {
		updates["status"] = domain.StatusFailed
	}

	return r.db.WithContext(ctx).
		Model(&domain.ProvisioningTask{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *provisioningRepository) CountPending(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.ProvisioningTask{}).
		Where("status IN ?", []domain.TaskStatus{domain.StatusPending, domain.StatusFailed}).
		Count(&count).Error
	return count, err
}

func (r *provisioningRepository) CancelTasksForNode(ctx context.Context, nodeID uint) error {
	// Mark all incomplete tasks for this node as DEAD to stop processing
	return r.db.WithContext(ctx).
		Model(&domain.ProvisioningTask{}).
		Where("node_id = ? AND status IN ?", nodeID, []domain.TaskStatus{domain.StatusPending, domain.StatusProcessing, domain.StatusFailed}).
		Updates(map[string]interface{}{
			"status":     domain.StatusDead,
			"last_error": "Task cancelled due to node deletion",
		}).Error
}

func (r *provisioningRepository) CleanupCompletedTasks(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("status IN ? AND created_at < ?", []domain.TaskStatus{domain.StatusCompleted, domain.StatusDead}, cutoff).
		Delete(&domain.ProvisioningTask{})
	return result.RowsAffected, result.Error
}
