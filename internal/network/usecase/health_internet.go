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

// observe runs on the health tick, snapshot on whichever goroutine asks.
type internetState struct {
	mu         sync.Mutex
	down       bool
	everUp     bool
	fails      int
	successes  int
	lastDownAt time.Time
}

// everUp separates "never answered" from "missed this tick".
func (s *internetState) snapshot() (down, everUp bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down, s.everUp
}

func (s *internetState) observe(ok bool, lim internetLimits, now time.Time) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.everUp = true
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

// mergeHistories averages tail-aligned samples; members tick together, so
// index alignment from the end is honest enough for a sparkline.
func mergeHistories(hs [][]HealthSample) []HealthSample {
	if len(hs) == 0 {
		return nil
	}
	n := len(hs[0])
	for _, h := range hs[1:] {
		if len(h) < n {
			n = len(h)
		}
	}
	out := make([]HealthSample, n)
	for i := 0; i < n; i++ {
		var okSum float64
		var rttSum, rttN int
		for _, h := range hs {
			s := h[len(h)-n+i]
			okSum += s.OKRatio
			if s.RTTms > 0 {
				rttSum += s.RTTms
				rttN++
			}
			out[i].Unix = s.Unix
		}
		out[i].OKRatio = okSum / float64(len(hs))
		if rttN > 0 {
			out[i].RTTms = rttSum / rttN
		}
	}
	return out
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
