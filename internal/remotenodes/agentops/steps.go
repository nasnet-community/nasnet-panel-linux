// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"slices"
	"sync"
)

// Step is one line of a job's progress, as the page shows it. A step goes
// running, then ok or failed; while an upload runs, Detail carries how far it
// has got ("38% · 3.9 MB sent · about 2 min left").
type Step struct {
	Name   string
	State  string // StateRunning, StateOK or StateFailed
	Detail string
}

const (
	StateRunning = "running"
	StateOK      = "ok"
	StateFailed  = "failed"
)

// Progress hears about steps as they happen. Nil is fine. Calls come one at a
// time, though not always from the goroutine that made the call, since an
// upload reports from the one that sends it.
type Progress func(Step)

// The step names are plain English. The page words each step by the key that
// the manager gives it (manager.StepKey) and shows the name for a key it has no
// words for. Each operation's plan lists the steps it goes through, in order,
// so the manager can show what is still to come.
const (
	StepCheckServer   = "Check the server"
	StepDownloadAgent = "Download the agent"
	StepCertificate   = "Create the certificate"
	StepPrepareServer = "Prepare the server"
	StepUploadAgent   = "Upload the agent"
	StepStartAgent    = "Start the agent"
	StepCheckAgent    = "Check the agent"
	StepStopAgent     = "Stop the agent"
	StepRemoveAgent   = "Remove the agent"
	StepRestartAgent  = "Restart the agent"
	StepDownloadXray  = "Download Xray"
	StepUploadXray    = "Upload Xray"
	StepCheckXray     = "Check Xray"
	StepChooseTarget  = "Choose a Reality target"
	StepCreateKeys    = "Create the keys"
	StepApplyConfig   = "Apply the configuration"
	// UndoInstall's one step, after an install that failed.
	StepCleanServer = "Clean up the server"
	// The install's step that takes over an Xray the server runs, when the person
	// asked for that: it is in the plan only then.
	StepTakeOverXray = "Take over the server's Xray"
)

// skippedDetail is what a step says when there was nothing for it to do.
const skippedDetail = "Not needed"

// InstallPlan is the steps of InstallAgent, in order.
func InstallPlan() []string {
	return []string{StepCheckServer, StepDownloadAgent, StepCertificate, StepPrepareServer, StepUploadAgent, StepStartAgent, StepCheckAgent}
}

// InstallPlanFor is the steps of InstallAgent with these options, in order. A
// takeover goes after everything that only looks or fetches (the check, the
// agent's download, the certificate), so that a failure there costs the
// server's own Xray nothing, and before the server is written to.
func InstallPlanFor(opts InstallOptions) []string {
	plan := InstallPlan()
	if !opts.TakeOverXray {
		return plan
	}
	i := slices.Index(plan, StepCertificate) + 1
	return slices.Insert(plan, i, StepTakeOverXray)
}

// UninstallPlan is the steps of Uninstall, in order.
func UninstallPlan() []string {
	return []string{StepCheckServer, StepStopAgent, StepRemoveAgent}
}

// ProvisionPlan is the steps of Provision, in order.
func ProvisionPlan() []string {
	return []string{StepDownloadXray, StepUploadXray, StepCheckXray, StepChooseTarget, StepCreateKeys, StepApplyConfig}
}

// UpdateAgentPlan is the steps of UpdateAgent, in order.
func UpdateAgentPlan() []string {
	return []string{StepDownloadAgent, StepUploadAgent, StepRestartAgent, StepCheckAgent}
}

// UpdateXrayPlan is the steps of UpdateXray, in order.
func UpdateXrayPlan() []string {
	return []string{StepDownloadXray, StepUploadXray, StepCheckXray}
}

// stepper reports an operation's steps. It serializes the calls, so a
// Progress never has to worry about being called twice at once.
type stepper struct {
	mu sync.Mutex
	fn Progress
}

func newStepper(fn Progress) *stepper { return &stepper{fn: fn} }

func (s *stepper) emit(st Step) {
	if s == nil || s.fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fn(st)
}

// begin starts a step and returns what finishes it. The finisher passes its
// error on, so a step reads
//
//	done := st.begin(name)
//	...
//	return done(detail, err)
func (s *stepper) begin(name string) func(detail string, err error) error {
	s.emit(Step{Name: name, State: StateRunning})
	return func(detail string, err error) error {
		if err != nil {
			s.emit(Step{Name: name, State: StateFailed, Detail: err.Error()})
			return err
		}
		s.emit(Step{Name: name, State: StateOK, Detail: detail})
		return nil
	}
}

// skip is a step that has nothing to do this time. It is shown done, so a
// finished job doesn't keep steps waiting that never will run.
func (s *stepper) skip(name, detail string) {
	if detail == "" {
		detail = skippedDetail
	}
	s.emit(Step{Name: name, State: StateOK, Detail: detail})
}

// progress is the step's running line, for the meter to update.
func (s *stepper) progress(name, detail string) {
	s.emit(Step{Name: name, State: StateRunning, Detail: detail})
}
