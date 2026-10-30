// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestPushXraySendsThePinnedBuildGzippedWithItsHash(t *testing.T) {
	r := newLinkRig(t)
	r.files.xrayPad = 3<<20 + 300_000 // random, so about 3.3 MB goes over, in several messages
	c := r.connect()

	if err := r.env.PushXray(context.Background(), c, r.node, false, r.progress); err != nil {
		t.Fatal(err)
	}
	pushes := r.ag.Pushes()
	if len(pushes) != 1 {
		t.Fatalf("%d pushes", len(pushes))
	}
	p := pushes[0]
	x, _ := r.files.Xray(context.Background(), "amd64")
	if p.Header.Version != pin.Version || p.Header.Sha256 != x.PlainSHA256 || !p.Header.Gzip || p.Header.RestartAfter {
		t.Errorf("header %+v", p.Header)
	}
	// what arrives, unpacked, is the build, and it came in messages the agent takes
	if sum256(p.Build) != x.PlainSHA256 || int64(len(p.Build)) != x.PlainSize {
		t.Errorf("the agent got %d bytes with hash %s, want %d with %s", len(p.Build), sum256(p.Build), x.PlainSize, x.PlainSHA256)
	}
	if p.Chunks < 3 || p.MaxChunk > 1<<20 {
		t.Errorf("%d messages, the largest %d bytes", p.Chunks, p.MaxChunk)
	}

	// the steps, with the upload counted and the node's own answer to close
	if got, want := r.stepNames(), []string{StepDownloadXray, StepUploadXray, StepCheckXray}; !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
	if got, want := r.stepNames(), UpdateXrayPlan(); !slices.Equal(got, want) {
		t.Errorf("not the plan: %q", got)
	}
	if d := r.lastStep(StepCheckXray); d.State != StateOK || d.Detail != "Xray "+pin.Version {
		t.Errorf("check: %+v", d)
	}
	re := regexp.MustCompile(`^\d+% · [\d.]+ (KB|MB) sent`)
	var counted int
	for _, s := range r.steps {
		if s.Name == StepUploadXray && s.State == StateRunning && re.MatchString(s.Detail) {
			counted++
		}
	}
	if counted == 0 {
		t.Errorf("no progress was reported for the upload: %+v", r.steps)
	}
}

// What the node says it is, not what the node record says, decides the build.
func TestPushXrayTakesTheArchitectureFromTheAgent(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) { a.Arch = "arm64" })
	r.node.Arch = "amd64" // out of date
	if err := r.env.PushXray(context.Background(), r.connect(), r.node, false, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.files.asked, []string{"xray arm64"}) {
		t.Errorf("the build was asked for as %v", r.files.asked)
	}
}

func TestPushXrayLeavesANodeThatHasThePinnedBuild(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.XrayLine = "Xray " + pin.Version + " (Xray, Penetrates Everything.) 1b1647c (go1.27.1 linux/amd64)"
	})
	c := r.connect()
	if err := r.env.PushXray(context.Background(), c, r.node, true, r.progress); err != nil {
		t.Fatal(err)
	}
	if len(r.ag.Pushes()) != 0 || len(r.files.asked) != 0 {
		t.Errorf("sent %d builds, downloaded %v", len(r.ag.Pushes()), r.files.asked)
	}
	for _, name := range UpdateXrayPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s: %+v", name, last)
		}
	}
	if d := r.lastStep(StepDownloadXray).Detail; !strings.Contains(d, "already") {
		t.Errorf("download says %q", d)
	}
}

func TestPushXrayStartsXrayOnlyWhenAsked(t *testing.T) {
	for _, restart := range []bool{false, true} {
		r := newLinkRig(t)
		if err := r.env.PushXray(context.Background(), r.connect(), r.node, restart, nil); err != nil {
			t.Fatal(err)
		}
		if got := r.ag.Pushes()[0].Header.RestartAfter; got != restart {
			t.Errorf("restart %v was sent as %v", restart, got)
		}
		// asked to start Xray, the hub also wants to see it running
		calls := r.ag.Calls()
		if slices.Contains(calls, "GetStatus") != restart {
			t.Errorf("restart %v: the calls were %v", restart, calls)
		}
	}
}

// An agent from before the call answers Unimplemented, and the hub says what to do.
func TestPushXrayToAnAgentThatPredatesItSaysToUpdateTheAgent(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) { a.NoPushXray = true })
	err := r.env.PushXray(context.Background(), r.connect(), r.node, false, r.progress)
	if !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Update the agent first") {
		t.Errorf("the message: %q", err)
	}
	if last := r.lastStep(StepUploadXray); last.State != StateFailed || last.Detail != err.Error() {
		t.Errorf("step %+v", last)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("the status is gone from under the error: %v", status.Code(err))
	}
}

func TestPushXrayReportsWhatTheAgentRefusedWith(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnPushXray = func(*pb.XrayBinaryHeader, []byte) *pb.CommandResponse {
			return &pb.CommandResponse{Success: false, Message: "Installed binary failed to report a version; rolled back"}
		}
	})
	err := r.env.PushXray(context.Background(), r.connect(), r.node, false, r.progress)
	if err == nil || !strings.Contains(err.Error(), "rolled back") || !strings.HasPrefix(err.Error(), "The node didn't take") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "xray push failed") {
		t.Errorf("the client's own words are in it: %q", err)
	}
}

// The node says it has taken the build and goes on reporting another version.
func TestPushXrayChecksWhichVersionTheNodeRunsAfterwards(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnPushXray = func(*pb.XrayBinaryHeader, []byte) *pb.CommandResponse {
			return &pb.CommandResponse{Success: true, Message: "Xray updated"} // and XrayLine stays what it was
		}
	})
	err := r.env.PushXray(context.Background(), r.connect(), r.node, false, r.progress)
	var mm *VersionMismatchError
	if !errors.Is(err, ErrVersionMismatch) || !errors.As(err, &mm) || mm.What != "Xray" || mm.Want != pin.Version || mm.Got != "26.10.10" {
		t.Fatalf("err = %v", err)
	}
	if last := r.lastStep(StepCheckXray); last.State != StateFailed {
		t.Errorf("step %+v", last)
	}
}

func TestPushXrayWantsXrayRunningWhenItWasToBeStarted(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnStatus = func() *pb.NodeStatus { return &pb.NodeStatus{XrayRunning: false} }
	})
	err := r.env.PushXray(context.Background(), r.connect(), r.node, true, nil)
	if !errors.Is(err, ErrXrayNotRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushXrayPassesDownloadErrorsThrough(t *testing.T) {
	r := newLinkRig(t)
	r.files.xrayErr = wrapped{artifacts.ErrBadHash}
	err := r.env.PushXray(context.Background(), r.connect(), r.node, false, r.progress)
	if !errors.Is(err, artifacts.ErrBadHash) || strings.Contains(err.Error(), "artifacts:") {
		t.Fatalf("err = %v", err)
	}
	if len(r.ag.Pushes()) != 0 {
		t.Error("something was sent")
	}
	if got := r.stepNames(); !slices.Equal(got, []string{StepDownloadXray}) {
		t.Errorf("steps %q", got)
	}
}

func TestPushXrayNeedsADownloadSource(t *testing.T) {
	r := newLinkRig(t)
	r.env.Files = nil
	if err := r.env.PushXray(context.Background(), r.connect(), r.node, false, nil); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushXrayStopsWhenItsContextDoes(t *testing.T) {
	r := newLinkRig(t)
	r.files.xrayPad = 2 << 20
	c := r.connect()
	ctx, cancel := context.WithCancel(context.Background())
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnPushXray = func(*pb.XrayBinaryHeader, []byte) *pb.CommandResponse {
			cancel()
			return &pb.CommandResponse{Success: true}
		}
	})
	err := r.env.PushXray(ctx, c, r.node, false, nil)
	if err == nil {
		t.Fatal("a cancelled push went through")
	}
}

func TestUpdateXrayPushesWithARestartAndReportsWhatTheNodeRuns(t *testing.T) {
	r := newLinkRig(t)
	c := r.connect()
	v, err := r.env.UpdateXray(context.Background(), c, r.node, r.progress)
	if err != nil {
		t.Fatal(err)
	}
	if v.Xray != pin.Version || v.Agent != "v1.3.0" {
		t.Errorf("versions %+v", v)
	}
	if h := r.ag.Pushes()[0].Header; !h.RestartAfter {
		t.Errorf("an update was sent without a restart: %+v", h)
	}
	if got, want := r.stepNames(), UpdateXrayPlan(); !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
}
