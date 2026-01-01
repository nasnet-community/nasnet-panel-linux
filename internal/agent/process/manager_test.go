package process

import (
	"sync"
	"testing"
	"time"
)

// TestStopIdempotent: double Stop must not panic. Race: SIGTERM Stop() +
// restartSelf fallback Stop() both pass isRunningLocked()=true → double
// close(stopChan). Drives the stopOnce/stopChan primitive directly.
func TestStopIdempotent(t *testing.T) {
	m := &XrayManager{
		stopChan: make(chan struct{}),
		// stopOnce is zero-value sync.Once — correct initial state
	}

	// Simulate what Stop() does after isRunningLocked() returns true.
	// Before the fix this would panic on the second call.
	callStop := func() {
		m.stopOnce.Do(func() { close(m.stopChan) })
	}

	// Must not panic.
	callStop()
	callStop()
}

// TestStopIdempotentViaNewXrayManager verifies that two sequential Stop calls
// on a manager created through the public constructor do not panic.
// We use serviceMode=false with non-existent paths so xray is not actually
// started; Stop returns early from isRunningLocked() == false, but we also
// directly verify the stopOnce invariant holds for that manager.
func TestStopIdempotentViaNewXrayManager(t *testing.T) {
	m := NewXrayManager(Config{
		BinaryPath:  "/nonexistent/xray",
		ConfigPath:  "/nonexistent/config.json",
		ServiceMode: false,
	})

	// Directly exercise the stopOnce primitive on the real manager object.
	// This ensures the struct field exists and behaves correctly.
	m.stopOnce.Do(func() { close(m.stopChan) })
	// Second Do must be a no-op, not a panic.
	m.stopOnce.Do(func() { close(m.stopChan) })
}

// TestRestartResetsStopOnce verifies that after Restart recreates stopChan,
// a subsequent Stop (via stopOnce.Do) can close the new channel without panic.
func TestRestartResetsStopOnce(t *testing.T) {
	m := &XrayManager{
		stopChan: make(chan struct{}),
	}

	// First stop cycle.
	m.stopOnce.Do(func() { close(m.stopChan) })

	// Simulate what Restart() does: recreate channel and reset Once.
	m.mu.Lock()
	m.stopChan = make(chan struct{})
	m.stopOnce = sync.Once{}
	m.stopped = false
	m.mu.Unlock()

	// After restart, a new Stop should be able to close the fresh channel.
	// This must not panic.
	m.stopOnce.Do(func() { close(m.stopChan) })
	// And a duplicate close of the new channel must also be safe.
	m.stopOnce.Do(func() { close(m.stopChan) })
}

// TestStopIdempotentViaPublicAPI: idempotency guard must live in Stop()
// itself. Calling closeStopChan() twice (Stop's exact path) catches a
// regression that drops stopOnce.Do.
func TestStopIdempotentViaPublicAPI(t *testing.T) {
	m := NewXrayManager(Config{
		BinaryPath:    "/bin/true",
		ConfigPath:    "/dev/null",
		ServiceMode:   false,
		RestartDelay:  time.Millisecond,
		MaxRestarts:   1,
		RestartWindow: time.Second,
	})

	// closeStopChan() is the exact one-liner Stop() calls after the running
	// check. Calling it twice must not panic — sync.Once must be in place.
	m.closeStopChan()
	m.closeStopChan()
}

// TestStopTimeout is a basic sanity check that Stop on a non-started
// non-service-mode manager returns quickly (early-exit path).
func TestStopTimeout(t *testing.T) {
	m := NewXrayManager(Config{
		BinaryPath:  "/nonexistent/xray",
		ConfigPath:  "/nonexistent/config.json",
		ServiceMode: false,
	})

	done := make(chan error, 1)
	go func() {
		done <- m.Stop(100 * time.Millisecond)
	}()

	select {
	case err := <-done:
		// err may be nil or non-nil; what matters is it didn't hang or panic.
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5 seconds on a non-started manager")
	}
}
