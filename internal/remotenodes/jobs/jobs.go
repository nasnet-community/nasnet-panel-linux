// SPDX-License-Identifier: AGPL-3.0-or-later

// The shape of a job (the steps planned up front and shown as pending, a step
// the plan missed, one running job per node, a short history kept in memory)
// follows the job engine in the Telegram MTProto plugin
// (github.com/nasnet-community/mtproto-remote-plugin), which is MIT licensed:
//
//	MIT License
//
//	Copyright (c) 2026 NASNET Community
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

// Package jobs runs the plugin's long work in the background: installing a
// node, updating its agent, rotating its keys, removing it. The caller starts
// a job with a function and gets a Job back at once. The function reports its
// steps as it goes, and the page reads them, from the list or live.
//
// Jobs live in memory and nowhere else. A restart forgets them, which is fine:
// a node whose install died is the manager's business, not a job's. Finished
// jobs are kept so the page can say how they went (the newest 20, for an
// hour), and a job that is still running is never dropped.
//
// There is no cancel in v1. Close is the only thing that stops a job, at
// shutdown.
//
// The package uses the standard library only, so the hub's binary stays small.
package jobs

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"slices"
	"sync"
	"time"
)

// Kind is what a job does. These are the words the page knows.
type Kind string

const (
	KindInstall        Kind = "install"
	KindUpdateAgent    Kind = "update-agent"
	KindUpdateXray     Kind = "update-xray"
	KindRenewCert      Kind = "renew-cert"
	KindRotateKeys     Kind = "rotate-keys"
	KindChangeSettings Kind = "change-settings"
	KindRemove         Kind = "remove"
)

var kinds = [...]Kind{KindInstall, KindUpdateAgent, KindUpdateXray, KindRenewCert, KindRotateKeys, KindChangeSettings, KindRemove}

// State is where a job is.
type State string

const (
	StateRunning State = "running"
	StateOK      State = "ok"
	StateFailed  State = "failed"
)

// StepState is where one step of a job is: a job's states, and pending for a
// step it has not got to.
type StepState string

const (
	StepPending StepState = "pending"
	StepRunning StepState = "running"
	StepOK      StepState = "ok"
	StepFailed  StepState = "failed"
)

func (s StepState) valid() bool {
	return s == StepPending || s == StepRunning || s == StepOK || s == StepFailed
}

// Step is one line of a job. The page words steps itself, by Key, and shows
// Name, which is English, for a key it has no words for. Detail is the hub's
// own text and may start with a share done, as in "62% · 8.4 MB".
type Step struct {
	Key    string    `json:"key"`
	Name   string    `json:"name"`
	State  StepState `json:"state"`
	Detail string    `json:"detail,omitempty"`
}

// InstallForm is what an install was asked for, but the login's secrets: the
// page fills its form in again from it for another try.
type InstallForm struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	SSHPort   int    `json:"sshPort"`
	SSHUser   string `json:"sshUser"`
	EntryPort int    `json:"entryPort"`
}

// ErrorInfo is what a failed job's error says, in a form the page can use
// without reading its words: the numbers, names and fingerprints that go with
// the error's code. Every field is optional and a job carries only the ones its
// error has; none of them holds a secret. The hub fills it in from the typed
// errors, never by looking at the text.
type ErrorInfo struct {
	// port_in_use: the port, whose it is ("agent" or "entry") and the process that
	// holds it on the server, when the server says.
	Port    int    `json:"port,omitempty"`
	Role    string `json:"role,omitempty"`
	Process string `json:"process,omitempty"`
	// host_key_changed: the fingerprint that was agreed to, and the one the server
	// shows now.
	PinnedKey string `json:"pinnedKey,omitempty"`
	ShownKey  string `json:"shownKey,omitempty"`
	// server_has_xray: what was found ("binary", "unit", "process"), the version of
	// the binary at the standard path when it can be read, and whether it is the
	// build this plugin pins (its version and its checksum both match).
	Found       []string `json:"found,omitempty"`
	XrayVersion string   `json:"xrayVersion,omitempty"`
	SameBuild   bool     `json:"sameBuild,omitempty"`
	// server_is_node: the node that is on this server already.
	NodeID   uint   `json:"nodeId,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	// TookOver: the install had taken over the server's own Xray when it failed.
	// That Xray was stopped and removed as asked, and nothing puts it back.
	TookOver bool `json:"tookOver,omitempty"`
}

// Job is what the page reads, as the API contract has it. Everything Start,
// Get, List and Running return is a copy that the caller may change.
type Job struct {
	ID    string `json:"id"`
	Kind  Kind   `json:"kind"`
	State State  `json:"state"`
	Steps []Step `json:"steps"`
	Error string `json:"error,omitempty"`
	// Code is the failure's API code when it has one the page words, as an
	// error answer's code ("server_has_xray"). Left out otherwise.
	Code string `json:"code,omitempty"`
	// ErrorInfo goes with Code for the errors that have facts the page needs.
	ErrorInfo *ErrorInfo `json:"errorInfo,omitempty"`
	// NodeID is the node the job works on, and Target a name for it that the
	// page shows when it has no node to look up. Both are left out when empty.
	NodeID     uint       `json:"nodeId,omitempty"`
	Target     string     `json:"target,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"` // absent while it runs
	// Install is what an install job was asked for, left out of other jobs.
	Install *InstallForm `json:"install,omitempty"`
}

// What a job that runs away can hold. Text over the limit is cut and ends in
// an ellipsis; steps over the limit are not added.
const (
	MaxSteps  = 32
	MaxDetail = 200  // runes
	MaxError  = 500  // runes
	maxName   = 64   // runes
	maxPanic  = 2000 // runes of a panic's text that the log keeps
)

// Defaults for the fields of Config left at zero.
const (
	DefaultKeep   = 20
	DefaultMaxAge = time.Hour
)

var (
	// ErrBusy is what Start answers for a node that already has a job running.
	// The error itself is a *BusyError, which says which job.
	ErrBusy = errors.New("jobs: a job is already running on that node")
	// ErrClosed is what Start answers once Close has been called.
	ErrClosed = errors.New("jobs: the engine is closed")

	// errInternal is all a job says about a panic: the value may hold anything.
	errInternal = errors.New("internal error")
)

// BusyError is the error Start returns for a node with a job running. It
// matches ErrBusy, and errors.As gets the running job's id out of it.
type BusyError struct {
	NodeID uint
	JobID  string
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("jobs: node %d already has job %s running", e.NodeID, e.JobID)
}

func (e *BusyError) Unwrap() error { return ErrBusy }

// Spec says what a job is and how it should look before it starts.
type Spec struct {
	// Kind is one of the Kind constants; Start refuses anything else.
	Kind Kind
	// NodeID is the node the job works on; zero means none. Only one job runs
	// on a node at a time: two on one box step on each other.
	NodeID uint
	// Target names what the job is for before there is a node to name.
	Target string
	// Steps are the names of the steps the job expects to take, all pending
	// until it reaches them. A name that is empty or repeated is left out, and
	// so is any beyond MaxSteps.
	Steps []string
	// Secrets are values that must never show up in the job, such as the
	// password, key and passphrase of the login it uses. Whatever the job
	// reports, in a step's text or an error, has each of them replaced by "…"
	// first, and the engine forgets them when the job ends.
	Secrets []string
	// Install is what an install was asked for, with no secret in it; the
	// job carries it so that the page can offer the same install again.
	Install *InstallForm
}

// Config says how the engine runs. Every field may be left at zero.
type Config struct {
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Log hears when a job starts and ends, and the stack of a panic; nil
	// means slog.Default.
	Log *slog.Logger
	// Keep is how many finished jobs are remembered, and MaxAge for how long
	// after they ended.
	Keep   int
	MaxAge time.Duration
	// StepKey gives a step its machine key from its name, when the step is
	// created. The key is what the page words a step by, and it never changes
	// once a build has sent it. Nil gives every step an empty key.
	StepKey func(name string) string
	// ErrorCode gives a failed job the code of its error, "" for none. Nil
	// gives none.
	ErrorCode func(err error) string
	// ErrorInfo gives a failed job the facts of its error, nil for none. Nil
	// gives none.
	ErrorInfo func(err error) *ErrorInfo
}

// Engine runs jobs. Its methods are safe for concurrent use.
type Engine struct {
	cfg    Config
	ctx    context.Context // ends when Close is called
	cancel context.CancelFunc
	wg     sync.WaitGroup // one for each job whose function has not returned

	// mu guards everything below, and every job's own fields. One lock is
	// plenty for a handful of jobs, and keeps the order of changes simple.
	mu     sync.Mutex
	closed bool
	ended  uint64          // jobs finished so far, to tell which ended last
	byID   map[string]*job // every job still remembered
	order  []*job          // the same, oldest start first
	onNode map[uint]*job   // the running job of each node that has one
}

type job struct {
	view     Job                   // what the page sees; its Steps belong to the job
	hide     *hider                // nil when the job has no secrets, and once it is over
	subs     map[chan Job]struct{} // who is following it, until it ends
	finished time.Time             // when it ended, as the clock read it
	endSeq   uint64                // 1 for the first job to end, and so on
}

// snapshot is the job as the outside may hold it.
func (j *job) snapshot() Job {
	v := j.view
	v.Steps = slices.Clone(j.view.Steps)
	if v.Steps == nil {
		v.Steps = []Step{} // JSON gets [], never null
	}
	if v.Install != nil {
		f := *v.Install
		v.Install = &f
	}
	if v.ErrorInfo != nil {
		i := *v.ErrorInfo
		i.Found = slices.Clone(i.Found)
		v.ErrorInfo = &i
	}
	return v
}

// New makes an engine with no jobs.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Keep <= 0 {
		cfg.Keep = DefaultKeep
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = DefaultMaxAge
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{cfg: cfg, ctx: ctx, cancel: cancel, byID: map[string]*job{}, onNode: map[uint]*job{}}
}

// Start begins a job that runs fn in the background and returns the job as it
// stands, with every planned step pending. fn gets a context that Close ends,
// and reports through r. The job is ok when fn returns nil and failed when it
// returns an error, whose text becomes the job's. A panic in fn ends the job
// as failed with the plain error "internal error" and the stack in the log;
// one in a goroutine fn starts itself is not caught.
//
// A node has one job at a time: for a second one Start returns a *BusyError,
// which matches ErrBusy and names the job that is running.
func (e *Engine) Start(spec Spec, fn func(ctx context.Context, r *Run) error) (Job, error) {
	if !slices.Contains(kinds[:], spec.Kind) {
		return Job{}, fmt.Errorf("jobs: %q is not a kind of job", spec.Kind)
	}
	if fn == nil {
		return Job{}, errors.New("jobs: nothing to run")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return Job{}, ErrClosed
	}
	if cur := e.onNode[spec.NodeID]; spec.NodeID != 0 && cur != nil {
		e.mu.Unlock()
		return Job{}, &BusyError{NodeID: spec.NodeID, JobID: cur.view.ID}
	}
	id, err := e.newID()
	if err != nil {
		e.mu.Unlock()
		return Job{}, err
	}
	var form *InstallForm
	if spec.Install != nil {
		f := *spec.Install
		f.Name, f.Host, f.SSHUser = clip(f.Name, maxName), clip(f.Host, maxName*4), clip(f.SSHUser, maxName)
		form = &f
	}
	j := &job{view: Job{
		ID: id, Kind: spec.Kind, State: StateRunning, Steps: e.plan(spec.Steps),
		NodeID: spec.NodeID, Target: spec.Target, StartedAt: e.cfg.Now().UTC(), Install: form,
	}, hide: newHider(spec.Secrets)}
	e.byID[id] = j
	e.order = append(e.order, j)
	if spec.NodeID != 0 {
		e.onNode[spec.NodeID] = j
	}
	e.prune()
	e.wg.Add(1)
	snap := j.snapshot()
	e.mu.Unlock()

	e.cfg.Log.Info("job started", "job", id, "kind", spec.Kind, "node", spec.NodeID)
	go e.run(j, fn)
	return snap, nil
}

// newID is 6 random bytes in hex, one that no remembered job has. The caller
// holds mu.
func (e *Engine) newID() (string, error) {
	for {
		var b [6]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		if id := hex.EncodeToString(b[:]); e.byID[id] == nil {
			return id, nil
		}
	}
}

// plan makes the pending steps of a spec.
func (e *Engine) plan(names []string) []Step {
	v := Job{Steps: make([]Step, 0, len(names))}
	for _, n := range names {
		v.step(n, e.keyOf)
	}
	return v.Steps
}

// keyOf is the key a step goes by, empty when the engine has no way to say.
func (e *Engine) keyOf(name string) string {
	if e.cfg.StepKey == nil {
		return ""
	}
	return e.cfg.StepKey(name)
}

// step finds the step called name and adds it, pending and with the key that
// keyOf gives it, when the job has no such step. It returns nil for an empty
// name, and for a new name once there are MaxSteps. The pointer is good until
// the next step is added.
func (v *Job) step(name string, keyOf func(string) string) *Step {
	name = clip(name, maxName)
	if name == "" {
		return nil
	}
	for i := range v.Steps {
		if v.Steps[i].Name == name {
			return &v.Steps[i]
		}
	}
	if len(v.Steps) >= MaxSteps {
		return nil
	}
	v.Steps = append(v.Steps, Step{Key: keyOf(name), Name: name, State: StepPending})
	return &v.Steps[len(v.Steps)-1]
}

// run is a job's goroutine.
func (e *Engine) run(j *job, fn func(context.Context, *Run) error) {
	defer e.wg.Done()
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	// A function that never returns, by a panic or by runtime.Goexit, ends
	// the job as failed all the same.
	err := errInternal
	defer func() { e.finish(j, err) }()
	err = e.call(ctx, j, fn)
}

// call runs fn and turns a panic into an error.
func (e *Engine) call(ctx context.Context, j *job, fn func(context.Context, *Run) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			// the page gets only "internal error", but the log is read too
			e.mu.Lock()
			what := j.text(fmt.Sprint(p), maxPanic)
			e.mu.Unlock()
			e.cfg.Log.Error("job panicked", "job", j.view.ID, "kind", j.view.Kind, "panic", what, "stack", string(debug.Stack()))
			err = errInternal
		}
	}()
	return fn(ctx, &Run{e: e, j: j})
}

// finish ends a job with fn's result. A step still running is closed the way
// the job ends. Steps a good job never reached are dropped: they did not need
// doing. A bad one keeps them, pending, which shows where it stopped.
func (e *Engine) finish(j *job, err error) {
	// the error is read before the lock is taken: finish has nobody to
	// catch a panic, and the lock must not be left held
	raw, code := "", ""
	var info *ErrorInfo
	if err != nil {
		raw = errText(err)
		code = e.codeOf(err)
		info = e.infoOf(err)
	}
	e.mu.Lock()
	now := e.cfg.Now()
	v := &j.view
	var text, stepText string
	if err != nil {
		text, stepText = j.text(raw, MaxError), j.text(raw, MaxDetail)
	}
	for i := range v.Steps {
		if s := &v.Steps[i]; s.State == StepRunning {
			s.State = StepOK
			if err != nil {
				s.State, s.Detail = StepFailed, stepText
			}
		}
	}
	v.State = StateOK
	if err != nil {
		v.State, v.Error, v.Code = StateFailed, text, clip(code, maxName)
		v.ErrorInfo = j.cleanInfo(info)
	} else {
		v.Steps = slices.DeleteFunc(v.Steps, func(s Step) bool { return s.State == StepPending })
	}
	at := now.UTC()
	v.FinishedAt = &at
	j.finished = now
	j.hide = nil // nothing more will be reported, and the secrets need not outlive the job
	e.ended++
	j.endSeq = e.ended
	if v.NodeID != 0 && e.onNode[v.NodeID] == j {
		delete(e.onNode, v.NodeID)
	}
	e.publish(j)
	e.prune()
	id, kind, node, state, took := v.ID, v.Kind, v.NodeID, v.State, at.Sub(v.StartedAt)
	e.mu.Unlock()

	args := []any{"job", id, "kind", kind, "node", node, "state", state, "took", took.Round(time.Millisecond)}
	if text != "" {
		args = append(args, "error", text)
	}
	e.cfg.Log.Info("job finished", args...)
}

// codeOf is the code of a job's error. A panic in ErrorCode costs the job its
// code and nothing else: finish has nobody to catch it.
func (e *Engine) codeOf(err error) (code string) {
	if e.cfg.ErrorCode == nil || errors.Is(err, errInternal) {
		return ""
	}
	defer func() {
		if recover() != nil {
			code = ""
		}
	}()
	return e.cfg.ErrorCode(err)
}

// infoOf is the facts of a job's error, with the same care as codeOf.
func (e *Engine) infoOf(err error) (info *ErrorInfo) {
	if e.cfg.ErrorInfo == nil || errors.Is(err, errInternal) {
		return nil
	}
	defer func() {
		if recover() != nil {
			info = nil
		}
	}()
	return e.cfg.ErrorInfo(err)
}

// maxFact is the longest a name or a fingerprint in an ErrorInfo may be.
const maxFact = 128

// cleanInfo is info as a job may show it: a copy of its own, with the secrets
// taken out of the text in it and each text cut, and nil when it says nothing.
// The caller holds the engine's lock.
func (j *job) cleanInfo(info *ErrorInfo) *ErrorInfo {
	if info == nil {
		return nil
	}
	c := *info
	c.Process, c.NodeName = j.text(c.Process, maxFact), j.text(c.NodeName, maxName)
	c.Role, c.XrayVersion = j.text(c.Role, maxName), j.text(c.XrayVersion, maxName)
	c.PinnedKey, c.ShownKey = j.text(c.PinnedKey, maxFact), j.text(c.ShownKey, maxFact)
	c.Found = nil
	for _, f := range info.Found {
		if f = j.text(f, maxName); f != "" && len(c.Found) < 8 {
			c.Found = append(c.Found, f)
		}
	}
	if reflect.DeepEqual(c, ErrorInfo{}) {
		return nil
	}
	return &c
}

// errText is what an error says. One whose Error method panics, as a nil
// pointer inside an error value does, is as bad as a panic in the job and
// gets the same plain words.
func errText(err error) (s string) {
	defer func() {
		if recover() != nil {
			s = errInternal.Error()
		}
	}()
	return err.Error()
}

// prune forgets finished jobs that are too old, and then those beyond Keep,
// the ones that ended longest ago first. The caller holds mu.
func (e *Engine) prune() {
	now := e.cfg.Now()
	alive := 0
	e.order = slices.DeleteFunc(e.order, func(j *job) bool {
		if j.view.State == StateRunning {
			return false
		}
		if now.Sub(j.finished) > e.cfg.MaxAge {
			delete(e.byID, j.view.ID)
			return true
		}
		alive++
		return false
	})
	if alive <= e.cfg.Keep {
		return
	}
	ended := make([]*job, 0, alive)
	for _, j := range e.order {
		if j.view.State != StateRunning {
			ended = append(ended, j)
		}
	}
	slices.SortFunc(ended, func(a, b *job) int { return cmp.Compare(b.endSeq, a.endSeq) })
	drop := map[*job]bool{}
	for _, j := range ended[e.cfg.Keep:] {
		drop[j] = true
		delete(e.byID, j.view.ID)
	}
	e.order = slices.DeleteFunc(e.order, func(j *job) bool { return drop[j] })
}

// Get is the job with this id, if the engine still remembers it.
func (e *Engine) Get(id string) (Job, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	j := e.byID[id]
	if j == nil {
		return Job{}, false
	}
	return j.snapshot(), true
}

// List is every job the engine remembers, the newest start first. It is never
// nil.
func (e *Engine) List() []Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	out := make([]Job, 0, len(e.order))
	for i := len(e.order) - 1; i >= 0; i-- {
		out = append(out, e.order[i].snapshot())
	}
	return out
}

// Running is the job running on a node, if there is one.
func (e *Engine) Running(nodeID uint) (Job, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j := e.onNode[nodeID]
	if j == nil {
		return Job{}, false
	}
	return j.snapshot(), true
}

// Close ends every running job's context and waits for their functions to
// return, or for ctx to end, whichever comes first. After it Start answers
// ErrClosed. Close the engine before the HTTP server's Shutdown: an event
// stream ends when its job does, and Shutdown waits for the streams.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.cancel()
	idle := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
