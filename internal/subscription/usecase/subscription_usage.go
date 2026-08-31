package usecase

import (
	"context"
	"fmt"
	"time"

	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

func (u *subscriptionUsecase) UpdateDataUsage(ctx context.Context, id uint, bytesUsed int64) error {
	// If admin is setting data_used higher, add the positive delta to lifetime
	log := logger.GetLogger()
	sub, err := u.subRepo.FindByID(ctx, id)
	if err == nil && bytesUsed > sub.DataUsed {
		delta := bytesUsed - sub.DataUsed
		if ltErr := u.subRepo.AddLifetimeDataUsed(ctx, id, delta); ltErr != nil {
			log.WithError(ltErr).WithField("subscription_id", id).Warn("[UpdateDataUsage] Failed to add lifetime data used")
		}
	}
	return u.subRepo.UpdateDataUsed(ctx, id, bytesUsed)
}

func (u *subscriptionUsecase) CheckAndExpireSubscriptions(ctx context.Context) error {
	log := logger.GetLogger()
	expired, err := u.subRepo.ListExpired(ctx)
	if err != nil {
		return fmt.Errorf("failed to list expired subscriptions: %w", err)
	}

	if len(expired) > 0 {
		log.WithField("count", len(expired)).Info("[CheckAndExpireSubscriptions] Processing expired subscriptions")
	}

	for _, sub := range expired {
		u.deactivateOnNodes(ctx, sub)

		if err := u.subRepo.UpdateStatus(ctx, sub.ID, domain.SubscriptionStatusExpired); err != nil {
			log.WithError(err).Errorf("[CheckAndExpireSubscriptions] Failed to update status for sub %d", sub.ID)
		}
		log.WithFields(map[string]interface{}{
			"subscription_id": sub.ID,
			"user_id":         sub.GetUserID(),
		}).Info("[CheckAndExpireSubscriptions] Subscription expired by date")
	}
	return nil
}

func (u *subscriptionUsecase) CheckAndExpireByDataLimit(ctx context.Context) error {
	log := logger.GetLogger()
	exhausted, err := u.subRepo.ListDataExhausted(ctx)
	if err != nil {
		return fmt.Errorf("failed to list data-exhausted subscriptions: %w", err)
	}

	if len(exhausted) > 0 {
		log.WithField("count", len(exhausted)).Info("[CheckAndExpireByDataLimit] Processing data-exhausted subscriptions")
	}

	for _, sub := range exhausted {
		u.deactivateOnNodes(ctx, sub)

		if err := u.subRepo.UpdateStatus(ctx, sub.ID, domain.SubscriptionStatusTrafficExhausted); err != nil {
			log.WithError(err).Errorf("[CheckAndExpireByDataLimit] Failed to update status for sub %d", sub.ID)
		}
		log.WithFields(map[string]interface{}{
			"subscription_id": sub.ID,
			"user_id":         sub.GetUserID(),
			"data_used":       sub.DataUsed,
			"data_limit":      sub.DataLimit,
		}).Info("[CheckAndExpireByDataLimit] Subscription traffic exhausted")
	}
	return nil
}

func (u *subscriptionUsecase) SyncUsageFromXray(ctx context.Context, id uint) error {
	if u.accountManager == nil {
		return nil
	}
	accounts, err := u.accountManager.ListAccountsBySubscription(ctx, id)
	if err != nil || len(accounts) == 0 {
		return nil
	}

	// Per-node sweep is the only valid path for traffic writes;
	// GetUserStats(reset=true) here would double-count vs the collector.
	// Nodes are gotten from the subscription's accounts
	log := logger.GetLogger()
	seen := make(map[uint]struct{})
	for _, acc := range accounts {
		if acc.Inbound == nil || acc.Inbound.Node == nil || !acc.Inbound.Node.IsActive {
			continue
		}
		nodeID := acc.Inbound.Node.ID
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		if err := u.nodeUC.SyncSingleNodeByID(ctx, nodeID); err != nil {
			// Warn, not Debug: default log level is warning, and an unexpected
			// sweep failure (DB error, panic, etc.) was otherwise invisible.
			// Offline-node errors are the common case and still readable at a
			// glance — callers don't aggregate errors today.
			log.WithError(err).WithFields(map[string]interface{}{
				"subscription_id": id,
				"node_id":         nodeID,
			}).Warn("[SyncUsageFromXray] node sweep failed")
		}
	}
	return nil
}

// SetCustomDataLimit sets a custom data limit override for a subscription
func (u *subscriptionUsecase) SetCustomDataLimit(ctx context.Context, id uint, limitGB *float64) error {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	var limitBytes *int64
	if limitGB != nil {
		bytes := int64(*limitGB * float64(domain.GB))
		limitBytes = &bytes
	}

	if err := u.subRepo.SetCustomDataLimit(ctx, id, limitBytes); err != nil {
		return err
	}

	// Clear account-level data limits so the subscription-level override is authoritative.
	// This prevents CheckAndDisableExhaustedAccounts from re-disabling accounts based on
	// stale per-account limits.
	if u.accountManager != nil && limitBytes != nil {
		if clearErr := u.accountManager.ClearAccountDataLimitsBySubscription(ctx, id); clearErr != nil {
			log.WithError(clearErr).WithField("subscription_id", id).Warn("[SetCustomDataLimit] Failed to clear account data limits")
		}
	}

	// Reset warning level when limit changes
	if warnErr := u.resetDataWarnings(ctx, id); warnErr != nil {
		log.WithError(warnErr).WithField("subscription_id", id).Warn("[SetCustomDataLimit] Failed to reset data warning level")
	}

	// Auto-reactivate only if traffic-exhausted (not time-expired — changing the
	// data limit doesn't extend the expiry date, so time-expired subs stay expired).
	if sub.Status == domain.SubscriptionStatusTrafficExhausted {
		effectiveLimit := sub.DataLimit
		if limitBytes != nil {
			effectiveLimit = *limitBytes
		}

		if effectiveLimit == 0 || effectiveLimit > sub.DataUsed {
			u.reactivateSubscription(ctx, id)
		}
	} else if sub.Status == domain.SubscriptionStatusActive {
		// Subscription is Active but accounts may have been disabled by per-account
		// data limit checks (CheckAndDisableExhaustedAccounts). Since we've cleared
		// account data limits above, re-enable any disabled accounts and re-provision on Xray.
		if enableErr := u.setTunnelAccess(ctx, id, true); enableErr != nil {
			log.WithError(enableErr).WithField("subscription_id", id).Warn("[SetCustomDataLimit] Failed to restore tunnel access")
		}
	}

	actionStr := "reset to default"
	if limitGB != nil {
		actionStr = fmt.Sprintf("set to %.2f GB", *limitGB)
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"action":          actionStr,
	}).Info("[SetCustomDataLimit] Custom data limit updated")

	return nil
}

// SetCustomBandwidthLimit sets a custom bandwidth limit override for a subscription
// Pass nil to clear the override (unlimited), Pass pointer to 0 for unlimited
// Note: The new bandwidth tier takes effect on next config push/sync.
func (u *subscriptionUsecase) SetCustomBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	if err := u.subRepo.SetCustomBandwidthLimit(ctx, id, limitMbps); err != nil {
		return err
	}

	actionStr := "reset to default"
	if limitMbps != nil {
		if *limitMbps == 0 {
			actionStr = "set to unlimited"
		} else {
			actionStr = fmt.Sprintf("set to %d Mbps", *limitMbps)
		}
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"action":          actionStr,
	}).Info("[SetCustomBandwidthLimit] Custom bandwidth limit updated")

	return nil
}

// SetMaxDevices sets the per-subscription device cap. Pass 0 for unlimited
// New limits apply on the next device add and cap check existing connected devices are not torn down
func (u *subscriptionUsecase) SetMaxDevices(ctx context.Context, id uint, maxDevices int) error {
	log := logger.GetLogger()

	if maxDevices < 0 {
		maxDevices = 0
	}

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	if err := u.subRepo.SetMaxDevices(ctx, id, maxDevices); err != nil {
		return err
	}

	actionStr := "reset to default"
	if maxDevices > 0 {
		actionStr = fmt.Sprintf("set to %d devices", maxDevices)
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"action":          actionStr,
	}).Info("[SetMaxDevices] Device limit updated")

	return nil
}

// SetCustomEndDate sets a custom end date override for a subscription
// Pass nil to clear the override and fall back to the base end date
func (u *subscriptionUsecase) SetCustomEndDate(ctx context.Context, id uint, endDate *time.Time, isCustom bool) (*domain.Subscription, error) {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	if err := u.subRepo.SetCustomEndDate(ctx, id, endDate, isCustom); err != nil {
		return nil, err
	}

	// Update local object to check status
	sub.CustomEndDate = endDate
	sub.IsEndDateCustom = isCustom
	isExpired := sub.IsExpired()

	if isExpired && sub.Status == domain.SubscriptionStatusActive {
		log.WithField("id", id).Info("[SetCustomEndDate] Subscription is now expired, updating status")
		if err := u.subRepo.UpdateStatus(ctx, id, domain.SubscriptionStatusExpired); err != nil {
			return nil, err
		}
		// Update local object
		sub.Status = domain.SubscriptionStatusExpired
		if disableErr := u.setTunnelAccess(ctx, id, false); disableErr != nil {
			log.WithError(disableErr).WithField("subscription_id", id).Warn("[SetCustomEndDate] Failed to revoke tunnel access")
		}
	} else if !isExpired && sub.Status == domain.SubscriptionStatusExpired {
		// Check if data is also exhausted before reactivating
		if sub.IsDataExhausted() {
			log.WithField("id", id).Info("[SetCustomEndDate] Subscription time extended but data exhausted, setting traffic_exhausted")
			if err := u.subRepo.UpdateStatus(ctx, id, domain.SubscriptionStatusTrafficExhausted); err != nil {
				return nil, err
			}
			sub.Status = domain.SubscriptionStatusTrafficExhausted
		} else {
			log.WithField("id", id).Info("[SetCustomEndDate] Subscription is now active, updating status")
			if err := u.subRepo.UpdateStatus(ctx, id, domain.SubscriptionStatusActive); err != nil {
				return nil, err
			}
			sub.Status = domain.SubscriptionStatusActive
			if enableErr := u.setTunnelAccess(ctx, id, true); enableErr != nil {
				log.WithError(enableErr).WithField("subscription_id", id).Warn("[SetCustomEndDate] Failed to restore tunnel access")
			}
		}
	}

	actionStr := "reset to default"
	if isCustom && endDate == nil {
		actionStr = "set to unlimited"
	} else if endDate != nil {
		actionStr = fmt.Sprintf("set to %s", endDate.Format("2006-01-02"))
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"action":          actionStr,
	}).Info("[SetCustomEndDate] Custom end date updated")

	return sub, nil
}

// AddData adds additional data (in GB) to a subscription's current limit
func (u *subscriptionUsecase) AddData(ctx context.Context, id uint, additionalGB float64) error {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	// Calculate the new limit
	currentLimit := sub.GetEffectiveDataLimit()
	if currentLimit == 0 {
		// Already unlimited, no need to add
		return nil
	}

	additionalBytes := int64(additionalGB * float64(domain.GB))
	newLimit := currentLimit + additionalBytes

	// Set as custom limit
	if err := u.subRepo.SetCustomDataLimit(ctx, id, &newLimit); err != nil {
		return err
	}

	// Clear account-level data limits so the subscription-level override is authoritative
	if u.accountManager != nil {
		if clearErr := u.accountManager.ClearAccountDataLimitsBySubscription(ctx, id); clearErr != nil {
			log.WithError(clearErr).WithField("subscription_id", id).Warn("[AddData] Failed to clear account data limits")
		}
	}

	// Reset warning level since we added data
	if warnErr := u.resetDataWarnings(ctx, id); warnErr != nil {
		log.WithError(warnErr).WithField("subscription_id", id).Warn("[AddData] Failed to reset data warning level")
	}

	// Auto-reactivate only if traffic-exhausted (not time-expired — adding data
	// doesn't extend the expiry date, so time-expired subs must stay expired).
	if sub.Status == domain.SubscriptionStatusTrafficExhausted {
		if newLimit > sub.DataUsed {
			u.reactivateSubscription(ctx, id)
		}
	} else if sub.Status == domain.SubscriptionStatusActive {
		// Re-enable accounts that were disabled by per-account data limit checks
		if enableErr := u.setTunnelAccess(ctx, id, true); enableErr != nil {
			log.WithError(enableErr).WithField("subscription_id", id).Warn("[AddData] Failed to restore tunnel access")
		}
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"added_gb":        additionalGB,
		"new_limit_gb":    float64(newLimit) / float64(domain.GB),
	}).Info("[AddData] Data added to subscription")

	return nil
}

// ResetDataUsed resets the data used counter to 0
func (u *subscriptionUsecase) ResetDataUsed(ctx context.Context, id uint) error {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	if err := u.subRepo.ResetDataUsed(ctx, id); err != nil {
		return err
	}

	// Also reset account-level data_used so per-account checks stay in sync
	if u.accountManager != nil {
		if accErr := u.accountManager.ResetAccountDataUsedBySubscription(ctx, id); accErr != nil {
			log.WithError(accErr).WithField("subscription_id", id).Warn("[ResetDataUsed] Failed to reset account data used")
		}
	}

	// Reset warning level
	if warnErr := u.resetDataWarnings(ctx, id); warnErr != nil {
		log.WithError(warnErr).WithField("subscription_id", id).Warn("[ResetDataUsed] Failed to reset data warning level")
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
		"previous_used":   domain.FormatBytes(sub.DataUsed),
	}).Info("[ResetDataUsed] Data usage reset to 0")

	return nil
}

// GetUsageDetails returns detailed usage information for a subscription
func (u *subscriptionUsecase) GetUsageDetails(ctx context.Context, id uint) (*SubscriptionUsageDetails, error) {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	effectiveLimit := sub.GetEffectiveDataLimit()
	isUnlimited := effectiveLimit == 0

	var dataRemainingGB float64
	if !isUnlimited {
		remaining := effectiveLimit - sub.DataUsed
		if remaining < 0 {
			remaining = 0
		}
		dataRemainingGB = float64(remaining) / float64(domain.GB)
	}

	return &SubscriptionUsageDetails{
		Subscription:       sub,
		EffectiveDataLimit: effectiveLimit,
		DataLimitGB:        float64(effectiveLimit) / float64(domain.GB),
		DataUsedGB:         float64(sub.DataUsed) / float64(domain.GB),
		DataRemainingGB:    dataRemainingGB,
		UsagePercentage:    sub.GetDataUsagePercentage(),
		IsUnlimited:        isUnlimited,
		IsCustomLimit:      sub.IsDataLimitCustom,
		IsCustomExpiry:     sub.IsEndDateCustom,
		DaysRemaining:      sub.DaysRemaining(),
		Status:             string(sub.Status),
		WarningLevel:       sub.GetDataWarningLevelString(),
	}, nil
}

// CheckAndSendDataWarnings returns subs that are burning through their data quota.
// The scheduler calls this and handles the actual notifications.
func (u *subscriptionUsecase) CheckAndSendDataWarnings(ctx context.Context) ([]*domain.Subscription, error) {
	log := logger.GetLogger()
	var updatedSubs []*domain.Subscription

	// Get subscriptions approaching data limit (50% or more)
	subs, err := u.subRepo.ListApproachingDataLimit(ctx, 50)
	if err != nil {
		return nil, err
	}

	for _, sub := range subs {
		percentage := sub.GetDataUsagePercentage()

		// Determine the warning level based on percentage
		var newLevel int
		switch {
		case percentage >= 100:
			newLevel = 4 // exhausted
		case percentage >= 90:
			newLevel = 3 // critical
		case percentage >= 75:
			newLevel = 2 // warning
		case percentage >= 50:
			newLevel = 1 // notice
		default:
			newLevel = 0
		}

		// Only update if level increased
		if newLevel > sub.DataWarningLevel {
			if err := u.subRepo.UpdateDataWarningLevel(ctx, sub.ID, newLevel); err != nil {
				log.WithError(err).WithField("subscription_id", sub.ID).Warn("Failed to update data warning level")
				continue
			}

			log.WithFields(map[string]interface{}{
				"subscription_id": sub.ID,
				"user_id":         sub.GetUserID(),
				"percentage":      percentage,
				"warning_level":   newLevel,
			}).Debug("[CheckAndSendDataWarnings] Updated warning level")

			sub.DataWarningLevel = newLevel
			updatedSubs = append(updatedSubs, sub)
		}
	}

	return updatedSubs, nil
}

// GetSubscriptionUsageHistory returns daily usage points for a subscription.
// Records are stored as per-day deltas (bytes used that UTC day); values are
// returned as-is with a clamp against negative corruption.
func (u *subscriptionUsecase) GetSubscriptionUsageHistory(ctx context.Context, subID uint, days int) ([]UsageHistoryPoint, error) {
	records, err := u.ListDailyUsageRecords(ctx, subID, days)
	if err != nil {
		return nil, err
	}
	points := make([]UsageHistoryPoint, 0, len(records))
	for _, r := range records {
		v := r.DataUsed
		if v < 0 {
			v = 0
		}
		points = append(points, UsageHistoryPoint{
			Date:     r.Date.UTC().Format("2006-01-02"),
			DataUsed: v,
		})
	}
	return points, nil
}

// ListDailyUsageRecords returns the raw per-day delta records for a
// subscription over the last `days` days (UTC-aligned). Callers that need
// the shared analytics.ComputeExhaustion helper should use this instead of
// GetSubscriptionUsageHistory.
func (u *subscriptionUsecase) ListDailyUsageRecords(ctx context.Context, subID uint, days int) ([]*domain.SubscriptionDailyUsage, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	to := time.Now().UTC().Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -days)
	return u.subRepo.ListDailyUsage(ctx, subID, from, to)
}

// GetSubscriptionUsagePattern returns hourly connection counts for a single subscription.
func (u *subscriptionUsecase) GetSubscriptionUsagePattern(ctx context.Context, configID string, days int) ([]HourlyUsagePoint, error) {
	if days <= 0 {
		days = 30
	}
	if days > 30 {
		days = 30
	}

	sub, err := u.subRepo.FindByConfigID(ctx, configID)
	if err != nil {
		return nil, err
	}

	if sub.ConfigEmail == "" {
		// No email means no access log data
		points := make([]HourlyUsagePoint, 24)
		for h := 0; h < 24; h++ {
			points[h] = HourlyUsagePoint{Hour: h}
		}
		return points, nil
	}

	to := time.Now()
	from := to.AddDate(0, 0, -days)

	filter := nodeRepo.AccessLogSummaryFilter{
		Email: sub.ConfigEmail,
		From:  from,
		To:    to,
	}

	aggregates, err := u.nodeRepo.GetHourlyAggregates(ctx, filter)
	if err != nil {
		return nil, err
	}

	hourCounts := make([]int64, 24)
	for _, agg := range aggregates {
		if agg.Hour >= 0 && agg.Hour < 24 {
			hourCounts[agg.Hour] += agg.Accepted + agg.Rejected
		}
	}

	points := make([]HourlyUsagePoint, 24)
	for h := 0; h < 24; h++ {
		points[h] = HourlyUsagePoint{
			Hour:  h,
			Count: hourCounts[h],
		}
	}

	return points, nil
}

// GetSubscriptionUsageTrend returns daily totals + split (when available) for
// the last rangeDays calendar days ending today (UTC). rangeDays must be 7 or 30.
func (u *subscriptionUsecase) GetSubscriptionUsageTrend(ctx context.Context, subID uint, rangeDays int) (*domain.UsageTrend, error) {
	if rangeDays != 7 && rangeDays != 30 {
		return nil, fmt.Errorf("invalid range: %d (allowed: 7, 30)", rangeDays)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	from := today.AddDate(0, 0, -(rangeDays - 1))

	rows, err := u.subRepo.ListDailyUsageRange(ctx, subID, from, today)
	if err != nil {
		return nil, fmt.Errorf("failed to list daily usage range for subscription %d: %w", subID, err)
	}

	points := make([]domain.UsageTrendPoint, 0, len(rows))
	var maxTotal int64
	for _, r := range rows {
		if r.DataUsed > maxTotal {
			maxTotal = r.DataUsed
		}
		points = append(points, domain.UsageTrendPoint{
			Date:     r.Date,
			Upload:   r.DataUpload,
			Download: r.DataDownload,
			Total:    r.DataUsed,
		})
	}

	// Binary (1024-based) thresholds so the chosen unit matches the panel's
	// 1024-based divisor (UNIT_FACTOR / FormatBytes).
	var unit string
	switch {
	case maxTotal < 1024*1024:
		unit = "KB"
	case maxTotal < 1024*1024*1024:
		unit = "MB"
	default:
		unit = "GB"
	}

	rangeStr := fmt.Sprintf("%dd", rangeDays)
	return &domain.UsageTrend{
		Range:    rangeStr,
		Points:   points,
		UnitHint: unit,
	}, nil
}
