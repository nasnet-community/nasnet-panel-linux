package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// Backups is the backup service the pre-update dump goes through.
type Backups interface {
	TryLock() bool
	Unlock()
	BackupDir() string
	RunPgDump(ctx context.Context, path string) error
}

// XrayCache takes the release's Xray builds and drops ones nothing needs.
type XrayCache interface {
	Import(version, arch string, data []byte, sum string) error
	PruneCache(keep ...string) ([]string, error)
}

// Request is what to install: a version from the last check, or an upload.
type Request struct {
	Version  string `json:"version,omitempty"`
	UploadID string `json:"upload_id,omitempty"`
}

// ErrJobRunning means a job is already going.
var ErrJobRunning = errors.New("an update is already running")

// keepPreUpdate is how many pre-update backups stay; scheduled rotation
// leaves them alone.
const keepPreUpdate = 3

// Apply starts an update. The panel runs the first steps itself and hands
// the rest to the runner unit; the job keeps going without the caller.
func (u *Usecase) Apply(ctx context.Context, req Request) (*domain.Job, error) {
	if u.kind.Name != KindSystemd {
		return nil, errors.New(u.kind.Why)
	}
	if req.Version == "" && req.UploadID == "" {
		return nil, errors.New("pick a version or an upload")
	}
	unlock, err := u.store.TryLock()
	if err != nil {
		return nil, ErrJobRunning
	}
	if j, err := u.store.Load(); err != nil || (j != nil && !j.Finished()) {
		unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrJobRunning
	}
	job := &domain.Job{
		ID:        newJobID(),
		Kind:      domain.KindUpdate,
		From:      u.deps.Version,
		To:        req.Version,
		Source:    domain.SourceDownload,
		UploadID:  req.UploadID,
		Phase:     domain.PhasePanel,
		StartedAt: u.now().UTC(),
	}
	if req.UploadID != "" {
		up, err := u.upload(req.UploadID)
		if err != nil {
			unlock()
			return nil, err
		}
		job.Source, job.To = domain.SourceUpload, up.Version
	}
	job.Steps = domain.UpdateSteps(job.To, job.Source == domain.SourceUpload)
	if err := u.store.Save(job); err != nil {
		unlock()
		return nil, err
	}
	u.running.Store(true)
	go u.runPanelSteps(context.WithoutCancel(ctx), job, unlock)
	return job, nil
}

func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// run records one job's progress in state.json as it goes.
type run struct {
	u   *Usecase
	mu  sync.Mutex // the runner's heartbeat writes too
	job *domain.Job
}

func (r *run) set(id, status, detail string) {
	r.mu.Lock()
	for i := range r.job.Steps {
		if r.job.Steps[i].ID == id {
			r.job.Steps[i].Status, r.job.Steps[i].Detail = status, detail
		}
	}
	r.mu.Unlock()
	r.save()
}

func (r *run) begin(id, detail string) { r.set(id, domain.StepRunning, detail) }
func (r *run) done(id, detail string)  { r.set(id, domain.StepDone, detail) }

func (r *run) save() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.u.store.Save(r.job); err != nil {
		logger.GetLogger().WithError(err).Warn("[update] could not save the job")
	}
}

// fail ends the job at step id. Before the swap, nothing has changed.
func (r *run) fail(id string, err error) {
	r.set(id, domain.StepFailed, err.Error())
	r.finish(domain.ResultFailed, err.Error())
}

func (r *run) finish(result, reason string) {
	now := r.u.now().UTC()
	r.mu.Lock()
	r.job.Phase, r.job.Result, r.job.Error, r.job.FinishedAt = domain.PhaseDone, result, reason, &now
	r.mu.Unlock()
	r.save()
}

func (u *Usecase) runPanelSteps(ctx context.Context, job *domain.Job, unlock func()) {
	r := &run{u: u, job: job}
	handedOff := false
	defer func() {
		u.running.Store(false)
		if !handedOff {
			unlock()
			u.cleanup(job)
			u.record(ctx, job)
		}
	}()
	staging := filepath.Join(u.deps.Paths.Staging(), job.ID)

	// 1. guards. A download's size comes from its release.json.
	r.begin(domain.StepGuards, "")
	var tarball string
	var manifest *release.Manifest
	var size int64
	if job.Source == domain.SourceUpload {
		up, err := u.upload(job.UploadID)
		if err != nil {
			r.fail(domain.StepGuards, err)
			return
		}
		tarball, size = u.uploadPath(job.UploadID), up.Size
		if up.Arch != runtime.GOARCH {
			r.fail(domain.StepGuards, fmt.Errorf("that bundle is for %s; this server is %s", up.Arch, runtime.GOARCH))
			return
		}
	} else {
		m, route, err := u.checker.Manifest(ctx, job.To)
		if err != nil {
			r.fail(domain.StepGuards, fmt.Errorf("couldn't read %s's release: %w", job.To, err))
			return
		}
		a, ok := m.Assets[release.BundleAsset(runtime.GOARCH)]
		if !ok {
			r.fail(domain.StepGuards, fmt.Errorf("%s has no %s build", job.To, runtime.GOARCH))
			return
		}
		manifest, size, job.Route = m, a.Size, route
		// a part file only resumes the same release
		tarball = filepath.Join(u.deps.Paths.Downloads(), job.To, release.BundleAsset(runtime.GOARCH))
		// refuse what can't install before downloading it
		if err := u.checkRelease(m.Version, m.MinFrom, runtime.GOARCH); err != nil {
			r.fail(domain.StepGuards, err)
			return
		}
	}
	if err := u.guard(ctx, size); err != nil {
		r.fail(domain.StepGuards, err)
		return
	}
	r.done(domain.StepGuards, "")

	// 2. download, resuming a part file
	if job.Source == domain.SourceDownload {
		a := manifest.Assets[release.BundleAsset(runtime.GOARCH)]
		r.begin(domain.StepDownload, "")
		u.dropOtherDownloads(job.To)
		if err := os.MkdirAll(filepath.Dir(tarball), 0o700); err != nil {
			r.fail(domain.StepDownload, err)
			return
		}
		route, err := u.fetch.Download(ctx, ReleaseURL(job.To, release.BundleAsset(runtime.GOARCH)), tarball, a.Size, a.SHA256, job.Route,
			func(done int64, route string) {
				r.job.Route = route
				r.set(domain.StepDownload, domain.StepRunning, fmt.Sprintf("%s of %s · %s", mb(done), mb(a.Size), u.fetch.RouteLabel(route)))
			})
		if err != nil {
			r.fail(domain.StepDownload, err)
			return
		}
		job.Route = route
		r.done(domain.StepDownload, fmt.Sprintf("%s · %s", mb(a.Size), u.fetch.RouteLabel(route)))
	}

	// 3. verify: the download's hash was checked against release.json, now
	// the bundle's own signature and every file in it
	r.begin(domain.StepVerify, "")
	_ = os.RemoveAll(staging)
	b, err := unpackBundle(tarball, staging)
	if err == nil {
		err = u.checkBundle(b, job)
	}
	if err != nil {
		r.fail(domain.StepVerify, err)
		return
	}
	r.done(domain.StepVerify, "")

	// 4. backup
	r.begin(domain.StepBackup, "")
	name, err := u.backup(ctx, job)
	if err != nil {
		r.fail(domain.StepBackup, err)
		return
	}
	r.done(domain.StepBackup, name)

	// 5. stage: the release's Xray goes into the cache next to the old one
	r.begin(domain.StepStage, "")
	if err := u.stageXray(ctx, staging, b); err != nil {
		r.fail(domain.StepStage, err)
		return
	}
	r.done(domain.StepStage, "")

	// 6. test the new binary before anything is swapped
	r.begin(domain.StepTest, "")
	if err := u.testBinary(ctx, filepath.Join(staging, "bin", "nasnet-panel"), job.To); err != nil {
		r.fail(domain.StepTest, err)
		return
	}
	r.done(domain.StepTest, "")

	// the rest runs in the runner unit, which outlives the restart
	// counts as a heartbeat, so nobody takes the lock before the runner does
	handoff := u.now().UTC()
	job.Phase, job.HealthAddr, job.RunnerSeen = domain.PhaseRunner, u.deps.HealthAddr, &handoff
	if u.deps.XrayRunning != nil {
		job.XrayRan = u.deps.XrayRunning()
	}
	r.save()
	unlock()
	handedOff = true
	if err := u.system().StartRunner(ctx, job.ID); err != nil {
		if relock, lerr := u.store.TryLock(); lerr == nil {
			r.fail(domain.StepSystem, fmt.Errorf("couldn't start the update runner: %w", err))
			relock()
		}
		u.cleanup(job)
		u.record(ctx, job)
		return
	}
	u.watchRunner(ctx)
}

func mb(n int64) string {
	return fmt.Sprintf("%d MB", (n+(1<<20)/2)>>20)
}

// guard refuses an update the server can't take right now.
func (u *Usecase) guard(ctx context.Context, size int64) error {
	if u.deps.NetPending != nil && u.deps.NetPending() {
		return errors.New("a network change is waiting for confirmation; confirm or revert it first")
	}
	if b := u.deps.Backups; b != nil {
		if !b.TryLock() {
			return errors.New("a backup or restore is running")
		}
		b.Unlock()
	}
	var db int64
	if u.deps.DBSize != nil {
		db, _ = u.deps.DBSize(ctx)
	}
	need := 3*size + db
	if free, err := freeBytes(u.deps.Paths.Root); err == nil && free < need {
		return fmt.Errorf("there's %s free; the update needs %s", mb(free), mb(need))
	}
	return nil
}

func freeBytes(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)), nil
}

// checkBundle holds the bundle to what this job is meant to install.
func (u *Usecase) checkBundle(b *release.Bundle, job *domain.Job) error {
	if b.Version != job.To {
		return fmt.Errorf("the bundle is %s, not %s", b.Version, job.To)
	}
	if err := u.checkRelease(b.Version, b.MinFrom, b.Arch); err != nil {
		return err
	}
	for _, need := range []string{"bin/nasnet-panel", "nasnet-tool.sh"} {
		if _, ok := b.Files[need]; !ok {
			return fmt.Errorf("the bundle has no %s", need)
		}
	}
	return nil
}

// checkRelease: newer than this panel, allowed from it, and for this arch.
func (u *Usecase) checkRelease(version, minFrom, arch string) error {
	v, err := release.Parse(version)
	if err != nil {
		return err
	}
	running, err := release.Parse(u.deps.Version)
	if err != nil {
		return ErrNoVersion
	}
	if !v.Newer(running) {
		return fmt.Errorf("%s isn't newer than %s; updates never go back", version, u.deps.Version)
	}
	if !release.AllowsFrom(minFrom, running) {
		return fmt.Errorf("%s needs %s or later first", version, minFrom)
	}
	if arch != runtime.GOARCH {
		return fmt.Errorf("that bundle is for %s; this server is %s", arch, runtime.GOARCH)
	}
	return nil
}

var preUpdateRE = regexp.MustCompile(`^pre_update_.+\.sql$`)

func (u *Usecase) backup(ctx context.Context, job *domain.Job) (string, error) {
	b := u.deps.Backups
	if b == nil {
		return "", nil
	}
	if !b.TryLock() {
		return "", errors.New("a backup or restore is running")
	}
	defer b.Unlock()
	name := fmt.Sprintf("pre_update_%s_to_%s_%s.sql", job.From, job.To, u.now().UTC().Format("20060102_150405"))
	// an install whose scheduled backups never ran has no folder yet
	if err := os.MkdirAll(b.BackupDir(), 0o700); err != nil {
		return "", fmt.Errorf("the database backup failed: %w", err)
	}
	if err := b.RunPgDump(ctx, filepath.Join(b.BackupDir(), name)); err != nil {
		return "", fmt.Errorf("the database backup failed: %w", err)
	}
	rotatePreUpdate(b.BackupDir(), keepPreUpdate)
	return name, nil
}

// rotatePreUpdate keeps the newest keep pre-update backups. Names sort by
// time only within one version pair, so file times decide.
func rotatePreUpdate(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		name string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && preUpdateRE.MatchString(e.Name()) {
			files = append(files, file{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	for i := keep; i < len(files); i++ {
		_ = os.Remove(filepath.Join(dir, files[i].name))
	}
}

func (u *Usecase) stageXray(ctx context.Context, staging string, b *release.Bundle) error {
	cache := u.deps.XrayCache
	if cache == nil || b.Xray == "" {
		return nil
	}
	for _, arch := range []string{"amd64", "arm64"} {
		rel := "bin/xray/xray-linux-" + arch
		sum, ok := b.Files[rel]
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if err := cache.Import(b.Xray, arch, data, sum); err != nil {
			return err
		}
	}
	keep := []string{pin.Version, b.Xray}
	if u.deps.BuiltinXray != nil {
		if v := u.deps.BuiltinXray(ctx); v != "" {
			keep = append(keep, v)
		}
	}
	if removed, err := cache.PruneCache(keep...); err == nil && len(removed) > 0 {
		logger.GetLogger().WithField("versions", removed).Info("[update] dropped unused xray builds")
	}
	return nil
}

func (u *Usecase) testBinary(ctx context.Context, bin, want string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := u.command(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the new binary doesn't run: %w", err)
	}
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("the new binary says %q, not %s", strings.TrimSpace(string(out)), want)
	}
	if out, err := u.command(ctx, bin, "update", "selftest").CombinedOutput(); err != nil {
		return fmt.Errorf("the new version can't read this install: %s", lastLine(out, err))
	}
	return nil
}

func (u *Usecase) command(ctx context.Context, bin string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = u.deps.Paths.Root
	return cmd
}

func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// dropOtherDownloads removes part files of releases other than version.
func (u *Usecase) dropOtherDownloads(version string) {
	entries, _ := os.ReadDir(u.deps.Paths.Downloads())
	for _, e := range entries {
		if e.Name() != version {
			_ = os.RemoveAll(filepath.Join(u.deps.Paths.Downloads(), e.Name()))
		}
	}
}

// cleanup drops the job's staging and, unless a resume can use it, the
// download.
func (u *Usecase) cleanup(job *domain.Job) {
	_ = os.RemoveAll(filepath.Join(u.deps.Paths.Staging(), job.ID))
	keepPart := false
	for _, s := range job.Steps {
		if s.ID == domain.StepDownload && s.Status == domain.StepFailed {
			keepPart = true
		}
	}
	if !keepPart {
		_ = os.RemoveAll(u.deps.Paths.Downloads())
	}
	if job.Source == domain.SourceUpload && job.Result == domain.ResultOK {
		u.dropUpload(job.UploadID)
	}
}
