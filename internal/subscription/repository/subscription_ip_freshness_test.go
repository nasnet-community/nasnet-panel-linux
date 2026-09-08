package repository

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

func TestSubscriptionIPLastSeenUsesMonotonicObservationTime(t *testing.T) {
	db, _ := setupTrafficSubDB(t, 0)
	if err := db.AutoMigrate(&domain.SubscriptionIP{}); err != nil {
		t.Fatal(err)
	}
	repo := NewSubscriptionIPRepository(db)
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	record := SubscriptionIPRecord{SubscriptionID: 1, IP: "1.2.3.4", NodeID: 1, SeenAt: at}
	if err := repo.BulkUpsertSubscriptionIPs(ctx, []SubscriptionIPRecord{record}); err != nil {
		t.Fatal(err)
	}
	var first domain.SubscriptionIP
	if err := db.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	// Duplicate records in the same batch are collapsed, including on PostgreSQL.
	older := record
	older.SeenAt = at.Add(-time.Minute)
	if err := repo.BulkUpsertSubscriptionIPs(ctx, []SubscriptionIPRecord{record, older, record}); err != nil {
		t.Fatal(err)
	}
	var repeated domain.SubscriptionIP
	if err := db.First(&repeated).Error; err != nil {
		t.Fatal(err)
	}
	if !repeated.LastSeen.Equal(at) || !repeated.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("repeated/older snapshot refreshed record: last_seen=%v updated_at=%v", repeated.LastSeen, repeated.UpdatedAt)
	}
	newer := record
	newer.SeenAt = at.Add(30 * time.Second)
	otherNode := older
	otherNode.NodeID = 2
	if err := repo.BulkUpsertSubscriptionIPs(ctx, []SubscriptionIPRecord{newer, otherNode}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.GetSubscriptionIPs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].NodeID != 1 || !rows[0].LastSeen.Equal(newer.SeenAt) || !rows[1].LastSeen.Equal(otherNode.SeenAt) {
		t.Fatalf("node-scoped observations lost: %+v", rows)
	}
}

func TestSubscriptionIPLegacyAndFutureObservationTimes(t *testing.T) {
	db, _ := setupTrafficSubDB(t, 0)
	if err := db.AutoMigrate(&domain.SubscriptionIP{}); err != nil {
		t.Fatal(err)
	}
	repo := NewSubscriptionIPRepository(db)
	before := time.Now()
	if err := repo.BulkUpsertSubscriptionIPs(context.Background(), []SubscriptionIPRecord{
		{SubscriptionID: 1, IP: "legacy", NodeID: 1},
		{SubscriptionID: 1, IP: "future", NodeID: 1, SeenAt: before.Add(time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.GetSubscriptionIPs(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.LastSeen.Before(before) || row.LastSeen.After(time.Now()) {
			t.Fatalf("unclamped/legacy observation: %+v", row)
		}
	}
}
