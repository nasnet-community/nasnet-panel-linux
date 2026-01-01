package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newCtx() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func assertBody(t *testing.T, w *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d", w.Code, status)
	}
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestError(t *testing.T) {
	c, w := newCtx()
	Error(c, http.StatusNotFound, "nope")
	assertBody(t, w, http.StatusNotFound, `{"success":false,"error":"nope"}`)
}

func TestOK(t *testing.T) {
	c, w := newCtx()
	OK(c, map[string]string{"k": "v"})
	assertBody(t, w, http.StatusOK, `{"success":true,"data":{"k":"v"}}`)
}

func TestCreated(t *testing.T) {
	c, w := newCtx()
	Created(c, map[string]int{"id": 1})
	assertBody(t, w, http.StatusCreated, `{"success":true,"data":{"id":1}}`)
}

func TestPaged(t *testing.T) {
	c, w := newCtx()
	Paged(c, []int{1, 2}, &Meta{Page: 1, PerPage: 50})
	assertBody(t, w, http.StatusOK,
		`{"success":true,"data":[1,2],"meta":{"page":1,"per_page":50,"total":0,"total_pages":0}}`)
}

func TestParamUint(t *testing.T) {
	c, _ := newCtx()
	c.Params = gin.Params{{Key: "id", Value: "42"}}
	id, ok := ParamUint(c, "id")
	if !ok || id != 42 {
		t.Fatalf("got (%d, %v), want (42, true)", id, ok)
	}

	c, w := newCtx()
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	if _, ok := ParamUint(c, "id"); ok {
		t.Fatal("expected ok=false for non-numeric param")
	}
	assertBody(t, w, http.StatusBadRequest, `{"success":false,"error":"invalid id"}`)
}

func TestBindJSON(t *testing.T) {
	type body struct {
		Name string `json:"name" binding:"required"`
	}

	c, _ := newCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var ok body
	if !BindJSON(c, &ok) || ok.Name != "x" {
		t.Fatalf("valid bind failed: %+v", ok)
	}

	c, w := newCtx()
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var bad body
	if BindJSON(c, &bad) {
		t.Fatal("expected bind to fail on missing required field")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParsePage(t *testing.T) {
	c, _ := newCtx()
	c.Request = httptest.NewRequest(http.MethodGet, "/?page=3&per_page=10", nil)
	if p := ParsePage(c, 50); p.Page != 3 || p.PerPage != 10 || p.Offset != 20 {
		t.Fatalf("got %+v, want {3 10 20}", p)
	}

	// defaults + clamping: page<1 -> 1, per_page over max -> default
	c, _ = newCtx()
	c.Request = httptest.NewRequest(http.MethodGet, "/?page=0&per_page=999", nil)
	if p := ParsePage(c, 50); p.Page != 1 || p.PerPage != 50 || p.Offset != 0 {
		t.Fatalf("got %+v, want {1 50 0}", p)
	}
}

func TestPageMeta(t *testing.T) {
	p := Page{Page: 2, PerPage: 20}
	if m := p.Meta(45); m.Total != 45 || m.TotalPages != 3 {
		t.Fatalf("got %+v, want total=45 total_pages=3", m)
	}
}
