// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
)

// withAddr is a healthy report that gives the router's address on the
// Domestic link, or none when addr is empty.
func withAddr(at time.Time, addr string) routerfacts.Facts {
	f := healthy(at)
	if addr != "" {
		f.DomesticAddr = netip.MustParseAddr(addr)
	}
	return f
}

const (
	addrA = "192.168.1.20"
	addrB = "192.168.1.33"
	addrC = "192.168.1.44"
)

// restarted is how many times the hub restarted Xray on the agent, and how many
// of those asked for the config to be checked first, which it always should.
func restarted(ag *fakeAgent) (all, checked int) {
	return ag.count("RestartXray"), ag.count("RestartXray validate=true")
}

// xrayRestartEvents are a node's events of the kind the hub records when it
// restarts Xray for the router's address.
func xrayRestartEvents(m *Manager, id uint) []api.NodeEvent {
	var out []api.NodeEvent
	for _, e := range m.NodeEvents(id) {
		if e.Kind == EventXrayRestarted {
			out = append(out, e)
		}
	}
	return out
}

// A hub that has no address yet takes the first one it reads as where things
// stand, whatever it is, so that starting up, or starting up again after the
// router's address changed while the hub was down, never restarts a node.
func TestTheFirstAddressTheHubReadsIsNoChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		r.advance(10 * time.Minute)
		if all, _ := restarted(ag); all != 0 {
			t.Errorf("Xray was restarted %d times for the first address", all)
		}
		if p, _ := r.m.peek(n.ID); p.resetDue {
			t.Error("the node is marked for the first address")
		}
		r.stop()

		// the hub starts again, and the router has another address by now: still no change
		later := newFacts(ok(withAddr(epoch, addrB)))
		r2 := newRig(t, withFacts(later), func(c *Config) { c.Store, c.Connector = r.cfg.Store, r.net })
		r2.start()
		defer r2.stop()
		r2.advance(10 * time.Minute)
		if all, _ := restarted(ag); all != 0 {
			t.Errorf("Xray was restarted %d times by a hub that had no address before", all)
		}
		if len(xrayRestartEvents(r.m, n.ID))+len(xrayRestartEvents(r2.m, n.ID)) != 0 || r2.log.count("Domestic link changed") != 0 {
			t.Errorf("a change was reported:\n%s", r2.log)
		}
	})
}

// The router's address changes: every node's Xray is restarted, once, after its
// config is checked, and the clients that were added to it while it ran are put
// back at once. The restart is the hub's own doing, so it is no crash loop, and
// the node's list says what happened.
func TestAChangeOfTheRoutersAddressRestartsEachNodesXrayOnceAndPutsTheClientsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		st := r.cfg.Store
		n1, n2 := addNode(t, st), addNode(t, st)
		alice := addSubscription(t, st, "alice")
		enable(t, st, alice.ID, n1.ID, true)
		enable(t, st, alice.ID, n2.ID, true)
		email := nodecfg.SubscriptionEmail(int(alice.ID))
		ag1, ag2 := r.net.agent(n1.ID), r.net.agent(n2.ID)
		ag2.set(func(a *fakeAgent) { a.users["rvs-t2@reverse"] = bridgeIDOf(2) }) // the bridge's client is in the node's config
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		for _, ag := range []*fakeAgent{ag1, ag2} {
			if got, _ := restarted(ag); got != 0 || !slices.Contains(ag.emails(), email) {
				t.Fatalf("before the change: %d restarts, clients %v", got, ag.emails())
			}
		}

		facts.set(ok(withAddr(epoch, addrB)))
		before := ag1.callCount()
		r.advance(40 * time.Second)

		for i, ag := range []*fakeAgent{ag1, ag2} {
			if all, checked := restarted(ag); all != 1 || checked != 1 {
				t.Errorf("node %d: %d restarts, %d with the config checked first, want 1 and 1", i+1, all, checked)
			}
			if got := ag.emails(); !slices.Contains(got, email) {
				t.Errorf("node %d holds %v after the restart: alice was not put back", i+1, got)
			}
		}
		if got := ag2.emails(); !slices.Contains(got, "rvs-t2@reverse") {
			t.Errorf("the bridge's client is gone from node 2: %v", got)
		}

		// the clients come back in the same pass, before the next poll, and not a poll later
		calls := ag1.callsFrom(before)
		at := slices.Index(calls, "RestartXray validate=true")
		if at < 0 {
			t.Fatalf("node 1's calls: %v", calls)
		}
		after := calls[at+1:]
		if next := slices.Index(after, "GetStatsBundle"); next >= 0 {
			after = after[:next]
		}
		if !slices.Contains(after, "AddUser "+email) {
			t.Errorf("alice was not added in the pass that restarted Xray: %v", calls[at:])
		}

		for _, n := range []model.Node{n1, n2} {
			ev := xrayRestartEvents(r.m, n.ID)
			if len(ev) != 1 || ev[0].Detail != addrA+" to "+addrB {
				t.Errorf("node %d events %+v", n.ID, r.m.NodeEvents(n.ID))
			}
			// the hub's own restart is not Xray crashing
			if p, _ := r.m.peek(n.ID); p.restarts != 0 || p.agent.Xray.CrashLoop || p.resetDue {
				t.Errorf("node %d after the restart: %+v", n.ID, p)
			}
		}
		if r.log.count("the router's address on the Domestic link changed", "from="+addrA, "to="+addrB, "nodes=2") != 1 ||
			r.log.count("restarted a node's Xray because the router's address on the Domestic link changed", "from="+addrA, "to="+addrB) != 2 {
			t.Errorf("log:\n%s", r.log)
		}

		// and that is all: nothing is restarted again, however long it goes on
		r.advance(15 * time.Minute)
		for i, ag := range []*fakeAgent{ag1, ag2} {
			if all, _ := restarted(ag); all != 1 {
				t.Errorf("node %d was restarted %d times in all", i+1, all)
			}
		}
	})
}

// Only a different address is a change. The same one again, a report that gives
// none (the link is down, or the script is older than the key), a report that
// can't be had at all, and one that gave something that is not an address are
// all unknown, and unknown is no change. The change that counts is from the
// last address the hub had.
func TestTheSameAddressAndAReportWithNoneAreNoChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()

		missing := &fs.PathError{Op: "stat", Path: "/data/router/state", Err: fs.ErrNotExist}
		down := withAddr(epoch, "")
		down.Domestic = false
		for _, step := range []struct {
			what string
			give factsResult
		}{
			{"the same address", ok(withAddr(epoch, addrA))},
			{"no address while the link is up", ok(withAddr(epoch, ""))},
			{"the link down, with none", ok(down)},
			{"no report", failing(missing)},
			{"a report that can't be read", failing(errors.New("input/output error"))},
			{"the same address again, after all that", ok(withAddr(epoch, addrA))},
		} {
			facts.set(step.give)
			r.advance(70 * time.Second)
			if all, _ := restarted(ag); all != 0 {
				t.Fatalf("%s: Xray was restarted", step.what)
			}
			if p, _ := r.m.peek(n.ID); p.resetDue {
				t.Fatalf("%s: the node is marked", step.what)
			}
		}
		if r.log.count("Domestic link changed") != 0 || len(xrayRestartEvents(r.m, n.ID)) != 0 {
			t.Errorf("a change was reported:\n%s", r.log)
		}

		// a different address after a blank is a change from the one before the blank
		facts.set(ok(withAddr(epoch, "")))
		r.advance(70 * time.Second)
		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(40 * time.Second)
		if all, _ := restarted(ag); all != 1 {
			t.Errorf("after a blank and then another address: %d restarts, want 1", all)
		}
		if ev := xrayRestartEvents(r.m, n.ID); len(ev) != 1 || ev[0].Detail != addrA+" to "+addrB {
			t.Errorf("events %+v", ev)
		}
	})
}

// A node the hub can't reach keeps the mark and is restarted once it is back,
// and the nodes that can be reached don't wait for it. A second change while
// it is away is still one restart.
func TestANodeOutOfReachKeepsTheMarkUntilItIsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		st := r.cfg.Store
		near, far := addNode(t, st), addNode(t, st)
		alice := addSubscription(t, st, "alice")
		enable(t, st, alice.ID, far.ID, true)
		agNear, agFar := r.net.agent(near.ID), r.net.agent(far.ID)
		r.start()
		defer r.stop()
		r.advance(30 * time.Second)

		agFar.setDown(true)
		r.advance(time.Minute)
		if p, _ := r.m.peek(far.ID); !p.unreachable {
			t.Fatalf("the far node is not unreachable: %+v", p)
		}
		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(2 * time.Minute)
		if all, _ := restarted(agNear); all != 1 {
			t.Errorf("the node that can be reached was restarted %d times", all)
		}
		if p, _ := r.m.peek(far.ID); !p.resetDue || !p.unreachable {
			t.Errorf("the far node: %+v", p)
		}
		if all, _ := restarted(agFar); all != 0 {
			t.Fatalf("a node that is down was restarted: %v", agFar.callsFrom(0))
		}
		if len(xrayRestartEvents(r.m, far.ID)) != 0 {
			t.Error("an event for a restart that has not happened")
		}

		// the address changes again while it is away
		facts.set(ok(withAddr(epoch, addrC)))
		r.advance(2 * time.Minute)
		if all, _ := restarted(agNear); all != 2 {
			t.Errorf("the node that can be reached was restarted %d times for two changes, want 2", all)
		}

		agFar.setDown(false)
		r.advance(time.Minute)
		if all, _ := restarted(agFar); all != 1 {
			t.Errorf("the far node was restarted %d times when it came back, want 1 for the two changes", all)
		}
		if p, _ := r.m.peek(far.ID); p.resetDue || p.unreachable {
			t.Errorf("the far node after it came back: %+v", p)
		}
		if got := agFar.emails(); !slices.Contains(got, nodecfg.SubscriptionEmail(int(alice.ID))) {
			t.Errorf("alice was not put back on the far node: %v", got)
		}
		if ev := xrayRestartEvents(r.m, far.ID); len(ev) != 1 || ev[0].Detail != addrA+" to "+addrC {
			t.Errorf("the far node's events %+v", ev)
		}
		r.advance(10 * time.Minute)
		if all, _ := restarted(agFar); all != 1 {
			t.Errorf("the far node was restarted %d times in all", all)
		}
	})
}

// The hub restarts Xray itself, as it does for a config push, so a run of
// address changes is no crash loop. A restart nobody asked for still counts.
func TestRestartsForTheRoutersAddressAreNotACrashLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		r.advance(time.Minute)

		// five addresses in ten minutes: four restarts, and three is a crash loop when they are Xray's own
		for _, a := range []string{"192.168.1.21", "192.168.1.22", "192.168.1.23", "192.168.1.24"} {
			facts.set(ok(withAddr(epoch, a)))
			r.advance(2 * time.Minute)
		}
		if all, _ := restarted(ag); all != 4 {
			t.Fatalf("%d restarts, want 4", all)
		}
		p, _ := r.m.peek(n.ID)
		if p.restarts != 0 || p.agent.Xray.CrashLoop {
			t.Fatalf("after four restarts the hub made: %+v", p)
		}

		// the grace is over, and a restart of Xray's own counts as before
		r.advance(restartGrace)
		ag.set(func(a *fakeAgent) { a.pid, a.started, a.uptime = a.pid+1, a.started+60, 1 })
		r.advance(10 * time.Second)
		if p, _ := r.m.peek(n.ID); p.restarts != 1 {
			t.Errorf("a restart of Xray's own after them: %+v", p)
		}
	})
}

// One change is one restart, whatever happens after it: the poll that sees Xray's
// new process, a new connection to the node, the agent going away and coming
// back, the report being read again with the same address, and a hub that
// starts again.
func TestOneChangeNeverRestartsAnXrayTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		r.advance(time.Minute)
		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(time.Minute)
		if all, _ := restarted(ag); all != 1 {
			t.Fatalf("%d restarts for the change", all)
		}

		r.m.Reconnect(n.ID)
		r.advance(30 * time.Second)
		r.m.Reconnect(n.ID)
		r.advance(30 * time.Second)
		ag.setDown(true)
		r.advance(time.Minute)
		ag.setDown(false)
		r.advance(2 * time.Minute)
		for i := 0; i < 20; i++ { // the same address, read again and again
			facts.set(ok(withAddr(epoch, addrB)))
			r.advance(35 * time.Second)
		}
		if all, _ := restarted(ag); all != 1 {
			t.Errorf("%d restarts after a reconnection, an outage and many reads of the same address", all)
		}
		r.stop()

		// the hub starts again and reads the same address
		r2 := newRig(t, withFacts(newFacts(ok(withAddr(epoch, addrB)))), func(c *Config) { c.Store, c.Connector = r.cfg.Store, r.net })
		r2.start()
		defer r2.stop()
		r2.advance(10 * time.Minute)
		if all, _ := restarted(ag); all != 1 {
			t.Errorf("%d restarts after the hub started again", all)
		}
	})
}

// A restart that fails is said, leaves the mark and the clients as they were, and isn't tried
// at every poll: that is when it is tried again with the next connection, or for the next change.
func TestARestartThatFailsIsTriedAgainOnlyWithTheNextConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		st := r.cfg.Store
		n := addNode(t, st)
		alice := addSubscription(t, st, "alice")
		enable(t, st, alice.ID, n.ID, true)
		email := nodecfg.SubscriptionEmail(int(alice.ID))
		ag := r.net.agent(n.ID)
		ag.set(func(a *fakeAgent) { a.restartErr = errors.New("failed to restart xray: config validation failed") })
		r.start()
		defer r.stop()
		r.advance(time.Minute)

		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(time.Minute)
		if all, _ := restarted(ag); all != 1 {
			t.Fatalf("%d tries, want 1", all)
		}
		if p, _ := r.m.peek(n.ID); !p.resetDue {
			t.Error("the mark went with a restart that failed")
		}
		if r.log.count("restarting a node's Xray after the router's address changed failed", "node=1", "config validation failed") != 1 {
			t.Errorf("the failure is not in the log:\n%s", r.log)
		}
		if len(xrayRestartEvents(r.m, n.ID)) != 0 || !slices.Contains(ag.emails(), email) {
			t.Errorf("events %+v, clients %v", r.m.NodeEvents(n.ID), ag.emails())
		}
		if p, _ := r.m.peek(n.ID); p.restarts != 0 {
			t.Errorf("a restart that never happened was counted: %+v", p)
		}

		// many polls later it has not been tried again
		r.advance(10 * time.Minute)
		if all, _ := restarted(ag); all != 1 {
			t.Fatalf("%d tries over ten minutes, want 1", all)
		}

		// the next connection tries once more, and this time it works
		ag.set(func(a *fakeAgent) { a.restartErr = nil })
		r.m.Reconnect(n.ID)
		r.advance(15 * time.Second)
		if all, _ := restarted(ag); all != 2 {
			t.Fatalf("%d tries after a new connection, want 2", all)
		}
		if p, _ := r.m.peek(n.ID); p.resetDue {
			t.Error("the mark stays after the restart worked")
		}
		if ev := xrayRestartEvents(r.m, n.ID); len(ev) != 1 || ev[0].Detail != addrA+" to "+addrB {
			t.Errorf("events %+v", ev)
		}
		if !slices.Contains(ag.emails(), email) {
			t.Errorf("alice was not put back: %v", ag.emails())
		}
	})
}

// A new change after a failure is a new reason, and is tried on the connection
// the failure was on. The restart answers for both, from the first address to
// the last.
func TestANewChangeAfterAFailedRestartIsTriedOnTheSameConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		ag.set(func(a *fakeAgent) { a.restartErr = errors.New("restart rejected: too many restarts") })
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(time.Minute)
		if all, _ := restarted(ag); all != 1 {
			t.Fatalf("%d tries, want 1", all)
		}

		ag.set(func(a *fakeAgent) { a.restartErr = nil })
		facts.set(ok(withAddr(epoch, addrC)))
		r.advance(time.Minute)
		if all, _ := restarted(ag); all != 2 {
			t.Fatalf("%d tries after another change, want 2", all)
		}
		if made, _ := r.net.opened(n.ID); made != 1 {
			t.Errorf("%d connections were made, the second try was meant for the first", made)
		}
		if ev := xrayRestartEvents(r.m, n.ID); len(ev) != 1 || ev[0].Detail != addrA+" to "+addrC {
			t.Errorf("events %+v", ev)
		}
		if p, _ := r.m.peek(n.ID); p.resetDue {
			t.Error("the mark stays")
		}
	})
}

// Only nodes whose install is done are marked. One that is still being
// installed has nothing to restart, and one added after the change connects
// to the router's new address in the first place.
func TestOnlyRegisteredNodesAreMarkedAndOnlyForChangesBeforeThem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		st := r.cfg.Store
		done := addNode(t, st)
		unfinished := addNode(t, st, func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()
		r.advance(time.Minute)

		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(time.Minute)
		if all, _ := restarted(r.net.agent(done.ID)); all != 1 {
			t.Errorf("the finished node was restarted %d times", all)
		}
		if p, ok := r.m.peek(unfinished.ID); ok && p.resetDue {
			t.Error("a node whose install is not done is marked")
		}
		if all, _ := restarted(r.net.agent(unfinished.ID)); all != 0 {
			t.Error("a node whose install is not done was restarted")
		}

		// the install finishes after the change, and a node is added after it
		editNode(t, st, unfinished.ID, func(n *model.Node) { n.State = model.NodeRegistered })
		late := addNode(t, st)
		r.m.Sync()
		r.advance(10 * time.Minute)
		for _, n := range []model.Node{unfinished, late} {
			if all, _ := restarted(r.net.agent(n.ID)); all != 0 {
				t.Errorf("node %d, which came after the change, was restarted %d times", n.ID, all)
			}
		}
		if got := strings.Count(r.log.String(), "restarted a node's Xray"); got != 1 {
			t.Errorf("%d restarts in the log, want 1:\n%s", got, r.log)
		}
	})
}

// A config push, a switch and a reconcile each hold the node's turn at its
// clients, and a restart that landed in the middle of one would undo it. So the
// restart waits for its turn, and the node is marked all the while.
func TestTheRestartWaitsForWhatHoldsTheNodesTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(withAddr(epoch, addrA)))
		r := newRig(t, withFacts(facts))
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		r.advance(time.Minute)

		unlock, err := r.m.lockNodes(context.Background(), n.ID)
		must(t, err)
		facts.set(ok(withAddr(epoch, addrB)))
		r.advance(3 * time.Minute)
		if all, _ := restarted(ag); all != 0 {
			t.Fatalf("Xray was restarted %d times while something else held the node's turn", all)
		}
		if p, _ := r.m.peek(n.ID); !p.resetDue {
			t.Error("the mark is gone before the restart was made")
		}

		unlock()
		r.advance(30 * time.Second)
		if all, _ := restarted(ag); all != 1 {
			t.Errorf("%d restarts once the turn was free, want 1", all)
		}
		if p, _ := r.m.peek(n.ID); p.resetDue {
			t.Error("the mark stays after the restart")
		}
	})
}
