package events

import (
	"testing"
	"time"
)

func TestCollector_PublishesOnFlush(t *testing.T) {
	bus := NewEventBus()
	sub := bus.Subscribe("test")
	defer bus.Unsubscribe("test")

	collector := NewEventCollector()
	collector.Add(Event{
		Type:      EventSubscriptionCreated,
		Timestamp: time.Now(),
		Payload:   "test-payload",
	})

	// Before flush, no events
	select {
	case <-sub:
		t.Fatal("event received before flush")
	default:
	}

	collector.Flush(bus)

	select {
	case e := <-sub:
		if e.Type != EventSubscriptionCreated {
			t.Errorf("expected %s, got %s", EventSubscriptionCreated, e.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event after flush")
	}
}

func TestCollector_DiscardsOnNoFlush(t *testing.T) {
	bus := NewEventBus()
	sub := bus.Subscribe("test")
	defer bus.Unsubscribe("test")

	collector := NewEventCollector()
	collector.Add(Event{Type: EventSubscriptionCreated, Timestamp: time.Now()})
	collector = nil
	_ = collector

	select {
	case <-sub:
		t.Fatal("phantom event!")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCollector_FlushWithNilBus(t *testing.T) {
	collector := NewEventCollector()
	collector.Add(Event{Type: EventSubscriptionCreated, Timestamp: time.Now()})
	collector.Flush(nil) // should not panic
}

func TestCollector_MultipleEvents(t *testing.T) {
	bus := NewEventBus()
	sub := bus.Subscribe("test")
	defer bus.Unsubscribe("test")

	collector := NewEventCollector()
	collector.Add(Event{Type: EventSubscriptionCreated, Timestamp: time.Now()})
	collector.Add(Event{Type: EventSubscriptionCreated, Timestamp: time.Now()})
	collector.Flush(bus)

	received := 0
	for i := 0; i < 2; i++ {
		select {
		case <-sub:
			received++
		case <-time.After(100 * time.Millisecond):
		}
	}
	if received != 2 {
		t.Errorf("expected 2 events, got %d", received)
	}
}

func TestCollector_FlushClearsBuffer(t *testing.T) {
	bus := NewEventBus()
	sub := bus.Subscribe("test")
	defer bus.Unsubscribe("test")

	collector := NewEventCollector()
	collector.Add(Event{Type: EventSubscriptionCreated, Timestamp: time.Now()})
	collector.Flush(bus)
	<-sub

	collector.Flush(bus)

	select {
	case <-sub:
		t.Fatal("second flush should not publish")
	case <-time.After(50 * time.Millisecond):
	}
}
