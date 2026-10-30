// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAJobEndsOKWhenItsFunctionReturnsNil(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 3, Target: "Node 3", Steps: []string{"release", "verify", "upload"}})
	if job.State != StateRunning || job.FinishedAt != nil || job.Kind != KindInstall || job.NodeID != 3 || job.Target != "Node 3" ||
		!job.StartedAt.Equal(t0) || job.Error != "" || len(job.ID) != 12 {
		t.Fatalf("a job as it starts = %+v", job)
	}
	if want := []Step{pending("release"), pending("verify"), pending("upload")}; !slices.Equal(job.Steps, want) {
		t.Fatalf("steps at the start = %v, want every planned one pending", job.Steps)
	}

	r.Begin("release")
	r.Detail("release", "v1.3.0")
	r.Done("release")
	r.Begin("verify")
	g.clock.Advance(90 * time.Second)
	end(nil)

	done := settle(t, g.e, job.ID)
	if done.State != StateOK || done.Error != "" || done.FinishedAt == nil || !done.FinishedAt.Equal(t0.Add(90*time.Second)) {
		t.Fatalf("a job that went well = %+v", done)
	}
	// verify was still running when the function returned, so it is closed ok;
	// upload was never reached by a job that went well, so it is not listed
	want := []Step{{Name: "release", State: StepOK, Detail: "v1.3.0"}, ok("verify")}
	if !slices.Equal(done.Steps, want) {
		t.Fatalf("steps = %v, want %v", done.Steps, want)
	}
}

func TestAJobFailsWithTheErrorItsFunctionReturns(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"release", "verify", "upload"}})
	r.Begin("release")
	r.Done("release")
	r.Begin("verify")
	g.clock.Advance(5 * time.Second)
	end(errors.New("dial tcp 203.0.113.9:22: i/o timeout"))

	done := settle(t, g.e, job.ID)
	if done.State != StateFailed || done.Error != "dial tcp 203.0.113.9:22: i/o timeout" || done.FinishedAt == nil || !done.FinishedAt.Equal(t0.Add(5*time.Second)) {
		t.Fatalf("a job that failed = %+v", done)
	}
	// the step that was running takes the blame, and the ones never reached
	// stay listed, which shows where it stopped
	want := []Step{ok("release"), {Name: "verify", State: StepFailed, Detail: "dial tcp 203.0.113.9:22: i/o timeout"}, pending("upload")}
	if !slices.Equal(done.Steps, want) {
		t.Fatalf("steps = %v, want %v", done.Steps, want)
	}
}

func TestAJobWithNoStepsEndsWithEmptySteps(t *testing.T) {
	g := newRig(t)
	for _, err := range []error{nil, errors.New("nope")} {
		done := g.run(t, Spec{Kind: KindRemove}, func(context.Context, *Run) error { return err })
		if done.Steps == nil || len(done.Steps) != 0 {
			t.Errorf("steps of a job that had none (err %v) = %#v, want an empty list", err, done.Steps)
		}
	}
}

func TestACallAfterTheJobHasEndedDoesNothing(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindRenewCert, Steps: []string{"a"}})
	r.Begin("a")
	end(nil)
	done := settle(t, g.e, job.ID)

	r.Begin("late")
	r.Detail("a", "too late")
	r.Fail("a", errors.New("too late"))
	r.Set("b", StepRunning, "too late")
	if after, _ := g.e.Get(job.ID); !jobsEqual(after, done) {
		t.Fatalf("a finished job changed:\n before %+v\n after  %+v", done, after)
	}
}

func jobsEqual(a, b Job) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func TestAStartedJobIsACopy(t *testing.T) {
	g := newRig(t)
	job, _, end := g.begin(t, Spec{Kind: KindUpdateXray, Steps: []string{"a", "b"}})
	job.Steps[0].State = StepFailed
	got, _ := g.e.Get(job.ID)
	got.Steps[1].Detail = "scribbled"
	got.State = StateFailed
	list := g.e.List()
	list[0].Steps[0].Name = "scribbled"
	if now, _ := g.e.Get(job.ID); now.State != StateRunning || !slices.Equal(now.Steps, []Step{pending("a"), pending("b")}) {
		t.Fatalf("the engine's job changed through a copy: %+v", now)
	}
	end(nil)
}

func TestAPanicFailsTheJobAndNothingElse(t *testing.T) {
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindRemove, NodeID: 5, Steps: []string{"a", "b"}}, func(ctx context.Context, r *Run) error {
		r.Begin("a")
		panic("kaboom: the secret is in here")
	})
	if done.State != StateFailed || done.Error != "internal error" {
		t.Fatalf("a job that panicked = %+v", done)
	}
	if want := []Step{{Name: "a", State: StepFailed, Detail: "internal error"}, pending("b")}; !slices.Equal(done.Steps, want) {
		t.Fatalf("steps = %v, want %v", done.Steps, want)
	}
	if body, _ := json.Marshal(done); strings.Contains(string(body), "kaboom") {
		t.Fatalf("the panic's text reached the job: %s", body)
	}
	log := g.log.String()
	if !strings.Contains(log, "job panicked") || !strings.Contains(log, "kaboom") || !strings.Contains(log, "jobs_test.go") || !strings.Contains(log, "goroutine") {
		t.Fatalf("the log should have the panic and its stack, got:\n%s", log)
	}

	// the node is free and the engine still works
	if _, busy := g.e.Running(5); busy {
		t.Fatal("the node is still held by the job that panicked")
	}
	if again := g.run(t, Spec{Kind: KindRemove, NodeID: 5}, func(context.Context, *Run) error { return nil }); again.State != StateOK {
		t.Fatalf("the next job on that node = %+v", again)
	}
}

func TestAPanicWithAnErrorValueIsJustAsPlain(t *testing.T) {
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error {
		panic(fmt.Errorf("login as root with hunter2: %w", errors.New("nope")))
	})
	if done.Error != "internal error" {
		t.Fatalf("error = %q", done.Error)
	}
}

// brokenErr is an error that is fine until it is a nil pointer: then Error
// panics, though the error is not nil.
type brokenErr struct{ msg string }

func (e *brokenErr) Error() string { return e.msg }

func TestAnErrorThatPanicsWhenReadDoesNotTakeTheHubDown(t *testing.T) {
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindRemove, NodeID: 6, Steps: []string{"a"}}, func(ctx context.Context, r *Run) error {
		r.Begin("a")
		var e *brokenErr
		return e
	})
	if done.State != StateFailed || done.Error != "internal error" {
		t.Fatalf("job = %+v", done)
	}
	if want := []Step{{Name: "a", State: StepFailed, Detail: "internal error"}}; !slices.Equal(done.Steps, want) {
		t.Fatalf("steps = %v, want %v", done.Steps, want)
	}
	if _, held := g.e.Running(6); held {
		t.Fatal("the node is still held")
	}

	// handed to Fail, the same error is told the same way and the job carries on
	carried := g.run(t, Spec{Kind: KindRemove, Steps: []string{"a"}}, func(ctx context.Context, r *Run) error {
		var e *brokenErr
		r.Fail("a", e)
		return nil
	})
	if carried.State != StateOK || !slices.Equal(carried.Steps, []Step{{Name: "a", State: StepFailed, Detail: "internal error"}}) {
		t.Fatalf("job = %+v", carried)
	}
}

func TestAFunctionThatNeverReturnsNormallyStillEndsTheJob(t *testing.T) {
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindRenewCert, NodeID: 8}, func(context.Context, *Run) error {
		runtime.Goexit()
		return nil
	})
	if done.State != StateFailed || done.Error != "internal error" {
		t.Fatalf("a job whose goroutine exited = %+v", done)
	}
	if _, held := g.e.Running(8); held {
		t.Fatal("the node is still held")
	}
}

func TestOneJobRunsPerNode(t *testing.T) {
	g := newRig(t)
	first, _, endFirst := g.begin(t, Spec{Kind: KindInstall, NodeID: 1})

	_, err := g.e.Start(Spec{Kind: KindUpdateAgent, NodeID: 1}, func(context.Context, *Run) error { return nil })
	var busy *BusyError
	if !errors.Is(err, ErrBusy) || !errors.As(err, &busy) || busy.JobID != first.ID || busy.NodeID != 1 {
		t.Fatalf("a second job on node 1: %v, want ErrBusy naming job %s", err, first.ID)
	}
	// wrapped, it is still both
	wrapped := fmt.Errorf("updating: %w", err)
	if !errors.Is(wrapped, ErrBusy) || !errors.As(wrapped, &busy) || busy.JobID != first.ID {
		t.Fatalf("wrapped busy error lost what it says: %v", wrapped)
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("the message should name the job: %q", err)
	}
	if len(g.e.List()) != 1 {
		t.Fatalf("a refused job left a record: %+v", g.e.List())
	}

	// other nodes, and jobs without a node, run beside it
	_, _, end2 := g.begin(t, Spec{Kind: KindInstall, NodeID: 2})
	_, _, endA := g.begin(t, Spec{Kind: KindRemove})
	_, _, endB := g.begin(t, Spec{Kind: KindRemove})
	if len(g.e.List()) != 4 {
		t.Fatalf("%d jobs listed, want 4", len(g.e.List()))
	}
	for node, want := range map[uint]bool{1: true, 2: true, 3: false, 0: false} {
		if _, got := g.e.Running(node); got != want {
			t.Errorf("Running(%d) = %v, want %v", node, got, want)
		}
	}
	if j, _ := g.e.Running(1); j.ID != first.ID {
		t.Errorf("Running(1) is job %s, want %s", j.ID, first.ID)
	}

	// once it ends the node takes another
	endFirst(nil)
	settle(t, g.e, first.ID)
	if _, held := g.e.Running(1); held {
		t.Fatal("node 1 is still held by a job that ended")
	}
	_, _, endNext := g.begin(t, Spec{Kind: KindUpdateAgent, NodeID: 1})
	for _, end := range []func(error){end2, endA, endB, endNext} {
		end(nil)
	}
}

func TestOnlyOneOfManyStartsOnTheSameNodeWins(t *testing.T) {
	g := newRig(t)
	hold := make(chan struct{})
	var wins, busy atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := g.e.Start(Spec{Kind: KindRenewCert, NodeID: 7}, func(ctx context.Context, r *Run) error {
				select {
				case <-hold:
				case <-ctx.Done():
				}
				return nil
			})
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrBusy):
				busy.Add(1)
			default:
				t.Errorf("Start: %v", err)
			}
		}()
	}
	wg.Wait()
	close(hold)
	if wins.Load() != 1 || busy.Load() != 49 {
		t.Fatalf("%d started and %d were told busy, want 1 and 49", wins.Load(), busy.Load())
	}
}

func TestStartRefusesWhatItCannotRun(t *testing.T) {
	g := newRig(t)
	nop := func(context.Context, *Run) error { return nil }
	for _, k := range []Kind{"", "install ", "reboot", "INSTALL"} {
		if _, err := g.e.Start(Spec{Kind: k}, nop); err == nil {
			t.Errorf("kind %q was accepted", k)
		}
	}
	if _, err := g.e.Start(Spec{Kind: KindInstall}, nil); err == nil {
		t.Error("a job with no function was accepted")
	}
	if len(g.e.List()) != 0 {
		t.Errorf("a refused job left a record: %+v", g.e.List())
	}
	for _, k := range kinds {
		if _, err := g.e.Start(Spec{Kind: k}, nop); err != nil {
			t.Errorf("kind %q was refused: %v", k, err)
		}
	}
}

func TestIDsAreRandomAndTwelveHexCharacters(t *testing.T) {
	g := newRig(t, func(c *Config) { c.Keep = 5000 })
	re := regexp.MustCompile(`^[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		job, err := g.e.Start(Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(job.ID) || seen[job.ID] {
			t.Fatalf("job %d has id %q (seen before: %v)", i, job.ID, seen[job.ID])
		}
		seen[job.ID] = true
	}
}

func TestGetListAndRunning(t *testing.T) {
	g := newRig(t)
	if list := g.e.List(); list == nil || len(list) != 0 {
		t.Fatalf("List of an empty engine = %#v, want an empty list", list)
	}
	if _, found := g.e.Get("nope"); found {
		t.Fatal("Get found a job that was never started")
	}
	if _, found := g.e.Running(0); found {
		t.Fatal("Running(0) found a job")
	}

	a, _, endA := g.begin(t, Spec{Kind: KindInstall, NodeID: 1})
	g.clock.Advance(time.Second)
	b, _, endB := g.begin(t, Spec{Kind: KindUpdateAgent, NodeID: 2})
	g.clock.Advance(time.Second)
	c, _, endC := g.begin(t, Spec{Kind: KindRemove, NodeID: 3})

	var ids []string
	for _, j := range g.e.List() {
		ids = append(ids, j.ID)
	}
	if want := []string{c.ID, b.ID, a.ID}; !slices.Equal(ids, want) {
		t.Fatalf("List = %v, want newest first %v", ids, want)
	}
	for _, j := range []Job{a, b, c} {
		if got, found := g.e.Get(j.ID); !found || got.ID != j.ID || got.Kind != j.Kind {
			t.Errorf("Get(%s) = %+v, %v", j.ID, got, found)
		}
	}
	endA(nil)
	endB(errors.New("x"))
	endC(nil)
}

func TestJobsStartedAtTheSameInstantListInTheOrderTheyStarted(t *testing.T) {
	g := newRig(t) // the clock never moves
	var made []string
	for i := 0; i < 5; i++ {
		job, _, end := g.begin(t, Spec{Kind: KindRemove})
		made = append(made, job.ID)
		defer end(nil)
	}
	var listed []string
	for _, j := range g.e.List() {
		listed = append(listed, j.ID)
	}
	slices.Reverse(made)
	if !slices.Equal(listed, made) {
		t.Fatalf("List = %v, want %v", listed, made)
	}
}

func TestOnlyTheNewestFinishedJobsAreKept(t *testing.T) {
	g := newRig(t, func(c *Config) { c.Keep = 3 })
	var ids []string
	for i := 0; i < 5; i++ {
		g.clock.Advance(time.Minute)
		done := g.run(t, Spec{Kind: KindRenewCert, NodeID: uint(i + 1)}, func(context.Context, *Run) error { return nil })
		ids = append(ids, done.ID)
	}
	var listed []string
	for _, j := range g.e.List() {
		listed = append(listed, j.ID)
	}
	if want := []string{ids[4], ids[3], ids[2]}; !slices.Equal(listed, want) {
		t.Fatalf("List = %v, want the last three, newest first: %v", listed, want)
	}
	for i, id := range ids {
		if _, found := g.e.Get(id); found != (i >= 2) {
			t.Errorf("Get(job %d) found = %v", i, found)
		}
	}
	if n := len(g.e.byID); n != 3 {
		t.Errorf("the engine holds %d jobs in its index, want 3", n)
	}
}

func TestTheDefaultsKeepTwentyJobsForAnHour(t *testing.T) {
	g := newRig(t)
	if g.e.cfg.Keep != 20 || g.e.cfg.MaxAge != time.Hour || DefaultKeep != 20 || DefaultMaxAge != time.Hour {
		t.Fatalf("defaults: keep %d for %v", g.e.cfg.Keep, g.e.cfg.MaxAge)
	}
	for i := 0; i < 25; i++ {
		g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })
	}
	if n := len(g.e.List()); n != 20 {
		t.Fatalf("%d jobs kept, want 20", n)
	}
}

func TestAFinishedJobIsForgottenAfterMaxAge(t *testing.T) {
	g := newRig(t, func(c *Config) { c.MaxAge = time.Hour })
	g.clock.Advance(10 * time.Minute)
	done := g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })
	other := g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })

	g.clock.Advance(59 * time.Minute)
	if _, found := g.e.Get(done.ID); !found {
		t.Fatal("gone after 59 minutes")
	}
	g.clock.Advance(2 * time.Minute)
	// nothing has started or ended since, so only the read can have noticed
	if _, found := g.e.Get(done.ID); found {
		t.Error("Get still has a job from over an hour ago")
	}
	if list := g.e.List(); len(list) != 0 {
		t.Errorf("List still has %d jobs from over an hour ago", len(list))
	}
	if _, found := g.e.Get(other.ID); found {
		t.Error("the other one is still there too")
	}
	if n := len(g.e.byID) + len(g.e.order); n != 0 {
		t.Errorf("the engine still holds %d references", n)
	}
}

// Age counts from when a job ended, not from when it began.
func TestAgeCountsFromTheEndOfTheJob(t *testing.T) {
	g := newRig(t, func(c *Config) { c.MaxAge = time.Hour })
	job, _, end := g.begin(t, Spec{Kind: KindInstall})
	g.clock.Advance(3 * time.Hour)
	end(nil)
	settle(t, g.e, job.ID)
	g.clock.Advance(30 * time.Minute)
	if _, found := g.e.Get(job.ID); !found {
		t.Fatal("a job that ended half an hour ago was forgotten because it began long before")
	}
}

func TestARunningJobIsNeverDropped(t *testing.T) {
	g := newRig(t, func(c *Config) { c.Keep = 1; c.MaxAge = time.Hour })
	long, _, endLong := g.begin(t, Spec{Kind: KindInstall, NodeID: 1})
	g.clock.Advance(5 * time.Hour)
	for i := 0; i < 4; i++ {
		g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })
	}
	g.clock.Advance(2 * time.Hour)

	list := g.e.List()
	if len(list) != 1 || list[0].ID != long.ID || list[0].State != StateRunning {
		t.Fatalf("List = %+v, want only the job that is still running", list)
	}
	if _, found := g.e.Get(long.ID); !found {
		t.Fatal("Get lost the running job")
	}
	if _, found := g.e.Running(1); !found {
		t.Fatal("Running lost the running job")
	}
	endLong(nil)
	settle(t, g.e, long.ID)
}

// The job that ended last is the one the page is most likely about to ask for.
func TestTheJobThatEndedLastIsKeptEvenIfItStartedFirst(t *testing.T) {
	g := newRig(t, func(c *Config) { c.Keep = 1 })
	slow, _, endSlow := g.begin(t, Spec{Kind: KindInstall, NodeID: 1})
	g.clock.Advance(time.Second)
	quick, _, endQuick := g.begin(t, Spec{Kind: KindRenewCert, NodeID: 2})
	endQuick(nil)
	settle(t, g.e, quick.ID)
	endSlow(nil)
	got := settle(t, g.e, slow.ID)
	if got.State != StateOK {
		t.Fatalf("slow = %+v", got)
	}
	if _, found := g.e.Get(quick.ID); found {
		t.Error("the job that ended first was kept over the one that ended last")
	}
}

func TestCloseCancelsRunningJobsAndWaitsForThem(t *testing.T) {
	g := newRig(t)
	var returned atomic.Bool
	started := make(chan struct{})
	job, err := g.e.Start(Spec{Kind: KindInstall, NodeID: 1, Steps: []string{"a"}}, func(ctx context.Context, r *Run) error {
		r.Begin("a")
		close(started)
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond) // the work a stopping job still has to do
		returned.Store(true)
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := g.e.Close(ctx); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if !returned.Load() {
		t.Fatal("Close came back before the job's function had returned")
	}
	// the job has its final state by the time Close returns
	got, found := g.e.Get(job.ID)
	if !found || got.State != StateFailed || got.FinishedAt == nil || !strings.Contains(got.Error, "canceled") {
		t.Fatalf("a job stopped by Close = %+v, %v", got, found)
	}

	if _, err := g.e.Start(Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close = %v, want ErrClosed", err)
	}
	if err := g.e.Close(ctx); err != nil {
		t.Fatalf("a second Close = %v", err)
	}
}

func TestCloseGivesUpWhenItsContextEnds(t *testing.T) {
	g := newRig(t)
	release := make(chan struct{})
	started := make(chan struct{})
	job, err := g.e.Start(Spec{Kind: KindInstall}, func(ctx context.Context, r *Run) error {
		close(started)
		<-release // ignores the context, as a stuck job would
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := g.e.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close of a stuck job = %v, want the deadline", err)
	}
	if got, _ := g.e.Get(job.ID); got.State != StateRunning {
		t.Fatalf("the stuck job = %+v", got)
	}
	close(release)
	settle(t, g.e, job.ID)
}

func TestCloseWithNothingRunningReturnsAtOnce(t *testing.T) {
	g := newRig(t)
	g.run(t, Spec{Kind: KindRenewCert}, func(context.Context, *Run) error { return nil })
	if err := g.e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// what it remembered is still there to read
	if len(g.e.List()) != 1 {
		t.Fatalf("List after Close = %+v", g.e.List())
	}
}

// The job's context is cancelled once its function returns, so what the
// function started with it can't outlive the job.
func TestTheContextEndsWithTheJob(t *testing.T) {
	g := newRig(t)
	var seen context.Context
	g.run(t, Spec{Kind: KindRenewCert}, func(ctx context.Context, _ *Run) error {
		seen = ctx
		return nil
	})
	select {
	case <-seen.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the context of a finished job is still live")
	}
}

func TestTheLogSaysHowEachJobStartedAndEnded(t *testing.T) {
	g := newRig(t)
	g.run(t, Spec{Kind: KindUpdateAgent, NodeID: 4}, func(context.Context, *Run) error { return nil })
	g.run(t, Spec{Kind: KindRemove, NodeID: 5}, func(context.Context, *Run) error { return errors.New("it broke") })
	log := g.log.String()
	for _, want := range []string{"job started", "kind=update-agent", "node=4", "job finished", "state=ok", "kind=remove", "state=failed", `error="it broke"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
}

// Step reports from many goroutines while others read: nothing may race, and
// the job stays within its bounds.
func TestConcurrentReportsAndReadsAreSafe(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 1, Steps: []string{"a", "b", "c"}})
	var writers, readers sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 4; w++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := 0; i < 500; i++ {
				name := fmt.Sprint("step-", (w*7+i)%50)
				r.Begin(name)
				r.Detail(name, fmt.Sprint(i, "% · working"))
				r.Set(name, StepOK, "")
				r.Fail("a", errors.New("x"))
			}
		}()
	}
	for rd := 0; rd < 3; rd++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				g.e.List()
				g.e.Get(job.ID)
				g.e.Running(1)
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	if got, _ := g.e.Get(job.ID); len(got.Steps) != MaxSteps {
		t.Errorf("%d steps after 50 names were tried, want the cap of %d", len(got.Steps), MaxSteps)
	}
	end(nil)
	settle(t, g.e, job.ID)
}

// What the page reads is the contract's Job: these are its keys, and these
// are the only ones.
func TestTheJSONIsTheContracts(t *testing.T) {
	required := strings.Fields("id kind state steps startedAt")
	optional := strings.Fields("error nodeId target finishedAt")
	keys := func(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }
	decode := func(j Job) map[string]any {
		t.Helper()
		b, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	check := func(what string, m map[string]any, wantKeys ...string) {
		t.Helper()
		slices.Sort(wantKeys)
		if got := keys(m); !slices.Equal(got, wantKeys) {
			t.Errorf("%s has the keys %v, want %v", what, got, wantKeys)
		}
		for k := range m {
			if !slices.Contains(required, k) && !slices.Contains(optional, k) {
				t.Errorf("%s has %q, which the contract does not", what, k)
			}
		}
		for _, k := range required {
			if _, there := m[k]; !there {
				t.Errorf("%s lacks %q", what, k)
			}
		}
	}

	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 2, Target: "Node 2", Steps: []string{"release", "verify", "upload"}})
	r.Begin("release")
	r.Detail("release", "62% · 8.4 of 13.6 MB")
	r.Done("verify")

	running, _ := g.e.Get(job.ID)
	m := decode(running)
	check("a running job", m, "id", "kind", "state", "steps", "startedAt", "nodeId", "target")
	if m["state"] != "running" || m["kind"] != "install" || m["nodeId"] != 2.0 || m["target"] != "Node 2" {
		t.Errorf("a running job = %v", m)
	}
	if _, has := m["finishedAt"]; has {
		t.Error("finishedAt is there while the job runs")
	}
	steps := m["steps"].([]any)
	check2 := func(i int, want ...string) {
		t.Helper()
		got := keys(steps[i].(map[string]any))
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("step %d has the keys %v, want %v", i, got, want)
		}
	}
	check2(0, "name", "state", "detail") // has a detail
	check2(1, "name", "state")           // done, no detail
	check2(2, "name", "state")           // pending, none either
	if s := steps[0].(map[string]any); s["name"] != "release" || s["state"] != "running" || s["detail"] != "62% · 8.4 of 13.6 MB" {
		t.Errorf("step 0 = %v", s)
	}

	g.clock.Advance(time.Minute)
	end(nil)
	done := settle(t, g.e, job.ID)
	m = decode(done)
	check("a finished job", m, "id", "kind", "state", "steps", "startedAt", "finishedAt", "nodeId", "target")
	for _, k := range []string{"startedAt", "finishedAt"} {
		s, _ := m[k].(string)
		at, err := time.Parse(time.RFC3339, s)
		if err != nil || !strings.HasSuffix(s, "Z") || at.Location() != time.UTC {
			t.Errorf("%s = %q, want RFC 3339 in UTC", k, s)
		}
	}
	if m["startedAt"] != "2026-10-30T12:00:00Z" || m["finishedAt"] != "2026-10-30T12:01:00Z" {
		t.Errorf("times = %v and %v", m["startedAt"], m["finishedAt"])
	}

	// a failed one has its error, and a job with no node or target leaves them out
	failed := g.run(t, Spec{Kind: KindRotateKeys}, func(context.Context, *Run) error { return errors.New("no luck") })
	m = decode(failed)
	check("a failed job", m, "id", "kind", "state", "steps", "startedAt", "finishedAt", "error")
	if m["error"] != "no luck" || m["state"] != "failed" {
		t.Errorf("a failed job = %v", m)
	}
	if got, ok := m["steps"].([]any); !ok || len(got) != 0 {
		t.Errorf("steps = %#v, want [] and not null", m["steps"])
	}
}

// The words the page switches on.
func TestTheWordsAreTheContracts(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{string(KindInstall), "install"}, {string(KindUpdateAgent), "update-agent"}, {string(KindUpdateXray), "update-xray"},
		{string(KindRenewCert), "renew-cert"}, {string(KindRotateKeys), "rotate-keys"}, {string(KindChangeSettings), "change-settings"},
		{string(KindRemove), "remove"},
		{string(StateRunning), "running"}, {string(StateOK), "ok"}, {string(StateFailed), "failed"},
		{string(StepPending), "pending"}, {string(StepRunning), "running"}, {string(StepOK), "ok"}, {string(StepFailed), "failed"},
	} {
		if c.got != c.want {
			t.Errorf("%q, want %q", c.got, c.want)
		}
	}
	if len(kinds) != 7 {
		t.Errorf("%d kinds, the contract has 7", len(kinds))
	}
}
