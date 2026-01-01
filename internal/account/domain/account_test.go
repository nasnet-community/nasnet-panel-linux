package domain

import (
	"testing"
	"time"
)

func timePtr(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

func TestAccount_IsExpired(t *testing.T) {
	if (&Account{}).IsExpired() {
		t.Error("no expiry should never be expired")
	}
	if !(&Account{ExpiresAt: timePtr(-time.Hour)}).IsExpired() {
		t.Error("past expiry should be expired")
	}
	if (&Account{ExpiresAt: timePtr(time.Hour)}).IsExpired() {
		t.Error("future expiry should not be expired")
	}
}

func TestAccount_IsDataExhausted(t *testing.T) {
	if (&Account{DataLimit: 0, DataUsed: 999}).IsDataExhausted() {
		t.Error("unlimited account can't be exhausted")
	}
	if !(&Account{DataLimit: 100, DataUsed: 100}).IsDataExhausted() {
		t.Error("usage at limit should be exhausted")
	}
	if (&Account{DataLimit: 100, DataUsed: 50}).IsDataExhausted() {
		t.Error("usage below limit should not be exhausted")
	}
}

func TestAccount_RemainingData(t *testing.T) {
	if got := (&Account{DataLimit: 0}).RemainingData(); got != -1 {
		t.Errorf("unlimited = %d, want -1", got)
	}
	if got := (&Account{DataLimit: 100, DataUsed: 40}).RemainingData(); got != 60 {
		t.Errorf("remaining = %d, want 60", got)
	}
	if got := (&Account{DataLimit: 100, DataUsed: 150}).RemainingData(); got != 0 {
		t.Errorf("over-limit = %d, want 0", got)
	}
}
