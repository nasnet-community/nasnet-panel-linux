package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTrafficAccountDB(t *testing.T, count int) (*gorm.DB, AccountRepository) {
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
	if err := db.AutoMigrate(&domain.Account{}); err != nil {
		t.Fatal(err)
	}
	accounts := make([]domain.Account, count)
	for i := range accounts {
		accounts[i] = domain.Account{ID: uint(i + 1), InboundID: 1, Email: fmt.Sprintf("user-%d", i), UUID: fmt.Sprintf("uuid-%d", i), Source: domain.AccountSourceSubscription}
	}
	if count > 0 {
		if err := db.CreateInBatches(&accounts, 50).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, NewAccountRepository(db)
}

func TestTrafficBatchAccountCountersAndActivity(t *testing.T) {
	db, repo := setupTrafficAccountDB(t, 3)
	now := time.Now().UTC().Truncate(time.Second)
	later := now.Add(time.Hour)
	if err := db.Model(&domain.Account{}).Where("id = ?", 1).Updates(map[string]interface{}{"data_used": 100, "last_activity_at": later}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&domain.Account{}, 3).Error; err != nil {
		t.Fatal(err)
	}
	big := int64(1) << 34
	if err := repo.AddUsageDeltas(context.Background(), []UsageDelta{
		{AccountID: 1, Bytes: 3, LastActive: now},
		{AccountID: 2, Bytes: big, LastActive: now.Add(-time.Minute)},
		{AccountID: 1, Bytes: 7, LastActive: now.Add(-time.Minute)},
		{AccountID: 2, Bytes: 11, LastActive: now},
		{AccountID: 3, Bytes: 19, LastActive: now},
	}); err != nil {
		t.Fatal(err)
	}
	var accounts []domain.Account
	if err := db.Unscoped().Order("id").Find(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if accounts[0].DataUsed != 110 || accounts[0].LastActivityAt == nil || !accounts[0].LastActivityAt.Equal(later) {
		t.Fatalf("duplicate/older sample mismatch: %+v", accounts[0])
	}
	if accounts[1].DataUsed != big+11 || accounts[1].LastActivityAt == nil || !accounts[1].LastActivityAt.Equal(now) {
		t.Fatalf("large counter/newer activity mismatch: %+v", accounts[1])
	}
	if accounts[2].DataUsed != 0 || accounts[2].LastActivityAt != nil {
		t.Fatal("soft-deleted account was updated")
	}
}

func TestTrafficBatchThousandAccountsUseTwentyUpdates(t *testing.T) {
	db, repo := setupTrafficAccountDB(t, 1000)
	updates := 0
	if err := db.Callback().Update().After("gorm:update").Register("test:traffic_count", func(tx *gorm.DB) {
		updates++
		if len(tx.Statement.Vars) > 999 {
			t.Errorf("too many binds: %d", len(tx.Statement.Vars))
		}
	}); err != nil {
		t.Fatal(err)
	}
	deltas := make([]UsageDelta, 1000)
	for i := range deltas {
		deltas[i] = UsageDelta{AccountID: uint(i + 1), Bytes: int64(i + 1), LastActive: time.Now()}
	}
	if err := repo.AddUsageDeltas(context.Background(), deltas); err != nil {
		t.Fatal(err)
	}
	if updates != 20 {
		t.Fatalf("1000 accounts used %d UPDATEs, want 20", updates)
	}
	var total int64
	if err := db.Model(&domain.Account{}).Select("SUM(data_used)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 500500 {
		t.Fatalf("unexpected total %d", total)
	}
}

func TestTrafficBatchAccountRollbackAcrossChunks(t *testing.T) {
	db, repo := setupTrafficAccountDB(t, 101)
	injected := errors.New("second account batch failed")
	updates := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:traffic_fail", func(tx *gorm.DB) {
		updates++
		if updates == 2 {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	deltas := make([]UsageDelta, 101)
	for i := range deltas {
		deltas[i] = UsageDelta{AccountID: uint(i + 1), Bytes: 5, LastActive: time.Now()}
	}
	err := database.NewTransactionManager(db).Do(context.Background(), func(ctx context.Context) error {
		return repo.AddUsageDeltas(ctx, deltas)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("expected failure, got %v", err)
	}
	var total, active int64
	if err := db.Model(&domain.Account{}).Select("SUM(data_used)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.Account{}).Where("last_activity_at IS NOT NULL").Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if total != 0 || active != 0 {
		t.Fatalf("partial write survived rollback: total=%d active=%d", total, active)
	}
}

func TestTrafficAccountRefsIncludeWGAttribution(t *testing.T) {
	db, repo := setupTrafficAccountDB(t, 4)
	inbounds := []nodeDomain.Inbound{
		{ID: 1, NodeID: 1, Tag: "wg", Protocol: "wireguard", Port: 51820},
		{ID: 2, NodeID: 2, Tag: "other", Protocol: "wireguard", Port: 51820},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, row := range []struct {
		id        uint
		inboundID uint
		created   time.Time
		source    domain.AccountSource
	}{
		{1, 1, now.Add(-time.Hour), domain.AccountSourceManual},
		{2, 1, now, domain.AccountSourceAdminExcluded},
		{3, 1, now, domain.AccountSourceManual},
		{4, 2, now.Add(time.Hour), domain.AccountSourceManual},
	} {
		if err := db.Model(&domain.Account{}).Where("id = ?", row.id).Updates(map[string]interface{}{
			"subscription_id": 7, "inbound_id": row.inboundID, "created_at": row.created, "source": row.source,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&domain.Account{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	refs, err := repo.ListTrafficRefsByNode(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[uint]AccountTrafficRef, len(refs))
	for _, ref := range refs {
		byID[ref.ID] = ref
	}
	if len(refs) != 2 || byID[3].SubscriptionID != 7 || byID[3].InboundID != 1 ||
		byID[3].Source != string(domain.AccountSourceManual) || byID[2].Source != string(domain.AccountSourceAdminExcluded) ||
		!byID[3].CreatedAt.Equal(now) {
		t.Fatalf("unexpected account attribution projection: %+v", refs)
	}
}
