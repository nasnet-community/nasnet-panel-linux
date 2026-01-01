package conversation

import (
	"testing"
	"time"
)

func TestNewSession_Defaults(t *testing.T) {
	s := NewSession(7)
	if s.UserID != 7 {
		t.Errorf("UserID = %d, want 7", s.UserID)
	}
	if s.State != StateIdle {
		t.Errorf("State = %q, want %q", s.State, StateIdle)
	}
	if s.Data == nil {
		t.Error("Data should be initialized, not nil")
	}
	if !s.ExpiresAt.After(time.Now().Add(SessionTimeout - time.Second)) {
		t.Errorf("ExpiresAt = %v, want ~now+%v", s.ExpiresAt, SessionTimeout)
	}
}

func TestUserSession_IsExpired(t *testing.T) {
	if (&UserSession{ExpiresAt: time.Now().Add(-time.Minute)}).IsExpired() != true {
		t.Error("past ExpiresAt should be expired")
	}
	if (&UserSession{ExpiresAt: time.Now().Add(time.Minute)}).IsExpired() != false {
		t.Error("future ExpiresAt should not be expired")
	}
}

// Reset wipes Data and refreshes ExpiresAt so the user can start fresh
// without juggling a new session struct in callers.
func TestUserSession_Reset_ClearsDataAndRefreshes(t *testing.T) {
	s := NewSession(1)
	s.State = StateAddNodeName
	s.Set("key", "value")
	s.ExpiresAt = time.Now().Add(-time.Minute) // pretend expired

	s.Reset()

	if s.State != StateIdle {
		t.Errorf("Reset should return to Idle, got %q", s.State)
	}
	if len(s.Data) != 0 {
		t.Errorf("Reset should clear Data, got %+v", s.Data)
	}
	if s.IsExpired() {
		t.Error("Reset should refresh ExpiresAt")
	}
}

// Set / SetState refresh expiry so an in-flight wizard doesn't time out
// mid-conversation.
func TestUserSession_SetState_RefreshesExpiry(t *testing.T) {
	s := NewSession(1)
	s.ExpiresAt = time.Now().Add(-time.Minute)
	s.SetState(StateAddNodeName)
	if s.IsExpired() {
		t.Error("SetState should refresh ExpiresAt")
	}

	s.ExpiresAt = time.Now().Add(-time.Minute)
	s.Set("k", "v")
	if s.IsExpired() {
		t.Error("Set should refresh ExpiresAt")
	}
}

func TestUserSession_GetSetMissing(t *testing.T) {
	s := NewSession(1)
	s.Set("k", "v")
	if v, ok := s.Get("k"); !ok || v != "v" {
		t.Errorf("Get(k) = %v, %v", v, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Error("Get(missing) should report !ok")
	}
}

func TestUserSession_GetString(t *testing.T) {
	s := NewSession(1)
	s.Set("s", "hello")
	s.Set("n", 5)
	if got := s.GetString("s"); got != "hello" {
		t.Errorf("GetString(s) = %q", got)
	}
	// Non-string and missing both yield "" — keeps call sites branch-free.
	if got := s.GetString("n"); got != "" {
		t.Errorf("GetString on non-string = %q, want ''", got)
	}
	if got := s.GetString("missing"); got != "" {
		t.Errorf("GetString missing = %q, want ''", got)
	}
}

// GetFloat accepts float64/int/int64 and zero-defaults anything else.
func TestUserSession_GetFloat(t *testing.T) {
	s := NewSession(1)
	s.Set("f", float64(3.5))
	s.Set("i", 7)
	s.Set("i64", int64(11))
	s.Set("s", "no")
	if s.GetFloat("f") != 3.5 || s.GetFloat("i") != 7 || s.GetFloat("i64") != 11 {
		t.Errorf("got f=%v i=%v i64=%v", s.GetFloat("f"), s.GetFloat("i"), s.GetFloat("i64"))
	}
	if s.GetFloat("s") != 0 || s.GetFloat("missing") != 0 {
		t.Error("unsupported / missing should zero")
	}
}

// GetInt accepts every common numeric kind; everything else zeros.
func TestUserSession_GetIntAndUint(t *testing.T) {
	s := NewSession(1)
	s.Set("i", 5)
	s.Set("i64", int64(9))
	s.Set("f", float64(7.9)) // truncates to 7
	s.Set("u", uint(3))
	s.Set("u32", uint32(11))
	s.Set("u64", uint64(13))

	if s.GetInt("i") != 5 || s.GetInt("i64") != 9 || s.GetInt("f") != 7 ||
		s.GetInt("u") != 3 || s.GetInt("u32") != 11 || s.GetInt("u64") != 13 {
		t.Errorf("GetInt conversions wrong: %+v", s.Data)
	}
	if s.GetInt("missing") != 0 {
		t.Error("missing key should zero")
	}
	if s.GetUint("u32") != uint(11) {
		t.Errorf("GetUint wraps GetInt; got %d", s.GetUint("u32"))
	}
}
