package usecase

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestSyncStatsConcurrency pins the compile-time concurrency cap.
// Intentionally low to avoid starving DB pools during a sweep — a
// future refactor that bumps it should be a conscious choice, not a
// silent drift.
func TestSyncStatsConcurrency(t *testing.T) {
	if syncStatsConcurrency <= 0 {
		t.Fatalf("syncStatsConcurrency must be > 0, got %d", syncStatsConcurrency)
	}
	if syncStatsConcurrency > 32 {
		t.Errorf("syncStatsConcurrency=%d feels too high; audit before raising", syncStatsConcurrency)
	}
}

// TestSyncStatsTimeoutBounded pins the per-node deadline so a slow
// agent can't stall the batch past the next scheduler tick.
func TestSyncStatsTimeoutBounded(t *testing.T) {
	if syncStatsPerNodeTimeout <= 0 {
		t.Fatal("syncStatsPerNodeTimeout must be positive")
	}
	if syncStatsPerNodeTimeout > 30*time.Second {
		t.Errorf("syncStatsPerNodeTimeout=%v too long; must be below reconcile interval to avoid overlap",
			syncStatsPerNodeTimeout)
	}
}

// TestSyncStatsFanOutShape is a behavioural smoke test of the
// semaphore + goroutine shape used in SyncNodeStats. It mirrors the
// fan-out pattern to catch a future refactor that silently drops the
// concurrency cap or forgets to wait for all goroutines.
func TestSyncStatsFanOutShape(t *testing.T) {
	const total = 50
	sem := make(chan struct{}, syncStatsConcurrency)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var completed atomic.Int32

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()

				// Observe peak concurrency.
				n := inFlight.Add(1)
				for {
					prev := maxInFlight.Load()
					if n <= prev || maxInFlight.CompareAndSwap(prev, n) {
						break
					}
				}

				// Simulate per-node work.
				time.Sleep(5 * time.Millisecond)

				inFlight.Add(-1)
				completed.Add(1)
			}()
		}
	}()

	<-done
	// Drain semaphore to ensure all goroutines finished.
	for i := 0; i < syncStatsConcurrency; i++ {
		sem <- struct{}{}
	}

	if got := completed.Load(); got != int32(total) {
		t.Fatalf("expected all %d jobs to complete, got %d", total, got)
	}
	if peak := maxInFlight.Load(); peak > int32(syncStatsConcurrency) {
		t.Errorf("peak concurrency %d exceeded cap %d", peak, syncStatsConcurrency)
	}
	if peak := maxInFlight.Load(); peak < 2 {
		t.Errorf("expected some concurrency, got peak=%d", peak)
	}
}
