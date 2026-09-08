package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SQLite execution cannot detect PostgreSQL inferring CASE bind parameters as
// int4. Keep the generated PostgreSQL statements explicitly bigint-typed for
// ordinary multi-gigabyte traffic, without requiring a developer's DB server.
func TestTrafficBatchPostgresUsesBigintCounters(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 user=traffic_test dbname=traffic_test sslmode=disable"), &gorm.Config{
		DryRun: true, SkipDefaultTransaction: true, DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	big := int64(1) << 34
	statements := 0
	if err := db.Callback().Update().After("gorm:update").Register("test:postgres_bigint", func(tx *gorm.DB) {
		statements++
		sql := tx.Statement.SQL.String()
		if !strings.Contains(sql, "ELSE CAST(0 AS BIGINT) END") || !strings.Contains(sql, "$1") || strings.Contains(sql, "ELSE 0 END") {
			t.Errorf("counter CASE does not explicitly infer PostgreSQL bigint: %s", sql)
		}
		foundBig := false
		for _, value := range tx.Statement.Vars {
			if n, ok := value.(int64); ok && n >= big {
				foundBig = true
			}
		}
		if !foundBig {
			t.Error("multi-gigabyte delta was not bound as int64")
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if err := NewSubscriptionRepository(db).AddUsageDeltas(ctx, []UsageDelta{{SubscriptionID: 1, Upload: big, Download: 1, LastActive: now}}); err != nil {
		t.Fatal(err)
	}
	if err := accountRepo.NewAccountRepository(db).AddUsageDeltas(ctx, []accountRepo.UsageDelta{{AccountID: 1, Bytes: big, LastActive: now}}); err != nil {
		t.Fatal(err)
	}
	if statements != 2 {
		t.Fatalf("expected two PostgreSQL update statements, got %d", statements)
	}
}

// An optional live check complements the SQLite suite and PostgreSQL SQL
// generation checks. Every table is temporary and confined to one connection.
func TestTrafficBatchPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TRAFFIC_BATCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TRAFFIC_BATCH_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, statement := range []string{
		`SET TIME ZONE 'UTC'`,
		`CREATE TEMP TABLE subscriptions (id bigint PRIMARY KEY, data_used bigint DEFAULT 0, lifetime_data_used bigint DEFAULT 0,
		 data_upload bigint DEFAULT 0, data_download bigint DEFAULT 0, lifetime_data_upload bigint DEFAULT 0, lifetime_data_download bigint DEFAULT 0,
		 last_active_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE TEMP TABLE subscription_daily_usage (id bigserial PRIMARY KEY, subscription_id bigint NOT NULL, date date NOT NULL,
		 data_used bigint DEFAULT 0, data_upload bigint NOT NULL DEFAULT 0, data_download bigint NOT NULL DEFAULT 0, created_at timestamptz, UNIQUE(subscription_id, date))`,
		`CREATE TEMP TABLE accounts (id bigint PRIMARY KEY, data_used bigint DEFAULT 0, last_activity_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`INSERT INTO subscriptions (id) VALUES (1), (2), (3)`,
		`INSERT INTO accounts (id) VALUES (1), (2), (3)`,
		`UPDATE subscriptions SET deleted_at=now() WHERE id=3`,
		`UPDATE accounts SET deleted_at=now() WHERE id=3`,
		`INSERT INTO subscription_daily_usage (subscription_id,date,data_used) VALUES (1,'2026-09-04',100)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	subs := NewSubscriptionRepository(db)
	accounts := accountRepo.NewAccountRepository(db)
	big := int64(1) << 34
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	apply := func(ctx context.Context) error {
		if err := subs.AddUsageDeltas(ctx, []UsageDelta{
			{SubscriptionID: 1, Upload: big, Download: 2, LastActive: now},
			{SubscriptionID: 1, Upload: 3, Download: 5, LastActive: now.Add(-time.Hour)},
			{SubscriptionID: 2, Upload: 7, Download: 11, LastActive: now},
			{SubscriptionID: 3, Upload: big, LastActive: now},
		}); err != nil {
			return err
		}
		if err := subs.AddDailyUsageSplits(ctx, []DailyUsageDelta{
			{SubscriptionID: 1, Date: now, Upload: big, Download: 2},
			{SubscriptionID: 1, Date: now.Add(-time.Hour), Upload: 3, Download: 5},
			{SubscriptionID: 2, Date: now, Upload: 7, Download: 11},
		}); err != nil {
			return err
		}
		if err := accounts.AddUsageDeltas(ctx, []accountRepo.UsageDelta{
			{AccountID: 1, Bytes: big, LastActive: now}, {AccountID: 1, Bytes: 10, LastActive: now.Add(-time.Hour)},
			{AccountID: 2, Bytes: 18, LastActive: now}, {AccountID: 3, Bytes: big, LastActive: now},
		}); err != nil {
			return err
		}
		return nil
	}
	tm := database.NewTransactionManager(db)
	if err := tm.Do(context.Background(), apply); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("rollback postgres batch")
	if err := tm.Do(context.Background(), func(ctx context.Context) error {
		if err := apply(ctx); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatalf("expected injected rollback, got %v", err)
	}
	for _, table := range []string{"subscriptions", "accounts"} {
		var total int64
		if err := db.Table(table).Select("SUM(data_used)").Scan(&total).Error; err != nil {
			t.Fatal(err)
		}
		if total != big+28 {
			t.Errorf("%s total %d, want %d", table, total, big+28)
		}
	}
	var daily struct{ DataUsed, DataUpload, DataDownload int64 }
	if err := db.Table("subscription_daily_usage").Where("subscription_id = 1").First(&daily).Error; err != nil {
		t.Fatal(err)
	}
	if daily.DataUsed != big+110 || daily.DataUpload != big+3 || daily.DataDownload != 7 {
		t.Fatalf("unexpected daily counters: %+v", daily)
	}
	var activity struct{ LastActiveAt time.Time }
	if err := db.Table("subscriptions").Where("id = 1").First(&activity).Error; err != nil {
		t.Fatal(err)
	}
	if !activity.LastActiveAt.Equal(now) {
		t.Fatalf("activity %s, want %s", activity.LastActiveAt, now)
	}
	for _, statement := range []string{
		`INSERT INTO subscriptions (id) SELECT generate_series(4,1000)`,
		`INSERT INTO accounts (id) SELECT generate_series(4,1000)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	writes := make(map[string]int)
	countWrites := func(tx *gorm.DB) { writes[tx.Statement.Table]++ }
	if err := db.Callback().Update().After("gorm:update").Register("test:pg_batch_updates", countWrites); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register("test:pg_batch_inserts", countWrites); err != nil {
		t.Fatal(err)
	}
	subDeltas := make([]UsageDelta, 1000)
	dailyDeltas := make([]DailyUsageDelta, 1000)
	accountDeltas := make([]accountRepo.UsageDelta, 1000)
	for i := range subDeltas {
		id := uint(i + 1)
		subDeltas[i] = UsageDelta{SubscriptionID: id, Upload: 1, Download: 2, LastActive: now}
		dailyDeltas[i] = DailyUsageDelta{SubscriptionID: id, Date: now, Upload: 1, Download: 2}
		accountDeltas[i] = accountRepo.UsageDelta{AccountID: id, Bytes: 3, LastActive: now}
	}
	if err := tm.Do(context.Background(), func(ctx context.Context) error {
		if err := subs.AddUsageDeltas(ctx, subDeltas); err != nil {
			return err
		}
		if err := subs.AddDailyUsageSplits(ctx, dailyDeltas); err != nil {
			return err
		}
		if err := accounts.AddUsageDeltas(ctx, accountDeltas); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"subscriptions", "subscription_daily_usage", "accounts"} {
		if writes[table] != 20 {
			t.Errorf("1000 %s deltas used %d statements, want 20", table, writes[table])
		}
	}
}
