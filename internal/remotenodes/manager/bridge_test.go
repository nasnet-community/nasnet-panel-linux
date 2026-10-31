// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

func TestTheBridgeIsStartedWithTheReadyNodesAndTheConfiguredAddresses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()

		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Fatalf("the bridge holds %v", got)
		}
		spec := r.bridge.last()
		if spec.DialAddr != "192.168.50.18" || spec.Exit.Addr != "192.168.50.17" || !slices.Equal(spec.DNS.Servers, DefaultDNS) || spec.StatusAddr != DefaultStatusAddr {
			t.Errorf("spec = %+v", spec)
		}
		if p := spec.Nodes[1]; p.Addr != "203.0.113.2" || p.Port != 443 || p.UUID != bridgeIDOf(2) || p.ReverseTag != "rvs-to-node-2" || p.Flow != bridgespec.VisionFlow {
			t.Errorf("portal = %+v", p)
		}
		if n, stops := r.bridge.counts(); n != 1 || stops != 0 {
			t.Errorf("the bridge was given %d specs and stopped %d times", n, stops)
		}
		if got := r.m.Entry().Bridge; got != "running" {
			t.Errorf("the entry says the bridge is %s", got)
		}
		if r.log.count("the bridge has its spec", "portals=2") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		// the tunnels, as the bridge's page says
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "" {
			t.Errorf("before the tunnel is up: %+v", got)
		}
		r.bridge.tunnel("node-1", up(3, 900, 100))
		r.advance(3 * time.Second)
		got := r.m.tunnelNow(1)
		if got.State != "up" || got.Streams != 3 || got.BytesToSubs != 900 || got.BytesFromSubs != 100 || got.Since == nil || !got.Since.Equal(epoch) {
			t.Errorf("tunnel 1 = %+v", got)
		}
		if got := r.m.tunnelNow(2); got.State != "down" {
			t.Errorf("tunnel 2 = %+v", got)
		}
	})
}

// A look that finds the same spec costs the tunnels nothing: there is a
// restart for each change of the hash and none for a poll that finds the same.
func TestIdenticalSpecsAreNotSentAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(10 * time.Minute) // a lot of looks, polls, Syncs
		r.m.Sync()
		r.m.Sync()
		synctest.Wait()
		if n, stops := r.bridge.counts(); n != 1 || stops != 0 {
			t.Errorf("%d specs and %d stops", n, stops)
		}
		if r.bridge.updates != 1 {
			t.Errorf("Update was called %d times", r.bridge.updates)
		}
	})
}

func TestTheSpecFollowsTheNodes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		st := r.cfg.Store
		addNode(t, st)
		addNode(t, st)
		r.start()
		defer r.stop()
		sync := func() { r.m.Sync(); synctest.Wait() }
		specs := func() int { n, _ := r.bridge.counts(); return n }

		addNode(t, st)
		sync()
		if got := r.bridge.tags(); specs() != 2 || !slices.Equal(got, []string{"node-1", "node-2", "node-3"}) {
			t.Fatalf("a node added: %d specs, bridge holds %v", specs(), got)
		}
		editNode(t, st, 2, func(n *model.Node) { n.EntryPort = 8443 })
		sync()
		if p := r.bridge.last().Nodes[1]; specs() != 3 || p.Port != 8443 {
			t.Fatalf("a port edited: %d specs, portal = %+v", specs(), p)
		}
		removeNode(t, st, 3)
		sync()
		if got := r.bridge.tags(); specs() != 4 || !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Fatalf("a node removed: %d specs, bridge holds %v", specs(), got)
		}
		// a node that has no bridge client is not carried, whatever else it has
		editNode(t, st, 1, func(n *model.Node) { n.BridgeUUID = "" })
		sync()
		if got := r.bridge.tags(); specs() != 5 || !slices.Equal(got, []string{"node-2"}) {
			t.Fatalf("a node with no bridge id: %d specs, bridge holds %v", specs(), got)
		}
		// a node's install is begun again: it is not carried
		editNode(t, st, 2, func(n *model.Node) { n.State = model.NodeInstalling })
		sync()
		if got := r.bridge.last(); got != nil || r.bridge.stops != 1 {
			t.Fatalf("with nothing ready the bridge holds %v after %d stops", got, r.bridge.stops)
		}
		if got := r.m.Entry().Bridge; got != "waiting" {
			t.Errorf("with the entry on and nothing to carry the bridge is %s", got)
		}
		// and the one that is finished is carried again
		editNode(t, st, 2, func(n *model.Node) { n.State = model.NodeRegistered })
		sync()
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-2"}) || r.m.Entry().Bridge != "running" {
			t.Fatalf("bridge holds %v and is %s", got, r.m.Entry().Bridge)
		}
	})
}

func TestTheBridgeStopsWithTheEntryAndRunsAgainWithIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.bridge.tunnel("node-1", up(1, 1, 1))
		r.advance(3 * time.Second)
		if r.m.tunnelNow(1).State != "up" {
			t.Fatal("the tunnel isn't up before the entry goes off")
		}

		// the VPN goes
		r.flip(func(f *routerfacts.Facts) { f.VPN, f.VPNKind, f.VPNLabel, f.RouteVPN = false, "", "", false })
		if r.m.Entry().On {
			t.Fatal("the entry is on with no VPN")
		}
		if n, stops := r.bridge.counts(); n != 1 || stops != 1 || r.bridge.last() != nil {
			t.Fatalf("%d specs, %d stops, holds %v", n, stops, r.bridge.last())
		}
		if got := r.m.Entry().Bridge; got != "stopped" {
			t.Errorf("the bridge is %s", got)
		}
		for _, id := range []uint{1, 2} {
			if got := r.m.tunnelNow(id); got.State != "off" || got.Reason != "entry_off" {
				t.Errorf("tunnel %d = %+v", id, got)
			}
		}
		ev := r.m.NodeEvents(1)
		if got := kinds(ev); !slices.Equal(got, []string{EventTunnelUp, EventTunnelDown}) || ev[0].Detail != "entry_off" {
			t.Errorf("node 1's events = %v, the last: %+v", got, ev[0])
		}
		if got := kinds(r.m.NodeEvents(2)); len(got) != 0 {
			t.Errorf("node 2's events = %v: its tunnel was never up", got)
		}
		if r.log.count("the bridge is stopped", "the router's entry is off") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		// it comes back, and the bridge is started again with the spec it had
		r.flip(func(*routerfacts.Facts) {})
		if n, stops := r.bridge.counts(); n != 2 || stops != 1 {
			t.Fatalf("%d specs, %d stops after the entry came back", n, stops)
		}
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) || r.m.Entry().Bridge != "running" {
			t.Errorf("bridge holds %v and is %s", got, r.m.Entry().Bridge)
		}
	})
}

// A router that doesn't report keeps the bridge down, and so does every
// other reason the entry is off, whichever link it is.
func TestTheBridgeNeverRunsWithTheEntryOff(t *testing.T) {
	for name, edit := range map[string]func(*routerfacts.Facts){
		"no Domestic link":  func(f *routerfacts.Facts) { f.Domestic, f.RouteDomestic = false, false },
		"RouterOS too old":  func(f *routerfacts.Facts) { f.ROS = "7.9" },
		"a route not ready": func(f *routerfacts.Facts) { f.RouteVPN = false },
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := healthy(epoch)
				edit(&h)
				r := newBridgeRig(t)
				r.facts.set(ok(h))
				r.m.evalEntry(t.Context())
				addNode(t, r.cfg.Store)
				r.start()
				defer r.stop()
				r.advance(time.Minute)
				if r.bridge.updates != 0 || r.m.Entry().On || r.m.Entry().Bridge != "stopped" {
					t.Errorf("%d updates, entry %+v", r.bridge.updates, r.m.Entry())
				}
				if got := r.m.tunnelNow(1); got.State != "off" || got.Reason != "entry_off" {
					t.Errorf("tunnel = %+v", got)
				}
			})
		})
	}
}

// The Xray guard: the bridge and a node's portal run one build.
func TestANodeWithAnotherXrayLeavesTheSpecAndComesBackWithTheRightOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Fatalf("the bridge holds %v", got)
		}

		// node 2 is given another Xray by hand
		r.net.agent(2).set(func(a *fakeAgent) {
			a.xrayLine = "Xray 26.10.20 (Xray, Penetrates Everything.) abc (go1.26.1 linux/arm64)"
		})
		r.advance(6 * time.Second)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Fatalf("the bridge holds %v after node 2's Xray changed", got)
		}
		if got := r.m.tunnelNow(2); got.State != "off" || got.Reason != "xray_mismatch" {
			t.Errorf("tunnel 2 = %+v", got)
		}
		if got := r.m.tunnelNow(1); got.State == "off" {
			t.Errorf("tunnel 1 = %+v: one node's Xray stopped another's tunnel", got)
		}
		if got := nodeOf(r.cfg.Store, 2).XrayVersion; got != "26.10.20" {
			t.Errorf("the state holds %q", got)
		}
		if r.log.count("left out of the bridge", "node=2", "26.10.20") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		// it is brought up to the pin
		r.net.agent(2).set(func(a *fakeAgent) {
			a.xrayLine = "Xray " + pin.Version + " (Xray, Penetrates Everything.) 1b1647c (go1.26.1 linux/arm64)"
		})
		r.advance(6 * time.Second)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Fatalf("the bridge holds %v after node 2 was updated", got)
		}
		if got := r.m.tunnelNow(2); got.State != "down" || got.Reason != "" {
			t.Errorf("tunnel 2 = %+v", got)
		}
	})
}

// The hub restarts while a node is out of reach. What the state kept of its
// Xray decides, and a node that never said stays out.
func TestWhatTheStateKeptOfAnUnreachableNodesXrayDecides(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)                                                     // the pin: carried
		addNode(t, r.cfg.Store, func(n *model.Node) { n.XrayVersion = "26.10.20" }) // another
		addNode(t, r.cfg.Store, func(n *model.Node) { n.XrayVersion = "" })         // never said
		for _, id := range []uint{1, 2, 3} {
			r.net.agent(id).setDown(true)
		}
		r.start()
		defer r.stop()
		r.advance(10 * time.Second) // each is unreachable by now

		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Fatalf("the bridge holds %v: an agent that is down doesn't keep its node out, and its Xray does", got)
		}
		for id, want := range map[uint]struct{ state, reason string }{
			1: {"down", "agent_down"}, 2: {"off", "xray_mismatch"}, 3: {"down", "agent_down"},
		} {
			if got := r.m.tunnelNow(id); got.State != want.state || got.Reason != want.reason {
				t.Errorf("tunnel %d = %+v, want %v", id, got, want)
			}
		}
		if r.log.count("left out of the bridge", "node=3", "not said which Xray") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		// node 3 answers, and says it runs the pin: it is carried
		r.net.agent(3).setDown(false)
		r.advance(35 * time.Second) // the wait between tries is up to 30 s by now
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-3"}) {
			t.Errorf("the bridge holds %v", got)
		}
	})
}

func TestAHostNameIsResolvedThroughTheResolverAndTheAnswerIsKept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		r.dns.set("node.example.com", "203.0.113.50")
		addNode(t, r.cfg.Store, func(n *model.Node) { n.Host = "node.example.com" })
		addNode(t, r.cfg.Store) // an address needs no lookup
		r.start()
		defer r.stop()

		if p := r.bridge.last().Nodes[0]; p.Addr != "203.0.113.50" {
			t.Fatalf("the portal is dialled at %s, not at what the name resolves to", p.Addr)
		}
		if r.dns.lookups() != 1 || r.dns.calls[0] != "ip4 node.example.com" {
			t.Fatalf("lookups = %v", r.dns.calls)
		}
		// kept for five minutes, whatever the number of looks
		r.advance(4 * time.Minute)
		if r.dns.lookups() != 1 {
			t.Errorf("%d lookups within the five minutes", r.dns.lookups())
		}
		// then asked again, and a new address is a new spec
		r.dns.set("node.example.com", "203.0.113.51")
		r.advance(2 * time.Minute)
		if r.dns.lookups() != 2 || r.bridge.last().Nodes[0].Addr != "203.0.113.51" {
			t.Errorf("lookups = %v, portal at %s", r.dns.calls, r.bridge.last().Nodes[0].Addr)
		}
		if n, _ := r.bridge.counts(); n != 2 {
			t.Errorf("%d specs", n)
		}
		// an answer in another family, a multicast or an unspecified one is none
		r.dns.set("node.example.com", "2001:db8::5", "224.0.0.1", "0.0.0.0", "203.0.113.52")
		r.advance(6 * time.Minute)
		if got := r.bridge.last().Nodes[0].Addr; got != "203.0.113.52" {
			t.Errorf("portal at %s", got)
		}
	})
}

func TestAFailedLookupKeepsTheLastAddressAndALookupThatNeverWorkedLeavesTheNodeOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		r.dns.set("good.example.com", "203.0.113.50")
		r.dns.fail("never.example.com", errors.New("no such host"))
		addNode(t, r.cfg.Store, func(n *model.Node) { n.Host = "good.example.com" })
		addNode(t, r.cfg.Store, func(n *model.Node) { n.Host = "never.example.com" })
		r.start()
		defer r.stop()

		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Fatalf("the bridge holds %v", got)
		}
		if r.log.count("left out of the bridge", "node=2", "no such host") != 1 || r.log.count("could not resolve", "node=2") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		// the good one stops resolving: its last address stays, and it is said once
		r.dns.fail("good.example.com", errors.New("server failure"))
		before := r.dns.lookups()
		r.advance(11 * time.Minute)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1"}) {
			t.Fatalf("the bridge holds %v", got)
		}
		if n, _ := r.bridge.counts(); n != 1 {
			t.Errorf("%d specs: a lookup that failed changed the bridge", n)
		}
		if r.log.count("could not resolve", "node=1", "server failure", "keeping=203.0.113.50") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
		// and it is not asked at every look: two nodes' lookups every thirty seconds at most
		if got := r.dns.lookups() - before; got > 2*(11*60/30+2) {
			t.Errorf("%d lookups in 11 minutes", got)
		}

		// the other one is found at last
		r.dns.fail("never.example.com", nil)
		r.dns.set("never.example.com", "203.0.113.60")
		r.advance(time.Minute)
		if got := r.bridge.tags(); !slices.Equal(got, []string{"node-1", "node-2"}) {
			t.Errorf("the bridge holds %v", got)
		}
		if r.log.count("resolves again", "node=2") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
	})
}

// No lookup is made for a node that couldn't be carried anyway.
func TestNoLookupIsMadeForANodeThatIsNotReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		r.dns.set("node.example.com", "203.0.113.50")
		addNode(t, r.cfg.Store, func(n *model.Node) { n.Host, n.XrayVersion = "node.example.com", "26.10.20" })
		addNode(t, r.cfg.Store, func(n *model.Node) { n.Host, n.BridgeUUID = "other.example.com", "" })
		// the first node's agent says what the state kept: it runs another Xray
		r.net.agent(1).set(func(a *fakeAgent) { a.xrayLine = "Xray 26.10.20 (Xray, Penetrates Everything.)" })
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		if r.dns.lookups() != 0 {
			t.Errorf("lookups = %v", r.dns.calls)
		}
	})
}

// The bridge's addresses have to be the router's. An exit it can't bind would
// leave from whichever address the system picks, and the dial address the same.
func TestTheBridgeStaysStoppedWithoutItsAddresses(t *testing.T) {
	for name, tc := range map[string]struct {
		mod  func(*Config)
		says string
	}{
		"the exit isn't held": {func(c *Config) { c.Addrs.Exit.Local = false }, "ADDR_EXIT 192.168.50.17"},
		"the dial isn't held": {func(c *Config) { c.Addrs.Nodes.Local = false }, "ADDR_NODES 192.168.50.18"},
		"neither is held":     {func(c *Config) { c.Addrs.Exit.Local, c.Addrs.Nodes.Local = false, false }, "ADDR_EXIT 192.168.50.17"},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newBridgeRig(t, tc.mod)
				addNode(t, r.cfg.Store)
				r.start()
				defer r.stop()
				r.advance(time.Minute)

				if r.bridge.updates != 0 || r.bridge.last() != nil {
					t.Fatalf("the bridge was started %d times with an address it can't bind", r.bridge.updates)
				}
				// the entry is on and a node is ready: the bridge is stopped, not waiting
				if e := r.m.Entry(); !e.On || e.Bridge != "stopped" {
					t.Errorf("entry = %+v", e)
				}
				if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "bridge_down" {
					t.Errorf("tunnel = %+v", got)
				}
				// said once, with the address, however long it goes on
				if r.log.count("the bridge is not running", tc.says) != 1 {
					t.Errorf("log:\n%s", r.log)
				}
			})
		})
	}
}

// A bridge program that is missing is a thing the hub starts without, and
// tries again for, now and then and not at every look.
func TestABridgeThatCannotBeStartedIsTriedAgainEveryHalfMinuteAndSaidOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		r.bridge.failUpdate = errors.New("fork/exec /remote-nodes-bridge: no such file or directory")
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(95 * time.Second) // tries at 0, 30, 60 and 90

		if r.bridge.updates != 4 {
			t.Fatalf("Update was called %d times, want 4", r.bridge.updates)
		}
		if r.log.count("the bridge could not be started", "no such file") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
		if got := r.m.Entry().Bridge; got != "stopped" {
			t.Errorf("the bridge is %s", got)
		}
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "bridge_down" {
			t.Errorf("tunnel = %+v", got)
		}
		// the program turns up
		r.bridge.set(func(b *fakeBridge) { b.failUpdate = nil })
		r.advance(30 * time.Second)
		if n, _ := r.bridge.counts(); n != 1 || r.m.Entry().Bridge != "running" {
			t.Errorf("%d specs, the bridge is %s", n, r.m.Entry().Bridge)
		}
		// a change of spec is not made to wait out the thirty seconds
		r.bridge.set(func(b *fakeBridge) { b.failUpdate = errors.New("again") })
		addNode(t, r.cfg.Store)
		r.m.Sync()
		synctest.Wait()
		if r.bridge.updates != 6 {
			t.Errorf("Update was called %d times, want 6 (a try for the new spec, at once)", r.bridge.updates)
		}
	})
}

// When the bridge was running and the new spec won't start, the supervisor
// holds it and tries by itself. Calling Update again would only stop it.
func TestASupervisorThatHoldsTheSpecIsLeftToTryAgainByItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.bridge.set(func(b *fakeBridge) {
			b.failUpdate, b.failState = errors.New("exec format error"), bridgectl.StateBackoff
		})
		addNode(t, r.cfg.Store)
		r.m.Sync()
		synctest.Wait()
		r.advance(2 * time.Minute)
		if r.bridge.updates != 2 {
			t.Errorf("Update was called %d times, want 2", r.bridge.updates)
		}
		if r.log.count("the bridge could not be started", "supervisor_retries=true") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
		// it isn't running at the moment, and the page says so
		if got := r.m.Entry().Bridge; got != "stopped" {
			t.Errorf("the bridge is %s", got)
		}
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "bridge_down" {
			t.Errorf("tunnel = %+v", got)
		}
		// the supervisor got it going: the tunnels are read again
		r.bridge.set(func(b *fakeBridge) { b.state, b.failUpdate = bridgectl.StateRunning, nil })
		r.advance(3 * time.Second)
		if got := r.m.Entry().Bridge; got != "running" {
			t.Errorf("the bridge is %s", got)
		}
	})
}

func TestTheEntrysWordForTheBridge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		r.start()
		defer r.stop()
		// the entry is on and there is nothing to carry
		if got := r.m.Entry(); !got.On || got.Bridge != "waiting" {
			t.Errorf("no nodes: %+v", got)
		}
		n := addNode(t, r.cfg.Store, func(n *model.Node) { n.BridgeUUID = "" })
		r.m.Sync()
		synctest.Wait()
		if got := r.m.Entry().Bridge; got != "waiting" {
			t.Errorf("a node with no bridge id: %s", got)
		}
		editNode(t, r.cfg.Store, n.ID, func(n *model.Node) { n.BridgeUUID = bridgeIDOf(n.ID) })
		r.m.Sync()
		synctest.Wait()
		if got := r.m.Entry().Bridge; got != "running" {
			t.Errorf("a node that is ready: %s", got)
		}
		r.flip(func(f *routerfacts.Facts) { f.Domestic = false })
		if got := r.m.Entry(); got.On || got.Bridge != "stopped" {
			t.Errorf("entry off: %+v", got)
		}
	})
}

func TestWithNoBridgeProgramTheNodesAreStillWatchedAndTheTunnelsAreDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t, func(c *Config) { c.Bridge = nil })
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		if got := r.m.Entry(); !got.On || got.Bridge != "stopped" {
			t.Errorf("entry = %+v", got)
		}
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "bridge_down" {
			t.Errorf("tunnel = %+v", got)
		}
		if p, _ := r.m.peek(1); !p.connected {
			t.Error("the node isn't polled")
		}
	})
}

// A tunnel is news when it comes up and when it stops being up, and not when
// it is polled again in the same state.
func TestTunnelEventsComeOncePerChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		events := func() []string { return kinds(r.m.NodeEvents(1)) }

		r.advance(10 * time.Second)
		if len(events()) != 0 {
			t.Fatalf("events = %v before it is up", events())
		}
		r.bridge.tunnel("node-1", up(0, 0, 0))
		r.advance(10 * time.Second)
		r.advance(10 * time.Second)
		if got := events(); !slices.Equal(got, []string{EventTunnelUp}) {
			t.Fatalf("events = %v", got)
		}

		r.bridge.tunnel("node-1", bridgespec.NodeStatus{Up: false, Since: epoch.Add(time.Minute)})
		r.advance(10 * time.Second)
		r.advance(10 * time.Second)
		if got := events(); !slices.Equal(got, []string{EventTunnelUp, EventTunnelDown}) {
			t.Fatalf("events = %v", got)
		}
		if ev := r.m.NodeEvents(1); ev[0].Detail != "" {
			t.Errorf("a tunnel that dropped has the reason %q", ev[0].Detail)
		}

		// the bridge's page stops answering: it was not up, so it isn't news
		r.bridge.tunnel("node-1", up(0, 0, 0))
		r.advance(5 * time.Second)
		r.bridge.set(func(b *fakeBridge) { b.pageDown = true })
		r.advance(5 * time.Second)
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "bridge_down" {
			t.Errorf("with the page down: %+v", got)
		}
		if got := events(); !slices.Equal(got, []string{EventTunnelUp, EventTunnelDown, EventTunnelUp, EventTunnelDown}) {
			t.Fatalf("events = %v", got)
		}
		if ev := r.m.NodeEvents(1); ev[0].Detail != "bridge_down" {
			t.Errorf("the last: %+v", ev[0])
		}
		r.bridge.set(func(b *fakeBridge) { b.pageDown = false })
		r.advance(5 * time.Second)
		if got := events(); len(got) != 5 || got[4] != EventTunnelUp {
			t.Errorf("events = %v after the page came back", got)
		}
	})
}

// The tunnel doesn't go through the agent, so one that is up stays up when the
// agent stops answering, and agent_down is for a tunnel that is down.
func TestAnAgentThatStopsAnsweringIsAReasonOnlyForATunnelThatIsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.bridge.tunnel("node-1", up(2, 5, 6))
		r.advance(3 * time.Second)

		r.net.agent(1).setDown(true)
		r.advance(20 * time.Second)
		if p, _ := r.m.peek(1); !p.unreachable {
			t.Fatal("the node isn't unreachable")
		}
		if got := r.m.tunnelNow(1); got.State != "up" || got.Reason != "" {
			t.Errorf("a tunnel that is up, with its agent out of reach: %+v", got)
		}
		r.bridge.tunnel("node-1", bridgespec.NodeStatus{Up: false, Since: epoch})
		r.advance(3 * time.Second)
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "agent_down" {
			t.Errorf("a tunnel that is down, with its agent out of reach: %+v", got)
		}
		r.net.agent(1).setDown(false)
		r.advance(35 * time.Second)
		if got := r.m.tunnelNow(1); got.State != "down" || got.Reason != "" {
			t.Errorf("once the agent answers again: %+v", got)
		}
	})
}

func TestTheBridgeIsLeftToTheSupervisorWhenTheManagerStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		r.stop()
		// the hub stops the bridge itself, after the manager and the counters: see main
		if n, stops := r.bridge.counts(); n != 1 || stops != 0 {
			t.Errorf("%d specs, %d stops", n, stops)
		}
	})
}

func TestTheBridgeTagsAreTheNodesAndTheStatusPageIsLookedAtByTag(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		// the page lists a tunnel the spec doesn't have (an old one): it is no node's
		r.bridge.tunnel("node-9", up(7, 7, 7))
		r.bridge.tunnel("node-2", up(2, 20, 2))
		r.advance(3 * time.Second)
		if got := r.m.tunnelNow(2); got.State != "up" || got.Streams != 2 || got.BytesToSubs != 20 {
			t.Errorf("tunnel 2 = %+v", got)
		}
		if got := r.m.tunnelNow(1); got.State == "up" {
			t.Errorf("tunnel 1 = %+v", got)
		}
		if got := r.m.tunnelNow(9); got.State != "off" {
			t.Errorf("a node nobody has: %+v", got)
		}
		if strings.Contains(r.log.String(), bridgeIDOf(1)) || strings.Contains(r.log.String(), fixtureKey) {
			t.Errorf("the log has a credential in it:\n%s", r.log)
		}
	})
}
