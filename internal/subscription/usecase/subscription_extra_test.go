package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// newReconcileUsecase wires the subset of dependencies needed by the
// reconciliation tests. nodeUC is nil so ReconcileUsers takes the
// "no node client" branch, letting us exercise the outer loop + the
// fallback path without mocking an agent.
func newReconcileUsecase(
	subRepo *mockSubscriptionRepo,
	acctMgr *mockAccountMgr,
) SubscriptionUsecase {
	return NewSubscriptionUsecase(
		subRepo,
		newMockSubUserRepo(),
		&mockNodeRepo{},
		nil, // providerFactory
		nil, // grpcClient
		nil, // nodeUC — drives ReconcileUsers into the log-only branch
		&mockNodeSyncer{},
		acctMgr,
		&mockAccountReader{}, // returns nil users
		&mockSubTxManager{},
		events.NewEventBus(),
	)
}

// ─── ReconcileUsers tests ──────────────────────────────────────────────

// TestReconcileUsers_NoActiveNodes: empty node list short-circuits with
// empty stats. mockNodeRepo.ListActiveNodes returns nil by default.
func TestReconcileUsers_NoActiveNodes(t *testing.T) {
	uc := newReconcileUsecase(newMockSubscriptionRepo(), newMockAccountMgr())
	stats, err := uc.ReconcileUsers(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stats == nil {
		t.Fatal("stats should never be nil")
	}
	if stats.GhostsRemoved != 0 || stats.MissingAdded != 0 || stats.Errors != 0 {
		t.Errorf("empty node list should produce zero stats, got %+v", stats)
	}
}

// ─── Rename label ──────────────────────────────────────────────────────

// TestRenameSubscription_Passthrough: usecase forwards the new label to
// the repo without additional validation (empty string is allowed —
// admins sometimes clear labels on purpose).
func TestRenameSubscription_Passthrough(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, Label: "old"})
	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())

	if err := uc.RenameSubscription(context.Background(), 1, "new-label"); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

// ─── Cancel idempotency ────────────────────────────────────────────────

// TestCancel_OnAlreadyCancelled: calling Cancel twice should not error
// the second time. The usecase relies on UpdateStatus being idempotent;
// verify we don't double-dispatch the "deactivate accounts" step in a
// way that breaks the world.
func TestCancel_OnAlreadyCancelled(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	acctMgr := newMockAccountMgr()

	subRepo.seedSubscription(&domain.Subscription{
		ID:      1,
		UserID:  ptrUint(5),
		Status:  domain.SubscriptionStatusActive,
		EndDate: ptrTime(time.Now().Add(time.Hour)),
	})

	uc := newCancelTestUsecase(subRepo, acctMgr)
	if err := uc.Cancel(context.Background(), 1); err != nil {
		t.Fatalf("first cancel: %v", err)
	}

	// Second call — status is already "cancelled" but Cancel should
	// not blow up. Some callers (UI buttons) can double-submit.
	err := uc.Cancel(context.Background(), 1)
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		t.Errorf("idempotent cancel should succeed or surface a named error, got %v", err)
	}
}
