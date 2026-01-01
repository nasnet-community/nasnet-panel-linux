package usecase

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func validELF() []byte {
	return append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 100)...)
}

func createTestZip(t *testing.T, filename string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	zw.Close()
	return buf.Bytes()
}

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"simple version", "1.8.4", true},
		{"single digit", "1", true},
		{"two digits", "1.8", true},
		{"four segments", "1.8.4.1", true},
		{"with prerelease", "1.8.4-beta1", true},
		{"with prerelease alpha", "1.8.4-alpha", true},
		{"empty string", "", false},
		{"v prefix", "v1.8.4", false},
		{"path traversal dots", "../etc/passwd", false},
		{"path traversal slash", "1.8.4/../../etc", false},
		{"too long", "12345678901234567890x", false},
		{"exactly 20 chars", "12345678901234567890", true},
		{"special chars", "1.8.4@bad", false},
		{"spaces", "1.8 .4", false},
		{"uppercase prerelease", "1.8.4-Beta", false},
		{"leading zero ok", "01.02.03", true},
		{"double dash", "1.8.4--beta", false},
		{"trailing dash", "1.8.4-", false},
		{"trailing dot", "1.8.4.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidVersion(tt.version)
			if got != tt.want {
				t.Errorf("IsValidVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestBinaryPath(t *testing.T) {
	bm := NewBinaryManager("/tmp/xray-test", nil)
	got := bm.binaryPath("1.8.4", "amd64")
	want := "/tmp/xray-test/v1.8.4/xray-linux-amd64"
	if got != want {
		t.Errorf("binaryPath() = %q, want %q", got, want)
	}
}

func TestChecksumPath(t *testing.T) {
	bm := NewBinaryManager("/tmp/xray-test", nil)
	got := bm.checksumPath("1.8.4", "arm64")
	want := "/tmp/xray-test/v1.8.4/xray-linux-arm64.sha256"
	if got != want {
		t.Errorf("checksumPath() = %q, want %q", got, want)
	}
}

func TestGitHubURL(t *testing.T) {
	bm := NewBinaryManager("/tmp/xray-test", nil)

	tests := []struct {
		name    string
		version string
		arch    string
		want    string
	}{
		{
			"amd64",
			"1.8.4",
			"amd64",
			"https://github.com/XTLS/Xray-core/releases/download/v1.8.4/Xray-linux-64.zip",
		},
		{
			"arm64",
			"1.8.4",
			"arm64",
			"https://github.com/XTLS/Xray-core/releases/download/v1.8.4/Xray-linux-arm64-v8a.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bm.githubURL(tt.version, tt.arch)
			if got != tt.want {
				t.Errorf("githubURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsCached(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Not cached initially
	if bm.IsCached("1.8.4", "amd64") {
		t.Error("expected not cached initially")
	}

	// Create the binary file
	binPath := bm.binaryPath("1.8.4", "amd64")
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, validELF(), 0755); err != nil {
		t.Fatal(err)
	}

	// Now it should be cached
	if !bm.IsCached("1.8.4", "amd64") {
		t.Error("expected cached after creating file")
	}
}

func TestGetChecksum(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Error when no checksum file
	_, err := bm.GetChecksum("1.8.4", "amd64")
	if err == nil {
		t.Error("expected error when checksum file does not exist")
	}

	// Create a checksum file
	csPath := bm.checksumPath("1.8.4", "amd64")
	if err := os.MkdirAll(filepath.Dir(csPath), 0755); err != nil {
		t.Fatal(err)
	}
	expectedChecksum := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if err := os.WriteFile(csPath, []byte(expectedChecksum), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := bm.GetChecksum("1.8.4", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expectedChecksum {
		t.Errorf("GetChecksum() = %q, want %q", got, expectedChecksum)
	}
}

func TestGetBinaryPath(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Error when not cached
	_, err := bm.GetBinaryPath("1.8.4", "amd64")
	if err == nil {
		t.Error("expected error when binary not cached")
	}

	// Create the binary file
	binPath := bm.binaryPath("1.8.4", "amd64")
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, validELF(), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := bm.GetBinaryPath("1.8.4", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binPath {
		t.Errorf("GetBinaryPath() = %q, want %q", got, binPath)
	}
}

func TestListVersions(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Empty dir returns empty list
	versions := bm.ListVersions()
	if len(versions) != 0 {
		t.Errorf("expected empty list, got %d items", len(versions))
	}

	// Add a version with amd64
	elf := validELF()
	if err := bm.StoreBinary("1.8.4", "amd64", elf); err != nil {
		t.Fatal(err)
	}

	versions = bm.ListVersions()
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].Version != "1.8.4" {
		t.Errorf("expected version 1.8.4, got %s", versions[0].Version)
	}

	pi, ok := versions[0].Platforms["amd64"]
	if !ok {
		t.Fatal("expected amd64 platform")
	}
	if !pi.Cached {
		t.Error("expected amd64 to be cached")
	}
	if pi.Size != int64(len(elf)) {
		t.Errorf("expected size %d, got %d", len(elf), pi.Size)
	}
	if pi.Checksum == "" {
		t.Error("expected non-empty checksum")
	}
}

func TestStoreBinary(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	elf := validELF()
	err := bm.StoreBinary("1.8.4", "amd64", elf)
	if err != nil {
		t.Fatalf("StoreBinary() error: %v", err)
	}

	// Verify cached
	if !bm.IsCached("1.8.4", "amd64") {
		t.Error("expected cached after store")
	}

	// Verify checksum
	h := sha256.Sum256(elf)
	expectedChecksum := hex.EncodeToString(h[:])
	gotChecksum, err := bm.GetChecksum("1.8.4", "amd64")
	if err != nil {
		t.Fatalf("GetChecksum() error: %v", err)
	}
	if gotChecksum != expectedChecksum {
		t.Errorf("checksum mismatch: got %q, want %q", gotChecksum, expectedChecksum)
	}

	// Verify binary content
	binPath := bm.binaryPath("1.8.4", "amd64")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !bytes.Equal(data, elf) {
		t.Error("binary content mismatch")
	}
}

func TestStoreBinary_InvalidELF(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	err := bm.StoreBinary("1.8.4", "amd64", []byte("not an elf binary"))
	if err == nil {
		t.Error("expected error for non-ELF data")
	}

	// Verify NOT cached
	if bm.IsCached("1.8.4", "amd64") {
		t.Error("expected not cached after invalid store")
	}
}

func TestStoreBinary_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	err := bm.StoreBinary("../etc/passwd", "amd64", validELF())
	if err == nil {
		t.Error("expected error for path traversal version")
	}
}

func TestDownloadAndStore(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	elf := validELF()
	zipData := createTestZip(t, "xray", elf)

	// Start a test HTTP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	defer srv.Close()

	// Use the internal downloadAndStore with test URL
	err := bm.downloadAndStore("1.8.4", "amd64", srv.URL+"/test.zip")
	if err != nil {
		t.Fatalf("downloadAndStore() error: %v", err)
	}

	// Verify cached
	if !bm.IsCached("1.8.4", "amd64") {
		t.Error("expected cached after download")
	}

	// Verify the stored content is the ELF from inside the zip
	binPath := bm.binaryPath("1.8.4", "amd64")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !bytes.Equal(data, elf) {
		t.Error("binary content mismatch")
	}
}

func TestDownloadAndStore_NotELF(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	zipData := createTestZip(t, "xray", []byte("not an ELF"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	defer srv.Close()

	err := bm.downloadAndStore("1.8.4", "amd64", srv.URL+"/test.zip")
	if err == nil {
		t.Error("expected error for non-ELF content in zip")
	}

	if bm.IsCached("1.8.4", "amd64") {
		t.Error("expected not cached after failed download")
	}
}

func TestDownloadAndStore_ServerDown(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Use an unreachable URL
	err := bm.downloadAndStore("1.8.4", "amd64", "http://127.0.0.1:1/nonexistent.zip")
	if err == nil {
		t.Error("expected error when server is unreachable")
	}
}

func TestDeleteVersion(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	// Store a binary first
	if err := bm.StoreBinary("1.8.4", "amd64", validELF()); err != nil {
		t.Fatal(err)
	}

	// Verify cached
	if !bm.IsCached("1.8.4", "amd64") {
		t.Fatal("expected cached before delete")
	}

	// Delete
	err := bm.DeleteVersion("1.8.4")
	if err != nil {
		t.Fatalf("DeleteVersion() error: %v", err)
	}

	// Verify uncached
	if bm.IsCached("1.8.4", "amd64") {
		t.Error("expected not cached after delete")
	}
}

func TestDeleteVersion_NonExistent(t *testing.T) {
	dir := t.TempDir()
	bm := NewBinaryManager(dir, nil)

	err := bm.DeleteVersion("9.9.9")
	if err == nil {
		t.Error("expected error for non-existent version")
	}
}
