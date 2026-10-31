// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

func xrayLine(version string) string {
	return "Xray " + version + " (Xray, Penetrates Everything.) 1b1647c (go1.26.1 linux/amd64)"
}

// The page is told an update is due when the latest release carries a newer
// agent than the node runs, and when the node's Xray is anything but the build
// the plugin pins.
func TestTheUpdatesDueAreWorkedOutFromTheReleaseAndThePin(t *testing.T) {
	for _, c := range []struct {
		name        string
		agent       string // what the node runs
		latest      string // what the release carries
		xray        string
		wantAgent   bool
		wantXray    bool
		wantVersion string // the release's agent, as the page shows it
	}{
		{"behind by a patch", "1.3.0", "v1.3.1", pin.Version, true, false, "1.3.1"},
		{"on the release", "1.3.1", "v1.3.1", pin.Version, false, false, "1.3.1"},
		{"ahead of it", "1.4.0", "v1.3.1", pin.Version, false, false, "1.3.1"},
		{"behind by a minor", "1.3.9", "v1.4.0", pin.Version, true, false, "1.4.0"},
		{"ten is more than nine", "1.9.0", "v1.10.0", pin.Version, true, false, "1.10.0"},
		{"a prerelease is behind its release", "1.4.0-rc.1", "v1.4.0", pin.Version, true, false, "1.4.0"},
		{"a build from the source can't be compared", "dev", "v1.4.0", pin.Version, false, false, "1.4.0"},
		{"nor can one from git describe", "1.3.1-5-g1234567", "v1.4.0", pin.Version, false, false, "1.4.0"},
		{"an older Xray", "1.3.0", "v1.3.0", "26.10.18", false, true, "1.3.0"},
		{"a newer Xray is not the pin either", "1.3.0", "v1.3.0", "26.11.1", false, true, "1.3.0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				n := r.addNode(func(n *model.Node) { n.AgentVersion, n.XrayVersion = c.agent, c.xray })
				r.net.agent(n.ID).set(func(a *fakeAgent) { a.agentVersion, a.xrayLine = c.agent, xrayLine(c.xray) })
				r.dl.set("arm64", c.latest, nil)
				r.start()
				defer r.stop()

				if got := r.m.NodeLive(n.ID).Updates; got != (api.Updates{XrayVersion: pin.Version, Xray: c.wantXray}) {
					t.Errorf("before anyone has looked, updates = %+v", got)
				}
				must(t, r.m.CheckUpdates(context.Background(), n.ID))
				want := api.Updates{Agent: c.wantAgent, Xray: c.wantXray, AgentVersion: c.wantVersion, XrayVersion: pin.Version}
				if got := r.m.NodeLive(n.ID).Updates; got != want {
					t.Errorf("updates = %+v, want %+v", got, want)
				}
			})
		})
	}
}

// A node whose Xray the hub has never heard of isn't said to be behind.
func TestAnUnknownXrayIsNotAnUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode(func(n *model.Node) { n.XrayVersion = "" })
		r.net.agent(n.ID).setDown(true)
		r.start()
		defer r.stop()
		if got := r.m.NodeLive(n.ID).Updates; got.Xray || got.XrayVersion != pin.Version {
			t.Errorf("updates = %+v", got)
		}
	})
}

// Nothing is said of a node whose install didn't finish, or that nobody has.
func TestNoUpdateIsDueForANodeThatIsntSetUp(t *testing.T) {
	r := newOpsRig(t)
	n := r.addNode(func(n *model.Node) { n.State, n.XrayVersion = model.NodeInstalling, "26.1.1" })
	if got := r.m.NodeLive(n.ID).Updates; got != (api.Updates{}) {
		t.Errorf("an unfinished node: %+v", got)
	}
	if got := r.m.NodeLive(99).Updates; got != (api.Updates{}) {
		t.Errorf("a node nobody has: %+v", got)
	}
}

// The release is asked about once an hour for each architecture the nodes are
// of, from the first half minute, and the page never makes it wait: what a look
// found is read from memory.
func TestTheReleaseIsAskedOnceAnHourAndNeverByThePage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		arm := r.addNode()
		r.addNode(func(n *model.Node) { n.Arch = "amd64" })
		r.addNode(func(n *model.Node) { n.State, n.Arch = model.NodeInstalling, "riscv64" })
		r.dl.set("arm64", "v1.3.1", nil)
		r.start()
		defer r.stop()

		r.advance(29 * time.Second)
		if r.dl.lookupCount() != 0 {
			t.Fatalf("looked up the release at once: %v", r.dl.asked())
		}
		r.advance(2 * time.Second)
		// one for each architecture of a node that is set up, and none for the install that isn't
		if got := r.dl.asked(); len(got) != 2 || !slices.Contains(got, "arm64") || !slices.Contains(got, "amd64") {
			t.Fatalf("lookups = %v", got)
		}
		for range 100 { // the page asks as often as it likes
			r.m.NodeLive(arm.ID)
		}
		if got := r.m.NodeLive(arm.ID).Updates; !got.Agent || got.AgentVersion != "1.3.1" {
			t.Errorf("updates = %+v", got)
		}
		r.advance(58 * time.Minute)
		if r.dl.lookupCount() != 2 {
			t.Errorf("%d lookups before the hour is up", r.dl.lookupCount())
		}
		r.advance(2 * time.Minute) // 1 h 0 m 31 s after the first
		if r.dl.lookupCount() != 4 {
			t.Errorf("%d lookups after the hour", r.dl.lookupCount())
		}
		r.advance(time.Hour)
		if r.dl.lookupCount() != 6 {
			t.Errorf("%d lookups after another hour", r.dl.lookupCount())
		}
	})
}

// What the page reads comes from memory, and a look that is in flight, as one
// at a slow release server is, holds nobody up.
func TestAPageThatAsksForUpdatesDoesNotWaitForTheReleaseServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode()
		r.dl.block = make(chan struct{})
		r.dl.set("arm64", "v1.3.1", nil)
		r.start()
		defer r.stop()
		r.advance(31 * time.Second) // the look begins and waits

		done := make(chan api.Updates, 1)
		go func() { done <- r.m.NodeLive(n.ID).Updates }()
		synctest.Wait()
		select {
		case got := <-done:
			if got.Agent || got.AgentVersion != "" || got.XrayVersion != pin.Version {
				t.Errorf("updates while the look waits = %+v", got)
			}
		default:
			t.Fatal("NodeLive waited for the release server")
		}
		if r.dl.lookupCount() != 1 {
			t.Fatalf("lookups = %d", r.dl.lookupCount())
		}

		// the loops go on too: the node is still polled while the look waits
		polls := r.net.agent(n.ID).pollCount()
		r.advance(12 * time.Second)
		if r.net.agent(n.ID).pollCount() <= polls {
			t.Error("the node wasn't polled while the release was being asked about")
		}

		close(r.dl.block)
		synctest.Wait()
		if got := r.m.NodeLive(n.ID).Updates; !got.Agent || got.AgentVersion != "1.3.1" {
			t.Errorf("updates after the look = %+v", got)
		}
	})
}

// A person who asks again gets a new look, but not two in a moment; and
// that look's answer is what the page shows from then on.
func TestAskingForUpdatesAgainLooksAgainButNotTwiceInAMoment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		n := r.addNode()
		r.start()
		defer r.stop()
		ctx := context.Background()

		must(t, r.m.CheckUpdates(ctx, n.ID))
		if r.dl.lookupCount() != 1 || r.m.NodeLive(n.ID).Updates.Agent {
			t.Fatalf("the first look: %d lookups, updates %+v", r.dl.lookupCount(), r.m.NodeLive(n.ID).Updates)
		}
		// a release comes out, and the button is pressed within the moment: the same answer
		r.dl.set("arm64", "v1.3.1", nil)
		r.advance(10 * time.Second)
		must(t, r.m.CheckUpdates(ctx, n.ID))
		must(t, r.m.CheckUpdates(ctx, n.ID))
		if r.dl.lookupCount() != 1 || r.m.NodeLive(n.ID).Updates.Agent {
			t.Errorf("pressed twice in a moment: %d lookups, updates %+v", r.dl.lookupCount(), r.m.NodeLive(n.ID).Updates)
		}
		// and a little later it looks again, though the hour is far off
		r.advance(25 * time.Second)
		must(t, r.m.CheckUpdates(ctx, n.ID))
		if got := r.m.NodeLive(n.ID).Updates; r.dl.lookupCount() != 2 || !got.Agent || got.AgentVersion != "1.3.1" {
			t.Errorf("pressed after the moment: %d lookups, updates %+v", r.dl.lookupCount(), got)
		}
		// the background look then leaves it alone until its hour is up
		r.advance(55 * time.Minute)
		if r.dl.lookupCount() != 2 {
			t.Errorf("%d lookups: the background look ignored the one a person asked for", r.dl.lookupCount())
		}
	})
}

// A look that fails shows nothing new, is tried again after five minutes, is
// said once in the log, and doesn't take back what an earlier one found.
func TestALookThatFailsIsTriedAgainSoonAndKeepsWhatWasFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		n := r.addNode()
		r.dl.set("arm64", "v1.3.1", nil)
		r.start()
		defer r.stop()
		r.advance(31 * time.Second)
		if !r.m.NodeLive(n.ID).Updates.Agent {
			t.Fatal("the first look found nothing")
		}

		r.dl.set("arm64", "", errors.New("artifacts: GET /release.json: 502 Bad Gateway"))
		r.advance(time.Hour) // the next look fails
		if r.dl.lookupCount() != 2 {
			t.Fatalf("%d lookups", r.dl.lookupCount())
		}
		if got := r.m.NodeLive(n.ID).Updates; !got.Agent || got.AgentVersion != "1.3.1" {
			t.Errorf("a failed look took back what was found: %+v", got)
		}
		r.advance(5 * time.Minute) // fails again
		r.advance(5 * time.Minute) // and again
		if r.dl.lookupCount() != 4 {
			t.Errorf("%d lookups after two retries", r.dl.lookupCount())
		}
		if n := r.log.count("couldn't look up the latest release"); n != 1 {
			t.Errorf("the failure was logged %d times:\n%s", n, r.log)
		}
		// a person who asks is told, and what was found stays
		r.advance(time.Minute)
		var e *api.Error
		if err := r.m.CheckUpdates(context.Background(), n.ID); !errors.As(err, &e) || e.Code != api.CodeInternal ||
			!strings.Contains(e.Msg, "couldn't read the latest release") || !strings.Contains(e.Msg, "502 Bad Gateway") || strings.Contains(e.Msg, "artifacts:") {
			t.Errorf("CheckUpdates: %v", err)
		}
		if got := r.m.NodeLive(n.ID).Updates; !got.Agent {
			t.Errorf("updates = %+v", got)
		}
		// it comes back
		r.dl.set("arm64", "v1.3.2", nil)
		r.advance(10 * time.Minute) // the person's look held off the background one for a while
		if got := r.m.NodeLive(n.ID).Updates; got.AgentVersion != "1.3.2" || !got.Agent {
			t.Errorf("updates after the release came back = %+v", got)
		}
		before := r.dl.lookupCount()
		r.advance(50 * time.Minute) // now an hour is the wait again
		if r.dl.lookupCount() != before {
			t.Errorf("%d lookups where the hour wasn't up", r.dl.lookupCount()-before)
		}
	})
}

// A release the plugin can't push isn't an error, and offers nothing: it is not
// asked about again for an hour, and it is not said to be a failure.
func TestAReleaseThePluginCantPushOffersNothing(t *testing.T) {
	for _, bad := range []error{artifacts.ErrAgentTooOld, artifacts.ErrNoGzAgents} {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t, lazyLoops)
			n := r.addNode()
			r.dl.set("arm64", "", bad)
			r.start()
			defer r.stop()
			must(t, r.m.CheckUpdates(context.Background(), n.ID))
			if r.dl.lookupCount() != 1 {
				t.Fatalf("%v: %d lookups", bad, r.dl.lookupCount())
			}
			// not at the half minute, which the person's look was too recent for, nor five minutes on
			r.advance(40 * time.Minute)
			if got := r.m.NodeLive(n.ID).Updates; got.Agent || got.AgentVersion != "" {
				t.Errorf("%v: updates = %+v", bad, got)
			}
			if r.dl.lookupCount() != 1 {
				t.Errorf("%v: %d lookups in 40 minutes", bad, r.dl.lookupCount())
			}
			if r.log.count("couldn't look up") != 0 {
				t.Errorf("%v was logged as a failure:\n%s", bad, r.log)
			}
			r.advance(30 * time.Minute) // past the hour
			if r.dl.lookupCount() != 2 {
				t.Errorf("%v: %d lookups after the hour", bad, r.dl.lookupCount())
			}
		})
	}
}

// Without a place to look, no agent is offered, and nothing is asked for.
func TestWithNoReleaseSourceOnlyXrayIsOffered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops, func(c *Config) { c.Downloads = nil })
		n := r.addNode(func(n *model.Node) { n.XrayVersion = "26.10.18" })
		r.net.agent(n.ID).set(func(a *fakeAgent) { a.xrayLine = xrayLine("26.10.18") })
		r.start()
		defer r.stop()
		must(t, r.m.CheckUpdates(context.Background(), n.ID))
		r.advance(2 * time.Hour)
		if got := r.m.NodeLive(n.ID).Updates; got.Agent || got.AgentVersion != "" || !got.Xray {
			t.Errorf("updates = %+v", got)
		}
	})
}

func TestCheckingForUpdatesNeedsANodeThatIsSetUp(t *testing.T) {
	r := newOpsRig(t)
	n := r.addNode(func(n *model.Node) { n.State = model.NodeInstalling })
	var e *api.Error
	if err := r.m.CheckUpdates(context.Background(), n.ID); !errors.As(err, &e) || e.Code != api.CodeBadRequest {
		t.Errorf("an unfinished node: %v", err)
	}
	if err := r.m.CheckUpdates(context.Background(), 99); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("a node nobody has: %v", err)
	}
	if r.dl.lookupCount() != 0 {
		t.Error("the release was asked about")
	}
	// it needs no answer from the node: one that nothing is listening for is checked all the same
	down := r.addNode()
	must(t, r.m.CheckUpdates(context.Background(), down.ID))
	if r.dl.lookupCount() != 1 {
		t.Errorf("%d lookups", r.dl.lookupCount())
	}
}

func TestACallerThatGivesUpWhileWaitingForALookIsLetGo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		n := r.addNode()
		r.dl.block = make(chan struct{})
		first := make(chan error, 1)
		go func() { first <- r.m.CheckUpdates(context.Background(), n.ID) }()
		synctest.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		second := make(chan error, 1)
		go func() { second <- r.m.CheckUpdates(ctx, n.ID) }()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case err := <-second:
			if err != nil {
				t.Errorf("a caller that gave up got %v, want nothing said to a page that has gone", err)
			}
		default:
			t.Fatal("a caller that gave up is still waiting")
		}
		if r.dl.lookupCount() != 1 {
			t.Errorf("%d lookups", r.dl.lookupCount())
		}
		close(r.dl.block)
		if err := <-first; err != nil {
			t.Errorf("the look in flight: %v", err)
		}
	})
}

// updatable is a router with a node that is set up and answering, and both of
// its versions behind.
func updatable(t *testing.T, mods ...func(*model.Node)) (*opsRig, model.Node) {
	t.Helper()
	r := newOpsRig(t)
	n := r.addNode(mods...)
	return r, n
}

func TestUpdatingTheAgentRunsTheStepsAndTheNodeAndTheStateAgree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, n := updatable(t)
		r.dl.set("arm64", "v1.3.1", nil)
		r.start()
		defer r.stop()
		must(t, r.m.CheckUpdates(context.Background(), n.ID))
		if !r.m.NodeLive(n.ID).Updates.Agent {
			t.Fatal("no update is due")
		}

		id, err := r.m.UpdateNode(context.Background(), n.ID, "agent")
		must(t, err)
		j := r.ok(id)
		if j.Kind != jobs.KindUpdateAgent || j.NodeID != n.ID || j.Target != n.Name || !slices.Equal(stepNames(j), agentops.UpdateAgentPlan()) {
			t.Errorf("job = %+v", j)
		}
		if len(r.ops.updates) != 1 || r.ops.updates[0].kind != "agent" || r.ops.updates[0].node.ID != n.ID || r.ops.updates[0].client.(*fakeConn).a != r.net.agent(n.ID) {
			t.Errorf("updates = %+v", r.ops.updates)
		}
		if got := nodeOf(r.cfg.Store, n.ID).AgentVersion; got != "1.3.1" {
			t.Errorf("the state has agent %q", got)
		}
		// one event, whichever heard of it first: the job, or the loop that asks the agent its version
		r.advance(3 * time.Minute)
		ev := r.m.NodeEvents(n.ID)
		if len(ev) != 1 || ev[0].Kind != EventAgentUpdated || ev[0].Detail != "1.3.0 to 1.3.1" {
			t.Errorf("events = %+v", ev)
		}
		// no new look is needed to know the update is done
		if got := r.m.NodeLive(n.ID).Updates; got.Agent || got.AgentVersion != "1.3.1" {
			t.Errorf("updates after = %+v", got)
		}
		if r.dl.releases() != 1 {
			t.Errorf("downloads let go of %d times", r.dl.releases())
		}
	})
}

// An update restarts the agent, or Xray, on purpose. While the job runs the
// hub sees the node's Xray start time move, and that is not a crash.
func TestUpdatesTheHubRunsAreNotCountedAsXrayCrashes(t *testing.T) {
	for _, kind := range []string{"agent", "xray"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r, n := updatable(t)
				r.dl.set("arm64", "v1.3.1", nil)
				r.start()
				defer r.stop()
				must(t, r.m.CheckUpdates(context.Background(), n.ID))

				at := agentops.StepRestartAgent
				if kind == "xray" {
					at = agentops.StepUploadXray
				}
				release := r.ops.holdAt(at)
				id, err := r.m.UpdateNode(context.Background(), n.ID, kind)
				must(t, err)
				for pid := int64(101); pid <= 103; pid++ {
					r.net.agent(n.ID).set(func(a *fakeAgent) { a.pid, a.started = pid, a.started+60 })
					r.advance(5 * time.Second)
				}
				release()
				r.ok(id)
				r.advance(5 * time.Second)
				if p, _ := r.m.peek(n.ID); p.restarts != 0 || p.agent.Xray.CrashLoop {
					t.Errorf("an update of the %s left the node looking like it crash-loops: %+v", kind, p)
				}
			})
		})
	}
}

// The bridge keeps a node out while its Xray is another build, and has it
// the moment it is the pin: that is what the Sync after an Xray update is for.
func TestUpdatingXrayBringsTheNodeIntoTheBridge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, n := updatable(t, func(n *model.Node) { n.XrayVersion = "26.10.18" })
		r.net.agent(n.ID).set(func(a *fakeAgent) { a.xrayLine = xrayLine("26.10.18") })
		r.start()
		defer r.stop()
		if tags := r.bridge.tags(); len(tags) != 0 {
			t.Fatalf("the bridge carries %v with the wrong Xray", tags)
		}
		if tun := r.m.NodeLive(n.ID).Tunnel; tun.State != "off" || tun.Reason != "xray_mismatch" {
			t.Fatalf("tunnel = %+v", tun)
		}

		id, err := r.m.UpdateNode(context.Background(), n.ID, "xray")
		must(t, err)
		j := r.ok(id)
		if j.Kind != jobs.KindUpdateXray || !slices.Equal(stepNames(j), agentops.UpdateXrayPlan()) {
			t.Errorf("job = %+v", j)
		}
		if got := nodeOf(r.cfg.Store, n.ID).XrayVersion; got != pin.Version {
			t.Errorf("the state has Xray %q", got)
		}
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Errorf("the bridge carries %v after the update", got)
		}
		if tun := r.m.NodeLive(n.ID).Tunnel; tun.State != "down" || tun.Reason != "" {
			t.Errorf("tunnel = %+v: the bridge holds the node and its page doesn't say the tunnel is up yet", tun)
		}
		r.advance(3 * time.Minute)
		var changed []api.NodeEvent
		for _, e := range r.m.NodeEvents(n.ID) {
			if e.Kind == EventXrayChanged {
				changed = append(changed, e)
			}
		}
		if len(changed) != 1 || changed[0].Detail != "26.10.18 to "+pin.Version {
			t.Errorf("xray_changed events = %+v", changed)
		}
		if got := r.m.NodeLive(n.ID).Updates; got.Xray {
			t.Errorf("updates = %+v", got)
		}
	})
}

// An update is for one node at a time: the second waits for the first, and says so.
func TestOneUpdateRunsAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		a := r.addNode()
		b := r.addNode()
		r.start()
		defer r.stop()
		release := r.ops.holdAt(agentops.StepDownloadAgent)

		first, err := r.m.UpdateNode(context.Background(), a.ID, "agent")
		must(t, err)
		synctest.Wait()
		second, err := r.m.UpdateNode(context.Background(), b.ID, "agent")
		must(t, err)
		synctest.Wait()
		if got := r.ops.allCalls(); !slices.Equal(got, []string{"UpdateAgent 1"}) {
			t.Fatalf("calls %v: the second node was updated at the same time", got)
		}
		j, _ := r.jobs.Get(second)
		if st := j.Steps[0]; st.State != jobs.StepRunning || !strings.Contains(st.Detail, "Waiting for another update") {
			t.Errorf("the waiting job says %+v", j.Steps)
		}
		// an Xray update waits behind an agent update just the same, and other jobs don't wait for either
		xray, err := r.m.UpdateNode(context.Background(), b.ID, "xray")
		if !errors.Is(err, jobs.ErrBusy) {
			t.Fatalf("a second job on a node that has one: %v (%s)", err, xray)
		}
		renew, err := r.m.RenewCert(context.Background(), addNodeLater(t, r).ID)
		must(t, err)
		r.ok(renew)

		release()
		r.ok(first)
		r.ok(second)
		var updates []string
		for _, c := range r.ops.allCalls() {
			if strings.HasPrefix(c, "Update") {
				updates = append(updates, c)
			}
		}
		if !slices.Equal(updates, []string{"UpdateAgent 1", "UpdateAgent 2"}) {
			t.Errorf("updates ran as %v", updates)
		}
		// the turn is given back, also by an update that fails
		r.ops.failAt(agentops.StepUploadAgent, errors.New("The connection broke."))
		failing, err := r.m.UpdateNode(context.Background(), a.ID, "agent")
		must(t, err)
		r.failed(failing)
		r.ops.failAt(agentops.StepUploadAgent, nil)
		next, err := r.m.UpdateNode(context.Background(), b.ID, "xray")
		must(t, err)
		r.ok(next)
	})
}

// addNodeLater adds a node that is up, once the manager is running, and gives
// it a moment to be connected.
func addNodeLater(t *testing.T, r *opsRig) model.Node {
	t.Helper()
	n := r.addNode()
	r.m.Sync()
	synctest.Wait()
	return n
}

func TestAnUpdateThatFailsChangesNothing(t *testing.T) {
	for _, kind := range []string{"agent", "xray"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r, n := updatable(t, func(n *model.Node) { n.AgentVersion, n.XrayVersion = "1.3.0", "26.10.18" })
				r.net.agent(n.ID).set(func(a *fakeAgent) { a.xrayLine = xrayLine("26.10.18") })
				r.dl.set("arm64", "v1.3.1", nil)
				r.start()
				defer r.stop()
				must(t, r.m.CheckUpdates(context.Background(), n.ID))
				before := nodeOf(r.cfg.Store, n.ID)
				events := len(r.m.NodeEvents(n.ID))

				point := agentops.StepUploadAgent
				if kind == "xray" {
					point = agentops.StepUploadXray
				}
				r.ops.failAt(point, newUpdateFailure())
				id, err := r.m.UpdateNode(context.Background(), n.ID, kind)
				must(t, err)
				j := r.failed(id)
				if !strings.Contains(j.Error, "TCP port 9090") || stepStates(j)[point] != jobs.StepFailed {
					t.Errorf("job = %q %v", j.Error, j.Steps)
				}
				r.advance(3 * time.Minute)
				if got := nodeOf(r.cfg.Store, n.ID); got != before {
					t.Errorf("the node changed: %+v", got)
				}
				if len(r.m.NodeEvents(n.ID)) != events {
					t.Errorf("events = %v", kinds(r.m.NodeEvents(n.ID)))
				}
				// still due, since nothing was done
				if got := r.m.NodeLive(n.ID).Updates; !got.Agent || !got.Xray {
					t.Errorf("updates = %+v", got)
				}
				if r.dl.releases() != 1 {
					t.Errorf("downloads let go of %d times", r.dl.releases())
				}
			})
		})
	}
}

func newUpdateFailure() error {
	return &agentops.UnreachableError{What: "agent", Host: "203.0.113.1", Port: 9090}
}

func TestAnUpdateNeedsANodeThatIsReadyAndFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, n := updatable(t)
		down := r.addNode()
		r.net.agent(down.ID).setDown(true)
		unfinished := r.addNode(func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()
		ctx := context.Background()
		var e *api.Error

		for _, kind := range []string{"", "firmware", "Agent", "agent xray"} {
			if _, err := r.m.UpdateNode(ctx, n.ID, kind); !isInvalid(err) {
				t.Errorf("an update of %q: %v", kind, err)
			}
		}
		if _, err := r.m.UpdateNode(ctx, 99, "agent"); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		if _, err := r.m.UpdateNode(ctx, unfinished.ID, "xray"); !errors.As(err, &e) || e.Code != api.CodeBadRequest {
			t.Errorf("a node that never finished: %v", err)
		}
		if _, err := r.m.UpdateNode(ctx, down.ID, "agent"); !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable {
			t.Errorf("a node that doesn't answer: %v", err)
		}
		hold := make(chan struct{})
		_, err := r.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: n.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := r.m.UpdateNode(ctx, n.ID, "agent"); !errors.Is(err, jobs.ErrBusy) {
			t.Errorf("a node with a job: %v", err)
		}
		close(hold)
		synctest.Wait()
		if r.ops.called("Update") != 0 {
			t.Errorf("calls %v", r.ops.allCalls())
		}
	})
}
