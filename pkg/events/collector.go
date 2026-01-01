package events

// EventCollector buffers events for deferred publication.
// Events are only published when Flush is explicitly called, ensuring
// they are never sent for operations that later fail.
type EventCollector struct {
	events []Event
}

func NewEventCollector() *EventCollector {
	return &EventCollector{}
}

func (c *EventCollector) Add(event Event) {
	c.events = append(c.events, event)
}

func (c *EventCollector) Flush(bus *EventBus) {
	if bus == nil || len(c.events) == 0 {
		return
	}
	for _, e := range c.events {
		bus.Publish(e)
	}
	c.events = nil
}
