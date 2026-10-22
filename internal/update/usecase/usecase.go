// Package usecase checks for, installs and rolls back panel releases.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// Settings the Updates section edits.
const (
	keyCheckEnabled = "update_check_enabled"
	keyChannel      = "update_channel"
	keyMirror       = "update_mirror_url"
)

// State rows, hidden from the settings API.
const (
	keyLastCheck = "update_last_check"
	keyNotified  = "update_notified_version"
)

const (
	checkEvery  = 6 * time.Hour
	checkJitter = 30 * time.Minute
	firstCheck  = 2 * time.Minute
)

// Deps are what the updater works with. Only Paths, Version and Settings
// are required; the rest switch a step off when nil.
type Deps struct {
	Paths    Paths
	Version  string
	Settings settingDomain.SettingRepository
	DB       *gorm.DB
	HTTP     *httpclient.Factory
	Events   *events.EventBus // nil in the CLI, which never announces

	Backups     Backups
	DBSize      func(ctx context.Context) (int64, error)
	NetPending  func() bool // a network apply waits for confirmation
	XrayRunning func() bool // the built-in node's Xray
	XrayCache   XrayCache
	BuiltinXray func(ctx context.Context) string // the version picked for the built-in node
	HealthAddr  string                           // loopback host:port of /health/update
	NotifyReady func() bool                      // the bot can send
	System      System
	SystemFiles []string
	Nodes       Nodes
	BuiltAt     time.Time                        // when this binary and its geofiles were built
	UpdatesURL  func(ctx context.Context) string // Settings → Updates, for notifications
}

type Usecase struct {
	deps    Deps
	store   *Store
	fetch   Transport
	checker *Checker
	kind    Kind
	now     func() time.Time
	started time.Time

	trialFor   time.Duration
	pollEvery  time.Duration
	runnerGone time.Duration
	watching   atomic.Bool

	mu        sync.Mutex
	checking  bool
	nextCheck time.Time
}

func New(d Deps) *Usecase {
	u := &Usecase{deps: d, store: NewStore(d.Paths), now: time.Now, started: time.Now().UTC(),
		trialFor: trialTime, pollEvery: 2 * time.Second, runnerGone: runnerTimeout}
	u.fetch = NewRouteFetcher(d.HTTP, func() string { return u.setting(context.Background(), keyMirror) })
	u.checker = NewChecker(u.fetch, runtime.GOARCH)
	u.kind = kindOf(thisHost(d.Version, d.Paths.Root))
	u.nextCheck = u.now().Add(firstCheck)
	return u
}

// Store is the job file, shared with the runner.
func (u *Usecase) Store() *Store { return u.store }

// Kind is how this panel was installed.
func (u *Usecase) Kind() Kind { return u.kind }

// Status is the Updates section.
type Status struct {
	Current      string               `json:"current"`
	Arch         string               `json:"arch"`
	Kind         Kind                 `json:"kind"`
	Channel      string               `json:"channel"`
	CheckEnabled bool                 `json:"check_enabled"`
	Mirror       string               `json:"mirror_url"`
	TestedXray   string               `json:"tested_xray"`
	BuiltAt      *time.Time           `json:"built_at,omitempty"`
	NetPending   bool                 `json:"net_pending,omitempty"` // updates wait for a network change to be kept or reverted
	Check        *Check               `json:"check,omitempty"`
	Available    *Offer               `json:"available,omitempty"`
	Job          *domain.Job          `json:"job,omitempty"`
	Schedule     *Schedule            `json:"schedule,omitempty"`
	Rollback     *RollbackWindow      `json:"rollback,omitempty"`
	History      []domain.PanelUpdate `json:"history"`
	Parts        Parts                `json:"parts"`
}

func (u *Usecase) Status(ctx context.Context) (*Status, error) {
	st := &Status{
		Current:      u.deps.Version,
		Arch:         runtime.GOARCH,
		Kind:         u.kind,
		Channel:      u.channel(ctx),
		CheckEnabled: u.setting(ctx, keyCheckEnabled) != "false",
		Mirror:       u.setting(ctx, keyMirror),
		TestedXray:   pin.Version,
		Check:        u.lastCheck(ctx),
		NetPending:   u.deps.NetPending != nil && u.deps.NetPending(),
	}
	if !u.deps.BuiltAt.IsZero() {
		t := u.deps.BuiltAt
		st.BuiltAt = &t
	}
	if st.Check != nil && st.Check.Available {
		st.Available = st.Check.Latest
	}
	job, err := u.store.Load()
	if err != nil {
		return nil, err
	}
	st.Job = job
	st.Schedule = u.schedule(ctx)
	if !(job != nil && !job.Finished()) {
		st.Rollback = u.rollbackWindow()
	}
	// before the panel's first boot on this version the table may not exist
	if st.History, err = u.History(ctx, 20); err != nil {
		st.History = []domain.PanelUpdate{}
	}
	st.Parts = u.parts(ctx)
	return st, nil
}

// Job is the current or last job, if id matches.
func (u *Usecase) Job(id string) (*domain.Job, bool) {
	j, err := u.store.Load()
	if err != nil || j == nil || j.ID != id {
		return nil, false
	}
	return j, true
}

// Available is the version the sidebar dot and the header chip point at.
func (u *Usecase) Available(ctx context.Context) (string, bool) {
	c := u.lastCheck(ctx)
	if c == nil || !c.Available || c.Latest == nil {
		return "", false
	}
	return c.Latest.Version, true
}

// ErrNoVersion means this build has no release version to compare.
var ErrNoVersion = errors.New("this build has no release version to compare")

// CheckNow checks at once and stores the result.
func (u *Usecase) CheckNow(ctx context.Context) (*Check, error) {
	running, err := release.Parse(u.deps.Version)
	if err != nil {
		return nil, ErrNoVersion
	}
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return nil, errors.New("a check is already running")
	}
	u.checking = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.checking = false
		u.mu.Unlock()
	}()

	res := u.checker.Check(ctx, running, u.channel(ctx))
	if err := u.saveState(ctx, keyLastCheck, res); err != nil {
		return &res, err
	}
	if res.Error != "" {
		logger.GetLogger().WithField("error", res.Error).Info("[update] check failed")
	}
	if res.Available {
		u.announce(ctx, res.Latest)
	}
	return &res, nil
}

// MaybeRun is the scheduler's minute tick.
func (u *Usecase) MaybeRun(ctx context.Context) {
	u.settleJob(ctx)
	u.runSchedule(ctx)
	keep := ""
	if s := u.schedule(ctx); s != nil {
		keep = s.UploadID
	}
	u.pruneUploads(keep)
	u.mu.Lock()
	due := !u.checking && !u.now().Before(u.nextCheck)
	if due {
		u.nextCheck = u.now().Add(checkEvery + rand.N(checkJitter))
	}
	u.mu.Unlock()
	if due && u.setting(ctx, keyCheckEnabled) != "false" {
		if _, err := release.Parse(u.deps.Version); err == nil {
			_, _ = u.CheckNow(ctx)
		}
	}
}

// settleJob catches a job that ended, or runs, with nobody following it:
// one the CLI started, say.
func (u *Usecase) settleJob(ctx context.Context) {
	if u.deps.Events == nil || u.watching.Load() {
		return
	}
	job, err := u.store.Load()
	if err != nil || job == nil || job.Recorded {
		return
	}
	if job.Finished() {
		u.record(ctx, job)
		return
	}
	go u.watchRunner(ctx)
}

// announce tells the admins about a version once.
func (u *Usecase) announce(ctx context.Context, o *Offer) {
	// the CLI checks too, but only the panel can send the news
	if u.deps.Events == nil || u.setting(ctx, keyNotified) == o.Version {
		return
	}
	if err := u.saveRaw(ctx, keyNotified, o.Version); err != nil {
		return
	}
	u.publish(events.EventPanelUpdateAvailable, events.PanelUpdatePayload{
		Version: o.Version, From: u.deps.Version, Notes: o.Notes,
	})
}

func (u *Usecase) publish(t events.EventType, p events.PanelUpdatePayload) {
	if u.deps.Events == nil {
		return
	}
	if u.deps.UpdatesURL != nil {
		p.Link = u.deps.UpdatesURL(context.Background())
	}
	u.deps.Events.Publish(events.Event{Type: t, Payload: p})
}

func (u *Usecase) channel(ctx context.Context) string {
	if u.setting(ctx, keyChannel) == ChannelBeta {
		return ChannelBeta
	}
	return ChannelStable
}

func (u *Usecase) lastCheck(ctx context.Context) *Check {
	raw := u.setting(ctx, keyLastCheck)
	if raw == "" {
		return nil
	}
	var c Check
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil
	}
	return &c
}

func (u *Usecase) setting(ctx context.Context, key string) string {
	if u.deps.Settings == nil {
		return ""
	}
	s, err := u.deps.Settings.GetByKey(ctx, key)
	if err != nil || s == nil {
		return ""
	}
	return s.Value
}

func (u *Usecase) saveState(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return u.saveRaw(ctx, key, string(b))
}

// saveRaw writes a state row straight to the repository: these change on
// their own and don't belong in the settings audit trail.
func (u *Usecase) saveRaw(ctx context.Context, key, value string) error {
	if u.deps.Settings == nil {
		return nil
	}
	return u.deps.Settings.Update(ctx, &settingDomain.Setting{
		Key: key, Value: value, Type: "json", Category: "admin", Label: "Panel update state",
		Description: "Written by the updater.",
	})
}
