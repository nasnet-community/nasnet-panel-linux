package tgctx

import (
	"testing"
	"time"
)

// telebot.Context isn't consulted by either constructor, so passing nil
// keeps the test self-contained without dragging in a telebot fixture.
func TestFromTelebot_UsesDefaultTimeout(t *testing.T) {
	ctx, cancel := FromTelebot(nil)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline on the returned context")
	}
	remaining := time.Until(deadline)
	if remaining > DefaultTimeout+time.Second || remaining < DefaultTimeout-2*time.Second {
		t.Errorf("deadline ~ now+%v, got remaining=%v", DefaultTimeout, remaining)
	}
}

func TestFromTelebotWithTimeout_UsesProvided(t *testing.T) {
	ctx, cancel := FromTelebotWithTimeout(nil, 10*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 10*time.Second+time.Second || remaining < 10*time.Second-2*time.Second {
		t.Errorf("deadline ~ now+10s, got remaining=%v", remaining)
	}
}
