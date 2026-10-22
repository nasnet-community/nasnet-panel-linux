package usecase

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

type fakeSystem struct {
	h        *harness
	mu       sync.Mutex
	calls    []string
	starts   int
	crashes  int // NRestarts added by each restart
	sysError error
}

func (s *fakeSystem) note(c string) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
}

func (s *fakeSystem) StartRunner(_ context.Context, id string) error {
	s.note("runner")
	go s.h.runRunner(id)
	return nil
}

func (s *fakeSystem) UpdateSystem(_ context.Context, script string) error {
	s.note("update-system")
	if s.sysError != nil {
		return s.sysError
	}
	tool, _ := os.ReadFile(script)
	for _, f := range s.h.sysFiles {
		if err := os.WriteFile(f, []byte("unit from "+string(tool)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s *fakeSystem) DaemonReload(context.Context) error { s.note("daemon-reload"); return nil }

func (s *fakeSystem) Restart(context.Context) error {
	s.note("restart")
	s.mu.Lock()
	s.starts += s.crashes
	s.mu.Unlock()
	return nil
}

func (s *fakeSystem) Starts(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts, nil
}

type fakeBackups struct {
	dir  string
	busy bool
}

func (b *fakeBackups) TryLock() bool     { return !b.busy }
func (b *fakeBackups) Unlock()           {}
func (b *fakeBackups) BackupDir() string { return b.dir }
func (b *fakeBackups) RunPgDump(_ context.Context, path string) error {
	return os.WriteFile(path, []byte("dump"), 0o600)
}

type fakeCache struct {
	mu       sync.Mutex
	imported []string
	kept     []string
}

func (c *fakeCache) Import(version, arch string, data []byte, sum string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hexSum(data) != sum {
		return errors.New("hash")
	}
	c.imported = append(c.imported, version+"/"+arch)
	return nil
}

func (c *fakeCache) PruneCache(keep ...string) ([]string, error) {
	c.mu.Lock()
	c.kept = keep
	c.mu.Unlock()
	return nil, nil
}

// memTransport serves release files and bundles from memory.
type memTransport struct{ fakeFetcher }

func (m *memTransport) Download(_ context.Context, url, dest string, size int64, sum, _ string, progress func(int64, string)) (string, error) {
	b, ok := m.files[url]
	if !ok {
		return "", errMissing
	}
	if err := os.WriteFile(dest+".part", b, 0o600); err != nil {
		return "", err
	}
	progress(int64(len(b)), RouteDirect)
	if err := checkFile(dest+".part", size, sum); err != nil {
		_ = os.Remove(dest + ".part")
		return RouteDirect, err
	}
	return RouteDirect, os.Rename(dest+".part", dest)
}

func (m *memTransport) RouteLabel(string) string { return "github.com" }

type harness struct {
	t        *testing.T
	root     string
	key      ed25519.PrivateKey
	sys      *fakeSystem
	backups  *fakeBackups
	cache    *fakeCache
	net      *memTransport
	sysFiles []string
	health   *httptest.Server
	bus      *events.EventBus
	sub      events.Subscriber
	panel    *Usecase

	mu         sync.Mutex
	broken     map[string]bool // versions that fail their health check
	netPending bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, root: root, key: testKey(t), broken: map[string]bool{},
		cache: &fakeCache{}, net: &memTransport{fakeFetcher{files: map[string][]byte{}}}}
	h.sys = &fakeSystem{h: h}
	h.backups = &fakeBackups{dir: filepath.Join(root, "data", "backups")}
	_ = os.MkdirAll(h.backups.dir, 0o700)
	h.sysFiles = []string{filepath.Join(root, "sys", "nasnet-panel.service"), filepath.Join(root, "sys", "nasnet"),
		filepath.Join(root, "sys", "nasnet-netrollback.timer")}
	for rel, body := range map[string]string{
		"bin/nasnet-panel": "old panel", "bin/agent/nasnet-agent-linux-" + runtime.GOARCH: "old agent",
		"bin/agent/VERSION": "v1.0.0", "nasnet-tool.sh": "old tool", ".version": "v1.0.0\n",
		"sys/nasnet-panel.service": "old unit", "sys/nasnet": "old cli",
	} {
		writeTestFile(t, filepath.Join(root, rel), body)
	}
	h.health = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := os.ReadFile(filepath.Join(root, ".version"))
		version := strings.TrimSpace(string(v))
		h.mu.Lock()
		broken := h.broken[version]
		h.mu.Unlock()
		if broken {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(Health{Version: version, DB: true, Xray: true})
	}))
	t.Cleanup(h.health.Close)
	h.bus = events.NewEventBus()
	h.sub = h.bus.Subscribe("test")
	h.panel = h.usecase("v1.0.0", h.bus)
	return h
}

func writeTestFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) usecase(version string, bus *events.EventBus) *Usecase {
	u := New(Deps{
		Paths: Paths{Root: h.root}, Version: version, Settings: newMemSettings(), Events: bus,
		Backups: h.backups, XrayCache: h.cache, System: h.sys, SystemFiles: h.sysFiles,
		HealthAddr: strings.TrimPrefix(h.health.URL, "http://"),
		NetPending: func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.netPending },
	})
	u.kind = Kind{Name: KindSystemd}
	u.trialFor, u.pollEvery = 600*time.Millisecond, 10*time.Millisecond
	u.fetch = h.net
	u.checker = NewChecker(h.net, runtime.GOARCH)
	return u
}

// runRunner is the transient unit: the old binary, a process of its own.
func (h *harness) runRunner(id string) {
	_ = h.usecase(h.installed(), nil).RunJob(context.Background(), id)
}

func (h *harness) installed() string {
	v, _ := os.ReadFile(filepath.Join(h.root, ".version"))
	return strings.TrimSpace(string(v))
}

func (h *harness) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(h.root, rel))
	return string(b)
}

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// bundle builds a signed offline bundle; tamper runs after signing.
func (h *harness) bundle(version string, tamper func(files map[string][]byte)) []byte {
	h.t.Helper()
	elf := []byte{0x7f, 'E', 'L', 'F'}
	files := map[string][]byte{
		"bin/nasnet-panel": []byte("#!/bin/sh\ncase \"$1\" in\n--version) echo \"nasnet-panel version " + version + "\" ;;\nupdate) exit 0 ;;\nesac\n"),
		"bin/agent/nasnet-agent-linux-" + runtime.GOARCH: []byte("agent " + version),
		"bin/agent/VERSION":         []byte(version),
		"bin/xray/xray-linux-amd64": append(elf, "amd64"...),
		"bin/xray/xray-linux-arm64": append(elf, "arm64"...),
		"nasnet-tool.sh":            []byte("tool " + version),
		".bundle-manifest":          []byte("{}"),
	}
	sums := map[string]string{}
	for p, b := range files {
		sums[p] = hexSum(b)
	}
	data, _ := json.Marshal(release.Bundle{Schema: release.Schema, Version: version, Xray: "26.11.1", Arch: runtime.GOARCH, Files: sums})
	files[release.BundleName] = data
	files[release.BundleName+release.SigSuffix] = []byte(signing.EncodeSig(ed25519.Sign(h.key, data)))
	if tamper != nil {
		tamper(files)
	}
	return tarGz(h.t, bundleTopPrefix+runtime.GOARCH, files)
}

func tarGz(t *testing.T, top string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: top + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b := files[n]
		_ = tw.WriteHeader(&tar.Header{Name: top + "/" + n, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(b))})
		_, _ = tw.Write(b)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func (h *harness) upload(version string) *Upload {
	h.t.Helper()
	up, err := h.panel.SaveUpload(bytes.NewReader(h.bundle(version, nil)))
	if err != nil {
		h.t.Fatal(err)
	}
	return up
}

// wait returns the job once it ended and the panel recorded it.
func (h *harness) wait(u *Usecase) *domain.Job {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := u.Store().Load()
		if j.Finished() && (j.Recorded || u.deps.Events == nil) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := u.Store().Load()
	h.t.Fatalf("the job never ended: %+v", j)
	return nil
}

func stepStatus(j *domain.Job, id string) string {
	for _, s := range j.Steps {
		if s.ID == id {
			return s.Status
		}
	}
	return ""
}

func TestAnUploadedUpdateSwapsRestartsAndPassesItsTrial(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	if up.Version != "v1.1.0" || up.Arch != runtime.GOARCH {
		t.Fatalf("upload = %+v", up)
	}
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	j := h.wait(h.panel)
	if j.Result != domain.ResultOK {
		t.Fatalf("job = %+v", j)
	}
	for _, s := range j.Steps {
		want := domain.StepDone
		if s.ID == domain.StepDownload {
			want = domain.StepSkipped
		}
		if s.Status != want {
			t.Errorf("step %s is %s", s.ID, s.Status)
		}
	}
	if h.installed() != "v1.1.0" || !strings.Contains(h.read("bin/nasnet-panel"), "v1.1.0") ||
		h.read("bin/agent/nasnet-agent-linux-"+runtime.GOARCH) != "agent v1.1.0" || h.read("nasnet-tool.sh") != "tool v1.1.0" {
		t.Fatal("the new files aren't in place")
	}
	if h.read("bin/nasnet-panel.prev") != "old panel" {
		t.Fatal("the old binary wasn't kept for a rollback")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".bundle-manifest")); err == nil {
		t.Fatal("an online install got .bundle-manifest and would switch to offline mode")
	}
	if h.read("sys/nasnet-panel.service") != "unit from tool v1.1.0" {
		t.Fatal("the system files weren't rewritten by the release's script")
	}
	if strings.Join(h.sys.calls, " ") != "runner update-system restart" {
		t.Fatalf("host calls: %v", h.sys.calls)
	}
	if len(h.cache.imported) != 2 || !contains(h.cache.kept, "26.11.1") {
		t.Fatalf("xray cache: imported %v, kept %v", h.cache.imported, h.cache.kept)
	}
	dumps, _ := filepath.Glob(filepath.Join(h.backups.dir, "pre_update_v1.0.0_to_v1.1.0_*.sql"))
	if len(dumps) != 1 {
		t.Fatalf("pre-update backups: %v", dumps)
	}
	if left, _ := os.ReadDir(h.panel.deps.Paths.Staging()); len(left) != 0 {
		t.Fatal("staging was left behind")
	}
	if _, err := h.panel.upload(up.ID); err == nil {
		t.Fatal("the installed upload was kept")
	}
	if got := drain(h.sub); len(got) != 1 || got[0].Type != events.EventPanelUpdated {
		t.Fatalf("events = %+v", got)
	}
	if w := h.usecase("v1.1.0", nil).rollbackWindow(); w == nil || w.Version != "v1.0.0" {
		t.Fatalf("rollback window = %+v", w)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestADownloadedUpdateIsCheckedAgainstItsRelease(t *testing.T) {
	h := newHarness(t)
	b := h.bundle("v1.1.0", nil)
	asset := release.BundleAsset(runtime.GOARCH)
	h.net.publish(t, h.key, "v1.1.0", release.Manifest{Version: "v1.1.0",
		Assets: map[string]release.Asset{asset: {SHA256: hexSum(b), Size: int64(len(b))}}})
	h.net.files[ReleaseURL("v1.1.0", asset)] = b

	if _, err := h.panel.Apply(context.Background(), Request{Version: "v1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if j := h.wait(h.panel); j.Result != domain.ResultOK || stepStatus(j, domain.StepDownload) != domain.StepDone {
		t.Fatalf("job = %+v", j)
	}
	if left, _ := os.ReadDir(h.panel.deps.Paths.Downloads()); len(left) != 0 {
		t.Fatal("the download was left behind")
	}

	// a mirror serving another file than the release lists
	h = newHarness(t)
	h.net.publish(t, h.key, "v1.1.0", release.Manifest{Version: "v1.1.0",
		Assets: map[string]release.Asset{asset: {SHA256: hexSum(b), Size: int64(len(b))}}})
	h.net.files[ReleaseURL("v1.1.0", asset)] = h.bundle("v1.1.0", func(f map[string][]byte) { f["nasnet-tool.sh"] = []byte("evil") })
	if _, err := h.panel.Apply(context.Background(), Request{Version: "v1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if j := h.wait(h.panel); j.Result != domain.ResultFailed || stepStatus(j, domain.StepDownload) != domain.StepFailed {
		t.Fatalf("job = %+v", j)
	}
	if h.installed() != "v1.0.0" {
		t.Fatal("a bad download was installed")
	}
}

func TestAFailedTrialPutsTheOldVersionBack(t *testing.T) {
	h := newHarness(t)
	h.broken["v1.1.0"] = true
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	j := h.wait(h.panel)
	if j.Result != domain.ResultRolledBack || stepStatus(j, domain.StepHealth) != domain.StepFailed ||
		stepStatus(j, domain.StepRevert) != domain.StepDone {
		t.Fatalf("job = %+v", j)
	}
	if !strings.Contains(j.Error, "didn't answer within") {
		t.Fatalf("reason = %s", j.Error)
	}
	if h.installed() != "v1.0.0" || h.read("bin/nasnet-panel") != "old panel" || h.read("nasnet-tool.sh") != "old tool" ||
		h.read("bin/agent/VERSION") != "v1.0.0" || h.read("sys/nasnet-panel.service") != "old unit" {
		t.Fatal("the old version isn't fully back")
	}
	if _, err := os.Stat(h.sysFiles[2]); !os.IsNotExist(err) {
		t.Fatal("a system file the update added is still there")
	}
	if h.usecase("v1.0.0", nil).rollbackWindow() != nil {
		t.Fatal("a rollback is offered after the revert")
	}
	if got := drain(h.sub); len(got) != 1 || got[0].Type != events.EventPanelUpdateFailed {
		t.Fatalf("events = %+v", got)
	}
}

func TestACrashLoopEndsTheTrialEarly(t *testing.T) {
	h := newHarness(t)
	h.sys.crashes = maxStarts
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	h.broken["v1.1.0"] = true
	h.sys.mu.Lock()
	h.sys.crashes = 0
	h.sys.mu.Unlock()
	j := h.wait(h.panel)
	if j.Result != domain.ResultRolledBack {
		t.Fatalf("job = %+v", j)
	}
}

func TestABundleThatDoesNotMatchItsSignatureChangesNothing(t *testing.T) {
	cases := map[string]func(map[string][]byte){
		"changed file": func(f map[string][]byte) { f["bin/nasnet-panel"] = []byte("#!/bin/sh\necho evil\n") },
		"extra file":   func(f map[string][]byte) { f["bin/extra"] = []byte("x") },
		"bad signature": func(f map[string][]byte) {
			f[release.BundleName+release.SigSuffix] = []byte(signing.EncodeSig(make([]byte, 64)))
		},
	}
	for name, tamper := range cases {
		h := newHarness(t)
		bundle := h.bundle("v1.1.0", tamper)
		up, err := h.panel.SaveUpload(bytes.NewReader(bundle))
		if name == "bad signature" {
			if err == nil {
				t.Fatalf("%s: the upload was accepted", name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
			t.Fatal(err)
		}
		j := h.wait(h.panel)
		if j.Result != domain.ResultFailed || stepStatus(j, domain.StepVerify) != domain.StepFailed {
			t.Fatalf("%s: job = %+v", name, j)
		}
		if h.installed() != "v1.0.0" || len(h.sys.calls) != 0 {
			t.Fatalf("%s: something changed: %v", name, h.sys.calls)
		}
	}
}

func TestGuardsStopAnUpdateBeforeAnythingChanges(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	h.netPending = true
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	if j := h.wait(h.panel); j.Result != domain.ResultFailed || !strings.Contains(j.Error, "network change") {
		t.Fatalf("job = %+v", j)
	}

	h = newHarness(t)
	up = h.upload("v1.1.0")
	h.backups.busy = true
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	if j := h.wait(h.panel); j.Result != domain.ResultFailed || !strings.Contains(j.Error, "backup") {
		t.Fatalf("job = %+v", j)
	}

	h = newHarness(t)
	h.panel.kind = Kind{Name: KindDocker, Why: "Docker installs update through Docker."}
	if _, err := h.panel.Apply(context.Background(), Request{Version: "v1.1.0"}); err == nil {
		t.Fatal("a Docker install started an update")
	}
}

func TestOnlyOneJobRunsAtATime(t *testing.T) {
	h := newHarness(t)
	unlock, err := h.panel.Store().TryLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.panel.Apply(context.Background(), Request{Version: "v1.1.0"}); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("err = %v", err)
	}
	unlock()
	_ = h.panel.Store().Save(&domain.Job{ID: "x", Phase: domain.PhaseRunner})
	if _, err := h.panel.Apply(context.Background(), Request{Version: "v1.1.0"}); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadsRefuseWhatCannotBeInstalled(t *testing.T) {
	h := newHarness(t)
	for _, v := range []string{"v1.0.0", "v0.9.0"} {
		if _, err := h.panel.SaveUpload(bytes.NewReader(h.bundle(v, nil))); err == nil {
			t.Fatalf("%s was accepted on v1.0.0", v)
		}
	}
	if _, err := h.panel.SaveUpload(strings.NewReader("not a bundle")); err == nil {
		t.Fatal("a random file was accepted")
	}
	if left, _ := os.ReadDir(h.panel.deps.Paths.Uploads()); len(left) != 0 {
		t.Fatalf("refused uploads stayed: %d files", len(left))
	}
}

func TestRollbackPutsThePreviousVersionBack(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	h.wait(h.panel)
	drain(h.sub)

	// the new panel, a few days later
	panel := h.usecase("v1.1.0", h.bus)
	if _, err := panel.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := h.wait(panel)
	if j.Kind != domain.KindRollback || j.Result != domain.ResultOK {
		t.Fatalf("job = %+v", j)
	}
	if h.installed() != "v1.0.0" || h.read("bin/nasnet-panel") != "old panel" || h.read("sys/nasnet-panel.service") != "old unit" {
		t.Fatal("v1.0.0 isn't back")
	}
	if h.usecase("v1.0.0", nil).rollbackWindow() != nil || panel.rollbackWindow() != nil {
		t.Fatal("the rollback window stayed open")
	}
	if _, err := h.usecase("v1.0.0", nil).Rollback(context.Background()); err == nil {
		t.Fatal("rolled back twice")
	}
}

func TestARollbackThatWontComeUpGoesForwardAgain(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	h.wait(h.panel)
	h.broken["v1.0.0"] = true

	panel := h.usecase("v1.1.0", h.bus)
	if _, err := panel.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := h.wait(panel)
	if j.Result != domain.ResultFailed || !strings.Contains(j.Error, "so v1.1.0 is running again") {
		t.Fatalf("job = %+v", j)
	}
	if h.installed() != "v1.1.0" || !strings.Contains(h.read("bin/nasnet-panel"), "v1.1.0") {
		t.Fatal("v1.1.0 isn't back")
	}
}

func TestTheRollbackWindowClosesAfterSevenDays(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	h.wait(h.panel)
	panel := h.usecase("v1.1.0", nil)
	panel.now = func() time.Time { return time.Now().Add(rollbackDays*24*time.Hour + time.Minute) }
	if panel.rollbackWindow() != nil {
		t.Fatal("the window is open after 7 days")
	}
}

func TestARunnerThatDiedMidSwapIsPutBack(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	agent := filepath.Join(h.root, "bin", "agent", "nasnet-agent-linux-"+runtime.GOARCH)
	// a runner that gets halfway through the swap and dies: system files and
	// the agents are new, the panel binary isn't
	h.panel.deps.System = &halfRunner{fakeSystem: h.sys, swap: func() {
		set := &fileSet{Version: "v1.0.0", ReplacedBy: "v1.1.0", At: time.Now()}
		if err := set.save(filepath.Join(h.panel.deps.Paths.Prev(), "system"), h.sysFiles...); err != nil {
			t.Error(err)
		}
		if err := set.save(filepath.Join(h.panel.deps.Paths.Prev(), "files"), agent); err != nil {
			t.Error(err)
		}
		_ = set.write(h.panel.setFile())
		writeTestFile(t, agent, "agent v1.1.0")
		writeTestFile(t, h.sysFiles[0], "new unit")
	}}
	h.panel.runnerGone = 50 * time.Millisecond
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	j := h.wait(h.panel)
	if j.Result != domain.ResultRolledBack {
		t.Fatalf("job = %+v", j)
	}
	if h.read("bin/agent/nasnet-agent-linux-"+runtime.GOARCH) != "old agent" || h.read("sys/nasnet-panel.service") != "old unit" {
		t.Fatal("the half-swapped files weren't put back")
	}
}

// halfRunner plays a runner unit that dies partway.
type halfRunner struct {
	*fakeSystem
	swap func()
}

func (d *halfRunner) StartRunner(context.Context, string) error { d.swap(); return nil }

func TestASystemStepFailureChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.sys.sysError = errors.New("write_backend_unit failed")
	up := h.upload("v1.1.0")
	if _, err := h.panel.Apply(context.Background(), Request{UploadID: up.ID}); err != nil {
		t.Fatal(err)
	}
	j := h.wait(h.panel)
	if j.Result != domain.ResultFailed || stepStatus(j, domain.StepSystem) != domain.StepFailed {
		t.Fatalf("job = %+v", j)
	}
	if h.installed() != "v1.0.0" || h.read("sys/nasnet-panel.service") != "old unit" {
		t.Fatal("something changed")
	}
}

func TestBundlePathsCannotEscape(t *testing.T) {
	for _, name := range []string{"etc/passwd", "nasnet-panel-offline-linux-amd64/../../etc/passwd", "/nasnet-panel-offline-linux-amd64/x"} {
		if rel, err := bundleEntry(name); err == nil {
			t.Errorf("%s was allowed as %q", name, rel)
		}
	}
	if rel, err := bundleEntry("./nasnet-panel-offline-linux-amd64/bin/nasnet-panel"); err != nil || rel != "bin/nasnet-panel" {
		t.Fatalf("rel = %q, %v", rel, err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: bundleTopPrefix + "amd64/bin/nasnet-panel", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	p := filepath.Join(t.TempDir(), "b.tar.gz")
	_ = os.WriteFile(p, buf.Bytes(), 0o600)
	if _, err := unpackBundle(p, t.TempDir()); err == nil {
		t.Fatal("a symlink was unpacked")
	}
}

func TestPreUpdateBackupsKeepTheNewestThree(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, "pre_update_v1.0."+string(rune('0'+i))+"_to_v1.1.0_20261022_000000.sql")
		_ = os.WriteFile(p, nil, 0o600)
		_ = os.Chtimes(p, time.Now(), time.Now().Add(time.Duration(i)*time.Hour))
	}
	_ = os.WriteFile(filepath.Join(dir, "backup_20261022_000000.sql"), nil, 0o600)
	rotatePreUpdate(dir, 3)
	left, _ := filepath.Glob(filepath.Join(dir, "pre_update_*"))
	if len(left) != 3 || !strings.Contains(strings.Join(left, " "), "v1.0.4") {
		t.Fatalf("left %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup_20261022_000000.sql")); err != nil {
		t.Fatal("a scheduled backup was rotated")
	}
}
