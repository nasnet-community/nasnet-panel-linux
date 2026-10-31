// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func withLogin() api.RemoveRequest {
	return api.RemoveRequest{Login: &api.Login{SSHPort: 2222, SSHUser: "admin", Password: sshPassword, HostKey: fingerprintB}}
}

func (r *opsRig) remove(id uint, req api.RemoveRequest) string {
	r.t.Helper()
	job, err := r.m.RemoveNode(context.Background(), id, req)
	must(r.t, err)
	return job
}

// gone says that a node is as forgotten as the manager can make it.
func (r *opsRig) gone(id uint) {
	r.t.Helper()
	synctest.Wait()
	if _, err := r.m.nodeFor(id); !errors.Is(err, model.ErrNotFound) {
		r.t.Errorf("node %d is still in the state: %v", id, err)
	}
	st := r.cfg.Store.Read()
	for _, e := range st.Enablements {
		if e.NodeID == id {
			r.t.Errorf("node %d still holds a client: %+v", id, e)
		}
	}
	if _, ok := r.m.peek(id); ok {
		r.t.Errorf("the manager still keeps a record of node %d", id)
	}
	if _, ok := r.m.Client(id); ok {
		r.t.Errorf("the manager still holds a link to node %d", id)
	}
	if made, open := r.net.opened(id); open != 0 {
		r.t.Errorf("node %d has %d of %d links still open", id, open, made)
	}
	if u := r.cfg.Usage.Node(id); u.Live || u.Online != 0 || u.TodayDown != 0 {
		r.t.Errorf("the numbers of node %d are still counted: %+v", id, u)
	}
	if slices.Contains(r.bridge.tags(), tagOf(id)) {
		r.t.Errorf("the bridge still carries node %d: %v", id, r.bridge.tags())
	}
}

// Nothing is taken off a server without a login, and the hub keeps none, so the
// answer is needs_login, for a node that answers as for one that doesn't.
func TestWithNoLoginAndNoForceTheAnswerIsNeedsLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		up := r.addNode()
		down := r.addNode()
		r.net.agent(down.ID).setDown(true)
		dead := r.addNode(func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()

		for name, id := range map[string]uint{"a node that answers": up.ID, "one that doesn't": down.ID, "one whose install died": dead.ID} {
			_, err := r.m.RemoveNode(context.Background(), id, api.RemoveRequest{})
			var e *api.Error
			if !errors.As(err, &e) || e.Code != api.CodeNeedsLogin || !strings.Contains(e.Msg, "SSH login") {
				t.Errorf("%s: %v", name, err)
			}
		}
		if len(r.jobs.List()) != 0 || len(r.nodes()) != 3 || r.ops.called("") != 0 {
			t.Errorf("a removal that wasn't asked for properly did something: %d jobs, %d nodes, calls %v", len(r.jobs.List()), len(r.nodes()), r.ops.allCalls())
		}
	})
}

// With a login the agent comes off the server, and then the node is forgotten
// with everything the manager had of it; the other nodes don't notice.
func TestRemovingANodeWithALoginTakesTheAgentOffAndForgetsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		gone := r.addNode()
		stays := r.addNode()
		ali := addSubscription(t, r.cfg.Store, "ali")
		enable(t, r.cfg.Store, ali.ID, gone.ID, true)
		enable(t, r.cfg.Store, ali.ID, stays.ID, true)
		r.start()
		defer r.stop()
		r.advance(12 * time.Second)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Fatalf("the bridge carries %v", got)
		}
		_, made := r.net.opened(gone.ID)
		if made != 1 || r.cfg.Usage.Node(gone.ID).Live == false {
			t.Fatalf("the node wasn't being watched: %d links open", made)
		}

		id := r.remove(gone.ID, withLogin())
		j := r.ok(id)
		if j.Kind != jobs.KindRemove || j.NodeID != gone.ID || j.Target != gone.Name {
			t.Errorf("job = %+v", j)
		}
		if want := append(agentops.UninstallPlan(), StepForgetNode); !slices.Equal(stepNames(j), want) {
			t.Errorf("steps %v, want %v", stepNames(j), want)
		}
		for _, s := range j.Steps {
			if s.State != jobs.StepOK {
				t.Errorf("step %+v", s)
			}
		}

		// what Uninstall was given: the node's saved address and the login the person gave, with the key they agreed to
		if len(r.ops.uninstalls) != 1 {
			t.Fatalf("calls %v", r.ops.allCalls())
		}
		u := r.ops.uninstalls[0]
		if u.node.ID != gone.ID || u.node.Host != gone.Host || u.node.AgentPort != gone.AgentPort || u.node.SSHPort != 2222 || u.node.SSHUser != "admin" || u.node.SSHHostKey != fingerprintB {
			t.Errorf("Uninstall was given the node %+v", u.node)
		}
		if u.login != (agentops.Login{Password: sshPassword}) {
			t.Errorf("Uninstall was given the login %+v", u.login)
		}

		// forgotten: the state, its clients, the link, the numbers, and the bridge's portal
		r.gone(gone.ID)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-2"}) {
			t.Errorf("the bridge carries %v", got)
		}
		if _, err := r.m.nodeFor(stays.ID); err != nil {
			t.Errorf("the other node: %v", err)
		}
		st := r.cfg.Store.Read()
		if e := st.FindEnablement(ali.ID, stays.ID); e == nil || !e.Enabled {
			t.Errorf("the other node's client: %+v", e)
		}
		if _, ok := r.m.Client(stays.ID); !ok {
			t.Error("the other node lost its link")
		}
		// a node that is gone is gone for the page too
		if _, err := r.m.settled(gone.ID); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("settled: %v", err)
		}
		if r.dl.releases() != 0 {
			t.Errorf("a removal let go of downloads %d times", r.dl.releases())
		}
	})
}

// A login that leaves the port and the user blank is for the node as it was
// set up.
func TestALoginThatLeavesThePortAndUserBlankIsForTheNodeAsItWasSetUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode(func(n *model.Node) { n.SSHPort, n.SSHUser = 2200, "deploy" })
		r.start()
		defer r.stop()
		r.ok(r.remove(n.ID, api.RemoveRequest{Login: &api.Login{Password: sshPassword, HostKey: fingerprintA}}))
		if u := r.ops.uninstalls[0].node; u.SSHPort != 2200 || u.SSHUser != "deploy" {
			t.Errorf("signed in to port %d as %q", u.SSHPort, u.SSHUser)
		}
	})
}

// With force the server is left as it is: nothing is asked of agentops, not
// even for a node that answers, and not when the hub can't make operations.
func TestForgettingANodeWithForceTouchesNoServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		up := r.addNode()
		down := r.addNode()
		r.net.agent(down.ID).setDown(true)
		dead := r.addNode(func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()

		for _, id := range []uint{up.ID, down.ID, dead.ID} {
			j := r.ok(r.remove(id, api.RemoveRequest{Force: true}))
			if !slices.Equal(stepNames(j), []string{StepForgetNode}) || j.Target == "" || j.NodeID != id {
				t.Errorf("job = %+v", j)
			}
			r.gone(id)
		}
		if got := r.ops.allCalls(); len(got) != 0 {
			t.Errorf("a server was touched: %v", got)
		}
	})
	// and a hub with no operations can still let go of a node
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, func(c *Config) { c.NewOps = nil })
		n := r.addNode()
		r.start()
		defer r.stop()
		r.ok(r.remove(n.ID, api.RemoveRequest{Force: true}))
		r.gone(n.ID)
		var e *api.Error
		other := r.addNode()
		if _, err := r.m.RemoveNode(context.Background(), other.ID, withLogin()); !errors.As(err, &e) || e.Code != api.CodeInternal {
			t.Errorf("a login with no operations to use it: %v", err)
		}
	})
}

// A removal that fails leaves the node where it was, watched and carried, and
// still removable, by force if the server can't be signed in to.
func TestARemovalThatFailsLeavesTheNodeAndForceStillWorks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode()
		ali := addSubscription(t, r.cfg.Store, "ali")
		enable(t, r.cfg.Store, ali.ID, n.ID, true)
		r.start()
		defer r.stop()
		r.ops.failAt(agentops.StepStopAgent, agentops.ErrLoginFailed)

		j := r.failed(r.remove(n.ID, withLogin()))
		if j.Error != agentops.ErrLoginFailed.Error() || stepStates(j)[agentops.StepCheckServer] != jobs.StepOK ||
			stepStates(j)[agentops.StepStopAgent] != jobs.StepFailed || stepStates(j)[StepForgetNode] != jobs.StepPending {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if got := nodeOf(r.cfg.Store, n.ID); got.Name != n.Name {
			t.Errorf("the node changed: %+v", got)
		}
		st := r.cfg.Store.Read()
		if e := st.FindEnablement(ali.ID, n.ID); e == nil || !e.Enabled {
			t.Errorf("the node's client: %+v", e)
		}
		if _, ok := r.m.Client(n.ID); !ok || !slices.Equal(r.bridge.tags(), []string{"node-1"}) {
			t.Errorf("the node isn't watched or carried: bridge %v", r.bridge.tags())
		}
		if _, running := r.jobs.Running(n.ID); running {
			t.Error("the job is still running")
		}

		// and a server that isn't the hub's to touch says so in words
		r.ops.failAt(agentops.StepCheckServer, agentops.ErrOtherHub)
		j = r.failed(r.remove(n.ID, withLogin()))
		if !strings.Contains(j.Error, "another hub") {
			t.Errorf("error %q", j.Error)
		}

		// forgetting works all the same
		r.ok(r.remove(n.ID, api.RemoveRequest{Force: true}))
		r.gone(n.ID)
	})
}

// A node that can't be taken out of the state fails at the last step, and stays.
func TestANodeThatCantBeForgottenStays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		s.disk.failNext(1, errors.New("the disk is full"))
		j := s.failed(s.remove(s.node.ID, api.RemoveRequest{Force: true}))
		if j.Error != "the disk is full" || stepStates(j)[StepForgetNode] != jobs.StepFailed {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if _, err := s.m.nodeFor(s.node.ID); err != nil {
			t.Errorf("the node: %v", err)
		}
		if _, ok := s.m.Client(s.node.ID); !ok {
			t.Error("the node lost its link")
		}
	})
}

func TestARemovalNeedsAClearRequestAndAFreeNode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode()
		r.start()
		defer r.stop()
		ctx := context.Background()

		if _, err := r.m.RemoveNode(ctx, 99, api.RemoveRequest{Force: true}); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		both := withLogin()
		both.Force = true
		if _, err := r.m.RemoveNode(ctx, n.ID, both); !isInvalid(err) {
			t.Errorf("force and a login: %v", err)
		}
		for name, mod := range map[string]func(*api.Login){
			"no host key agreed to": func(l *api.Login) { l.HostKey = "" },
			"no password or key":    func(l *api.Login) { l.Password = "" },
			"a bad port":            func(l *api.Login) { l.SSHPort = 99999 },
			"a bad user":            func(l *api.Login) { l.SSHUser = "a b" },
		} {
			req := withLogin()
			mod(req.Login)
			if _, err := r.m.RemoveNode(ctx, n.ID, req); !isInvalid(err) {
				t.Errorf("%s: %v", name, err)
			}
		}
		// a job on the node comes first, even before the question of a login
		hold := make(chan struct{})
		_, err := r.m.startJob(jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: n.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		for name, req := range map[string]api.RemoveRequest{"no login": {}, "force": {Force: true}, "a login": withLogin()} {
			if _, err := r.m.RemoveNode(ctx, n.ID, req); !errors.Is(err, jobs.ErrBusy) {
				t.Errorf("%s on a node with a job: %v", name, err)
			}
		}
		close(hold)
		synctest.Wait()
		if len(r.nodes()) != 1 || r.ops.called("Uninstall") != 0 {
			t.Errorf("%d nodes, calls %v", len(r.nodes()), r.ops.allCalls())
		}
	})
}

// A node can't be taken out from under an install that is running, but an
// unfinished install can, with a login or without, and its server can be
// started on again.
func TestAFailedInstallIsRemovedLikeAnyNode(t *testing.T) {
	for _, how := range []string{"a login", "force"} {
		t.Run(how, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(agentops.StepUploadAgent, errors.New("The upload broke."))
				r.ops.failAt(agentops.StepCleanServer, errors.New("The server went away."))
				release := r.ops.holdAt(agentops.StepUploadAgent)
				install := r.add(installReq("Frankfurt", "203.0.113.9"))
				synctest.Wait()
				if _, err := r.m.RemoveNode(context.Background(), 1, api.RemoveRequest{Force: true}); !errors.Is(err, jobs.ErrBusy) {
					t.Errorf("a node whose install is running: %v", err)
				}
				release()
				r.failed(install)

				req := api.RemoveRequest{Force: true}
				if how == "a login" {
					req = withLogin()
				}
				j := r.ok(r.remove(1, req))
				if j.Target != "Frankfurt" || j.NodeID != 1 {
					t.Errorf("job = %+v", j)
				}
				r.gone(1)
				if want := map[string]int{"a login": 1, "force": 0}[how]; r.ops.called("Uninstall") != want {
					t.Errorf("calls %v", r.ops.allCalls())
				}
				// the server is free to be started on again
				r.ops.failAt(agentops.StepUploadAgent, nil)
				r.ok(r.add(installReq("Frankfurt", "203.0.113.9")))
				if n := r.nodes(); len(n) != 1 || n[0].ID != 2 || n[0].State != model.NodeRegistered {
					t.Errorf("nodes = %+v: the id of a removed node must not come back", n)
				}
			})
		})
	}
}

// The login never shows, whatever the library says on the way: not in the job
// and not in the log.
func TestTheLoginOfARemovalShowsNowhere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode()
		r.start()
		defer r.stop()
		r.ops.failAt(agentops.StepCheckServer, errors.New("ssh: could not sign in"))
		r.ops.leakLogin = true
		req := withLogin()
		req.Login.PrivateKey, req.Login.Passphrase = sshKey, sshPassphrase
		j := r.failed(r.remove(n.ID, req))
		for _, secret := range []string{sshPassword, sshPassphrase, sshKeyLine} {
			if strings.Contains(fmt.Sprintf("%+v", j), secret) || strings.Contains(r.log.String(), secret) {
				t.Errorf("%q shows:\n%+v\n%s", secret, j, r.log)
			}
		}
		if !strings.Contains(j.Error, "…") {
			t.Errorf("error %q", j.Error)
		}
	})
}
