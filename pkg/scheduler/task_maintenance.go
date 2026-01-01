package scheduler

import (
	"context"
	"strconv"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// retentionDays: configured day count. -1 = "keep forever" (skip cleanup).
// Returns fallback if unset, unparseable, or negative.
func (s *Scheduler) retentionDays(ctx context.Context, key string, fallback int) int {
	if s.settingUC == nil {
		return fallback
	}
	val, err := s.settingUC.GetByKey(ctx, key)
	if err != nil || val == "" {
		return fallback
	}
	days, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	if days == 0 {
		return -1 // explicit "keep forever"
	}
	if days < 0 {
		return fallback
	}
	return days
}

// CleanupSummary is the per-task outcome of a retention sweep. Tasks that
// were skipped (nil dep, retention=0) do not appear in the map. Callers can
// use it to report "deleted N rows across M tables" for a manual-cleanup UI.
type CleanupSummary map[string]int64

// tryCleanup: read-setting → skip-if-forever → run → log + write deleted
// counts into summary. Non-(ctx, days) repo signatures adapt via closures.
func (s *Scheduler) tryCleanup(
	ctx context.Context,
	summary CleanupSummary,
	label, key string,
	fallback int,
	run func(ctx context.Context, days int) (int64, error),
) {
	days := s.retentionDays(ctx, key, fallback)
	if days < 0 {
		return // 0 = keep forever
	}
	log := logger.GetLogger()
	deleted, err := run(ctx, days)
	if err != nil {
		log.WithError(err).WithField("task", label).Warn("Retention: cleanup failed")
		return
	}
	summary[label] = deleted
	if deleted > 0 {
		log.WithFields(map[string]interface{}{
			"task":    label,
			"deleted": deleted,
			"days":    days,
		}).Info("Retention: cleaned up old rows")
	}
}

// runRetentionCleanup: 6h scheduler tick. RunRetentionNow wraps it for
// the admin manual cleanup endpoint.
func (s *Scheduler) runRetentionCleanup(ctx context.Context) CleanupSummary {
	summary := make(CleanupSummary)

	// Time-series / node metrics
	if s.nodeRepository != nil {
		s.tryCleanup(ctx, summary, "node_stats", "retention_node_stats_days", 30, s.nodeRepository.CleanupOldNodeStats)
		s.tryCleanup(ctx, summary, "node_daily_traffic", "retention_node_daily_traffic_days", 365, s.nodeRepository.CleanupOldNodeDailyTraffic)
		s.tryCleanup(ctx, summary, "node_uptime_events", "retention_node_uptime_events_days", 90, s.nodeRepository.CleanupOldUptimeEvents)
		s.tryCleanup(ctx, summary, "starlink_stats", "retention_starlink_stats_days", 30, s.nodeRepository.CleanupOldStarlinkStats)
		s.tryCleanup(ctx, summary, "access_log_summaries", "retention_access_log_days", 30, func(ctx context.Context, days int) (int64, error) {
			before := time.Now().AddDate(0, 0, -days)
			return s.nodeRepository.CleanupOldAccessLogSummaries(ctx, before)
		})
	}

	if s.onlineUsersCleaner != nil {
		s.tryCleanup(ctx, summary, "online_users_snapshots", "retention_online_users_history_days", 7, s.onlineUsersCleaner.CleanupOlderThan)
	}

	// Subscription activity
	if s.subIPRepo != nil {
		s.tryCleanup(ctx, summary, "subscription_ips", "retention_ip_days", 30, func(ctx context.Context, days int) (int64, error) {
			return s.subIPRepo.DeleteOldSubscriptionIPs(ctx, time.Now().AddDate(0, 0, -days))
		})
	}
	if s.subDailyUsageCleaner != nil {
		s.tryCleanup(ctx, summary, "subscription_daily_usage", "retention_subscription_daily_usage_days", 365, s.subDailyUsageCleaner.CleanupOldDailyUsage)
	}
	if s.usageRepo != nil {
		s.tryCleanup(ctx, summary, "user_daily_usage", "retention_user_daily_usage_days", 365, func(ctx context.Context, days int) (int64, error) {
			// Legacy signature returns only error; row counts unreported.
			return 0, s.usageRepo.DeleteOlderThan(ctx, time.Now().AddDate(0, 0, -days))
		})
	}

	// Operational
	if s.auditUC != nil {
		s.tryCleanup(ctx, summary, "audit_logs", "retention_audit_logs_days", 90, s.auditUC.Cleanup)
	}
	if s.provisioningRepo != nil {
		s.tryCleanup(ctx, summary, "provisioning_tasks", "retention_provisioning_tasks_days", 14, s.provisioningRepo.CleanupCompletedTasks)
	}
	if s.notifRepo != nil {
		s.tryCleanup(ctx, summary, "notification_logs", "retention_notification_logs_days", 30, func(ctx context.Context, days int) (int64, error) {
			return 0, s.notifRepo.CleanupOldNotifications(ctx, days)
		})
	}
	if s.alertEventCleaner != nil {
		s.tryCleanup(ctx, summary, "alert_events", "retention_alert_events_days", 180, s.alertEventCleaner.CleanupOldEvents)
	}

	// Chat cleanup is driven by the chat usecase's own settings-aware helper
	// (it also emits its own retention log), so just invoke the hook.
	if s.chatCleanupFn != nil {
		if err := s.chatCleanupFn(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("Retention: failed to cleanup chat messages")
		}
	}
	return summary
}

// RunRetentionNow: synchronous full retention sweep. Resets
// lastRetentionClean so the next automatic sweep doesn't re-run immediately.
func (s *Scheduler) RunRetentionNow(ctx context.Context) CleanupSummary {
	summary := s.runRetentionCleanup(ctx)
	s.mu.Lock()
	s.lastRetentionClean = time.Now()
	s.mu.Unlock()
	return summary
}
