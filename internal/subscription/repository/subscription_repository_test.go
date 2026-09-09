package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func subscriptionTestDB(t *testing.T) (*gorm.DB, SubscriptionRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&domain.Subscription{}, &domain.SubscriptionDailyUsage{}); err != nil {
		t.Fatal(err)
	}
	return db, NewSubscriptionRepository(db)
}

func TestSubscriptionLinkKeyCreationAndRotation(t *testing.T) {
	db, repo := subscriptionTestDB(t)
	ctx := context.Background()
	sub := &domain.Subscription{ConfigID: "protocol-credential"}
	if err := repo.Create(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if sub.LinkKey == "" || sub.LinkKey == sub.ConfigID {
		t.Fatalf("expected an independent public key, got %q", sub.LinkKey)
	}
	if got, err := repo.FindByLinkKey(ctx, sub.LinkKey); err != nil || got.ID != sub.ID {
		t.Fatalf("public key does not resolve: %v, %v", got, err)
	}
	if _, err := repo.FindByLinkKey(ctx, sub.ConfigID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("protocol credential resolved as a public key: %v", err)
	}
	oldKey := sub.LinkKey
	sub.LinkKey = "rotated-public-key"
	if err := repo.Update(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByLinkKey(ctx, oldKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("old public key still resolves: %v", err)
	}
	if _, err := repo.FindByLinkKey(ctx, sub.LinkKey); err != nil {
		t.Fatalf("rotated key does not resolve: %v", err)
	}
	if err := db.Model(sub).Update("link_key", "").Error; err == nil {
		t.Fatal("database accepted an empty public key")
	}
	imported := &domain.Subscription{ConfigID: "imported-credential", LinkKey: "imported-public-key"}
	if err := repo.Create(ctx, imported); err != nil || imported.LinkKey != "imported-public-key" {
		t.Fatalf("explicit imported key was not preserved: %q, %v", imported.LinkKey, err)
	}
}

func TestDailyUsageAccumulatesBothDirections(t *testing.T) {
	db, repo := subscriptionTestDB(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	for _, delta := range [][2]int64{{100, 300}, {50, 0}, {0, 20}} {
		if err := repo.AddDailyUsageSplit(ctx, 1, day, delta[0], delta[1]); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := repo.ListDailyUsageRange(ctx, 1, day, day)
	if err != nil || len(rows) != 1 {
		t.Fatalf("daily rows = %v, %v", rows, err)
	}
	row := rows[0]
	if row.DataUpload != 150 || row.DataDownload != 320 || row.DataUsed != 470 {
		t.Fatalf("incorrect accumulated usage: %+v", row)
	}
	if err := db.Model(row).Update("data_upload", nil).Error; err == nil {
		t.Fatal("database accepted an unknown upload total")
	}
}
