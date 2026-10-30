// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// clock is a clock the test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// rig is an engine on a fake clock, with its log kept.
type rig struct {
	e     *Engine
	clock *clock
	log   *syncBuf
}

func newRig(t *testing.T, mods ...func(*Config)) *rig {
	t.Helper()
	g := &rig{clock: &clock{t: t0}, log: &syncBuf{}}
	cfg := Config{Now: g.clock.Now, Log: slog.New(slog.NewTextHandler(g.log, nil))}
	for _, m := range mods {
		m(&cfg)
	}
	g.e = New(cfg)
	// no job outlives its test
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := g.e.Close(ctx); err != nil {
			t.Errorf("closing the engine: %v", err)
		}
	})
	return g
}

// begin starts a job that does nothing until it is told to end, and hands
// back its Run, so the test can report steps from its own goroutine and look
// at the job between them. end makes the function return what it is given.
func (g *rig) begin(t *testing.T, spec Spec) (job Job, r *Run, end func(error)) {
	t.Helper()
	runs := make(chan *Run, 1)
	stop := make(chan error, 1)
	job, err := g.e.Start(spec, func(ctx context.Context, r *Run) error {
		runs <- r
		select {
		case err := <-stop:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return job, <-runs, func(err error) { stop <- err }
}

// run starts a job whose function is fn, and waits for it to end.
func (g *rig) run(t *testing.T, spec Spec, fn func(context.Context, *Run) error) Job {
	t.Helper()
	job, err := g.e.Start(spec, fn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return settle(t, g.e, job.ID)
}

// settle waits for a job to end and returns it as it ended.
func settle(t *testing.T, e *Engine, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, ok := e.Get(id)
		if !ok {
			t.Fatalf("job %s is not there", id)
		}
		if j.State != StateRunning {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still running after 10 s: %+v", id, j)
		}
		time.Sleep(time.Millisecond)
	}
}

// stepsOf is what a job shows now.
func (g *rig) stepsOf(t *testing.T, id string) []Step {
	t.Helper()
	j, ok := g.e.Get(id)
	if !ok {
		t.Fatalf("job %s is not there", id)
	}
	return j.Steps
}

func ok(name string) Step      { return Step{Name: name, State: StepOK} }
func pending(name string) Step { return Step{Name: name, State: StepPending} }
func running(name string) Step { return Step{Name: name, State: StepRunning} }
