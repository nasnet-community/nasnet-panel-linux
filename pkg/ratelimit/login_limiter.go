package ratelimit

import (
	"sync"
	"time"
)

const (
	maxAttempts   = 5
	lockoutWindow = 5 * time.Minute
	cleanupPeriod = 10 * time.Minute
)

type attemptRecord struct {
	count    int
	lockedAt time.Time
	mu       sync.Mutex
}

// LoginLimiter tracks failed login attempts per IP and per username.
// After maxAttempts failures within the lockout window, further attempts
// are blocked until the window expires.
type LoginLimiter struct {
	attempts sync.Map // key: "ip:<addr>" or "user:<name>" → *attemptRecord
	stopCh   chan struct{}
}

// NewLoginLimiter creates a limiter and starts a background cleanup goroutine.
func NewLoginLimiter() *LoginLimiter {
	l := &LoginLimiter{stopCh: make(chan struct{})}
	go l.cleanupLoop()
	return l
}

// Check returns whether a request from ip/username is allowed.
// If not allowed, retryAfter indicates how long the caller should wait.
func (l *LoginLimiter) Check(ip, username string) (allowed bool, retryAfter time.Duration) {
	if ra := l.checkKey("ip:" + ip); ra > 0 {
		return false, ra
	}
	if ra := l.checkKey("user:" + username); ra > 0 {
		return false, ra
	}
	return true, 0
}

// RecordFailure records a failed login attempt for both ip and username.
func (l *LoginLimiter) RecordFailure(ip, username string) {
	l.recordKey("ip:" + ip)
	l.recordKey("user:" + username)
}

// Reset clears attempt counters for both ip and username (call on successful login).
func (l *LoginLimiter) Reset(ip, username string) {
	l.attempts.Delete("ip:" + ip)
	l.attempts.Delete("user:" + username)
}

// Stop terminates the background cleanup goroutine.
func (l *LoginLimiter) Stop() {
	close(l.stopCh)
}

func (l *LoginLimiter) checkKey(key string) time.Duration {
	val, ok := l.attempts.Load(key)
	if !ok {
		return 0
	}
	rec := val.(*attemptRecord)
	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.count < maxAttempts {
		return 0
	}
	remaining := lockoutWindow - time.Since(rec.lockedAt)
	if remaining <= 0 {
		// Lockout expired — reset
		rec.count = 0
		rec.lockedAt = time.Time{}
		return 0
	}
	return remaining
}

func (l *LoginLimiter) recordKey(key string) {
	val, _ := l.attempts.LoadOrStore(key, &attemptRecord{})
	rec := val.(*attemptRecord)
	rec.mu.Lock()
	defer rec.mu.Unlock()

	// If previously locked and expired, reset first
	if rec.count >= maxAttempts && time.Since(rec.lockedAt) >= lockoutWindow {
		rec.count = 0
		rec.lockedAt = time.Time{}
	}

	rec.count++
	if rec.count >= maxAttempts && rec.lockedAt.IsZero() {
		rec.lockedAt = time.Now()
	}
}

func (l *LoginLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.stopCh:
			return
		}
	}
}

func (l *LoginLimiter) cleanup() {
	l.attempts.Range(func(key, value any) bool {
		rec := value.(*attemptRecord)
		rec.mu.Lock()
		expired := rec.count >= maxAttempts && time.Since(rec.lockedAt) >= lockoutWindow
		idle := rec.count < maxAttempts && rec.count > 0
		rec.mu.Unlock()
		if expired || idle {
			l.attempts.Delete(key)
		}
		return true
	})
}
