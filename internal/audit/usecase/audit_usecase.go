package usecase

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

const bufferSize = 1000

type auditUsecase struct {
	repo    domain.AuditLogRepository
	logChan chan *domain.AuditLog
	done    chan struct{}
}

func NewAuditUsecase(repo domain.AuditLogRepository) domain.AuditLogUsecase {
	u := &auditUsecase{
		repo:    repo,
		logChan: make(chan *domain.AuditLog, bufferSize),
		done:    make(chan struct{}),
	}
	go u.writer()
	return u
}

// Log enqueues an audit entry without blocking the caller
func (u *auditUsecase) Log(ctx context.Context, entry *domain.AuditLog) {
	select {
	case u.logChan <- entry:
	default:
		logger.GetLogger().Warn("Audit log buffer full, dropping entry")
	}
}

// List returns audit logs matching the given filters
func (u *auditUsecase) List(ctx context.Context, filters domain.AuditListFilters) ([]*domain.AuditLog, int64, error) {
	return u.repo.List(ctx, filters)
}

// Cleanup removes audit logs older than the specified number of days
func (u *auditUsecase) Cleanup(ctx context.Context, days int) (int64, error) {
	return u.repo.CleanupOlderThan(ctx, days)
}

// Stop closes the log channel and waits for the writer to drain
func (u *auditUsecase) Stop() {
	close(u.logChan)
	<-u.done
}

// writer is a background goroutine that persists audit entries
func (u *auditUsecase) writer() {
	defer close(u.done)
	log := logger.GetLogger()
	for entry := range u.logChan {
		if err := u.repo.Create(context.Background(), entry); err != nil {
			log.WithError(err).WithField("action", entry.Action).Error("Failed to persist audit log")
		}
	}
}
