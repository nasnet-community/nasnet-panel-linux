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

// seedPassword inserts a single sub_panel_password row directly so we
// can simulate the pre-migration (plaintext) and already-migrated
// (bcrypt) states.
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

func TestPasswordMigration_HashesPlaintext(t *testing.T) {
	// Bypass UpdateMany's own bcrypt-on-write so we can plant a
	// raw plaintext password as if it was stored by an older build.
	db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	_ = db.Migrator().DropTable(&domain.Setting{})
	_ = db.AutoMigrate(&domain.Setting{})
	repo := repository.NewSettingRepository(db)
	// Write raw.
	if err := repo.Update(context.Background(), &domain.Setting{
		Key:      "sub_panel_password",
		Value:    "plaintext-secret",
		Type:     "string",
		Category: "sub_panel",
	}); err != nil {
		t.Fatalf("seed raw: %v", err)
	}

	uc := NewSettingUsecase(repo, &InitialConfig{})
	uc.MigrateGlobalPanelPassword(context.Background())

	stored, err := uc.GetByKey(context.Background(), "sub_panel_password")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.HasPrefix(stored, "$2") {
		t.Fatalf("expected bcrypt hash, got %q", stored)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte("plaintext-secret")); err != nil {
		t.Errorf("bcrypt compare should verify original password: %v", err)
	}
}

func TestPasswordMigration_Idempotent(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	_ = db.Migrator().DropTable(&domain.Setting{})
	_ = db.AutoMigrate(&domain.Setting{})
	repo := repository.NewSettingRepository(db)

	// Pre-hashed value; migration must not double-hash.
	preHashed, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	_ = repo.Update(context.Background(), &domain.Setting{
		Key:      "sub_panel_password",
		Value:    string(preHashed),
		Type:     "string",
		Category: "sub_panel",
	})

	uc := NewSettingUsecase(repo, &InitialConfig{})
	uc.MigrateGlobalPanelPassword(context.Background())
	uc.MigrateGlobalPanelPassword(context.Background()) // run twice

	after, _ := uc.GetByKey(context.Background(), "sub_panel_password")
	if after != string(preHashed) {
		t.Errorf("bcrypt value was mutated by migration: before=%q after=%q", string(preHashed), after)
	}
	// Original password still verifies.
	if err := bcrypt.CompareHashAndPassword([]byte(after), []byte("secret")); err != nil {
		t.Errorf("idempotent migration broke password: %v", err)
	}
}

func TestPasswordMigration_EmptyNoop(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	_ = db.Migrator().DropTable(&domain.Setting{})
	_ = db.AutoMigrate(&domain.Setting{})
	repo := repository.NewSettingRepository(db)
	// Row exists but value is empty (no panel password set yet).
	_ = repo.Update(context.Background(), &domain.Setting{
		Key:      "sub_panel_password",
		Value:    "",
		Type:     "string",
		Category: "sub_panel",
	})

	uc := NewSettingUsecase(repo, &InitialConfig{})
	uc.MigrateGlobalPanelPassword(context.Background())
	after, _ := uc.GetByKey(context.Background(), "sub_panel_password")
	if after != "" {
		t.Errorf("empty password should stay empty, got %q", after)
	}
}

func TestPasswordMigration_MissingRowNoop(t *testing.T) {
	uc := newTestUsecase(t)
	// Should not panic or error when row doesn't exist at all.
	uc.MigrateGlobalPanelPassword(context.Background())
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
