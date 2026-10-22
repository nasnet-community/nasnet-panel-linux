package usecase

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// pinnedFake is a manager pinned to a fake release served by a test server,
// which also publishes a .dgst for it the way XTLS does.
func pinnedFake(t *testing.T, version string, zipData, bin []byte) (*BinaryManager, *int) {
	t.Helper()
	hits := 0
	dgst := "SHA2-256= " + sha256Hex(zipData) + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".dgst") {
			w.Write([]byte(dgst))
			return
		}
		hits++
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	t.Cleanup(srv.Close)
	bm := NewPinnedBinaryManager(t.TempDir(), nil, version,
		map[string]string{"amd64": sha256Hex(zipData)}, map[string]string{"amd64": sha256Hex(bin)})
	bm.releaseURL = func(string, string) string { return srv.URL + "/xray.zip" }
	return bm, &hits
}

func TestThePinnedReleaseIsDownloadedVerifiedAndCached(t *testing.T) {
	elf := validELF()
	bm, hits := pinnedFake(t, "26.9.30", createTestZip(t, "xray", elf), elf)
	for i := 0; i < 2; i++ {
		data, sum, err := bm.EnsureAndLoad("26.9.30", "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, elf) || sum != sha256Hex(elf) {
			t.Fatalf("got %d bytes, sum %s", len(data), sum)
		}
	}
	if *hits != 1 {
		t.Fatalf("downloaded %d times, want once then the cache", *hits)
	}
}

func TestAnInvalidVersionOrArchIsNeverFetched(t *testing.T) {
	elf := validELF()
	bm, hits := pinnedFake(t, "26.9.30", createTestZip(t, "xray", elf), elf)
	for _, v := range []string{"../etc/passwd", "", "v26.9.30"} {
		if _, _, err := bm.EnsureAndLoad(v, "amd64"); err == nil {
			t.Errorf("version %q was served", v)
		}
	}
	if _, _, err := bm.EnsureAndLoad("26.9.30", "riscv64"); err == nil {
		t.Error("an unknown arch was served")
	}
	if *hits != 0 {
		t.Fatalf("bad requests downloaded %d times", *hits)
	}
}

func TestAnotherReleaseIsCheckedAgainstItsPublishedDigest(t *testing.T) {
	elf := validELF()
	zipData := createTestZip(t, "xray", elf)
	bm, _ := pinnedFake(t, "26.9.30", zipData, elf)
	data, sum, err := bm.EnsureAndLoad("26.7.28", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, elf) || sum != sha256Hex(elf) {
		t.Fatalf("got %d bytes, sum %s", len(data), sum)
	}

	// a digest that disagrees with the zip refuses it
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".dgst") {
			w.Write([]byte("SHA2-256= " + sha256Hex([]byte("other")) + "\n"))
			return
		}
		w.Write(zipData)
	}))
	defer bad.Close()
	bm2 := NewPinnedBinaryManager(t.TempDir(), nil, "26.9.30", map[string]string{"amd64": "x"}, map[string]string{"amd64": "y"})
	bm2.releaseURL = func(string, string) string { return bad.URL + "/xray.zip" }
	if _, _, err := bm2.EnsureAndLoad("26.7.28", "amd64"); err == nil {
		t.Fatal("a zip that fails its published digest was accepted")
	}
	if bm2.IsCached("26.7.28", "amd64") {
		t.Fatal("a refused download was cached")
	}
}

func TestAReleaseThatIsNotThePinnedOneIsRefused(t *testing.T) {
	elf := validELF()
	// the zip on the wire differs from the pinned one
	bm, _ := pinnedFake(t, "26.9.30", createTestZip(t, "xray", elf), elf)
	bm.pin.Zip["amd64"] = sha256Hex([]byte("another zip"))
	if _, _, err := bm.EnsureAndLoad("26.9.30", "amd64"); err == nil {
		t.Fatal("a zip that is not the pinned release was accepted")
	}
	if bm.IsCached("26.9.30", "amd64") {
		t.Fatal("a refused download was cached")
	}

	// the zip matches but the binary inside is not the pinned build
	bm2, _ := pinnedFake(t, "26.9.30", createTestZip(t, "xray", elf), []byte("pinned build"))
	if _, _, err := bm2.EnsureAndLoad("26.9.30", "amd64"); err == nil {
		t.Fatal("a binary that is not the pinned build was accepted")
	}
}

func TestAStaleCacheIsReplacedByTheVerifiedRelease(t *testing.T) {
	elf := validELF()
	bm, hits := pinnedFake(t, "26.9.30", createTestZip(t, "xray", elf), elf)
	// an old upload sits where the pinned build belongs, with a matching sidecar
	stale := append(validELF(), []byte("uploaded")...)
	if err := bm.storeRaw("26.9.30", "amd64", stale); err != nil {
		t.Fatal(err)
	}
	data, sum, err := bm.EnsureAndLoad("26.9.30", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, elf) || sum != sha256Hex(elf) || *hits != 1 {
		t.Fatalf("served %d bytes (sum %s) after %d downloads", len(data), sum, *hits)
	}
	if cs, _ := bm.GetChecksum("26.9.30", "amd64"); cs != sha256Hex(elf) {
		t.Fatalf("sidecar = %s", cs)
	}
}

func TestDownloadRefusesAZipWithoutAnELF(t *testing.T) {
	zipData := createTestZip(t, "xray", []byte("not an ELF"))
	bm, _ := pinnedFake(t, "26.9.30", zipData, []byte("not an ELF"))
	if _, _, err := bm.EnsureAndLoad("26.9.30", "amd64"); err == nil {
		t.Fatal("a non-ELF binary was accepted")
	}
}

func TestDownloadAndStore_ServerDown(t *testing.T) {
	bm := NewPinnedBinaryManager(t.TempDir(), nil, "26.9.30", map[string]string{"amd64": "x"}, map[string]string{"amd64": "y"})
	if err := bm.downloadAndStore("26.9.30", "amd64", "http://127.0.0.1:1/nonexistent.zip"); err == nil {
		t.Error("expected error when server is unreachable")
	}
}

func TestTheShippedPinIsWellFormed(t *testing.T) {
	bm := NewBinaryManager(t.TempDir(), nil)
	if !IsValidVersion(bm.Pinned()) {
		t.Fatalf("pinned version %q", bm.Pinned())
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for name, m := range map[string]map[string]string{"zip": bm.pin.Zip, "binary": bm.pin.Bin} {
			if h := m[arch]; len(h) != 64 || strings.Trim(h, "0123456789abcdef") != "" {
				t.Errorf("%s %s hash %q", arch, name, h)
			}
		}
	}
}

func TestVerifiedPathOnlyServesAnUntouchedBuild(t *testing.T) {
	elf := validELF()
	dir := t.TempDir()
	bm := NewPinnedBinaryManager(dir, nil, "26.9.30", map[string]string{"amd64": "x"}, map[string]string{"amd64": sha256Hex(elf)})
	if _, err := bm.VerifiedPath("26.9.30", "amd64"); err == nil {
		t.Fatal("a version that isn't cached got a path")
	}
	for _, v := range []string{"26.9.30", "26.11.1"} {
		if err := bm.storeRaw(v, "amd64", elf); err != nil {
			t.Fatal(err)
		}
		p, err := bm.VerifiedPath(v, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(p) || filepath.Base(filepath.Dir(p)) != "v"+v {
			t.Fatalf("path = %s", p)
		}
	}

	// the pinned build is checked against the hash in code, not its sidecar
	tampered := append(validELF(), 'x')
	if err := bm.storeRaw("26.9.30", "amd64", tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := bm.VerifiedPath("26.9.30", "amd64"); err == nil {
		t.Fatal("a changed pinned build got a path")
	}
	// any other build is checked against its sidecar
	if err := os.WriteFile(bm.binaryPath("26.11.1", "amd64"), tampered, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bm.VerifiedPath("26.11.1", "amd64"); err == nil {
		t.Fatal("a build that no longer matches its sidecar got a path")
	}
}

func TestPruneCacheKeepsTheTestedAndTheKeptVersions(t *testing.T) {
	dir := t.TempDir()
	bm := NewPinnedBinaryManager(dir, nil, "26.10.10", map[string]string{}, map[string]string{})
	for _, v := range []string{"26.9.30", "26.10.10", "26.11.1", "25.1.1"} {
		if err := bm.storeRaw(v, "amd64", validELF()); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := bm.PruneCache("v26.9.30", "26.11.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "25.1.1" {
		t.Fatalf("removed %v", removed)
	}
	for _, v := range []string{"26.9.30", "26.10.10", "26.11.1"} {
		if !bm.IsCached(v, "amd64") {
			t.Fatalf("%s was pruned", v)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes")); err != nil {
		t.Fatal("a directory that isn't a version was removed")
	}
	if removed, err := NewBinaryManager(filepath.Join(dir, "missing"), nil).PruneCache(); err != nil || len(removed) != 0 {
		t.Fatalf("missing cache: %v, %v", removed, err)
	}
}

func TestImportOnlyTakesTheBuildItWasToldAbout(t *testing.T) {
	bm := NewPinnedBinaryManager(t.TempDir(), nil, "26.10.10", map[string]string{}, map[string]string{})
	elf := validELF()
	if err := bm.Import("26.11.1", "amd64", elf, sha256Hex([]byte("other"))); err == nil {
		t.Fatal("a build with another hash was imported")
	}
	if err := bm.Import("26.11.1", "amd64", []byte("not elf"), sha256Hex([]byte("not elf"))); err == nil {
		t.Fatal("a non-ELF file was imported")
	}
	if err := bm.Import("26.11.1", "amd64", elf, strings.ToUpper(sha256Hex(elf))); err != nil {
		t.Fatal(err)
	}
	if _, err := bm.VerifiedPath("26.11.1", "amd64"); err != nil {
		t.Fatal(err)
	}
}
