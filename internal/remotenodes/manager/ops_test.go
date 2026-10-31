// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// The operations are made for the CA in use: once, and again when another CA
// takes its place, which a backup restore does. An agentops.Env holds the CA
// it signs with, so one made for the old CA would sign with it still.
func TestOpsAreMadeOnceForACAAndAgainWhenAnotherTakesItsPlace(t *testing.T) {
	r := newOpsRig(t)
	first, err := r.m.Ops()
	if err != nil || first != Ops(r.ops) {
		t.Fatalf("Ops = %v, %v", first, err)
	}
	for range 3 {
		r.m.Sync() // looks at the CA each time
		if _, err := r.m.Ops(); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.madeOps) != 1 || r.madeOps[0] != string(r.m.CA().CertPEM()) {
		t.Fatalf("made %d times, for %d CAs", len(r.madeOps), len(r.madeOps))
	}

	other := newCA(t, epoch)
	setCA(t, r.cfg.Store, other)
	r.m.Sync()
	if _, err := r.m.Ops(); err != nil {
		t.Fatal(err)
	}
	if len(r.madeOps) != 2 || r.madeOps[1] != string(other.CertPEM()) {
		t.Fatalf("after the CA was replaced, ops were made %d times: %v", len(r.madeOps), r.madeOps)
	}
}

func TestOpsSayWhyThereAreNone(t *testing.T) {
	// no way to make them
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	if _, err := m.Ops(); !errors.Is(err, ErrNoOps) {
		t.Errorf("a manager with no NewOps: %v", err)
	}
	_, err = m.operate()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInternal || !strings.Contains(apiErr.Msg, "isn't set up") {
		t.Errorf("worded for the page: %v", err)
	}

	// a clock that can't be right means no CA, and so nothing to sign or dial with
	r := newOpsRig(t, func(c *Config) { c.Now = func() time.Time { return time.Unix(0, 0) } })
	if r.m.CA() != nil {
		t.Fatal("a CA was made on a clock that reads 1970")
	}
	_, err = r.m.Ops()
	if err == nil || !strings.Contains(err.Error(), "certificate authority") || !strings.Contains(err.Error(), "1970") {
		t.Errorf("Ops with no CA: %v", err)
	}
	if len(r.madeOps) != 0 {
		t.Error("operations were made with no CA")
	}
	_, err = r.m.operate()
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInternal || !strings.HasSuffix(apiErr.Msg, ".") || !strings.Contains(apiErr.Msg, "1970") {
		t.Errorf("worded for the page: %v", err)
	}
}

// A connection that isn't the agent's own client can't carry the calls, which
// take that type. Each says so plainly, and without calling the Env.
func TestEnvOpsRefuseAConnectionThatIsNotTheAgentClient(t *testing.T) {
	ops := EnvOps(&agentops.Env{})
	ctx := context.Background()
	foreign := &fakeConn{a: newAgent()}
	n := model.Node{ID: 1, Name: "n", Host: "203.0.113.9", AgentPort: 9090, EntryPort: 443}
	calls := map[string]func() error{
		"Provision":   func() error { _, err := ops.Provision(ctx, foreign, agentops.ProvisionInput{Node: n}, nil); return err },
		"PushConfig":  func() error { return ops.PushConfig(ctx, foreign, nodecfg.Input{}) },
		"CheckTarget": func() error { _, err := ops.CheckTarget(ctx, foreign, "a.example:443"); return err },
		"UpdateAgent": func() error { _, err := ops.UpdateAgent(ctx, foreign, n, nil); return err },
		"UpdateXray":  func() error { _, err := ops.UpdateXray(ctx, foreign, n, nil); return err },
		"RenewCert":   func() error { _, err := ops.RenewCert(ctx, foreign, n); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, errForeignClient) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a nil client is as foreign as a stand-in
	if _, err := agentOf(nil); !errors.Is(err, errForeignClient) {
		t.Errorf("agentOf(nil) = %v", err)
	}

	// a Connect that fails gives no client at all: a typed nil in the interface would look like one
	c, err := ops.Connect(ctx, n)
	if c != nil || !errors.Is(err, agentops.ErrNoCA) {
		t.Errorf("Connect = %v, %v", c, err)
	}
}

func TestAFailureIsWordedForThePage(t *testing.T) {
	unreach := &agentops.UnreachableError{What: "agent", Host: "203.0.113.9", Port: 9090, Err: errors.New("i/o timeout")}
	apiErr := &api.Error{Code: api.CodeNameTaken, Msg: "That name is taken."}
	invalid := &model.InvalidError{Msg: "the host is required"}
	for _, c := range []struct {
		name string
		err  error
		code string // "" for an error that goes on as it is
		text string // a part of the message
	}{
		{"a changed host key", &agentops.HostKeyChangedError{Host: "h", Pinned: "SHA256:a", Got: "SHA256:b"}, api.CodeHostKeyChanged, "SHA256:b"},
		{"an agent that doesn't answer", unreach, api.CodeNodeUnreachable, "TCP port 9090"},
		{"the same, wrapped", fmt.Errorf("while updating: %w", unreach), api.CodeNodeUnreachable, "TCP port 9090"},
		{"a certificate the agent turned down", agentops.ErrAgentCert, api.CodeNodeUnreachable, "certificate"},
		{"another Xray than the one sent", &agentops.VersionMismatchError{What: "Xray", Want: "26.10.21", Got: "26.10.10"}, api.CodeVersionMismatch, "26.10.10"},
		{"an agent that is too old", agentops.ErrAgentOutdated, api.CodeAgentTooOld, "too old"},
		{"input that can't make a config", fmt.Errorf("%w: entry port 0 is out of range", nodecfg.ErrInvalid), api.CodeBadRequest, "Entry port 0 is out of range."},
		{"what the node refused", &agentops.ConfigRejectedError{Message: "bad key"}, api.CodeInternal, "refused the configuration"},
		{"something with no sentinel", errors.New("the disk is full"), api.CodeInternal, "the disk is full"},
		{"a code already there", apiErr, "", ""},
		{"a bad value", invalid, "", ""},
		{"not found", notFound("node", 3), "", ""},
		{"a node with a job", &jobs.BusyError{NodeID: 1, JobID: "abc"}, "", ""},
		{"the caller giving up", context.Canceled, "", ""},
	} {
		got := opFailure(c.err)
		if c.code == "" {
			if got != c.err {
				t.Errorf("%s: %v became %v, want it left as it is", c.name, c.err, got)
			}
			continue
		}
		var e *api.Error
		if !errors.As(got, &e) || e.Code != c.code || !strings.Contains(e.Msg, c.text) {
			t.Errorf("%s: %v became %#v, want code %s with %q", c.name, c.err, got, c.code, c.text)
		}
	}
	if opFailure(nil) != nil {
		t.Error("no error became one")
	}
	// a long message is cut for the page, in characters and not in bytes
	long := opFailure(errors.New(strings.Repeat("ж", 2*maxMsg)))
	var e *api.Error
	if !errors.As(long, &e) || len([]rune(e.Msg)) != maxMsg || !strings.HasSuffix(e.Msg, "…") {
		t.Errorf("a long message: %d characters", len([]rune(e.Msg)))
	}
}

func TestSentencesStartWithACapitalAndEndWithAStop(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "  the name is required ": "The name is required.", "Already one.": "Already one.", "What now?": "What now?",
		"ok!": "Ok!", "été": "été.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAJobStartsOrIsRefusedInWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		defer r.stop()
		hold := make(chan struct{})
		id, err := r.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: 7, Target: "n"}, func(ctx context.Context, _ *jobs.Run) error {
			<-hold
			return nil
		})
		must(t, err)

		// a second on the same node is busy, and says which job
		_, err = r.m.startJob(jobs.Spec{Kind: jobs.KindRotateKeys, NodeID: 7}, func(context.Context, *jobs.Run) error { return nil })
		var busy *jobs.BusyError
		if !errors.Is(err, jobs.ErrBusy) || !errors.As(err, &busy) || busy.JobID != id {
			t.Errorf("a second job on the node: %v", err)
		}
		// another node is free to go
		other, err := r.m.startJob(jobs.Spec{Kind: jobs.KindRotateKeys, NodeID: 8}, func(context.Context, *jobs.Run) error { return nil })
		must(t, err)
		close(hold)
		r.ok(id)
		r.ok(other)

		// with the engine closed there is nothing to run on
		must(t, r.jobs.Close(context.Background()))
		_, err = r.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: 9}, func(context.Context, *jobs.Run) error { return nil })
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInternal || !strings.Contains(apiErr.Msg, "shutting down") {
			t.Errorf("after Close: %v", err)
		}

		// and with no engine at all, the same, in other words
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		_, err = m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: 1}, func(context.Context, *jobs.Run) error { return nil })
		if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInternal || !strings.Contains(apiErr.Msg, "isn't set up") {
			t.Errorf("with no engine: %v", err)
		}
	})
}

// What agentops reports of its steps is what the job shows, with the same
// words for the states, and an upload's share done in the detail.
func TestWhatAgentopsReportsIsWhatTheJobShows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		defer r.stop()
		id, err := r.m.startJob(jobs.Spec{Kind: jobs.KindInstall, NodeID: 1, Steps: agentops.UpdateAgentPlan()}, func(ctx context.Context, run *jobs.Run) error {
			progress := progressTo(run)
			progress(agentops.Step{Name: agentops.StepDownloadAgent, State: agentops.StateRunning})
			progress(agentops.Step{Name: agentops.StepDownloadAgent, State: agentops.StateOK, Detail: "v1.3.1 · 18 MB"})
			progress(agentops.Step{Name: agentops.StepUploadAgent, State: agentops.StateRunning, Detail: "38% · 3.9 MB sent"})
			progress(agentops.Step{Name: agentops.StepUploadAgent, State: agentops.StateFailed, Detail: "The connection broke."})
			return errors.New("The connection broke.")
		})
		must(t, err)
		j := r.failed(id)
		want := []jobs.Step{
			{Key: "download_agent", Name: agentops.StepDownloadAgent, State: jobs.StepOK, Detail: "v1.3.1 · 18 MB"},
			{Key: "upload_agent", Name: agentops.StepUploadAgent, State: jobs.StepFailed, Detail: "The connection broke."},
			{Key: "restart_agent", Name: agentops.StepRestartAgent, State: jobs.StepPending},
			{Key: "check_agent", Name: agentops.StepCheckAgent, State: jobs.StepPending},
		}
		if fmt.Sprint(j.Steps) != fmt.Sprint(want) {
			t.Errorf("steps = %+v\nwant    %+v", j.Steps, want)
		}
	})
}

// What an install or an update downloaded is let go of when the last job that
// might hold it ends, not before: a second job may still be sending it.
func TestDownloadsAreLetGoWhenTheLastJobThatUsesThemEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		defer r.stop()
		holdA, holdB := make(chan struct{}), make(chan struct{})
		run := func(node uint, hold chan struct{}) string {
			id, err := r.m.startJob(jobs.Spec{Kind: jobs.KindUpdateXray, NodeID: node}, r.m.usesDownloads(func(context.Context, *jobs.Run) error {
				<-hold
				return nil
			}))
			must(t, err)
			return id
		}
		a, b := run(1, holdA), run(2, holdB)
		synctest.Wait()
		close(holdA)
		r.ok(a)
		if n := r.dl.releases(); n != 0 {
			t.Fatalf("let go %d times while a job was still running", n)
		}
		close(holdB)
		r.ok(b)
		if n := r.dl.releases(); n != 1 {
			t.Fatalf("let go %d times after both ended, want once", n)
		}
		// a job that fails counts the same
		id, err := r.m.startJob(jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: 1}, r.m.usesDownloads(func(context.Context, *jobs.Run) error { return errBoom }))
		must(t, err)
		r.failed(id)
		if n := r.dl.releases(); n != 2 {
			t.Errorf("let go %d times after a failed job, want 2 in all", n)
		}
	})
	// with nothing to let go of, a job is just a job
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, func(c *Config) { c.Downloads = nil })
		defer r.stop()
		id, err := r.m.startJob(jobs.Spec{Kind: jobs.KindUpdateXray, NodeID: 1}, r.m.usesDownloads(func(context.Context, *jobs.Run) error { return nil }))
		must(t, err)
		r.ok(id)
	})
}

var errBoom = errors.New("boom")

// What an operation on a node asks first: that it is there, that nothing runs
// on it, that its install is over, and that its agent can be reached.
func TestAnOperationsPreconditionsAreAskedInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		defer r.stop()
		ready := addNode(t, r.cfg.Store)
		unfinished := addNode(t, r.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		down := addNode(t, r.cfg.Store)
		r.net.agent(down.ID).setDown(true)
		r.start()

		var apiErr *api.Error
		if _, err := r.m.settled(99); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		if _, err := r.m.settled(unfinished.ID); !errors.As(err, &apiErr) || apiErr.Code != api.CodeBadRequest || !strings.Contains(apiErr.Msg, "didn't finish") {
			t.Errorf("a node whose install didn't finish: %v", err)
		}
		n, err := r.m.settled(ready.ID)
		if err != nil || n.ID != ready.ID {
			t.Fatalf("a node that is ready: %v", err)
		}

		// a job on it comes before everything else, an unfinished install included
		hold := make(chan struct{})
		id1, err := r.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: ready.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		id2, err := r.m.startJob(jobs.Spec{Kind: jobs.KindInstall, NodeID: unfinished.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		for _, id := range []uint{ready.ID, unfinished.ID} {
			if _, err := r.m.settled(id); !errors.Is(err, jobs.ErrBusy) {
				t.Errorf("node %d with a job: %v", id, err)
			}
		}
		close(hold)
		r.ok(id1)
		r.ok(id2)

		// its own connection, when it has one
		c, err := r.m.link(ready)
		if err != nil || c == nil {
			t.Fatalf("link of a node that answers: %v, %v", c, err)
		}
		if got := c.(*fakeConn).a; got != r.net.agent(ready.ID) {
			t.Error("the link is another node's")
		}
		if _, err := r.m.link(down); !errors.As(err, &apiErr) || apiErr.Code != api.CodeNodeUnreachable || !strings.Contains(apiErr.Msg, down.Name) {
			t.Errorf("link of a node that is down: %v", err)
		}
	})
}
