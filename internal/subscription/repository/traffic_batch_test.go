package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTrafficSubDB(t *testing.T, count int) (*gorm.DB, SubscriptionRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&domain.Subscription{}, &domain.SubscriptionDailyUsage{}); err != nil {
		t.Fatal(err)
	}
	subs := make([]domain.Subscription, count)
	for i := range subs {
		subs[i] = domain.Subscription{ID: uint(i + 1), ConfigID: fmt.Sprintf("cfg-%d", i), LinkKey: fmt.Sprintf("link-%d", i)}
	}
	if count > 0 {
		if err := db.CreateInBatches(&subs, 50).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, NewSubscriptionRepository(db)
}

func TestTrafficBatchSubscriptionCounters(t *testing.T) {
	db, repo := setupTrafficSubDB(t, 3)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	later := now.Add(time.Hour)
	if err := db.Model(&domain.Subscription{}).Where("id = ?", 1).Updates(map[string]interface{}{
		"data_used": 10, "lifetime_data_used": 100, "last_active_at": later,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&domain.Subscription{}, 3).Error; err != nil {
		t.Fatal(err)
	}
	big := int64(1) << 34
	if err := repo.AddUsageDeltas(ctx, []UsageDelta{
		{SubscriptionID: 2, Upload: big, Download: 20, LastActive: now},
		{SubscriptionID: 1, Upload: 3, Download: 5, LastActive: now.Add(-time.Minute)},
		{SubscriptionID: 1, Upload: 7, Download: 11, LastActive: now},
		{SubscriptionID: 3, Upload: 100, Download: 200, LastActive: now},
	}); err != nil {
		t.Fatal(err)
	}
	var subs []domain.Subscription
	if err := db.Unscoped().Order("id").Find(&subs).Error; err != nil {
		t.Fatal(err)
	}
	first := subs[0]
	if first.DataUsed != 36 || first.LifetimeDataUsed != 126 || first.DataUpload != 10 || first.DataDownload != 16 ||
		first.LifetimeDataUpload != 10 || first.LifetimeDataDownload != 16 {
		t.Fatalf("duplicate deltas did not accumulate correctly: %+v", first)
	}
	if first.LastActiveAt == nil || !first.LastActiveAt.Equal(later) {
		t.Fatalf("older sample moved last_active_at backwards: %v", first.LastActiveAt)
	}
	if second := subs[1]; second.DataUsed != big+20 || second.DataUpload != big || second.LastActiveAt == nil || !second.LastActiveAt.Equal(now) {
		t.Fatalf("large/new counter mismatch: %+v", second)
	}
	if subs[2].DataUsed != 0 || subs[2].LastActiveAt != nil {
		t.Fatal("soft-deleted subscription was updated")
	}
	if err := repo.AddUsageDeltas(ctx, []UsageDelta{{SubscriptionID: 2, Download: 1}}); err != nil {
		t.Fatal(err)
	}
	var second domain.Subscription
	if err := db.First(&second, 2).Error; err != nil {
		t.Fatal(err)
	}
	if second.LastActiveAt == nil || !second.LastActiveAt.Equal(now) {
		t.Fatal("zero activity timestamp overwrote existing activity")
	}
}

func TestTrafficBatchDailyUsageMergesUTCDatesAndLegacySplits(t *testing.T) {
	db, repo := setupTrafficSubDB(t, 2)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	legacy := domain.SubscriptionDailyUsage{SubscriptionID: 1, Date: day, DataUsed: 100}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.AddDailyUsageSplits(context.Background(), []DailyUsageDelta{
		{SubscriptionID: 1, Date: day.Add(time.Hour), Upload: 2, Download: 3},
		{SubscriptionID: 1, Date: day.In(time.FixedZone("west", -7*3600)), Upload: 5, Download: 7},
		{SubscriptionID: 2, Date: day, Upload: 11, Download: 13},
		{SubscriptionID: 1, Date: day.Add(24 * time.Hour), Download: 17},
	}); err != nil {
		t.Fatal(err)
	}
	var rows []dailyUsageWrite
	if err := db.Table("subscription_daily_usage").Order("subscription_id, date").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected three distinct daily rows, got %d", len(rows))
	}
	want := [][3]int64{{117, 7, 10}, {17, 0, 17}, {24, 11, 13}}
	for i, row := range rows {
		if row.DataUsed != want[i][0] || row.DataUpload != want[i][1] || row.DataDownload != want[i][2] {
			t.Fatalf("row %d: %+v, want %v", i, row, want[i])
		}
	}
}

func TestTrafficBatchThousandSubscriptionsUseBoundedStatements(t *testing.T) {
	db, repo := setupTrafficSubDB(t, 1000)
	updates, inserts := 0, 0
	if err := db.Callback().Update().After("gorm:update").Register("test:traffic_update_count", func(tx *gorm.DB) {
		updates++
		if len(tx.Statement.Vars) > 999 {
			t.Errorf("UPDATE used %d bound variables", len(tx.Statement.Vars))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register("test:traffic_insert_count", func(tx *gorm.DB) {
		if tx.Statement.Table == "subscription_daily_usage" {
			inserts++
			if len(tx.Statement.Vars) > 999 {
				t.Errorf("INSERT used %d bound variables", len(tx.Statement.Vars))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	deltas := make([]UsageDelta, 1000)
	daily := make([]DailyUsageDelta, 1000)
	for i := range deltas {
		deltas[i] = UsageDelta{SubscriptionID: uint(i + 1), Upload: int64(i + 1), Download: 2, LastActive: now}
		daily[i] = DailyUsageDelta{SubscriptionID: uint(i + 1), Date: now, Upload: int64(i + 1), Download: 2}
	}
	if err := database.NewTransactionManager(db).Do(context.Background(), func(ctx context.Context) error {
		if err := repo.AddUsageDeltas(ctx, deltas); err != nil {
			return err
		}
		return repo.AddDailyUsageSplits(ctx, daily)
	}); err != nil {
		t.Fatal(err)
	}
	if updates != 20 || inserts != 20 {
		t.Fatalf("1000 subscriptions used %d UPDATEs and %d INSERTs; want 20 each", updates, inserts)
	}
	var total, dailyTotal int64
	if err := db.Model(&domain.Subscription{}).Select("SUM(data_used)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SubscriptionDailyUsage{}).Select("SUM(data_used)").Scan(&dailyTotal).Error; err != nil {
		t.Fatal(err)
	}
	if total != 502500 || dailyTotal != total {
		t.Fatalf("unexpected totals: subscriptions=%d daily=%d", total, dailyTotal)
	}
}

func TestTrafficBatchRollbackAfterLaterDailyBatchFails(t *testing.T) {
	db, repo := setupTrafficSubDB(t, 101)
	injected := errors.New("second daily batch failed")
	inserts := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:traffic_fail_daily_batch", func(tx *gorm.DB) {
		if tx.Statement.Table == "subscription_daily_usage" {
			inserts++
			if inserts == 2 {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	deltas := make([]UsageDelta, 101)
	daily := make([]DailyUsageDelta, 101)
	for i := range deltas {
		deltas[i] = UsageDelta{SubscriptionID: uint(i + 1), Upload: 3, Download: 5, LastActive: time.Now()}
		daily[i] = DailyUsageDelta{SubscriptionID: uint(i + 1), Date: time.Now(), Upload: 3, Download: 5}
	}
	err := database.NewTransactionManager(db).Do(context.Background(), func(ctx context.Context) error {
		if err := repo.AddUsageDeltas(ctx, deltas); err != nil {
			return err
		}
		return repo.AddDailyUsageSplits(ctx, daily)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected failure, got %v", err)
	}
	var total, dailyRows, activeRows int64
	if err := db.Model(&domain.Subscription{}).Select("SUM(data_used)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SubscriptionDailyUsage{}).Count(&dailyRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.Subscription{}).Where("last_active_at IS NOT NULL").Count(&activeRows).Error; err != nil {
		t.Fatal(err)
	}
	if total != 0 || dailyRows != 0 || activeRows != 0 {
		t.Fatalf("transaction left partial writes: total=%d dailyRows=%d activeRows=%d", total, dailyRows, activeRows)
	}
}
