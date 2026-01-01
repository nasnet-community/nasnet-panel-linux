package repository

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.OnlineUsersSnapshot{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestOnlineUsersSnapshotRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOnlineUsersSnapshotRepository(db)
	ctx := context.Background()

	snap := &domain.OnlineUsersSnapshot{Count: 42, CreatedAt: time.Now()}
	if err := repo.Create(ctx, snap); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if snap.ID == 0 {
		t.Fatalf("expected ID to be set after Create")
	}
}

func TestOnlineUsersSnapshotRepository_ListSince(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOnlineUsersSnapshotRepository(db)
	ctx := context.Background()

	now := time.Now()
	old := &domain.OnlineUsersSnapshot{Count: 1, CreatedAt: now.Add(-20 * time.Minute)}
	mid := &domain.OnlineUsersSnapshot{Count: 2, CreatedAt: now.Add(-10 * time.Minute)}
	fresh := &domain.OnlineUsersSnapshot{Count: 3, CreatedAt: now.Add(-1 * time.Minute)}

	for _, s := range []*domain.OnlineUsersSnapshot{old, mid, fresh} {
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	since := now.Add(-15 * time.Minute)
	rows, err := repo.ListSince(ctx, since)
	if err != nil {
		t.Fatalf("ListSince failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows since %v, got %d", since, len(rows))
	}
	if rows[0].Count != 2 || rows[1].Count != 3 {
		t.Fatalf("expected ascending order [2, 3], got [%d, %d]", rows[0].Count, rows[1].Count)
	}
}

func TestOnlineUsersSnapshotRepository_CleanupOlderThan(t *testing.T) {
	db := setupTestDB(t)
	repo := NewOnlineUsersSnapshotRepository(db)
	ctx := context.Background()

	now := time.Now()
	veryOld := &domain.OnlineUsersSnapshot{Count: 1, CreatedAt: now.AddDate(0, 0, -10)}
	recent := &domain.OnlineUsersSnapshot{Count: 2, CreatedAt: now.AddDate(0, 0, -1)}

	for _, s := range []*domain.OnlineUsersSnapshot{veryOld, recent} {
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	deleted, err := repo.CleanupOlderThan(ctx, 7)
	if err != nil {
		t.Fatalf("CleanupOlderThan failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 row deleted, got %d", deleted)
	}

	rows, err := repo.ListSince(ctx, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("ListSince after cleanup failed: %v", err)
	}
	if len(rows) != 1 || rows[0].Count != 2 {
		t.Fatalf("expected 1 recent row with count=2, got %+v", rows)
	}
}
