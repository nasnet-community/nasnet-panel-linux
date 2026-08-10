package repository

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Its own database, unlike newDB's shared one: these tests count rows.
func newOwnDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

// Every statement here names columns by hand, and GORM's naming is not the JSON
// naming — DPort is `d_port`, not `dport`. Only a real database catches that.
func TestPortForwardRepository_RoundTrip(t *testing.T) {
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.PortForward{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := NewPortForwardRepository(db)

	pf := domain.PortForward{
		UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "10.77.0.5", ToPort: 80, Comment: "nas", Enabled: true,
	}
	if err := r.Create(ctx, &pf); err != nil {
		t.Fatalf("create: %v", err)
	}
	if pf.ID == 0 {
		t.Fatal("create did not assign an ID")
	}

	rows, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].DPort != 8080 || rows[0].ToAddr != "10.77.0.5" {
		t.Fatalf("list = %+v", rows)
	}

	pf.DPort, pf.Enabled = 9090, false
	if err := r.Update(ctx, &pf); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := r.Get(ctx, pf.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DPort != 9090 || got.Enabled {
		t.Errorf("after update = %+v", got)
	}

	if err := r.Delete(ctx, pf.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rows, _ := r.List(ctx); len(rows) != 0 {
		t.Errorf("delete left %d rows", len(rows))
	}
}

// Get never returns nil, so the UI has defaults and Reconcile sees no LAN.
func TestLANRepository_DefaultsThenPersists(t *testing.T) {
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.LANConfig{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := NewLANRepository(db)

	cfg, err := r.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg == nil || cfg.Enabled || cfg.CIDR != "10.77.0.1/24" {
		t.Fatalf("defaults = %+v", cfg)
	}

	cfg.Enabled, cfg.InputFirewall, cfg.LeaseHours = true, true, 6
	if err := r.Save(ctx, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	// A second save must land on the same row, not create a duplicate.
	cfg.LeaseHours = 8
	if err := r.Save(ctx, cfg); err != nil {
		t.Fatalf("second save: %v", err)
	}

	var count int64
	db.Model(&domain.LANConfig{}).Count(&count)
	if count != 1 {
		t.Errorf("stored %d LAN rows, want 1", count)
	}

	back, err := r.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Enabled || !back.InputFirewall || back.LeaseHours != 8 {
		t.Errorf("round trip = %+v", back)
	}
}

// The dead-man reverts the kernel but not the intent, so an unconfirmed firewall
// would come back armed. Disarming can only ever turn the lockout risk off.
func TestLANRepository_DisarmInputFirewall(t *testing.T) {
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.LANConfig{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := NewLANRepository(db)

	cfg, _ := r.Get(ctx)
	cfg.Enabled, cfg.InputFirewall = true, true
	if err := r.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	if err := r.DisarmInputFirewall(ctx); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	back, _ := r.Get(ctx)
	if back.InputFirewall {
		t.Error("the firewall is still armed after a revert")
	}
	// The LAN itself is a separate decision and must survive.
	if !back.Enabled {
		t.Error("disarming the firewall also turned the LAN off")
	}
}

// No row yet is not an error: there is nothing armed to disarm.
func TestLANRepository_DisarmWithNoRow(t *testing.T) {
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.LANConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := NewLANRepository(db).DisarmInputFirewall(context.Background()); err != nil {
		t.Errorf("disarm with no row: %v", err)
	}
}
