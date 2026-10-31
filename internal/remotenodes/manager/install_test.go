// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

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
		if in := live.Installing; in == nil || in.JobID != jobID || in.Step != 5 || in.Steps != len(installSteps()) || in.Label != agentops.StepUploadAgent {
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
func TestAnInstallThatFailsLeavesTheNodeInstalling(t *testing.T) {
	cases := []struct {
		point string // where it fails
		step  string // the step that shows it
		calls []string
	}{
		{agentops.StepCheckServer, agentops.StepCheckServer, []string{"InstallAgent 1"}},
		{agentops.StepDownloadAgent, agentops.StepDownloadAgent, []string{"InstallAgent 1"}},
		{agentops.StepUploadAgent, agentops.StepUploadAgent, []string{"InstallAgent 1"}},
		{agentops.StepCheckAgent, agentops.StepCheckAgent, []string{"InstallAgent 1"}},
		{"Connect", StepConnect, []string{"InstallAgent 1", "Connect 1"}},
		{agentops.StepDownloadXray, agentops.StepDownloadXray, []string{"InstallAgent 1", "Connect 1", "Provision 1"}},
		{agentops.StepChooseTarget, agentops.StepChooseTarget, []string{"InstallAgent 1", "Connect 1", "Provision 1"}},
		{agentops.StepApplyConfig, agentops.StepApplyConfig, []string{"InstallAgent 1", "Connect 1", "Provision 1"}},
	}
	for _, c := range cases {
		t.Run(c.point, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(c.point, &agentops.UnreachableError{What: "agent", Host: "203.0.113.9", Port: 9090})
				const sentence = "The agent doesn't answer on TCP port 9090. The server's firewall (or the provider's) may block it."

				jobID := r.add(installReq("Frankfurt", "203.0.113.9"))
				j := r.failed(jobID)
				if j.Error != sentence {
					t.Errorf("the job says %q, want the plain sentence %q", j.Error, sentence)
				}
				failedAt := -1
				for i, s := range j.Steps {
					switch {
					case s.Name == c.step:
						failedAt = i
						if s.State != jobs.StepFailed || s.Detail != sentence {
							t.Errorf("the failed step is %+v", s)
						}
					case failedAt < 0 && s.State != jobs.StepOK:
						t.Errorf("step %+v before the failure isn't done", s)
					case failedAt >= 0 && s.State != jobs.StepPending:
						t.Errorf("step %+v after the failure isn't left pending", s)
					}
				}
				if failedAt < 0 {
					t.Fatalf("no step %q in %v", c.step, stepNames(j))
				}
				if got := r.ops.allCalls(); !slices.Equal(got, c.calls) {
					t.Errorf("calls %v, want %v", got, c.calls)
				}

				// the node is still there, as the install began it
				nodes := r.nodes()
				if len(nodes) != 1 {
					t.Fatalf("%d nodes", len(nodes))
				}
				n := nodes[0]
				if n.State != model.NodeInstalling || n.SSHHostKey != fingerprintA || n.BridgeUUID == "" ||
					n.Arch != "" || n.RealityTarget != "" || n.RealityPublicKey != "" || n.RealityPrivateKey != "" || n.AgentVersion != "" || !n.CertNotAfter.IsZero() {
					t.Errorf("the node after a failed install = %+v", n)
				}
				if _, running := r.jobs.Running(n.ID); running {
					t.Error("a job is still running")
				}
				if live := r.m.NodeLive(n.ID); live.Installing != nil || live.Tunnel.State != "off" || live.Tunnel.Reason != "" {
					t.Errorf("live = %+v", live)
				}
				// the only link made is the install's own, which was closed
				linked := r.ops.called("Connect") - boolInt(c.point == "Connect")
				if _, ok := r.m.Client(n.ID); ok || r.net.tries(n.ID) != linked || len(r.bridge.tags()) != 0 {
					t.Error("a node whose install failed is being watched or carried")
				}
				if made, open := r.net.opened(n.ID); open != 0 || made != linked {
					t.Errorf("links made %d, still open %d", made, open)
				}
				// only taking it out works: nothing else is done to a node that never finished
				var e *api.Error
				if _, err := r.m.settled(n.ID); !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "never fully installed") {
					t.Errorf("an operation on it: %v", err)
				}
				// and the server can't be started on again until it is
				_, err := r.m.AddNode(context.Background(), installReq("Frankfurt 2", "203.0.113.9"))
				if !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "never finished") {
					t.Errorf("a second install on the same server: %v", err)
				}
				if r.dl.releases() != 1 {
					t.Errorf("downloads let go of %d times", r.dl.releases())
				}
			})
		})
	}
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
			if got := r.nodes()[0].RealityPrivateKey; got != "" {
				t.Errorf("the failed node keeps a key: %q", got)
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
		if _, err := r.m.AddNode(context.Background(), installReq("New", dead.Host)); !errors.As(err, &e) || e.Code != api.CodeBadRequest || !strings.Contains(e.Msg, "never finished") {
			t.Errorf("a server whose install died: %v", err)
		}
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
// the node is as it was, a node whose install didn't finish, and the job says so.
func TestAnInstallWhoseNodeCantBeSavedFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var disk *scriptedStore
		r := newOpsRig(t, func(c *Config) { disk = newScripted(c.Store); c.Store = disk })
		r.start()
		defer r.stop()
		disk.failNext(2, errors.New("the disk is full")) // the first write saves the node, the second sets it up

		id := r.add(installReq("Frankfurt", "203.0.113.9"))
		j := r.failed(id)
		if j.Error != "the disk is full" || stepStates(j)[StepSaveNode] != jobs.StepFailed {
			t.Errorf("job = %+v", j)
		}
		if n := r.nodes()[0]; n.State != model.NodeInstalling || n.RealityPrivateKey != "" {
			t.Errorf("node = %+v", n)
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
