package handler

import (
	"crypto/sha256"
	"encoding/hex"
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

// fakeELF is the test's "pinned build": an ELF header plus a payload.
var fakeELF = append([]byte{0x7f, 'E', 'L', 'F'}, []byte("testpayload")...)

func fakeSum() string { h := sha256.Sum256(fakeELF); return hex.EncodeToString(h[:]) }

// setupTestHandler pins 26.2.6 to fakeELF and returns a deployment token for the routes.
func setupTestHandler(t *testing.T) (*XrayHandler, string, string) {
	t.Helper()
	dir := t.TempDir()
	bm := usecase.NewPinnedBinaryManager(dir, nil, "26.2.6",
		map[string]string{"amd64": "unused-when-cached", "arm64": "unused-when-cached"},
		map[string]string{"amd64": fakeSum(), "arm64": fakeSum()})
	tm := auth.NewTokenManager("test-secret")
	tok, err := tm.GenerateDeploymentToken(1, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return NewXrayHandler(bm, tm), dir, tok
}

// cacheTestBinary writes the pinned fake binary and its checksum into the cache dir.
func cacheTestBinary(t *testing.T, dir, version, arch string) []byte {
	t.Helper()
	versionDir := filepath.Join(dir, "v"+version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(versionDir, "xray-linux-"+arch)
	if err := os.WriteFile(binPath, fakeELF, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath+".sha256", []byte(fakeSum()), 0644); err != nil {
		t.Fatal(err)
	}
	return fakeELF
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

func TestGetXrayBinary_AnotherVersionFromItsVerifiedCache(t *testing.T) {
	h, dir, tok := setupTestHandler(t)
	// an earlier update verified 26.1.1 and left it with its sidecar
	data := cacheTestBinary(t, dir, "26.1.1", "amd64")

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?version=26.1.1&arch=amd64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK || w.Body.Len() != len(data) {
		t.Fatalf("expected the cached 26.1.1, got %d: %s", w.Code, w.Body.String())
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

func TestGetXrayBinary_NoVersionMeansThePin(t *testing.T) {
	h, dir, tok := setupTestHandler(t)
	cacheTestBinary(t, dir, "26.2.6", "arm64")

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/binary", h.GetXrayBinary)
	c.Request = httptest.NewRequest(http.MethodGet, "/binary?arch=arm64&token="+tok, nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK || w.Header().Get("X-Binary-SHA256") != fakeSum() {
		t.Fatalf("got %d, sha %q: %s", w.Code, w.Header().Get("X-Binary-SHA256"), w.Body.String())
	}
}
