package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/repository"
)

// ruleRepo keeps rules in memory; the rest of the repository is never reached.
type ruleRepo struct {
	repository.AlertRepository
	rules []*domain.Rule
}

func (r *ruleRepo) ListRules(context.Context) ([]*domain.Rule, error) { return r.rules, nil }
func (r *ruleRepo) CreateRule(_ context.Context, x *domain.Rule) error {
	r.rules = append(r.rules, x)
	return nil
}

func TestDefaultRulesAreSeededOnlyOnce(t *testing.T) {
	repo := &ruleRepo{rules: []*domain.Rule{{Name: "mine", RuleType: domain.RuleTypeNodeOffline}}}
	uc := NewAlertUsecase(repo, nil)
	for i := 0; i < 2; i++ {
		if err := uc.SeedDefaults(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[domain.RuleType]int{}
	for _, r := range repo.rules {
		seen[r.RuleType]++
		if err := validateRule(r); err != nil {
			t.Errorf("%s seeded as %+v: %v", r.RuleType, r, err)
		}
	}
	for _, r := range defaultRules() {
		if seen[r.RuleType] != 1 {
			t.Errorf("%s seeded %d times", r.RuleType, seen[r.RuleType])
		}
	}
}
