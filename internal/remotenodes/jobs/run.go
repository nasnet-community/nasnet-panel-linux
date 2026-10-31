// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

// Run is how a job's function reports its steps. It is safe for concurrent
// use, so a progress callback may call it from another goroutine. Once the
// function has returned the job is over, and a late call does nothing.
//
// A step is named in English, and gets its key from Config.StepKey when it is
// created. One the plan did not list is added at the end. Each call changes
// only what its name says, and the steps may go in any order: a retry can
// begin a step that failed.
type Run struct {
	e *Engine
	j *job
}

// ID is the id of the job.
func (r *Run) ID() string { return r.j.view.ID }

// FailedStep is the name of the first step that has failed, "" while none
// has.
func (r *Run) FailedStep() string {
	r.e.mu.Lock()
	defer r.e.mu.Unlock()
	for _, s := range r.j.view.Steps {
		if s.State == StepFailed {
			return s.Name
		}
	}
	return ""
}

// Begin marks a step as running.
func (r *Run) Begin(step string) {
	r.change(step, func(s *Step) { s.State = StepRunning })
}

// Detail sets the text beside a step and leaves its state alone. It may start
// with a share done ("38% · 8.4 of 13.6 MB"), which the page draws as a bar.
func (r *Run) Detail(step, text string) {
	r.change(step, func(s *Step) { s.Detail = r.j.text(text, MaxDetail) })
}

// Done marks a step as ok.
func (r *Run) Done(step string) {
	r.change(step, func(s *Step) { s.State = StepOK })
}

// Fail marks a step as failed and says why in its text.
func (r *Run) Fail(step string, err error) {
	raw := ""
	if err != nil {
		raw = errText(err)
	}
	r.change(step, func(s *Step) {
		s.State, s.Detail = StepFailed, ""
		if err != nil {
			s.Detail = r.j.text(raw, MaxDetail)
		}
	})
}

// Set gives a step a state and a text in one call, for passing on another
// package's progress as it is: installer.Step{Name, State, Detail} goes in as
// Set(s.Name, StepState(s.State), s.Detail). A state that is none of the four
// is refused and logged.
func (r *Run) Set(step string, state StepState, detail string) {
	if !state.valid() {
		r.e.cfg.Log.Warn("job step given a state it does not have", "job", r.ID(), "step", clip(step, maxName), "state", clip(string(state), maxName))
		return
	}
	r.change(step, func(s *Step) { s.State, s.Detail = state, r.j.text(detail, MaxDetail) })
}

// change applies f to the named step, adding it when it is new, and tells
// whoever follows the job if that changed anything.
func (r *Run) change(step string, f func(*Step)) {
	e, j := r.e, r.j
	e.mu.Lock()
	defer e.mu.Unlock()
	if j.view.State != StateRunning {
		return
	}
	n := len(j.view.Steps)
	s := j.view.step(step, e.keyOf)
	if s == nil {
		return
	}
	before := *s
	f(s)
	if len(j.view.Steps) != n || *s != before {
		e.publish(j)
	}
}
