package engine

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func mtprotoEngine(rules ...*domain.Rule) (*Engine, *fakeRepo) {
	repo := newFakeRepo(rules...)
	eng := NewEngine(events.NewEventBus(), repo)
	eng.startTime = time.Now().Add(-10 * time.Minute)
	return eng, repo
}

func mtprotoEvent(t events.EventType, nodeID uint) events.Event {
	return events.Event{Type: t, Payload: events.MTProtoPayload{NodeID: nodeID, NodeName: "vps", Detail: "149.154.167.51:443", Error: "i/o timeout"}}
}

func countStatus(repo *fakeRepo, s domain.EventStatus) int {
	n := 0
	for _, e := range repo.events {
		if e.Status == s {
			n++
		}
	}
	return n
}

func TestMTProtoViolationsFirePastTheThresholdAndHoldTheCooldown(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "violations", RuleType: domain.RuleTypeMTProtoAllowlistViolation, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 20, WindowSec: 300},
	})
	ctx := context.Background()
	for i := 0; i < 19; i++ {
		eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3))
	}
	if len(repo.events) != 0 {
		t.Fatalf("fired below the threshold: %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3))
	if countStatus(repo, domain.EventStatusFired) != 1 {
		t.Fatalf("want one fire at the threshold, got %+v", repo.events)
	}
	for i := 0; i < 50; i++ {
		eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3))
	}
	if len(repo.events) != 1 {
		t.Fatalf("the cooldown let %d more through", len(repo.events)-1)
	}
	// another node has its own window
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 4))
	if len(repo.events) != 1 {
		t.Fatal("one violation on another node fired")
	}
}

func TestMTProtoRulesHonourNodeScope(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "violations on 5", RuleType: domain.RuleTypeMTProtoAllowlistViolation, Scope: domain.ScopeNodeIDs,
		ScopeValue: "[5]", Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 3, WindowSec: 300},
	}, &domain.Rule{
		ID: 2, Name: "cover site on 5", RuleType: domain.RuleTypeMTProtoDisguiseDown, Scope: domain.ScopeNodeIDs,
		ScopeValue: "[5]", Enabled: true, CooldownSec: 900,
	})
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 6))
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseDown, 6))
	if len(repo.events) != 0 {
		t.Fatalf("an out-of-scope node fired: %+v", repo.events)
	}
	for i := 0; i < 3; i++ {
		eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 5))
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseDown, 5))
	if len(repo.events) != 2 {
		t.Fatalf("in-scope node: want 2 fires, got %+v", repo.events)
	}
}

func TestTelemtCrashLoopFiresNodeCrashLoopAndTelemtUpResolvesIt(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "loop", RuleType: domain.RuleTypeNodeCrashLoop, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 600, Threshold: domain.Threshold{Count: 5, WindowSec: 300},
	})
	ctx := context.Background()
	// the agent already counted the exits: one event is the threshold
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoTelemtCrashLoop, 8))
	if countStatus(repo, domain.EventStatusFired) != 1 || repo.events[0].EntityKey != "node:8" {
		t.Fatalf("want one fire for node 8, got %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoTelemtCrashLoop, 8)) // repeated on every status poll
	if len(repo.events) != 1 {
		t.Fatalf("the cooldown did not hold: %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoTelemtUp, 8))
	if countStatus(repo, domain.EventStatusResolved) != 1 {
		t.Fatalf("telemt_up did not resolve: %+v", repo.events)
	}
}

func TestMTProtoEgressFiresAndResolvesAfterAQuietWindow(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "egress", RuleType: domain.RuleTypeMTProtoEgressDown, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 5, WindowSec: 120},
	})
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoEgressFailed, 2))
	}
	if len(repo.events) != 0 {
		t.Fatal("fired below the threshold")
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoEgressFailed, 2))
	if countStatus(repo, domain.EventStatusFired) != 1 {
		t.Fatalf("want one fire, got %+v", repo.events)
	}
	// still failing within the window: the tick leaves it firing
	eng.tick(ctx)
	if countStatus(repo, domain.EventStatusResolved) != 0 {
		t.Fatal("resolved while failures are still in the window")
	}
	// the window goes quiet
	eng.mu.Lock()
	for k, w := range eng.mtprotoWindows {
		for i := range w {
			w[i].at = w[i].at.Add(-3 * time.Minute)
		}
		eng.mtprotoWindows[k] = w
	}
	eng.mu.Unlock()
	eng.tick(ctx)
	if countStatus(repo, domain.EventStatusResolved) != 1 {
		t.Fatalf("a quiet window did not resolve: %+v", repo.events)
	}
	eng.tick(ctx)
	if len(repo.events) != 2 {
		t.Fatalf("resolved twice: %+v", repo.events)
	}
}

func TestMTProtoDisguiseDownFiresAndUpResolves(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "cover", RuleType: domain.RuleTypeMTProtoDisguiseDown, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 3600,
	})
	ctx := context.Background()
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseUp, 1)) // nothing firing yet: quiet
	if len(repo.events) != 0 {
		t.Fatalf("up with nothing firing produced %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseDown, 1))
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseDown, 1))
	if len(repo.events) != 1 || repo.events[0].Status != domain.EventStatusFired {
		t.Fatalf("want one fire, got %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoDisguiseUp, 1))
	if len(repo.events) != 2 || repo.events[1].Status != domain.EventStatusResolved {
		t.Fatalf("want a resolve, got %+v", repo.events)
	}
}

// The manager folds a flood into one event that carries its size; the
// threshold still means that many occurrences.
func TestMTProtoCountingRulesCountWhatAnEventCarries(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "violations", RuleType: domain.RuleTypeMTProtoAllowlistViolation, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 20, WindowSec: 300},
	})
	ctx := context.Background()
	ev := mtprotoEvent(events.EventMTProtoViolation, 3)
	p := ev.Payload.(events.MTProtoPayload)
	p.Count = 19
	ev.Payload = p
	eng.handleEvent(ctx, ev)
	if len(repo.events) != 0 {
		t.Fatalf("19 occurrences fired a threshold of 20: %+v", repo.events)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3))
	if countStatus(repo, domain.EventStatusFired) != 1 {
		t.Fatalf("the 20th occurrence did not fire: %+v", repo.events)
	}
}

// A counting alert firing when the panel stopped lives on only in the database:
// once the panel is back and the node stays quiet for a window, it resolves.
func TestMTProtoCountingAlertsFiringAtShutdownResolveAfterARestart(t *testing.T) {
	rule := &domain.Rule{
		ID: 1, Name: "egress", RuleType: domain.RuleTypeMTProtoEgressDown, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 5, WindowSec: 120},
	}
	repo := newFakeRepo(rule)
	fired := time.Now().Add(-time.Hour)
	repo.states[repo.stateKey(1, "node:3")] = &domain.State{RuleID: 1, EntityKey: "node:3", Firing: true, FirstTriggeredAt: &fired, LastNotifiedAt: &fired}
	eng := NewEngine(events.NewEventBus(), repo)
	eng.Start(context.Background())
	eng.Stop()
	ctx := context.Background()

	eng.tick(ctx)
	if countStatus(repo, domain.EventStatusResolved) != 0 {
		t.Fatal("resolved before the node had been watched for a whole window")
	}
	eng.startTime = time.Now().Add(-3 * time.Minute) // a quiet window has passed
	eng.tick(ctx)
	if countStatus(repo, domain.EventStatusResolved) != 1 {
		t.Fatalf("the stale alert was not resolved: %+v", repo.events)
	}
}
