package ratelimit

import (
	"testing"
	"time"
)

func TestLoginLimiter_AllowsFresh(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	allowed, ra := l.Check("1.1.1.1", "alice")
	if !allowed || ra != 0 {
		t.Errorf("fresh check: allowed=%v, retryAfter=%v", allowed, ra)
	}
}

func TestLoginLimiter_BelowThresholdAllowed(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	for i := 0; i < maxAttempts-1; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	if allowed, _ := l.Check("1.1.1.1", "alice"); !allowed {
		t.Errorf("got locked after %d attempts, want allow until %d", maxAttempts-1, maxAttempts)
	}
}

func TestLoginLimiter_LocksAfterMaxAttempts(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	for i := 0; i < maxAttempts; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	allowed, ra := l.Check("1.1.1.1", "alice")
	if allowed {
		t.Fatal("expected lockout after maxAttempts")
	}
	if ra <= 0 || ra > lockoutWindow {
		t.Errorf("retryAfter = %v, want in (0, %v]", ra, lockoutWindow)
	}
}

func TestLoginLimiter_ResetClears(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	for i := 0; i < maxAttempts; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	l.Reset("1.1.1.1", "alice")
	if allowed, _ := l.Check("1.1.1.1", "alice"); !allowed {
		t.Fatal("Reset did not clear lockout")
	}
}

// IP and username are tracked independently, so locking one dimension
// blocks any request that touches the locked side.
func TestLoginLimiter_IpAndUserIndependent(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	for i := 0; i < maxAttempts; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	if allowed, _ := l.Check("1.1.1.1", "bob"); allowed {
		t.Error("same IP with different user should still be locked")
	}
	if allowed, _ := l.Check("2.2.2.2", "alice"); allowed {
		t.Error("same user from different IP should still be locked")
	}
	if allowed, _ := l.Check("2.2.2.2", "bob"); !allowed {
		t.Error("unrelated ip+user should be allowed")
	}
}

// White-box: rewind lockedAt past the window, then Check should auto-unlock.
func TestLoginLimiter_LockoutExpiryResets(t *testing.T) {
	l := NewLoginLimiter()
	defer l.Stop()
	for i := 0; i < maxAttempts; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	// RecordFailure locks both the ip and user records, so rewind both.
	for _, key := range []string{"ip:1.1.1.1", "user:alice"} {
		val, _ := l.attempts.Load(key)
		rec := val.(*attemptRecord)
		rec.mu.Lock()
		rec.lockedAt = time.Now().Add(-lockoutWindow - time.Minute)
		rec.mu.Unlock()
	}

	if allowed, _ := l.Check("1.1.1.1", "alice"); !allowed {
		t.Fatal("expired lockout should auto-reset on next Check")
	}
}
