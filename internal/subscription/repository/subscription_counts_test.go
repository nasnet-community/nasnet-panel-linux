package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"gorm.io/gorm"
)

func TestCountExpiringExceedsListLimitAndUsesEffectiveExpiry(t *testing.T) {
	db, repo := setupTrafficSubDB(t, 0)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(7 * 24 * time.Hour)
	soon, overdue, later := now.Add(time.Hour), now.Add(-time.Hour), cutoff.Add(time.Second)

	// The sidebar must count every match even beyond its old 1000-row list cap.
	var rows []domain.Subscription
	for i := 0; i < 1005; i++ {
		row := domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &soon}
		rows = append(rows, row)
	}
	rows = append(rows,
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &cutoff}, // inclusive upper bound
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &now},    // exclusive lower bound
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &overdue},
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &later},
		domain.Subscription{Status: domain.SubscriptionStatusActive}, // unlimited
		domain.Subscription{Status: domain.SubscriptionStatusPaused, EndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusPending, EndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusExpired, EndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusCancelled, EndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusTrafficExhausted, EndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &soon, DeletedAt: gorm.DeletedAt{Time: now, Valid: true}},
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &later, IsEndDateCustom: true, CustomEndDate: &soon},
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &soon, IsEndDateCustom: true, CustomEndDate: &later},
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &soon, IsEndDateCustom: true},   // custom unlimited
		domain.Subscription{Status: domain.SubscriptionStatusActive, EndDate: &soon, CustomEndDate: &overdue}, // override flag is off
	)
	for i := range rows {
		rows[i].ConfigID = fmt.Sprintf("expiry-%d", i)
		rows[i].LinkKey = fmt.Sprintf("expiry-link-%d", i)
	}
	if err := db.CreateInBatches(&rows, 50).Error; err != nil {
		t.Fatal(err)
	}

	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("count_expiry_queries", func(tx *gorm.DB) {
		queries++
		if !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "COUNT(*)") {
			t.Errorf("expected SQL aggregate, got %s", tx.Statement.SQL.String())
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Pass a non-UTC representation of the same instant as a caller might;
	// the repository compares instants consistently with stored UTC dates.
	zone := time.FixedZone("offset", 3*60*60)
	got, err := repo.CountExpiring(context.Background(), now.In(zone), cutoff.In(zone))
	if err != nil {
		t.Fatal(err)
	}
	if got != 1008 {
		t.Fatalf("count = %d, want 1008 across the full list and effective expiry rules", got)
	}
	if queries != 1 {
		t.Fatalf("query count = %d, want one aggregate with no association loads", queries)
	}
}
