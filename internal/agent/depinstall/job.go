package depinstall

import (
	"fmt"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
)

// Job / step / status string constants (match the proto and the panel).
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusPartial   = "partial"
	StatusFailed    = "failed"

	StepPending = "pending"
	StepRunning = "running"
	StepOK      = "ok"
	StepSkipped = "skipped"
	StepFailed  = "failed"
)

const maxLogLines = 5000

// Step is one dependency's install state within a job.
type Step struct {
	Name     string
	CoreType string
	Kind     string
	State    string
	Detail   string
	dep      coredeps.Dependency
}

// Job holds the mutable state of one install run. All access goes through mu.
type Job struct {
	ID  string
	req InstallRequest

	mu         sync.Mutex
	status     string
	steps      []*Step
	log        []string
	done       bool
	startedAt  time.Time
	finishedAt time.Time
}

// Snapshot is an immutable view returned to callers.
type Snapshot struct {
	Status     string
	Steps      []StepView
	LogLines   []string
	NextCursor int
	Done       bool
}

// StepView is the exported, lock-free copy of a Step.
type StepView struct {
	Name     string
	CoreType string
	Kind     string
	State    string
	Detail   string
}

// newJob flattens the requested cores' manifests into ordered steps.
func newJob(id string, req InstallRequest, now time.Time) *Job {
	j := &Job{ID: id, req: req, status: StatusPending, startedAt: now}
	for _, p := range req.Cores {
		for _, d := range coredeps.For(p) {
			j.steps = append(j.steps, &Step{
				Name:     d.Name,
				CoreType: string(p),
				Kind:     string(d.Kind),
				State:    StepPending,
				dep:      d,
			})
		}
	}
	return j
}

func (j *Job) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	j.mu.Lock()
	if len(j.log) < maxLogLines {
		j.log = append(j.log, line)
	}
	j.mu.Unlock()
}

func (j *Job) setStatus(s string) {
	j.mu.Lock()
	j.status = s
	j.mu.Unlock()
}

func (j *Job) setStep(s *Step, state, detail string) {
	j.mu.Lock()
	s.State = state
	s.Detail = detail
	j.mu.Unlock()
}

// finish records the terminal status: succeeded if no step failed, partial if
// some non-optional steps failed alongside successes, failed if nothing worked.
func (j *Job) finish(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var failed, ok int
	for _, s := range j.steps {
		switch s.State {
		case StepFailed:
			failed++
		case StepOK, StepSkipped:
			ok++
		}
	}
	switch {
	case failed == 0:
		j.status = StatusSucceeded
	case ok == 0:
		j.status = StatusFailed
	default:
		j.status = StatusPartial
	}
	j.done = true
	j.finishedAt = now
}

func (j *Job) snapshot(sinceCursor int) Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	steps := make([]StepView, len(j.steps))
	for i, s := range j.steps {
		steps[i] = StepView{Name: s.Name, CoreType: s.CoreType, Kind: s.Kind, State: s.State, Detail: s.Detail}
	}
	if sinceCursor < 0 || sinceCursor > len(j.log) {
		sinceCursor = len(j.log)
	}
	newLines := append([]string(nil), j.log[sinceCursor:]...)
	return Snapshot{
		Status:     j.status,
		Steps:      steps,
		LogLines:   newLines,
		NextCursor: len(j.log),
		Done:       j.done,
	}
}
