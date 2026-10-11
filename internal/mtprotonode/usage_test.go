package mtprotonode

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	coreRepository "github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepository "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// booking is a real database behind the real node usecase, so a test sees what
// a subscriber's quota sees.
type booking struct {
	db *gorm.DB
	uc nodeUsecase.NodeUsecase
}

func newBooking(t *testing.T, row LiteNode) *booking {
	t.Helper()
	// a file, not :memory:: a cancelled query may cost the connection, and a new one would see an empty database
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "panel.db")), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1) // the panel's SQLite runs on one connection
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&nodeDomain.Node{}, &nodeDomain.Inbound{}, &nodeDomain.NodeDailyTraffic{},
		&subDomain.Subscription{}, &subDomain.SubscriptionDailyUsage{}, &accountDomain.Account{},
		&coreDomain.CoreInbound{}, &coreDomain.CoreUser{}, &LiteNode{}, &UsageBaseline{}); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Create(&nodeDomain.Node{ID: 1, Name: "vps-1", UUID: "vps-1", IP: "127.0.0.1", Kind: nodeDomain.NodeKindLite, ConnectMode: nodeDomain.ConnectModeLink, IsActive: true}).Error)
	must(db.Create(&subDomain.Subscription{ID: 1, ConfigID: "cfg-1", LinkKey: "link-1", ConfigEmail: "alice@example.com"}).Error)
	must(db.Create(&subDomain.Subscription{ID: 2, ConfigID: "cfg-2", LinkKey: "link-2", ConfigEmail: "mallory@example.com"}).Error)
	must(db.Create(&coreDomain.CoreInbound{ID: 1, NodeID: 1, Protocol: coreDomain.CoreProtoMTProto, Port: 443, EgressMode: coreDomain.EgressHub, Settings: []byte(`{"tls_domain":"example.com"}`)}).Error)
	subID := uint(1)
	must(db.Create(&coreDomain.CoreUser{ID: 1, CoreInboundID: 1, SubscriptionID: &subID, Username: "alice", Secret: strings.Repeat("ab", 16), Email: "alice@example.com", Enabled: true}).Error)
	must(db.Create(&row).Error)

	uc := nodeUsecase.NewNodeUsecase(nodeRepository.NewNodeRepository(db), subRepo.NewSubscriptionRepository(db), nil,
		accountRepo.NewAccountRepository(db), nil, nil, nil, events.NewEventBus(), nil, database.NewTransactionManager(db))
	uc.SetCoreUsageProvider(coreRepository.NewCoreRepository(db))
	return &booking{db: db, uc: uc}
}

// quietNodes is the real usecase minus the health writer, which needs the
// whole panel behind it.
type quietNodes struct{ uc nodeUsecase.NodeUsecase }

func (q quietNodes) GetNode(ctx context.Context, id uint) (*nodeDomain.Node, error) {
	return q.uc.GetNode(ctx, id)
}
func (quietNodes) ObserveNodeHealth(context.Context, *nodeDomain.Node, bool, string, string) {}
func (q quietNodes) IngestTraffic(ctx context.Context, id uint, recs []*agent.TrafficRecord, inTx func(context.Context) error) (int64, error) {
	return q.uc.IngestTraffic(ctx, id, recs, inTx)
}
func (q quietNodes) RecordNodeStat(ctx context.Context, s *nodeDomain.NodeStat) error {
	return q.uc.RecordNodeStat(ctx, s)
}

func (b *booking) manager(t *testing.T) *Manager { t.Helper(); return b.managerWith(t, nil) }

func (b *booking) managerWith(t *testing.T, tweak func(*Config)) *Manager {
	t.Helper()
	cfg := Config{
		Store: NewStore(b.db), Nodes: quietNodes{b.uc}, Dialers: plainDialers(),
		Allow:       &tgnet.Refresher{List: tgnet.NewWithPorts([]netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, 443)},
		ConnsPerWAN: 1, Tick: 100 * time.Millisecond, WatchEvery: time.Hour,
		StatusEvery: 100 * time.Millisecond, StatsEvery: 100 * time.Millisecond, DisguiseEvery: time.Hour,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	m := NewManager(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); m.Stop() })
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return m
}

func (b *booking) used(t *testing.T, subID uint) int64 {
	t.Helper()
	var s subDomain.Subscription
	if err := b.db.First(&s, subID).Error; err != nil {
		t.Fatal(err)
	}
	return s.DataUsed
}

func (b *booking) tunnelUsed(t *testing.T) int64 {
	t.Helper()
	var cu coreDomain.CoreUser
	if err := b.db.First(&cu, 1).Error; err != nil {
		t.Fatal(err)
	}
	return cu.DataUsed
}

func pushAlice(t *testing.T, m *Manager) {
	t.Helper()
	waitFor(t, func() bool { v, ok := m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
	p := payload(u("alice@example.com", "alice", true))
	p.Settings = []byte(`{"tls_domain":"example.com"}`)
	if err := m.Pusher(1).ApplyCoreState(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestMTProtoUsageReachesSubscriptionsAndTunnels(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1000, OctetsDown: 4000}, {Name: "mallory@example.com", OctetsUp: 10, OctetsDown: 10}}
	})
	b := newBooking(t, e.row)
	m := b.manager(t)
	pushAlice(t, m)
	waitFor(t, func() bool {
		base, _ := NewStore(b.db).Baseline(context.Background(), 1)
		return base["alice@example.com"].Up == 1000
	}, "the first report to set the baseline")

	// mallory was never pushed to this node: a node reporting her must not charge her
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1600, OctetsDown: 4500}, {Name: "mallory@example.com", OctetsUp: 9000, OctetsDown: 9000}}
	})
	waitFor(t, func() bool { return b.used(t, 1) == 1100 }, "alice's subscription to be charged 1100 bytes")
	if got := b.tunnelUsed(t); got != 1100 {
		t.Errorf("tunnel usage = %d, want 1100", got)
	}
	var daily int64
	b.db.Model(&subDomain.SubscriptionDailyUsage{}).Where("subscription_id = ?", 1).Select("COALESCE(SUM(data_used),0)").Scan(&daily)
	if daily != 1100 {
		t.Errorf("daily usage = %d, want 1100", daily)
	}
	time.Sleep(300 * time.Millisecond)
	if got := b.used(t, 2); got != 0 {
		t.Fatalf("a subscriber never pushed to this node was charged %d bytes", got)
	}
}

func TestMTProtoUsageIsBookedOnceWhenTheBaselineSaveFails(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1000, OctetsDown: 4000}}
	})
	b := newBooking(t, e.row)
	var fail atomic.Bool
	if err := b.db.Callback().Create().Before("gorm:create").Register("test:fail-baseline", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "lite_usage_baselines" {
			tx.AddError(errors.New("injected baseline failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	m := b.manager(t)
	pushAlice(t, m)
	waitFor(t, func() bool {
		base, _ := NewStore(b.db).Baseline(context.Background(), 1)
		return base["alice@example.com"].Up == 1000
	}, "the first baseline")

	fail.Store(true)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1600, OctetsDown: 4000}}
	})
	time.Sleep(500 * time.Millisecond) // several polls while the save keeps failing
	fail.Store(false)
	waitFor(t, func() bool {
		base, _ := NewStore(b.db).Baseline(context.Background(), 1)
		return base["alice@example.com"].Up == 1600
	}, "the baseline to catch up")
	time.Sleep(300 * time.Millisecond)
	if got := b.used(t, 1); got != 600 {
		t.Fatalf("booked %d bytes, want exactly 600", got)
	}
}

func TestMTProtoUsageIsBookedOnceWhenTheEntryRestartsMidBooking(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1000, OctetsDown: 4000}}
	})
	b := newBooking(t, e.row)
	var m *Manager
	var restart atomic.Bool
	if err := b.db.Callback().Create().Before("gorm:create").Register("test:restart", func(tx *gorm.DB) {
		// the WAN set changed, or a rotation redials, right after the booking
		if tx.Statement.Table == "lite_usage_baselines" && restart.CompareAndSwap(true, false) {
			m.entry(1).cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	m = b.manager(t)
	pushAlice(t, m)
	waitFor(t, func() bool {
		base, _ := NewStore(b.db).Baseline(context.Background(), 1)
		return base["alice@example.com"].Up == 1000
	}, "the first baseline")

	first := m.entry(1)
	restart.Store(true)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice@example.com", OctetsUp: 1600, OctetsDown: 4000}}
	})
	waitFor(t, func() bool { return !restart.Load() }, "the booking to be interrupted")
	if err := m.Ensure(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if m.entry(1) == first {
		t.Fatal("the node was not restarted")
	}
	pushAlice(t, m)
	waitFor(t, func() bool {
		base, _ := NewStore(b.db).Baseline(context.Background(), 1)
		return base["alice@example.com"].Up == 1600
	}, "the new entry to catch up")
	time.Sleep(300 * time.Millisecond)
	if got := b.used(t, 1); got != 600 {
		t.Fatalf("booked %d bytes, want exactly 600", got)
	}
}

// The node page renames and disables an MTProto node through the generic node update.
func TestAnMTProtoNodeCanBeRenamedAndDisabledThroughTheNodeUpdate(t *testing.T) {
	e := newEnv(t)
	b := newBooking(t, e.row)
	m := b.managerWith(t, func(c *Config) { c.WatchEvery = 100 * time.Millisecond })
	waitFor(t, func() bool { return m.IsRunning(1) }, "start")
	ctx := context.Background()
	n, err := b.uc.GetNode(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	n.Name, n.IsActive = "VPS Tehran", false
	if err := b.uc.UpdateNode(ctx, n); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := b.uc.GetNode(ctx, 1)
	if got.Name != "VPS Tehran" || got.IsActive {
		t.Fatalf("node = %q active=%v", got.Name, got.IsActive)
	}
	waitFor(t, func() bool { return !m.IsRunning(1) }, "the disabled node to stop")
	got.IsActive = true
	if err := b.uc.UpdateNode(ctx, got); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitFor(t, func() bool { return m.IsRunning(1) }, "the node to come back")
}
