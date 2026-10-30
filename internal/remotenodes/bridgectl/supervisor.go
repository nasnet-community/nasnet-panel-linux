// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bridgectl runs the remote-nodes bridge program for the hub. It
// starts the program as another user with the spec on stdin, restarts it when
// it dies, keeps what it prints in memory, and answers for it: the process's
// state together with what the bridge's own status page says.
//
// It uses the standard library and the spec package, nothing of the Xray
// core, so the hub's binary stays small.
package bridgectl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// Defaults for the fields of Config left at zero.
const (
	DefaultMemLimit    = "128MiB"
	DefaultOutputBytes = 64 << 10
	DefaultStopGrace   = 5 * time.Second
	DefaultStatusWait  = time.Second
)

// ErrRunning is returned by Start when the bridge is already supervised.
var ErrRunning = errors.New("bridgectl: already started")

// Config says how to run the bridge.
type Config struct {
	// Binary is the bridge program; Args go after it.
	Binary string
	Args   []string
	// Env is the child's whole environment, apart from GOMEMLIMIT. The hub's
	// own is not passed on: it may hold things the bridge has no use for.
	Env []string
	// MemLimit is the child's GOMEMLIMIT, in the form the Go runtime reads
	// ("96MiB"). It keeps the bridge's heap in check beside the hub.
	MemLimit string
	// Credential, if set, is the user and group the child runs as. Only a hub
	// running as root on Linux can do that: anywhere else the child runs as
	// the hub's own user, and the log and Status say so.
	Credential *Credential
	// OutputBytes is how much of the child's output is kept.
	OutputBytes int
	Backoff     Backoff
	// StopGrace is how long a stopping child gets to exit before it is killed.
	StopGrace time.Duration
	// StatusWait bounds the request for the bridge's status page.
	StatusWait time.Duration
	// Logf receives the supervisor's own messages, which also go into the
	// output kept for the child. Nil means the standard logger.
	Logf func(format string, args ...any)
}

// Credential is a user and group to run the bridge as.
type Credential struct{ UID, GID uint32 }

// Backoff is the wait before a dead child is started again: Min for the
// first, doubling up to Max. A child that ran for Healthy before dying starts
// the sequence over.
type Backoff struct{ Min, Max, Healthy time.Duration }

// DefaultBackoff is a second doubling to half a minute, forgiven after a minute.
var DefaultBackoff = Backoff{Min: time.Second, Max: 30 * time.Second, Healthy: time.Minute}

// State is what the supervisor is doing.
type State string

const (
	StateStopped State = "stopped" // not started, or stopped
	StateRunning State = "running" // a child process exists
	StateBackoff State = "backoff" // the child died; a new one is due at RetryAt
)

// Status is the supervisor's state merged with the bridge's.
type Status struct {
	State State `json:"state"`
	PID   int   `json:"pid,omitempty"`
	// Started is when the current child started.
	Started time.Time `json:"started,omitzero"`
	// Restarts counts the children started after the first, whatever the
	// reason: a crash or a new spec.
	Restarts   int       `json:"restarts"`
	LastExit   string    `json:"last_exit,omitempty"`
	LastExitAt time.Time `json:"last_exit_at,omitzero"`
	RetryAt    time.Time `json:"retry_at,omitzero"`
	// Credential says whom the child runs as, or why it isn't who was asked for.
	Credential string `json:"credential,omitempty"`
	// Bridge is the bridge's own status page. It is nil while the child isn't
	// running or doesn't answer, and BridgeError says why.
	Bridge      *bridgespec.Status `json:"bridge,omitempty"`
	BridgeError string             `json:"bridge_error,omitempty"`
}

// Supervisor keeps one bridge child running. Its methods are safe for
// concurrent use.
type Supervisor struct {
	cfg    Config
	ring   *ring
	client *http.Client
	// start makes a child. It is a field so tests can put a fake in.
	start func(frame []byte) (child, error)

	// ctl serializes Start, Update and Stop; the loop never takes it.
	ctl sync.Mutex
	run *runState // the live loop, if any; guarded by mu

	mu         sync.Mutex
	state      State
	spec       *bridgespec.Spec
	frame      []byte
	pid        int
	started    time.Time
	launches   int
	lastExit   string
	lastExitAt time.Time
	retryAt    time.Time
	credential string
}

// runState is one run of the loop, from Start to Stop.
type runState struct {
	cmds chan command
	done chan struct{}
}

type command struct {
	update bool // else stop
	spec   *bridgespec.Spec
	frame  []byte
	reply  chan error
}

// New checks cfg and fills in its defaults.
func New(cfg Config) (*Supervisor, error) {
	if cfg.Binary == "" {
		return nil, errors.New("bridgectl: no bridge binary given")
	}
	if cfg.MemLimit == "" {
		cfg.MemLimit = DefaultMemLimit
	}
	if cfg.OutputBytes <= 0 {
		cfg.OutputBytes = DefaultOutputBytes
	}
	if cfg.Backoff == (Backoff{}) {
		cfg.Backoff = DefaultBackoff
	}
	if b := cfg.Backoff; b.Min <= 0 || b.Max < b.Min || b.Healthy <= 0 {
		return nil, fmt.Errorf("bridgectl: backoff %+v needs 0 < Min <= Max and a Healthy time", b)
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = DefaultStopGrace
	}
	if cfg.StatusWait <= 0 {
		cfg.StatusWait = DefaultStatusWait
	}
	s := &Supervisor{
		cfg:  cfg,
		ring: newRing(cfg.OutputBytes),
		client: &http.Client{
			Timeout: cfg.StatusWait,
			// Loopback, one request at a time, and the child may be replaced
			// between them: no proxy, no kept connection.
			Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		},
		state: StateStopped,
	}
	s.start = s.execChild
	return s, nil
}

// Start runs the bridge with spec and keeps it running until Stop. The first
// launch happens before Start returns, so a missing binary or a bad spec is
// an error here; after that, deaths are the supervisor's business.
func (s *Supervisor) Start(spec *bridgespec.Spec) error {
	frame, err := bridgespec.Encode(spec)
	if err != nil {
		return err
	}
	s.ctl.Lock()
	defer s.ctl.Unlock()
	return s.startLocked(spec, frame)
}

func (s *Supervisor) startLocked(spec *bridgespec.Spec, frame []byte) error {
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return ErrRunning
	}
	s.spec, s.frame, s.launches = spec, frame, 0
	s.lastExit, s.lastExitAt = "", time.Time{}
	s.mu.Unlock()

	ch, err := s.launch(frame)
	if err != nil {
		return err
	}
	r := &runState{cmds: make(chan command), done: make(chan struct{})}
	s.mu.Lock()
	s.run = r
	s.mu.Unlock()
	go s.loop(r, ch)
	return nil
}

// Update restarts the bridge with a new spec, without the wait a crash would
// bring. If the bridge isn't running it is started.
func (s *Supervisor) Update(spec *bridgespec.Spec) error {
	frame, err := bridgespec.Encode(spec)
	if err != nil {
		return err
	}
	s.ctl.Lock()
	defer s.ctl.Unlock()
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return s.startLocked(spec, frame)
	}
	reply := make(chan error, 1)
	select {
	case r.cmds <- command{update: true, spec: spec, frame: frame, reply: reply}:
		return <-reply
	case <-r.done:
		return errors.New("bridgectl: stopped")
	}
}

// Stop stops the bridge for good: the child gets SIGTERM and its stdin closed,
// and is killed if it hasn't gone after StopGrace. It returns once the child
// has. Stopping a stopped supervisor does nothing.
func (s *Supervisor) Stop() {
	s.ctl.Lock()
	defer s.ctl.Unlock()
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return
	}
	reply := make(chan error, 1)
	select {
	case r.cmds <- command{reply: reply}:
		<-reply
	case <-r.done:
	}
	<-r.done
}

// Status reports the supervisor and, while a child runs, asks the bridge's
// status page for the rest.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	st := Status{
		State:      s.state,
		PID:        s.pid,
		Started:    s.started,
		LastExit:   s.lastExit,
		LastExitAt: s.lastExitAt,
		RetryAt:    s.retryAt,
		Credential: s.credential,
	}
	if s.launches > 0 {
		st.Restarts = s.launches - 1
	}
	var addr string
	if s.state == StateRunning && s.spec != nil {
		addr = s.spec.StatusAddr
	}
	s.mu.Unlock()
	if st.State != StateRunning {
		st.Started = time.Time{}
	}

	if addr != "" {
		page, err := s.fetchStatus(addr)
		st.Bridge = page
		if err != nil {
			st.BridgeError = err.Error()
		}
	}
	return st
}

// Output is what the bridge printed lately, and what the supervisor said
// about it, oldest first. It holds the last OutputBytes bytes.
func (s *Supervisor) Output() string { return s.ring.String() }

func (s *Supervisor) fetchStatus(addr string) (*bridgespec.Status, error) {
	resp, err := s.client.Get("http://" + addr + bridgespec.StatusPath)
	if err != nil {
		return nil, fmt.Errorf("status page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status page answered %s", resp.Status)
	}
	var page bridgespec.Status
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&page); err != nil {
		return nil, fmt.Errorf("status page: %w", err)
	}
	return &page, nil
}

// loop owns the child. It waits for it to die, waits out the backoff, starts
// it again, and does what Update and Stop ask in between.
func (s *Supervisor) loop(r *runState, ch child) {
	defer s.finish(r)
	b := s.cfg.Backoff
	delay := b.Min // the wait after the next death
	startedAt := time.Now()

	for {
		if ch != nil {
			select {
			case <-ch.exited():
				s.noteExit(ch)
				if time.Since(startedAt) >= b.Healthy {
					delay = b.Min
				}
				ch = nil
			case c := <-r.cmds:
				if !s.handle(c, ch, &ch) {
					return
				}
				delay, startedAt = b.Min, time.Now()
				continue
			}
		}

		// No child: wait, then try again.
		timer := time.NewTimer(delay)
		s.mu.Lock()
		s.state, s.retryAt = StateBackoff, time.Now().Add(delay)
		s.mu.Unlock()
		select {
		case <-timer.C:
		case c := <-r.cmds:
			timer.Stop()
			if !s.handle(c, nil, &ch) {
				return
			}
			delay, startedAt = b.Min, time.Now()
			continue
		}
		delay = min(delay*2, b.Max)

		s.mu.Lock()
		frame := s.frame
		s.mu.Unlock()
		var err error
		if ch, err = s.launch(frame); err != nil {
			s.logf("starting the bridge failed: %v", err)
			s.mu.Lock()
			s.lastExit, s.lastExitAt = err.Error(), time.Now()
			s.mu.Unlock()
			ch = nil
			continue
		}
		startedAt = time.Now()
	}
}

// handle does what a command asks of the running child, if there is one. It
// returns false once the loop should end. For an update it leaves the new
// child in *next.
func (s *Supervisor) handle(c command, running child, next *child) (carryOn bool) {
	if running != nil {
		s.terminate(running)
	}
	if !c.update {
		c.reply <- nil
		return false
	}
	s.mu.Lock()
	s.spec, s.frame = c.spec, c.frame
	s.mu.Unlock()
	ch, err := s.launch(c.frame)
	if err != nil {
		// The old child is gone and the new one won't start: wait and retry
		// with the new spec, and tell the caller.
		s.logf("starting the bridge with the new spec failed: %v", err)
		*next = nil
	} else {
		*next = ch
	}
	c.reply <- err
	return true
}

// launch starts a child with the spec frame and records it.
func (s *Supervisor) launch(frame []byte) (child, error) {
	ch, err := s.start(frame)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.state, s.pid, s.started, s.retryAt = StateRunning, ch.pid(), time.Now(), time.Time{}
	s.launches++
	s.mu.Unlock()
	s.logf("started the bridge, pid %d", ch.pid())
	return ch, nil
}

// terminate asks the child to stop and kills it if it doesn't, then notes
// how it ended.
func (s *Supervisor) terminate(ch child) {
	ch.terminate()
	select {
	case <-ch.exited():
	case <-time.After(s.cfg.StopGrace):
		s.logf("the bridge ignored the stop for %s, killing it", s.cfg.StopGrace)
		ch.kill()
		<-ch.exited()
	}
	s.noteExit(ch)
}

func (s *Supervisor) noteExit(ch child) {
	what := "exited"
	if err := ch.err(); err != nil {
		what = err.Error()
	}
	s.mu.Lock()
	s.lastExit, s.lastExitAt = what, time.Now()
	s.state, s.pid = StateBackoff, 0
	s.mu.Unlock()
	s.logf("the bridge ended: %s", what)
}

// finish ends a run: the supervisor is stopped.
func (s *Supervisor) finish(r *runState) {
	s.mu.Lock()
	s.run = nil
	s.state, s.pid, s.retryAt = StateStopped, 0, time.Time{}
	s.mu.Unlock()
	close(r.done)
}

// logf writes to the supervisor's log and into the output the hub can show.
func (s *Supervisor) logf(format string, args ...any) {
	fmt.Fprintf(s.ring, "[bridgectl] "+format+"\n", args...)
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
		return
	}
	log.Printf("bridgectl: "+format, args...)
}

// child is a running bridge process, or a stand-in for one in tests.
type child interface {
	pid() int
	// exited is closed once the process is gone and reaped.
	exited() <-chan struct{}
	// err is how it ended; valid once exited is closed.
	err() error
	// terminate asks it to stop: SIGTERM, and its stdin closed.
	terminate()
	kill()
}

type execProc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	waitErr error
}

// execChild starts the bridge program with the spec on its stdin.
func (s *Supervisor) execChild(frame []byte) (child, error) {
	cmd := exec.Command(s.cfg.Binary, s.cfg.Args...)
	cmd.Env = append([]string{"GOMEMLIMIT=" + s.cfg.MemLimit}, s.cfg.Env...)
	cmd.Dir = "/"
	// One writer for both, so the two streams stay in order.
	cmd.Stdout, cmd.Stderr = s.ring, s.ring
	// If a stray grandchild held the pipes open, don't wait on them forever.
	cmd.WaitDelay = 2 * time.Second
	if s.cfg.Credential != nil {
		applied, note := applyCredential(cmd, *s.cfg.Credential)
		s.mu.Lock()
		s.credential = note
		s.mu.Unlock()
		if !applied {
			s.logf("%s", note)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execProc{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	// The spec goes in as the child starts. One that died first leaves a
	// broken pipe, and Wait reports how it ended.
	if _, err := stdin.Write(frame); err != nil {
		s.logf("sending the spec: %v", err)
	}
	return p, nil
}

func (p *execProc) pid() int                { return p.cmd.Process.Pid }
func (p *execProc) exited() <-chan struct{} { return p.done }
func (p *execProc) err() error              { return p.waitErr }
func (p *execProc) kill()                   { _ = p.cmd.Process.Kill() }

// terminate does both ways the bridge listens for: a signal, and the hub
// letting go of its stdin.
func (p *execProc) terminate() {
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	_ = p.stdin.Close()
}
