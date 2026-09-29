package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/gorm"
)

// Connectivity history storage: the transition columns on `nodes`, the
// debounce candidate, the hub's own lifecycle rows, and the daily rollup.
// The transition *events* themselves still live with the other stats
// operations in node_repository.go.
//
// SQLite compares times as text, so a stored zone that differs from the bound
// one compares wrong. The connectivity writer stores event timestamps, hub
// lifecycle rows and rollup dates in UTC, and every bound here is UTC too —
// which also survives the router's timezone changing between runs. The
// instants don't change, so Postgres is unaffected.

// UpdateNodeConnectivity commits one confirmed transition: the flag, the
// "last seen looking" stamp, and whichever transition column applies. Written
// as one UPDATE so a reader can never observe is_online disagreeing with the
// timestamps. Clears the debounce candidate — by the time a transition is
// committed the window is resolved either way.
func (r *nodeRepository) UpdateNodeConnectivity(ctx context.Context, id uint, isOnline bool, at time.Time, reason string) error {
	fields := map[string]interface{}{
		"is_online":               isOnline,
		"last_check":              time.Now(),
		"offline_candidate_since": nil,
	}
	if isOnline {
		fields["last_connected_at"] = at
	} else {
		fields["last_disconnected_at"] = at
		fields["last_offline_reason"] = reason
	}
	// Model+Where (not Save) so a concurrent soft-delete isn't resurrected.
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", id).Updates(fields).Error
}

// SetOfflineCandidate records (or clears) the start of an unconfirmed outage.
// Persisted rather than kept in memory so a hub restart inside the debounce
// window still attributes the outage to when it actually began.
func (r *nodeRepository) SetOfflineCandidate(ctx context.Context, id uint, since *time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", id).
		Update("offline_candidate_since", since).Error
}

// BackfillNodeTransitionColumns seeds the transition columns from existing
// uptime events, once, for nodes that have never had one written. Without it
// every node reads "never disconnected" the moment this ships, even ones that
// flapped yesterday. Idempotent: only fills NULLs.
func (r *nodeRepository) BackfillNodeTransitionColumns(ctx context.Context) error {
	db := r.db.WithContext(ctx)
	// Newest event of each status per node. Correlated subqueries keep this
	// portable across SQLite and Postgres (no DISTINCT ON, no window funcs).
	for _, spec := range []struct {
		column string
		status string
	}{
		{"last_disconnected_at", domain.NodeStatusOffline},
		{"last_connected_at", domain.NodeStatusOnline},
	} {
		sql := `UPDATE nodes SET ` + spec.column + ` = (
			SELECT MAX(e.timestamp) FROM node_uptime_events e
			WHERE e.node_id = nodes.id AND e.status = ?
		) WHERE ` + spec.column + ` IS NULL`
		if err := db.Exec(sql, spec.status).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetUptimeEventsRange returns one node's events inside a window, ordered
// oldest first. The rollup needs a bounded range; GetUptimeEvents (open-ended
// "since") is what the timeline uses.
func (r *nodeRepository) GetUptimeEventsRange(ctx context.Context, nodeID uint, from, to time.Time) ([]*domain.NodeUptimeEvent, error) {
	var events []*domain.NodeUptimeEvent
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND timestamp >= ? AND timestamp < ?", nodeID, from.UTC(), to.UTC()).
		Order("timestamp ASC").
		Find(&events).Error
	return events, err
}

// GetLastUptimeEventBefore is the state a node was in when a window opened.
// Without it a day whose first event is "online" is ambiguous: the node was
// either down since before midnight, or up and untouched.
func (r *nodeRepository) GetLastUptimeEventBefore(ctx context.Context, nodeID uint, before time.Time) (*domain.NodeUptimeEvent, error) {
	var event domain.NodeUptimeEvent
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND timestamp < ?", nodeID, before.UTC()).
		Order("timestamp DESC").
		First(&event).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// === Hub lifecycle ===

// CreateHubLifecycleEvent opens a run. Called once at boot.
func (r *nodeRepository) CreateHubLifecycleEvent(ctx context.Context, event *domain.HubLifecycleEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}

// TouchHubLifecycleEvent records that an open run is still alive.
func (r *nodeRepository) TouchHubLifecycleEvent(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.HubLifecycleEvent{}).
		Where("id = ? AND stopped_at IS NULL", id).
		Update("last_alive_at", at.UTC()).Error
}

// CloseHubLifecycleEvent stamps one run's end (graceful shutdown).
func (r *nodeRepository) CloseHubLifecycleEvent(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.HubLifecycleEvent{}).
		Where("id = ? AND stopped_at IS NULL", id).
		Update("stopped_at", at).Error
}

// CloseStaleHubLifecycleEvents closes runs left open by a crash, power loss or
// SIGKILL, stamping them at the last moment we know the hub was alive (its
// last liveness stamp; `at` for rows that never got one). Called at boot
// before opening the new run, so an unclean exit still bounds its gap instead
// of swallowing everything up to now.
func (r *nodeRepository) CloseStaleHubLifecycleEvents(ctx context.Context, at time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&domain.HubLifecycleEvent{}).
		Where("stopped_at IS NULL").
		Update("stopped_at", gorm.Expr("COALESCE(last_alive_at, ?)", at.UTC()))
	return res.RowsAffected, res.Error
}

// GetHubLifecycleEvents returns runs overlapping [since, now), oldest first —
// the complement of these is what the UI greys out as unobserved.
func (r *nodeRepository) GetHubLifecycleEvents(ctx context.Context, since time.Time) ([]*domain.HubLifecycleEvent, error) {
	var events []*domain.HubLifecycleEvent
	err := r.db.WithContext(ctx).
		Where("stopped_at IS NULL OR stopped_at >= ?", since.UTC()).
		Order("started_at ASC").
		Find(&events).Error
	return events, err
}

// FirstHubLifecycleStart is when the oldest recorded run began; zero when
// none is recorded. Time before it has no lifecycle data at all.
func (r *nodeRepository) FirstHubLifecycleStart(ctx context.Context) (time.Time, error) {
	var ev domain.HubLifecycleEvent
	err := r.db.WithContext(ctx).Order("started_at ASC").First(&ev).Error
	if err == gorm.ErrRecordNotFound {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return ev.StartedAt, nil
}

// === Daily uptime rollup ===

// UpsertNodeDailyUptime writes one (node, date) rollup row, replacing any
// previous computation for that day — a re-run must be idempotent because the
// boot backfill and the daily task can both cover the same date.
func (r *nodeRepository) UpsertNodeDailyUptime(ctx context.Context, row *domain.NodeDailyUptime) error {
	var existing domain.NodeDailyUptime
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND date = ?", row.NodeID, row.Date.UTC()).
		First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.WithContext(ctx).Create(row).Error
	}
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&domain.NodeDailyUptime{}).
		Where("id = ?", existing.ID).
		Updates(map[string]interface{}{
			"observed_sec": row.ObservedSec,
			"downtime_sec": row.DowntimeSec,
			"planned_sec":  row.PlannedSec,
			"outage_count": row.OutageCount,
		}).Error
}

// GetNodeDailyUptime returns a node's rollup rows from `since` onward.
func (r *nodeRepository) GetNodeDailyUptime(ctx context.Context, nodeID uint, since time.Time) ([]*domain.NodeDailyUptime, error) {
	var rows []*domain.NodeDailyUptime
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND date >= ?", nodeID, since.UTC()).
		Order("date ASC").
		Find(&rows).Error
	return rows, err
}

// GetLatestNodeDailyUptimeDate reports the newest rolled-up day, so the boot
// backfill knows how far behind it is. Zero time = nothing rolled up yet.
func (r *nodeRepository) GetLatestNodeDailyUptimeDate(ctx context.Context) (time.Time, error) {
	var row domain.NodeDailyUptime
	err := r.db.WithContext(ctx).Order("date DESC").First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return row.Date, nil
}

// === Retention ===

// CleanupOldNodeDailyUptime prunes rollup rows older than retention. Default
// retention is 0 (keep forever) — the rollup exists to outlive the events.
func (r *nodeRepository) CleanupOldNodeDailyUptime(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays)
	res := r.db.WithContext(ctx).
		Where("date < ?", cutoff).
		Delete(&domain.NodeDailyUptime{})
	return res.RowsAffected, res.Error
}

// CleanupOldHubLifecycleEvents prunes closed runs older than retention. The
// currently-open run is never touched.
func (r *nodeRepository) CleanupOldHubLifecycleEvents(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays)
	res := r.db.WithContext(ctx).
		Where("started_at < ? AND stopped_at IS NOT NULL", cutoff).
		Delete(&domain.HubLifecycleEvent{})
	return res.RowsAffected, res.Error
}
