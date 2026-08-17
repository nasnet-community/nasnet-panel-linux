package events

import "sync"

// Recorder keeps the last max matching events, so a page opened after an
// incident can still show what led up to it.
type Recorder struct {
	mu  sync.Mutex
	buf []Event
	max int
}

func NewRecorder(bus *EventBus, id string, max int, keep func(Event) bool) *Recorder {
	if max < 1 {
		max = 1
	}
	r := &Recorder{max: max}
	ch := bus.SubscribeFiltered(id, keep)
	go func() {
		for ev := range ch {
			r.mu.Lock()
			r.buf = append(r.buf, ev)
			if len(r.buf) > r.max {
				r.buf = r.buf[len(r.buf)-r.max:]
			}
			r.mu.Unlock()
		}
	}()
	return r
}

// Recent returns a copy, oldest first.
func (r *Recorder) Recent() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.buf))
	copy(out, r.buf)
	return out
}
