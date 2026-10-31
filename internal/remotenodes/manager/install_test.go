// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// uuidRe is the form of an id a link and the state both use.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const (
	sshPassword   = "MARKER-hunter2"
	sshPassphrase = "MARKER-open-sesame"
	sshKeyLine    = "MARKER-b3BlbnNzaC1rZXktdjEAAAAABG5vbmU"
)

var sshKey = "-----BEGIN OPENSSH PRIVATE KEY-----\n" + sshKeyLine + "\n-----END OPENSSH PRIVATE KEY-----\n"

// installReq is what the page sends for a new node, with a password.
func installReq(name, host string) api.InstallRequest {
	return api.InstallRequest{
		Login: api.Login{SSHPort: 22, SSHUser: "root", Password: sshPassword, HostKey: fingerprintA},
		Name:  name, Host: host, EntryPort: 8443,
	}
}

func (r *opsRig) add(req api.InstallRequest) string {
	r.t.Helper()
	id, err := r.m.AddNode(context.Background(), req)
	must(r.t, err)
	return id
}

func (r *opsRig) nodes() []model.Node { return r.cfg.Store.Read().Nodes }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestAddingANodeInstallsItAndSavesItSetUpOnlyWhenAllHasWorked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		release := r.ops.holdAt(agentops.StepUploadAgent)

		jobID := r.add(installReq("Frankfurt", "203.0.113.9"))
		synctest.Wait()

		// under way: the node is saved, not set up, with the key that was agreed to and the id its bridge logs in with
		nodes := r.nodes()
		if len(nodes) != 1 {
			t.Fatalf("%d nodes", len(nodes))
		}
		n := nodes[0]
		if n.ID != 1 || n.Name != "Frankfurt" || n.Host != "203.0.113.9" || n.State != model.NodeInstalling || n.SSHHostKey != fingerprintA ||
			n.SSHPort != 22 || n.SSHUser != "root" || n.AgentPort != 9090 || n.EntryPort != 8443 || !n.CreatedAt.Equal(epoch) {
			t.Errorf("the node while it installs = %+v", n)
		}
		if !uuidRe.MatchString(n.BridgeUUID) {
			t.Errorf("bridge id %q", n.BridgeUUID)
		}
		if n.Arch != "" || n.RealityTarget != "" || n.RealityPublicKey != "" || n.RealityPrivateKey != "" || n.AgentVersion != "" || n.XrayVersion != "" || !n.CertNotAfter.IsZero() {
			t.Errorf("a node that isn't set up already has what the install finds out: %+v", n)
		}
		live := r.m.NodeLive(n.ID)
		if in := live.Installing; in == nil || in.JobID != jobID || in.Step != 5 || in.Steps != len(installSteps()) || in.Label != agentops.StepUploadAgent || in.Key != "upload_agent" {
			t.Errorf("installing = %+v", in)
		}
		if _, ok := r.m.Client(n.ID); ok || r.net.tries(n.ID) != 0 || len(r.bridge.tags()) != 0 {
			t.Error("a node that isn't set up is being watched or carried")
		}
		j, _ := r.jobs.Get(jobID)
		if j.Kind != jobs.KindInstall || j.NodeID != n.ID || j.Target != "Frankfurt" || j.State != jobs.StateRunning {
			t.Errorf("job = %+v", j)
		}
		states := stepStates(j)
		for i, name := range installSteps() {
			want := jobs.StepPending
			switch {
			case i < 4:
				want = jobs.StepOK
			case i == 4:
				want = jobs.StepRunning
			}
			if states[name] != want {
				t.Errorf("step %q is %s, want %s", name, states[name], want)
			}
		}

		release()
		j = r.ok(jobID)
		if !slices.Equal(stepNames(j), installSteps()) {
			t.Errorf("steps %q\nwant  %q", stepNames(j), installSteps())
		}
		for _, s := range j.Steps {
			if s.State != jobs.StepOK {
				t.Errorf("step %+v", s)
			}
		}

		// what agentops was asked, in order, and with what
		if got, want := r.ops.allCalls(), []string{"InstallAgent 1", "Connect 1", "Provision 1"}; !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v", got, want)
		}
		in := r.ops.installs[0]
		if in.node.ID != 1 || in.node.SSHHostKey != fingerprintA || in.node.Host != "203.0.113.9" || in.login.Password != sshPassword {
			t.Errorf("the install was given %+v", in)
		}
		prov := r.ops.provisions[0]
		if prov.Node.ID != 1 || prov.Node.Arch != "arm64" || prov.Node.EntryPort != 8443 || len(prov.Clients) != 0 || prov.Target != "" ||
			prov.Bridge != (nodecfg.Bridge{UUID: n.BridgeUUID, Email: "rvs-t1@reverse", ReverseTag: "rvs-to-node-1"}) {
			t.Errorf("provision was given %+v", prov)
		}
		if err := prov.Keys.Valid(); err != nil {
			t.Errorf("the keys that were made: %v", err)
		}

		// everything the install found out is saved in one go
		n = r.nodes()[0]
		if n.State != model.NodeRegistered || n.Arch != "arm64" || n.AgentVersion != "1.3.0" || n.XrayVersion != pin.Version ||
			n.RealityTarget != "www.google.com:443" || n.RealitySNI != "www.google.com" ||
			n.RealityPublicKey != prov.Keys.PublicKey || n.RealityShortID != prov.Keys.ShortID || n.RealityPrivateKey != prov.Keys.PrivateKey ||
			!n.CertNotBefore.Equal(epoch.Add(-time.Hour)) || !n.CertNotAfter.Equal(epoch.AddDate(1, 0, 0)) ||
			n.SSHHostKey != fingerprintA || n.BridgeUUID != prov.Bridge.UUID {
			t.Errorf("the node after it installed = %+v", n)
		}
		if err := n.Validate(); err != nil {
			t.Errorf("the saved node isn't valid: %v", err)
		}
		if live := r.m.NodeLive(n.ID); live.Installing != nil {
			t.Errorf("still installing: %+v", live.Installing)
		}

		// the loop and the bridge have it from here on
		if _, ok := r.m.Client(n.ID); !ok || r.net.tries(n.ID) == 0 {
			t.Error("the node is not being watched")
		}
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Errorf("the bridge carries %v", got)
		}
		if got := r.m.NodeEvents(n.ID); len(got) != 0 {
			t.Errorf("events = %v: the loop has nothing to say of a node that is what the install left", kinds(got))
		}
		if r.dl.releases() != 1 {
			t.Errorf("downloads let go of %d times", r.dl.releases())
		}
		// and the key is known for the server now, so a probe finds it
		if got, _ := r.m.Probe(context.Background(), api.ProbeRequest{Host: "203.0.113.9", SSHPort: 22}); got.Status != api.ProbeKnown {
			t.Errorf("probe after the install: %+v", got)
		}
	})
}

// An install that stops anywhere leaves a node that is not set up and has no
// job: its page says so, nothing watches it or carries it, and what it was told
// is kept apart from what it found out.
// An install that fails before it has changed the server has nothing to take
// off it: the job forgets the node and says so, and the server can be tried
// again at once.
func TestAnInstallThatFailsBeforeChangingTheServerLeavesNothing(t *testing.T) {
	for _, point := range []string{agentops.StepCheckServer, agentops.StepDownloadAgent, agentops.StepCertificate} {
		t.Run(point, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(point, &agentops.UnreachableError{What: "ssh", Host: "203.0.113.9", Port: 22})
				cause := (&agentops.UnreachableError{What: "ssh", Host: "203.0.113.9", Port: 22}).Error()

				j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
				if want := cause + " Nothing was changed on the server, and nothing was kept."; j.Error != want {
					t.Errorf("the job says %q, want %q", j.Error, want)
				}
				if j.Code != api.CodeNodeUnreachable {
					t.Errorf("code %q", j.Code)
				}
				states := stepStates(j)
				if states[point] != jobs.StepFailed || states[StepDiscardNode] != jobs.StepOK {
					t.Errorf("steps %+v", j.Steps)
				}
				if _, ran := states[agentops.StepCleanServer]; ran {
					t.Errorf("a cleanup ran on a server nothing was done to: %+v", j.Steps)
				}
				if r.ops.called("UndoInstall") != 0 {
					t.Errorf("calls %v", r.ops.allCalls())
				}
				if len(r.nodes()) != 0 {
					t.Fatalf("a node was kept: %+v", r.nodes())
				}
				r.gone(1)
				// the server, and the name, are free for the next try
				r.ops.failAt(point, nil)
				r.ok(r.add(installReq("Frankfurt", "203.0.113.9")))
				if n := r.nodes(); len(n) != 1 || n[0].State != model.NodeRegistered || n[0].ID != 2 {
					t.Errorf("the next try: %+v", n)
				}
			})
		})
	}
}

// An install that fails after it has changed the server takes off it what it
// put there, with the login the job still holds and the key the person agreed
// to, in steps the page shows. Then the node is forgotten, and the job's error
// says that the server was cleaned up and nothing was kept.
func TestAnInstallThatFailsAfterChangingTheServerCleansItUp(t *testing.T) {
	cases := []struct {
		point string // where it fails
		step  string // the step that shows it
		calls []string
	}{
		{agentops.StepPrepareServer, agentops.StepPrepareServer, []string{"InstallAgent 1", "UndoInstall 1"}},
		{agentops.StepUploadAgent, agentops.StepUploadAgent, []string{"InstallAgent 1", "UndoInstall 1"}},
		{agentops.StepCheckAgent, agentops.StepCheckAgent, []string{"InstallAgent 1", "UndoInstall 1"}},
		{"Connect", StepConnect, []string{"InstallAgent 1", "Connect 1", "UndoInstall 1"}},
		{agentops.StepDownloadXray, agentops.StepDownloadXray, []string{"InstallAgent 1", "Connect 1", "Provision 1", "UndoInstall 1"}},
		{agentops.StepChooseTarget, agentops.StepChooseTarget, []string{"InstallAgent 1", "Connect 1", "Provision 1", "UndoInstall 1"}},
		{agentops.StepApplyConfig, agentops.StepApplyConfig, []string{"InstallAgent 1", "Connect 1", "Provision 1", "UndoInstall 1"}},
	}
	for _, c := range cases {
		t.Run(c.point, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(c.point, &agentops.UnreachableError{What: "agent", Host: "203.0.113.9", Port: 9090})
				const sentence = "The agent doesn't answer on TCP port 9090. The server's firewall (or the provider's) may block it."

				req := installReq("Frankfurt", "203.0.113.9")
				req.SSHPort, req.SSHUser = 2222, "admin"
				j := r.failed(r.add(req))
				if want := sentence + " The server was cleaned up and nothing was kept."; j.Error != want {
					t.Errorf("the job says %q, want %q", j.Error, want)
				}
				if j.Code != api.CodeNodeUnreachable {
					t.Errorf("code %q", j.Code)
				}
				// the steps up to the failure are done, the failed one says why, the ones it
				// never reached wait, and the cleanup comes last
				failedAt := slices.IndexFunc(j.Steps, func(s jobs.Step) bool { return s.Name == c.step })
				if failedAt < 0 {
					t.Fatalf("no step %q in %v", c.step, stepNames(j))
				}
				for i, s := range j.Steps {
					switch {
					case i == failedAt:
						if s.State != jobs.StepFailed || s.Detail != sentence {
							t.Errorf("the failed step is %+v", s)
						}
					case s.Name == agentops.StepCleanServer || s.Name == StepDiscardNode:
						if s.State != jobs.StepOK || i < len(j.Steps)-2 {
							t.Errorf("cleanup step %+v at %d of %d", s, i, len(j.Steps))
						}
					case i < failedAt && s.State != jobs.StepOK:
						t.Errorf("step %+v before the failure isn't done", s)
					case i > failedAt && s.State != jobs.StepPending:
						t.Errorf("step %+v after the failure isn't left pending", s)
					}
				}
				if names := stepNames(j); !slices.Equal(names[len(names)-2:], []string{agentops.StepCleanServer, StepDiscardNode}) {
					t.Errorf("steps %v", names)
				}
				for _, s := range j.Steps[len(j.Steps)-2:] {
					if s.Key == "" {
						t.Errorf("step %q has no key", s.Name)
					}
				}
				if got := r.ops.allCalls(); !slices.Equal(got, c.calls) {
					t.Errorf("calls %v, want %v", got, c.calls)
				}
				// with the login the install was given, held to the key that was pinned
				u := r.ops.undos[0]
				if u.login.Password != sshPassword || u.node.SSHHostKey != fingerprintA || u.node.SSHPort != 2222 || u.node.SSHUser != "admin" || u.node.Host != "203.0.113.9" {
					t.Errorf("cleaned up with %+v as %+v", u.login, u.node)
				}
				// and nothing is kept
				if len(r.nodes()) != 0 {
					t.Fatalf("a node was kept: %+v", r.nodes())
				}
				r.gone(1)
				// only the install's own link was ever made, and it was closed
				linked := r.ops.called("Connect") - boolInt(c.point == "Connect")
				if made, open := r.net.opened(1); open != 0 || made != linked {
					t.Errorf("links made %d, still open %d", made, open)
				}
				if r.dl.releases() != 1 {
					t.Errorf("downloads let go of %d times", r.dl.releases())
				}
				// the server can be tried again
				r.ops.failAt(c.point, nil)
				r.ok(r.add(installReq("Frankfurt", "203.0.113.9")))
			})
		})
	}
}

// When the cleanup fails too, the record is kept, as an unfinished install and
// not as a node: it says what failed, it doesn't count towards the cap, nothing
// but a cleanup or forgetting it is done with it, and its server can't be
// started on again until one of those has happened.
func TestACleanupThatFailsKeepsAnUnfinishedInstall(t *testing.T) {
	for _, how := range []string{"cleaned up", "forgotten"} {
		t.Run(how, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(agentops.StepUploadAgent, errors.New("The connection broke."))
				r.ops.failAt(agentops.StepCleanServer, &agentops.UnreachableError{What: "ssh", Host: "203.0.113.9", Port: 22})

				j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
				if !strings.HasPrefix(j.Error, "The connection broke. ") || !strings.Contains(j.Error, "kept as unfinished") || strings.Contains(j.Error, "nothing was kept") {
					t.Errorf("the job says %q", j.Error)
				}
				if s := stepStates(j); s[agentops.StepCleanServer] != jobs.StepFailed || s[StepDiscardNode] != "" {
					t.Errorf("steps %+v", j.Steps)
				}
				nodes := r.nodes()
				if len(nodes) != 1 || !nodes[0].Unfinished() {
					t.Fatalf("nodes %+v", nodes)
				}
				f := nodes[0].Failure
				if f.Step != agentops.StepUploadAgent || f.StepKey != "upload_agent" || f.Error != "The connection broke." ||
					!strings.Contains(f.Cleanup, "Can't reach SSH") || !f.At.Equal(epoch) {
					t.Errorf("the failure on record: %+v", f)
				}
				st := r.cfg.Store.Read()
				if st.NodeCount() != 0 || st.UnfinishedCount() != 1 {
					t.Errorf("counted %d nodes, %d unfinished", st.NodeCount(), st.UnfinishedCount())
				}
				// it doesn't count: the router still takes its full number of nodes beside it
				r.ops.failAt(agentops.StepUploadAgent, nil)
				for i := 1; i <= model.MaxNodes; i++ {
					r.ok(r.add(installReq(fmt.Sprintf("n%d", i), fmt.Sprintf("203.0.113.%d", 20+i))))
				}
				if _, err := r.m.AddNode(context.Background(), installReq("one more", "203.0.113.40")); !errors.Is(err, model.ErrNodeLimit) {
					t.Errorf("past the cap: %v", err)
				}
				// nothing but taking it out is done with it
				var e *api.Error
				if _, err := r.m.settled(1); !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "didn't finish") {
					t.Errorf("an operation on it: %v", err)
				}
				if _, err := r.m.subscribable(r.cfg.Store.Read(), 1); !errors.Is(err, model.ErrNotFound) {
					t.Errorf("switching a subscription on there: %v", err)
				}
				if live := r.m.NodeLive(1); live.Installing != nil || live.Tunnel.State != "off" {
					t.Errorf("live = %+v", live)
				}
				if _, ok := r.m.Client(1); ok || slices.Contains(r.bridge.tags(), tagOf(1)) {
					t.Error("an unfinished install is watched or carried")
				}
				// and its server waits for it
				removeNode(t, r.cfg.Store, 4) // room again
				_, err := r.m.AddNode(context.Background(), installReq("Frankfurt 2", "203.0.113.9"))
				if !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "didn't finish") {
					t.Errorf("a second install on the same server: %v", err)
				}

				// Clean up is a removal with a login, Forget a forced one
				r.ops.failAt(agentops.StepCleanServer, nil)
				req := api.RemoveRequest{Force: true}
				if how == "cleaned up" {
					req = withLogin()
				}
				r.ok(r.remove(1, req))
				r.gone(1)
				if want := map[string]int{"cleaned up": 1, "forgotten": 0}[how]; r.ops.called("Uninstall") != want {
					t.Errorf("calls %v", r.ops.allCalls())
				}
				r.ok(r.add(installReq("Frankfurt 2", "203.0.113.9")))
			})
		})
	}
}

// The job of an install carries what the person asked for, so that the page
// can offer the same install again after a reload. Nothing of the login's
// secrets is in it.
func TestAnInstallJobCarriesItsFormForAnotherTry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		r.ops.failAt(agentops.StepCheckServer, errors.New("The login was refused."))
		req := api.InstallRequest{
			Login: api.Login{SSHPort: 2222, SSHUser: "admin", Password: sshPassword, PrivateKey: sshKey, Passphrase: sshPassphrase, HostKey: fingerprintA},
			Name:  " Frankfurt ", Host: "203.0.113.9", EntryPort: 8443,
		}
		j := r.failed(r.add(req))
		want := jobs.InstallForm{Name: "Frankfurt", Host: "203.0.113.9", SSHPort: 2222, SSHUser: "admin", EntryPort: 8443}
		if j.Install == nil || *j.Install != want {
			t.Fatalf("the job carries %+v", j.Install)
		}
		b, err := json.Marshal(j)
		must(t, err)
		for _, secret := range []string{sshPassword, sshPassphrase, sshKeyLine, fingerprintA} {
			if strings.Contains(string(b), secret) {
				t.Errorf("the job's JSON has %q: %s", secret, b)
			}
		}
		// the router's own entry port, for a request that left it out
		req.EntryPort, req.Host = 0, "203.0.113.10"
		j = r.failed(r.add(req))
		if j.Install == nil || j.Install.EntryPort != model.DefaultSettings().EntryPort {
			t.Errorf("with no entry port: %+v", j.Install)
		}
	})
}

// A server that already runs an Xray or an agent of its own is refused at
// "Check the server", with a code of its own the page words, and nothing is
// done to it or kept.
func TestARefusedServerSaysWhyInACodeOfItsOwn(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("%w: This server already runs an Xray that Remote Nodes did not install (/usr/local/bin/xray). Use a server without one.", agentops.ErrServerHasXray), api.CodeServerHasXray},
		{fmt.Errorf("%w: This server already runs a nasnet-agent that Remote Nodes did not install.", agentops.ErrServerHasAgent), api.CodeServerHasAgent},
		{fmt.Errorf("%w: This server is already the node \"Frankfurt\" here, under another address.", agentops.ErrServerIsNode), api.CodeServerIsNode},
	} {
		t.Run(c.code, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(agentops.StepCheckServer, c.err)
				j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
				if j.Code != c.code || !strings.Contains(j.Error, "nothing was kept") {
					t.Errorf("job %+v", j)
				}
				if r.ops.called("UndoInstall") != 0 || len(r.nodes()) != 0 {
					t.Errorf("calls %v, nodes %+v", r.ops.allCalls(), r.nodes())
				}
				// and an answer with that error carries the same code
				var e *api.Error
				if !errors.As(opFailure(c.err), &e) || e.Code != c.code {
					t.Errorf("opFailure: %v", opFailure(c.err))
				}
			})
		})
	}
}

// An install the state file holds as running, from a hub that stopped or from
// before installs cleaned up after themselves, has no job: it comes back as an
// unfinished install, which no cap counts and only a cleanup or forgetting
// takes out.
func TestAnInstallingRecordWithNoJobIsAnUnfinishedInstall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		first, err := store.Open(dir)
		must(t, err)
		st := model.NewState()
		for i, h := range []string{"203.0.113.9", "203.0.113.10"} {
			n, err := st.AddNode(model.Node{Name: fmt.Sprintf("old %d", i), Host: h, SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeInstalling, CreatedAt: epoch})
			must(t, err)
			if i == 1 {
				st.FindNode(n.ID).State = model.NodeRegistered
			}
		}
		must(t, first.Update(func(s *model.State) error { *s = st; return nil }))

		reopened, err := store.Open(dir)
		must(t, err)
		r := newOpsRig(t, func(c *Config) { c.Store = reopened })
		r.start()
		defer r.stop()
		got := r.cfg.Store.Read()
		if n := got.FindNode(1); n == nil || !n.Unfinished() || n.Failure.Error == "" {
			t.Fatalf("the old install came back as %+v", n)
		}
		if got.NodeCount() != 1 {
			t.Errorf("%d nodes counted", got.NodeCount())
		}
		var e *api.Error
		if _, err := r.m.AddNode(context.Background(), installReq("again", "203.0.113.9")); !errors.As(err, &e) || !strings.Contains(e.Msg, "didn't finish") {
			t.Errorf("an install on its server: %v", err)
		}
		r.ok(r.remove(1, api.RemoveRequest{Force: true}))
		r.gone(1)
		r.ok(r.add(installReq("again", "203.0.113.9")))
	})
}

// A library may repeat what it was given in the errors it makes. Nothing of
// the login, and no REALITY key, shows in the job or in the log, or is kept in
// the state.
func TestNoSecretShowsWhateverTheLibraryEchoes(t *testing.T) {
	login := api.Login{SSHPort: 22, SSHUser: "root", PrivateKey: sshKey, Passphrase: sshPassphrase, Password: sshPassword, HostKey: fingerprintA}
	t.Run("the login", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.leakLogin = true
			r.ops.failAt(agentops.StepUploadAgent, errors.New("ssh: could not sign in"))
			// the cleanup fails too, so the record keeps both texts
			r.ops.failAt(agentops.StepCleanServer, errors.New("ssh: could not sign in again"))
			id := r.add(api.InstallRequest{Login: login, Name: "Frankfurt", Host: "203.0.113.9"})
			j := r.failed(id)
			shown := fmt.Sprintf("%+v", j)
			for _, secret := range []string{sshPassword, sshPassphrase, sshKeyLine} {
				if strings.Contains(shown, secret) || strings.Contains(j.Error, secret) {
					t.Errorf("the job shows %q: %+v", secret, j)
				}
				if strings.Contains(r.log.String(), secret) {
					t.Errorf("the log has %q:\n%s", secret, r.log)
				}
			}
			if !strings.Contains(j.Error, "could not sign in") || !strings.Contains(j.Error, "…") {
				t.Errorf("the job's error is %q: it should say what happened, with the secrets taken out", j.Error)
			}
			state, err := model.Encode(r.cfg.Store.Read())
			must(t, err)
			for _, secret := range []string{sshPassword, sshPassphrase, sshKeyLine} {
				if strings.Contains(string(state), secret) {
					t.Errorf("the state file keeps %q", secret)
				}
			}
			if f := r.nodes()[0].Failure; !strings.Contains(f.Error, "could not sign in") || !strings.Contains(f.Cleanup, "could not sign in again") {
				t.Errorf("the failure on record: %+v", f)
			}
		})
	})
	t.Run("the REALITY key", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.leakKeys = true
			r.ops.failAt(agentops.StepApplyConfig, errors.New("xray: config refused"))
			id := r.add(installReq("Frankfurt", "203.0.113.9"))
			j := r.failed(id)
			key := r.ops.provisions[0].Keys.PrivateKey
			if key == "" {
				t.Fatal("no key was made")
			}
			if strings.Contains(fmt.Sprintf("%+v", j), key) || strings.Contains(r.log.String(), key) {
				t.Errorf("the REALITY key shows:\n%+v\n%s", j, r.log)
			}
			if !strings.Contains(j.Error, "…") {
				t.Errorf("error %q", j.Error)
			}
			// a node that isn't set up keeps no key: it is saved with the rest, when all has worked
			state, err := model.Encode(r.cfg.Store.Read())
			must(t, err)
			if strings.Contains(string(state), key) {
				t.Error("the state keeps the failed install's key")
			}
		})
	})
}

// What is wrong with a request is said before anything is saved or tried.
func TestARequestThatCantBeAServerOrANodeIsRefusedWhole(t *testing.T) {
	r := newOpsRig(t)
	for name, mod := range map[string]func(*api.InstallRequest){
		"no name":                     func(q *api.InstallRequest) { q.Name = "" },
		"a name with a line break":    func(q *api.InstallRequest) { q.Name = "a\nb" },
		"a name that is too long":     func(q *api.InstallRequest) { q.Name = strings.Repeat("x", model.MaxNameRunes+1) },
		"no host":                     func(q *api.InstallRequest) { q.Host = "" },
		"a host that is a URL":        func(q *api.InstallRequest) { q.Host = "https://203.0.113.9" },
		"an entry port too big":       func(q *api.InstallRequest) { q.EntryPort = 70000 },
		"a negative entry port":       func(q *api.InstallRequest) { q.EntryPort = -1 },
		"the entry on the SSH port":   func(q *api.InstallRequest) { q.EntryPort, q.SSHPort = 2222, 2222 },
		"the entry on the agent's":    func(q *api.InstallRequest) { q.EntryPort = 9090 },
		"the SSH port on the agent's": func(q *api.InstallRequest) { q.SSHPort = 9090 },
		"no password or key":          func(q *api.InstallRequest) { q.Password = "" },
		"no host key agreed to":       func(q *api.InstallRequest) { q.HostKey = "" },
		"a bad SSH port":              func(q *api.InstallRequest) { q.SSHPort = 99999 },
	} {
		req := installReq("Frankfurt", "203.0.113.9")
		mod(&req)
		_, err := r.m.AddNode(context.Background(), req)
		if !isInvalid(err) {
			t.Errorf("%s: err = %v, want an invalid value", name, err)
		}
		if strings.Contains(fmt.Sprint(err), sshPassword) {
			t.Errorf("%s: the message quotes the password: %v", name, err)
		}
	}
	if len(r.nodes()) != 0 || len(r.ops.allCalls()) != 0 {
		t.Errorf("a refused request left %d nodes and %v calls", len(r.nodes()), r.ops.allCalls())
	}
}

func TestANodeNeedsRoomAndAServerAndANameOfItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		first := addNode(t, r.cfg.Store, func(n *model.Node) { n.Name, n.Host = "Frankfurt", "203.0.113.1" })
		dead := addNode(t, r.cfg.Store, func(n *model.Node) { n.Name, n.Host, n.State = "Dead", "203.0.113.2", model.NodeInstalling })
		before := len(r.nodes())
		var e *api.Error

		// the name, compared as a person would
		for _, name := range []string{"Frankfurt", "frankfurt", "FRANKFURT", "dead"} {
			if _, err := r.m.AddNode(context.Background(), installReq(name, "203.0.113.50")); !errors.Is(err, model.ErrNameTaken) {
				t.Errorf("the name %q: %v", name, err)
			}
		}
		// the server
		if _, err := r.m.AddNode(context.Background(), installReq("New", first.Host)); !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "already a node") {
			t.Errorf("a server that is a node: %v", err)
		}
		if _, err := r.m.AddNode(context.Background(), installReq("New", dead.Host)); !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "didn't finish") {
			t.Errorf("a server whose install died: %v", err)
		}
		// an unfinished install is no node, and its name is free for the next try
		gone := addNode(t, r.cfg.Store, func(n *model.Node) {
			n.Name, n.Host, n.State = "Gone", "203.0.113.3", model.NodeInstalling
			n.Failure = model.InstallFailure{Error: "The connection broke."}
		})
		if _, err := r.m.AddNode(context.Background(), installReq("gone", gone.Host)); !errors.As(err, &e) || !strings.Contains(e.Msg, "didn't finish") {
			t.Errorf("an unfinished install's server: %v", err)
		}
		removeNode(t, r.cfg.Store, gone.ID)
		before = len(r.nodes())
		// one whose install is still going is busy, which the page words as a job running
		hold := make(chan struct{})
		_, err := r.m.startJob(jobs.Spec{Kind: jobs.KindInstall, NodeID: dead.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := r.m.AddNode(context.Background(), installReq("New", dead.Host)); !errors.Is(err, jobs.ErrBusy) {
			t.Errorf("a server with an install running: %v", err)
		}
		close(hold)
		synctest.Wait()
		if len(r.nodes()) != before || len(r.ops.allCalls()) != 0 {
			t.Fatalf("a refused request left %d nodes (was %d) and %v calls", len(r.nodes()), before, r.ops.allCalls())
		}

		// the third fits, the fourth doesn't, whatever else is wrong with it
		id := r.add(installReq("Third", "203.0.113.3"))
		r.ok(id)
		for _, req := range []api.InstallRequest{installReq("Fourth", "203.0.113.4"), installReq("Frankfurt", first.Host)} {
			if _, err := r.m.AddNode(context.Background(), req); !errors.Is(err, model.ErrNodeLimit) {
				t.Errorf("a fourth node (%s): %v", req.Name, err)
			}
		}
		if len(r.nodes()) != model.MaxNodes {
			t.Errorf("%d nodes", len(r.nodes()))
		}
	})
}

// Two adds at once at the limit take the last place once.
func TestTwoAddsAtOnceTakeTheLastPlaceOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		ids := make([]string, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ids[i], errs[i] = r.m.AddNode(context.Background(), installReq(fmt.Sprintf("New %d", i), fmt.Sprintf("203.0.113.%d", 50+i)))
			}()
		}
		wg.Wait()
		ok := 0
		for i, err := range errs {
			switch {
			case err == nil:
				ok++
				r.ok(ids[i])
			case !errors.Is(err, model.ErrNodeLimit):
				t.Errorf("add %d: %v", i, err)
			}
		}
		if ok != 1 || len(r.nodes()) != model.MaxNodes {
			t.Errorf("%d adds worked and there are %d nodes", ok, len(r.nodes()))
		}
	})
}

// What isn't said is the router's default: the entry port from the settings,
// the SSH port 22 and root, and the agent's port from the settings.
func TestWhatIsLeftOutIsTheRoutersDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		must(t, r.cfg.Store.Update(func(s *model.State) error {
			s.Settings = model.Settings{AgentPort: 9191, EntryPort: 8444}
			return nil
		}))
		id := r.add(api.InstallRequest{Login: api.Login{Password: sshPassword, HostKey: fingerprintA}, Name: " Frankfurt ", Host: " Node.Example.COM "})
		r.ok(id)
		n := r.nodes()[0]
		if n.Name != "Frankfurt" || n.Host != "node.example.com" || n.SSHPort != 22 || n.SSHUser != "root" || n.AgentPort != 9191 || n.EntryPort != 8444 {
			t.Errorf("node = %+v", n)
		}
		if p := r.ops.provisions[0].Node; p.EntryPort != 8444 {
			t.Errorf("provision was given entry port %d", p.EntryPort)
		}
	})
}

// A hub whose router can't carry the entry yet still sets nodes up: they are
// managed, and the tunnel waits.
func TestANodeIsInstalledWhileTheEntryIsOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, func(c *Config) { c.Facts = nil })
		r.start()
		defer r.stop()
		if r.m.Entry().On {
			t.Fatal("the entry is on")
		}
		r.ok(r.add(installReq("Frankfurt", "203.0.113.9")))
		if tun := r.m.NodeLive(1).Tunnel; tun.State != "off" || tun.Reason != "entry_off" {
			t.Errorf("tunnel = %+v", tun)
		}
		if len(r.bridge.tags()) != 0 || r.nodes()[0].State != model.NodeRegistered {
			t.Errorf("bridge %v, node %+v", r.bridge.tags(), r.nodes()[0])
		}
	})
}

// The job is the engine's, not the request's: a page that goes away doesn't
// stop an install.
func TestAnInstallOutlivesTheRequestThatStartedIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		ctx, cancel := context.WithCancel(context.Background())
		id, err := r.m.AddNode(ctx, installReq("Frankfurt", "203.0.113.9"))
		must(t, err)
		cancel()
		r.ok(id)
		if r.nodes()[0].State != model.NodeRegistered {
			t.Error("the node isn't set up")
		}
	})
}

// A job that can't be started takes its node back: nothing would ever finish it.
func TestAnInstallThatCantBeStartedLeavesNoNodeBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		defer r.stop()
		must(t, r.jobs.Close(context.Background()))
		var e *api.Error
		if _, err := r.m.AddNode(context.Background(), installReq("Frankfurt", "203.0.113.9")); !errors.As(err, &e) || !strings.Contains(e.Msg, "shutting down") {
			t.Errorf("err = %v", err)
		}
		if len(r.nodes()) != 0 || len(r.ops.allCalls()) != 0 {
			t.Errorf("left %d nodes and %v calls", len(r.nodes()), r.ops.allCalls())
		}
	})
	// with no engine it is refused before the node is saved
	r := newOpsRig(t, func(c *Config) { c.Jobs = nil })
	var e *api.Error
	if _, err := r.m.AddNode(context.Background(), installReq("Frankfurt", "203.0.113.9")); !errors.As(err, &e) || !strings.Contains(e.Msg, "run jobs") {
		t.Errorf("with no engine: %v", err)
	}
	if len(r.nodes()) != 0 {
		t.Errorf("left %d nodes", len(r.nodes()))
	}
}

// The node is saved set up in one write at the very end. If that write fails
// the install has failed like any other: the server is cleaned up, and the
// node forgotten.
func TestAnInstallWhoseNodeCantBeSavedFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var disk *scriptedStore
		r := newOpsRig(t, func(c *Config) { disk = newScripted(c.Store); c.Store = disk })
		r.start()
		defer r.stop()
		disk.failNext(2, errors.New("the disk is full")) // the first write saves the node, the second sets it up

		id := r.add(installReq("Frankfurt", "203.0.113.9"))
		j := r.failed(id)
		if j.Error != "the disk is full. The server was cleaned up and nothing was kept." || stepStates(j)[StepSaveNode] != jobs.StepFailed {
			t.Errorf("job = %+v", j)
		}
		if len(r.nodes()) != 0 || r.ops.called("UndoInstall") != 1 {
			t.Errorf("nodes %+v, calls %v", r.nodes(), r.ops.allCalls())
		}
		if _, ok := r.m.Client(1); ok || len(r.bridge.tags()) != 0 {
			t.Error("a node that couldn't be saved is carried")
		}
	})
}

// A node taken out while its install was being finished can't be saved.
func TestANodeThatWasTakenOutIsNotSavedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		release := r.ops.holdAt(agentops.StepApplyConfig)
		id := r.add(installReq("Frankfurt", "203.0.113.9"))
		synctest.Wait()
		removeNode(t, r.cfg.Store, 1)
		release()
		j := r.failed(id)
		if !strings.Contains(j.Error, "removed while it was being installed") || len(r.nodes()) != 0 {
			t.Errorf("job error %q, nodes %v", j.Error, r.nodes())
		}
	})
}
