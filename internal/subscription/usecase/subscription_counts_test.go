package usecase

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"testing"
	"time"
)

func (m *mockSubscriptionRepo) CountExpiring(_ context.Context, _, _ time.Time) (int64, error) {
	return 0, nil
}

type countExpiryRepo struct {
	repository.SubscriptionRepository
	after, through time.Time
}

func (r *countExpiryRepo) CountExpiring(_ context.Context, after, through time.Time) (int64, error) {
	r.after, r.through = after, through
	return 1005, nil
}

func TestCountExpiringSubscriptionsBounds(t *testing.T) {
	repo := &countExpiryRepo{}
	u := &subscriptionUsecase{subRepo: repo}
	for _, days := range []int{-1, 0, 366} {
		if _, err := u.CountExpiringSubscriptions(context.Background(), days); err == nil {
			t.Fatalf("accepted days %d", days)
		}
		if !repo.after.IsZero() {
			t.Fatal("invalid days reached repository")
		}
	}
	before := time.Now().UTC()
	count, err := u.CountExpiringSubscriptions(context.Background(), 7)
	if err != nil || count != 1005 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if repo.after.Before(before) || repo.after.After(time.Now().UTC()) || repo.through.Sub(repo.after) != 7*24*time.Hour {
		t.Fatalf("wrong expiry interval: %s to %s", repo.after, repo.through)
	}
}
