package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops/relaytest"
	"github.com/nasnet-community/nasnet-panel-linux/internal/mtprotonode"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

// liteNodes is the node usecase as the MTProto node adapter sees it.
type liteNodes struct {
	nodeUsecase.NodeUsecase
	mu      sync.Mutex
	rows    map[uint]bool
	failDel error
}

func (f *liteNodes) GetNode(_ context.Context, id uint) (*nodeDomain.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.rows[id] {
		return nil, gorm.ErrRecordNotFound
	}
	return &nodeDomain.Node{ID: id, Kind: nodeDomain.NodeKindLite, IP: "203.0.113.9"}, nil
}

func (f *liteNodes) DeleteNode(_ context.Context, id uint, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDel != nil {
		return f.failDel
	}
	if !f.rows[id] {
		return gorm.ErrRecordNotFound
	}
	delete(f.rows, id)
	return nil
}

func (f *liteNodes) has(id uint) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.rows[id] }

// A nodes-row delete that fails must leave the MTProto record, or nothing can remove the node.
func TestAFailedNodeDeleteLeavesTheMTProtoNodeRemovable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "panel.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&mtprotonode.LiteNode{}, &mtprotonode.UsageBaseline{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mtprotonode.NewStore(db).Insert(ctx, &mtprotonode.LiteNode{NodeID: 1, AgentID: "a", RealityPub: "aa", ShortID: "bb", Token: "cc", Disguise: "d.example.ir"}); err != nil {
		t.Fatal(err)
	}
	uc := &liteNodes{rows: map[uint]bool{1: true}, failDel: errors.New("database is locked")}
	store, admin := mtprotoRecords(mtprotonode.NewStore(db), nil, uc)
	rig := relaytest.NewRig(func(c *relayops.ServiceConfig) { c.Store, c.Admin = store, admin })

	remove := func() relayops.JobView {
		t.Helper()
		jid, err := rig.Svc.StartUninstall(ctx, 1, nil, true)
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		return rig.Wait(t, jid)
	}
	if v := remove(); v.State != relayops.JobFailed {
		t.Fatalf("job = %+v", v)
	}
	if _, err := store.Get(ctx, 1); err != nil {
		t.Fatalf("the MTProto record went before its node row: %v", err)
	}
	uc.mu.Lock()
	uc.failDel = nil
	uc.mu.Unlock()
	if v := remove(); v.State != relayops.JobOK {
		t.Fatalf("retry = %+v", v)
	}
	if _, err := store.Get(ctx, 1); !errors.Is(err, relayops.ErrNotFound) {
		t.Fatalf("record after the retry: %v", err)
	}
	if uc.has(1) {
		t.Fatal("the nodes row survived the retry")
	}
}
