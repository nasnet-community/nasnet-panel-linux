// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

// The restart timing is tested with fake children inside a synctest bubble,
// where time is virtual: a thirty-second backoff costs nothing, and the gaps
// between starts are exact.

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// fakeChild is a bridge that lives for a set time and then dies.
type fakeChild struct {
	n        int
	done     chan struct{}
	once     sync.Once
	exitErr  error
	stubborn bool // ignores terminate; only kill ends it
}

func (c *fakeChild) pid() int                { return 1000 + c.n }
func (c *fakeChild) exited() <-chan struct{} { return c.done }
func (c *fakeChild) err() error              { return c.exitErr }
func (c *fakeChild) end(err error)           { c.once.Do(func() { c.exitErr = err; close(c.done) }) }
func (c *fakeChild) kill()                   { c.end(errors.New("signal: killed")) }
func (c *fakeChild) terminate() {
	if !c.stubborn {
		c.end(nil)
	}
}

// script decides what each start does: how long the child lives, or that it
// fails to start at all.
type script struct {
	lives    []time.Duration // by start; a start past the end lives for ever
	failures map[int]bool    // starts (counted from 0) that fail to exec
	stubborn bool

	mu     sync.Mutex
	starts []time.Time
	spec   [][]byte
	made   []*fakeChild
	began  chan time.Time // every start attempt, in order
}

func newScript() *script { return &script{failures: map[int]bool{}, began: make(chan time.Time, 100)} }

func (sc *script) start(frame []byte) (child, error) {
	sc.mu.Lock()
	n := len(sc.starts)
	now := time.Now()
	sc.starts = append(sc.starts, now)
	sc.spec = append(sc.spec, frame)
	sc.mu.Unlock()
	sc.began <- now
	if sc.failures[n] {
		return nil, fmt.Errorf("fork/exec: attempt %d refused", n)
	}
	c := &fakeChild{n: n, done: make(chan struct{}), stubborn: sc.stubborn}
	sc.mu.Lock()
	sc.made = append(sc.made, c)
	sc.mu.Unlock()
	if n < len(sc.lives) {
		life := sc.lives[n]
		go func() {
			time.Sleep(life)
			c.end(errors.New("exit status 1"))
		}()
	}
	return c, nil
}

// gaps are the times between consecutive start attempts.
func (sc *script) gaps(n int) []time.Duration {
	var out []time.Duration
	var prev time.Time
	for i := 0; i < n; i++ {
		at := <-sc.began
		if i > 0 {
			out = append(out, at.Sub(prev))
		}
		prev = at
	}
	return out
}

func fakeSupervisor(t *testing.T, sc *script, b Backoff) *Supervisor {
	t.Helper()
	s, err := New(Config{Binary: "unused", Backoff: b, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	s.start = sc.start
	return s
}

func seconds(n ...int) []time.Duration {
	out := make([]time.Duration, len(n))
	for i, v := range n {
		out[i] = time.Duration(v) * time.Second
	}
	return out
}

func TestBackoffDoublesToTheCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = make([]time.Duration, 12) // every child dies at once
		s := fakeSupervisor(t, sc, DefaultBackoff)
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		got := sc.gaps(10)
		s.Stop()
		if want := seconds(1, 2, 4, 8, 16, 30, 30, 30, 30); !slices.Equal(got, want) {
			t.Fatalf("waits between starts %v, want %v", got, want)
		}
	})
}

// A child that ran for the Healthy time before it died starts the sequence
// over; one that died a moment short of it doesn't.
func TestHealthyRunResetsTheBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		// Starts 0-2 die at once (waits 1, 2, 4); start 3 lives two minutes;
		// 4 and 5 die at once; 6 lives 59 seconds, a second short; 7 and 8 die.
		sc.lives = []time.Duration{0, 0, 0, 2 * time.Minute, 0, 0, 59 * time.Second, 0, 0, 0}
		s := fakeSupervisor(t, sc, DefaultBackoff)
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		got := sc.gaps(9)
		s.Stop()
		want := []time.Duration{
			1 * time.Second, 2 * time.Second, 4 * time.Second, // 0, 1, 2 -> 3
			2*time.Minute + 1*time.Second,  // 3 ran a healthy two minutes, so only 1 s
			2 * time.Second,                // 4 died at once: the sequence goes on
			4 * time.Second,                // 5 -> 6
			59*time.Second + 8*time.Second, // 6 ran 59 s, not healthy: 8 s, not 1
			16 * time.Second,               // 7 -> 8
		}
		if !slices.Equal(got, want) {
			t.Fatalf("gaps between starts\n got %v\nwant %v", got, want)
		}
	})
}

func TestBackoffWithTheCallersNumbers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = make([]time.Duration, 6)
		s := fakeSupervisor(t, sc, Backoff{Min: 100 * time.Millisecond, Max: 300 * time.Millisecond, Healthy: time.Second})
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		got := sc.gaps(5)
		s.Stop()
		want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 300 * time.Millisecond}
		if !slices.Equal(got, want) {
			t.Fatalf("gaps %v, want %v", got, want)
		}
	})
}

// A start that fails to exec counts as a death: the wait doubles, and the
// supervisor goes on trying.
func TestFailedLaunchesBackOffToo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = []time.Duration{0}                         // the first child dies at once
		sc.failures = map[int]bool{1: true, 2: true, 3: true} // then three starts fail
		s := fakeSupervisor(t, sc, DefaultBackoff)
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		got := sc.gaps(5)
		synctest.Wait() // start 4 has returned and the supervisor has noted it
		st := s.Status()
		s.Stop()
		if want := seconds(1, 2, 4, 8); !slices.Equal(got, want) {
			t.Fatalf("gaps %v, want %v", got, want)
		}
		if st.State != StateRunning || st.Restarts != 1 {
			// start 4 worked after three refusals
			t.Errorf("status %+v", st)
		}
	})
}

func TestStatusDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = []time.Duration{0}
		s := fakeSupervisor(t, sc, DefaultBackoff)
		at := time.Now()
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the child has died and the supervisor is waiting
		st := s.Status()
		if st.State != StateBackoff || st.PID != 0 || st.LastExit != "exit status 1" {
			t.Fatalf("status %+v", st)
		}
		if want := at.Add(time.Second); !st.RetryAt.Equal(want) {
			t.Errorf("retry at %v, want %v", st.RetryAt, want)
		}
		s.Stop()
	})
}

func TestUpdateInABackoffStartsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = []time.Duration{0, 0, 0, 0} // all die at once, and it's a long wait to the next
		s := fakeSupervisor(t, sc, Backoff{Min: time.Hour, Max: time.Hour, Healthy: time.Hour})
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		at := time.Now()
		if err := s.Update(specFor(t, "n2")); err != nil {
			t.Fatal(err)
		}
		// The new child was started at the moment of the update.
		if got := <-sc.began; !got.Equal(sc.starts[0]) {
			t.Fatalf("first start at %v, want %v", got, sc.starts[0])
		}
		second := <-sc.began
		if !second.Equal(at) {
			t.Fatalf("the update started a child at %v, want right away at %v", second, at)
		}
		// The two specs reached their children.
		if len(sc.spec) != 2 || string(sc.spec[0]) == string(sc.spec[1]) {
			t.Errorf("specs sent: %d, same: %v", len(sc.spec), len(sc.spec) == 2 && string(sc.spec[0]) == string(sc.spec[1]))
		}
		// And the sequence started over: the wait after the new child dies is the minimum.
		synctest.Wait()
		if st := s.Status(); !st.RetryAt.Equal(at.Add(time.Hour)) {
			t.Errorf("retry at %v", st.RetryAt)
		}
		s.Stop()
	})
}

func TestStopEndsAWaitingSupervisor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = []time.Duration{0}
		s := fakeSupervisor(t, sc, Backoff{Min: time.Hour, Max: time.Hour, Healthy: time.Hour})
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		at := time.Now()
		s.Stop()
		if now := time.Now(); !now.Equal(at) {
			t.Errorf("Stop waited %s for the backoff", now.Sub(at))
		}
		time.Sleep(2 * time.Hour)
		if n := len(sc.starts); n != 1 {
			t.Errorf("%d starts: the supervisor went on after Stop", n)
		}
	})
}

// A child that ignores the stop is killed when the grace is up, and not before.
func TestStopKillsAfterTheGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.stubborn = true
		s, err := New(Config{Binary: "unused", StopGrace: 5 * time.Second, Logf: func(string, ...any) {}})
		if err != nil {
			t.Fatal(err)
		}
		s.start = sc.start
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		s.Stop()
		if took := time.Since(at); took != 5*time.Second {
			t.Errorf("Stop took %s, want exactly the 5 s grace", took)
		}
		if st := s.Status(); st.LastExit != "signal: killed" {
			t.Errorf("last exit %q", st.LastExit)
		}
	})
}

// Whatever stopped it, a child that went on after the supervisor was told to
// stop would be a bridge nobody supervises.
func TestEveryChildIsGoneAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := newScript()
		sc.lives = []time.Duration{0, 0, 0}
		s := fakeSupervisor(t, sc, DefaultBackoff)
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		sc.gaps(4)
		if err := s.Update(specFor(t, "n2")); err != nil {
			t.Fatal(err)
		}
		s.Stop()
		sc.mu.Lock()
		defer sc.mu.Unlock()
		for _, c := range sc.made {
			select {
			case <-c.done:
			default:
				t.Errorf("child %d is still running", c.n)
			}
		}
	})
}
