package repository

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

const trafficBatchSize = 50

type UsageDelta struct {
	AccountID  uint
	Bytes      int64
	LastActive time.Time
}

// AddUsageDeltas increments counters and advances activity together. Duplicate
// IDs are combined; an older node sample cannot move activity backwards.
func (r *accountRepository) AddUsageDeltas(ctx context.Context, deltas []UsageDelta) error {
	byID := make(map[uint]UsageDelta, len(deltas))
	for _, delta := range deltas {
		merged := byID[delta.AccountID]
		merged.AccountID = delta.AccountID
		merged.Bytes += delta.Bytes
		if delta.LastActive.After(merged.LastActive) {
			merged.LastActive = delta.LastActive
		}
		byID[delta.AccountID] = merged
	}
	ids := make([]uint, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	exec := database.GetExecutor(r.db, ctx).WithContext(ctx)
	for start := 0; start < len(ids); start += trafficBatchSize {
		batchIDs := ids[start:min(start+trafficBatchSize, len(ids))]
		var usage, activity strings.Builder
		usage.WriteString("data_used + CASE id")
		activity.WriteString("CASE id")
		usageArgs := make([]interface{}, 0, 2*len(batchIDs))
		activityArgs := make([]interface{}, 0, 3*len(batchIDs))
		for _, id := range batchIDs {
			delta := byID[id]
			usage.WriteString(" WHEN ? THEN ?")
			usageArgs = append(usageArgs, id, delta.Bytes)
			if !delta.LastActive.IsZero() {
				activity.WriteString(" WHEN ? THEN CASE WHEN last_activity_at IS NULL OR last_activity_at < ? THEN ? ELSE last_activity_at END")
				activityArgs = append(activityArgs, id, delta.LastActive, delta.LastActive)
			}
		}
		usage.WriteString(" ELSE CAST(0 AS BIGINT) END")
		activity.WriteString(" ELSE last_activity_at END")
		updates := map[string]interface{}{"data_used": gorm.Expr(usage.String(), usageArgs...)}
		if len(activityArgs) > 0 {
			updates["last_activity_at"] = gorm.Expr(activity.String(), activityArgs...)
		}
		if err := exec.Model(&domain.Account{}).Where("id IN ?", batchIDs).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
