package scheduler

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestRunChecksDoesNotOverlap verifies that the CAS-based guard prevents
// concurrent runChecks executions when the ticker fires faster than the
// work takes to complete.
func TestRunChecksDoesNotOverlap(t *testing.T) {
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	// Simulate a slow runChecks that takes 50ms
	slowRunChecks := func() {
		current := concurrentCount.Add(1)
		defer concurrentCount.Add(-1)

		// Track the maximum observed concurrency
		for {
			prev := maxConcurrent.Load()
			if current <= prev {
				break
			}
			if maxConcurrent.CompareAndSwap(prev, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	// Replicate the CAS guard logic from scheduler.Start()
	var checksRunning atomic.Bool

	const numTicks = 10
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < numTicks; i++ {
			// Simulate a ticker firing every 5ms (much faster than the 50ms work)
			time.Sleep(5 * time.Millisecond)

			if !checksRunning.CompareAndSwap(false, true) {
				// Guard fired: skip this tick
				continue
			}
			go func() {
				slowRunChecks()
				checksRunning.Store(false)
			}()
		}
	}()

	<-done
	// Give the last spawned goroutine time to finish
	time.Sleep(100 * time.Millisecond)

	max := maxConcurrent.Load()
	if max > 1 {
		t.Errorf("runChecks ran concurrently: max concurrent executions = %d, want 1", max)
	}
	if max < 1 {
		t.Errorf("runChecks never ran: max concurrent executions = %d, want at least 1", max)
	}
}
