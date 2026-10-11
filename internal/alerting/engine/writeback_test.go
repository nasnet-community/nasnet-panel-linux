package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// MTProto node events fire from rules cached for a second: firing must not write that copy back.
func TestFiringFromACachedRuleKeepsTheAdminsEdits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "alerts.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Rule{}, &domain.State{}, &domain.Event{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAlertRepository(db)
	ctx := context.Background()
	edited := &domain.Rule{Name: "violations", RuleType: domain.RuleTypeMTProtoAllowlistViolation, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 2, WindowSec: 300}}
	deleted := &domain.Rule{Name: "egress", RuleType: domain.RuleTypeMTProtoEgressDown, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 900, Threshold: domain.Threshold{Count: 1, WindowSec: 120}}
	for _, r := range []*domain.Rule{edited, deleted} {
		if err := repo.CreateRule(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	eng := NewEngine(events.NewEventBus(), repo)
	eng.startTime = time.Now().Add(-10 * time.Minute)
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3)) // loads the cache, below the threshold

	// within the cache's second the admin raises one threshold and deletes the other rule
	fresh, _ := repo.GetRule(ctx, edited.ID)
	fresh.Threshold.Count = 50
	if err := repo.UpdateRule(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRule(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoViolation, 3))
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoEgressFailed, 3))

	got, err := repo.GetRule(ctx, edited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threshold.Count != 50 {
		t.Errorf("firing put back the old threshold: %+v", got.Threshold)
	}
	if got.LastFiredAt == nil {
		t.Error("last_fired_at was not recorded")
	}
	if _, err := repo.GetRule(ctx, deleted.ID); err == nil {
		t.Error("firing brought back a deleted rule")
	}
}

// Fixing a crash loop by removing every user leaves the proxy stopped; the alert still clears, and says so.
func TestACrashLoopThatEndsStoppedResolvesWithoutClaimingTheProxyRuns(t *testing.T) {
	eng, repo := mtprotoEngine(&domain.Rule{
		ID: 1, Name: "loop", RuleType: domain.RuleTypeNodeCrashLoop, Scope: domain.ScopeGlobal,
		Enabled: true, CooldownSec: 600, Threshold: domain.Threshold{Count: 5, WindowSec: 300},
	})
	ctx := context.Background()
	eng.handleEvent(ctx, mtprotoEvent(events.EventMTProtoTelemtCrashLoop, 8))
	eng.handleEvent(ctx, events.Event{Type: events.EventMTProtoTelemtUp, Payload: events.MTProtoPayload{NodeID: 8, NodeName: "vps", Detail: "no users"}})
	if countStatus(repo, domain.EventStatusResolved) != 1 {
		t.Fatalf("not resolved: %+v", repo.events)
	}
	last := repo.events[len(repo.events)-1]
	if last.Message != "The proxy on vps stopped crash-looping. It is not running: no users" {
		t.Fatalf("message = %q", last.Message)
	}
}
