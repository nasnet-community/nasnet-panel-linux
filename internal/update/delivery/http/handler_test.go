package http

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
)

func testRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	uc := usecase.New(usecase.Deps{Paths: usecase.Paths{Root: t.TempDir()}, Version: "v1.0.0"})
	h := NewHandler(uc, nil)
	r := gin.New()
	h.RegisterHealth(r)
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func TestHealthAnswersLoopbackOnly(t *testing.T) {
	r := testRouter(t)
	for addr, want := range map[string]int{"127.0.0.1:5000": http.StatusOK, "[::1]:5000": http.StatusOK, "203.0.113.7:5000": http.StatusNotFound} {
		req := httptest.NewRequest(http.MethodGet, "/health/update", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%s: %d, want %d", addr, w.Code, want)
		}
		if want == http.StatusOK {
			var h usecase.Health
			if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil || h.Version != "v1.0.0" {
				t.Errorf("%s: %s", addr, w.Body)
			}
		}
	}
}

func TestUploadRefusesWhatIsNotABundle(t *testing.T) {
	r := testRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upload", bytes.NewReader([]byte("x")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no multipart: %d", w.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	f, _ := mw.CreateFormFile("file", "bundle.tar.gz")
	f.Write([]byte("not a bundle"))
	mw.Close()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/updates/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("garbage: %d %s", w.Code, w.Body)
	}
}

func TestApplyNeedsSomethingToInstall(t *testing.T) {
	r := testRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/apply", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
