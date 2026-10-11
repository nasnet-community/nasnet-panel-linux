package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/nasnet-community/nasnet-panel-linux/config"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	settingUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/setting/usecase"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type auditRecorder struct{ entries []*auditDomain.AuditLog }

func (r *auditRecorder) Log(_ context.Context, e *auditDomain.AuditLog) {
	r.entries = append(r.entries, e)
}
func (r *auditRecorder) List(context.Context, auditDomain.AuditListFilters) ([]*auditDomain.AuditLog, int64, error) {
	return r.entries, int64(len(r.entries)), nil
}
func (r *auditRecorder) Cleanup(context.Context, int) (int64, error) { return 0, nil }
func (r *auditRecorder) Stop()                                       {}

func changePassword(t *testing.T, router *gin.Engine, current, next string) {
	t.Helper()
	body := `{"current_password":"` + current + `","new_password":"` + next + `","confirm_password":"` + next + `"}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/change-password", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("change %s -> %s: %d %s", current, next, w.Code, w.Body.String())
	}
}

// The audit log is readable by every admin session and kept for months; a
// bcrypt hash there can be cracked offline.
func TestChangePasswordAuditHasNoHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&settingDomain.Setting{}); err != nil {
		t.Fatal(err)
	}
	// A row saved before the hash was flagged sensitive.
	legacy, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	row := settingDomain.AdminPasswordHashSetting(string(legacy))
	row.Sensitive = false
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}

	audit := &auditRecorder{}
	settings := settingUsecase.NewSettingUsecase(settingRepo.NewSettingRepository(db), &settingUsecase.InitialConfig{})
	settings.SetAuditUsecase(audit)
	h := NewAuthHandler(nil, nil, settings, audit, "", config.AdminConfig{Username: "admin", PasswordHash: string(legacy)})
	router := gin.New()
	h.RegisterAdminRoutes(router.Group(""))

	changePassword(t, router, "old-password", "new-password-1")
	changePassword(t, router, "new-password-1", "new-password-2")

	stored, err := settings.GetByKey(context.Background(), settingDomain.AdminPasswordHashKey)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(stored), []byte("new-password-2")) != nil {
		t.Fatalf("password not changed: %q, %v", stored, err)
	}
	var settingEntries int
	for _, e := range audit.entries {
		if e.Action == string(auditDomain.AuditSettingsUpdate) {
			settingEntries++
		}
		if values := e.OldValues + e.NewValues; strings.Contains(values, "$2") || strings.Contains(values, string(legacy)) {
			t.Errorf("audit %s holds a password hash: old=%s new=%s", e.Action, e.OldValues, e.NewValues)
		}
	}
	if settingEntries != 2 {
		t.Fatalf("want 2 settings audit entries, got %d", settingEntries)
	}
}
