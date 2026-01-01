package engine

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// fakeRepo is an in-memory AlertRepository for tests — avoids spinning up
// a real DB. Only the engine's call sites are implemented.
type fakeRepo struct {
	rules  []*domain.Rule
	states map[string]*domain.State
	events []*domain.Event
}

func newFakeRepo(rules ...*domain.Rule) *fakeRepo {
	return &fakeRepo{rules: rules, states: map[string]*domain.State{}}
}

func (f *fakeRepo) stateKey(ruleID uint, entity string) string {
	return entity + "@" + string(rune(ruleID))
}

func (f *fakeRepo) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	return f.rules, nil
}

func (f *fakeRepo) ListEnabledRules(ctx context.Context) ([]*domain.Rule, error) {
	out := make([]*domain.Rule, 0)
	for _, r := range f.rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetRule(ctx context.Context, id uint) (*domain.Rule, error) {
	for _, r := range f.rules {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) GetRuleByType(ctx context.Context, t domain.RuleType) (*domain.Rule, error) {
	for _, r := range f.rules {
		if r.RuleType == t {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) CreateRule(ctx context.Context, r *domain.Rule) error {
	f.rules = append(f.rules, r)
	return nil
}

func (f *fakeRepo) UpdateRule(ctx context.Context, r *domain.Rule) error { return nil }
func (f *fakeRepo) DeleteRule(ctx context.Context, id uint) error        { return nil }

func (f *fakeRepo) GetState(ctx context.Context, ruleID uint, entityKey string) (*domain.State, error) {
	if s, ok := f.states[f.stateKey(ruleID, entityKey)]; ok {
		return s, nil
	}
	return nil, nil
}

func (f *fakeRepo) UpsertState(ctx context.Context, s *domain.State) error {
	f.states[f.stateKey(s.RuleID, s.EntityKey)] = s
	return nil
}

func (f *fakeRepo) ListFiringStates(ctx context.Context) ([]*domain.State, error) {
	out := make([]*domain.State, 0)
	for _, s := range f.states {
		if s.Firing {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) DeleteState(ctx context.Context, ruleID uint, entityKey string) error {
	delete(f.states, f.stateKey(ruleID, entityKey))
	return nil
}

func (f *fakeRepo) InsertEvent(ctx context.Context, e *domain.Event) error {
	f.events = append(f.events, e)
	return nil
}

func (f *fakeRepo) ListEvents(ctx context.Context, limit int) ([]*domain.Event, error) {
	return f.events, nil
}

func (f *fakeRepo) CleanupOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	return 0, nil
}

func (f *fakeRepo) ListEventsByRule(ctx context.Context, ruleID uint, limit int) ([]*domain.Event, error) {
	out := make([]*domain.Event, 0)
	for _, e := range f.events {
		if e.RuleID == ruleID {
			out = append(out, e)
		}
	}
	return out, nil
}

// TestNodeOfflineFireAndResolve verifies a single fire + single resolve
// across a node offline→online transition. Bypasses grace period by
// backdating startTime.
func TestNodeOfflineFireAndResolve(t *testing.T) {
	rule := &domain.Rule{
		ID:          1,
		Name:        "offline",
		RuleType:    domain.RuleTypeNodeOffline,
		Scope:       domain.ScopeGlobal,
		Enabled:     true,
		CooldownSec: 900,
	}
	repo := newFakeRepo(rule)
	bus := events.NewEventBus()
	eng := NewEngine(bus, repo)
	eng.startTime = time.Now().Add(-10 * time.Minute) // bypass grace

	ctx := context.Background()
	p := events.NodeStatusPayload{NodeID: 7, NodeName: "n7", IP: "1.2.3.4"}

	eng.evalNodeOffline(ctx, p)
	if len(repo.events) != 1 || repo.events[0].Status != domain.EventStatusFired {
		t.Fatalf("expected 1 fired event, got %+v", repo.events)
	}

	// Repeat offline within cooldown — should NOT produce a second fire.
	eng.evalNodeOffline(ctx, p)
	if len(repo.events) != 1 {
		t.Fatalf("expected cooldown to suppress second fire, got %d events", len(repo.events))
	}

	// Online event → resolve.
	eng.resolveNodeOffline(ctx, p)
	if len(repo.events) != 2 || repo.events[1].Status != domain.EventStatusResolved {
		t.Fatalf("expected resolved event, got %+v", repo.events)
	}
}

// TestGracePeriodSuppressesOffline confirms we do NOT fire node_offline
// during the post-start grace window.
func TestGracePeriodSuppressesOffline(t *testing.T) {
	rule := &domain.Rule{
		ID:       1,
		Name:     "offline",
		RuleType: domain.RuleTypeNodeOffline,
		Scope:    domain.ScopeGlobal,
		Enabled:  true,
	}
	repo := newFakeRepo(rule)
	bus := events.NewEventBus()
	eng := NewEngine(bus, repo)
	eng.startTime = time.Now() // still in grace

	eng.evalNodeOffline(context.Background(), events.NodeStatusPayload{NodeID: 1})
	if len(repo.events) != 0 {
		t.Fatalf("grace period should suppress fire; got %d events", len(repo.events))
	}
}

// TestCrashLoopFiresAtThreshold asserts that we only fire once the crash
// window count exceeds the configured threshold, not before.
func TestCrashLoopFiresAtThreshold(t *testing.T) {
	rule := &domain.Rule{
		ID:          1,
		Name:        "loop",
		RuleType:    domain.RuleTypeNodeCrashLoop,
		Scope:       domain.ScopeGlobal,
		Enabled:     true,
		CooldownSec: 600,
		Threshold:   domain.Threshold{Count: 3, WindowSec: 300},
	}
	repo := newFakeRepo(rule)
	bus := events.NewEventBus()
	eng := NewEngine(bus, repo)
	eng.startTime = time.Now().Add(-10 * time.Minute)

	ctx := context.Background()
	p := events.XrayStatusPayload{NodeID: 9, NodeName: "n9"}

	// 2 crashes — below threshold.
	eng.recordCrash(9)
	eng.evalCrashLoop(ctx, p)
	eng.recordCrash(9)
	eng.evalCrashLoop(ctx, p)
	if len(repo.events) != 0 {
		t.Fatalf("should not fire below threshold; got %d events", len(repo.events))
	}

	// 3rd crash hits threshold.
	eng.recordCrash(9)
	eng.evalCrashLoop(ctx, p)
	if len(repo.events) != 1 {
		t.Fatalf("expected fire at threshold; got %d events", len(repo.events))
	}
}

// TestSustainedCPUFiresAfterDuration confirms high_cpu only fires once
// the over-threshold window exceeds DurationSec.
func TestSustainedCPUFiresAfterDuration(t *testing.T) {
	rule := &domain.Rule{
		ID:          1,
		Name:        "cpu",
		RuleType:    domain.RuleTypeHighCPU,
		Scope:       domain.ScopeGlobal,
		Enabled:     true,
		CooldownSec: 600,
		Threshold:   domain.Threshold{Value: 80, DurationSec: 2},
	}
	repo := newFakeRepo(rule)
	bus := events.NewEventBus()
	eng := NewEngine(bus, repo)
	eng.startTime = time.Now().Add(-10 * time.Minute)

	ctx := context.Background()
	p := events.NodeStatsPayload{NodeID: 3, CPUPercent: 95}

	// First event sets firstOverAt — no fire yet.
	eng.evalStatsForRule(ctx, rule, p)
	if len(repo.events) != 0 {
		t.Fatalf("should not fire before duration elapses")
	}

	// Backdate the tracker so DurationSec appears exceeded.
	key := "1:node:3"
	eng.mu.Lock()
	eng.firstOverAt[key] = time.Now().Add(-3 * time.Second)
	eng.mu.Unlock()

	eng.evalStatsForRule(ctx, rule, p)
	if len(repo.events) != 1 {
		t.Fatalf("expected fire after sustained duration; got %d events", len(repo.events))
	}

	// Drop below threshold — resolves.
	p.CPUPercent = 40
	eng.evalStatsForRule(ctx, rule, p)
	if len(repo.events) != 2 || repo.events[1].Status != domain.EventStatusResolved {
		t.Fatalf("expected resolve on recovery; got %+v", repo.events)
	}
}

// TestScopeNodeIDsFilters checks that scope=node_ids limits firing to
// matching node IDs and leaves others alone.
func TestScopeNodeIDsFilters(t *testing.T) {
	rule := &domain.Rule{
		ID:         1,
		Name:       "offline only node 5",
		RuleType:   domain.RuleTypeNodeOffline,
		Scope:      domain.ScopeNodeIDs,
		ScopeValue: "[5]",
		Enabled:    true,
	}
	repo := newFakeRepo(rule)
	bus := events.NewEventBus()
	eng := NewEngine(bus, repo)
	eng.startTime = time.Now().Add(-10 * time.Minute)

	ctx := context.Background()
	eng.evalNodeOffline(ctx, events.NodeStatusPayload{NodeID: 6}) // excluded
	if len(repo.events) != 0 {
		t.Fatalf("scope filter failed; got %d events", len(repo.events))
	}
	eng.evalNodeOffline(ctx, events.NodeStatusPayload{NodeID: 5}) // included
	if len(repo.events) != 1 {
		t.Fatalf("scope match should fire; got %d events", len(repo.events))
	}
}
