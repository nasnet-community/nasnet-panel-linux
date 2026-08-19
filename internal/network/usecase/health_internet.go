package usecase

import (
	"sort"
	"sync"
	"time"
)

// Flappier than ARP: damps harder, starts optimistic so boot can't withdraw.
type internetLimits struct {
	FailsToDown int
	SuccsToUp   int
	Dwell       time.Duration
}

func defaultInternetLimits() internetLimits {
	return internetLimits{FailsToDown: 5, SuccsToUp: 12, Dwell: 120 * time.Second}
}

type internetState struct {
	down       bool
	fails      int
	successes  int
	lastDownAt time.Time
}

func (s *internetState) observe(ok bool, lim internetLimits, now time.Time) (bool, bool) {
	if ok {
		s.fails = 0
		s.successes++
		if s.down && s.successes >= lim.SuccsToUp &&
			now.Sub(s.lastDownAt) >= lim.Dwell {
			s.down = false
			return true, true
		}
		return !s.down, false
	}
	s.successes = 0
	s.fails++
	if !s.down && s.fails >= lim.FailsToDown {
		s.down = true
		s.lastDownAt = now
		return false, true
	}
	return !s.down, false
}

type HealthSample struct {
	Unix    int64   `json:"unix"`
	OKRatio float64 `json:"ok_ratio"`
	RTTms   int     `json:"rtt_ms"`
}

const ringSize = 720 // one hour at the 5s tick

type healthRing struct {
	mu   sync.Mutex
	buf  [ringSize]HealthSample
	head int
	n    int
}

func (r *healthRing) push(s HealthSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = s
	r.head = (r.head + 1) % ringSize
	if r.n < ringSize {
		r.n++
	}
}

func (r *healthRing) snapshot() []HealthSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]HealthSample, 0, r.n)
	start := (r.head - r.n + ringSize) % ringSize
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(start+i)%ringSize])
	}
	return out
}

func window(samples []HealthSample, n int) []HealthSample {
	if len(samples) > n {
		return samples[len(samples)-n:]
	}
	return samples
}

func lossPct(samples []HealthSample, n int) int {
	w := window(samples, n)
	if len(w) == 0 {
		return 0
	}
	var loss float64
	for _, s := range w {
		loss += 1 - s.OKRatio
	}
	return int(loss / float64(len(w)) * 100)
}

func medianRTT(samples []HealthSample, n int) int {
	var rtts []int
	for _, s := range window(samples, n) {
		if s.RTTms > 0 {
			rtts = append(rtts, s.RTTms)
		}
	}
	if len(rtts) == 0 {
		return 0
	}
	sort.Ints(rtts)
	return rtts[len(rtts)/2]
}

func tickSample(now time.Time, results []ProbeResult) HealthSample {
	s := HealthSample{Unix: now.Unix()}
	if len(results) == 0 {
		return s
	}
	var ok int
	var rtts []int
	for _, r := range results {
		if r.OK {
			ok++
			rtts = append(rtts, int(r.RTT.Milliseconds()))
		}
	}
	s.OKRatio = float64(ok) / float64(len(results))
	if len(rtts) > 0 {
		sort.Ints(rtts)
		s.RTTms = rtts[len(rtts)/2]
	}
	return s
}
