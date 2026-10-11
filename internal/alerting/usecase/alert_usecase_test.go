package usecase

import (
	"context"
	"strings"
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

func TestMTProtoRulesAreSeededDisabledAndOnlyOnce(t *testing.T) {
	repo := &ruleRepo{rules: []*domain.Rule{{Name: "mine", RuleType: domain.RuleTypeNodeOffline}}}
	uc := NewAlertUsecase(repo, nil)
	for i := 0; i < 2; i++ {
		if err := uc.SeedDefaults(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := map[domain.RuleType]domain.Threshold{
		domain.RuleTypeMTProtoAllowlistViolation: {Count: 20, WindowSec: 300},
		domain.RuleTypeMTProtoEgressDown:         {Count: 5, WindowSec: 120},
		domain.RuleTypeMTProtoDisguiseDown:       {},
	}
	seen := map[domain.RuleType]int{}
	for _, r := range repo.rules {
		seen[r.RuleType]++
		th, ok := want[r.RuleType]
		if !ok {
			continue
		}
		if r.Enabled || r.Threshold != th || validateRule(r) != nil {
			t.Errorf("%s seeded as %+v", r.RuleType, r)
		}
	}
	for rt := range want {
		if seen[rt] != 1 {
			t.Errorf("%s seeded %d times", rt, seen[rt])
		}
	}
	if seen[domain.RuleTypeNodeOffline] != 1 {
		t.Error("an existing type was seeded again")
	}
}

func (r *ruleRepo) RenameRule(_ context.Context, id uint, from, to string) error {
	for _, x := range r.rules {
		if x.ID == id && x.Name == from {
			x.Name = to
		}
	}
	return nil
}

// Rules are seeded once, so rows still on the old default names are renamed in place.
func TestSeedingRenamesTheOldRelayDefaults(t *testing.T) {
	repo := &ruleRepo{rules: []*domain.Rule{
		{ID: 1, Name: "Relay refused destinations", RuleType: domain.RuleTypeMTProtoAllowlistViolation},
		{ID: 2, Name: "Relay cannot reach Telegram", RuleType: domain.RuleTypeMTProtoEgressDown},
		{ID: 3, Name: "My cover site alert", RuleType: domain.RuleTypeMTProtoDisguiseDown},
		{ID: 4, Name: "Relay cover site down", RuleType: domain.RuleTypeMTProtoDisguiseDown},
	}}
	uc := NewAlertUsecase(repo, nil)
	for i := 0; i < 2; i++ {
		if err := uc.SeedDefaults(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := map[uint]string{1: "MTProto refused destinations", 2: "MTProto cannot reach Telegram", 3: "My cover site alert", 4: "MTProto cover site down"}
	for _, r := range repo.rules {
		if w, ok := want[r.ID]; ok && r.Name != w {
			t.Errorf("rule %d is %q, want %q", r.ID, r.Name, w)
		}
	}
	for _, r := range defaultRules() {
		if strings.Contains(r.Name, "Relay") {
			t.Errorf("a new install is seeded %q", r.Name)
		}
	}
}
