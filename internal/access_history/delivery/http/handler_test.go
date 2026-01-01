package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/access_history/usecase"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
)

// ─── Stub usecase ───────────────────────────────────────────────────

type stubUC struct {
	resp   *usecase.Response
	err    error
	gotReq usecase.Request
	calls  int

	searchResp   *usecase.SearchResponse
	searchErr    error
	gotSearchReq usecase.SearchRequest
	searchCalls  int

	globalResp   *usecase.GlobalSearchResponse
	globalErr    error
	gotGlobalReq usecase.GlobalSearchRequest
	globalCalls  int
}

type stubAudit struct {
	logs []string
}

func (s *stubAudit) Log(_ context.Context, entry *auditDomain.AuditLog) {
	s.logs = append(s.logs, entry.Action)
}
func (s *stubAudit) List(_ context.Context, _ auditDomain.AuditListFilters) ([]*auditDomain.AuditLog, int64, error) {
	return nil, 0, nil
}
func (s *stubAudit) Cleanup(_ context.Context, _ int) (int64, error) { return 0, nil }
func (s *stubAudit) Stop()                                           {}

func (s *stubUC) GetSubscriptionAccessHistory(_ context.Context, req usecase.Request) (*usecase.Response, error) {
	s.calls++
	s.gotReq = req
	return s.resp, s.err
}

func (s *stubUC) SearchSubscriptionAccessLog(_ context.Context, req usecase.SearchRequest) (*usecase.SearchResponse, error) {
	s.searchCalls++
	s.gotSearchReq = req
	return s.searchResp, s.searchErr
}

func (s *stubUC) SearchGlobalAccessLog(_ context.Context, req usecase.GlobalSearchRequest) (*usecase.GlobalSearchResponse, error) {
	s.globalCalls++
	s.gotGlobalReq = req
	return s.globalResp, s.globalErr
}

func newRouter(uc usecase.Usecase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	NewHandler(uc).RegisterAdminRoutes(g)
	return r
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

// ─── Tests ──────────────────────────────────────────────────────────

func TestHandler_HappyPath(t *testing.T) {
	uc := &stubUC{
		resp: &usecase.Response{
			Granularity:   "hour",
			Series:        nil,
			RetentionDays: 30,
		},
	}
	r := newRouter(uc)

	from := "2026-05-01T00:00:00Z"
	to := "2026-05-01T23:59:59Z"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/42/access-history?from="+from+"&to="+to+"&granularity=hour&node_ids=1,2&top_n=50&include_ips=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if uc.calls != 1 {
		t.Fatalf("usecase invoked %d times", uc.calls)
	}
	got := uc.gotReq
	if got.SubscriptionID != 42 {
		t.Errorf("subID: want 42, got %d", got.SubscriptionID)
	}
	if !got.From.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("from: %v", got.From)
	}
	if !got.To.Equal(time.Date(2026, 5, 1, 23, 59, 59, 0, time.UTC)) {
		t.Errorf("to: %v", got.To)
	}
	if got.Granularity != "hour" {
		t.Errorf("granularity: %q", got.Granularity)
	}
	if len(got.NodeIDs) != 2 || got.NodeIDs[0] != 1 || got.NodeIDs[1] != 2 {
		t.Errorf("node_ids: %v", got.NodeIDs)
	}
	if got.TopN != 50 {
		t.Errorf("top_n: %d", got.TopN)
	}
	if !got.IncludeSourceIPs {
		t.Errorf("include_ips not parsed")
	}

	body := decodeBody(t, w)
	if body["success"] != true {
		t.Errorf("success flag: %v", body["success"])
	}
}

func TestHandler_MissingParams(t *testing.T) {
	uc := &stubUC{}
	r := newRouter(uc)

	cases := []struct {
		name string
		path string
	}{
		{"no from", "/api/v1/subscriptions/1/access-history?to=2026-05-01T00:00:00Z"},
		{"no to", "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z"},
		{"bad time", "/api/v1/subscriptions/1/access-history?from=yesterday&to=2026-05-01T00:00:00Z"},
		{"bad sub id", "/api/v1/subscriptions/abc/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z"},
		{"bad node id", "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z&node_ids=1,foo"},
		{"bad top_n", "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z&top_n=-1"},
		{"bad include_ips", "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z&include_ips=maybe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: want 400, got %d (body: %s)", w.Code, w.Body.String())
			}
		})
	}
	if uc.calls != 0 {
		t.Errorf("usecase should not be called for bad inputs, got %d", uc.calls)
	}
}

func TestHandler_RangeRejected(t *testing.T) {
	uc := &stubUC{err: usecase.ErrRangeOutsideRetention}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/1/access-history?from=2025-01-01T00:00:00Z&to=2026-05-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
}

func TestHandler_EmptySubscriptionReturns200(t *testing.T) {
	uc := &stubUC{err: usecase.ErrSubscriptionEmpty}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Empty-state isn't an error — frontend renders the no-data card.
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandler_InternalError(t *testing.T) {
	uc := &stubUC{err: context.DeadlineExceeded}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/1/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", w.Code)
	}
}

func TestHandler_AuditLoggedOnSuccess(t *testing.T) {
	uc := &stubUC{resp: &usecase.Response{Granularity: "hour"}}
	au := &stubAudit{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	h := NewHandler(uc)
	h.SetAuditUC(au)
	h.RegisterAdminRoutes(g)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/7/access-history?from=2026-05-01T00:00:00Z&to=2026-05-01T23:59:59Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if len(au.logs) != 1 || au.logs[0] != string(auditDomain.AuditSubViewAccessHistory) {
		t.Fatalf("expected one audit row of action %q, got %v", auditDomain.AuditSubViewAccessHistory, au.logs)
	}
}

func TestHandler_NoAuditOnFailure(t *testing.T) {
	uc := &stubUC{err: usecase.ErrInvalidRange}
	au := &stubAudit{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	h := NewHandler(uc)
	h.SetAuditUC(au)
	h.RegisterAdminRoutes(g)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/7/access-history?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if len(au.logs) != 0 {
		t.Fatalf("audit must not fire on failure; got %v", au.logs)
	}
}

// ─── Search ─────────────────────────────────────────────────────────

func TestSearch_HappyPath(t *testing.T) {
	uc := &stubUC{searchResp: &usecase.SearchResponse{Query: "google"}}
	r := newRouter(uc)

	from := "2026-05-01T00:00:00Z"
	to := "2026-05-01T23:59:59Z"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/42/access-history/search?from="+from+"&to="+to+"&q=google&kinds=domain,rejected_domain&limit=300&include_ips=true&node_ids=1,2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if uc.searchCalls != 1 {
		t.Fatalf("usecase invoked %d times", uc.searchCalls)
	}
	got := uc.gotSearchReq
	if got.SubscriptionID != 42 {
		t.Errorf("subID: %d", got.SubscriptionID)
	}
	if got.Query != "google" {
		t.Errorf("query: %q", got.Query)
	}
	if got.Limit != 300 {
		t.Errorf("limit: %d", got.Limit)
	}
	if !got.IncludeSourceIPs {
		t.Errorf("include_ips not parsed")
	}
	if len(got.Kinds) != 2 {
		t.Errorf("kinds: %v", got.Kinds)
	}
	if len(got.NodeIDs) != 2 {
		t.Errorf("node_ids: %v", got.NodeIDs)
	}
}

func TestSearch_MissingQuery(t *testing.T) {
	uc := &stubUC{}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/1/access-history/search?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	if uc.searchCalls != 0 {
		t.Errorf("usecase should not be called when q missing")
	}
}

func TestSearch_QueryTooShort_500to400(t *testing.T) {
	uc := &stubUC{searchErr: usecase.ErrInvalidQuery}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/1/access-history/search?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z&q=ab", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
}

func TestSearch_EmptySubscription_200(t *testing.T) {
	uc := &stubUC{searchErr: usecase.ErrSubscriptionEmpty}
	r := newRouter(uc)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/1/access-history/search?from=2026-05-01T00:00:00Z&to=2026-05-02T00:00:00Z&q=google", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestSearch_AuditLoggedOnSuccess(t *testing.T) {
	uc := &stubUC{searchResp: &usecase.SearchResponse{}}
	au := &stubAudit{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	h := NewHandler(uc)
	h.SetAuditUC(au)
	h.RegisterAdminRoutes(g)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/subscriptions/7/access-history/search?from=2026-05-01T00:00:00Z&to=2026-05-01T23:59:59Z&q=google", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if len(au.logs) != 1 || au.logs[0] != string(auditDomain.AuditSubSearchAccessHistory) {
		t.Fatalf("expected one audit row of action %q, got %v", auditDomain.AuditSubSearchAccessHistory, au.logs)
	}
}
