package usecase

import (
	"context"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
)

// GetRetentionStats returns per-table size + oldest-row data for the admin
// retention settings panel. Powered by the RetentionStatsRepository, which
// fans out one query per table concurrently.
func (u *adminUsecase) GetRetentionStats(ctx context.Context) ([]adminDomain.RetentionStat, error) {
	if u.retentionStatsRepo == nil {
		return nil, nil
	}
	return u.retentionStatsRepo.GetAll(ctx)
}

// RunRetentionCleanup runs the retention sweep synchronously via the hook
// wired in by the bootstrap code after the scheduler is built. Returns an
// empty map when no runner is configured (in tests, for instance) so the
// handler doesn't need to distinguish "nothing to clean" from "cleanup not
// available".
func (u *adminUsecase) RunRetentionCleanup(ctx context.Context) map[string]int64 {
	if u.retentionCleanupRunner == nil {
		return map[string]int64{}
	}
	return u.retentionCleanupRunner(ctx)
}

// SetRetentionCleanupRunner is the late-binding escape hatch for the
// bootstrap cycle: the admin usecase is built before the scheduler because
// the scheduler depends on it (RecordOnlineUsersSnapshot); then the
// scheduler's cleanup method is wired back in here so the admin endpoint
// can trigger it. Once set, it's a stable pointer for the process lifetime.
func (u *adminUsecase) SetRetentionCleanupRunner(run func(ctx context.Context) map[string]int64) {
	u.retentionCleanupRunner = run
}
