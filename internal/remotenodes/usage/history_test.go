// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"math"
	"testing"
	"time"
)

func unix(y int, m time.Month, d, h, min, s int) int64 {
	return time.Date(y, m, d, h, min, s, 0, time.UTC).Unix()
}

func TestADayIsAUTCDay(t *testing.T) {
	for _, c := range []struct {
		unix int64
		want day
	}{
		{0, 0}, {1, 0}, {86399, 0}, {86400, 1}, {86401, 1}, {-1, -1}, {-86400, -1}, {-86401, -2},
	} {
		if got := dayOf(c.unix); got != c.want {
			t.Errorf("dayOf(%d) = %d, want %d", c.unix, got, c.want)
		}
	}
	// midnight UTC, whatever the zone of the machine
	late, early := unix(2026, 10, 30, 23, 59, 59), unix(2026, 10, 31, 0, 0, 0)
	if dayOf(early) != dayOf(late)+1 {
		t.Errorf("the days of %d and %d are %d and %d", late, early, dayOf(late), dayOf(early))
	}
	if got := dayOf(early).String(); got != "2026-10-31" {
		t.Errorf("day = %s", got)
	}
	for _, s := range []string{"2026-10-31", "2024-02-29", "1970-01-01", "2026-01-01"} {
		d, err := parseDay(s)
		if err != nil || d.String() != s {
			t.Errorf("parseDay(%q) = %v, %v", s, d, err)
		}
	}
	for _, s := range []string{"", "2026-13-01", "2026-02-30", "x", "2026-1-1", "2026-10-31T00:00:00Z", "26-10-31"} {
		if _, err := parseDay(s); err == nil {
			t.Errorf("parseDay(%q) was accepted", s)
		}
	}
}

func TestABucketIsCountedUnderTheDayOfItsTimestamp(t *testing.T) {
	h := newNodeHist()
	before := unix(2026, 10, 30, 23, 0, 0) // the last hour of the 30th
	after := unix(2026, 10, 31, 0, 0, 0)   // the first of the 31st
	h.add(before, bucket{subs: map[uint]Totals{1: {Up: 10, Down: 100}, 2: {Up: 1, Down: 2}}, bridge: Totals{Up: 7, Down: 70}})
	h.add(after, bucket{subs: map[uint]Totals{1: {Up: 20, Down: 200}}})
	h.add(after, bucket{subs: map[uint]Totals{1: {Up: 5, Down: 5}}})

	d30, d31 := dayOf(before), dayOf(after)
	if got, want := h.days[d30], (Totals{Up: 11, Down: 102}); got != want {
		t.Errorf("node on the 30th = %+v, want %+v", got, want)
	}
	if got, want := h.days[d31], (Totals{Up: 25, Down: 205}); got != want {
		t.Errorf("node on the 31st = %+v, want %+v", got, want)
	}
	if got, want := h.subs[1][d31], (Totals{Up: 25, Down: 205}); got != want {
		t.Errorf("subscription 1 on the 31st = %+v, want %+v", got, want)
	}
	if got, want := h.subs[2][d30], (Totals{Up: 1, Down: 2}); got != want {
		t.Errorf("subscription 2 on the 30th = %+v, want %+v", got, want)
	}
	// the bridge's bytes are the same traffic again, and are kept apart
	if got, want := h.bridge, (Totals{Up: 7, Down: 70}); got != want {
		t.Errorf("bridge = %+v, want %+v", got, want)
	}
}

func TestAnEmptyBucketLeavesNoTrace(t *testing.T) {
	h := newNodeHist()
	h.add(unix(2026, 10, 30, 1, 0, 0), bucket{subs: map[uint]Totals{4: {}}})
	if !h.empty() || len(h.days) != 0 || len(h.subs) != 0 {
		t.Fatalf("a bucket of zeros left %+v", h)
	}
	h.through = 5
	if h.empty() {
		t.Fatal("a mark alone is worth keeping: it is what the agent is told")
	}
}

func TestPruneKeepsTheDaysFromTheCutoff(t *testing.T) {
	h := newNodeHist()
	today := dayOf(unix(2026, 10, 30, 12, 0, 0))
	for back := day(0); back <= 40; back++ {
		ts := int64(today-back)*secondsPerDay + 3600
		h.add(ts, subs(1, 1, 1, 2, 1, 1))
	}
	// subscription 3 has only old days
	h.add(int64(today-40)*secondsPerDay, subs(3, 9, 9))

	h.prune(today - (keepDays - 1))
	if len(h.days) != keepDays || len(h.subs[1]) != keepDays {
		t.Fatalf("days kept: node %d, subscription %d; want %d", len(h.days), len(h.subs[1]), keepDays)
	}
	if _, ok := h.days[today-30]; !ok {
		t.Error("the thirty-first day is gone")
	}
	if _, ok := h.days[today-31]; ok {
		t.Error("the day before is still there")
	}
	if _, ok := h.subs[3]; ok {
		t.Error("a subscription with no days left is still there")
	}
}

func TestSpanAddsTheDaysBothEndsIncluded(t *testing.T) {
	series := map[day]Totals{5: {Up: 1, Down: 10}, 6: {Up: 2, Down: 20}, 7: {Up: 4, Down: 40}, 8: {Up: 8, Down: 80}}
	if got, want := span(series, 6, 7), (Totals{Up: 6, Down: 60}); got != want {
		t.Errorf("span(6, 7) = %+v, want %+v", got, want)
	}
	if got := span(series, 9, 20); !got.isZero() {
		t.Errorf("span past the end = %+v", got)
	}
	if got := span(nil, 0, 100); !got.isZero() {
		t.Errorf("span of nothing = %+v", got)
	}
}

func TestCountsSaturateInsteadOfWrapping(t *testing.T) {
	big := Totals{Up: math.MaxUint64 - 1, Down: 5}
	got := big.plus(Totals{Up: 10, Down: 5})
	if got.Up != math.MaxUint64 || got.Down != 10 {
		t.Fatalf("plus = %+v", got)
	}
}
