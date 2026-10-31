// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// next is the next value on the channel, waiting for it for a while.
func next(t *testing.T, ch <-chan Job) (Job, bool) {
	t.Helper()
	select {
	case v, open := <-ch:
		return v, open
	case <-time.After(5 * time.Second):
		t.Fatal("nothing came on the channel in 5 s")
		return Job{}, false
	}
}

// waiting is the value already on the channel, if there is one. A call that
// changes a job has published by the time it returns, so no wait is needed.
func waiting(ch <-chan Job) (Job, bool) {
	select {
	case v, open := <-ch:
		return v, open
	default:
		return Job{}, false
	}
}

func subscribe(t *testing.T, e *Engine, id string) (<-chan Job, func()) {
	t.Helper()
	ch, cancel, found := e.Subscribe(id)
	if !found {
		t.Fatalf("Subscribe(%s) did not find the job", id)
	}
	t.Cleanup(cancel)
	return ch, cancel
}

func TestSubscribeStartsWithTheJobAsItStands(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 1, Target: "Node 1", Steps: []string{"a", "b"}})
	r.Begin("a")
	r.Detail("a", "12% · 1 of 8 MB")
	defer end(nil)

	ch, _ := subscribe(t, g.e, job.ID)
	first, open := waiting(ch)
	if !open {
		t.Fatal("the snapshot was not there at once")
	}
	if want, _ := g.e.Get(job.ID); !jobsEqual(first, want) {
		t.Fatalf("first value = %+v, want the job as it stands %+v", first, want)
	}
	if first.State != StateRunning || first.Steps[0].Detail != "12% · 1 of 8 MB" {
		t.Fatalf("first value = %+v", first)
	}
	if _, more := waiting(ch); more {
		t.Fatal("a second value with nothing changed")
	}
}

func TestEveryChangeIsOneEventAndNothingChangedIsNone(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindUpdateAgent, Steps: []string{"a", "b"}})
	ch, _ := subscribe(t, g.e, job.ID)
	next(t, ch) // the snapshot

	expect := func(what string, want ...Step) {
		t.Helper()
		v, open := waiting(ch)
		if !open || v.State != StateRunning || !slices.Equal(v.Steps, want) {
			t.Fatalf("%s: got %+v (open %v), want steps %v", what, v, open, want)
		}
		if extra, more := waiting(ch); more {
			t.Fatalf("%s: a second event %+v", what, extra)
		}
	}
	silent := func(what string) {
		t.Helper()
		if v, more := waiting(ch); more {
			t.Fatalf("%s: an event for nothing: %+v", what, v)
		}
	}

	r.Begin("a")
	expect("Begin", running("a"), pending("b"))
	r.Begin("a")
	silent("Begin on a step that is running")
	r.Detail("a", "1%")
	expect("Detail", Step{Name: "a", State: StepRunning, Detail: "1%"}, pending("b"))
	r.Detail("a", "1%")
	r.Set("a", StepRunning, "1%")
	silent("the same text, the same state")
	r.Done("a")
	expect("Done", Step{Name: "a", State: StepOK, Detail: "1%"}, pending("b"))
	r.Begin("c")
	expect("a new step", Step{Name: "a", State: StepOK, Detail: "1%"}, pending("b"), running("c"))
	r.Begin("")
	r.Set("d", "nonsense", "x")
	silent("a name or state that is refused")
	end(nil)
}

func TestTheChannelEndsRightAfterTheFinalState(t *testing.T) {
	for name, result := range map[string]error{"ok": nil, "failed": errors.New("it broke")} {
		g := newRig(t)
		job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a", "b"}})
		ch, _ := subscribe(t, g.e, job.ID)
		next(t, ch)
		r.Begin("a")
		next(t, ch)
		end(result)

		last, open := next(t, ch)
		if !open || last.State == StateRunning || last.FinishedAt == nil {
			t.Fatalf("%s: last value = %+v (open %v), want the final state", name, last, open)
		}
		if (name == "failed") != (last.Error != "") || string(last.State) != name {
			t.Fatalf("%s: last value = %+v", name, last)
		}
		if v, open := next(t, ch); open {
			t.Fatalf("%s: a value after the final state: %+v", name, v)
		}
		// what the channel said last is what the engine says
		if got, _ := g.e.Get(job.ID); !jobsEqual(got, last) {
			t.Fatalf("%s: the engine has %+v, the channel ended on %+v", name, got, last)
		}
	}
}

func TestSubscribingToAFinishedJobGivesItsFinalStateAndEnds(t *testing.T) {
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindRemove, Steps: []string{"a"}}, func(ctx context.Context, r *Run) error {
		r.Begin("a")
		return errors.New("no")
	})
	ch, cancel, found := g.e.Subscribe(done.ID)
	if !found {
		t.Fatal("not found")
	}
	v, open := next(t, ch)
	if !open || !jobsEqual(v, done) {
		t.Fatalf("first value = %+v, want %+v", v, done)
	}
	if _, open := next(t, ch); open {
		t.Fatal("the channel of a finished job stays open")
	}
	cancel()
	cancel()
}

func TestSubscribingToAnUnknownJobSaysSo(t *testing.T) {
	g := newRig(t)
	ch, cancel, found := g.e.Subscribe("nope")
	if found || ch != nil || cancel == nil {
		t.Fatalf("Subscribe(unknown) = %v, %v, %v", ch, cancel != nil, found)
	}
	cancel() // callable all the same
}

// A reader that never reads must not hold the job up, and must find the
// latest state when it does.
func TestAReaderThatNeverReadsGetsTheLatestAndHoldsNothingUp(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"upload"}})
	idle, _ := subscribe(t, g.e, job.ID) // never read until the end
	busy, _ := subscribe(t, g.e, job.ID)
	r.Begin("upload")

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; i < 5000; i++ {
			r.Detail("upload", fmt.Sprintf("%d%% · %d of 5000", i%100, i))
			if i%500 == 0 {
				waiting(busy) // this one keeps up now and then
			}
		}
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the job was held up by a reader that does not read")
	}

	// exactly one value is waiting, and it is the newest
	latest, _ := g.e.Get(job.ID)
	if got := len(idle); got != 1 {
		t.Fatalf("%d values waiting for the idle reader, want 1", got)
	}
	v, _ := next(t, idle)
	if !jobsEqual(v, latest) || v.Steps[0].Detail != "99% · 4999 of 5000" {
		t.Fatalf("the idle reader got %+v, want the latest %+v", v, latest)
	}
	if _, more := waiting(idle); more {
		t.Fatal("a second value")
	}

	// the end still reaches it: the final state, then the close
	end(nil)
	last, open := next(t, idle)
	if !open || last.State != StateOK {
		t.Fatalf("last = %+v (open %v)", last, open)
	}
	if _, open := next(t, idle); open {
		t.Fatal("not closed after the final state")
	}
}

// Whatever a reader sees is in the order things happened, ending on the end.
func TestAReaderSeesChangesInOrderAndEndsOnTheFinalState(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"upload"}})
	ch, _ := subscribe(t, g.e, job.ID)
	r.Begin("upload")

	var seen []int // what the text said, while the job ran
	var last Job
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for v := range ch {
			last = v
			var n int
			if _, err := fmt.Sscanf(v.Steps[0].Detail, "n=%d", &n); err == nil && v.State == StateRunning {
				seen = append(seen, n)
			}
			if len(seen)%7 == 0 {
				time.Sleep(time.Microsecond) // now and then a little slow
			}
		}
	}()
	for i := 1; i <= 2000; i++ {
		r.Detail("upload", fmt.Sprintf("n=%d", i))
		if i%10 == 0 {
			runtime.Gosched() // let the reader in, even on one processor
		}
	}
	end(nil)
	wg.Wait()

	// each change is one event, so none repeats, and a slow reader only skips
	if !slices.IsSorted(seen) || len(slices.Compact(slices.Clone(seen))) != len(seen) {
		t.Fatalf("the reader saw changes out of order or twice: %v", seen)
	}
	if len(seen) == 0 {
		t.Fatal("the reader saw no change at all")
	}
	if last.State != StateOK || last.FinishedAt == nil || last.Steps[0].Detail != "n=2000" {
		t.Fatalf("the last value = %+v", last)
	}
}

func TestEachSubscriberGetsItsOwnCopy(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	one, _ := subscribe(t, g.e, job.ID)
	two, _ := subscribe(t, g.e, job.ID)
	next(t, one)
	next(t, two)
	r.Begin("a")
	a, _ := next(t, one)
	a.Steps[0].State, a.Steps[0].Name = StepFailed, "scribbled"
	b, _ := next(t, two)
	if want := running("a"); b.Steps[0] != want {
		t.Fatalf("one reader's change reached another: %+v", b.Steps)
	}
	if now, _ := g.e.Get(job.ID); now.Steps[0] != running("a") {
		t.Fatalf("or the engine: %+v", now.Steps)
	}
	end(nil)
}

func TestCancelClosesTheChannelAndFreesTheSubscription(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	ch, cancel := subscribe(t, g.e, job.ID)
	other, _ := subscribe(t, g.e, job.ID)

	held := func() int {
		g.e.mu.Lock()
		defer g.e.mu.Unlock()
		return len(g.e.byID[job.ID].subs)
	}
	if held() != 2 {
		t.Fatalf("%d subscriptions held", held())
	}
	cancel()
	cancel() // twice is fine
	if held() != 1 {
		t.Fatalf("%d subscriptions held after cancel, want 1", held())
	}
	// the value that was waiting can still be read, then the channel ends
	if v, open := next(t, ch); !open || v.State != StateRunning {
		t.Fatalf("v = %+v (open %v)", v, open)
	}
	if _, open := next(t, ch); open {
		t.Fatal("the channel is open after cancel")
	}

	// the job goes on without it, with no send on a closed channel
	next(t, other) // its first snapshot
	r.Begin("a")
	if v, _ := next(t, other); v.Steps[0].State != StepRunning {
		t.Fatalf("the other subscriber missed an update: %+v", v)
	}
	r.Done("a")
	if v, _ := next(t, other); v.Steps[0].State != StepOK {
		t.Fatalf("the other subscriber missed an update: %+v", v)
	}
	end(nil)
	settle(t, g.e, job.ID)
	if held() != 0 {
		t.Fatalf("%d subscriptions held after the job ended", held())
	}
	cancel() // and after the end
	if v, open := next(t, other); !open || v.State != StateOK {
		t.Fatalf("the final state did not reach the other subscriber: %+v (open %v)", v, open)
	}
	if _, open := next(t, other); open {
		t.Fatal("the other channel is still open after the job ended")
	}
}

func TestCancelAndPublishRaceSafely(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				ch, cancel, found := g.e.Subscribe(job.ID)
				if !found {
					t.Error("job not found")
					return
				}
				waiting(ch)
				cancel()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			r.Detail("a", fmt.Sprint(i))
		}
	}()
	wg.Wait()
	end(nil)
	settle(t, g.e, job.ID)
}

func TestCloseEndsTheStreamsOfTheJobsItStops(t *testing.T) {
	g := newRig(t)
	job, err := g.e.Start(Spec{Kind: KindInstall, NodeID: 1}, func(ctx context.Context, r *Run) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := subscribe(t, g.e, job.ID)
	next(t, ch)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := g.e.Close(ctx); err != nil {
		t.Fatal(err)
	}
	last, open := next(t, ch)
	if !open || last.State != StateFailed {
		t.Fatalf("last = %+v (open %v)", last, open)
	}
	if _, open := next(t, ch); open {
		t.Fatal("still open")
	}
}

// No goroutine of its own: a thousand subscriptions come and go and the
// count of goroutines is where it was.
func TestCancelledSubscriptionsLeaveNoGoroutinesBehind(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	before := settledGoroutines()
	for i := 0; i < 1000; i++ {
		ch, cancel, _ := g.e.Subscribe(job.ID)
		if i%2 == 0 {
			r.Detail("a", fmt.Sprint(i))
			waiting(ch)
		}
		cancel()
	}
	if after := settledGoroutines(); after > before {
		t.Fatalf("%d goroutines before, %d after", before, after)
	}
	// and the readers a page would run on them end with cancel, or with the job
	var readers sync.WaitGroup
	cancels := make([]func(), 0, 20)
	for i := 0; i < 20; i++ {
		ch, cancel, _ := g.e.Subscribe(job.ID)
		cancels = append(cancels, cancel)
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range ch {
			}
		}()
	}
	for _, cancel := range cancels[:10] {
		cancel()
	}
	r.Detail("a", "go on")
	end(nil)
	waited := make(chan struct{})
	go func() { readers.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("a reader is still blocked after cancel or the end of the job")
	}
}

// settledGoroutines is the count once whatever was winding down has.
func settledGoroutines() int {
	n := runtime.NumGoroutine()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		m := runtime.NumGoroutine()
		if m >= n {
			break
		}
		n = m
	}
	return n
}

// A bubble ends only when its goroutines have, so a job, a stream or a
// Close that leaves one behind fails the test.
func TestNoGoroutineOutlivesTheEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		stuck := func(ctx context.Context, r *Run) error {
			r.Begin("a")
			<-ctx.Done()
			return ctx.Err()
		}
		var ids []string
		for node := uint(1); node <= 4; node++ {
			job, err := e.Start(Spec{Kind: KindInstall, NodeID: node, Steps: []string{"a"}}, stuck)
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, job.ID)
		}
		quick, _ := e.Start(Spec{Kind: KindRemove}, func(context.Context, *Run) error { return nil })
		panicky, _ := e.Start(Spec{Kind: KindRemove}, func(context.Context, *Run) error { panic("x") })
		ids = append(ids, quick.ID, panicky.ID)

		var readers sync.WaitGroup
		for _, id := range ids {
			for i := 0; i < 3; i++ {
				ch, cancel, ok := e.Subscribe(id)
				if !ok {
					t.Fatal("job not found")
				}
				if i == 0 {
					cancel()
				}
				readers.Add(1)
				go func() {
					defer readers.Done()
					for range ch {
					}
				}()
			}
		}
		synctest.Wait()
		if err := e.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		readers.Wait()
	})
}
