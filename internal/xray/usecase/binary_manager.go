package usecase

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// CacheDir holds every downloaded xray build, relative to the install root.
const CacheDir = "bin/xray"

var versionRegex = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*(-[a-z0-9]+)*$`)

// PlatformInfo contains information about a cached binary for a specific platform.
type PlatformInfo struct {
	Cached   bool   `json:"cached"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

// VersionInfo contains information about a cached xray version and its platforms.
type VersionInfo struct {
	Version   string                  `json:"version"`
	IsDefault bool                    `json:"is_default"`
	Platforms map[string]PlatformInfo `json:"platforms"`
}

// BinaryManager fetches, verifies and caches xray-core releases; the pinned one is the default.
type BinaryManager struct {
	baseDir     string
	mutexes     sync.Map // map[string]*sync.Mutex keyed by "version:arch"
	httpFactory *httpclient.Factory
	pin         pinned
	releaseURL  func(version, arch string) string // github by default; tests point it at a fake
}

// pinned is the default release, checked against hashes in code (tests swap it).
type pinned struct {
	Version  string
	Zip, Bin map[string]string // per arch: release zip hash, extracted binary hash
}

// NewBinaryManager creates a new BinaryManager with the given base directory
// and outbound HTTP factory (nil → direct internet).
func NewBinaryManager(baseDir string, f *httpclient.Factory) *BinaryManager {
	return NewPinnedBinaryManager(baseDir, f, pin.Version, pin.ZipSHA256, pin.BinarySHA256)
}

// NewPinnedBinaryManager pins another release; production uses NewBinaryManager.
func NewPinnedBinaryManager(baseDir string, f *httpclient.Factory, version string, zipSHA, binSHA map[string]string) *BinaryManager {
	bm := &BinaryManager{
		baseDir:     baseDir,
		httpFactory: f,
		pin:         pinned{Version: version, Zip: zipSHA, Bin: binSHA},
	}
	bm.releaseURL = bm.githubURL
	return bm
}

// Pinned is the xray-core version new nodes get.
func (bm *BinaryManager) Pinned() string { return bm.pin.Version }

func (bm *BinaryManager) checkVersion(version, arch string) error {
	if !IsValidVersion(version) {
		return fmt.Errorf("invalid version: %s", version)
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("xray: no %s build", arch)
	}
	if version == bm.pin.Version && (bm.pin.Zip[arch] == "" || bm.pin.Bin[arch] == "") {
		return fmt.Errorf("xray: no pinned %s build", arch)
	}
	return nil
}

// IsValidVersion validates a version string.
// It must match the pattern ^[0-9]+(\.[0-9]+)*(-[a-z0-9]+)*$, be at most 20 chars,
// and must NOT have a "v" prefix.
func IsValidVersion(v string) bool {
	if len(v) == 0 || len(v) > 20 {
		return false
	}
	if strings.HasPrefix(v, "v") {
		return false
	}
	return versionRegex.MatchString(v)
}

// binaryPath returns the filesystem path for a binary.
func (bm *BinaryManager) binaryPath(version, arch string) string {
	return filepath.Join(bm.baseDir, "v"+version, "xray-linux-"+arch)
}

// checksumPath returns the filesystem path for a binary's SHA256 checksum file.
func (bm *BinaryManager) checksumPath(version, arch string) string {
	return bm.binaryPath(version, arch) + ".sha256"
}

// githubURL constructs the GitHub release download URL for a given version and arch.
func (bm *BinaryManager) githubURL(version, arch string) string {
	var archSuffix string
	switch arch {
	case "arm64":
		archSuffix = "arm64-v8a"
	default: // amd64
		archSuffix = "64"
	}
	return fmt.Sprintf(
		"https://github.com/XTLS/Xray-core/releases/download/v%s/Xray-linux-%s.zip",
		version, archSuffix,
	)
}

// IsCached returns true if the binary file exists on disk.
func (bm *BinaryManager) IsCached(version, arch string) bool {
	_, err := os.Stat(bm.binaryPath(version, arch))
	return err == nil
}

// GetBinaryPath returns the path to a cached binary or an error if not cached.
func (bm *BinaryManager) GetBinaryPath(version, arch string) (string, error) {
	p := bm.binaryPath(version, arch)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("binary not cached: %s %s", version, arch)
	}
	return p, nil
}

// GetChecksum returns the stored SHA256 checksum string for a binary.
func (bm *BinaryManager) GetChecksum(version, arch string) (string, error) {
	data, err := os.ReadFile(bm.checksumPath(version, arch))
	if err != nil {
		return "", fmt.Errorf("checksum not found: %s %s: %w", version, arch, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// DownloadFromGitHub downloads the xray binary for the given version and arch from GitHub.
// It acquires a per-{version,arch} mutex, double-checks the cache, downloads the zip,
// extracts the binary, validates ELF, and stores it.
func (bm *BinaryManager) DownloadFromGitHub(version, arch string) error {
	if err := bm.checkVersion(version, arch); err != nil {
		return err
	}

	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	// Double-check cache after acquiring lock
	if bm.IsCached(version, arch) {
		return nil
	}

	return bm.downloadAndStore(version, arch, bm.releaseURL(version, arch))
}

// EnsureCached checks if a binary is cached and downloads it if not.
func (bm *BinaryManager) EnsureCached(version, arch string) error {
	_, _, err := bm.EnsureAndLoad(version, arch)
	return err
}

// EnsureAndLoad ensures the binary is cached, then reads its bytes + sidecar
// checksum while holding the per-(version,arch) mutex so a concurrent
// DeleteVersion or re-download cannot race between the cache check and the
// read. Returns the raw binary bytes and the SHA256 hex string.
func (bm *BinaryManager) EnsureAndLoad(version, arch string) ([]byte, string, error) {
	if err := bm.checkVersion(version, arch); err != nil {
		return nil, "", err
	}

	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	// a cached copy is served only if it is still what was verified: the pin, or its own sidecar
	if data, err := os.ReadFile(bm.binaryPath(version, arch)); err == nil && sha256Hex(data) == bm.expectedBinary(version, arch) {
		return data, sha256Hex(data), nil
	}
	if err := bm.downloadAndStore(version, arch, bm.releaseURL(version, arch)); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(bm.binaryPath(version, arch))
	if err != nil {
		return nil, "", fmt.Errorf("read binary: %w", err)
	}
	if sum := sha256Hex(data); sum != bm.expectedBinary(version, arch) {
		return nil, "", fmt.Errorf("xray: the cached %s %s build does not match its checksum", version, arch)
	}
	return data, sha256Hex(data), nil
}

// VerifiedPath is the absolute path of a cached build that still matches its hash.
func (bm *BinaryManager) VerifiedPath(version, arch string) (string, error) {
	if err := bm.checkVersion(version, arch); err != nil {
		return "", err
	}
	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	p := bm.binaryPath(version, arch)
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("xray %s %s is not cached: %w", version, arch, err)
	}
	if want := bm.expectedBinary(version, arch); want == "" || sha256Hex(data) != want {
		return "", fmt.Errorf("xray: the cached %s %s build does not match its checksum", version, arch)
	}
	return filepath.Abs(p)
}

// PruneCache removes every cached version except keep and the tested one.
func (bm *BinaryManager) PruneCache(keep ...string) ([]string, error) {
	entries, err := os.ReadDir(bm.baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	want := map[string]bool{bm.pin.Version: true}
	for _, v := range keep {
		want[strings.TrimPrefix(v, "v")] = true
	}
	var removed []string
	for _, e := range entries {
		v, ok := strings.CutPrefix(e.Name(), "v")
		if !e.IsDir() || !ok || !IsValidVersion(v) || want[v] {
			continue
		}
		if err := bm.removeVersion(v); err != nil {
			return removed, err
		}
		removed = append(removed, v)
	}
	return removed, nil
}

func (bm *BinaryManager) removeVersion(version string) error {
	for _, arch := range []string{"amd64", "arm64"} {
		mu := bm.getMutex(version, arch)
		mu.Lock()
		defer mu.Unlock()
	}
	return os.RemoveAll(filepath.Join(bm.baseDir, "v"+version))
}

// expectedBinary is the pinned hash for the pinned release, else the sidecar written when it was verified.
func (bm *BinaryManager) expectedBinary(version, arch string) string {
	if version == bm.pin.Version {
		return bm.pin.Bin[arch]
	}
	cs, err := bm.GetChecksum(version, arch)
	if err != nil {
		return ""
	}
	return cs
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// PrefetchVersion spawns goroutines to download both amd64 and arm64 binaries.
func (bm *BinaryManager) PrefetchVersion(version string) {
	log := logger.GetLogger()
	for _, arch := range []string{"amd64", "arm64"} {
		go func(a string) {
			if err := bm.EnsureCached(version, a); err != nil {
				log.WithField("version", version).
					WithField("arch", a).
					WithError(err).
					Warn("prefetch failed")
			}
		}(arch)
	}
}

// storeRaw writes the binary and its SHA256 checksum to disk atomically via
// tmp file + rename so a crash mid-write cannot leave a truncated binary in
// the cache. The caller MUST hold the per-{version,arch} mutex.
func (bm *BinaryManager) storeRaw(version, arch string, data []byte) error {
	binPath := bm.binaryPath(version, arch)
	dir := filepath.Dir(binPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := writeFileAtomic(binPath, data, 0755); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}

	h := sha256.Sum256(data)
	checksum := hex.EncodeToString(h[:])
	csPath := bm.checksumPath(version, arch)
	if err := writeFileAtomic(csPath, []byte(checksum), 0644); err != nil {
		// Roll back the binary so a missing checksum sidecar never leaves
		// an unverifiable binary behind.
		_ = os.Remove(binPath)
		return fmt.Errorf("write checksum: %w", err)
	}

	return nil
}

// writeFileAtomic writes data to a tmp file in the same directory and then
// renames it over the target path. Rename within a single filesystem is
// atomic on POSIX; readers see either the old file or the new file, never
// a partial write.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// downloadAndStore downloads a zip from the given URL, extracts the xray binary,
// validates it as ELF, and stores it using storeRaw.
// The caller MUST hold the per-{version,arch} mutex.
func (bm *BinaryManager) downloadAndStore(version, arch, url string) error {
	var client *http.Client
	if bm.httpFactory != nil {
		client = bm.httpFactory.ClientFor(httpclient.FeatureXrayBinary, httpclient.EgressForeign, 5*time.Minute)
	} else {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download from %s: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	want := bm.pin.Zip[arch]
	if version != bm.pin.Version {
		// no pin for this release: check XTLS's published digest instead
		if want, err = fetchDigest(client, url+".dgst"); err != nil {
			return err
		}
	}
	if got := sha256Hex(body); got != want {
		return fmt.Errorf("xray: %s failed its checksum (sha256 %s)", url, got)
	}

	xrayBin, err := extractXrayFromZip(body)
	if err != nil {
		return fmt.Errorf("extract from zip: %w", err)
	}

	if !isELF(xrayBin) {
		return fmt.Errorf("extracted binary is not a valid ELF file")
	}
	if version == bm.pin.Version && sha256Hex(xrayBin) != bm.pin.Bin[arch] {
		return fmt.Errorf("xray: the binary in %s is not the pinned build", url)
	}

	return bm.storeRaw(version, arch, xrayBin)
}

var digestRe = regexp.MustCompile(`(?i)sha2?-?256\s*=\s*([0-9a-f]{64})`)

// fetchDigest reads the SHA2-256 line of a release's .dgst file.
func fetchDigest(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("xray: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("xray: fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	m := digestRe.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("xray: %s has no SHA2-256 line", url)
	}
	return strings.ToLower(string(m[1])), nil
}

// extractXrayFromZip extracts the "xray" file from a zip archive.
func extractXrayFromZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	for _, f := range r.File {
		if f.Name == "xray" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open file in zip: %w", err)
			}
			defer rc.Close()

			content, err := io.ReadAll(io.LimitReader(rc, 200<<20))
			if err != nil {
				return nil, fmt.Errorf("read file from zip: %w", err)
			}
			return content, nil
		}
	}

	return nil, fmt.Errorf("xray binary not found in zip archive")
}

// isELF checks if the data starts with the ELF magic bytes.
func isELF(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F'
}

// getMutex returns a per-{version,arch} mutex from the sync.Map.
func (bm *BinaryManager) getMutex(version, arch string) *sync.Mutex {
	key := version + ":" + arch
	val, _ := bm.mutexes.LoadOrStore(key, &sync.Mutex{})
	return val.(*sync.Mutex)
}
