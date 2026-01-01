package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestHandler creates a BinaryManager with a temp directory and an
// XrayHandler. Returns the handler, the cache dir, and a valid deployment
// token (since the public routes now require one).
func setupTestHandler(t *testing.T) (*XrayHandler, string, string) {
	t.Helper()
	dir := t.TempDir()
	bm := usecase.NewBinaryManager(dir, nil)
	tm := auth.NewTokenManager("test-secret")
	tok, err := tm.GenerateDeploymentToken(1, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h := NewXrayHandler(bm, nil, tm, nil)
	return h, dir, tok
}

// cacheTestBinary writes a fake ELF binary and its checksum into the cache dir.
func cacheTestBinary(t *testing.T, dir, version, arch string) []byte {
	t.Helper()
	// ELF magic bytes + some payload
	data := append([]byte{0x7f, 'E', 'L', 'F'}, []byte("testpayload")...)

	versionDir := filepath.Join(dir, "v"+version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(versionDir, "xray-linux-"+arch)
	if err := os.WriteFile(binPath, data, 0755); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(data)
	checksum := hex.EncodeToString(h[:])
	csPath := binPath + ".sha256"
	if err := os.WriteFile(csPath, []byte(checksum), 0644); err != nil {
		t.Fatal(err)
	}

	return data
}

func TestGetXrayBinary(t *testing.T) {
	h, dir, tok := setupTestHandler(t)
	data := cacheTestBinary(t, dir, "26.2.6", "amd64")

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?version=26.2.6&arch=amd64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.Len() != len(data) {
		t.Fatalf("expected body length %d, got %d", len(data), w.Body.Len())
	}
}

func TestGetXrayBinary_NotCached(t *testing.T) {
	h, _, tok := setupTestHandler(t)

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?version=99.99.99&arch=amd64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatal("expected error field in response")
	}
}

func TestGetXrayChecksum(t *testing.T) {
	h, dir, tok := setupTestHandler(t)
	data := cacheTestBinary(t, dir, "26.2.6", "amd64")

	expectedHash := sha256.Sum256(data)
	expectedChecksum := hex.EncodeToString(expectedHash[:])

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/checksum", h.GetXrayChecksum)
	c.Request = httptest.NewRequest(http.MethodGet, "/checksum?version=26.2.6&arch=amd64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.String() != expectedChecksum {
		t.Fatalf("expected checksum %s, got %s", expectedChecksum, w.Body.String())
	}
}

func TestGetXrayBinary_InvalidVersion(t *testing.T) {
	h, _, tok := setupTestHandler(t)

	// Path traversal attempt
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?version=../../../etc/passwd&arch=amd64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetXrayBinary_MissingToken(t *testing.T) {
	h, _, _ := setupTestHandler(t)

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?version=26.2.6&arch=amd64", nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
