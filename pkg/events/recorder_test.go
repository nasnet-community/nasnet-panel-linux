package events

import (
	"testing"
	"time"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func TestRecorderKeepsOnlyMatchingEvents(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	rec := NewRecorder(bus, "test-rec", 10, IsNetworkEvent)

	bus.Publish(Event{Type: EventWANUp, Payload: map[string]any{"if_name": "eth0"}})
	bus.Publish(Event{Type: "payment.created"})
	bus.Publish(Event{Type: EventVPNDown})

	waitFor(t, func() bool { return len(rec.Recent()) == 2 })
	got := rec.Recent()
	if got[0].Type != EventWANUp || got[1].Type != EventVPNDown {
		t.Fatalf("wrong events kept: %v", got)
	}
}

func TestRecorderDropsOldestPastMax(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	rec := NewRecorder(bus, "test-rec", 2, func(Event) bool { return true })

	bus.Publish(Event{Type: "a"})
	bus.Publish(Event{Type: "b"})
	bus.Publish(Event{Type: "c"})

	waitFor(t, func() bool {
		r := rec.Recent()
		return len(r) == 2 && r[0].Type == "b" && r[1].Type == "c"
	})
}

func TestIsNetworkEvent(t *testing.T) {
	for _, tc := range []struct {
		typ  EventType
		want bool
	}{
		{EventWANUp, true}, {EventVPNUp, true}, {EventInterfaceAdded, true},
		{EventWANApplied, true}, {"payment.created", false}, {"node.online", false},
	} {
		if got := IsNetworkEvent(Event{Type: tc.typ}); got != tc.want {
			t.Errorf("%s: got %v", tc.typ, got)
		}
	}
}

// A non-positive size used to panic the recorder's goroutine on the first
// event, taking the process with it.
func TestNewRecorderSurvivesANonPositiveSize(t *testing.T) {
	bus := NewEventBus()
	r := NewRecorder(bus, "guard", 0, func(Event) bool { return true })
	bus.Publish(Event{Type: EventWANApplied})
	time.Sleep(20 * time.Millisecond)
	if got := len(r.Recent()); got > 1 {
		t.Errorf("kept %d events with a zero size", got)
	}
}
