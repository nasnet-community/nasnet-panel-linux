package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

// NodeAccountCount carries the per-node account totals returned by
// CountByNodes. Active excludes disabled + pending_provision + expired.
type NodeAccountCount struct {
	Total  int64
	Active int64
}

// AccountTrafficRef is the minimal projection the stats sweep needs to
// attribute per-email traffic to accounts — no relation preloads.
type AccountTrafficRef struct {
	ID        uint
	Email     string
	InboundID uint
}

// AccountFilter defines criteria for listing accounts
type AccountFilter struct {
	Offset    int
	Limit     int
	Status    domain.AccountStatus
	Search    string // email or uuid
	Exhausted *bool
	// Additional filters
	NodeID    *uint  // Filter by node
	InboundID *uint  // Filter by inbound
	Source    string // Filter by source (subscription, manual, import)
}

type AccountRepository interface {
	Create(ctx context.Context, account *domain.Account) error
	FindByID(ctx context.Context, id uint) (*domain.Account, error)
	FindByEmail(ctx context.Context, email string) (*domain.Account, error)
	FindAllByEmail(ctx context.Context, email string) ([]*domain.Account, error)
	// FindByEmails returns one row per email × inbound match. Subscription is
	// preloaded so callers can resolve the owning sub/user without per-row
	// follow-up queries.
	FindByEmails(ctx context.Context, emails []string) ([]*domain.Account, error)
	// FindBySubscriptionIDs is the batched variant used by global access
	// history search to collapse N per-sub lookups into one query.
	FindBySubscriptionIDs(ctx context.Context, subIDs []uint) ([]*domain.Account, error)
	ListByInboundID(ctx context.Context, inboundID uint) ([]*domain.Account, error)
	ListByNodeID(ctx context.Context, nodeID uint) ([]*domain.Account, error)
	// ListTrafficRefsByNode returns (id, email, inbound_id) for every
	// account on the node in one query — replaces the stats sweep's
	// per-(email, inbound) FindByEmailAndInbound lookups.
	ListTrafficRefsByNode(ctx context.Context, nodeID uint) ([]AccountTrafficRef, error)
	ListByNodeIDPaginated(ctx context.Context, nodeID uint, offset, limit int) ([]*domain.Account, int64, error)
	ListBySubscriptionID(ctx context.Context, subID uint) ([]*domain.Account, error)
	ListAllBySubscriptionID(ctx context.Context, subID uint) ([]*domain.Account, error)
	ListActive(ctx context.Context) ([]*domain.Account, error)
	ListAll(ctx context.Context, filter AccountFilter) ([]*domain.Account, error)
	Count(ctx context.Context, filter AccountFilter) (int64, error)
	// CountByNodes returns per-node total + active account counts in a
	// single GROUP BY query. Zero-count nodes are omitted — callers
	// should default to {0,0}.
	CountByNodes(ctx context.Context, nodeIDs []uint) (map[uint]NodeAccountCount, error)
	UpdateStatus(ctx context.Context, id uint, status domain.AccountStatus) error
	UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error
	AddDataUsed(ctx context.Context, id uint, bytes int64) error
	Delete(ctx context.Context, id uint) error
	ForceDelete(ctx context.Context, id uint) error
	ForceDeleteBySubscriptionID(ctx context.Context, subID uint) error
	FindByEmailUnscoped(ctx context.Context, email string) (*domain.Account, error)
	FindByEmailAndInbound(ctx context.Context, email string, inboundID uint) (*domain.Account, error)
	FindByEmailAndInboundUnscoped(ctx context.Context, email string, inboundID uint) (*domain.Account, error)
	UpdateLastActive(ctx context.Context, id uint, t time.Time) error
	UpdateInbound(ctx context.Context, id, inboundID uint) error
	// UpdateInboundAndCredentials atomically updates an account's inbound, flow, and encryption.
	// Used for cross-protocol inbound migration.
	UpdateInboundAndCredentials(ctx context.Context, id, inboundID uint, flow, encryption string) error
	Update(ctx context.Context, account *domain.Account) error
	ListByUserID(ctx context.Context, userID uint) ([]*domain.Account, error)
	ListDataExhausted(ctx context.Context) ([]*domain.Account, error)
	UpdateDataLimitBySubscriptionID(ctx context.Context, subID uint, dataLimit int64) error
	ResetDataUsedBySubscriptionID(ctx context.Context, subID uint) error
	ExistsByUUIDAndInbound(ctx context.Context, uuid string, inboundID uint, excludeAccountID uint) (bool, error)
}

type accountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) AccountRepository {
	return &accountRepository{db: db}
}

func (r *accountRepository) Update(ctx context.Context, account *domain.Account) error {
	return database.GetExecutor(r.db, ctx).Save(account).Error
}

func (r *accountRepository) UpdateInbound(ctx context.Context, id, inboundID uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Update("inbound_id", inboundID).Error
}

func (r *accountRepository) UpdateInboundAndCredentials(ctx context.Context, id, inboundID uint, flow, encryption string) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"inbound_id": inboundID,
			"flow":       flow,
			"encryption": encryption,
		}).Error
}

func (r *accountRepository) Create(ctx context.Context, account *domain.Account) error {
	return database.GetExecutor(r.db, ctx).Create(account).Error
}

func (r *accountRepository) FindByID(ctx context.Context, id uint) (*domain.Account, error) {
	var account domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User").
		First(&account, id).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *accountRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	var account domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User").
		Where("email = ?", email).
		First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// FindAllByEmail returns all accounts with a given email across all inbounds
// Used for traffic sync where one email may have multiple accounts on different inbounds
func (r *accountRepository) FindAllByEmail(ctx context.Context, email string) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Where("email = ?", email).
		Find(&accounts).Error
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

// FindByEmails returns all accounts whose email is in the supplied set,
// with Subscription + Inbound preloaded for downstream resolution.
// Empty input returns (nil, nil) so callers don't need to special-case.
func (r *accountRepository) FindByEmails(ctx context.Context, emails []string) ([]*domain.Account, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Subscription").
		Preload("Subscription.User").
		Where("email IN ?", emails).
		Find(&accounts).Error
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

// FindBySubscriptionIDs returns all accounts whose subscription_id is in
// the supplied set, with Subscription + Inbound preloaded. One query —
// collapses the N+1 per-sub loop the global access search previously did.
func (r *accountRepository) FindBySubscriptionIDs(ctx context.Context, subIDs []uint) ([]*domain.Account, error) {
	if len(subIDs) == 0 {
		return nil, nil
	}
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Subscription").
		Preload("Subscription.User").
		Where("subscription_id IN ?", subIDs).
		Find(&accounts).Error
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) FindByEmailUnscoped(ctx context.Context, email string) (*domain.Account, error) {
	var account domain.Account
	err := database.GetExecutor(r.db, ctx).
		Unscoped().
		Where("email = ?", email).
		First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *accountRepository) FindByEmailAndInbound(ctx context.Context, email string, inboundID uint) (*domain.Account, error) {
	var account domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User").
		Where("email = ? AND inbound_id = ?", email, inboundID).
		First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *accountRepository) FindByEmailAndInboundUnscoped(ctx context.Context, email string, inboundID uint) (*domain.Account, error) {
	var account domain.Account
	err := database.GetExecutor(r.db, ctx).
		Unscoped().
		Where("email = ? AND inbound_id = ?", email, inboundID).
		First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// ... List methods ...

func (r *accountRepository) ForceDelete(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Unscoped().Delete(&domain.Account{}, id).Error
}

func (r *accountRepository) ForceDeleteBySubscriptionID(ctx context.Context, subID uint) error {
	return database.GetExecutor(r.db, ctx).Unscoped().Where("subscription_id = ?", subID).Delete(&domain.Account{}).Error
}

func (r *accountRepository) ListByInboundID(ctx context.Context, inboundID uint) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("inbound_id = ?", inboundID).
		Order("created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

func (r *accountRepository) ListByNodeID(ctx context.Context, nodeID uint) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User").
		Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
		Where("inbounds.node_id = ?", nodeID).
		Order("accounts.created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

// ListTrafficRefsByNode: bare-column projection (no preloads) of all
// accounts on a node. Model() keeps GORM's soft-delete scope so the set
// matches what FindByEmailAndInbound would have returned per pair.
func (r *accountRepository) ListTrafficRefsByNode(ctx context.Context, nodeID uint) ([]AccountTrafficRef, error) {
	var refs []AccountTrafficRef
	err := database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Select("accounts.id, accounts.email, accounts.inbound_id").
		Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
		Where("inbounds.node_id = ?", nodeID).
		Scan(&refs).Error
	return refs, err
}

func (r *accountRepository) ListByNodeIDPaginated(ctx context.Context, nodeID uint, offset, limit int) ([]*domain.Account, int64, error) {
	var accounts []*domain.Account
	var total int64

	base := database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
		Where("inbounds.node_id = ?", nodeID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User").
		Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
		Where("inbounds.node_id = ?", nodeID).
		Order("accounts.created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&accounts).Error
	return accounts, total, err
}

func (r *accountRepository) ListBySubscriptionID(ctx context.Context, subID uint) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("subscription_id = ? AND source != ?", subID, domain.AccountSourceAdminExcluded).
		Order("created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

// ListAllBySubscriptionID returns all accounts for a subscription including admin_excluded ones.
// Used by BulkManageInbounds to find excluded accounts for reactivation.
func (r *accountRepository) ListAllBySubscriptionID(ctx context.Context, subID uint) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("subscription_id = ?", subID).
		Order("created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

func (r *accountRepository) ListActive(ctx context.Context) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("status = ?", domain.AccountStatusActive).
		Find(&accounts).Error
	return accounts, err
}

func (r *accountRepository) applyFilter(db *gorm.DB, filter AccountFilter) *gorm.DB {
	query := db
	if filter.Status != "" {
		query = query.Where("accounts.status = ?", filter.Status)
	}
	if filter.Search != "" {
		search := "%" + filter.Search + "%"
		query = query.Where("accounts.email LIKE ? OR accounts.uuid LIKE ?", search, search)
	}
	if filter.Exhausted != nil {
		if *filter.Exhausted {
			query = query.Where("accounts.data_limit > 0 AND accounts.data_used >= accounts.data_limit")
		} else {
			query = query.Where("accounts.data_limit = 0 OR accounts.data_used < accounts.data_limit")
		}
	}
	// New filters
	if filter.NodeID != nil {
		query = query.Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
			Where("inbounds.node_id = ?", *filter.NodeID)
	}
	if filter.InboundID != nil {
		query = query.Where("accounts.inbound_id = ?", *filter.InboundID)
	}
	if filter.Source != "" {
		query = query.Where("accounts.source = ?", filter.Source)
	}
	return query
}

func (r *accountRepository) ListAll(ctx context.Context, filter AccountFilter) ([]*domain.Account, error) {
	var accounts []*domain.Account
	query := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Preload("Subscription").
		Preload("Subscription.User")

	query = r.applyFilter(query, filter)

	err := query.
		Offset(filter.Offset).
		Limit(filter.Limit).
		Order("created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

func (r *accountRepository) UpdateStatus(ctx context.Context, id uint, status domain.AccountStatus) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *accountRepository) UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Update("data_used", dataUsed).Error
}

func (r *accountRepository) AddDataUsed(ctx context.Context, id uint, bytes int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Update("data_used", gorm.Expr("data_used + ?", bytes)).Error
}

func (r *accountRepository) UpdateLastActive(ctx context.Context, id uint, t time.Time) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("id = ?", id).
		Update("last_activity_at", t).Error
}

func (r *accountRepository) Delete(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Delete(&domain.Account{}, id).Error
}

// ListByUserID returns all accounts belonging to a user (via subscriptions)
func (r *accountRepository) ListByUserID(ctx context.Context, userID uint) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("subscription_id IN (SELECT id FROM subscriptions WHERE user_id = ? AND deleted_at IS NULL)", userID).
		Order("created_at DESC").
		Find(&accounts).Error
	return accounts, err
}

func (r *accountRepository) Count(ctx context.Context, filter AccountFilter) (int64, error) {
	var count int64
	query := database.GetExecutor(r.db, ctx).Model(&domain.Account{})
	query = r.applyFilter(query, filter)
	err := query.Count(&count).Error
	return count, err
}

// CountByNodes: per-node account totals via one JOIN+GROUP BY through
// inbounds (accounts have no node_id). Used by the stats sweep instead
// of 2 Counts per node.
func (r *accountRepository) CountByNodes(ctx context.Context, nodeIDs []uint) (map[uint]NodeAccountCount, error) {
	out := make(map[uint]NodeAccountCount)
	if len(nodeIDs) == 0 {
		return out, nil
	}

	type row struct {
		NodeID uint  `gorm:"column:node_id"`
		Total  int64 `gorm:"column:total"`
		Active int64 `gorm:"column:active"`
	}
	var rows []row

	// SUM(CASE WHEN ...) is portable across Postgres + SQLite (both
	// supported by this repo). A FILTER clause would be cleaner on
	// Postgres but sqlite lacks it.
	err := database.GetExecutor(r.db, ctx).
		Table("accounts").
		Select(
			"inbounds.node_id AS node_id, "+
				"COUNT(*) AS total, "+
				"SUM(CASE WHEN accounts.status = ? THEN 1 ELSE 0 END) AS active",
			domain.AccountStatusActive,
		).
		Joins("JOIN inbounds ON inbounds.id = accounts.inbound_id").
		Where("inbounds.node_id IN ? AND accounts.deleted_at IS NULL", nodeIDs).
		Group("inbounds.node_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, r := range rows {
		out[r.NodeID] = NodeAccountCount{Total: r.Total, Active: r.Active}
	}
	return out, nil
}

// ListDataExhausted returns active accounts where data_limit > 0 and data_used >= data_limit
func (r *accountRepository) ListDataExhausted(ctx context.Context) ([]*domain.Account, error) {
	var accounts []*domain.Account
	err := database.GetExecutor(r.db, ctx).
		Preload("Inbound").
		Preload("Inbound.Node").
		Preload("Inbound.Hosts").
		Where("data_limit > 0 AND data_used >= data_limit AND status = ?", domain.AccountStatusActive).
		Find(&accounts).Error
	return accounts, err
}

// UpdateDataLimitBySubscriptionID updates data_limit for all accounts belonging to a subscription.
// Used when a subscription's custom data limit changes to keep account-level limits in sync.
func (r *accountRepository) UpdateDataLimitBySubscriptionID(ctx context.Context, subID uint, dataLimit int64) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("subscription_id = ?", subID).
		Update("data_limit", dataLimit).Error
}

// ResetDataUsedBySubscriptionID resets data_used to 0 for all accounts belonging to a subscription.
// Used on renewal so account-level usage tracking stays in sync with the subscription reset.
func (r *accountRepository) ResetDataUsedBySubscriptionID(ctx context.Context, subID uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("subscription_id = ?", subID).
		Update("data_used", 0).Error
}

func (r *accountRepository) ExistsByUUIDAndInbound(ctx context.Context, uuid string, inboundID uint, excludeAccountID uint) (bool, error) {
	var count int64
	err := database.GetExecutor(r.db, ctx).
		Model(&domain.Account{}).
		Where("uuid = ? AND inbound_id = ? AND id != ?", uuid, inboundID, excludeAccountID).
		Count(&count).Error
	return count > 0, err
}
