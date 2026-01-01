package usecase

import (
	"testing"
	"time"
)

// reconnectBackoff doubles each failure up to a cap of 2^5 = 32× base, then
// jitters into [delay/2, delay). We check the bounds for each failure
// count — the random component prevents asserting an exact value.
func TestHeartbeatManager_ReconnectBackoff(t *testing.T) {
	hm := &HeartbeatManager{
		baseReconnectDelay: time.Second,
		maxReconnectDelay:  time.Minute,
	}

	cases := []struct {
		failures       int
		minDur, maxDur time.Duration // result must be in [minDur, maxDur)
	}{
		{0, 500 * time.Millisecond, time.Second}, // delay = base*1 = 1s
		{1, time.Second, 2 * time.Second},        // delay = 2s
		{2, 2 * time.Second, 4 * time.Second},    // delay = 4s
		{5, 16 * time.Second, 32 * time.Second},  // delay = 32s, under 60s cap
		{10, 16 * time.Second, 32 * time.Second}, // exp clamped at 5, same as above
	}

	// Repeat each case a few times to catch a jitter that drifts out of range.
	for _, tc := range cases {
		for i := 0; i < 25; i++ {
			got := hm.reconnectBackoff(tc.failures)
			if got < tc.minDur || got >= tc.maxDur {
				t.Errorf("failures=%d iter=%d: got %v, want in [%v, %v)", tc.failures, i, got, tc.minDur, tc.maxDur)
				break
			}
		}
	}
}

// When the next "natural" delay would exceed maxReconnectDelay, the cap is
// applied to the delay BEFORE jitter, so the jitter range shrinks too.
func TestHeartbeatManager_ReconnectBackoff_RespectsMaxCap(t *testing.T) {
	hm := &HeartbeatManager{
		baseReconnectDelay: 4 * time.Second,
		maxReconnectDelay:  60 * time.Second,
	}
	// failures=5 → 4s * 32 = 128s, capped to 60s; jitter into [30s, 60s).
	for i := 0; i < 25; i++ {
		got := hm.reconnectBackoff(5)
		if got < 30*time.Second || got >= 60*time.Second {
			t.Fatalf("iter=%d: got %v, want in [30s, 60s)", i, got)
		}
	}
}

func TestHeartbeatManager_GetSessionInfo(t *testing.T) {
	hm := &HeartbeatManager{sessions: map[uint]*heartbeatSession{}}
	hm.sessions[5] = &heartbeatSession{nodeID: 5, lastRTT: 42, configHash: "abc"}

	rtt, hash, ok := hm.GetSessionInfo(5)
	if !ok || rtt != 42 || hash != "abc" {
		t.Errorf("known session: rtt=%d hash=%q ok=%v", rtt, hash, ok)
	}

	// Missing session reports !ok and zero values — callers branch on ok.
	if rtt, hash, ok := hm.GetSessionInfo(999); ok || rtt != 0 || hash != "" {
		t.Errorf("unknown session: rtt=%d hash=%q ok=%v", rtt, hash, ok)
	}
}
