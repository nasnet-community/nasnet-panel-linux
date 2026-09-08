package repository

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// At most 16 binds per subscription plus UpdatedAt: keep below SQLite's
// historical 999-variable limit as well as PostgreSQL's parameter limit.
const trafficBatchSize = 50

type UsageDelta struct {
	SubscriptionID uint
	Upload         int64
	Download       int64
	LastActive     time.Time
}

type DailyUsageDelta struct {
	SubscriptionID uint
	Date           time.Time
	Upload         int64
	Download       int64
}

// AddUsageDeltas merges repeated subscription IDs and increments each counter
// in SQL. Callers can include every batch in a larger transaction via ctx.
func (r *subscriptionRepository) AddUsageDeltas(ctx context.Context, deltas []UsageDelta) error {
	byID := make(map[uint]UsageDelta, len(deltas))
	for _, delta := range deltas {
		merged := byID[delta.SubscriptionID]
		merged.SubscriptionID = delta.SubscriptionID
		merged.Upload += delta.Upload
		merged.Download += delta.Download
		if delta.LastActive.After(merged.LastActive) {
			merged.LastActive = delta.LastActive
		}
		byID[delta.SubscriptionID] = merged
	}
	ids := make([]uint, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	exec := database.GetExecutor(r.db, ctx).WithContext(ctx)
	for start := 0; start < len(ids); start += trafficBatchSize {
		batchIDs := ids[start:min(start+trafficBatchSize, len(ids))]
		batch := make([]UsageDelta, len(batchIDs))
		for i, id := range batchIDs {
			batch[i] = byID[id]
		}
		total := subscriptionCounterCase(batch, func(d UsageDelta) int64 { return d.Upload + d.Download })
		upload := subscriptionCounterCase(batch, func(d UsageDelta) int64 { return d.Upload })
		download := subscriptionCounterCase(batch, func(d UsageDelta) int64 { return d.Download })
		updates := map[string]interface{}{
			"data_used":              gorm.Expr("data_used + "+total.SQL, total.Vars...),
			"lifetime_data_used":     gorm.Expr("lifetime_data_used + "+total.SQL, total.Vars...),
			"data_upload":            gorm.Expr("data_upload + "+upload.SQL, upload.Vars...),
			"data_download":          gorm.Expr("data_download + "+download.SQL, download.Vars...),
			"lifetime_data_upload":   gorm.Expr("lifetime_data_upload + "+upload.SQL, upload.Vars...),
			"lifetime_data_download": gorm.Expr("lifetime_data_download + "+download.SQL, download.Vars...),
		}
		if activity, ok := subscriptionActivityCase(batch); ok {
			updates["last_active_at"] = activity
		}
		if err := exec.Model(&domain.Subscription{}).Where("id IN ?", batchIDs).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func subscriptionCounterCase(batch []UsageDelta, value func(UsageDelta) int64) clause.Expr {
	var sql strings.Builder
	sql.WriteString("CASE id")
	args := make([]interface{}, 0, 2*len(batch))
	for _, delta := range batch {
		sql.WriteString(" WHEN ? THEN ?")
		args = append(args, delta.SubscriptionID, value(delta))
	}
	// Without an explicit bigint arm PostgreSQL can infer integer (int4)
	// for the bind parameters, rejecting ordinary multi-gigabyte deltas.
	sql.WriteString(" ELSE CAST(0 AS BIGINT) END")
	return gorm.Expr(sql.String(), args...)
}

func subscriptionActivityCase(batch []UsageDelta) (clause.Expr, bool) {
	var sql strings.Builder
	sql.WriteString("CASE id")
	args := make([]interface{}, 0, 3*len(batch))
	for _, delta := range batch {
		if delta.LastActive.IsZero() {
			continue
		}
		sql.WriteString(" WHEN ? THEN CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END")
		args = append(args, delta.SubscriptionID, delta.LastActive, delta.LastActive)
	}
	sql.WriteString(" ELSE last_active_at END")
	return gorm.Expr(sql.String(), args...), len(args) > 0
}

// Keep batch writes independent of the model's legacy nullable split fields.
type dailyUsageWrite struct {
	SubscriptionID uint
	Date           time.Time
	DataUsed       int64
	DataUpload     int64
	DataDownload   int64
	CreatedAt      time.Time
}

// AddDailyUsageSplits combines duplicate (subscription, UTC day) entries before
// upserting. EXCLUDED values apply the corresponding row's delta, and COALESCE
// remains compatible while legacy NULL splits are being migrated.
func (r *subscriptionRepository) AddDailyUsageSplits(ctx context.Context, deltas []DailyUsageDelta) error {
	type dailyKey struct {
		id   uint
		date time.Time
	}
	byDay := make(map[dailyKey]DailyUsageDelta, len(deltas))
	for _, delta := range deltas {
		date := delta.Date.UTC().Truncate(24 * time.Hour)
		key := dailyKey{id: delta.SubscriptionID, date: date}
		merged := byDay[key]
		merged.SubscriptionID = delta.SubscriptionID
		merged.Date = date
		merged.Upload += delta.Upload
		merged.Download += delta.Download
		byDay[key] = merged
	}
	keys := make([]dailyKey, 0, len(byDay))
	for key := range byDay {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].id != keys[j].id {
			return keys[i].id < keys[j].id
		}
		return keys[i].date.Before(keys[j].date)
	})
	exec := database.GetExecutor(r.db, ctx).WithContext(ctx)
	for start := 0; start < len(keys); start += trafficBatchSize {
		batchKeys := keys[start:min(start+trafficBatchSize, len(keys))]
		entries := make([]dailyUsageWrite, 0, len(batchKeys))
		for _, key := range batchKeys {
			delta := byDay[key]
			entries = append(entries, dailyUsageWrite{
				SubscriptionID: delta.SubscriptionID,
				Date:           delta.Date,
				DataUsed:       delta.Upload + delta.Download,
				DataUpload:     delta.Upload,
				DataDownload:   delta.Download,
			})
		}
		if err := exec.Table("subscription_daily_usage").Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "subscription_id"}, {Name: "date"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"data_used":     gorm.Expr("subscription_daily_usage.data_used + EXCLUDED.data_used"),
				"data_upload":   gorm.Expr("COALESCE(subscription_daily_usage.data_upload, 0) + EXCLUDED.data_upload"),
				"data_download": gorm.Expr("COALESCE(subscription_daily_usage.data_download, 0) + EXCLUDED.data_download"),
			}),
		}).Create(&entries).Error; err != nil {
			return err
		}
	}
	return nil
}
