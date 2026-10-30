// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAStepGoesRunningThenOK(t *testing.T) {
	var got []Step
	st := newStepper(func(s Step) { got = append(got, s) })
	done := st.begin(StepCheckServer)
	if err := done("amd64 · Debian 12", nil); err != nil {
		t.Fatal(err)
	}
	want := []Step{
		{StepCheckServer, StateRunning, ""},
		{StepCheckServer, StateOK, "amd64 · Debian 12"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v\nwant   %+v", got, want)
	}
}

func TestAFailedStepSaysWhyAndPassesTheErrorOn(t *testing.T) {
	var got []Step
	st := newStepper(func(s Step) { got = append(got, s) })
	boom := errors.New("the disk is full")
	if err := st.begin(StepUploadAgent)("", boom); err != boom {
		t.Fatalf("the error came back as %v", err)
	}
	if last := got[len(got)-1]; last.State != StateFailed || last.Detail != "the disk is full" {
		t.Fatalf("last step %+v", last)
	}
}

func TestANilProgressIsFine(t *testing.T) {
	st := newStepper(nil)
	st.begin("x")("", nil)
	st.skip("y", "")
	st.progress("z", "1%")
	var none *stepper
	none.emit(Step{}) // and so is no stepper at all
}

func TestASkippedStepIsShownDone(t *testing.T) {
	var got []Step
	st := newStepper(func(s Step) { got = append(got, s) })
	st.skip(StepUploadXray, "")
	st.skip(StepCheckXray, "26.10.21 is already there")
	want := []Step{
		{StepUploadXray, StateOK, "Not needed"},
		{StepCheckXray, StateOK, "26.10.21 is already there"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v", got)
	}
}

// An upload reports from the goroutine that sends, while the step that owns it
// waits. Progress never has to cope with two calls at once.
func TestProgressIsCalledOneAtATime(t *testing.T) {
	var inside, overlaps atomic.Int32
	st := newStepper(func(Step) {
		if inside.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(time.Millisecond)
		inside.Add(-1)
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				st.progress("x", "1%")
			}
		}()
	}
	wg.Wait()
	if overlaps.Load() != 0 {
		t.Fatalf("%d calls overlapped", overlaps.Load())
	}
}

func TestThePlansNameKnownStepsOnce(t *testing.T) {
	known := []string{
		StepCheckServer, StepDownloadAgent, StepCertificate, StepPrepareServer, StepUploadAgent, StepStartAgent,
		StepCheckAgent, StepStopAgent, StepRemoveAgent, StepRestartAgent, StepDownloadXray, StepUploadXray,
		StepCheckXray, StepChooseTarget, StepCreateKeys, StepApplyConfig,
	}
	for name, plan := range map[string][]string{
		"install": InstallPlan(), "uninstall": UninstallPlan(), "provision": ProvisionPlan(),
		"update agent": UpdateAgentPlan(), "update xray": UpdateXrayPlan(),
	} {
		seen := map[string]bool{}
		for _, s := range plan {
			if !slices.Contains(known, s) {
				t.Errorf("%s: %q is not a step name", name, s)
			}
			if seen[s] {
				t.Errorf("%s: %q twice", name, s)
			}
			seen[s] = true
		}
		if len(plan) < 3 {
			t.Errorf("%s: %v", name, plan)
		}
	}
	// the manager may keep or change what it gets
	p := InstallPlan()
	p[0] = "changed"
	if InstallPlan()[0] == "changed" {
		t.Error("the plan is shared")
	}
}
