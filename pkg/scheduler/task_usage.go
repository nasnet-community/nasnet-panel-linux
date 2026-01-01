package scheduler

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// recordDailyUsageSnapshots creates cumulative daily usage records per user
func (s *Scheduler) recordDailyUsageSnapshots(ctx context.Context) {
	if s.usageRepo == nil {
		return
	}
	log := logger.GetLogger()

	// Get all active subscriptions to aggregate usage by user
	activeSubs, err := s.subUsecase.ListAllSubscriptions(ctx, SubStatusActive, 0, 100000)
	if err != nil {
		log.WithError(err).Warn("Failed to list active subs for usage snapshot")
		return
	}

	// Group data_used by user_id
	userUsage := make(map[uint]int64)
	for _, sub := range activeSubs {
		uid := sub.GetUserID()
		if uid > 0 {
			userUsage[uid] += sub.LifetimeDataUsed
		}
	}

	today := time.Now().Truncate(24 * time.Hour)
	upserted := 0
	for userID, dataUsed := range userUsage {
		entry := &UserDailyUsage{
			UserID:   userID,
			Date:     today,
			DataUsed: dataUsed,
		}
		if err := s.usageRepo.Upsert(ctx, entry); err != nil {
			log.WithError(err).WithField("user_id", userID).Warn("Failed to upsert daily usage")
			continue
		}
		upserted++
	}

	if upserted > 0 {
		log.WithField("upserted", upserted).Info("Daily usage snapshots recorded")
	}

	// Per-subscription daily usage is owned by the node stats sweep's
	// AddDailyUsageSplit path (delta semantics keyed on UTC day). Do not
	// write cumulative sub.DataUsed here — the two writers used to clash
	// and produce mixed cumulative+delta rows for the same (sub, date).
}
