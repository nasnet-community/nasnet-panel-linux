// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// wantNode compares what the page reads for a node; the time is compared as a
// time.
func wantNode(t *testing.T, got, want NodeUsage) {
	t.Helper()
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("node updated at %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
	got.UpdatedAt, want.UpdatedAt = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("node reads %+v, want %+v", got, want)
	}
}

func filePoints(t *testing.T, dir string, node uint) *nodeHist {
	t.Helper()
	hists, err := decode(readFile(t, filepath.Join(dir, FileName)))
	must(t, err)
	return hists[node]
}

// The page shows what the newest bucket holds so far, but nothing of it is
// counted: there is nothing to write and nothing to acknowledge.
func TestTheFirstPollShowsTheNewestBucketAndWritesNothing(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 9, 50), email(1), 100, 900)
	a.setOnline(email(1), "198.51.100.7")

	s := r.poll(1)
	if !s.At.Equal(when(12, 10, 0)) || s.RateUp != 0 || s.RateDown != 0 || s.Counted != (Totals{}) || s.Online != 1 || s.Connections != 1 {
		t.Fatalf("sample = %+v", s)
	}
	wantNode(t, r.c.Node(1), NodeUsage{
		Live: true, Online: 1, Connections: 1, TodayUp: 100, TodayDown: 900, MonthUp: 100, MonthDown: 900, UpdatedAt: when(12, 10, 0),
	})
	sub := r.c.Subscription(1, 1)
	if !sub.Live || !sub.Online || sub.Connections != 1 || !sub.LastSeen.Equal(when(12, 10, 0)) || sub.TodayUp != 100 || sub.TodayDown != 900 {
		t.Errorf("subscription reads %+v", sub)
	}
	if got := r.c.Traffic(1); len(got) != 0 {
		t.Errorf("the chart has %v after one poll: there is no rate to draw yet", got)
	}

	must(t, r.flush())
	if r.c.Writes() != 0 || len(names(t, r.dir)) != 0 {
		t.Errorf("writes = %d, folder %v: nothing is complete, so nothing is counted", r.c.Writes(), names(t, r.dir))
	}
	if got := a.acked(); len(got) != 0 {
		t.Errorf("the agent was told %v: its newest bucket is still growing", got)
	}
}

func TestSteadyTrafficGivesTheRates(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 0, 0))
	// 550 bytes up and 1600 down arrive in every five seconds
	add := func(int) {
		now := r.clk.Now().Add(-time.Second)
		a.add(now, email(1), 500, 1500)
		a.add(now, email(2), 50, 100)
	}
	got := r.pollEvery(1, 5*time.Second, 6, add)
	if got[0].RateUp != 0 || got[0].RateDown != 0 {
		t.Errorf("the first poll has a rate: %+v", got[0])
	}
	for i, s := range got[1:] {
		if s.RateUp != 110 || s.RateDown != 320 {
			t.Errorf("poll %d: rate = %d up and %d down, want 110 and 320", i+2, s.RateUp, s.RateDown)
		}
	}
	n := r.c.Node(1)
	if n.RateUp != 110 || n.RateDown != 320 || n.TodayUp != 3300 || n.TodayDown != 9600 {
		t.Errorf("node reads %+v", n)
	}

	// the chart has a point for every poll that had something to compare with
	pts := r.c.Traffic(1)
	if len(pts) != 5 {
		t.Fatalf("chart = %v, want 5 points", pts)
	}
	for i, p := range pts {
		if want := when(12, 0, 10+5*i); !p.At.Equal(want) || p.Up != 110 || p.Down != 320 {
			t.Errorf("point %d = %+v, want %v with 110 and 320", i, p, want)
		}
	}

	// then it goes quiet: the rate runs down over the window, not at once
	idle := r.pollEvery(1, 5*time.Second, 3, nil)
	for i, want := range [][2]uint64{{73, 213}, {37, 107}, {0, 0}} {
		if idle[i].RateUp != want[0] || idle[i].RateDown != want[1] {
			t.Errorf("quiet poll %d: rate = %d up and %d down, want %d and %d", i+1, idle[i].RateUp, idle[i].RateDown, want[0], want[1])
		}
	}
}

func TestACompletedBucketIsCountedOnceAndThenAcknowledged(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)

	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 10, 0), email(1), 100, 900)
	r.poll(1)
	r.clk.Set(when(12, 50, 0))
	a.add(when(12, 50, 0), email(1), 50, 450)
	r.poll(1)
	if got := r.c.Node(1); got.TodayUp != 150 || got.TodayDown != 1350 {
		t.Fatalf("node reads %+v", got)
	}
	must(t, r.flush())
	if r.c.Writes() != 0 || len(a.acked()) != 0 {
		t.Fatalf("a flush with only a growing bucket wrote %d times and acknowledged %v", r.c.Writes(), a.acked())
	}

	// the next hour begins: the first bucket is complete
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	s := r.poll(1)
	if s.Counted != (Totals{Up: 150, Down: 1350}) {
		t.Fatalf("counted %+v, want the whole of the hour that ended", s.Counted)
	}
	if got := r.c.Node(1); got.TodayUp != 157 || got.TodayDown != 1420 || got.MonthUp != 157 {
		t.Fatalf("node reads %+v", got)
	}

	must(t, r.flush())
	if r.c.Writes() != 1 {
		t.Fatalf("writes = %d", r.c.Writes())
	}
	if got := a.acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Fatalf("the agent was told %v, want only the hour that ended, %d", got, at(12))
	}
	if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
		t.Fatalf("the agent holds %v, want the growing bucket %d", got, at(13))
	}
	if got := filePoints(t, r.dir, 1).through; got != at(12) {
		t.Fatalf("the file's mark is %d, want %d", got, at(12))
	}

	// nothing is counted twice or acknowledged again
	if s := r.poll(1); s.Counted != (Totals{}) {
		t.Fatalf("a second poll counted %+v", s.Counted)
	}
	must(t, r.flush())
	if r.c.Writes() != 1 || len(a.acked()) != 1 {
		t.Fatalf("writes = %d, acknowledgements = %v", r.c.Writes(), a.acked())
	}

	// traffic that arrives in the hour after the flush is not lost with it: the
	// growing bucket was never acknowledged
	r.clk.Set(when(13, 40, 0))
	a.add(when(13, 40, 0), email(1), 3, 30)
	r.poll(1)
	if got := r.c.Node(1); got.TodayUp != 160 || got.TodayDown != 1450 {
		t.Fatalf("node reads %+v", got)
	}
	r.clk.Set(when(14, 5, 0))
	a.add(when(14, 5, 0), email(1), 1, 10)
	if s := r.poll(1); s.Counted != (Totals{Up: 10, Down: 100}) {
		t.Fatalf("counted %+v, want all of hour 13", s.Counted)
	}
	must(t, r.flush())
	if got := a.acked(); !slices.Equal(got, []int64{at(12), at(13)}) {
		t.Fatalf("acknowledgements = %v", got)
	}
	if got := r.c.Node(1); got.TodayUp != 161 || got.TodayDown != 1460 {
		t.Fatalf("node reads %+v after the second flush", got)
	}
}

func TestRecordsTheAgentReturnsAgainAreNotCountedAgain(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 30, 0))
	a.add(when(12, 30, 0), email(1), 100, 900)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)

	// the agent keeps the completed bucket until it is acknowledged, so every
	// poll in between returns it
	first := r.poll(1)
	for i := 0; i < 4; i++ {
		r.clk.Advance(5 * time.Second)
		if s := r.poll(1); s.Counted != (Totals{}) {
			t.Fatalf("poll %d counted %+v again", i+2, s.Counted)
		}
	}
	if first.Counted != (Totals{Up: 100, Down: 900}) {
		t.Fatalf("the first poll counted %+v", first.Counted)
	}
	if got := r.c.Node(1); got.MonthUp != 107 || got.MonthDown != 970 {
		t.Fatalf("node reads %+v, want each byte once", got)
	}
	// the bucket is counted in memory only, so the agent must keep it: nothing
	// is acknowledged before the file is written
	if got := a.acked(); len(got) != 0 {
		t.Fatalf("the agent was told %v before anything was written", got)
	}
}

// The order is the point: the agent is told only after the file holds the
// counts, so a write that fails tells it nothing.
func TestNothingIsAcknowledgedWhenTheWriteFails(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 50, 0))
	a.add(when(12, 50, 0), email(1), 150, 1350)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)

	good := r.c.path
	r.c.path = filepath.Join(r.dir, "gone", FileName) // the folder is missing: the write fails
	if err := r.flush(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	if got := a.acked(); len(got) != 0 {
		t.Fatalf("the agent was told %v although the counts are not on disk", got)
	}
	// and a poll that finds the bucket again doesn't tell it either: the mark in memory is ahead of the disk's
	if s := r.poll(1); s.Counted != (Totals{}) {
		t.Fatalf("counted %+v again", s.Counted)
	}
	if got := a.acked(); len(got) != 0 {
		t.Fatalf("the agent was told %v by a poll", got)
	}

	r.c.path = good
	must(t, r.flush())
	if got := a.acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Fatalf("acknowledgements = %v once the disk works", got)
	}
}

func TestAGrowingBucketIsShownAsItStandsNotAddedUp(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	for i := 1; i <= 5; i++ {
		r.clk.Set(when(12, i*10, 0))
		a.add(when(12, i*10, 0), email(1), 100, 100)
		r.poll(1)
		if got := r.c.Node(1); got.TodayUp != uint64(100*i) || got.MonthUp != uint64(100*i) {
			t.Fatalf("poll %d: node reads %+v, want the bucket as it stands, %d", i, got, 100*i)
		}
	}
	must(t, r.flush())
	if r.c.Writes() != 0 || len(a.acked()) != 0 {
		t.Fatalf("writes = %d, acknowledgements = %v", r.c.Writes(), a.acked())
	}
}

// The hub dies after the file is written and before the agent is told. The
// agent still has the bucket, and the hub that starts again must not count it
// a second time.
func TestACrashBetweenTheWriteAndTheAcknowledgement(t *testing.T) {
	for _, landsAnyway := range []bool{false, true} {
		name := "the acknowledgement never arrived"
		if landsAnyway {
			name = "the acknowledgement arrived and its answer was lost"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			a := r.agent(1)
			r.clk.Set(when(12, 50, 0))
			a.add(when(12, 50, 0), email(1), 150, 1350)
			r.poll(1)
			r.clk.Set(when(13, 5, 0))
			a.add(when(13, 5, 0), email(1), 7, 70)
			r.poll(1)

			a.failAcks, a.landsAnyway = []error{errBoom}, landsAnyway
			err := r.flush()
			if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "node 1") {
				t.Fatalf("flush error = %v", err)
			}
			if r.c.Writes() != 1 {
				t.Fatalf("writes = %d: the file is written before anyone is told", r.c.Writes())
			}
			if got := a.held(); landsAnyway == slices.Contains(got, at(12)) {
				t.Fatalf("the agent holds %v", got)
			}

			// the hub dies here; the agent has not changed
			r.reopen()
			wantNode(t, r.c.Node(1), NodeUsage{TodayUp: 150, TodayDown: 1350, MonthUp: 150, MonthDown: 1350})
			s := r.poll(1)
			if s.Counted != (Totals{}) || s.RateUp != 0 {
				t.Fatalf("the first poll after the restart: %+v", s)
			}
			// the 150 are from the file, the 7 from the bucket that is still growing
			if got := r.c.Node(1); got.TodayUp != 157 || got.TodayDown != 1420 || got.MonthUp != 157 {
				t.Fatalf("node reads %+v, want every byte once", got)
			}
			if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
				t.Fatalf("the agent holds %v after the poll, want only %d", got, at(13))
			}
			if !landsAnyway {
				// the poll found the bucket the file already counts, dropped it and said so
				if got := a.acked(); len(got) != 2 || got[0] != at(12) || got[1] != at(12) {
					t.Fatalf("acknowledgements = %v: the failed one and the poll's", got)
				}
			}
			must(t, r.flush())
			if r.c.Writes() != 0 {
				t.Fatalf("the restarted hub wrote the file %d times for nothing", r.c.Writes())
			}
		})
	}
}

func TestAFailedAcknowledgementIsTriedAgainByTheNextFlush(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 50, 0))
	a.add(when(12, 50, 0), email(1), 150, 1350)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)

	a.failAcks = []error{errBoom}
	if err := r.flush(); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v", err)
	}
	must(t, r.flush())
	if got := a.acked(); !slices.Equal(got, []int64{at(12), at(12)}) {
		t.Fatalf("acknowledgements = %v", got)
	}
	if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
		t.Fatalf("the agent holds %v", got)
	}
	if r.c.Writes() != 1 {
		t.Fatalf("writes = %d: the retry has nothing to write", r.c.Writes())
	}
	must(t, r.flush())
	if len(a.acked()) != 2 {
		t.Fatalf("a flush after the retry told the agent again: %v", a.acked())
	}
	if got := r.c.Node(1); got.MonthUp != 157 {
		t.Fatalf("node reads %+v", got)
	}
}

// An agent that restarts from a copy of its buffer older than the last
// acknowledgement has buckets again that were dropped, and has to be told again.
func TestBucketsTheAgentBringsBackAreDroppedAgain(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 50, 0))
	a.add(when(12, 50, 0), email(1), 150, 1350)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)
	must(t, r.flush())
	if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
		t.Fatalf("the agent holds %v", got)
	}

	// the same bucket is back
	a.add(when(12, 50, 0), email(1), 150, 1350)
	r.clk.Advance(5 * time.Second)
	if s := r.poll(1); s.Counted != (Totals{}) {
		t.Fatalf("counted %+v again", s.Counted)
	}
	if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
		t.Fatalf("the agent holds %v: the bucket should have been dropped again", got)
	}
	if got := a.acked(); len(got) != 2 || got[1] != at(12) {
		t.Fatalf("acknowledgements = %v", got)
	}
	if got := r.c.Node(1); got.MonthUp != 157 || got.MonthDown != 1420 {
		t.Fatalf("node reads %+v", got)
	}
}

// An agent that lost its buffer starts the newest bucket again below what the
// page was showing. That is a loss of what was never written, not a reason for
// a huge rate or a wrapped count.
func TestAWipedAgentBufferIsNotMistakenForTraffic(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 10, 0), email(1), 1000, 10000)
	r.poll(1)
	r.clk.Set(when(12, 10, 5))
	a.add(when(12, 10, 4), email(1), 200, 2000)
	s := r.poll(1)
	if s.RateUp != 40 || s.RateDown != 400 {
		t.Fatalf("rate = %+v", s)
	}

	a.wipe()
	r.clk.Set(when(12, 10, 10))
	a.add(when(12, 10, 9), email(1), 30, 300)
	s = r.poll(1)
	if s.RateUp > 40 || s.RateDown > 400 {
		t.Fatalf("the wipe shows as a rate of %d and %d", s.RateUp, s.RateDown)
	}
	if got := r.c.Node(1); got.TodayUp != 30 || got.TodayDown != 300 {
		t.Fatalf("node reads %+v, want the new bucket as it stands", got)
	}

	// and it carries on from there
	r.clk.Set(when(12, 10, 15))
	a.add(when(12, 10, 14), email(1), 100, 1000)
	r.poll(1)
	if got := r.c.Node(1); got.TodayUp != 130 || got.RateUp == 0 || got.RateUp > 40 {
		t.Fatalf("node reads %+v", got)
	}
	must(t, r.flush())
	if r.c.Writes() != 0 || len(a.acked()) != 0 {
		t.Fatalf("writes = %d, acknowledgements = %v: nothing was complete", r.c.Writes(), a.acked())
	}
}

func TestAnHourThatEndsBetweenPollsIsCountedWhole(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 59, 55))
	a.add(when(12, 59, 50), email(1), 400, 4000)
	r.poll(1)

	// 100 more fall in the old hour, 30 in the new one, before the next poll
	r.clk.Set(when(13, 0, 5))
	a.add(when(12, 59, 59), email(1), 100, 1000)
	a.add(when(13, 0, 2), email(1), 30, 300)
	s := r.poll(1)
	if s.Counted != (Totals{Up: 500, Down: 5000}) {
		t.Fatalf("counted %+v, want the final hour, 500 and 5000", s.Counted)
	}
	// 130 new bytes in ten seconds
	if s.RateUp != 13 || s.RateDown != 130 {
		t.Fatalf("rate = %d and %d, want 13 and 130", s.RateUp, s.RateDown)
	}
	if got := r.c.Node(1); got.TodayUp != 530 || got.TodayDown != 5300 {
		t.Fatalf("node reads %+v", got)
	}
}

func TestTheDayChangesAtMidnightUTC(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(23, 30, 0))
	a.add(when(23, 30, 0), email(1), 10, 100)
	r.poll(1)
	wantNode(t, r.c.Node(1), NodeUsage{Live: true, TodayUp: 10, TodayDown: 100, MonthUp: 10, MonthDown: 100, UpdatedAt: when(23, 30, 0)})

	// the first traffic of the 31st: the hour of the 30th is complete
	r.clk.Set(time.Date(2026, 10, 31, 0, 10, 0, 0, time.UTC))
	a.add(time.Date(2026, 10, 31, 0, 5, 0, 0, time.UTC), email(1), 3, 30)
	if s := r.poll(1); s.Counted != (Totals{Up: 10, Down: 100}) {
		t.Fatalf("counted %+v", s.Counted)
	}
	if got := r.c.Node(1); got.TodayUp != 3 || got.TodayDown != 30 || got.MonthUp != 13 || got.MonthDown != 130 {
		t.Fatalf("on the 31st the node reads %+v, want 3 today and 13 in the month", got)
	}
	must(t, r.flush())
	days := filePoints(t, r.dir, 1).days
	if len(days) != 1 || days[dayOf(at(23))] != (Totals{Up: 10, Down: 100}) {
		t.Fatalf("the file holds %v: the hour belongs to the 30th", days)
	}

	// the day goes on and ends without a poll: reads follow the clock
	for _, c := range []struct {
		now              time.Time
		todayUp, monthUp uint64
	}{
		{time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC), 3, 13},
		{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 0, 13},
		// the month is thirty days with today among them: on the 29th of November it begins on the 31st of October
		{time.Date(2026, 11, 29, 12, 0, 0, 0, time.UTC), 0, 3},
		{time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), 0, 0},
	} {
		r.clk.Set(c.now)
		if got := r.c.Node(1); got.TodayUp != c.todayUp || got.MonthUp != c.monthUp {
			t.Errorf("at %v the node reads %d today and %d in the month, want %d and %d", c.now, got.TodayUp, got.MonthUp, c.todayUp, c.monthUp)
		}
		if got := r.c.Subscription(1, 1); got.TodayUp != c.todayUp || got.MonthUp != c.monthUp {
			t.Errorf("at %v the subscription reads %d today and %d in the month, want %d and %d", c.now, got.TodayUp, got.MonthUp, c.todayUp, c.monthUp)
		}
	}
}

func TestAHubThatWasAwayCatchesUp(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	// three days of hourly buckets, 1 up and 10 down each, and the hour that has begun
	for h := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC); h.Before(when(12, 0, 0)); h = h.Add(time.Hour) {
		a.add(h.Add(time.Minute), email(1), 1, 10)
	}
	a.add(when(12, 5, 0), email(1), 5, 50)

	r.clk.Set(when(12, 30, 0))
	s := r.poll(1)
	if s.Counted != (Totals{Up: 72, Down: 720}) {
		t.Fatalf("counted %+v, want 72 hours", s.Counted)
	}
	if got := r.c.Node(1); got.TodayUp != 17 || got.TodayDown != 170 || got.MonthUp != 77 || got.MonthDown != 770 {
		t.Fatalf("node reads %+v, want 17 today (12 hours and the growing one) and 77 in the month", got)
	}
	if s.RateUp != 0 {
		t.Fatalf("three days of traffic arrived in one poll and made a rate of %d", s.RateUp)
	}
	must(t, r.flush())
	if got := a.acked(); !slices.Equal(got, []int64{at(11)}) {
		t.Fatalf("acknowledgements = %v, want through %d", got, at(11))
	}
	days := filePoints(t, r.dir, 1).days
	if len(days) != 4 || days[dayOf(at(1))] != (Totals{Up: 12, Down: 120}) || days[dayOf(at(1)-86400)] != (Totals{Up: 24, Down: 240}) {
		t.Fatalf("days in the file: %v", days)
	}
}

func TestRecordsAreReadForWhatCounts(t *testing.T) {
	rec := func(ts int64, up, down map[string]int64) *agent.TrafficRecord {
		return &agent.TrafficRecord{Timestamp: ts, UserUplink: up, UserDownlink: down}
	}
	bridge1, bridge9 := nodecfg.BridgeEmail(1), nodecfg.BridgeEmail(9)
	in := []*agent.TrafficRecord{
		rec(300, map[string]int64{email(1): 10, email(2): 20, bridge1: 1000, bridge9: 5, "alice@example.com": 7, "sub-0@remote-nodes": 3, "sub-01@remote-nodes": 4}, map[string]int64{email(1): 100, bridge1: 2000, "alice@example.com": 7}),
		nil,
		rec(0, map[string]int64{email(1): 1}, nil),
		rec(-5, map[string]int64{email(1): 1}, nil),
		rec(100, map[string]int64{email(1): 1}, map[string]int64{email(3): -50, email(1): 4}),
		rec(300, map[string]int64{email(1): 5}, nil), // a second record under 300 is added to the first
		rec(500, map[string]int64{email(1): 1}, nil), // the limit itself is fine
		rec(501, map[string]int64{email(1): 1}, nil),
		rec(900, map[string]int64{email(1): 1}, nil),
	}
	got, ahead := records(in, 1, 500)
	if ahead != 2 {
		t.Errorf("ahead = %d, want the ones dated 501 and 900", ahead)
	}
	if len(got) != 3 || got[0].ts != 100 || got[1].ts != 300 || got[2].ts != 500 {
		t.Fatalf("records = %+v, want 100, 300 and 500 in that order", got)
	}
	if want := (Totals{Up: 1, Down: 4}); got[0].b.subs[1] != want || got[0].node != want || len(got[0].b.subs) != 1 {
		t.Errorf("first record: %+v (the negative count is nothing)", got[0])
	}
	b := got[1].b
	if b.subs[1] != (Totals{Up: 15, Down: 100}) || b.subs[2] != (Totals{Up: 20}) || len(b.subs) != 2 {
		t.Errorf("subscriptions in the second: %+v", b.subs)
	}
	if b.bridge != (Totals{Up: 1000, Down: 2000}) {
		t.Errorf("bridge = %+v, want this node's only", b.bridge)
	}
	if got[1].node != (Totals{Up: 35, Down: 100}) {
		t.Errorf("node total = %+v, want the subscriptions' only", got[1].node)
	}
}

func TestUnknownEmailsAndTheBridgeAreKeptOutOfTheSubscriptions(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 30, 0))
	now := when(12, 30, 0)
	a.add(now, email(1), 10, 100)
	a.add(now, email(2), 20, 200)
	a.add(now, nodecfg.BridgeEmail(1), 1000, 2000) // this node's bridge: the same bytes again
	a.add(now, nodecfg.BridgeEmail(9), 5, 5)       // another node's bridge has no business here
	a.add(now, "alice@example.com", 7, 7)
	a.add(now, "sub-0@remote-nodes", 3, 3)
	a.add(now, "sub-01@remote-nodes", 4, 4)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 1, 1)
	s := r.poll(1)

	if s.Counted != (Totals{Up: 30, Down: 300}) {
		t.Fatalf("counted %+v: the subscriptions' bytes and nobody else's", s.Counted)
	}
	if got := r.c.Bridge(1); got != (Totals{Up: 1000, Down: 2000}) {
		t.Errorf("bridge = %+v", got)
	}
	if got := r.c.Node(1); got.TodayUp != 31 || got.TodayDown != 301 {
		t.Errorf("node reads %+v: the bridge's bytes are not usage", got)
	}
	if got := r.c.Subscription(1, 1); got.TodayUp != 11 || got.TodayDown != 101 {
		t.Errorf("subscription 1 reads %+v", got)
	}
	if got := r.c.Subscription(2, 1); got.TodayUp != 20 || got.TodayDown != 200 {
		t.Errorf("subscription 2 reads %+v", got)
	}
	if got := r.c.Subscription(3, 1); got != (SubUsage{Live: true}) {
		t.Errorf("subscription 3 reads %+v", got)
	}
	// the other node's bridge is nobody's here
	r.agent(2).add(now, nodecfg.BridgeEmail(1), 77, 77)
	r.clk.Set(when(13, 6, 0))
	r.agent(2).add(when(13, 6, 0), email(1), 1, 1)
	r.poll(2)
	if got := r.c.Bridge(2); got != (Totals{}) {
		t.Errorf("node 2's bridge = %+v", got)
	}
}

func TestANodeThatStopsAnsweringIsNotLive(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 0, 0))
	a.setOnline(email(1), "198.51.100.7")
	r.pollEvery(1, 5*time.Second, 4, func(int) { a.add(r.clk.Now().Add(-time.Second), email(1), 500, 1500) })
	before := r.c.Node(1)
	if !before.Live || before.RateUp != 100 || before.Online != 1 {
		t.Fatalf("node reads %+v before it stops answering", before)
	}
	chart := r.c.Traffic(1)

	a.failGet = errBoom
	for i := 1; i <= 3; i++ {
		r.clk.Advance(5 * time.Second)
		_, err := r.c.Poll(context.Background(), 1, a)
		if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "node 1") {
			t.Fatalf("poll %d: err = %v", i, err)
		}
		got := r.c.Node(1)
		if i < 3 && (!got.Live || got.RateUp != before.RateUp || got.Online != 1) {
			t.Fatalf("after %d missed polls the node reads %+v: it is not given up on yet", i, got)
		}
	}
	got := r.c.Node(1)
	if got.Live || got.RateUp != 0 || got.RateDown != 0 || got.Online != 0 || got.Connections != 0 {
		t.Fatalf("after three missed polls the node reads %+v", got)
	}
	// what it had is kept, the time it last answered too
	if got.TodayUp != before.TodayUp || got.MonthDown != before.MonthDown || !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("the history changed: %+v, was %+v", got, before)
	}
	sub := r.c.Subscription(1, 1)
	if sub.Live || sub.Online || sub.Connections != 0 || sub.LastSeen.IsZero() || sub.TodayUp != before.TodayUp {
		t.Fatalf("subscription reads %+v", sub)
	}
	if total := r.c.SubscriptionTotal(1); total.Live || total.Online || total.TodayUp != before.TodayUp {
		t.Fatalf("subscription total reads %+v", total)
	}
	if !slices.Equal(r.c.Traffic(1), chart) {
		t.Fatalf("the chart changed while the node was away")
	}

	// it comes back: live at once, and the rate starts afresh
	a.failGet = nil
	r.clk.Advance(5 * time.Second)
	a.add(r.clk.Now().Add(-time.Second), email(1), 500, 1500)
	s := r.poll(1)
	if got := r.c.Node(1); !got.Live || s.RateUp != 0 || got.Online != 1 {
		t.Fatalf("first poll back: %+v, node %+v", s, got)
	}
	r.clk.Advance(5 * time.Second)
	a.add(r.clk.Now().Add(-time.Second), email(1), 500, 1500)
	if s := r.poll(1); s.RateUp != 100 {
		t.Fatalf("second poll back: rate = %d, want 100", s.RateUp)
	}

	// the misses are counted from the last answer: one failure now is one, not four
	a.failGet = errBoom
	r.clk.Advance(5 * time.Second)
	if _, err := r.c.Poll(context.Background(), 1, a); err == nil {
		t.Fatal("no error")
	}
	if !r.c.Node(1).Live {
		t.Fatal("a node that was answering until now is given up on after one failed poll")
	}
}

func TestHowManyMissedPollsMakeANodeNotLive(t *testing.T) {
	r := newRig(t)
	r.cfg.MissedPolls = 1
	r.reopen()
	a := r.agent(1)
	r.poll(1)
	a.failGet = errBoom
	if _, err := r.c.Poll(context.Background(), 1, a); err == nil {
		t.Fatal("no error")
	}
	if r.c.Node(1).Live {
		t.Fatal("one missed poll is enough when that is the setting")
	}
	// a node that has never answered is not live either
	if _, err := r.c.Poll(context.Background(), 2, a); err == nil {
		t.Fatal("no error")
	}
	if r.c.Node(2).Live {
		t.Fatal("a node that never answered is live")
	}
}

func TestWhoIsOnlineAndHowManyConnections(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 0, 0))
	a.setOnline(email(1), "198.51.100.7", "203.0.113.9") // two addresses: one subscription, two connections
	a.setOnline(email(2))                                // Xray's counter lingering: no address, no connection
	a.setOnline(email(3), "192.0.2.4")
	a.setOnline(nodecfg.BridgeEmail(1), "198.51.100.1") // the tunnel, not a person
	a.setOnline("alice@example.com", "192.0.2.77")      // not ours
	s := r.poll(1)
	if s.Online != 2 || s.Connections != 3 {
		t.Fatalf("sample = %+v, want 2 online with 3 connections", s)
	}
	n := r.c.Node(1)
	if n.Online != 2 || n.Connections != 3 {
		t.Fatalf("node reads %+v", n)
	}
	for _, c := range []struct {
		sub    uint
		online bool
		conns  int
	}{{1, true, 2}, {2, false, 0}, {3, true, 1}, {4, false, 0}} {
		if got := r.c.Subscription(c.sub, 1); got.Online != c.online || got.Connections != c.conns || !got.Live {
			t.Errorf("subscription %d reads %+v, want online %v with %d", c.sub, got, c.online, c.conns)
		}
	}
	if got := r.c.Subscription(2, 1); !got.LastSeen.IsZero() {
		t.Errorf("subscription 2 was never online and was last seen %v", got.LastSeen)
	}

	// a sweep the agent couldn't make leaves the last answer as it was
	a.noOnline = true
	r.clk.Advance(5 * time.Second)
	if s := r.poll(1); s.Online != 2 || s.Connections != 3 {
		t.Fatalf("with no answer the sample says %+v", s)
	}

	// one it could make, with nobody on, is an answer
	a.noOnline = false
	delete(a.online, email(1))
	delete(a.online, email(3))
	r.clk.Advance(5 * time.Second)
	if s := r.poll(1); s.Online != 0 || s.Connections != 0 {
		t.Fatalf("with nobody on the sample says %+v", s)
	}
	sub := r.c.Subscription(1, 1)
	if sub.Online || !sub.LastSeen.Equal(when(12, 0, 0)) {
		t.Fatalf("subscription 1 reads %+v: it was last found online at 12:00:00", sub)
	}

	// the total puts the nodes together
	b := r.agent(2)
	b.setOnline(email(1), "203.0.113.50")
	r.clk.Advance(5 * time.Second)
	r.poll(2)
	r.poll(1)
	total := r.c.SubscriptionTotal(1)
	if !total.Live || !total.Online || total.Connections != 1 || !total.LastSeen.Equal(when(12, 0, 15)) {
		t.Fatalf("total reads %+v", total)
	}
	if one := r.c.Subscription(1, 1); one.Online {
		t.Fatalf("on node 1 it reads %+v", one)
	}
}

func TestTheChartKeepsFifteenMinutesInFiveSecondSteps(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 0, 0))
	r.pollEvery(1, 5*time.Second, 240, func(int) { a.add(r.clk.Now().Add(-time.Second), email(1), 5, 50) })
	// twenty minutes of polls: the chart holds the last fifteen
	pts := r.c.Traffic(1)
	if len(pts) != 180 {
		t.Fatalf("%d points, want 180", len(pts))
	}
	for i, p := range pts {
		if want := when(12, 20, 0).Add(time.Duration(i-179) * Step); !p.At.Equal(want) || p.Up != 1 || p.Down != 10 {
			t.Fatalf("point %d = %+v, want %v with 1 and 10", i, p, want)
		}
	}

	// the chart is read at the current time: later, older points have left it
	r.clk.Advance(10 * time.Minute)
	if got := r.c.Traffic(1); len(got) != 60 || !got[0].At.Equal(when(12, 15, 5)) {
		t.Fatalf("ten minutes on the chart has %d points from %v, want 60 from 12:15:05", len(got), got[0].At)
	}
	r.clk.Advance(20 * time.Minute)
	if got := r.c.Traffic(1); got == nil || len(got) != 0 {
		t.Fatalf("a chart with nothing in its window is %#v, want an empty list", got)
	}
	if got := r.c.Traffic(99); got == nil || len(got) != 0 {
		t.Fatalf("a node nobody knows has the chart %#v", got)
	}
}

func TestTwoPollsOfOneNodeWaitForEachOther(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	a.slow = time.Millisecond
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 5; j++ {
				if _, err := r.c.Poll(context.Background(), 1, a); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if got := a.peak(); got != 1 {
		t.Fatalf("%d polls of one node were in flight at once", got)
	}
}

func TestPollsOfDifferentNodesAndReadsRunTogether(t *testing.T) {
	r := newRig(t)
	const nodes = 3
	for id := uint(1); id <= nodes; id++ {
		r.agent(id).slow = 100 * time.Microsecond
	}
	// the hour's traffic is in place; the polls and reads race over it
	for id := uint(1); id <= nodes; id++ {
		for h := 0; h < 6; h++ {
			r.agent(id).add(when(h, 30, 0), email(int(id)), 10, 100)
		}
	}
	r.clk.Set(when(7, 0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	run := func(f func()) {
		go func() {
			defer func() { done <- struct{}{} }()
			f()
		}()
	}
	workers := 0
	for id := uint(1); id <= nodes; id++ {
		workers++
		run(func() {
			for i := 0; i < 30; i++ {
				if _, err := r.c.Poll(ctx, id, r.agent(id)); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers += 2
	stop := make(chan struct{})
	run(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			for id := uint(1); id <= nodes; id++ {
				r.c.Node(id)
				r.c.Subscription(id, id)
				r.c.SubscriptionTotal(id)
				r.c.Traffic(id)
				r.c.Bridge(id)
			}
		}
	})
	run(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.c.Flush(ctx)
			r.clk.Advance(100 * time.Millisecond)
		}
	})
	for i := 0; i < nodes; i++ {
		<-done
	}
	close(stop)
	for i := nodes; i < workers; i++ {
		<-done
	}
	cancel()

	// nothing was counted twice, whatever the order things ran in
	must(t, r.flush())
	for id := uint(1); id <= nodes; id++ {
		got := r.c.Subscription(id, id)
		if got.MonthUp != 60 || got.MonthDown != 600 {
			t.Errorf("node %d: subscription %d reads %+v, want 6 hours of 10 and 100", id, id, got)
		}
	}
}

func TestEachNodeIsAcknowledgedThroughItsOwnMark(t *testing.T) {
	r := newRig(t)
	for id, hours := range map[uint][]int{1: {12, 13}, 2: {12, 13, 14}} {
		for _, h := range hours {
			r.agent(id).add(when(h, 5, 0), email(1), 1, 1)
		}
	}
	r.clk.Set(when(14, 10, 0))
	r.poll(1)
	r.poll(2)
	must(t, r.flush())
	if got := r.agent(1).acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Errorf("node 1 was told %v", got)
	}
	if got := r.agent(2).acked(); !slices.Equal(got, []int64{at(13)}) {
		t.Errorf("node 2 was told %v", got)
	}

	// one node's agent fails to answer, and the other is not held up by it
	for id := uint(1); id <= 2; id++ {
		r.agent(id).add(when(15, 5, 0), email(1), 1, 1)
	}
	r.clk.Set(when(15, 10, 0))
	r.poll(1)
	r.poll(2)
	r.agent(2).failAcks = []error{errBoom}
	err := r.flush()
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "node 2") || strings.Contains(err.Error(), "node 1") {
		t.Fatalf("flush error = %v, want node 2's alone", err)
	}
	if got := r.agent(1).acked(); !slices.Equal(got, []int64{at(12), at(13)}) {
		t.Errorf("node 1 was told %v", got)
	}
	must(t, r.flush())
	if got := r.agent(2).acked(); !slices.Equal(got, []int64{at(13), at(14), at(14)}) {
		t.Errorf("node 2 was told %v", got)
	}
}

func TestOnlyNodesPolledSinceTheStartAreAcknowledged(t *testing.T) {
	r := newRig(t)
	for id := uint(1); id <= 2; id++ {
		r.agent(id).add(when(12, 5, 0), email(1), 1, 1)
		r.agent(id).add(when(13, 5, 0), email(1), 1, 1)
	}
	r.clk.Set(when(13, 10, 0))
	r.poll(1)
	r.poll(2)
	must(t, r.flush())

	// the hub starts again and has not heard from either node yet
	r.agent(1).acks, r.agent(2).acks = nil, nil
	for id := uint(1); id <= 2; id++ {
		// both agents come back with the bucket they were told to drop
		r.agent(id).add(when(12, 5, 0), email(1), 1, 1)
	}
	r.reopen()
	must(t, r.flush())
	if len(r.agent(1).acked())+len(r.agent(2).acked()) != 0 {
		t.Fatalf("flushed before any poll and told the agents %v and %v", r.agent(1).acked(), r.agent(2).acked())
	}
	// one is polled, and it settles what its agent holds; the other waits
	r.poll(1)
	if got := r.agent(1).acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Fatalf("node 1 was told %v", got)
	}
	must(t, r.flush())
	if len(r.agent(2).acked()) != 0 {
		t.Fatalf("node 2 was told %v without being polled", r.agent(2).acked())
	}
}

func TestForgettingANodeStopsTheTalkToItsAgent(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	a.add(when(12, 5, 0), email(1), 1, 1)
	a.add(when(13, 5, 0), email(1), 1, 1)
	r.clk.Set(when(13, 10, 0))
	r.poll(1)
	r.c.Forget(1)
	must(t, r.flush())
	if len(a.acked()) != 0 {
		t.Fatalf("the agent of a forgotten node was told %v", a.acked())
	}
	if got := r.c.Node(1); got != (NodeUsage{}) {
		t.Fatalf("node reads %+v", got)
	}
	if len(names(t, r.dir)) != 0 {
		t.Fatalf("the folder holds %v", names(t, r.dir))
	}
}

func TestForgettingASubscriptionKeepsTheNodesNumbers(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 30, 0))
	a.add(when(12, 30, 0), email(1), 10, 100)
	a.add(when(12, 30, 0), email(2), 20, 200)
	a.setOnline(email(2), "198.51.100.7")
	r.poll(1)
	r.c.ForgetSubscription(2)
	if got := r.c.Subscription(2, 1); got != (SubUsage{Live: true}) {
		t.Fatalf("subscription 2 reads %+v", got)
	}
	if got := r.c.Node(1); got.TodayUp != 30 || got.TodayDown != 300 {
		t.Fatalf("node reads %+v: what the subscription used went through the node", got)
	}
	if got := r.c.Node(1); got.Online != 0 {
		t.Fatalf("node counts the forgotten subscription as online: %+v", got)
	}
}

func TestTheFileHoldsNoEmailsAndNoCredentials(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	now := when(12, 30, 0)
	for _, e := range []string{email(1), email(2), nodecfg.BridgeEmail(1), "alice@example.com"} {
		a.add(now, e, 10, 100)
	}
	a.setOnline(email(1), "198.51.100.7")
	r.clk.Set(now)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 1, 1)
	r.poll(1)
	must(t, r.flush())
	raw := string(readFile(t, filepath.Join(r.dir, FileName)))
	for _, leak := range []string{"@", "sub-", "remote-nodes", "rvs-", "reverse", "198.51.100.7", "alice"} {
		if strings.Contains(raw, leak) {
			t.Errorf("the file contains %q: %s", leak, raw)
		}
	}
	if !strings.Contains(raw, `"bridge":{"up":10,"down":100}`) {
		t.Errorf("the bridge's totals are missing: %s", raw)
	}
}

// A bucket dated far ahead of this clock is the work of a wrong one. Counting
// it would put the mark in the future and every bucket after it would look
// counted already.
func TestBucketsFromTheFutureAreSkipped(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 30, 0))
	future := when(12, 30, 0).Add(400 * 24 * time.Hour)
	a.add(future, email(1), 999, 999) // first: adding it later would also evict the rest, as the agent's buffer does with its own clock
	a.add(when(12, 30, 0), email(1), 10, 100)
	r.poll(1)
	r.poll(1)
	r.poll(1)
	if got := r.c.Node(1); got.TodayUp != 10 || got.MonthUp != 10 {
		t.Fatalf("node reads %+v: the future bucket was counted or shown", got)
	}
	if n := strings.Count(r.logs.text(), "ahead of this clock"); n != 1 {
		t.Fatalf("the log says it %d times: %q", n, r.logs.text())
	}

	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 1, 1)
	if s := r.poll(1); s.Counted != (Totals{Up: 10, Down: 100}) {
		t.Fatalf("counted %+v", s.Counted)
	}
	must(t, r.flush())
	if got := a.acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Fatalf("acknowledgements = %v, want only through %d: the bucket from the future is left with the agent", got, at(12))
	}
	if !slices.Contains(a.held(), future.Truncate(time.Hour).Unix()) {
		t.Fatalf("the agent holds %v", a.held())
	}
}

func TestNothingIsAcknowledgedWhileTheFileIsFromANewerRelease(t *testing.T) {
	r := newRig(t)
	path := filepath.Join(r.dir, FileName)
	newer := []byte(`{"version":2,"nodes":[]}` + "\n")
	must(t, os.WriteFile(path, newer, 0o600))
	r.reopen()
	a := r.agent(1)
	a.add(when(12, 5, 0), email(1), 10, 100)
	a.add(when(13, 5, 0), email(1), 1, 1)
	r.clk.Set(when(13, 10, 0))
	r.poll(1)
	r.poll(1)
	must(t, r.flush())
	// it counts, in memory
	if got := r.c.Node(1); got.MonthUp != 11 || got.MonthDown != 101 {
		t.Fatalf("node reads %+v", got)
	}
	if len(a.acked()) != 0 || len(a.held()) != 2 {
		t.Fatalf("the agent was told %v and holds %v: what isn't on disk must stay with it", a.acked(), a.held())
	}
	if got := readFile(t, path); string(got) != string(newer) {
		t.Fatalf("the newer file was changed: %s", got)
	}
}

func TestWithoutAFolderTheAgentIsStillAcknowledged(t *testing.T) {
	c, err := Open(Config{Now: newClock(when(13, 10, 0)).Now})
	must(t, err)
	a := newAgentBuffer()
	a.add(when(12, 5, 0), email(1), 10, 100)
	a.add(when(13, 5, 0), email(1), 1, 1)
	if _, err := c.Poll(context.Background(), 1, a); err != nil {
		t.Fatal(err)
	}
	must(t, c.Flush(context.Background()))
	if got := a.acked(); !slices.Equal(got, []int64{at(12)}) {
		t.Fatalf("acknowledgements = %v", got)
	}
}

func TestABundleWithNothingInItIsStillAnAnswer(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 0, 0))
	a.setOnline(email(1), "198.51.100.7")
	r.pollEvery(1, 5*time.Second, 3, func(int) { a.add(r.clk.Now().Add(-time.Second), email(1), 500, 1500) })
	if r.c.Node(1).RateUp != 100 {
		t.Fatalf("node reads %+v", r.c.Node(1))
	}

	// the agent's collectors failed: no buffered traffic and no online list
	a.noBuffered, a.noOnline = true, true
	for i := 0; i < 5; i++ {
		r.clk.Advance(5 * time.Second)
		if _, err := r.c.Poll(context.Background(), 1, a); err != nil {
			t.Fatal(err)
		}
	}
	n := r.c.Node(1)
	if !n.Live {
		t.Fatalf("an agent that answers is live: %+v", n)
	}
	if n.RateUp != 0 {
		t.Fatalf("the rate stands at %d with no data for twenty-five seconds", n.RateUp)
	}
	if n.Online != 1 {
		t.Fatalf("the last list of who is online was dropped: %+v", n)
	}
	if n.TodayUp != 1500 {
		t.Fatalf("the newest bucket went from the page: %+v", n)
	}

	// but a list that old is not about now: past a minute without a new one
	// nobody is taken to be online, though the node still answers. The last
	// list was the third poll's, at 12:00:15.
	for i := 0; i < 7; i++ {
		r.clk.Advance(5 * time.Second)
		if _, err := r.c.Poll(context.Background(), 1, a); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.c.Node(1); !n.Live || n.Online != 1 {
		t.Fatalf("a minute after the last list the node reads %+v", n)
	}
	r.clk.Advance(5 * time.Second)
	if _, err := r.c.Poll(context.Background(), 1, a); err != nil {
		t.Fatal(err)
	}
	if n := r.c.Node(1); !n.Live || n.Online != 0 || n.Connections != 0 {
		t.Fatalf("past a minute the node reads %+v", n)
	}
	if sub := r.c.Subscription(1, 1); sub.Online || sub.LastSeen.IsZero() {
		t.Fatalf("subscription reads %+v", sub)
	}
}

func TestTheOnlineAddressesAreAlwaysAskedFor(t *testing.T) {
	r := newRig(t)
	r.poll(1)
	r.poll(1)
	a := r.agent(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.wantedIP) != 2 || !a.wantedIP[0] || !a.wantedIP[1] {
		t.Fatalf("includeOnlineIPs = %v", a.wantedIP)
	}
}

func TestTheFirstPollAfterARestartHasNoRate(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	a.add(when(12, 5, 0), email(1), 1<<30, 1<<30) // a gigabyte in the hour that is running
	r.clk.Set(when(12, 30, 0))
	r.poll(1)
	r.reopen()
	s := r.poll(1)
	if s.RateUp != 0 || s.RateDown != 0 {
		t.Fatalf("the first poll of a run made a rate: %+v", s)
	}
	if len(r.c.Traffic(1)) != 0 {
		t.Fatalf("the chart has %v", r.c.Traffic(1))
	}
	if got := r.c.Node(1); got.TodayUp != 1<<30 {
		t.Fatalf("node reads %+v", got)
	}
}

// The mark that goes to the agent is the one the file holds. A poll can take
// in another bucket between the write and the acknowledgement, and that one is
// in memory only.
func TestTheAgentIsToldTheMarkOnDiskNotTheOneInMemory(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	a.add(when(12, 5, 0), email(1), 1, 1)
	a.add(when(13, 5, 0), email(1), 1, 1)
	r.clk.Set(when(13, 10, 0))
	r.poll(1)
	must(t, r.c.Flush(context.Background())) // writes the mark at(12) and tells the agent

	a.add(when(14, 5, 0), email(1), 1, 1)
	r.clk.Set(when(14, 10, 0))
	r.poll(1) // bucket 13 is complete now: counted in memory, not on disk
	r.c.mu.RLock()
	inMemory, onDisk := r.c.nodes[1].hist.through, r.c.nodes[1].diskThrough
	r.c.mu.RUnlock()
	if inMemory != at(13) || onDisk != at(12) {
		t.Fatalf("marks in memory and on disk: %d and %d", inMemory, onDisk)
	}
	r.c.mu.Lock()
	r.c.nodes[1].acked, r.c.nodes[1].staleSeen = 0, true // as if the agent had to be told again
	r.c.mu.Unlock()
	must(t, r.c.acknowledge(context.Background()))
	if got := a.acked(); got[len(got)-1] != at(12) {
		t.Fatalf("the agent was told %v, want the last to be %d, the mark on disk", got, at(12))
	}
}

// A poll that finds buckets the file counts tells the agent to drop them. If
// that fails it is said once, not at every poll, and the next flush tries again.
func TestAnAcknowledgementThatFailsInAPollIsSaidOnce(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	a.add(when(12, 5, 0), email(1), 1, 1)
	a.add(when(13, 5, 0), email(1), 1, 1)
	r.clk.Set(when(13, 10, 0))
	r.poll(1)
	must(t, r.flush())

	// the agent comes back with the bucket it dropped, and then won't drop it
	a.add(when(12, 5, 0), email(1), 1, 1)
	a.failAcks = []error{errBoom, errBoom, errBoom}
	for i := 0; i < 3; i++ {
		r.clk.Advance(5 * time.Second)
		r.poll(1)
	}
	if n := strings.Count(r.logs.text(), "acknowledge"); n != 1 {
		t.Fatalf("the log says it %d times: %q", n, r.logs.text())
	}
	if got := r.c.Node(1); got.MonthUp != 2 {
		t.Fatalf("node reads %+v: the failures must not change the counts", got)
	}

	// it works at last, and a failure after that is news again
	r.clk.Advance(5 * time.Second)
	r.poll(1)
	if got := a.held(); !slices.Equal(got, []int64{at(13)}) {
		t.Fatalf("the agent holds %v", got)
	}
	a.add(when(12, 5, 0), email(1), 1, 1)
	a.failAcks = []error{errBoom}
	r.clk.Advance(5 * time.Second)
	r.poll(1)
	if n := strings.Count(r.logs.text(), "acknowledge"); n != 2 {
		t.Fatalf("the log says it %d times: %q", n, r.logs.text())
	}
}
