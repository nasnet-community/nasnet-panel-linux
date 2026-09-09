package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
)

// CountExpiring counts active subscriptions whose effective expiry is in
// (after, through]. The CASE mirrors GetEffectiveEndDate: a custom nil date
// means unlimited, while a custom date without its flag is ignored.
func (r *subscriptionRepository) CountExpiring(ctx context.Context, after, through time.Time) (int64, error) {
	const effectiveExpiry = "CASE WHEN is_end_date_custom THEN custom_end_date ELSE end_date END"
	var count int64
	err := database.GetExecutor(r.db, ctx).Model(&domain.Subscription{}).
		Where("status = ?", domain.SubscriptionStatusActive).
		Where("("+effectiveExpiry+") > ? AND ("+effectiveExpiry+") <= ?", after.UTC(), through.UTC()).
		Count(&count).Error
	return count, err
}
