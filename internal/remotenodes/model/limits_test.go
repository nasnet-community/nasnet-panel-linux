// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"strings"
	"testing"
	"time"
)

func TestWhenASubscriptionShouldStop(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		sub  Subscription
		used uint64
		want string
	}{
		"no limit and no date":      {Subscription{}, 1 << 40, ""},
		"under the limit":           {Subscription{TrafficLimit: 1000}, 999, ""},
		"at the limit":              {Subscription{TrafficLimit: 1000}, 1000, StopLimit},
		"over the limit":            {Subscription{TrafficLimit: 1000}, 5000, StopLimit},
		"nothing used yet":          {Subscription{TrafficLimit: 1000}, 0, ""},
		"before its date":           {Subscription{ExpiresAt: now.Add(time.Second)}, 0, ""},
		"at its date":               {Subscription{ExpiresAt: now}, 0, StopExpired},
		"after its date":            {Subscription{ExpiresAt: now.Add(-time.Hour)}, 0, StopExpired},
		"both, the date wins":       {Subscription{TrafficLimit: 10, ExpiresAt: now.Add(-time.Hour)}, 99, StopExpired},
		"the limit with a far date": {Subscription{TrafficLimit: 10, ExpiresAt: now.Add(time.Hour)}, 99, StopLimit},
	} {
		if got := c.sub.StopReason(c.used, now); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestARunningSubscriptionIsOneThatIsNotStopped(t *testing.T) {
	if !(Subscription{}).Running() || (Subscription{Stopped: StopLimit}).Running() || (Subscription{Stopped: StopExpired}).Running() {
		t.Error("Running is wrong")
	}
}

func TestASubscriptionsLimitsAreChecked(t *testing.T) {
	ok := Subscription{ID: 1, Name: "Sara", CreatedAt: time.Now()}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*Subscription){
		"a limit above the largest": func(s *Subscription) { s.TrafficLimit = MaxTrafficLimit + 1 },
		"a reason nobody knows":     func(s *Subscription) { s.Stopped = "angry" },
	} {
		bad := ok
		mod(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	full := ok
	full.TrafficLimit, full.TrafficBase, full.ExpiresAt, full.Stopped = MaxTrafficLimit, 7, time.Now().Add(time.Hour), StopExpired
	if err := full.Validate(); err != nil {
		t.Errorf("the largest limit with a date and a reason: %v", err)
	}
}

func TestLimitsSurviveTheFileAndAnOlderFileHasNone(t *testing.T) {
	s := NewState()
	end := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := s.AddSubscription(Subscription{Name: "Sara", CreatedAt: time.Now(), TrafficLimit: 5 << 30, TrafficBase: 1 << 20, ExpiresAt: end, Stopped: StopLimit}); err != nil {
		t.Fatal(err)
	}
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	sub := got.Subscriptions[0]
	if sub.TrafficLimit != 5<<30 || sub.TrafficBase != 1<<20 || !sub.ExpiresAt.Equal(end) || sub.Stopped != StopLimit {
		t.Errorf("the subscription came back as %+v", sub)
	}

	// a subscription with none of it writes none of it, so a file from before reads the same
	plain := NewState()
	if _, err := plain.AddSubscription(Subscription{Name: "Ali", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b, err = Encode(plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"traffic_limit", "traffic_base", "expires_at", "stopped"} {
		if strings.Contains(string(b), key) {
			t.Errorf("a plain subscription's file has %q", key)
		}
	}
	older, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if o := older.Subscriptions[0]; o.TrafficLimit != 0 || !o.ExpiresAt.IsZero() || !o.Running() {
		t.Errorf("an older file's subscription reads %+v", o)
	}
}
