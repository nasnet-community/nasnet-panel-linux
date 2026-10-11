package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/config"
	authHTTP "github.com/nasnet-community/nasnet-panel-linux/internal/auth/delivery/http"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	settingUC "github.com/nasnet-community/nasnet-panel-linux/internal/setting/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	userUC "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Only GetByUsername is reached by admin login; it reports "no such user".
type noUsers struct{ userUC.UserUsecase }

func (noUsers) GetByUsername(context.Context, string) (*userDomain.User, error) {
	return nil, gorm.ErrRecordNotFound
}

func testHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func sqliteConfig(t *testing.T) *config.DatabaseConfig {
	t.Helper()
	return &config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "nasnet_panel.db")}
}

func TestStoredHashWorksAtNextLoginWithoutRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbCfg := sqliteConfig(t)
	db, err := database.Connect(dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	if err := database.AutoMigrate(db, &settingDomain.Setting{}); err != nil {
		t.Fatal(err)
	}
	settings := settingUC.NewSettingUsecase(settingRepo.NewSettingRepository(db), nil)
	// A password changed earlier in the panel overrides the env hash; the reset must beat both.
	if err := settings.UpdateMany(context.Background(), []*settingDomain.Setting{settingDomain.AdminPasswordHashSetting(testHash(t, "panel-password"))}); err != nil {
		t.Fatal(err)
	}
	envHash := testHash(t, "install-password")
	handler := authHTTP.NewAuthHandler(noUsers{}, jwt.NewManager(jwt.Config{SecretKey: "test-secret"}), settings, nil, "",
		config.AdminConfig{Username: "admin", PasswordHash: envHash})
	router := gin.New()
	handler.RegisterRoutes(router.Group("/api/v1"))
	login := func(password string) int {
		body := `{"username":"admin","password":"` + password + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/admin-login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := login("reset-password"); code != http.StatusUnauthorized {
		t.Fatalf("login before reset = %d, want 401", code)
	}
	newHash := testHash(t, "reset-password")
	if err := storeAdminPasswordHash(context.Background(), dbCfg, newHash); err != nil {
		t.Fatal(err)
	}
	if code := login("reset-password"); code != http.StatusOK {
		t.Fatalf("login with the reset password on the running handler = %d, want 200", code)
	}
	if code := login("panel-password"); code != http.StatusUnauthorized {
		t.Fatalf("old panel password still accepted: %d", code)
	}

	// After a restart the env hash (written by the tool too) and the DB agree.
	cfg := &config.Config{Admin: config.AdminConfig{Username: "admin", PasswordHash: newHash}}
	if err := resolveStartupConfig(cfg, func(key string) (string, error) { return settings.GetByKey(context.Background(), key) }); err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.PasswordHash != newHash {
		t.Fatal("startup must resolve the reset hash")
	}
}

func TestStoreAdminPasswordHashNeedsInitializedDatabase(t *testing.T) {
	hash := testHash(t, "reset-password")
	missing := sqliteConfig(t)
	if err := storeAdminPasswordHash(context.Background(), missing, hash); !errors.Is(err, errDatabaseNotInitialized) {
		t.Fatalf("missing database: got %v", err)
	}
	if _, err := os.Stat(missing.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reset must not create the database file")
	}

	empty := sqliteConfig(t)
	db, err := database.Connect(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE unrelated (id integer)").Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
	if err := storeAdminPasswordHash(context.Background(), empty, hash); !errors.Is(err, errDatabaseNotInitialized) {
		t.Fatalf("database without settings: got %v", err)
	}
}

func TestReadAdminPasswordHash(t *testing.T) {
	hash := testHash(t, "reset-password")
	got, err := readAdminPasswordHash(strings.NewReader(hash + "\n"))
	if err != nil || got != hash {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, input := range []string{"", "$2a$10", "plain-password\n"} {
		if _, err := readAdminPasswordHash(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
