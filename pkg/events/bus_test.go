package events

import (
	"testing"
	"time"
)

func TestEventBus_SubscribePublish(t *testing.T) {
	bus := NewEventBus()

	// Subscribe
	sub1 := bus.Subscribe("client-1")
	sub2 := bus.Subscribe("client-2")

	if bus.SubscriberCount() != 2 {
		t.Errorf("Expected 2 subscribers, got %d", bus.SubscriberCount())
	}

	// Publish an event
	event := Event{
		Type:      EventNodeOnline,
		Timestamp: time.Now(),
		Payload: NodeStatusPayload{
			NodeID:   1,
			NodeName: "test-node",
			IP:       "192.168.1.1",
			IsOnline: true,
		},
	}
	bus.Publish(event)

	// Both subscribers should receive the event
	select {
	case received := <-sub1:
		if received.Type != EventNodeOnline {
			t.Errorf("Expected event type %s, got %s", EventNodeOnline, received.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscriber 1 did not receive event")
	}

	select {
	case received := <-sub2:
		if received.Type != EventNodeOnline {
			t.Errorf("Expected event type %s, got %s", EventNodeOnline, received.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscriber 2 did not receive event")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()

	_ = bus.Subscribe("client-1")
	if bus.SubscriberCount() != 1 {
		t.Errorf("Expected 1 subscriber, got %d", bus.SubscriberCount())
	}

	bus.Unsubscribe("client-1")
	if bus.SubscriberCount() != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", bus.SubscriberCount())
	}
}

func TestEventBus_ResubscribeSameID(t *testing.T) {
	bus := NewEventBus()

	sub1 := bus.Subscribe("client-1")
	sub2 := bus.Subscribe("client-1") // Same ID, should replace

	if bus.SubscriberCount() != 1 {
		t.Errorf("Expected 1 subscriber, got %d", bus.SubscriberCount())
	}

	// First channel should be closed
	_, ok := <-sub1
	if ok {
		t.Error("Expected first subscriber channel to be closed")
	}

	// Second channel should still work
	event := Event{Type: EventSystemAlert, Timestamp: time.Now()}
	bus.Publish(event)

	select {
	case <-sub2:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscriber 2 did not receive event")
	}
}
