package usecase

import (
	"testing"
	"time"
)

func TestInternetDamperNeedsFiveFailsToDrop(t *testing.T) {
	s := &internetState{}
	lim := defaultInternetLimits()
	now := time.Unix(1000, 0)
	for i := 0; i < 4; i++ {
		if up, changed := s.observe(false, lim, now); !up || changed {
			t.Fatalf("fail %d must not flip yet", i+1)
		}
	}
	up, changed := s.observe(false, lim, now)
	if up || !changed {
		t.Fatal("fifth consecutive fail must flip to down")
	}
}

func TestInternetDamperDwellBlocksEarlyRecovery(t *testing.T) {
	s := &internetState{}
	lim := defaultInternetLimits()
	now := time.Unix(1000, 0)
	for i := 0; i < 5; i++ {
		s.observe(false, lim, now)
	}
	// 12 successes but only 60s later: dwell (120s) must hold it down.
	at := now.Add(60 * time.Second)
	for i := 0; i < 12; i++ {
		if up, _ := s.observe(true, lim, at); up {
			t.Fatal("came up inside the dwell window")
		}
	}
	// Same streak past the dwell: up. The flip fires once, so track it.
	at = now.Add(200 * time.Second)
	var up, flipped bool
	for i := 0; i < 12; i++ {
		var changed bool
		up, changed = s.observe(true, lim, at)
		flipped = flipped || changed
	}
	if !up || !flipped {
		t.Fatal("must recover after dwell + success streak")
	}
}

func TestRingWrapsAndSnapshotsOldestFirst(t *testing.T) {
	r := &healthRing{}
	for i := 0; i < 725; i++ {
		r.push(HealthSample{Unix: int64(i)})
	}
	snap := r.snapshot()
	if len(snap) != 720 {
		t.Fatalf("want 720, got %d", len(snap))
	}
	if snap[0].Unix != 5 || snap[719].Unix != 724 {
		t.Fatalf("order wrong: first=%d last=%d", snap[0].Unix, snap[719].Unix)
	}
}

func TestLossAndMedian(t *testing.T) {
	var samples []HealthSample
	// 15 clean, 5 total-loss ticks: 25% loss over the last 20.
	for i := 0; i < 15; i++ {
		samples = append(samples, HealthSample{OKRatio: 1, RTTms: 100 + i})
	}
	for i := 0; i < 5; i++ {
		samples = append(samples, HealthSample{OKRatio: 0})
	}
	if got := lossPct(samples, 20); got != 25 {
		t.Fatalf("loss: want 25, got %d", got)
	}
	// Median ignores the zero-RTT loss ticks.
	if got := medianRTT(samples, 20); got < 100 || got > 114 {
		t.Fatalf("median out of range: %d", got)
	}
	if lossPct(nil, 20) != 0 {
		t.Fatal("no samples must read as 0 loss, not 100")
	}
}

// A tunnel enabled a minute ago used to cut the pool's whole sparkline down to
// its own twelve samples.
func TestMergeHistoriesKeepsTheLongestMember(t *testing.T) {
	old := make([]HealthSample, 60)
	for i := range old {
		old[i] = HealthSample{Unix: int64(i + 1), OKRatio: 1, RTTms: 100}
	}
	fresh := []HealthSample{
		{Unix: 59, OKRatio: 0, RTTms: 0},
		{Unix: 60, OKRatio: 0, RTTms: 0},
	}

	got := mergeHistories([][]HealthSample{old, fresh})
	if len(got) != 60 {
		t.Fatalf("len = %d, want the longer member's 60", len(got))
	}
	// The old member alone covers the head, so its ratio survives untouched.
	if got[0].OKRatio != 1 || got[0].Unix != 1 {
		t.Errorf("head = %+v, want the old member's sample", got[0])
	}
	// Both cover the tail, so it averages.
	if got[59].OKRatio != 0.5 {
		t.Errorf("tail ok_ratio = %v, want 0.5", got[59].OKRatio)
	}
	if got[59].Unix != 60 {
		t.Errorf("tail unix = %d, want 60", got[59].Unix)
	}
}

// The pool sparkline's type says array. A nil marshals to null and the whole
// router page goes down with it.
func TestMergeHistoriesNeverReturnsNil(t *testing.T) {
	for _, in := range [][][]HealthSample{
		{{}},
		{{}, {}},
		{},
	} {
		if got := mergeHistories(in); got == nil {
			t.Errorf("mergeHistories(%v) = nil, want an empty slice", in)
		}
	}
}
