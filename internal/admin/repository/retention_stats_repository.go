package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

// retentionTable declares one table queried by the retention stats endpoint.
// The three fields are all compile-time constants — no user input ever flows
// into the generated SQL, so string interpolation here is safe.
type retentionTable struct {
	settingKey string
	table      string
	ageField   string // column used for "oldest row" (e.g. created_at, date)
}

// retentionTables is the canonical list of historical tables the retention
// sweep touches. Kept in sync with pkg/scheduler/task_maintenance.go — each
// entry there should appear here (and vice versa) so the stats endpoint
// reflects every tunable retention knob in the UI.
var retentionTables = []retentionTable{
	// Time-series / node metrics
	{"retention_node_stats_days", "node_stats", "created_at"},
	{"retention_node_daily_traffic_days", "node_daily_traffics", "date"},
	{"retention_node_uptime_events_days", "node_uptime_events", "timestamp"},
	{"retention_starlink_stats_days", "starlink_stats", "created_at"},
	{"retention_online_users_history_days", "online_users_snapshots", "created_at"},
	{"retention_access_log_days", "access_log_summaries", "hour_time"},
	// Subscription activity
	{"retention_ip_days", "subscription_ips", "last_seen"},
	{"retention_subscription_daily_usage_days", "subscription_daily_usages", "date"},
	{"retention_user_daily_usage_days", "user_daily_usages", "date"},
	// Operational
	{"retention_audit_logs_days", "audit_logs", "created_at"},
	{"retention_provisioning_tasks_days", "provisioning_tasks", "created_at"},
	{"retention_notification_logs_days", "notification_logs", "sent_at"},
	{"retention_alert_events_days", "alert_events", "created_at"},
	{"retention_chat_messages_days", "chat_messages", "created_at"},
}

// RetentionStatsRepository answers "how big / how old" for each retention
// table. One round-trip per table, all issued in parallel.
type RetentionStatsRepository interface {
	GetAll(ctx context.Context) ([]adminDomain.RetentionStat, error)
}

type retentionStatsRepository struct {
	db *gorm.DB
}

func NewRetentionStatsRepository(db *gorm.DB) RetentionStatsRepository {
	return &retentionStatsRepository{db: db}
}

// GetAll fans out COUNT + MIN queries across every retention-tracked table
// concurrently. A single table failure (missing table from a half-rolled
// migration, for instance) is logged and reported as zero rows rather than
// aborting the whole response — the UI can still render the rest of the list.
func (r *retentionStatsRepository) GetAll(ctx context.Context) ([]adminDomain.RetentionStat, error) {
	log := logger.GetLogger()
	results := make([]adminDomain.RetentionStat, len(retentionTables))

	var wg sync.WaitGroup
	for i, t := range retentionTables {
		wg.Add(1)
		go func(idx int, t retentionTable) {
			defer wg.Done()
			stat := adminDomain.RetentionStat{
				SettingKey: t.settingKey,
				Table:      t.table,
			}
			// Single pass pulls count + MIN(age) in one row to halve the
			// round-trip cost vs. issuing two queries.
			query := fmt.Sprintf(
				"SELECT COUNT(*), MIN(%s) FROM %s",
				t.ageField, t.table,
			)
			var count sql.NullInt64
			var oldest sql.NullTime
			row := r.db.WithContext(ctx).Raw(query).Row()
			if err := row.Scan(&count, &oldest); err != nil {
				log.WithError(err).WithField("table", t.table).
					Debug("retention stats: query failed; reporting zero")
				results[idx] = stat
				return
			}
			if count.Valid {
				stat.Rows = count.Int64
			}
			if oldest.Valid {
				// Copy so the pointer survives the closure scope.
				t := oldest.Time
				stat.OldestAt = &t
			}
			results[idx] = stat
		}(i, t)
	}
	wg.Wait()
	return results, nil
}
