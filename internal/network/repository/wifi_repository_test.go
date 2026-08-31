package repository

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

func newWifiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.WifiConfig{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// Get never returns nil, so the UI has something to render and Reconcile sees
// a disabled AP
func TestWifiRepository_GetByInterfaceDefaultsWhenEmpty(t *testing.T) {
	r := NewWifiRepository(newWifiDB(t))
	cfg, err := r.GetByInterface(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Enabled || cfg.InterfaceID != 7 {
		t.Fatalf("default = %+v", cfg)
	}
	if cfg.Mode != "ap" {
		t.Errorf("default mode = %q, want ap", cfg.Mode)
	}
}

// GORM's Updates skips zero values without an explicit Select, which is how
// "turn the AP off" silently becomes a no-op
func TestWifiRepository_SaveWritesFalseAndZero(t *testing.T) {
	ctx := context.Background()
	r := NewWifiRepository(newWifiDB(t))

	on := domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "nasnet",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g", Channel: 6, Enabled: true, Hidden: true}
	if err := r.Save(ctx, &on); err != nil {
		t.Fatal(err)
	}

	off := on
	off.Enabled, off.Channel, off.Hidden = false, 0, false
	if err := r.Save(ctx, &off); err != nil {
		t.Fatal(err)
	}

	got, err := r.GetByInterface(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Channel != 0 || got.Hidden {
		t.Fatalf("zero values were not written: %+v", got)
	}
	// SSID and PSK are s_s_i_d and p_s_k to GORM; only a real db catches that
	if got.SSID != "nasnet" || got.PSK != "hunter2hunter2" {
		t.Errorf("ssid/psk lost on save: %q / %q", got.SSID, got.PSK)
	}
}

func TestWifiRepository_SaveUpsertsByInterface(t *testing.T) {
	ctx := context.Background()
	r := NewWifiRepository(newWifiDB(t))
	a := domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "one",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"}
	b := domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "two",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"}
	if err := r.Save(ctx, &a); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, &b); err != nil {
		t.Fatal(err)
	}
	rows, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SSID != "two" {
		t.Fatalf("rows = %+v", rows)
	}
}

// What a rollback calls: the captured set, exactly
func TestWifiRepository_ReplaceAllRestoresTheCapturedSet(t *testing.T) {
	ctx := context.Background()
	r := NewWifiRepository(newWifiDB(t))
	orig := domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "before",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"}
	if err := r.Save(ctx, &orig); err != nil {
		t.Fatal(err)
	}
	captured, _ := r.List(ctx)

	changed := orig
	changed.SSID, changed.Enabled = "after", true
	if err := r.Save(ctx, &changed); err != nil {
		t.Fatal(err)
	}

	if err := r.ReplaceAll(ctx, captured); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetByInterface(ctx, 7)
	if got.SSID != "before" || got.Enabled {
		t.Fatalf("rollback did not restore the intent: %+v", got)
	}
	rows, _ := r.List(ctx)
	if len(rows) != 1 {
		t.Fatalf("ReplaceAll duplicated rows: %+v", rows)
	}
}

// An empty capture means nothing was configured, so tear it all down
func TestWifiRepository_ReplaceAllEmptyClearsTheTable(t *testing.T) {
	ctx := context.Background()
	r := NewWifiRepository(newWifiDB(t))
	cfg := domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "x",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g", Enabled: true}
	if err := r.Save(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceAll(ctx, nil); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.List(ctx)
	if len(rows) != 0 {
		t.Fatalf("rows survived an empty restore: %+v", rows)
	}
	// And the soft-delete must not shadow a fresh row on the same interface
	if err := r.Save(ctx, &domain.WifiConfig{InterfaceID: 7, Mode: "ap", SSID: "new",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetByInterface(ctx, 7)
	if got.SSID != "new" {
		t.Fatalf("a deleted row shadowed the new one: %+v", got)
	}
}
