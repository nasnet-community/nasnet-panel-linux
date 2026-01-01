package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubscriptionIPRecord is one (sub, ip, node) triple for bulk upsert.
type SubscriptionIPRecord struct {
	SubscriptionID uint
	IP             string
	NodeID         uint
}

// SubscriptionIPRepository manages subscription IP persistence
type SubscriptionIPRepository interface {
	UpsertSubscriptionIP(ctx context.Context, subscriptionID uint, ip string, nodeID uint) error
	// BulkUpsertSubscriptionIPs collapses the stats-sweep per-IP loop
	// into a single CreateInBatches + ON CONFLICT upsert against the
	// existing idx_sub_ip unique index. On conflict last_seen + node_id
	// update so we keep tracking where a given IP was last seen.
	BulkUpsertSubscriptionIPs(ctx context.Context, records []SubscriptionIPRecord) error
	GetSubscriptionIPs(ctx context.Context, subscriptionID uint) ([]domain.SubscriptionIP, error)
	GetSubscriptionActiveIPs(ctx context.Context, subscriptionID uint, since time.Time) ([]domain.SubscriptionIP, error)
	DeleteOldSubscriptionIPs(ctx context.Context, olderThan time.Time) (int64, error)
}

type subscriptionIPRepository struct {
	db *gorm.DB
}

func NewSubscriptionIPRepository(db *gorm.DB) SubscriptionIPRepository {
	return &subscriptionIPRepository{db: db}
}

// UpsertSubscriptionIP inserts a new IP record or updates last_seen on conflict
func (r *subscriptionIPRepository) UpsertSubscriptionIP(ctx context.Context, subscriptionID uint, ip string, nodeID uint) error {
	now := time.Now()
	record := domain.SubscriptionIP{
		SubscriptionID: subscriptionID,
		IP:             ip,
		NodeID:         nodeID,
		FirstSeen:      now,
		LastSeen:       now,
	}
	return database.GetExecutor(r.db, ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subscription_id"}, {Name: "ip"}, {Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen", "updated_at"}),
	}).Create(&record).Error
}

// bulkUpsertChunk: rows per CreateInBatches. 500 × ~8 params = 4000,
// well under PG's 65535 bound-param cap.
const bulkUpsertChunk = 500

// BulkUpsertSubscriptionIPs inserts all records; rows colliding on
// idx_sub_ip update their last_seen, node_id, updated_at columns.
// Empty input is a no-op.
func (r *subscriptionIPRepository) BulkUpsertSubscriptionIPs(ctx context.Context, records []SubscriptionIPRecord) error {
	if len(records) == 0 {
		return nil
	}
	now := time.Now()
	rows := make([]domain.SubscriptionIP, 0, len(records))
	for _, rec := range records {
		if rec.SubscriptionID == 0 || rec.IP == "" {
			continue
		}
		rows = append(rows, domain.SubscriptionIP{
			SubscriptionID: rec.SubscriptionID,
			IP:             rec.IP,
			NodeID:         rec.NodeID,
			FirstSeen:      now,
			LastSeen:       now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return database.GetExecutor(r.db, ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subscription_id"}, {Name: "ip"}, {Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen", "updated_at"}),
	}).CreateInBatches(rows, bulkUpsertChunk).Error
}

// GetSubscriptionIPs returns all IPs ever seen for a subscription
func (r *subscriptionIPRepository) GetSubscriptionIPs(ctx context.Context, subscriptionID uint) ([]domain.SubscriptionIP, error) {
	var ips []domain.SubscriptionIP
	err := database.GetExecutor(r.db, ctx).
		Where("subscription_id = ?", subscriptionID).
		Order("last_seen DESC").
		Find(&ips).Error
	return ips, err
}

// GetSubscriptionActiveIPs returns IPs seen since the given timestamp
func (r *subscriptionIPRepository) GetSubscriptionActiveIPs(ctx context.Context, subscriptionID uint, since time.Time) ([]domain.SubscriptionIP, error) {
	var ips []domain.SubscriptionIP
	err := database.GetExecutor(r.db, ctx).
		Where("subscription_id = ? AND last_seen >= ?", subscriptionID, since).
		Order("last_seen DESC").
		Find(&ips).Error
	return ips, err
}

// DeleteOldSubscriptionIPs removes IP records older than the given timestamp
func (r *subscriptionIPRepository) DeleteOldSubscriptionIPs(ctx context.Context, olderThan time.Time) (int64, error) {
	result := database.GetExecutor(r.db, ctx).
		Where("last_seen < ?", olderThan).
		Delete(&domain.SubscriptionIP{})
	return result.RowsAffected, result.Error
}
