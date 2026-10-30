// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// the agent on the node is v1.3.0, and the release has v1.4.0
func newUpdateRig(t *testing.T) *linkRig {
	t.Helper()
	r := newLinkRig(t)
	r.files.version = "v1.4.0"
	r.env.Settle = 20 * time.Second // an agent takes seconds to restart
	return r
}

func TestUpdateAgentSendsTheReleaseAndVerifiesTheNewAgent(t *testing.T) {
	r := newUpdateRig(t)
	c := r.connect()
	v, err := r.env.UpdateAgent(context.Background(), c, r.node, r.progress)
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != "v1.4.0" {
		t.Errorf("versions %+v", v)
	}

	// what the agent was sent: the release's own, as SelfUpdate wants it, and never forced
	ag, _ := r.files.Agent(context.Background(), "amd64")
	ups := r.ag.Updates()
	if len(ups) != 1 {
		t.Fatalf("%d updates", len(ups))
	}
	u := ups[0]
	if string(u.BinaryContent) != string(ag.Gz) || u.Checksum != ag.PlainSHA256 || u.Version != "v1.4.0" || !u.RestartAfter || u.Force || string(u.Signature) != string(ag.Sig) {
		t.Errorf("update: version %q, restart %v, force %v, checksum %.12s (want %.12s), %d bytes", u.Version, u.RestartAfter, u.Force, u.Checksum, ag.PlainSHA256, len(u.BinaryContent))
	}
	if len(u.Signature) != 64 || u.Checksum != sum256(mustGunzip(t, u.BinaryContent)) {
		t.Errorf("the checksum is of the packed bytes or the signature is the wrong length: %d", len(u.Signature))
	}

	if got, want := r.stepNames(), UpdateAgentPlan(); !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
	for _, name := range UpdateAgentPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
	if d := r.lastStep(StepCheckAgent).Detail; d != "v1.4.0, managed profile" {
		t.Errorf("check says %q", d)
	}

	// the client it was given isn't touched, and reaches the new agent by itself
	deadline := time.Now().Add(15 * time.Second)
	for {
		info, err := c.GetVersion(context.Background())
		if err == nil && info.AgentVersion == "v1.4.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the manager's own client never reached the new agent: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestUpdateAgentLeavesAnAgentThatIsOnTheRelease(t *testing.T) {
	r := newUpdateRig(t)
	r.files.version = "v1.3.0"
	v, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	if err != nil || v.Agent != "v1.3.0" {
		t.Fatalf("%+v, %v", v, err)
	}
	if len(r.ag.Updates()) != 0 {
		t.Error("the agent was sent itself")
	}
	// a job's steps don't go missing: each one of the plan is done
	for _, name := range UpdateAgentPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
	if d := r.lastStep(StepDownloadAgent).Detail; !strings.Contains(d, "already") {
		t.Errorf("download says %q", d)
	}
}

func TestUpdateAgentSaysWhatTheAgentRefusedWith(t *testing.T) {
	r := newUpdateRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnSelfUpdate = func(*pb.UpdateRequest) *pb.UpdateResponse {
			return &pb.UpdateResponse{Success: false, Message: "Downgrade rejected: current=v1.5.0 requested=v1.4.0 (use force to override)", OldVersion: "v1.5.0"}
		}
	})
	_, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	if !errors.Is(err, ErrAgentRefused) || !strings.Contains(err.Error(), "Downgrade rejected") {
		t.Fatalf("err = %v", err)
	}
	if last := r.lastStep(StepUploadAgent); last.State != StateFailed {
		t.Errorf("step %+v", last)
	}
	if slices.Contains(r.stepNames(), StepRestartAgent) {
		t.Error("it waited for a restart that wasn't coming")
	}
}

func TestUpdateAgentPassesReleaseErrorsThroughAndSendsNothing(t *testing.T) {
	r := newUpdateRig(t)
	r.files.agentErr = wrapped{artifacts.ErrAgentTooOld}
	_, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	if !errors.Is(err, artifacts.ErrAgentTooOld) || strings.Contains(err.Error(), "artifacts:") {
		t.Fatalf("err = %v", err)
	}
	if len(r.ag.Updates()) != 0 {
		t.Error("something was sent")
	}
}

func TestUpdateAgentNeedsADownloadSource(t *testing.T) {
	r := newUpdateRig(t)
	r.env.Files = nil
	if _, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, nil); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("err = %v", err)
	}
}

// the agent takes the update and doesn't come back
func TestUpdateAgentThatNeverComesBackSaysSoWithThePort(t *testing.T) {
	r := newUpdateRig(t)
	r.env.Settle = 3 * time.Second
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnSelfUpdate = func(*pb.UpdateRequest) *pb.UpdateResponse {
			go func() { time.Sleep(100 * time.Millisecond); a.Stop() }()
			return &pb.UpdateResponse{Success: true, Message: "Update successful, restarting...", NewVersion: "v1.4.0"}
		}
	})
	_, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	var u *UnreachableError
	if !errors.Is(err, ErrUnreachable) || !errors.As(err, &u) || u.What != "restart" || u.Port != r.node.AgentPort {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "after the update") || !strings.Contains(err.Error(), "TCP port") {
		t.Errorf("the message: %q", err)
	}
}

// it comes back as what it was: the update didn't take
func TestUpdateAgentThatComesBackAsTheOldVersionIsAMismatch(t *testing.T) {
	r := newUpdateRig(t)
	r.env.Settle = 3 * time.Second
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnSelfUpdate = func(*pb.UpdateRequest) *pb.UpdateResponse {
			go func() { time.Sleep(100 * time.Millisecond); a.Restart("v1.3.0", 200*time.Millisecond) }()
			return &pb.UpdateResponse{Success: true, Message: "Update successful, restarting...", NewVersion: "v1.4.0"}
		}
	})
	_, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	var mm *VersionMismatchError
	if !errors.Is(err, ErrVersionMismatch) || !errors.As(err, &mm) || mm.Want != "v1.4.0" || mm.Got != "v1.3.0" {
		t.Fatalf("err = %v", err)
	}
}

// it comes back as the right version and not managed: an agent that would give a shell
func TestUpdateAgentThatComesBackUnmanagedIsRefused(t *testing.T) {
	r := newUpdateRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnSelfUpdate = func(r *pb.UpdateRequest) *pb.UpdateResponse {
			go func() {
				time.Sleep(100 * time.Millisecond)
				a.Configure(func(a *fakeagent.Agent) { a.Full = true })
				a.Restart(r.Version, 200*time.Millisecond)
			}()
			return &pb.UpdateResponse{Success: true, Message: "Update successful, restarting...", NewVersion: r.Version}
		}
	})
	_, err := r.env.UpdateAgent(context.Background(), r.connect(), r.node, r.progress)
	if !errors.Is(err, ErrNotManaged) {
		t.Fatalf("err = %v", err)
	}
	if last := r.lastStep(StepCheckAgent); last.State != StateFailed {
		t.Errorf("step %+v", last)
	}
}

func TestRenewCertToAnAgentThatPredatesItSaysToUpdateTheAgent(t *testing.T) {
	r := newLinkRig(t) // the stand-in doesn't serve InstallCertificate: Unimplemented, as an old agent says
	_, err := r.env.RenewCert(context.Background(), r.connect(), r.node)
	if !errors.Is(err, ErrAgentOutdated) || !strings.Contains(err.Error(), "Update the agent first") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewCertNeedsACAAndAHost(t *testing.T) {
	r := newLinkRig(t)
	c := r.connect()
	if _, err := (&Env{}).RenewCert(context.Background(), c, r.node); !errors.Is(err, ErrNoCA) {
		t.Errorf("without a CA: %v", err)
	}
	bad := r.node
	bad.Host = "not a host"
	if _, err := r.env.RenewCert(context.Background(), c, bad); err == nil {
		t.Error("a bad host was accepted")
	}
}

func mustGunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	return gunzipBytes(t, b)
}
