package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// ─── helpers ────────────────────────────────────────────────────────────────────

func ptrUint(v uint) *uint           { return &v }
func ptrTime(t time.Time) *time.Time { return &t }

// newCancelTestUsecase wires up just enough dependencies for Cancel tests.
func newCancelTestUsecase(subRepo *mockSubscriptionRepo, acctMgr *mockAccountMgr) SubscriptionUsecase {
	return NewSubscriptionUsecase(
		subRepo,
		newMockSubUserRepo(), // userRepo — not needed for Cancel
		&mockNodeRepo{},      // nodeRepo — not needed for Cancel
		nil,                  // providerFactory — Cancel doesn't provision
		nil,                  // grpcClient — not needed
		nil,                  // nodeUC — not needed for Cancel
		nil,                  // nodeSyncer — not needed for Cancel
		acctMgr,              // accountManager — to verify disable calls
		&mockAccountReader{}, // accountReader — not needed
		&mockSubTxManager{},  // tm — not needed for Cancel
		events.NewEventBus(), // eventBus — collector flushes here (no-op)
	)
}

// ─── Cancel tests ───────────────────────────────────────────────────────────────

func TestCancel_SetsStatusToCancelled(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	acctMgr := newMockAccountMgr()

	userID := uint(1)
	now := time.Now()
	subRepo.seedSubscription(&domain.Subscription{
		ID:        1,
		UserID:    ptrUint(userID),
		Status:    domain.SubscriptionStatusActive,
		StartDate: ptrTime(now),
		EndDate:   ptrTime(now.AddDate(0, 1, 0)),
	})

	uc := newCancelTestUsecase(subRepo, acctMgr)

	err := uc.Cancel(context.Background(), 1)
	if err != nil {
		t.Fatalf("Cancel returned unexpected error: %v", err)
	}

	// Verify the subscription status was updated to cancelled.
	sub, _ := subRepo.FindByID(context.Background(), 1)
	if sub.Status != domain.SubscriptionStatusCancelled {
		t.Errorf("expected status %q, got %q", domain.SubscriptionStatusCancelled, sub.Status)
	}

	// Verify the status log captured the transition.
	if len(subRepo.statusLog) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(subRepo.statusLog))
	}
	if subRepo.statusLog[0].Status != domain.SubscriptionStatusCancelled {
		t.Errorf("status log: expected %q, got %q", domain.SubscriptionStatusCancelled, subRepo.statusLog[0].Status)
	}
}

func TestCancel_DeactivatesAccounts(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	acctMgr := newMockAccountMgr()

	userID := uint(2)
	now := time.Now()
	subRepo.seedSubscription(&domain.Subscription{
		ID:        5,
		UserID:    ptrUint(userID),
		Status:    domain.SubscriptionStatusActive,
		StartDate: ptrTime(now),
		EndDate:   ptrTime(now.AddDate(0, 1, 0)),
	})

	uc := newCancelTestUsecase(subRepo, acctMgr)

	err := uc.Cancel(context.Background(), 5)
	if err != nil {
		t.Fatalf("Cancel returned unexpected error: %v", err)
	}

	// Verify DisableAccountsBySubscription was called.
	if acctMgr.disableCalls != 1 {
		t.Errorf("expected DisableAccountsBySubscription to be called 1 time, got %d", acctMgr.disableCalls)
	}
}

func TestCancel_NotFound(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	acctMgr := newMockAccountMgr()

	uc := newCancelTestUsecase(subRepo, acctMgr)

	err := uc.Cancel(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for non-existent subscription, got nil")
	}

	// No status updates should have occurred.
	if len(subRepo.statusLog) != 0 {
		t.Errorf("expected 0 status updates, got %d", len(subRepo.statusLog))
	}

	// No account disabling should have occurred.
	if acctMgr.disableCalls != 0 {
		t.Errorf("expected 0 disable calls, got %d", acctMgr.disableCalls)
	}
}
