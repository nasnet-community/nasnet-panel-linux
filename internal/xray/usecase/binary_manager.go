package usecase

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/elf"
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

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

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

// BinaryManager manages xray-core binary downloads and caching.
type BinaryManager struct {
	baseDir     string
	mutexes     sync.Map // map[string]*sync.Mutex keyed by "version:arch"
	httpFactory *httpclient.Factory
}

// NewBinaryManager creates a new BinaryManager with the given base directory
// and outbound HTTP factory (nil → direct internet).
func NewBinaryManager(baseDir string, f *httpclient.Factory) *BinaryManager {
	return &BinaryManager{
		baseDir:     baseDir,
		httpFactory: f,
	}
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

// ListVersions scans the cache directory and returns all cached versions with platform info.
func (bm *BinaryManager) ListVersions() []VersionInfo {
	entries, err := os.ReadDir(bm.baseDir)
	if err != nil {
		return nil
	}

	var versions []VersionInfo
	supportedArchs := []string{"amd64", "arm64"}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "v") {
			continue
		}
		version := strings.TrimPrefix(name, "v")
		if !IsValidVersion(version) {
			continue
		}

		vi := VersionInfo{
			Version:   version,
			Platforms: make(map[string]PlatformInfo),
		}

		for _, arch := range supportedArchs {
			binPath := bm.binaryPath(version, arch)
			info, err := os.Stat(binPath)
			if err != nil {
				continue
			}

			pi := PlatformInfo{
				Cached: true,
				Size:   info.Size(),
			}

			if cs, err := bm.GetChecksum(version, arch); err == nil {
				pi.Checksum = cs
			}

			vi.Platforms[arch] = pi
		}

		if len(vi.Platforms) > 0 {
			versions = append(versions, vi)
		}
	}

	return versions
}

// StoreBinary validates and stores a binary uploaded by an admin.
// It validates the version string and ELF magic bytes, then acquires a per-{version,arch}
// mutex before writing the binary and its SHA256 checksum to disk.
func (bm *BinaryManager) StoreBinary(version, arch string, data []byte) error {
	if !IsValidVersion(version) {
		return fmt.Errorf("invalid version: %s", version)
	}
	if !isELF(data) {
		return fmt.Errorf("invalid binary: not an ELF file")
	}

	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	return bm.storeRaw(version, arch, data)
}

// DownloadFromGitHub downloads the xray binary for the given version and arch from GitHub.
// It acquires a per-{version,arch} mutex, double-checks the cache, downloads the zip,
// extracts the binary, validates ELF, and stores it.
func (bm *BinaryManager) DownloadFromGitHub(version, arch string) error {
	if !IsValidVersion(version) {
		return fmt.Errorf("invalid version: %s", version)
	}

	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	// Double-check cache after acquiring lock
	if bm.IsCached(version, arch) {
		return nil
	}

	url := bm.githubURL(version, arch)
	return bm.downloadAndStore(version, arch, url)
}

// EnsureCached checks if a binary is cached and downloads it if not.
func (bm *BinaryManager) EnsureCached(version, arch string) error {
	if bm.IsCached(version, arch) {
		return nil
	}
	return bm.DownloadFromGitHub(version, arch)
}

// EnsureAndLoad ensures the binary is cached, then reads its bytes + sidecar
// checksum while holding the per-(version,arch) mutex so a concurrent
// DeleteVersion or re-download cannot race between the cache check and the
// read. Returns the raw binary bytes and the SHA256 hex string.
func (bm *BinaryManager) EnsureAndLoad(version, arch string) ([]byte, string, error) {
	if !IsValidVersion(version) {
		return nil, "", fmt.Errorf("invalid version: %s", version)
	}

	mu := bm.getMutex(version, arch)
	mu.Lock()
	defer mu.Unlock()

	if !bm.IsCached(version, arch) {
		if err := bm.downloadAndStore(version, arch, bm.githubURL(version, arch)); err != nil {
			return nil, "", err
		}
	}
	data, err := os.ReadFile(bm.binaryPath(version, arch))
	if err != nil {
		return nil, "", fmt.Errorf("read binary: %w", err)
	}
	cs, err := os.ReadFile(bm.checksumPath(version, arch))
	if err != nil {
		return nil, "", fmt.Errorf("read checksum: %w", err)
	}
	return data, strings.TrimSpace(string(cs)), nil
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

// DeleteVersion removes the version directory from the cache.
func (bm *BinaryManager) DeleteVersion(version string) error {
	if !IsValidVersion(version) {
		return fmt.Errorf("invalid version: %s", version)
	}

	versionDir := filepath.Join(bm.baseDir, "v"+version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("version not cached: %s", version)
	}

	return os.RemoveAll(versionDir)
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
		client = bm.httpFactory.ClientFor(httpclient.FeatureXrayBinary, 5*time.Minute)
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

	xrayBin, err := extractXrayFromZip(body)
	if err != nil {
		return fmt.Errorf("extract from zip: %w", err)
	}

	if !isELF(xrayBin) {
		return fmt.Errorf("extracted binary is not a valid ELF file")
	}

	return bm.storeRaw(version, arch, xrayBin)
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

// DetectELFArch reads the ELF header and maps the machine type to our arch
// label. Lets an uploaded binary be tagged by what it is, not a client hint.
func DetectELFArch(data []byte) (string, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("not a valid ELF binary: %w", err)
	}
	defer f.Close()
	switch f.Machine {
	case elf.EM_X86_64:
		return "amd64", nil
	case elf.EM_AARCH64:
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", f.Machine)
	}
}

// getMutex returns a per-{version,arch} mutex from the sync.Map.
func (bm *BinaryManager) getMutex(version, arch string) *sync.Mutex {
	key := version + ":" + arch
	val, _ := bm.mutexes.LoadOrStore(key, &sync.Mutex{})
	return val.(*sync.Mutex)
}
