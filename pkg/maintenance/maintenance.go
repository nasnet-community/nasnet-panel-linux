package maintenance

import "sync/atomic"

var (
	active atomic.Bool
	msg    atomic.Value // stores string
)

// Enable activates maintenance mode with the given reason.
func Enable(reason string) {
	msg.Store(reason)
	active.Store(true)
}

// Disable deactivates maintenance mode.
func Disable() {
	active.Store(false)
}

// IsActive returns true when maintenance mode is enabled.
func IsActive() bool {
	return active.Load()
}

// Message returns the current maintenance reason.
func Message() string {
	if v := msg.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "System maintenance in progress"
}
