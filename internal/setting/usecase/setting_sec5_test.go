package usecase

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// newTestUsecase spins up an in-memory sqlite + migrated settings table
// and returns a usecase wired to it. Each test gets its own DB so runs
// stay isolated.
func newTestUsecase(t *testing.T) domain.SettingUsecase {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Fresh schema per test — share-cache DSN would otherwise leak rows.
	if err := db.Migrator().DropTable(&domain.Setting{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := db.AutoMigrate(&domain.Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSettingUsecase(repository.NewSettingRepository(db), &InitialConfig{})
}

// seedPassword stores a panel password through the normal settings write path.
func seedPassword(t *testing.T, uc domain.SettingUsecase, value string) {
	t.Helper()
	err := uc.UpdateMany(context.Background(), []*domain.Setting{{
		Key:      "sub_panel_password",
		Value:    value,
		Type:     "string",
		Category: "sub_panel",
	}})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestUpdateMany_HashesOnWrite(t *testing.T) {
	// UpdateMany already hashes panel passwords on write.
	// Re-verify the contract so a future refactor doesn't lose it.
	uc := newTestUsecase(t)
	seedPassword(t, uc, "fresh-secret")

	stored, err := uc.GetByKey(context.Background(), "sub_panel_password")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.HasPrefix(stored, "$2") {
		t.Fatalf("UpdateMany must hash panel passwords, got %q", stored)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte("fresh-secret")); err != nil {
		t.Errorf("stored hash doesn't verify original: %v", err)
	}
}

func TestMaskValue_HidesSecrets(t *testing.T) {
	// maskValue is a private helper but worth pinning behavior — it's
	// what the API returns to clients for sensitive settings.
	cases := []struct {
		in, want string
	}{
		{"abcd", "****"},         // <=4: fully masked
		{"abcdefgh", "****efgh"}, // keep last 4
		{"x", "*"},               // single char
		{"", ""},                 // empty stays empty
	}
	for _, c := range cases {
		if got := maskValue(c.in); got != c.want {
			t.Errorf("maskValue(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSeedDefaults_InitializesPoolStrategyAndPreservesChoice(t *testing.T) {
	ctx := context.Background()
	uc := newTestUsecase(t)
	if err := uc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := uc.GetByKey(ctx, "router_vpn_pool_strategy"); err != nil || got != "spread" {
		t.Fatalf("fresh pool strategy = %q, err = %v", got, err)
	}
	if err := uc.UpdateMany(ctx, []*domain.Setting{{Key: "router_vpn_pool_strategy", Value: "fastest"}}); err != nil {
		t.Fatal(err)
	}
	if err := uc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := uc.GetByKey(ctx, "router_vpn_pool_strategy"); err != nil || got != "fastest" {
		t.Fatalf("chosen pool strategy after restart = %q, err = %v", got, err)
	}
}
