package usecase

import (
	"context"
	"strings"
	"testing"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
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

func auditText(r *auditRecorder) string {
	var b strings.Builder
	for _, e := range r.entries {
		b.WriteString(e.OldValues + e.NewValues + "\n")
	}
	return b.String()
}

// PUT /settings accepts any key, and rows saved by older versions say sensitive=false.
func TestUpdateMany_AuditRedactsAdminPasswordHashByKey(t *testing.T) {
	ctx := context.Background()
	uc := newTestUsecase(t)
	old := "$2a$10$legacyhashlegacyhashlegacyhashlegacyhashlegacyhashleg"
	if err := uc.UpdateMany(ctx, []*domain.Setting{{Key: domain.AdminPasswordHashKey, Value: old, Category: "admin"}}); err != nil {
		t.Fatal(err)
	}
	audit := &auditRecorder{}
	uc.SetAuditUsecase(audit)
	next := "$2a$10$newhashnewhashnewhashnewhashnewhashnewhashnewhashnewha"
	if err := uc.UpdateMany(ctx, []*domain.Setting{{Key: domain.AdminPasswordHashKey, Value: next}}); err != nil {
		t.Fatal(err)
	}
	if got := auditText(audit); len(audit.entries) != 1 || strings.Contains(got, "$2a$") {
		t.Fatalf("audit = %s", got)
	}
}

// The proxy URL carries user:pass@ per its own description.
func TestUpdateMany_AuditRedactsOutboundProxyCredentials(t *testing.T) {
	ctx := context.Background()
	uc := newTestUsecase(t)
	if err := uc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	audit := &auditRecorder{}
	uc.SetAuditUsecase(audit)
	if err := uc.UpdateMany(ctx, []*domain.Setting{{Key: "outbound_proxy_url", Value: "socks5h://alice:s3cret@proxy.example:1080"}}); err != nil {
		t.Fatal(err)
	}
	if got := auditText(audit); len(audit.entries) != 1 || strings.Contains(got, "s3cret") {
		t.Fatalf("audit = %s", got)
	}
}
