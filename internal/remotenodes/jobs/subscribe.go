// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

// Subscribe follows a job. The first value on the channel is the job as it
// stands, then comes the job after every change, and the channel is closed
// right after the final state. ok is false for a job the engine doesn't know.
//
// A reader that falls behind never holds the job up: there is room for one
// value, and a new one takes the place of a value nobody has read, so what a
// slow reader gets next is always the latest state. Stop reading by calling
// cancel, which also closes the channel; it may be called more than once and
// after the job has ended. A subscription has no goroutine of its own, so one
// that is never cancelled costs a channel until its job ends.
func (e *Engine) Subscribe(id string) (events <-chan Job, cancel func(), ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	j := e.byID[id]
	if j == nil {
		return nil, func() {}, false
	}
	ch := make(chan Job, 1)
	ch <- j.snapshot()
	if j.view.State != StateRunning {
		close(ch)
		return ch, func() {}, true
	}
	if j.subs == nil {
		j.subs = map[chan Job]struct{}{}
	}
	j.subs[ch] = struct{}{}
	return ch, func() { e.unsubscribe(j, ch) }, true
}

func (e *Engine) unsubscribe(j *job, ch chan Job) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// finish has already closed and forgotten the ones still there at the end
	if _, there := j.subs[ch]; there {
		delete(j.subs, ch)
		close(ch)
	}
}

// publish hands the job to everyone following it, and when it is over closes
// their channels. It never blocks: the sends happen under mu, so nobody else
// can fill the one place in a channel after it has been emptied. The caller
// holds mu.
func (e *Engine) publish(j *job) {
	for ch := range j.subs {
		select {
		case <-ch: // a value the reader never took
		default:
		}
		ch <- j.snapshot()
	}
	if j.view.State != StateRunning {
		for ch := range j.subs {
			close(ch)
		}
		j.subs = nil
	}
}
