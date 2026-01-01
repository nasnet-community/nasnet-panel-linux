package events

import (
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Subscriber is a channel that receives events
type Subscriber chan Event

// EventBus manages event publishing and subscription
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]Subscriber
	filters     map[string]func(Event) bool
	bufferSize  int

	// Optional metrics hooks (set externally to avoid circular imports)
	OnPublish          func(eventType string)                      // called after each Publish
	OnSubscriberChange func(count int)                             // called after Subscribe/Unsubscribe
	OnDrop             func(eventType string, subscriberID string) // called when event is dropped
}

// NewEventBus creates a new event bus with the default buffer size of 100.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]Subscriber),
		filters:     make(map[string]func(Event) bool),
		bufferSize:  100, // Buffer to prevent blocking publishers
	}
}

// Subscribe creates a new subscriber with the given ID and returns the event channel
func (b *EventBus) Subscribe(id string) Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Close existing subscription if present
	if existing, ok := b.subscribers[id]; ok {
		close(existing)
	}

	ch := make(Subscriber, b.bufferSize)
	b.subscribers[id] = ch
	count := len(b.subscribers)

	logger.GetLogger().Debugf("EventBus: New subscriber %s (total: %d)", id, count)

	if b.OnSubscriberChange != nil {
		b.OnSubscriberChange(count)
	}
	return ch
}

// SubscribeFiltered behaves like Subscribe but only forwards events for which
// predicate returns true. predicate runs under the bus lock — keep it cheap.
func (b *EventBus) SubscribeFiltered(id string, predicate func(Event) bool) Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.subscribers[id]; ok {
		close(existing)
	}
	ch := make(Subscriber, b.bufferSize)
	b.subscribers[id] = ch
	if predicate != nil {
		b.filters[id] = predicate
	} else {
		delete(b.filters, id)
	}
	if b.OnSubscriberChange != nil {
		b.OnSubscriberChange(len(b.subscribers))
	}
	return ch
}

// Unsubscribe removes a subscriber and closes its channel
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
		delete(b.filters, id)
		count := len(b.subscribers)
		logger.GetLogger().Debugf("EventBus: Unsubscribed %s (remaining: %d)", id, count)

		if b.OnSubscriberChange != nil {
			b.OnSubscriberChange(count)
		}
	}
}

// Publish sends an event to all subscribers
// Non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	log := logger.GetLogger()
	log.Debugf("EventBus: Publishing event %s to %d subscribers", event.Type, len(b.subscribers))

	for id, ch := range b.subscribers {
		if pred, ok := b.filters[id]; ok && !pred(event) {
			continue
		}
		select {
		case ch <- event:
			// Successfully sent
		default:
			// Buffer full, log and skip
			log.Warnf("EventBus: Dropping event %s for subscriber %s (buffer full)", event.Type, id)
			if b.OnDrop != nil {
				b.OnDrop(string(event.Type), id)
			}
		}
	}

	if b.OnPublish != nil {
		b.OnPublish(string(event.Type))
	}
}

// Close closes all subscriber channels, causing SSE handlers to exit
func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
		delete(b.filters, id)
	}

	logger.GetLogger().Debug("EventBus: Closed all subscribers")
}

// SubscriberCount returns the current number of active subscribers
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
