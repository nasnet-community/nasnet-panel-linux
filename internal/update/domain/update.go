// Package domain holds the panel's self-update jobs and their history.
package domain

import "time"

// Job kinds.
const (
	KindUpdate   = "update"
	KindRollback = "rollback"
)

// Where a job's bundle comes from.
const (
	SourceDownload = "download"
	SourceUpload   = "upload"
	SourcePrev     = "prev" // a rollback puts the previous files back
)

// Job phases: the panel runs the first steps itself, the runner unit the rest.
const (
	PhasePanel  = "panel"
	PhaseRunner = "runner"
	PhaseDone   = "done"
)

// Job results.
const (
	ResultOK         = "ok"
	ResultFailed     = "failed"      // stopped before anything changed, or a rollback went forward again
	ResultRolledBack = "rolled_back" // the new version failed its trial and the old one is back
)

// Step states.
const (
	StepWaiting = "waiting"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// Step IDs, in the order an update runs them.
const (
	StepGuards   = "guards"
	StepDownload = "download"
	StepVerify   = "verify"
	StepBackup   = "backup"
	StepStage    = "stage"
	StepTest     = "test"
	StepSystem   = "system"
	StepSwap     = "swap"
	StepRestart  = "restart"
	StepHealth   = "health"
	StepRevert   = "revert" // only shown once a trial fails
)

type Step struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Job is one update or rollback. It lives in data/update/state.json, not in
// the database, so it survives a broken database and the restart.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Source     string     `json:"source"`
	UploadID   string     `json:"upload_id,omitempty"`
	Route      string     `json:"route,omitempty"`
	Phase      string     `json:"phase"`
	Result     string     `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	Steps      []Step     `json:"steps"`
	XrayRan    bool       `json:"xray_ran,omitempty"`    // the trial checks Xray only if it ran before
	HealthAddr string     `json:"health_addr,omitempty"` // loopback host:port of /health/update
	RunnerSeen *time.Time `json:"runner_seen,omitempty"` // the runner's heartbeat
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Recorded   bool       `json:"recorded,omitempty"` // copied into history and announced
}

// Finished reports whether nothing runs the job anymore.
func (j *Job) Finished() bool { return j != nil && j.Phase == PhaseDone }

// UpdateSteps are an update's steps; an upload skips the download.
func UpdateSteps(to string, upload bool) []Step {
	steps := []Step{
		{ID: StepGuards, Label: "Checking this server"},
		{ID: StepDownload, Label: "Downloading"},
		{ID: StepVerify, Label: "Checking the signature"},
		{ID: StepBackup, Label: "Backing up the database"},
		{ID: StepStage, Label: "Unpacking"},
		{ID: StepTest, Label: "Testing the new version"},
		{ID: StepSystem, Label: "Updating system files"},
		{ID: StepSwap, Label: "Switching to " + to},
		{ID: StepRestart, Label: "Restarting the panel"},
		{ID: StepHealth, Label: "Health check"},
	}
	for i := range steps {
		steps[i].Status = StepWaiting
		if upload && steps[i].ID == StepDownload {
			steps[i].Status = StepSkipped
		}
	}
	return steps
}

// RollbackSteps are a rollback's steps.
func RollbackSteps(to string) []Step {
	steps := []Step{
		{ID: StepGuards, Label: "Checking this server"},
		{ID: StepSwap, Label: "Switching to " + to},
		{ID: StepRestart, Label: "Restarting the panel"},
		{ID: StepHealth, Label: "Health check"},
	}
	for i := range steps {
		steps[i].Status = StepWaiting
	}
	return steps
}

// PanelUpdate is a finished job in the history card.
type PanelUpdate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	JobID       string    `gorm:"size:40;uniqueIndex" json:"job_id"`
	Kind        string    `gorm:"size:16" json:"kind"`
	FromVersion string    `gorm:"size:64" json:"from"`
	ToVersion   string    `gorm:"size:64" json:"to"`
	Result      string    `gorm:"size:16" json:"result"`
	Reason      string    `json:"reason,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

func (PanelUpdate) TableName() string { return "panel_updates" }
