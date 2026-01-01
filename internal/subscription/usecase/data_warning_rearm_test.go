package usecase

import (
	"context"
	"testing"

	notifDomain "github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
)

type cleanerCall struct {
	subID uint
	types []notifDomain.NotificationType
}

type fakeNotifCleaner struct {
	calls []cleanerCall
	err   error
}

func (f *fakeNotifCleaner) DeleteBySubscriptionAndTypes(_ context.Context, subID uint, types ...notifDomain.NotificationType) error {
	f.calls = append(f.calls, cleanerCall{subID, types})
	return f.err
}

// resetDataWarnings must clear the one-shot data-exhausted notification log so a
// renewed/reset sub re-arms it (the warning-level counter already re-arms; the
// log previously did not, suppressing the next cycle's exhaustion alert).
func TestResetDataWarnings_ClearsExhaustedNotification(t *testing.T) {
	cleaner := &fakeNotifCleaner{}
	uc := &subscriptionUsecase{subRepo: &mockSubscriptionRepo{}, notifCleaner: cleaner}

	if err := uc.resetDataWarnings(context.Background(), 42); err != nil {
		t.Fatalf("resetDataWarnings returned error: %v", err)
	}
	if len(cleaner.calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(cleaner.calls))
	}
	c := cleaner.calls[0]
	if c.subID != 42 || len(c.types) != 1 || c.types[0] != notifDomain.NotificationTypeDataExhausted {
		t.Errorf("cleaner called with (%d, %v), want (42, [%s])",
			c.subID, c.types, notifDomain.NotificationTypeDataExhausted)
	}
}

// rearmExpiryNotifications must clear all four one-shot expiry reminder logs so
// a renewed sub re-fires them near the new end date.
func TestRearmExpiryNotifications_ClearsExpiryLogs(t *testing.T) {
	cleaner := &fakeNotifCleaner{}
	uc := &subscriptionUsecase{subRepo: &mockSubscriptionRepo{}, notifCleaner: cleaner}

	uc.rearmExpiryNotifications(context.Background(), 9)

	if len(cleaner.calls) != 1 {
		t.Fatalf("expected 1 cleaner call, got %d", len(cleaner.calls))
	}
	c := cleaner.calls[0]
	if c.subID != 9 {
		t.Errorf("subID = %d, want 9", c.subID)
	}
	want := map[notifDomain.NotificationType]bool{
		notifDomain.NotificationTypeExpiry7Days: true,
		notifDomain.NotificationTypeExpiry3Days: true,
		notifDomain.NotificationTypeExpiry1Day:  true,
		notifDomain.NotificationTypeExpired:     true,
	}
	if len(c.types) != len(want) {
		t.Fatalf("cleared %d types, want %d: %v", len(c.types), len(want), c.types)
	}
	for _, typ := range c.types {
		if !want[typ] {
			t.Errorf("unexpected cleared type %s", typ)
		}
	}
}

// nil cleaner must be a no-op, not a panic.
func TestRearmExpiryNotifications_NilCleanerNoPanic(t *testing.T) {
	uc := &subscriptionUsecase{subRepo: &mockSubscriptionRepo{}}
	uc.rearmExpiryNotifications(context.Background(), 3)
}

// A nil cleaner (unwired contexts / tests) must be a no-op, not a panic, and
// still reset the warning level.
func TestResetDataWarnings_NilCleanerNoPanic(t *testing.T) {
	uc := &subscriptionUsecase{subRepo: &mockSubscriptionRepo{}}
	if err := uc.resetDataWarnings(context.Background(), 7); err != nil {
		t.Fatalf("resetDataWarnings with nil cleaner: %v", err)
	}
}
