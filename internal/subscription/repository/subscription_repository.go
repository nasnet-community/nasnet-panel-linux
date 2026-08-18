package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubscriptionFilter holds optional filters for the admin list endpoint.
type SubscriptionFilter struct {
	Offset    int
	Limit     int
	Status    string
	Search    string
	IsManual  *bool
	Exhausted *bool
	Sort      string
	Order     string
}

type SubscriptionRepository interface {
	Create(ctx context.Context, sub *domain.Subscription) error
	FindByID(ctx context.Context, id uint) (*domain.Subscription, error)
	FindByConfigID(ctx context.Context, configID string) (*domain.Subscription, error)
	FindByConfigEmail(ctx context.Context, email string) (*domain.Subscription, error)

	FindByConfigEmails(ctx context.Context, emails []string) (map[string]*domain.Subscription, error)
	Update(ctx context.Context, sub *domain.Subscription) error
	Delete(ctx context.Context, id uint) error
	ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]*domain.Subscription, error)
	FindActiveByUserID(ctx context.Context, userID uint) ([]*domain.Subscription, error)
	UpdateStatus(ctx context.Context, id uint, status domain.SubscriptionStatus) error
	UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error
	ListExpired(ctx context.Context) ([]*domain.Subscription, error)
	ListDataExhausted(ctx context.Context) ([]*domain.Subscription, error)
	ListAllActive(ctx context.Context) ([]*domain.Subscription, error)
	ListActiveByNode(ctx context.Context, nodeID uint) ([]*domain.Subscription, error)

	// Admin methods
	ListAll(ctx context.Context, status string, offset, limit int) ([]*domain.Subscription, error)
	CountAll(ctx context.Context) (int64, error)
	CountByStatus(ctx context.Context, status string) (int64, error)
	ExtendDays(ctx context.Context, id uint, days int) error
	ResetDataUsed(ctx context.Context, id uint) error
	UpdateLabel(ctx context.Context, id uint, label string) error
	UpdateTelegramChatID(ctx context.Context, id uint, chatID int64) error

	// Active Stats
	UpdateLastActive(ctx context.Context, id uint, lastActive time.Time) error
	CountActiveByNode(ctx context.Context, nodeID uint, since time.Time) (int64, error)
	CountActiveByInbound(ctx context.Context, inboundID uint, since time.Time) (int64, error)

	// Filtered admin list (used by web panel)
	ListAllFiltered(ctx context.Context, filter SubscriptionFilter) ([]*domain.Subscription, int64, error)

	// Hard delete for admin permanent removal
	HardDelete(ctx context.Context, id uint) error

	// Migration/Assignment support
	UpdateUserID(ctx context.Context, id uint, userID uint) error
	SetUserID(ctx context.Context, id uint, userID *uint) error

	SetCustomDataLimit(ctx context.Context, id uint, limit *int64) error
	SetCustomEndDate(ctx context.Context, id uint, endDate *time.Time, isCustom bool) error
	SetCustomBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error

	SetMaxDevices(ctx context.Context, id uint, maxDevices int) error
	AddDataUsed(ctx context.Context, id uint, bytes int64) error
	AddLifetimeDataUsed(ctx context.Context, id uint, bytes int64) error
	AddDataUpload(ctx context.Context, id uint, bytes int64) error
	AddDataDownload(ctx context.Context, id uint, bytes int64) error
	AddLifetimeDataUpload(ctx context.Context, id uint, bytes int64) error
	AddLifetimeDataDownload(ctx context.Context, id uint, bytes int64) error
	// AddUsageDelta applies one stats-sync cycle's traffic counters in a
	// single UPDATE: data_used/lifetime totals, upload/download splits
	// (current + lifetime) and last_active_at.
	AddUsageDelta(ctx context.Context, id uint, upload, download int64, lastActive time.Time) error
	UpdateDataWarningLevel(ctx context.Context, id uint, level int) error
	ListApproachingDataLimit(ctx context.Context, thresholdPercent float64) ([]*domain.Subscription, error)
	ResetDataWarningLevel(ctx context.Context, id uint) error

	SetPanelPassword(ctx context.Context, id uint, hash string, mode string) error

	AddDailyUsageSplit(ctx context.Context, subID uint, date time.Time, upload, download int64) error

	ListDailyUsageRange(ctx context.Context, subID uint, from, to time.Time) ([]*domain.SubscriptionDailyUsage, error)

	ListDailyUsage(ctx context.Context, subID uint, from, to time.Time) ([]*domain.SubscriptionDailyUsage, error)
	CleanupOldDailyUsage(ctx context.Context, olderThanDays int) (int64, error)
}

type subscriptionRepository struct {
	db *gorm.DB
}

func NewSubscriptionRepository(db *gorm.DB) SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub *domain.Subscription) error {
	return database.GetExecutor(r.db, ctx).Create(sub).Error
}

func (r *subscriptionRepository) FindByID(ctx context.Context, id uint) (*domain.Subscription, error) {
	var sub domain.Subscription
	if err := database.GetExecutor(r.db, ctx).Preload("User").First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// FindByConfigID resolves by link_key first (rotatable), falling back to
// config_id for legacy rows. Once link_key is set, old config_id stops
// resolving — required for key rotation to invalidate old URLs.
func (r *subscriptionRepository) FindByConfigID(ctx context.Context, configID string) (*domain.Subscription, error) {
	var sub domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Preload("User").
		Where("link_key = ? OR ((link_key IS NULL OR link_key = '') AND config_id = ?)", configID, configID).
		First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *subscriptionRepository) Update(ctx context.Context, sub *domain.Subscription) error {
	return database.GetExecutor(r.db, ctx).Omit("User").Save(sub).Error
}

func (r *subscriptionRepository) Delete(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Delete(&domain.Subscription{}, id).Error
}

func (r *subscriptionRepository) ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).Limit(limit).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) FindActiveByUserID(ctx context.Context, userID uint) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Where("user_id = ? AND status = ?", userID, domain.SubscriptionStatusActive).
		Order("end_date ASC").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) UpdateStatus(ctx context.Context, id uint, status domain.SubscriptionStatus) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *subscriptionRepository) UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("data_used", dataUsed).Error
}

func (r *subscriptionRepository) ListExpired(ctx context.Context) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	// End-date resolution: is_end_date_custom=false → end_date;
	// custom + non-null custom_end_date → custom_end_date;
	// custom + null → unlimited.
	now := database.Now()
	grace := database.NowMinusInterval(1, "minute")
	clause := fmt.Sprintf(
		"status = ? AND created_at < %s AND ((is_end_date_custom = false AND end_date < %s) OR (is_end_date_custom = true AND custom_end_date IS NOT NULL AND custom_end_date < %s))",
		grace, now, now,
	)
	if err := database.GetExecutor(r.db, ctx).
		Where(clause, domain.SubscriptionStatusActive).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) ListDataExhausted(ctx context.Context) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Where("status = ?", domain.SubscriptionStatusActive).
		Where("COALESCE(custom_data_limit, data_limit) > 0").
		Where("data_used >= COALESCE(custom_data_limit, data_limit)").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) ListAllActive(ctx context.Context) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Where("status = ?", domain.SubscriptionStatusActive).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) ListAll(ctx context.Context, status string, offset, limit int) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	query := database.GetExecutor(r.db, ctx).Model(&domain.Subscription{}).Preload("User")

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

// applyFilter applies optional SubscriptionFilter conditions to a query.
func (r *subscriptionRepository) applyFilter(db *gorm.DB, f SubscriptionFilter) *gorm.DB {
	if f.Status != "" {
		db = db.Where("subscriptions.status = ?", f.Status)
	}
	if f.Search != "" {
		like := "%" + f.Search + "%"
		clause := fmt.Sprintf("%s OR %s OR %s OR %s",
			database.ILike("subscriptions.label", "?"),
			database.ILike("subscriptions.config_email", "?"),
			database.ILike("users.username", "?"),
			database.ILike("users.first_name", "?"),
		)
		db = db.Joins("LEFT JOIN users ON users.id = subscriptions.user_id AND users.deleted_at IS NULL").
			Where(clause, like, like, like, like)
	}
	if f.IsManual != nil {
		db = db.Where("subscriptions.is_manual = ?", *f.IsManual)
	}
	if f.Exhausted != nil {
		if *f.Exhausted {
			// Data exhausted: used >= effective limit and limit > 0
			db = db.Where("COALESCE(subscriptions.custom_data_limit, subscriptions.data_limit) > 0").
				Where("subscriptions.data_used >= COALESCE(subscriptions.custom_data_limit, subscriptions.data_limit)")
		} else {
			// Available: unlimited OR used < effective limit
			db = db.Where("COALESCE(subscriptions.custom_data_limit, subscriptions.data_limit) = 0 OR subscriptions.data_used < COALESCE(subscriptions.custom_data_limit, subscriptions.data_limit)")
		}
	}
	return db
}

// buildSortClause returns a safe ORDER BY clause from the filter's Sort and Order fields.
func buildSortClause(sort, order string) string {
	// Validate order direction
	dir := "DESC"
	switch strings.ToLower(order) {
	case "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	}

	// Whitelist sort columns to prevent SQL injection
	switch sort {
	case "id":
		return "subscriptions.id " + dir
	case "data_used":
		return "subscriptions.data_used " + dir
	case "lifetime_data_used":
		return "(subscriptions.lifetime_data_upload + subscriptions.lifetime_data_download) " + dir
	case "end_date":
		return database.NullsLast("COALESCE(subscriptions.custom_end_date, subscriptions.end_date) " + dir)
	case "last_active_at":
		return database.NullsLast("subscriptions.last_active_at " + dir)
	default:
		return "subscriptions.created_at DESC"
	}
}

// ListAllFiltered returns subscriptions matching the filter along with a total count.
func (r *subscriptionRepository) ListAllFiltered(ctx context.Context, filter SubscriptionFilter) ([]*domain.Subscription, int64, error) {
	var subs []*domain.Subscription
	var total int64

	base := database.GetExecutor(r.db, ctx).Model(&domain.Subscription{})
	base = r.applyFilter(base, filter)

	// Count before pagination (use Session to avoid polluting base's statement)
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderClause := buildSortClause(filter.Sort, filter.Order)

	if err := base.Session(&gorm.Session{}).Preload("User").
		Order(orderClause).
		Offset(filter.Offset).Limit(filter.Limit).
		Find(&subs).Error; err != nil {
		return nil, 0, err
	}

	return subs, total, nil
}

// CountAll counts all subscriptions
func (r *subscriptionRepository) CountAll(ctx context.Context) (int64, error) {
	var count int64
	if err := database.GetExecutor(r.db, ctx).Model(&domain.Subscription{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountByStatus counts subscriptions by status
func (r *subscriptionRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	if err := database.GetExecutor(r.db, ctx).Model(&domain.Subscription{}).
		Where("status = ?", status).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ExtendDays extends subscription end date by days
func (r *subscriptionRepository) ExtendDays(ctx context.Context, id uint, days int) error {
	db := database.GetExecutor(r.db, ctx)

	// Extend custom_end_date if it's set, otherwise extend end_date
	return db.Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"end_date":        gorm.Expr(database.AddInterval("end_date", "?"), days),
			"custom_end_date": gorm.Expr(database.AddIntervalConditional("custom_end_date IS NOT NULL", "custom_end_date", "?", "NULL"), days),
		}).Error
}

// ResetDataUsed resets data usage to 0 (including upload/download, but not lifetime variants)
func (r *subscriptionRepository) ResetDataUsed(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"data_used":     0,
			"data_upload":   0,
			"data_download": 0,
		}).Error
}

// UpdateLabel updates the subscription label
func (r *subscriptionRepository) UpdateLabel(ctx context.Context, id uint, label string) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("label", label).Error
}

// UpdateTelegramChatID updates the Telegram chat ID for notifications
func (r *subscriptionRepository) UpdateTelegramChatID(ctx context.Context, id uint, chatID int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("telegram_chat_id", chatID).Error
}

// UpdateLastActive updates the LastActiveAt timestamp
func (r *subscriptionRepository) UpdateLastActive(ctx context.Context, id uint, lastActive time.Time) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("last_active_at", lastActive).Error
}

// CountActiveByNode counts distinct users on a node active since time T
func (r *subscriptionRepository) CountActiveByNode(ctx context.Context, nodeID uint, since time.Time) (int64, error) {
	var count int64
	err := database.GetExecutor(r.db, ctx).
		Table("subscriptions").
		Joins("JOIN accounts ON accounts.subscription_id = subscriptions.id AND accounts.deleted_at IS NULL").
		Joins("JOIN inbounds ON accounts.inbound_id = inbounds.id").
		Where("inbounds.node_id = ? AND subscriptions.last_active_at > ? AND subscriptions.deleted_at IS NULL", nodeID, since).
		Distinct("subscriptions.id").
		Count(&count).Error
	return count, err
}

// CountActiveByInbound counts distinct users on an inbound active since time T
func (r *subscriptionRepository) CountActiveByInbound(ctx context.Context, inboundID uint, since time.Time) (int64, error) {
	var count int64
	err := database.GetExecutor(r.db, ctx).
		Table("subscriptions").
		Joins("JOIN accounts ON accounts.subscription_id = subscriptions.id AND accounts.deleted_at IS NULL").
		Where("accounts.inbound_id = ? AND subscriptions.last_active_at > ? AND subscriptions.deleted_at IS NULL", inboundID, since).
		Distinct("subscriptions.id").
		Count(&count).Error
	return count, err
}

func (r *subscriptionRepository) HardDelete(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Unscoped().Delete(&domain.Subscription{}, id).Error
}

// FindByConfigEmail finds a subscription by config email (for migration duplicate check)
func (r *subscriptionRepository) FindByConfigEmail(ctx context.Context, email string) (*domain.Subscription, error) {
	var sub domain.Subscription
	if err := database.GetExecutor(r.db, ctx).
		Where("config_email = ?", email).
		First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// findByConfigEmailsChunkSize bounds a single IN(...) query's list
// length. Postgres caps bound parameters at ~65k; keeping this well
// under that leaves room for other params without tipping over.
const findByConfigEmailsChunkSize = 1000

// FindByConfigEmails: keyed by config_email, batched IN queries with
// caller-input dedup. Missing emails absent from the map.
func (r *subscriptionRepository) FindByConfigEmails(ctx context.Context, emails []string) (map[string]*domain.Subscription, error) {
	out := make(map[string]*domain.Subscription, len(emails))
	if len(emails) == 0 {
		return out, nil
	}

	// Dedupe — traffic maps occasionally hold the same email across
	// multiple buckets, and batching 10k duplicates wastes work.
	seen := make(map[string]struct{}, len(emails))
	unique := make([]string, 0, len(emails))
	for _, e := range emails {
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		unique = append(unique, e)
	}
	if len(unique) == 0 {
		return out, nil
	}

	exec := database.GetExecutor(r.db, ctx)
	for start := 0; start < len(unique); start += findByConfigEmailsChunkSize {
		end := start + findByConfigEmailsChunkSize
		if end > len(unique) {
			end = len(unique)
		}
		var subs []*domain.Subscription
		if err := exec.
			Where("config_email IN ?", unique[start:end]).
			Find(&subs).Error; err != nil {
			return nil, err
		}
		for _, s := range subs {
			out[s.ConfigEmail] = s
		}
	}
	return out, nil
}

// UpdateUserID reassigns a subscription to a different user
func (r *subscriptionRepository) UpdateUserID(ctx context.Context, id uint, userID uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("user_id", userID).Error
}

// SetUserID sets or clears the user_id on a subscription
func (r *subscriptionRepository) SetUserID(ctx context.Context, id uint, userID *uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("user_id", userID).Error
}

// SetCustomDataLimit sets or clears the custom data limit override
func (r *subscriptionRepository) SetCustomDataLimit(ctx context.Context, id uint, limit *int64) error {
	updates := map[string]interface{}{
		"custom_data_limit":    limit,
		"is_data_limit_custom": limit != nil,
	}
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// SetCustomEndDate sets or clears the custom end date override.
// When isCustom is true and endDate is nil, the subscription has no expiry (unlimited).
func (r *subscriptionRepository) SetCustomEndDate(ctx context.Context, id uint, endDate *time.Time, isCustom bool) error {
	updates := map[string]interface{}{
		"custom_end_date":    endDate,
		"is_end_date_custom": isCustom,
	}
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// SetCustomBandwidthLimit sets or clears the custom bandwidth limit override
func (r *subscriptionRepository) SetCustomBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error {
	updates := map[string]interface{}{
		"custom_bandwidth_limit": limitMbps,
		"is_bandwidth_custom":    limitMbps != nil,
	}
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// SetMaxDevices sets the per-subscription device cap. 0 = unlimited
func (r *subscriptionRepository) SetMaxDevices(ctx context.Context, id uint, maxDevices int) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("max_devices", maxDevices).Error
}

// AddDataUsed adds bytes to the data_used field
func (r *subscriptionRepository) AddDataUsed(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("data_used", gorm.Expr("data_used + ?", bytes)).Error
}

// AddLifetimeDataUsed adds bytes to the lifetime_data_used field (never resets)
func (r *subscriptionRepository) AddLifetimeDataUsed(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("lifetime_data_used", gorm.Expr("lifetime_data_used + ?", bytes)).Error
}

// AddDataUpload adds bytes to the data_upload field
func (r *subscriptionRepository) AddDataUpload(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("data_upload", gorm.Expr("data_upload + ?", bytes)).Error
}

// AddDataDownload adds bytes to the data_download field
func (r *subscriptionRepository) AddDataDownload(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("data_download", gorm.Expr("data_download + ?", bytes)).Error
}

// AddLifetimeDataUpload adds bytes to the lifetime_data_upload field (never resets)
func (r *subscriptionRepository) AddLifetimeDataUpload(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("lifetime_data_upload", gorm.Expr("lifetime_data_upload + ?", bytes)).Error
}

// AddLifetimeDataDownload adds bytes to the lifetime_data_download field (never resets)
func (r *subscriptionRepository) AddLifetimeDataDownload(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Update("lifetime_data_download", gorm.Expr("lifetime_data_download + ?", bytes)).Error
}

// AddUsageDelta folds the seven per-email counter updates the stats sweep
// used to fire (data_used, lifetime_data_used, up/down splits, lifetime
// splits, last_active_at) into one UPDATE on the subscription row.
func (r *subscriptionRepository) AddUsageDelta(ctx context.Context, id uint, upload, download int64, lastActive time.Time) error {
	total := upload + download
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"data_used":              gorm.Expr("data_used + ?", total),
			"lifetime_data_used":     gorm.Expr("lifetime_data_used + ?", total),
			"data_upload":            gorm.Expr("data_upload + ?", upload),
			"data_download":          gorm.Expr("data_download + ?", download),
			"lifetime_data_upload":   gorm.Expr("lifetime_data_upload + ?", upload),
			"lifetime_data_download": gorm.Expr("lifetime_data_download + ?", download),
			"last_active_at":         lastActive,
		}).Error
}

// UpdateDataWarningLevel updates the data warning level
func (r *subscriptionRepository) UpdateDataWarningLevel(ctx context.Context, id uint, level int) error {
	now := time.Now()
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"data_warning_level": level,
			"last_data_warning":  &now,
		}).Error
}

// ResetDataWarningLevel resets warning level to 0
func (r *subscriptionRepository) ResetDataWarningLevel(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"data_warning_level": 0,
			"last_data_warning":  nil,
		}).Error
}

// AddDailyUsageSplit atomically upserts a daily row with split upload/download bytes.
// For existing legacy rows (NULL splits), COALESCE promotes NULL → 0 before incrementing,
// so the row cleanly transitions to NOT NULL on first split write.
func (r *subscriptionRepository) AddDailyUsageSplit(ctx context.Context, subID uint, date time.Time, upload, download int64) error {
	total := upload + download
	entry := &domain.SubscriptionDailyUsage{
		SubscriptionID: subID,
		Date:           date,
		DataUsed:       total,
		DataUpload:     &upload,
		DataDownload:   &download,
	}
	return database.GetExecutor(r.db, ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "subscription_id"}, {Name: "date"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"data_used":     gorm.Expr("subscription_daily_usage.data_used + ?", total),
			"data_upload":   gorm.Expr("COALESCE(subscription_daily_usage.data_upload, 0) + ?", upload),
			"data_download": gorm.Expr("COALESCE(subscription_daily_usage.data_download, 0) + ?", download),
		}),
	}).Create(entry).Error
}

// ListDailyUsageRange returns all daily usage rows for subID inside [from, to] (inclusive).
// Results are ordered by Date ascending. Rows with NULL data_upload / data_download keep
// those pointer fields nil, which the caller interprets as "legacy / split unavailable".
func (r *subscriptionRepository) ListDailyUsageRange(ctx context.Context, subID uint, from, to time.Time) ([]*domain.SubscriptionDailyUsage, error) {
	var rows []*domain.SubscriptionDailyUsage
	if err := database.GetExecutor(r.db, ctx).
		Where("subscription_id = ? AND date >= ? AND date <= ?", subID, from, to).
		Order("date ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListDailyUsage returns usage records for a subscription within a date range, ordered by date ASC
func (r *subscriptionRepository) ListDailyUsage(ctx context.Context, subID uint, from, to time.Time) ([]*domain.SubscriptionDailyUsage, error) {
	var records []*domain.SubscriptionDailyUsage
	err := database.GetExecutor(r.db, ctx).
		Where("subscription_id = ? AND date >= ? AND date <= ?", subID, from, to).
		Order("date ASC").
		Find(&records).Error
	return records, err
}

// CleanupOldDailyUsage prunes SubscriptionDailyUsage rows whose `date` column
// is older than the retention window. Grows at (active subs × days), so on a
// busy hub this is the largest of the "per-subscription" history tables.
func (r *subscriptionRepository) CleanupOldDailyUsage(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := database.GetExecutor(r.db, ctx).
		Where("date < ?", cutoff).
		Delete(&domain.SubscriptionDailyUsage{})
	return result.RowsAffected, result.Error
}

// SetPanelPassword updates the panel password hash and mode for a subscription
func (r *subscriptionRepository) SetPanelPassword(ctx context.Context, id uint, hash string, mode string) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"panel_password_hash": hash,
			"panel_password_mode": mode,
		}).Error
}

// ListApproachingDataLimit lists active subscriptions where usage is above threshold
func (r *subscriptionRepository) ListApproachingDataLimit(ctx context.Context, thresholdPercent float64) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	// Query subscriptions where data_used / effective_limit >= threshold
	// effective_limit = COALESCE(custom_data_limit, data_limit)
	if err := database.GetExecutor(r.db, ctx).
		Preload("User").
		Where("status = ?", domain.SubscriptionStatusActive).
		Where("COALESCE(custom_data_limit, data_limit) > 0"). // Has a limit (not unlimited)
		Where(fmt.Sprintf("(%s / %s) * 100 >= ?", database.CastFloat("data_used"), database.CastFloat("COALESCE(custom_data_limit, data_limit)")), thresholdPercent).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

// ListActiveByNode lists all active subscriptions that have an account on the given node
func (r *subscriptionRepository) ListActiveByNode(ctx context.Context, nodeID uint) ([]*domain.Subscription, error) {
	var subs []*domain.Subscription
	err := database.GetExecutor(r.db, ctx).
		Table("subscriptions").
		Joins("JOIN accounts ON accounts.subscription_id = subscriptions.id AND accounts.deleted_at IS NULL").
		Joins("JOIN inbounds ON accounts.inbound_id = inbounds.id").
		Where("inbounds.node_id = ? AND subscriptions.status = ? AND subscriptions.deleted_at IS NULL", nodeID, domain.SubscriptionStatusActive).
		Group("subscriptions.id").
		Find(&subs).Error
	return subs, err
}
