package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AlertRepository persists rules, firing state, and the audit log.
type AlertRepository interface {
	// Rules
	ListRules(ctx context.Context) ([]*domain.Rule, error)
	ListEnabledRules(ctx context.Context) ([]*domain.Rule, error)
	GetRule(ctx context.Context, id uint) (*domain.Rule, error)
	GetRuleByType(ctx context.Context, t domain.RuleType) (*domain.Rule, error)
	CreateRule(ctx context.Context, r *domain.Rule) error
	UpdateRule(ctx context.Context, r *domain.Rule) error
	DeleteRule(ctx context.Context, id uint) error

	// State (upsert semantics — one row per (rule, entity))
	GetState(ctx context.Context, ruleID uint, entityKey string) (*domain.State, error)
	UpsertState(ctx context.Context, s *domain.State) error
	ListFiringStates(ctx context.Context) ([]*domain.State, error)
	DeleteState(ctx context.Context, ruleID uint, entityKey string) error

	// Events
	InsertEvent(ctx context.Context, e *domain.Event) error
	ListEvents(ctx context.Context, limit int) ([]*domain.Event, error)
	ListEventsByRule(ctx context.Context, ruleID uint, limit int) ([]*domain.Event, error)
	CleanupOldEvents(ctx context.Context, olderThanDays int) (int64, error)
}

type alertRepository struct {
	db *gorm.DB
}

func NewAlertRepository(db *gorm.DB) AlertRepository {
	return &alertRepository{db: db}
}

// ------- Rules -------

func (r *alertRepository) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	var rules []*domain.Rule
	err := r.db.WithContext(ctx).Order("id asc").Find(&rules).Error
	return rules, err
}

func (r *alertRepository) ListEnabledRules(ctx context.Context) ([]*domain.Rule, error) {
	var rules []*domain.Rule
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("id asc").Find(&rules).Error
	return rules, err
}

func (r *alertRepository) GetRule(ctx context.Context, id uint) (*domain.Rule, error) {
	var rule domain.Rule
	if err := r.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *alertRepository) GetRuleByType(ctx context.Context, t domain.RuleType) (*domain.Rule, error) {
	var rule domain.Rule
	if err := r.db.WithContext(ctx).Where("rule_type = ?", t).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *alertRepository) CreateRule(ctx context.Context, rule *domain.Rule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *alertRepository) UpdateRule(ctx context.Context, rule *domain.Rule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *alertRepository) DeleteRule(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.Rule{}, id).Error
}

// ------- State -------

func (r *alertRepository) GetState(ctx context.Context, ruleID uint, entityKey string) (*domain.State, error) {
	var s domain.State
	err := r.db.WithContext(ctx).
		Where("rule_id = ? AND entity_key = ?", ruleID, entityKey).
		First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpsertState merges into the existing row when (rule_id, entity_key)
// already exists. Uses ON CONFLICT so the engine can call it every eval
// tick without an extra read.
func (r *alertRepository) UpsertState(ctx context.Context, s *domain.State) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "rule_id"}, {Name: "entity_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"firing",
			"first_triggered_at",
			"last_notified_at",
			"last_seen_value",
			"updated_at",
		}),
	}).Create(s).Error
}

func (r *alertRepository) ListFiringStates(ctx context.Context) ([]*domain.State, error) {
	var ss []*domain.State
	err := r.db.WithContext(ctx).Where("firing = ?", true).Find(&ss).Error
	return ss, err
}

func (r *alertRepository) DeleteState(ctx context.Context, ruleID uint, entityKey string) error {
	return r.db.WithContext(ctx).
		Where("rule_id = ? AND entity_key = ?", ruleID, entityKey).
		Delete(&domain.State{}).Error
}

// ------- Events -------

func (r *alertRepository) InsertEvent(ctx context.Context, e *domain.Event) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *alertRepository) ListEvents(ctx context.Context, limit int) ([]*domain.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	var events []*domain.Event
	err := r.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&events).Error
	return events, err
}

func (r *alertRepository) ListEventsByRule(ctx context.Context, ruleID uint, limit int) ([]*domain.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []*domain.Event
	err := r.db.WithContext(ctx).
		Where("rule_id = ?", ruleID).
		Order("created_at desc").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// CleanupOldEvents prunes alerting Event rows older than the retention window.
// Events are an immutable audit log — a caller passing 0 should be interpreted
// as "keep forever" at the scheduler layer; this method always deletes when
// called, which is why it's guarded in task_maintenance.go.
func (r *alertRepository) CleanupOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&domain.Event{})
	return result.RowsAffected, result.Error
}
