package usecase

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
)

const (
	trialTime     = 2 * time.Minute
	maxStarts     = 3
	rollbackDays  = 7
	runnerTimeout = 60 * time.Second // a runner silent this long is gone, by default
)

// Health is what /health/update answers on loopback.
type Health struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	DB        bool      `json:"db"`
	Xray      bool      `json:"xray"`
	legacy    bool      // a version from before /health/update: only /health answered
}

// Health reports this process for the runner's trial.
func (u *Usecase) Health(ctx context.Context) Health {
	h := Health{Version: u.deps.Version, StartedAt: u.started}
	if u.deps.DB != nil {
		if sqlDB, err := u.deps.DB.DB(); err == nil && sqlDB.PingContext(ctx) == nil {
			h.DB = true
		}
	}
	if u.deps.XrayRunning != nil {
		h.Xray = u.deps.XrayRunning()
	}
	return h
}

func (r *run) heartbeat() func() {
	stop := make(chan struct{})
	beat := func() {
		r.mu.Lock()
		done := r.job.Phase == domain.PhaseDone
		now := r.u.now().UTC()
		r.job.RunnerSeen = &now
		r.mu.Unlock()
		// once it's done the panel owns the file: it marks the job recorded
		if !done {
			r.save()
		}
	}
	beat()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				beat()
			}
		}
	}()
	return func() { close(stop) }
}

// RunJob is the runner unit's half of a job.
func (u *Usecase) RunJob(ctx context.Context, id string) error {
	var unlock func()
	for i := 0; ; i++ {
		var err error
		if unlock, err = u.store.TryLock(); err == nil {
			break
		}
		if i == 20 {
			return ErrBusy
		}
		time.Sleep(500 * time.Millisecond)
	}
	defer unlock()
	job, err := u.store.Load()
	if err != nil {
		return err
	}
	if job == nil || job.ID != id || job.Phase != domain.PhaseRunner {
		return fmt.Errorf("no job %s is waiting for the runner", id)
	}
	r := &run{u: u, job: job}
	stop := r.heartbeat()
	defer stop()
	if job.Kind == domain.KindRollback {
		u.runRollback(ctx, r)
	} else {
		u.runUpdate(ctx, r)
	}
	if job.Result != domain.ResultOK {
		return errors.New(job.Error)
	}
	return nil
}

func (u *Usecase) setFile() string { return filepath.Join(u.deps.Paths.Prev(), "set.json") }

func (u *Usecase) systemFiles() []string {
	if u.deps.SystemFiles != nil {
		return u.deps.SystemFiles
	}
	return SystemFiles
}

// installPlan pairs each live file with its staged copy. The panel binary
// goes last, so an interrupted swap leaves the old panel in place.
func (u *Usecase) installPlan(staging, version string) ([][2]string, error) {
	root := u.deps.Paths.Root
	var plan [][2]string
	agents, err := os.ReadDir(filepath.Join(staging, "bin", "agent"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range agents {
		if e.Type().IsRegular() {
			plan = append(plan, [2]string{filepath.Join(root, "bin", "agent", e.Name()), filepath.Join(staging, "bin", "agent", e.Name())})
		}
	}
	plan = append(plan, [2]string{filepath.Join(root, "nasnet-tool.sh"), filepath.Join(staging, "nasnet-tool.sh")})
	// offline-only files stay offline-only: .bundle-manifest switches the menu to offline mode
	for _, rel := range []string{"bin/nasnet-tool", ".bundle-manifest"} {
		live, src := filepath.Join(root, rel), filepath.Join(staging, rel)
		if exists(live) && exists(src) {
			plan = append(plan, [2]string{live, src})
		}
	}
	ver := filepath.Join(staging, ".version")
	if err := os.WriteFile(ver, []byte(version+"\n"), 0o644); err != nil {
		return nil, err
	}
	plan = append(plan, [2]string{filepath.Join(root, ".version"), ver})
	plan = append(plan, [2]string{u.deps.Paths.Binary(), filepath.Join(staging, "bin", "nasnet-panel")})
	return plan, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (u *Usecase) runUpdate(ctx context.Context, r *run) {
	job, sys := r.job, u.system()
	staging := filepath.Join(u.deps.Paths.Staging(), job.ID)
	prev := u.deps.Paths.Prev()
	_ = os.RemoveAll(prev)
	_ = os.Remove(u.deps.Paths.PrevBinary())
	set := &fileSet{Version: job.From, ReplacedBy: job.To, At: u.now().UTC()}

	// 7. system files, each saved before the release's script rewrites it
	r.begin(domain.StepSystem, "")
	if err := set.save(filepath.Join(prev, "system"), u.systemFiles()...); err != nil {
		r.fail(domain.StepSystem, err)
		u.cleanup(job)
		return
	}
	if err := set.write(u.setFile()); err != nil {
		r.fail(domain.StepSystem, err)
		u.cleanup(job)
		return
	}
	if err := sys.UpdateSystem(ctx, filepath.Join(staging, "nasnet-tool.sh")); err != nil {
		_ = u.putBack(ctx, set)
		u.dropPrev()
		r.fail(domain.StepSystem, err)
		u.cleanup(job)
		return
	}
	r.done(domain.StepSystem, "")

	// 8. swap
	r.begin(domain.StepSwap, "")
	plan, err := u.installPlan(staging, job.To)
	for _, p := range plan {
		if err != nil {
			break
		}
		if p[0] == u.deps.Paths.Binary() {
			err = set.saveAt(p[0], u.deps.Paths.PrevBinary())
		} else {
			err = set.save(filepath.Join(prev, "files"), p[0])
		}
	}
	if err == nil {
		err = set.write(u.setFile())
	}
	for _, p := range plan {
		if err != nil {
			break
		}
		err = installFile(p[1], p[0])
	}
	if err != nil {
		_ = u.putBack(ctx, set)
		u.dropPrev()
		r.fail(domain.StepSwap, err)
		u.cleanup(job)
		return
	}
	r.done(domain.StepSwap, "")

	// 9. restart
	r.begin(domain.StepRestart, "Waiting for the panel to come back…")
	if err := sys.Restart(ctx); err != nil {
		r.set(domain.StepRestart, domain.StepFailed, err.Error())
		u.revert(ctx, r, set, err)
		return
	}
	base, _ := sys.Starts(ctx)
	r.done(domain.StepRestart, "")

	// 10. trial boot
	r.begin(domain.StepHealth, "")
	if err := u.trial(ctx, job, job.To, base, false); err != nil {
		r.set(domain.StepHealth, domain.StepFailed, err.Error())
		u.revert(ctx, r, set, err)
		return
	}
	r.done(domain.StepHealth, "")
	r.finish(domain.ResultOK, "")
	u.cleanup(job)
}

// revert puts the old version back after a failed trial.
func (u *Usecase) revert(ctx context.Context, r *run, set *fileSet, cause error) {
	job, sys := r.job, u.system()
	r.addStep(domain.Step{ID: domain.StepRevert, Label: "Putting " + job.From + " back", Status: domain.StepRunning})
	err := u.putBack(ctx, set)
	if err == nil {
		err = sys.Restart(ctx)
	}
	if err == nil {
		base, _ := sys.Starts(ctx)
		err = u.trial(ctx, job, job.From, base, true)
	}
	u.dropPrev()
	u.cleanup(job)
	if err != nil {
		r.set(domain.StepRevert, domain.StepFailed, err.Error())
		r.finish(domain.ResultFailed, fmt.Sprintf("%s failed (%s), and %s didn't come back either (%s). Check the server.", job.To, cause, job.From, err))
		return
	}
	r.done(domain.StepRevert, "")
	r.finish(domain.ResultRolledBack, fmt.Sprintf("%s didn't pass its health check (%s), so %s is running again. The pre-update backup is in Backups.", job.To, cause, job.From))
}

func (r *run) addStep(s domain.Step) {
	r.mu.Lock()
	r.job.Steps = append(r.job.Steps, s)
	r.mu.Unlock()
	r.save()
}

func (u *Usecase) putBack(ctx context.Context, set *fileSet) error {
	return errors.Join(set.putBack(), u.system().DaemonReload(ctx))
}

func (u *Usecase) dropPrev() {
	_ = os.RemoveAll(u.deps.Paths.Prev())
	_ = os.Remove(u.deps.Paths.PrevBinary())
}

// trial waits for want to answer healthy. A version from before
// /health/update counts once it answers /health, when legacyOK.
func (u *Usecase) trial(ctx context.Context, job *domain.Job, want string, base int, legacyOK bool) error {
	deadline := u.now().Add(u.trialFor)
	reason := "the panel didn't answer"
	for {
		h, err := probeHealth(ctx, job.HealthAddr)
		switch {
		case err != nil:
			reason = "the panel didn't answer"
		case h.legacy && legacyOK:
			return nil
		case h.legacy || h.Version != want:
			reason = "the panel still runs " + h.Version
		case !h.DB:
			reason = "the database wasn't reachable"
		case job.XrayRan && !h.Xray:
			reason = "Xray didn't start"
		default:
			return nil
		}
		if n, err := u.system().Starts(ctx); err == nil && n-base >= maxStarts {
			return fmt.Errorf("it crashed %d times", n-base)
		}
		if !u.now().Before(deadline) {
			return fmt.Errorf("%s within %s", reason, minutes(u.trialFor))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(u.pollEvery):
		}
	}
}

func minutes(d time.Duration) string {
	if d < time.Minute {
		return d.String()
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

var healthClient = &http.Client{
	Timeout:   3 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // loopback only
}

// probeHealth asks the local panel, over HTTPS and then plain HTTP since
// either may be on.
func probeHealth(ctx context.Context, addr string) (Health, error) {
	if addr == "" {
		return Health{}, errors.New("no address to check")
	}
	var last error
	for _, scheme := range []string{"https", "http"} {
		h, err := getHealth(ctx, scheme+"://"+addr)
		if err == nil {
			return h, nil
		}
		last = err
	}
	return Health{}, last
}

func getHealth(ctx context.Context, base string) (Health, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health/update", nil)
	resp, err := healthClient.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		old, err := healthClient.Do(req)
		if err != nil {
			return Health{}, err
		}
		old.Body.Close()
		if old.StatusCode != http.StatusOK {
			return Health{}, fmt.Errorf("HTTP %d", old.StatusCode)
		}
		return Health{legacy: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var h Health
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// PanelXrayRunning asks the running panel whether its built-in Xray is up,
// for jobs the CLI starts.
func PanelXrayRunning(ctx context.Context, addr string) bool {
	h, err := probeHealth(ctx, addr)
	return err == nil && h.Xray
}

// RollbackWindow is the version a rollback returns to, and until when.
type RollbackWindow struct {
	Version string    `json:"version"`
	Until   time.Time `json:"until"`
}

func (u *Usecase) rollbackWindow() *RollbackWindow {
	set, err := readFileSet(u.setFile())
	if err != nil || set.ReplacedBy != u.deps.Version || !exists(u.deps.Paths.PrevBinary()) {
		return nil
	}
	until := set.At.Add(rollbackDays * 24 * time.Hour)
	if !u.now().Before(until) {
		return nil
	}
	return &RollbackWindow{Version: set.Version, Until: until}
}

// Rollback starts putting the previous version back. The database stays as
// it is; the pre-update backup is in Backups for a manual restore.
func (u *Usecase) Rollback(ctx context.Context) (*domain.Job, error) {
	if u.kind.Name != KindSystemd {
		return nil, errors.New(u.kind.Why)
	}
	w := u.rollbackWindow()
	if w == nil {
		return nil, errors.New("there's no earlier version to roll back to")
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
	if u.deps.NetPending != nil && u.deps.NetPending() {
		unlock()
		return nil, errors.New("a network change is waiting for confirmation; confirm or revert it first")
	}
	job := &domain.Job{
		ID: newJobID(), Kind: domain.KindRollback, From: u.deps.Version, To: w.Version,
		Source: domain.SourcePrev, Phase: domain.PhaseRunner, StartedAt: u.now().UTC(),
		Steps: domain.RollbackSteps(w.Version), HealthAddr: u.deps.HealthAddr,
	}
	job.Steps[0].Status = domain.StepDone
	if u.deps.XrayRunning != nil {
		job.XrayRan = u.deps.XrayRunning()
	}
	if err := u.store.Save(job); err != nil {
		unlock()
		return nil, err
	}
	unlock()
	if err := u.system().StartRunner(ctx, job.ID); err != nil {
		r := &run{u: u, job: job}
		r.fail(domain.StepSwap, fmt.Errorf("couldn't start the update runner: %w", err))
		go u.record(context.WithoutCancel(ctx), job)
		return job, nil
	}
	go u.watchRunner(context.WithoutCancel(ctx))
	return job, nil
}

func (u *Usecase) runRollback(ctx context.Context, r *run) {
	job, sys := r.job, u.system()
	set, err := readFileSet(u.setFile())
	if err != nil {
		r.fail(domain.StepSwap, fmt.Errorf("the saved files of %s are gone", job.To))
		return
	}
	fwd := &fileSet{Version: job.From, ReplacedBy: job.To, At: u.now().UTC()}

	r.begin(domain.StepSwap, "")
	for _, f := range set.Files {
		if err = fwd.save(filepath.Join(u.deps.Paths.Prev(), "forward"), f.Live); err != nil {
			break
		}
	}
	if err == nil {
		err = fwd.write(filepath.Join(u.deps.Paths.Prev(), "forward.json"))
	}
	if err == nil {
		if err = u.putBack(ctx, set); err != nil {
			_ = u.putBack(ctx, fwd)
		}
	}
	if err != nil {
		r.fail(domain.StepSwap, err)
		return
	}
	r.done(domain.StepSwap, "")

	r.begin(domain.StepRestart, "Waiting for the panel to come back…")
	err = sys.Restart(ctx)
	base, _ := sys.Starts(ctx)
	if err == nil {
		r.done(domain.StepRestart, "")
		r.begin(domain.StepHealth, "")
		err = u.trial(ctx, job, job.To, base, true)
	}
	if err == nil {
		r.done(domain.StepHealth, "")
		u.dropPrev()
		r.finish(domain.ResultOK, "")
		return
	}

	// the old version won't come up: go forward again
	r.set(domain.StepHealth, domain.StepFailed, err.Error())
	r.addStep(domain.Step{ID: domain.StepRevert, Label: "Going back to " + job.From, Status: domain.StepRunning})
	ferr := u.putBack(ctx, fwd)
	if ferr == nil {
		ferr = sys.Restart(ctx)
	}
	if ferr == nil {
		base, _ := sys.Starts(ctx)
		ferr = u.trial(ctx, job, job.From, base, false)
	}
	_ = os.RemoveAll(filepath.Join(u.deps.Paths.Prev(), "forward"))
	_ = os.Remove(filepath.Join(u.deps.Paths.Prev(), "forward.json"))
	if ferr != nil {
		r.set(domain.StepRevert, domain.StepFailed, ferr.Error())
		r.finish(domain.ResultFailed, fmt.Sprintf("%s didn't come up (%s), and %s didn't come back either (%s). Check the server.", job.To, err, job.From, ferr))
		return
	}
	r.done(domain.StepRevert, "")
	r.finish(domain.ResultFailed, fmt.Sprintf("%s didn't come up (%s), so %s is running again.", job.To, err, job.From))
}
