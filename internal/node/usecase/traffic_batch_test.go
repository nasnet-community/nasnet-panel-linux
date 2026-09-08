package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wgRepo "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type trafficTestPeers struct {
	repo  wgRepo.WGPeerRepository
	peers []WGRenderPeer
}

func (s trafficTestPeers) ActivePeersByInbound(context.Context, uint) ([]WGRenderPeer, error) {
	return s.peers, nil
}
func (s trafficTestPeers) AddPeerUsageBatch(ctx context.Context, ds []contract.WGUsageDelta) error {
	return s.repo.AddUsageBatch(ctx, ds)
}

func setupTrafficIntegration(t *testing.T, count int) (*gorm.DB, *nodeUsecase, *domain.Node, *fakeStatsAgentClient) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&domain.Node{}, &domain.NodeDailyTraffic{}, &domain.Outbound{}, &subDomain.Subscription{}, &subDomain.SubscriptionDailyUsage{}, &accountDomain.Account{}, &wgDomain.WGPeer{}); err != nil {
		t.Fatal(err)
	}
	node := &domain.Node{ID: 1, Name: "traffic", UUID: "traffic-node", IsActive: true, IsOnline: true, Inbounds: []domain.Inbound{{ID: 1, NodeID: 1, Tag: "vless-in", Protocol: "vless"}, {ID: 2, NodeID: 1, Tag: "wg-in", Protocol: "wireguard"}}}
	if err := db.Create(node).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Outbound{ID: 1, NodeID: 1, Tag: "direct", Protocol: "freedom"}).Error; err != nil {
		t.Fatal(err)
	}
	subs := make([]subDomain.Subscription, count)
	accounts := make([]accountDomain.Account, count)
	for i := range subs {
		id := uint(i + 1)
		email := fmt.Sprintf("user_%d", id)
		subs[i] = subDomain.Subscription{ID: id, ConfigID: fmt.Sprintf("cfg-%d", id), LinkKey: fmt.Sprintf("link-%d", id), ConfigEmail: email}
		accounts[i] = accountDomain.Account{ID: id, InboundID: 1, Email: email, UUID: fmt.Sprintf("uuid-%d", id), Source: accountDomain.AccountSourceSubscription, SubscriptionID: &id}
	}
	if count > 0 {
		if err := db.CreateInBatches(&subs, 50).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.CreateInBatches(&accounts, 50).Error; err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeStatsAgentClient{}
	u := newSyncSingleNodeTestUsecase(newFakeStatsNodeRepo(node), fake)
	u.nodeRepo = repository.NewNodeRepository(db)
	u.subRepo = subRepo.NewSubscriptionRepository(db)
	u.accountRepo = accountRepo.NewAccountRepository(db)
	u.tm = database.NewTransactionManager(db)
	return db, u, node, fake
}

func TestTrafficBatchThousandUsersUsesSixtyWrites(t *testing.T) {
	db, u, node, _ := setupTrafficIntegration(t, 1000)
	writes := 0
	count := func(tx *gorm.DB) {
		if tx.Statement.Table == "subscriptions" || tx.Statement.Table == "accounts" || tx.Statement.Table == "subscription_daily_usage" {
			writes++
		}
	}
	if err := db.Callback().Update().After("gorm:update").Register("test:count", count); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register("test:count", count); err != nil {
		t.Fatal(err)
	}
	record := &agent.TrafficRecord{Timestamp: time.Now().Unix(), UserUplink: map[string]int64{}, UserDownlink: map[string]int64{}, InboundUplink: map[string]int64{"vless-in": 1000}}
	for i := 1; i <= 1000; i++ {
		email := fmt.Sprintf("user_%d", i)
		record.UserUplink[email] = 30
		record.UserDownlink[email] = 70
	}
	if _, err := u.persistBufferedTraffic(context.Background(), node, []*agent.TrafficRecord{record}); err != nil {
		t.Fatal(err)
	}
	if writes != 60 {
		t.Fatalf("writes=%d, want 60 (previous path 4000)", writes)
	}
	for _, table := range []string{"subscriptions", "accounts", "subscription_daily_usage"} {
		var total int64
		if err := db.Table(table).Select("SUM(data_used)").Scan(&total).Error; err != nil {
			t.Fatal(err)
		}
		if total != 100000 {
			t.Fatalf("%s sum=%d", table, total)
		}
	}
	t.Logf("1000 active users: %d traffic writes, down from 4000", writes)
}

func TestTrafficBatchLateFailureRollsBackAndSuppressesAck(t *testing.T) {
	for _, failedTable := range []string{"accounts", "wg_peers"} {
		t.Run(failedTable, func(t *testing.T) {
			db, u, node, fake := setupTrafficIntegration(t, 2)
			subID := uint(2)
			if err := db.Create(&accountDomain.Account{ID: 3, InboundID: 2, SubscriptionID: &subID, Email: "wg-account", UUID: "wg-uuid", Source: accountDomain.AccountSourceManual}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&wgDomain.WGPeer{ID: 1, SubscriptionID: 2, InboundID: 2, PublicKey: "key", PresharedKey: "psk", AssignedIP: "10.0.0.2"}).Error; err != nil {
				t.Fatal(err)
			}
			u.wgPeerSource = trafficTestPeers{repo: wgRepo.NewWGPeerRepository(db), peers: []WGRenderPeer{{SubscriptionID: 2, PeerID: 1, InboundID: 2, AllowedIP: "10.0.0.2"}}}
			ts := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC).Unix()
			fake.buffered = &agent.BufferedTrafficStats{Records: []*agent.TrafficRecord{
				{Timestamp: ts - 1, UserUplink: map[string]int64{"user_1": 10}, UserDownlink: map[string]int64{"user_1": 20}, InboundUplink: map[string]int64{"vless-in": 10}, TotalUplink: 10, TotalDownlink: 20, OutboundUplink: map[string]int64{"direct": 10}},
				{Timestamp: ts, UserUplink: map[string]int64{"user_1": 30, "user_2": 5, "wg:wg-in:10.0.0.2": 11}, UserDownlink: map[string]int64{"user_1": 40, "user_2": 6, "wg:wg-in:10.0.0.2": 13}, InboundDownlink: map[string]int64{"vless-in": 40}, TotalUplink: 46, TotalDownlink: 59},
			}}
			fail := true
			if err := db.Callback().Update().Before("gorm:update").Register("test:fail", func(tx *gorm.DB) {
				if fail && tx.Statement.Table == failedTable {
					tx.AddError(errors.New("injected write failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			u.syncSingleNode(context.Background(), node, nil)
			if fake.ackCalled {
				t.Fatal("acknowledged failed batch")
			}
			for _, spec := range [][2]string{{"subscriptions", "data_used"}, {"accounts", "data_used"}, {"wg_peers", "up_bytes"}, {"nodes", "total_uplink"}, {"outbounds", "uplink"}} {
				var total int64
				if err := db.Table(spec[0]).Select("COALESCE(SUM(" + spec[1] + "),0)").Scan(&total).Error; err != nil {
					t.Fatal(err)
				}
				if total != 0 {
					t.Fatalf("partial commit %s=%d", spec[0], total)
				}
			}
			for _, table := range []string{"subscription_daily_usage", "node_daily_traffics"} {
				var n int64
				if err := db.Table(table).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatalf("partial rows in %s", table)
				}
			}
			fail = false
			u.syncSingleNode(context.Background(), node, nil)
			if !fake.ackCalled || fake.ackedAt != ts {
				t.Fatal("successful retry was not acknowledged")
			}
			var sub subDomain.Subscription
			if err := db.First(&sub, 1).Error; err != nil {
				t.Fatal(err)
			}
			if sub.DataUsed != 100 || sub.DataUpload != 40 || sub.DataDownload != 60 || sub.LifetimeDataUsed != 100 {
				t.Fatalf("retry counters=%+v", sub)
			}
			var combined subDomain.Subscription
			if err := db.First(&combined, 2).Error; err != nil {
				t.Fatal(err)
			}
			if combined.DataUsed != 35 {
				t.Fatalf("user+WG sub=%d", combined.DataUsed)
			}
			var daily []subDomain.SubscriptionDailyUsage
			if err := db.Where("subscription_id = ?", 1).Order("date").Find(&daily).Error; err != nil {
				t.Fatal(err)
			}
			if len(daily) != 2 || daily[0].DataUsed != 30 || daily[1].DataUsed != 70 {
				t.Fatalf("UTC splits=%+v", daily)
			}
			var peer wgDomain.WGPeer
			if err := db.First(&peer, 1).Error; err != nil {
				t.Fatal(err)
			}
			if peer.UpBytes != 11 || peer.DownBytes != 13 {
				t.Fatalf("WG counters=%+v", peer)
			}
		})
	}
}

type gatedTrafficClient struct {
	*fakeStatsAgentClient
	entered chan struct{}
	unblock chan struct{}
	once    sync.Once
}

func (f *gatedTrafficClient) GetBufferedTraffic(ctx context.Context) (*agent.BufferedTrafficStats, error) {
	first := false
	f.once.Do(func() { first = true; close(f.entered) })
	if first {
		select {
		case <-f.unblock:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ackCalled {
		return &agent.BufferedTrafficStats{}, nil
	}
	return f.buffered, nil
}
func TestTrafficSyncSerializesFetchThroughAcknowledgement(t *testing.T) {
	db, u, node, base := setupTrafficIntegration(t, 1)
	base.buffered = &agent.BufferedTrafficStats{Records: []*agent.TrafficRecord{{Timestamp: time.Now().Unix(), UserUplink: map[string]int64{"user_1": 7}}}}
	client := &gatedTrafficClient{fakeStatsAgentClient: base, entered: make(chan struct{}), unblock: make(chan struct{})}
	u.statsAgentClientFactory = func(context.Context, *domain.Node) (agent.NodeClient, error) { return client, nil }
	done := make(chan struct{})
	go func() { u.syncSingleNode(context.Background(), node, nil); close(done) }()
	<-client.entered
	// A canceled overlapping manual sweep must leave the live collector alone.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	u.syncSingleNode(canceled, node, nil)
	second := make(chan struct{})
	go func() { u.syncSingleNode(context.Background(), node, nil); close(second) }()
	close(client.unblock)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("first sync hung")
	}
	select {
	case <-second:
	case <-time.After(3 * time.Second):
		t.Fatal("second sync hung")
	}
	var sub subDomain.Subscription
	if err := db.First(&sub, 1).Error; err != nil {
		t.Fatal(err)
	}
	if sub.DataUsed != 7 {
		t.Fatalf("overlapping sync double counted: %d", sub.DataUsed)
	}
	u.statsSyncMu.Lock()
	defer u.statsSyncMu.Unlock()
	if len(u.statsSyncs) != 0 {
		t.Fatal("idle sync gates retained")
	}
}

// Agent Drain can recreate a new delta at the same hour-bucket timestamp. A
// timestamp-only dedup optimization would silently drop this second batch.
func TestTrafficBatchReusedAgentTimestampIsNotDropped(t *testing.T) {
	db, u, node, _ := setupTrafficIntegration(t, 1)
	records := []*agent.TrafficRecord{{Timestamp: time.Now().Unix(), UserUplink: map[string]int64{"user_1": 7}}}
	for range 2 {
		if _, err := u.persistBufferedTraffic(context.Background(), node, records); err != nil {
			t.Fatal(err)
		}
	}
	var sub subDomain.Subscription
	if err := db.First(&sub, 1).Error; err != nil {
		t.Fatal(err)
	}
	if sub.DataUsed != 14 {
		t.Fatalf("new delta with reused timestamp lost: %d", sub.DataUsed)
	}
}

type failingTrafficLookup struct{ subRepo.SubscriptionRepository }

func (failingTrafficLookup) FindByConfigEmails(context.Context, []string) (map[string]*subDomain.Subscription, error) {
	return nil, errors.New("lookup unavailable")
}
func TestTrafficLookupFailureDoesNotCommitOrAcknowledge(t *testing.T) {
	db, u, node, fake := setupTrafficIntegration(t, 1)
	u.subRepo = failingTrafficLookup{u.subRepo}
	fake.buffered = &agent.BufferedTrafficStats{Records: []*agent.TrafficRecord{{Timestamp: time.Now().Unix(), TotalUplink: 7, UserUplink: map[string]int64{"user_1": 7}}}}
	u.syncSingleNode(context.Background(), node, nil)
	if fake.ackCalled {
		t.Fatal("lookup failure acknowledged")
	}
	var n domain.Node
	if err := db.First(&n, 1).Error; err != nil {
		t.Fatal(err)
	}
	if n.TotalUplink != 0 {
		t.Fatalf("lookup failure partially committed node bytes: %d", n.TotalUplink)
	}
}

func TestTrafficBatchSelectsNewestNonExcludedWGAccount(t *testing.T) {
	db, u, node, _ := setupTrafficIntegration(t, 1)
	now := time.Now().UTC()
	subID := uint(1)
	accounts := []accountDomain.Account{
		{ID: 2, InboundID: 2, SubscriptionID: &subID, Email: "newest-wg", UUID: "newest", Source: accountDomain.AccountSourceManual, CreatedAt: now},
		{ID: 3, InboundID: 2, SubscriptionID: &subID, Email: "excluded-wg", UUID: "excluded", Source: accountDomain.AccountSourceAdminExcluded, CreatedAt: now.Add(time.Hour)},
		{ID: 4, InboundID: 2, SubscriptionID: &subID, Email: "old-wg", UUID: "old", Source: accountDomain.AccountSourceManual, CreatedAt: now.Add(-time.Hour)},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&wgDomain.WGPeer{ID: 1, SubscriptionID: 1, InboundID: 2, PublicKey: "key", PresharedKey: "psk", AssignedIP: "10.0.0.2"}).Error; err != nil {
		t.Fatal(err)
	}
	u.wgPeerSource = trafficTestPeers{repo: wgRepo.NewWGPeerRepository(db), peers: []WGRenderPeer{{SubscriptionID: 1, PeerID: 1, InboundID: 2, AllowedIP: "10.0.0.2"}}}
	if _, err := u.persistBufferedTraffic(context.Background(), node, []*agent.TrafficRecord{{Timestamp: now.Unix(), UserUplink: map[string]int64{"wg:wg-in:10.0.0.2": 24}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{2, 3, 4} {
		var a accountDomain.Account
		if err := db.First(&a, id).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(0)
		if id == 2 {
			want = 24
		}
		if a.DataUsed != want {
			t.Fatalf("account %d usage=%d want %d", id, a.DataUsed, want)
		}
	}
}
