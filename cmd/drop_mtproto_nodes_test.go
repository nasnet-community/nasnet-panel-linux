package cmd

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	alertDomain "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
)

func cleanupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&nodeDomain.Node{}, &nodeDomain.NodeStat{}, &coreDomain.CoreInbound{}, &coreDomain.CoreUser{},
		&alertDomain.Rule{}, &alertDomain.State{}, &alertDomain.Event{}, &settingDomain.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func count(t *testing.T, db *gorm.DB, model any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// A panel upgraded from one with MTProto nodes: their nodes, tunnels, stats,
// alert rules, setting and tables go; everything else stays.
func TestDropMTProtoNodes(t *testing.T) {
	db := cleanupDB(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// what the removed code left in the schema
	must(db.Exec(`ALTER TABLE nodes ADD COLUMN kind varchar(10) NOT NULL DEFAULT 'full'`).Error)
	must(db.Exec(`CREATE TABLE lite_nodes (node_id integer PRIMARY KEY)`).Error)
	must(db.Exec(`CREATE TABLE lite_usage_baselines (node_id integer, name text)`).Error)

	must(db.Create(&nodeDomain.Node{ID: 1, Name: "full", IP: "203.0.113.1"}).Error)
	must(db.Create(&nodeDomain.Node{ID: 2, Name: "mtproto", IP: "203.0.113.2", ConnectMode: "link"}).Error)
	must(db.Exec(`UPDATE nodes SET kind = 'lite' WHERE id = 2`).Error)
	for _, id := range []uint{1, 2} {
		must(db.Create(&nodeDomain.NodeStat{NodeID: id}).Error)
		must(db.Create(&coreDomain.CoreInbound{ID: 10 + id, NodeID: id, Protocol: coreDomain.CoreProtoMTProto, Port: 443}).Error)
		must(db.Create(&coreDomain.CoreUser{CoreInboundID: 10 + id, Username: "u", Email: "u@a", Enabled: true}).Error)
	}
	must(db.Create(&alertDomain.Rule{ID: 1, Name: "offline", RuleType: alertDomain.RuleTypeNodeOffline}).Error)
	must(db.Create(&alertDomain.Rule{ID: 2, Name: "egress", RuleType: "relay_egress_down"}).Error)
	for _, id := range []uint{1, 2} {
		must(db.Create(&alertDomain.State{RuleID: id, EntityKey: "node:2"}).Error)
		must(db.Create(&alertDomain.Event{RuleID: id, EntityKey: "node:2"}).Error)
	}
	must(db.Create(&settingDomain.Setting{Key: "relay_cidr_url", Value: "off"}).Error)
	must(db.Create(&settingDomain.Setting{Key: "site_name", Value: "x"}).Error)

	for run := 1; run <= 2; run++ {
		n, err := dropMTProtoNodes(db)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if want := map[int]int{1: 1, 2: 0}[run]; n != want {
			t.Fatalf("run %d removed %d nodes, want %d", run, n, want)
		}
	}

	if count(t, db, &nodeDomain.Node{}, "id = 2") != 0 || count(t, db, &nodeDomain.Node{}, "id = 1") != 1 {
		t.Error("only the MTProto node should be gone")
	}
	if count(t, db, &coreDomain.CoreInbound{}, "node_id = 2") != 0 || count(t, db, &coreDomain.CoreUser{}, "core_inbound_id = 12") != 0 {
		t.Error("the MTProto node's tunnel is left")
	}
	if count(t, db, &coreDomain.CoreInbound{}, "node_id = 1") != 1 || count(t, db, &coreDomain.CoreUser{}, "core_inbound_id = 11") != 1 {
		t.Error("the full node's tunnel went too")
	}
	if count(t, db, &nodeDomain.NodeStat{}, "node_id = 2") != 0 || count(t, db, &nodeDomain.NodeStat{}, "node_id = 1") != 1 {
		t.Error("stats: only the MTProto node's should go")
	}
	if count(t, db, &alertDomain.Rule{}, "1 = 1") != 1 || count(t, db, &alertDomain.State{}, "rule_id = 2") != 0 || count(t, db, &alertDomain.Event{}, "rule_id = 2") != 0 {
		t.Error("the MTProto alert rule or its history is left")
	}
	if count(t, db, &alertDomain.State{}, "rule_id = 1") != 1 || count(t, db, &alertDomain.Event{}, "rule_id = 1") != 1 {
		t.Error("another rule's history went too")
	}
	if count(t, db, &settingDomain.Setting{}, "1 = 1") != 1 {
		t.Error("want only site_name left in settings")
	}
	for _, table := range []string{"lite_nodes", "lite_usage_baselines"} {
		if db.Migrator().HasTable(table) {
			t.Errorf("%s is still there", table)
		}
	}
}

// A panel that never had MTProto nodes has no kind column and nothing to drop.
func TestDropMTProtoNodesOnAFreshPanel(t *testing.T) {
	db := cleanupDB(t)
	if err := db.Create(&nodeDomain.Node{ID: 1, Name: "full", IP: "203.0.113.1"}).Error; err != nil {
		t.Fatal(err)
	}
	n, err := dropMTProtoNodes(db)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if count(t, db, &nodeDomain.Node{}, "1 = 1") != 1 {
		t.Error("a node went")
	}
}
