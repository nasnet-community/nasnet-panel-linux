package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPeerTrafficDB(t *testing.T, count int) (*gorm.DB, WGPeerRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&domain.WGPeer{}); err != nil {
		t.Fatalf("migrate peers: %v", err)
	}
	peers := make([]domain.WGPeer, 0, count)
	for i := 1; i <= count; i++ {
		peers = append(peers, domain.WGPeer{
			ID: uint(i), InboundID: 1, SubscriptionID: 1,
			PublicKey: fmt.Sprintf("key-%d", i), AssignedIP: fmt.Sprintf("ip-%d", i),
		})
	}
	if len(peers) > 0 {
		if err := db.CreateInBatches(&peers, 50).Error; err != nil {
			t.Fatalf("seed peers: %v", err)
		}
	}
	return db, NewWGPeerRepository(db)
}

func TestAddUsageBatch_ThousandPeersUseTwentyBoundedWrites(t *testing.T) {
	db, repo := setupPeerTrafficDB(t, 1000)
	writes := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:count_peer_updates", func(tx *gorm.DB) {
		writes++
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("test:check_peer_update_size", func(tx *gorm.DB) {
		if len(tx.Statement.Vars) > 401 {
			t.Errorf("peer UPDATE has %d parameters, want at most 401", len(tx.Statement.Vars))
		}
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	deltas := make([]contract.WGUsageDelta, 0, 1000)
	for i := 1000; i > 0; i-- {
		deltas = append(deltas, contract.WGUsageDelta{PeerID: uint(i), Upload: int64(i), Download: int64(i * 2), LastSeen: now})
	}
	if err := database.NewTransactionManager(db).Do(context.Background(), func(ctx context.Context) error {
		return repo.AddUsageBatch(ctx, deltas)
	}); err != nil {
		t.Fatalf("batch usage: %v", err)
	}
	if writes != 20 {
		t.Fatalf("UPDATE statements = %d, want 20 for 1000 peers", writes)
	}
	var peers []domain.WGPeer
	if err := db.Order("id").Find(&peers).Error; err != nil {
		t.Fatal(err)
	}
	for _, peer := range peers {
		if peer.UpBytes != int64(peer.ID) || peer.DownBytes != int64(peer.ID*2) || peer.LastSeen == nil || !peer.LastSeen.Equal(now) {
			t.Fatalf("peer %d: up=%d down=%d last_seen=%v", peer.ID, peer.UpBytes, peer.DownBytes, peer.LastSeen)
		}
	}
}

func TestAddUsageBatch_MergesDuplicatesAndKeepsNewestLastSeen(t *testing.T) {
	db, repo := setupPeerTrafficDB(t, 3)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	if err := db.Model(&domain.WGPeer{}).Where("id IN ?", []uint{1, 2}).Updates(map[string]any{
		"up_bytes": int64(1 << 33), "down_bytes": 20, "last_seen": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.AddUsageBatch(ctx, []contract.WGUsageDelta{
		{PeerID: 1, Upload: 5, Download: 7, LastSeen: old},
		{PeerID: 2, Upload: 1, Download: 2},
		{PeerID: 1, Upload: 9, Download: 11, LastSeen: old.Add(-time.Hour)},
		{PeerID: 3, LastSeen: old},
		{PeerID: 3, Upload: 1 << 34, LastSeen: now},
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		id       uint
		up, down int64
	}{
		{1, (1 << 33) + 14, 38},
		{2, (1 << 33) + 1, 22},
		{3, 1 << 34, 0},
	} {
		peer, err := repo.FindByID(ctx, want.id)
		if err != nil {
			t.Fatal(err)
		}
		if peer.UpBytes != want.up || peer.DownBytes != want.down || peer.LastSeen == nil || !peer.LastSeen.Equal(now) {
			t.Fatalf("peer %d: up=%d down=%d last_seen=%v", peer.ID, peer.UpBytes, peer.DownBytes, peer.LastSeen)
		}
	}
	newer := now.Add(time.Hour)
	if err := repo.AddUsageBatch(ctx, []contract.WGUsageDelta{{PeerID: 1, Upload: 10, LastSeen: newer}}); err != nil {
		t.Fatal(err)
	}
	peer, err := repo.FindByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if peer.UpBytes != (1<<33)+24 || !peer.LastSeen.Equal(newer) {
		t.Fatalf("increment did not retain cumulative usage/latest time: %+v", peer)
	}
}

func TestAddUsageBatch_RollsBackEarlierChunksAndCanRetry(t *testing.T) {
	db, repo := setupPeerTrafficDB(t, 101)
	if err := db.Exec(`CREATE TRIGGER reject_peer_usage BEFORE UPDATE ON wg_peers
		WHEN OLD.id = 51 BEGIN SELECT RAISE(ABORT, 'injected batch failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	deltas := make([]contract.WGUsageDelta, 0, 101)
	for i := 101; i > 0; i-- {
		deltas = append(deltas, contract.WGUsageDelta{PeerID: uint(i), Upload: 3, Download: 5, LastSeen: now})
	}
	apply := func(ctx context.Context) error { return repo.AddUsageBatch(ctx, deltas) }
	tm := database.NewTransactionManager(db)
	if err := tm.Do(context.Background(), apply); err == nil {
		t.Fatal("expected injected second-chunk failure")
	}
	var changed int64
	if err := db.Model(&domain.WGPeer{}).Where("up_bytes != 0 OR down_bytes != 0 OR last_seen IS NOT NULL").Count(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("rollback left %d peers changed", changed)
	}
	if err := db.Exec("DROP TRIGGER reject_peer_usage").Error; err != nil {
		t.Fatal(err)
	}
	if err := tm.Do(context.Background(), apply); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := db.Model(&domain.WGPeer{}).Where("up_bytes = 3 AND down_bytes = 5 AND last_seen IS NOT NULL").Count(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed != 101 {
		t.Fatalf("retry persisted exactly once for %d peers, want 101", changed)
	}
}

func TestAddUsageBatch_EmptyInputDoesNotWrite(t *testing.T) {
	db, repo := setupPeerTrafficDB(t, 1)
	writes := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:count_empty_peer_updates", func(tx *gorm.DB) {
		writes++
	}); err != nil {
		t.Fatal(err)
	}
	for _, deltas := range [][]contract.WGUsageDelta{nil, {}, {{PeerID: 1}}} {
		if err := repo.AddUsageBatch(context.Background(), deltas); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 0 {
		t.Fatalf("empty/no-op deltas performed %d updates", writes)
	}
}
