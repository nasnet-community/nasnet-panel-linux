// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"time"
)

// poll calls fn until it reports done, for at most total, then gives up with
// the last thing fn complained about. fn returns (true, nil) when it is done
// and (false, err) to be tried again after every; an error from stop ends the
// wait at once, for what waiting won't mend. A node that was just changed
// needs a moment, and some of what it does in that moment looks like failure.
func poll(ctx context.Context, total, every time.Duration, fn func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(total)
	var last error
	for {
		done, err := fn(ctx)
		var f fatal
		switch {
		case errors.As(err, &f):
			return f.err
		case done:
			return nil
		case err != nil:
			last = err
		}
		if ctx.Err() != nil {
			return pollEnd(last, ctx.Err())
		}
		wait := every
		if left := time.Until(deadline); left < wait {
			wait = left
		}
		if wait <= 0 {
			return pollEnd(last, errPollTimeout)
		}
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return pollEnd(last, ctx.Err())
		}
	}
}

var errPollTimeout = errors.New("timed out")

// pollEnd is why a wait gave up: what fn last said, else why the time ran out.
func pollEnd(last, fallback error) error {
	if last != nil {
		return last
	}
	return fallback
}

// fatal is an error that ends a poll.
type fatal struct{ err error }

func (f fatal) Error() string { return f.err.Error() }
func (f fatal) Unwrap() error { return f.err }

func stop(err error) error { return fatal{err} }
