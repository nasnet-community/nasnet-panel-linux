package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/engine"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/repository"
)

// AlertUsecase exposes admin-facing operations on rules + events.
type AlertUsecase interface {
	ListRules(ctx context.Context) ([]*domain.Rule, error)
	GetRule(ctx context.Context, id uint) (*domain.Rule, error)
	CreateRule(ctx context.Context, r *domain.Rule) error
	UpdateRule(ctx context.Context, r *domain.Rule) error
	SetEnabled(ctx context.Context, id uint, enabled bool) error
	SetThreshold(ctx context.Context, id uint, t domain.Threshold, cooldownSec int) error
	DeleteRule(ctx context.Context, id uint) error

	ListEvents(ctx context.Context, limit int) ([]*domain.Event, error)

	// TestSend publishes a fake alert via the engine so the admin can
	// verify that Telegram/webhook routing is wired correctly.
	TestSend(ctx context.Context, id uint) error

	// SeedDefaults inserts the built-in rule set the first time the
	// table is empty. Idempotent — subsequent calls are no-ops.
	SeedDefaults(ctx context.Context) error
}

type alertUsecase struct {
	repo   repository.AlertRepository
	engine *engine.Engine
}

func NewAlertUsecase(repo repository.AlertRepository, eng *engine.Engine) AlertUsecase {
	return &alertUsecase{repo: repo, engine: eng}
}

func (u *alertUsecase) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	return u.repo.ListRules(ctx)
}

func (u *alertUsecase) GetRule(ctx context.Context, id uint) (*domain.Rule, error) {
	return u.repo.GetRule(ctx, id)
}

func (u *alertUsecase) CreateRule(ctx context.Context, r *domain.Rule) error {
	if err := validateRule(r); err != nil {
		return err
	}
	return u.repo.CreateRule(ctx, r)
}

func (u *alertUsecase) UpdateRule(ctx context.Context, r *domain.Rule) error {
	if err := validateRule(r); err != nil {
		return err
	}
	return u.repo.UpdateRule(ctx, r)
}

func (u *alertUsecase) SetEnabled(ctx context.Context, id uint, enabled bool) error {
	r, err := u.repo.GetRule(ctx, id)
	if err != nil {
		return err
	}
	r.Enabled = enabled
	return u.repo.UpdateRule(ctx, r)
}

func (u *alertUsecase) SetThreshold(ctx context.Context, id uint, t domain.Threshold, cooldownSec int) error {
	r, err := u.repo.GetRule(ctx, id)
	if err != nil {
		return err
	}
	r.Threshold = t
	if cooldownSec > 0 {
		r.CooldownSec = cooldownSec
	}
	if err := validateRule(r); err != nil {
		return err
	}
	return u.repo.UpdateRule(ctx, r)
}

func (u *alertUsecase) DeleteRule(ctx context.Context, id uint) error {
	return u.repo.DeleteRule(ctx, id)
}

func (u *alertUsecase) ListEvents(ctx context.Context, limit int) ([]*domain.Event, error) {
	return u.repo.ListEvents(ctx, limit)
}

func (u *alertUsecase) TestSend(ctx context.Context, id uint) error {
	r, err := u.repo.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if u.engine == nil {
		return errors.New("alert engine not initialised")
	}
	u.engine.TestFire(r)
	return nil
}

// validateRule catches obvious mistakes before they hit the DB. Keeps
// the evaluators simple by enforcing invariants up front.
func validateRule(r *domain.Rule) error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	switch r.RuleType {
	case domain.RuleTypeNodeOffline, domain.RuleTypeNodeCrashLoop,
		domain.RuleTypeHighCPU, domain.RuleTypeHighDisk:
	default:
		return fmt.Errorf("unknown rule_type %q", r.RuleType)
	}
	if r.CooldownSec < 0 {
		return errors.New("cooldown_sec must be >= 0")
	}
	switch r.Scope {
	case "", domain.ScopeGlobal:
		r.Scope = domain.ScopeGlobal
		r.ScopeValue = ""
	case domain.ScopeNodeIDs:
		if r.ScopeValue == "" {
			return errors.New("scope_value required when scope=node_ids")
		}
		var ids []uint
		if err := json.Unmarshal([]byte(r.ScopeValue), &ids); err != nil {
			return fmt.Errorf("scope_value must be JSON array of node ids: %w", err)
		}
		if len(ids) == 0 {
			return errors.New("scope_value must contain at least one id")
		}
	case domain.ScopeTag:
		// reserved for phase B
		return errors.New("scope=tag not supported yet")
	default:
		return fmt.Errorf("unknown scope %q", r.Scope)
	}
	return nil
}

// defaultRules returns the built-in rule set. Kept ENABLED=false so
// operators have to opt in after confirming Telegram is configured.
func defaultRules() []*domain.Rule {
	return []*domain.Rule{
		{
			Name:        "Node offline",
			RuleType:    domain.RuleTypeNodeOffline,
			Scope:       domain.ScopeGlobal,
			Threshold:   domain.Threshold{DurationSec: 180},
			CooldownSec: 900,
			Description: "Fires when a node stops responding to the hub.",
		},
		{
			Name:        "Xray crash loop",
			RuleType:    domain.RuleTypeNodeCrashLoop,
			Scope:       domain.ScopeGlobal,
			Threshold:   domain.Threshold{Count: 5, WindowSec: 300},
			CooldownSec: 1800,
			Description: "Fires when Xray restarts N times within a short window.",
		},
		{
			Name:        "High CPU",
			RuleType:    domain.RuleTypeHighCPU,
			Scope:       domain.ScopeGlobal,
			Threshold:   domain.Threshold{Value: 90, DurationSec: 300},
			CooldownSec: 1800,
			Description: "Fires when CPU stays above the threshold for the configured duration.",
		},
		{
			Name:        "High disk",
			RuleType:    domain.RuleTypeHighDisk,
			Scope:       domain.ScopeGlobal,
			Threshold:   domain.Threshold{Value: 90, DurationSec: 0},
			CooldownSec: 3600,
			Description: "Fires when disk usage crosses the threshold.",
		},
	}
}

// SeedDefaults inserts defaults once the table is empty. If any rule of
// a given type already exists, we skip that type so re-running never
// duplicates seeds.
func (u *alertUsecase) SeedDefaults(ctx context.Context) error {
	existing, err := u.repo.ListRules(ctx)
	if err != nil {
		return err
	}
	seenTypes := make(map[domain.RuleType]bool, len(existing))
	for _, r := range existing {
		seenTypes[r.RuleType] = true
	}
	for _, r := range defaultRules() {
		if seenTypes[r.RuleType] {
			continue
		}
		if err := u.repo.CreateRule(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
