package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupUserDB stands up an in-memory SQLite + the users table only. The
// helper sets DriverName to "sqlite" so dialect-aware builders (ILike etc.)
// emit valid SQLite SQL.
func setupUserDB(t *testing.T) (*gorm.DB, UserRepository) {
	t.Helper()
	database.DriverName = "sqlite"
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, NewUserRepository(db)
}

func seedUser(t *testing.T, repo UserRepository, u *domain.User) {
	t.Helper()
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestUserRepository_CreateAndFindByID(t *testing.T) {
	_, repo := setupUserDB(t)
	u := &domain.User{TelegramID: 100, Username: "alice"}
	seedUser(t, repo, u)
	if u.ID == 0 {
		t.Fatal("Create should populate ID")
	}

	got, err := repo.FindByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TelegramID != 100 || got.Username != "alice" {
		t.Errorf("got %+v", got)
	}

	if _, err := repo.FindByID(context.Background(), 999); err == nil {
		t.Error("FindByID on missing id should error")
	}
}

func TestUserRepository_FindByTelegramID(t *testing.T) {
	_, repo := setupUserDB(t)
	seedUser(t, repo, &domain.User{TelegramID: 555, Username: "bob"})

	got, err := repo.FindByTelegramID(context.Background(), 555)
	if err != nil || got.Username != "bob" {
		t.Errorf("got %+v, err %v", got, err)
	}
	if _, err := repo.FindByTelegramID(context.Background(), 999); err == nil {
		t.Error("missing telegram id should error")
	}
}

// FindByUsername uses ILike → on SQLite this becomes LIKE, which is
// case-insensitive for ASCII. Verify both cases match the stored value.
func TestUserRepository_FindByUsername_CaseInsensitive(t *testing.T) {
	_, repo := setupUserDB(t)
	seedUser(t, repo, &domain.User{TelegramID: 1, Username: "Alice"})

	got, err := repo.FindByUsername(context.Background(), "alice")
	if err != nil || got.Username != "Alice" {
		t.Errorf("got %+v, err %v", got, err)
	}
}

func TestUserRepository_BanAndAdminStatus_Counts(t *testing.T) {
	_, repo := setupUserDB(t)
	plain := &domain.User{TelegramID: 1}
	banned := &domain.User{TelegramID: 2}
	admin := &domain.User{TelegramID: 3}
	seedUser(t, repo, plain)
	seedUser(t, repo, banned)
	seedUser(t, repo, admin)

	if err := repo.UpdateBanStatus(context.Background(), banned.ID, true); err != nil {
		t.Fatalf("UpdateBanStatus: %v", err)
	}
	if err := repo.UpdateAdminStatus(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("UpdateAdminStatus: %v", err)
	}

	ctx := context.Background()
	if n, _ := repo.CountAll(ctx); n != 3 {
		t.Errorf("CountAll = %d, want 3", n)
	}
	if n, _ := repo.CountBanned(ctx); n != 1 {
		t.Errorf("CountBanned = %d, want 1", n)
	}
	if n, _ := repo.CountAdmins(ctx); n != 1 {
		t.Errorf("CountAdmins = %d, want 1", n)
	}
}

func TestUserRepository_ListAdmins(t *testing.T) {
	_, repo := setupUserDB(t)
	seedUser(t, repo, &domain.User{TelegramID: 1})
	a := &domain.User{TelegramID: 2}
	seedUser(t, repo, a)
	_ = repo.UpdateAdminStatus(context.Background(), a.ID, true)

	got, err := repo.ListAdmins(context.Background())
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("got %d admins; want only id=%d", len(got), a.ID)
	}
}

// Update writes the full struct back; verify the chosen field round-trips.
func TestUserRepository_Update(t *testing.T) {
	_, repo := setupUserDB(t)
	u := &domain.User{TelegramID: 1, Username: "old"}
	seedUser(t, repo, u)

	u.Username = "new"
	if err := repo.Update(context.Background(), u); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), u.ID)
	if got.Username != "new" {
		t.Errorf("Username = %q, want new", got.Username)
	}
}

func TestUserRepository_Delete_SoftDeletes(t *testing.T) {
	_, repo := setupUserDB(t)
	u := &domain.User{TelegramID: 1}
	seedUser(t, repo, u)

	if err := repo.Delete(context.Background(), u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// gorm.DeletedAt makes Delete soft: FindByID should now return
	// "record not found" because gorm filters deleted rows by default.
	_, err := repo.FindByID(context.Background(), u.ID)
	if err == nil || !strings.Contains(err.Error(), "record not found") {
		t.Errorf("expected record-not-found after soft delete, got %v", err)
	}
}
