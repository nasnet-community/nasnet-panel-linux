// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

func TestANodeIsConnectedAndPolledEveryFiveSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()

		if got := r.net.times(n.ID); !slices.Equal(got, secs(0)) {
			t.Fatalf("connected at %v, want once, at the start", got)
		}
		if ag.pollCount() != 1 || ag.count("GetVersion") != 1 || ag.count("ListUsers") != 1 {
			t.Fatalf("calls at the start: %v", ag.callsFrom(0))
		}

		r.advance(12 * time.Second) // polls at 5 and 10
		if ag.pollCount() != 3 {
			t.Errorf("%d polls after 12 s, want 3", ag.pollCount())
		}
		// the version and the clients were looked at once, on connecting
		if ag.count("GetVersion") != 1 || ag.count("ListUsers") != 1 {
			t.Errorf("calls: %v", ag.callsFrom(0))
		}
		p, ok := r.m.peek(n.ID)
		if !ok || !p.connected || p.unreachable || p.misses != 0 || !p.seen.Equal(epoch.Add(10*time.Second)) {
			t.Fatalf("node state = %+v", p)
		}
		a := p.agent
		if a == nil || a.Version != "1.3.0" || a.XrayVersion != pin.Version || !a.Xray.Running || a.Xray.UptimeSec != 600 ||
			a.Xray.Restarts != 0 || a.Xray.CrashLoop || !a.ReportedAt.Equal(epoch.Add(10*time.Second)) {
			t.Errorf("agent state = %+v", a)
		}
		// what the agent has no way to say stays at zero
		if a.UptimeSec != 0 || a.RSSBytes != 0 || a.Xray.LastExit != "" || a.SettingsApplied || a.LastPushError != "" {
			t.Errorf("agent state has what no agent reports: %+v", a)
		}
		if u := r.cfg.Usage.Node(n.ID); !u.Live || !u.UpdatedAt.Equal(epoch.Add(10*time.Second)) {
			t.Errorf("usage = %+v", u)
		}
		if got := r.m.NodeEvents(n.ID); len(got) != 0 {
			t.Errorf("events = %v", got)
		}
		c, ok := r.m.Client(n.ID)
		if _, isConn := c.(*fakeConn); !ok || !isConn {
			t.Errorf("Client = %v, %v: want the connection itself, not the manager's tap", c, ok)
		}
		if made, open := r.net.opened(n.ID); made != 1 || open != 1 {
			t.Errorf("connections made %d, open %d", made, open)
		}
		if _, ok := r.m.Client(99); ok {
			t.Error("a client for a node nobody has")
		}
	})
}

func TestAnUnreachableNodeIsTriedAgainAfterOneTwoFourSecondsAndSoOnToHalfAMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		ag.setDown(true)
		r.start()
		defer r.stop()

		r.advance(95 * time.Second)
		want := secs(0, 1, 3, 7, 15, 31, 61, 91)
		if got := r.net.times(n.ID); !slices.Equal(got, want) {
			t.Fatalf("tries at %v, want %v", got, want)
		}
		// the third miss is what makes it unreachable, and it is said once
		ev := r.m.NodeEvents(n.ID)
		if got := kinds(ev); !slices.Equal(got, []string{EventNodeUnreachable}) {
			t.Fatalf("events = %v", got)
		}
		if !ev[0].At.Equal(epoch.Add(3*time.Second)) || ev[0].Detail != "connection refused" {
			t.Errorf("the event = %+v", ev[0])
		}
		p, _ := r.m.peek(n.ID)
		if !p.unreachable || p.connected || p.misses != 8 {
			t.Errorf("node state = %+v", p)
		}
		if r.log.count("node is not answering") != 1 {
			t.Errorf("the log says it %d times:\n%s", r.log.count("node is not answering"), r.log)
		}
	})
}

func TestAGoodPollMakesTheWaitStartOverAndTheNodeIsBackOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		ag.setDown(true)
		r.start()
		defer r.stop()

		r.advance(80 * time.Second) // tries at 0, 1, 3, 7, 15, 31, 61: the next is due at 91
		ag.setDown(false)
		r.advance(15 * time.Second) // connects at 91
		if got := r.net.times(n.ID); got[len(got)-1] != 91*time.Second {
			t.Fatalf("tries at %v", got)
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventNodeUnreachable, EventNodeBack}) {
			t.Fatalf("events = %v", got)
		}
		if p, _ := r.m.peek(n.ID); p.unreachable || p.misses != 0 || !p.connected {
			t.Fatalf("node state = %+v", p)
		}

		// it goes away again, at 95: the polls at 96, 101 and 106 get no answer
		// and the connection is given up for a new one, which is tried at once,
		// then after a second, two and four, not after the half minute the node
		// had got to
		ag.setDown(true)
		r.advance(25 * time.Second) // now at 120
		got := r.net.times(n.ID)
		if after := got[slices.Index(got, 91*time.Second)+1:]; !slices.Equal(after, secs(106, 107, 109, 113)) {
			t.Fatalf("tries after the connection at 91: %v", after)
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventNodeUnreachable, EventNodeBack, EventNodeUnreachable}) {
			t.Fatalf("events = %v", got)
		}
	})
}

// Two polls without an answer are not an outage: the node isn't called
// unreachable and the connection stays. The clients are checked again all the
// same, since a node that was out of reach may have come back from a restart.
func TestAShortBlipIsNotAnOutage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()

		r.advance(7 * time.Second)
		ag.setDown(true)
		r.advance(10 * time.Second) // the polls at 10 and 15 fail (the second just now)
		if p, _ := r.m.peek(n.ID); p.misses != 2 || p.unreachable {
			t.Fatalf("after two misses: %+v", p)
		}
		ag.setDown(false)
		r.advance(10 * time.Second) // the poll at 20 gets through

		if got := r.m.NodeEvents(n.ID); len(got) != 0 {
			t.Errorf("a blip left events: %v", got)
		}
		if made, open := r.net.opened(n.ID); made != 1 || open != 1 {
			t.Errorf("connections made %d, open %d: the connection should have been kept", made, open)
		}
		if ag.count("ListUsers") != 2 {
			t.Errorf("clients looked at %d times, want 2 (the start, and after the blip)", ag.count("ListUsers"))
		}
		if p, _ := r.m.peek(n.ID); p.misses != 0 {
			t.Errorf("misses = %d after an answer", p.misses)
		}
	})
}

func TestClientsAreReconciledAfterEveryConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		st := r.cfg.Store
		n1, n2 := addNode(t, st), addNode(t, st)
		ali, sara, omid := addSubscription(t, st, "ali"), addSubscription(t, st, "sara"), addSubscription(t, st, "omid")
		enable(t, st, ali.ID, n1.ID, true)
		enable(t, st, sara.ID, n1.ID, false) // switched off here
		enable(t, st, omid.ID, n2.ID, true)  // and on the other node only

		ag1, ag2 := r.net.agent(n1.ID), r.net.agent(n2.ID)
		// node 1 holds a client nobody wants, and the bridge's own
		ag1.set(func(a *fakeAgent) {
			a.users["sub-9@remote-nodes"] = "11111111-0000-4000-8000-000009000001"
			a.users["rvs-t1@reverse"] = bridgeIDOf(1)
		})
		r.start()
		defer r.stop()

		wantCalls := []string{"AddUser sub-1@remote-nodes", "RemoveUser sub-9@remote-nodes"}
		for _, w := range wantCalls {
			if ag1.count(w) != 1 {
				t.Errorf("node 1 was not told %q: %v", w, ag1.callsFrom(0))
			}
		}
		if ag1.count("AddUser sub-2") != 0 || ag1.count("AddUser sub-3") != 0 || ag1.count("RemoveUser rvs") != 0 {
			t.Errorf("node 1 was touched where it shouldn't be: %v", ag1.callsFrom(0))
		}
		// the stored client id is the one that was added, and the bridge's stays
		if got := ag1.emails(); !slices.Equal(got, []string{"rvs-t1@reverse", "sub-1@remote-nodes"}) {
			t.Errorf("node 1 holds %v", got)
		}
		if ag1.users["sub-1@remote-nodes"] != clientIDOf(ali.ID, n1.ID) {
			t.Errorf("node 1 holds client id %q", ag1.users["sub-1@remote-nodes"])
		}
		if got := ag2.emails(); !slices.Equal(got, []string{"sub-3@remote-nodes"}) {
			t.Errorf("node 2 holds %v", got)
		}
		// the diff is in the log
		if r.log.count("reconciled a node's clients", "node=1", "added=[sub-1@remote-nodes]", "removed=[sub-9@remote-nodes]", "foreign=[rvs-t1@reverse]") != 1 {
			t.Errorf("the log has no diff for node 1:\n%s", r.log)
		}

		// node 1 goes away for good measure, and comes back with the clients it
		// has: the new connection is reconciled and finds nothing to do
		ag1.setDown(true)
		r.advance(30 * time.Second)
		ag1.setDown(false)
		before := ag1.callCount()
		r.advance(60 * time.Second)
		after := ag1.callsFrom(before)
		if ag1.count("ListUsers") != 2 {
			t.Errorf("clients looked at %d times, want 2", ag1.count("ListUsers"))
		}
		for _, c := range after {
			if strings.HasPrefix(c, "AddUser") || strings.HasPrefix(c, "RemoveUser") {
				t.Errorf("a node that was right was changed: %v", after)
			}
		}

		// then Xray comes back from a restart without the client it was given
		ag1.set(func(a *fakeAgent) { delete(a.users, "sub-1@remote-nodes") })
		r.m.Reconnect(n1.ID)
		synctest.Wait()
		if got := ag1.emails(); !slices.Equal(got, []string{"rvs-t1@reverse", "sub-1@remote-nodes"}) {
			t.Errorf("node 1 holds %v after a reconnection", got)
		}
	})
}

func TestAFailedReconcileIsLoggedAndTheLoopCarriesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		sub := addSubscription(t, r.cfg.Store, "ali")
		enable(t, r.cfg.Store, sub.ID, n.ID, true)
		ag := r.net.agent(n.ID)
		ag.set(func(a *fakeAgent) { a.listErr = errors.New("xray is not up yet") })
		r.start()
		defer r.stop()

		if r.log.count("could not all be reconciled", "xray is not up yet") != 1 {
			t.Fatalf("the failure is not in the log:\n%s", r.log)
		}
		if ag.count("AddUser") != 0 {
			t.Error("a client was added although the list failed")
		}
		// the node is polled all the same, and nothing is retried until a new connection
		r.advance(30 * time.Second)
		if ag.pollCount() != 7 || ag.count("ListUsers") != 1 {
			t.Errorf("%d polls and %d lists", ag.pollCount(), ag.count("ListUsers"))
		}
		if p, _ := r.m.peek(n.ID); !p.connected || p.misses != 0 {
			t.Errorf("node state = %+v", p)
		}
		r.m.Reconnect(n.ID)
		synctest.Wait()
		if got := ag.emails(); !slices.Equal(got, []string{"sub-1@remote-nodes"}) {
			t.Errorf("after the next connection the node holds %v", got)
		}
	})
}

// The node's versions go into the state when the agent says something else,
// both in one write, and not again while it keeps saying it.
func TestAVersionChangeIsWrittenOnceAndSaidInTheEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store, func(n *model.Node) { n.AgentVersion, n.XrayVersion = "1.2.0", "26.10.20" })
		ag := r.net.agent(n.ID)
		ag.set(func(a *fakeAgent) { a.agentVersion = "v1.3.0" })
		writes := r.cfg.Store.Writes()
		r.start()
		defer r.stop()

		got := nodeOf(r.cfg.Store, n.ID)
		if got.AgentVersion != "1.3.0" || got.XrayVersion != pin.Version {
			t.Fatalf("the state holds agent %q and xray %q", got.AgentVersion, got.XrayVersion)
		}
		if w := r.cfg.Store.Writes() - writes; w != 1 {
			t.Errorf("%d writes for one change of both versions", w)
		}
		ev := r.m.NodeEvents(n.ID)
		if got := kinds(ev); !slices.Equal(got, []string{EventXrayChanged, EventAgentUpdated}) {
			t.Fatalf("events = %v", got)
		}
		if ev[0].Detail != "1.2.0 to 1.3.0" || ev[1].Detail != "26.10.20 to "+pin.Version {
			t.Errorf("details %q and %q", ev[0].Detail, ev[1].Detail)
		}

		// steady: no write and no event for what the state holds already
		r.advance(3 * time.Minute) // past the version being asked again
		if w := r.cfg.Store.Writes() - writes; w != 1 {
			t.Errorf("%d writes after polling for three minutes", w)
		}
		if len(r.m.NodeEvents(n.ID)) != 2 {
			t.Errorf("events = %v", kinds(r.m.NodeEvents(n.ID)))
		}

		// an Xray upgrade is another write and another event
		ag.set(func(a *fakeAgent) {
			a.xrayLine = "Xray 26.11.1 (Xray, Penetrates Everything.) abc (go1.26.1 linux/arm64)"
		})
		r.advance(6 * time.Second)
		if got := nodeOf(r.cfg.Store, n.ID).XrayVersion; got != "26.11.1" {
			t.Errorf("the state holds xray %q", got)
		}
		if w := r.cfg.Store.Writes() - writes; w != 2 {
			t.Errorf("%d writes after the upgrade", w)
		}
		if got := r.m.NodeEvents(n.ID); got[0].Kind != EventXrayChanged || got[0].Detail != "26.10.21 to 26.11.1" || len(got) != 3 {
			t.Errorf("events = %+v", got)
		}
		if p, _ := r.m.peek(n.ID); p.agent.XrayVersion != "26.11.1" || p.agent.Version != "1.3.0" {
			t.Errorf("agent state = %+v", p.agent)
		}
	})
}

func TestTheFirstVersionsAreWrittenWithoutAnEventAndNoVersionIsMadeUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		// a node with none yet, and an agent that can't run xray
		n := addNode(t, r.cfg.Store, func(n *model.Node) { n.AgentVersion, n.XrayVersion = "", "" })
		r.net.agent(n.ID).set(func(a *fakeAgent) { a.xrayLine = "unknown" })
		r.start()
		defer r.stop()

		got := nodeOf(r.cfg.Store, n.ID)
		if got.AgentVersion != "1.3.0" || got.XrayVersion != "" {
			t.Fatalf("the state holds agent %q and xray %q: \"unknown\" is not a version", got.AgentVersion, got.XrayVersion)
		}
		if ev := r.m.NodeEvents(n.ID); len(ev) != 0 {
			t.Errorf("a first report is not a change: %v", ev)
		}
		if p, _ := r.m.peek(n.ID); p.agent.XrayVersion != "" || p.xrayVer != "" {
			t.Errorf("agent state = %+v", p)
		}
	})
}

func TestAVersionThatCannotBeKeptIsLoggedOnceAndTriedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, log := baseConfig(t)
		real := cfg.Store
		fs := &flakyStore{Store: real}
		cfg.Store = fs
		net := newNet()
		cfg.Connector = net
		n := addNode(t, real, func(n *model.Node) { n.AgentVersion = "1.2.0" })
		m, err := New(cfg) // the CA is made while the disk still works
		must(t, err)
		fs.failing.Store(true)
		r := &rig{t: t, cfg: cfg, log: log, net: net, m: m}
		r.start()
		defer r.stop()

		r.advance(30 * time.Second)
		if got := nodeOf(real, n.ID).AgentVersion; got != "1.2.0" {
			t.Fatalf("the state holds %q", got)
		}
		if log.count("keeping a node's versions failed") != 1 {
			t.Errorf("said %d times:\n%s", log.count("keeping a node's versions failed"), log)
		}
		fs.failing.Store(false)
		r.advance(6 * time.Second)
		if got := nodeOf(real, n.ID).AgentVersion; got != "1.3.0" {
			t.Errorf("the state holds %q after the disk came back", got)
		}
		// it was said once, and no event came twice for the one change
		if got := kinds(m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventAgentUpdated}) {
			t.Errorf("events = %v", got)
		}
	})
}

func TestRemovingANodeStopsItsLoopClosesItsClientAndForgetsItsNumbers(t *testing.T) {
	for name, explicit := range map[string]bool{"noticed by itself": false, "with Sync": true} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRig(t)
				n1, n2 := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
				a1, a2 := r.net.agent(n1.ID), r.net.agent(n2.ID)
				r.start()
				defer r.stop()
				r.advance(7 * time.Second)
				if !r.cfg.Usage.Node(n1.ID).Live {
					t.Fatal("node 1 isn't live before it is removed")
				}
				polls := a1.pollCount()

				removeNode(t, r.cfg.Store, n1.ID)
				if explicit {
					r.m.Sync()
					synctest.Wait()
				} else {
					r.advance(6 * time.Second) // the state is looked at every five
				}
				if _, there := r.m.peek(n1.ID); there {
					t.Error("the node's record is still there")
				}
				if _, ok := r.m.Client(n1.ID); ok {
					t.Error("a client is still held for the node")
				}
				if made, open := r.net.opened(n1.ID); made != 1 || open != 0 {
					t.Errorf("connections made %d, open %d", made, open)
				}
				if got := r.cfg.Usage.Node(n1.ID); got != (usage.NodeUsage{}) {
					t.Errorf("usage still knows the node: %+v", got)
				}
				if a1.pollCount() > polls+1 {
					t.Errorf("node 1 was polled %d more times after it was removed", a1.pollCount()-polls)
				}
				before := a1.pollCount()
				r.advance(30 * time.Second)
				if a1.pollCount() != before {
					t.Error("the removed node is still being polled")
				}
				// and the other is not touched
				if p, _ := r.m.peek(n2.ID); !p.connected || a2.pollCount() < 7 {
					t.Errorf("node 2: %+v after %d polls", p, a2.pollCount())
				}
				if r.log.count("node forgotten", "node=1") != 1 {
					t.Errorf("log:\n%s", r.log)
				}
			})
		})
	}
}

func TestSyncStartsALoopForANewNodeAtOnceAndTheLookDoesWithoutIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.start()
		defer r.stop()
		r.advance(3 * time.Second)
		if len(r.net.attempts) != 0 {
			t.Fatal("something was tried with no node")
		}

		n1 := addNode(t, r.cfg.Store)
		r.m.Sync()
		synctest.Wait()
		if got := r.net.times(n1.ID); !slices.Equal(got, secs(3)) {
			t.Errorf("node 1 tried at %v, want at once, at 3", got)
		}

		// one nobody announces is found by the look, every five seconds
		n2 := addNode(t, r.cfg.Store)
		r.advance(1 * time.Second)
		if r.net.tries(n2.ID) != 0 {
			t.Error("the new node was found before the next look")
		}
		r.advance(2 * time.Second)
		if got := r.net.times(n2.ID); len(got) != 1 || got[0] != 5*time.Second {
			t.Errorf("node 2 tried at %v, want at the look at 5", got)
		}
	})
}

func TestANodeStillBeingInstalledHasNoLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()
		r.advance(20 * time.Second)
		if r.net.tries(n.ID) != 0 {
			t.Fatal("a node whose install hasn't finished was connected to")
		}
		editNode(t, r.cfg.Store, n.ID, func(n *model.Node) { n.State = model.NodeRegistered })
		r.m.Sync()
		synctest.Wait()
		if r.net.tries(n.ID) != 1 {
			t.Errorf("tries = %d once it is registered", r.net.tries(n.ID))
		}
	})
}

func TestANodeThatMovedIsReachedAtItsNewAddress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)

		editNode(t, r.cfg.Store, n.ID, func(n *model.Node) { n.Host, n.AgentPort = "198.51.100.9", 9191 })
		r.m.Sync()
		synctest.Wait()
		if a := r.net.lastAttempt(); a.host != "198.51.100.9" || a.port != 9191 {
			t.Errorf("the last try was %+v", a)
		}
		if made, open := r.net.opened(n.ID); made != 2 || open != 1 {
			t.Errorf("connections made %d, open %d: the old one should be closed", made, open)
		}
	})
}

func TestReconnectDropsTheConnectionAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)

		r.m.Reconnect(n.ID)
		synctest.Wait()
		if got := r.net.times(n.ID); !slices.Equal(got, secs(0, 2)) {
			t.Errorf("tries at %v", got)
		}
		if made, open := r.net.opened(n.ID); made != 2 || open != 1 {
			t.Errorf("connections made %d, open %d", made, open)
		}
		r.m.Reconnect(77) // a node nobody has: nothing happens
	})
}

func TestNoNodeIsContactedWhileTheCAIsUnusableAndAllAreOnceItIsFixed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, func(c *Config) { setCA(t, c.Store, newCA(t, epoch.AddDate(-11, 0, 0))) })
		n := addNode(t, r.cfg.Store)
		if r.m.CA() != nil {
			t.Fatal("an expired CA is in use")
		}
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		if r.net.tries(n.ID) != 0 {
			t.Fatalf("a node was contacted with no usable CA: %v", r.net.times(n.ID))
		}
		if _, there := r.m.peek(n.ID); there {
			t.Error("a loop was started for a node that can't be reached")
		}

		setCA(t, r.cfg.Store, newCA(t, epoch))
		r.advance(6 * time.Second) // the next look
		if r.net.tries(n.ID) != 1 {
			t.Errorf("tries = %v once the CA was fixed", r.net.times(n.ID))
		}
	})
}

// A loop that was running when the CA went bad doesn't make new connections
// either, and picks the node up again when it is fixed.
func TestALoopWaitsOutACAThatGoesBad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := newShiftClock()
		r := newRig(t, func(c *Config) { c.Now = clk.Now })
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)

		// the clock jumps back to before the CA's life: it is not valid yet
		clk.Shift(-366 * 24 * time.Hour)
		ag.setDown(true)
		r.advance(2 * time.Minute)
		if r.m.CAError() == nil {
			t.Fatal("the manager didn't notice the CA is not valid")
		}
		tries := r.net.tries(n.ID)
		r.advance(2 * time.Minute)
		if r.net.tries(n.ID) != tries {
			t.Errorf("connections were tried with no usable CA: %v", r.net.times(n.ID))
		}
		clk.Shift(366 * 24 * time.Hour)
		ag.setDown(false)
		r.advance(30 * time.Second)
		if p, _ := r.m.peek(n.ID); !p.connected {
			t.Errorf("node state = %+v", p)
		}
	})
}

func TestXrayRestartsAreCountedAndACrashLoopIsSeenAndForgotten(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()

		restart := func(pid int64) {
			ag.set(func(a *fakeAgent) { a.pid, a.started, a.uptime = pid, a.started+60, 1 })
			r.advance(5 * time.Second)
		}
		restart(101)
		restart(102)
		p, _ := r.m.peek(n.ID)
		if p.restarts != 2 || p.agent.Xray.CrashLoop {
			t.Fatalf("after two restarts: %+v", p)
		}
		if ag.count("ListUsers") != 3 {
			t.Errorf("clients looked at %d times, want at the start and after each restart", ag.count("ListUsers"))
		}
		restart(103)
		if p, _ := r.m.peek(n.ID); p.restarts != 3 || !p.agent.Xray.CrashLoop || p.agent.Xray.Running != true {
			t.Fatalf("after three restarts in a few minutes: %+v", p)
		}
		// quiet for longer than the window: it is not a crash loop any more,
		// and the count of what was seen stays
		r.advance(11 * time.Minute)
		if p, _ := r.m.peek(n.ID); p.restarts != 3 || p.agent.Xray.CrashLoop {
			t.Errorf("after the window: %+v", p)
		}
		// a stop is not a restart, and an agent that says no pid is not one either
		ag.set(func(a *fakeAgent) { a.running, a.pid, a.started = false, 0, -62135596800 })
		r.advance(10 * time.Second)
		ag.set(func(a *fakeAgent) { a.running, a.pid, a.started = true, 104, 5000 })
		r.advance(10 * time.Second)
		if p, _ := r.m.peek(n.ID); p.restarts != 3 || !p.agent.Xray.Running {
			t.Errorf("a stop and a start from nothing was counted: %+v", p)
		}
	})
}

// The hub restarts Xray itself when it pushes a config and the agent when it
// updates one. Rotating the keys, changing a port and updating the agent within
// ten minutes read as "Xray keeps crashing" on a real node, since all three
// move what the hub watches. They are not a crash loop, though the node's
// clients are looked at again after each, since a restarted Xray has lost the
// ones added to it while it ran.
func TestRestartsTheHubCausesAreNotACrashLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()

		restart := func(pid int64) {
			ag.set(func(a *fakeAgent) { a.pid, a.started, a.uptime = pid, a.started+60, 1 })
			r.advance(5 * time.Second)
		}
		done := r.m.expectRestarts(n.ID)
		restart(101)
		restart(102)
		restart(103)
		done()
		// the poll that sees the last one can come after the operation has ended
		restart(104)
		p, _ := r.m.peek(n.ID)
		if p.restarts != 0 || p.agent.Xray.CrashLoop {
			t.Fatalf("after four restarts the hub caused: %+v", p)
		}
		if got := ag.count("ListUsers"); got != 5 {
			t.Errorf("clients looked at %d times, want at the start and after each of the four restarts", got)
		}

		// once the grace is over a restart nobody asked for counts again, and
		// three of them are a crash loop
		r.advance(restartGrace)
		restart(105)
		if p, _ := r.m.peek(n.ID); p.restarts != 1 || p.agent.Xray.CrashLoop {
			t.Fatalf("after a restart of its own: %+v", p)
		}
		restart(106)
		restart(107)
		if p, _ := r.m.peek(n.ID); p.restarts != 3 || !p.agent.Xray.CrashLoop {
			t.Fatalf("after three restarts of its own: %+v", p)
		}
	})
}

func TestABundleWithNoStatusLeavesTheLastReportAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		first, _ := r.m.peek(n.ID)

		ag.set(func(a *fakeAgent) { a.nostats = true })
		r.advance(10 * time.Second)
		p, _ := r.m.peek(n.ID)
		if p.agent == nil || !p.agent.ReportedAt.Equal(first.agent.ReportedAt) {
			t.Errorf("the report moved without a status: %+v", p.agent)
		}
		if !p.seen.After(first.seen) {
			t.Error("the node is not seen although it answers")
		}
	})
}

func TestTheEventListKeepsTheNewestFiftyAndFlattensWhatItIsGiven(t *testing.T) {
	cfg, _ := baseConfig(t)
	n := addNode(t, cfg.Store)
	m, err := New(cfg)
	must(t, err)

	for i := 0; i < 60; i++ {
		m.Event(n.ID, EventCertRenewed, strings.Repeat("x", i))
	}
	ev := m.NodeEvents(n.ID)
	if len(ev) != 50 || len(ev[0].Detail) != 59 || len(ev[49].Detail) != 10 {
		t.Fatalf("%d events, the newest has %d characters and the oldest %d", len(ev), len(ev[0].Detail), len(ev[len(ev)-1].Detail))
	}
	m.Event(n.ID, EventKeysRotated, "line one\n  line two\t"+strings.Repeat("é", 300))
	got := m.NodeEvents(n.ID)[0]
	if got.Kind != EventKeysRotated || !strings.HasPrefix(got.Detail, "line one line two é") || len([]rune(got.Detail)) != maxDetail || !strings.HasSuffix(got.Detail, "…") {
		t.Errorf("a long, multi-line detail came out as %q", got.Detail)
	}
	if got.At.IsZero() || got.At.Location() != time.UTC {
		t.Errorf("event time %v", got.At)
	}

	// no node, no kind: dropped
	m.Event(99, EventCertRenewed, "nobody's")
	m.Event(n.ID, "", "no kind")
	if got := m.NodeEvents(99); len(got) != 0 {
		t.Errorf("a node nobody has has events: %v", got)
	}
	if len(m.NodeEvents(n.ID)) != 50 || m.NodeEvents(n.ID)[0].Kind != EventKeysRotated {
		t.Error("a dropped event changed the list")
	}
	if m.NodeEvents(99) == nil {
		t.Error("NodeEvents is nil for a node with none")
	}
}

func TestAnEventForANodeThatLeftTheStateGoesWithIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()
		r.m.Event(n.ID, EventKeysRotated, "")
		if len(r.m.NodeEvents(n.ID)) != 1 {
			t.Fatal("an installing node can't have events")
		}
		removeNode(t, r.cfg.Store, n.ID)
		r.advance(6 * time.Second)
		if len(r.m.NodeEvents(n.ID)) != 0 {
			t.Error("the events of a removed node are still there")
		}
	})
}

// flakyStore is the real store with writes that can be made to fail.
type flakyStore struct {
	Store
	failing atomic.Bool
}

func (f *flakyStore) Update(fn func(*model.State) error) error {
	if f.failing.Load() {
		return errors.New("the disk is full")
	}
	return f.Store.Update(fn)
}

// A connection made under the old CA is no use once another has taken its
// place, so every loop makes a new one.
func TestANewCAMakesEveryLoopReconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)

		setCA(t, r.cfg.Store, newCA(t, epoch))
		r.m.Sync()
		synctest.Wait()
		if made, open := r.net.opened(n.ID); made != 2 || open != 1 {
			t.Errorf("connections made %d, open %d after the CA was replaced", made, open)
		}
	})
}
