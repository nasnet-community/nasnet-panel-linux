// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestSwitchingOffKeepsTheClientIdAndSwitchingOnGivesItBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		ag := r.net.agent(n.ID)
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		first := idOnNode(t, r.cfg.Store, sara.ID, n.ID).ClientUUID

		v, err := r.m.SetEnabled(ctx, sara.ID, n.ID, false)
		must(t, err)
		if v.Nodes[0].Enabled {
			t.Errorf("the view still says it is on: %+v", v.Nodes)
		}
		e := idOnNode(t, r.cfg.Store, sara.ID, n.ID)
		if e.Enabled || e.ClientUUID != first {
			t.Errorf("after switching off the state holds %+v, want the same id and off", e)
		}
		if _, ok := holds(ag, subEmail(sara.ID)); ok {
			t.Error("the node still has the client")
		}

		v, err = r.m.SetEnabled(ctx, sara.ID, n.ID, true)
		must(t, err)
		if !v.Nodes[0].Enabled {
			t.Errorf("the view says it is off: %+v", v.Nodes)
		}
		if got, ok := holds(ag, subEmail(sara.ID)); !ok || got != first {
			t.Errorf("after switching on again the node has %q (%v), want the first id %q", got, ok, first)
		}
		if got := idOnNode(t, r.cfg.Store, sara.ID, n.ID); !got.Enabled || got.ClientUUID != first {
			t.Errorf("the state holds %+v", got)
		}

		events := r.m.NodeEvents(n.ID)
		if got := kinds(events); !slices.Equal(got, []string{EventSubscriptionOn, EventSubscriptionOff, EventSubscriptionOn}) {
			t.Errorf("events = %v", got)
		}
		for _, ev := range events {
			if ev.Detail != "Sara" {
				t.Errorf("an event says %q", ev.Detail)
			}
		}
	})
}

// A subscription that was never on at a node gets its client id the first time
// it is switched on there, and keeps it from then on.
func TestSwitchingOnAtANodeItWasNeverOnAtMakesAnIdOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n1, n2 := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n1.ID}, api.SubscriptionLimits{})
		must(t, err)

		// off where it never was: no record, no call, no event
		calls := r.net.agent(n2.ID).callCount()
		if _, err := r.m.SetEnabled(ctx, sara.ID, n2.ID, false); err != nil {
			t.Fatal(err)
		}
		if st := r.cfg.Store.Read(); st.FindEnablement(sara.ID, n2.ID) != nil {
			t.Error("switching off made a record of a client that never was")
		}
		if got := r.net.agent(n2.ID).callsFrom(calls); len(got) != 0 {
			t.Errorf("node 2 was called: %v", got)
		}
		if len(r.m.NodeEvents(n2.ID)) != 0 {
			t.Error("an event for a switch that changed nothing")
		}

		_, err = r.m.SetEnabled(ctx, sara.ID, n2.ID, true)
		must(t, err)
		id := idOnNode(t, r.cfg.Store, sara.ID, n2.ID).ClientUUID
		if !lowerUUID.MatchString(id) || id == idOnNode(t, r.cfg.Store, sara.ID, n1.ID).ClientUUID {
			t.Errorf("the new client id is %q", id)
		}
		for i := 0; i < 3; i++ {
			for _, on := range []bool{false, true} {
				_, err = r.m.SetEnabled(ctx, sara.ID, n2.ID, on)
				must(t, err)
			}
		}
		if got := idOnNode(t, r.cfg.Store, sara.ID, n2.ID).ClientUUID; got != id {
			t.Errorf("the id changed to %q", got)
		}
	})
}

// Asking for what is so does nothing to the state and makes no event, but it
// does go to the node: that puts right one that missed the last change.
func TestAskingForWhatIsSoIsSentAgainAndMakesNoEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		ag := r.net.agent(n.ID)
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		writes := r.cfg.Store.Writes()
		events := len(r.m.NodeEvents(n.ID))

		// the node lost the client without the hub knowing
		ag.set(func(a *fakeAgent) { delete(a.users, subEmail(sara.ID)) })
		_, err = r.m.SetEnabled(ctx, sara.ID, n.ID, true)
		must(t, err)
		if _, ok := holds(ag, subEmail(sara.ID)); !ok {
			t.Error("the node was not told again")
		}
		if r.cfg.Store.Writes() != writes || len(r.m.NodeEvents(n.ID)) != events {
			t.Errorf("a switch that changed nothing wrote %d times and made %d events", r.cfg.Store.Writes()-writes, len(r.m.NodeEvents(n.ID))-events)
		}

		// and off twice: the second finds nothing to remove, which is no error
		for i := 0; i < 2; i++ {
			if _, err := r.m.SetEnabled(ctx, sara.ID, n.ID, false); err != nil {
				t.Fatalf("switching off, time %d: %v", i+1, err)
			}
		}
		if kinds := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(kinds, []string{EventSubscriptionOn, EventSubscriptionOff}) {
			t.Errorf("events = %v", kinds)
		}
	})
}

// A node the router can't reach takes the switch when it is back, whichever
// way it went, and the reconcile that does it works from the state.
func TestASwitchForANodeThatCantBeReachedIsAppliedWhenItIsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		ali, err := r.m.CreateSubscription(ctx, "Ali", nil, api.SubscriptionLimits{})
		must(t, err)
		saraID := idOnNode(t, r.cfg.Store, sara.ID, n.ID).ClientUUID

		ag.setDown(true)
		r.advance(time.Minute)
		if p, _ := r.m.peek(n.ID); p.connected || !p.unreachable {
			t.Fatalf("node = %+v", p)
		}
		// Sara off and Ali on, while it is away
		if _, err := r.m.SetEnabled(ctx, sara.ID, n.ID, false); err != nil {
			t.Fatalf("off for a node that can't be reached: %v", err)
		}
		if _, err := r.m.SetEnabled(ctx, ali.ID, n.ID, true); err != nil {
			t.Fatalf("on for a node that can't be reached: %v", err)
		}
		if e := idOnNode(t, r.cfg.Store, sara.ID, n.ID); e.Enabled || e.ClientUUID != saraID {
			t.Errorf("Sara = %+v", e)
		}
		aliID := idOnNode(t, r.cfg.Store, ali.ID, n.ID)
		if !aliID.Enabled {
			t.Errorf("Ali = %+v", aliID)
		}
		if got := kinds(r.m.NodeEvents(n.ID)); got[len(got)-2] != EventSubscriptionOff || got[len(got)-1] != EventSubscriptionOn {
			t.Errorf("the events don't show the switches: %v", got)
		}

		ag.setDown(false)
		r.advance(time.Minute)
		if _, ok := holds(ag, subEmail(sara.ID)); ok {
			t.Error("Sara's client is still on the node")
		}
		if got, ok := holds(ag, subEmail(ali.ID)); !ok || got != aliID.ClientUUID {
			t.Errorf("Ali's client on the node is %q (%v), want %q", got, ok, aliID.ClientUUID)
		}
	})
}

func TestSwitchingIsRefusedForWhatIsNotThere(t *testing.T) {
	ctx := context.Background()
	install := jobs.Job{ID: "job-3", Kind: jobs.KindInstall, State: jobs.StateRunning, Steps: []jobs.Step{{Name: "upload", State: jobs.StepRunning}}}
	cfg, _ := baseConfig(t)
	good := addNode(t, cfg.Store)
	dead := addNode(t, cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
	running := addNode(t, cfg.Store)
	cfg.Jobs = fakeJobs{running.ID: install}
	m, err := New(cfg)
	must(t, err)
	sara, err := m.CreateSubscription(ctx, "Sara", []uint{good.ID}, api.SubscriptionLimits{})
	must(t, err)
	before, writes := cfg.Store.Read(), cfg.Store.Writes()

	for name, c := range map[string]struct {
		sub, node uint
		want      error
	}{
		"a subscription nobody has":       {99, good.ID, model.ErrNotFound},
		"subscription 0":                  {0, good.ID, model.ErrNotFound},
		"a node nobody has":               {sara.ID, 99, model.ErrNotFound},
		"node 0":                          {sara.ID, 0, model.ErrNotFound},
		"a node whose install died":       {sara.ID, dead.ID, model.ErrNotFound},
		"a node whose install is running": {sara.ID, running.ID, jobs.ErrBusy},
	} {
		for _, on := range []bool{true, false} {
			if _, err := m.SetEnabled(ctx, c.sub, c.node, on); !errors.Is(err, c.want) {
				t.Errorf("%s (on=%v): %v, want %v", name, on, err, c.want)
			}
		}
	}
	after := cfg.Store.Read()
	if len(after.Enablements) != len(before.Enablements) || cfg.Store.Writes() != writes {
		t.Errorf("a refused switch changed the state: %d clients, %d writes", len(after.Enablements), cfg.Store.Writes()-writes)
	}
}

// slowList is a connector whose clients hold back ListUsers until released: a
// reconcile that is under way.
type slowList struct {
	Connector
	entered, release chan struct{}
}

func (s *slowList) Connect(ctx context.Context, n model.Node) (Client, error) {
	c, err := s.Connector.Connect(ctx, n)
	if err != nil {
		return nil, err
	}
	return &slowConn{Client: c, s: s}, nil
}

type slowConn struct {
	Client
	s *slowList
}

func (c *slowConn) ListUsers(ctx context.Context, tag string) ([]*pb.UserInfo, error) {
	select {
	case c.s.entered <- struct{}{}:
	default:
	}
	select {
	case <-c.s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.Client.ListUsers(ctx, tag)
}

// A reconcile reads what is wanted, then works on the node for as long as it
// takes. A switch that lands in the middle must wait for it, or the reconcile
// would put back a client that was just taken off.
func TestASwitchWaitsForAReconcileThatIsUnderWay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slow := &slowList{entered: make(chan struct{}, 1), release: make(chan struct{})}
		r := newRig(t, func(c *Config) { slow.Connector = c.Connector; c.Connector = slow })
		n := addNode(t, r.cfg.Store)
		sara := addSubscription(t, r.cfg.Store, "Sara")
		enable(t, r.cfg.Store, sara.ID, n.ID, true)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		select {
		case <-slow.entered:
		default:
			t.Fatal("the reconcile never began")
		}

		done := make(chan error, 1)
		go func() {
			_, err := r.m.SetEnabled(context.Background(), sara.ID, n.ID, false)
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("the switch went through while the reconcile was still working: %v", err)
		default:
		}
		if !clientIsOn(r.cfg.Store, sara.ID, n.ID) {
			t.Fatal("the switch was saved while the reconcile was still working")
		}

		close(slow.release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if _, ok := holds(ag, subEmail(sara.ID)); ok {
			t.Error("the node has a client that was switched off")
		}
		if clientIsOn(r.cfg.Store, sara.ID, n.ID) {
			t.Error("the state says it is on")
		}
	})
}

// A request that gives up while it waits for the node's lock leaves no change
// behind.
func TestASwitchThatIsGivenUpWhileItWaitsChangesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slow := &slowList{entered: make(chan struct{}, 1), release: make(chan struct{})}
		r := newRig(t, func(c *Config) { slow.Connector = c.Connector; c.Connector = slow })
		n := addNode(t, r.cfg.Store)
		sara := addSubscription(t, r.cfg.Store, "Sara")
		r.start()
		defer r.stop()
		<-slow.entered

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := r.m.SetEnabled(ctx, sara.ID, n.ID, true)
			done <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("the abandoned switch: %v", err)
		}
		if hasClient(r.cfg.Store, sara.ID, n.ID) {
			t.Error("an abandoned switch was saved")
		}
		close(slow.release)
		synctest.Wait()
		// and the lock is free again
		if _, err := r.m.SetEnabled(context.Background(), sara.ID, n.ID, true); err != nil {
			t.Errorf("after the reconcile: %v", err)
		}
	})
}

// Switches that come at once, on several nodes and subscriptions, leave each
// node with exactly the clients the state says are on, under the ids it says.
func TestSwitchesThatComeAtOnceLeaveTheNodesAsTheStateSays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		nodes := []model.Node{addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)}
		ctx := context.Background()
		r.start()
		defer r.stop()
		var subs []uint
		for i := 0; i < 3; i++ {
			s, err := r.m.CreateSubscription(ctx, fmt.Sprint("sub ", i), []uint{nodes[0].ID}, api.SubscriptionLimits{})
			must(t, err)
			subs = append(subs, s.ID)
		}

		done := make(chan struct{})
		const rounds = 12
		for w := 0; w < 6; w++ {
			go func() {
				defer func() { done <- struct{}{} }()
				for i := 0; i < rounds; i++ {
					sub, node := subs[(w+i)%len(subs)], nodes[(w*7+i)%len(nodes)].ID
					if _, err := r.m.SetEnabled(ctx, sub, node, (w+i)%3 != 0); err != nil {
						t.Errorf("switch: %v", err)
					}
					if i%4 == 0 {
						time.Sleep(time.Second)
					}
				}
			}()
		}
		for w := 0; w < 6; w++ {
			<-done
		}
		r.advance(time.Minute)

		st := r.cfg.Store.Read()
		for _, n := range nodes {
			ag := r.net.agent(n.ID)
			for _, s := range subs {
				got, has := holds(ag, subEmail(s))
				e := st.FindEnablement(s, n.ID)
				switch {
				case e != nil && e.Enabled && (!has || got != e.ClientUUID):
					t.Errorf("subscription %d is on at node %d and the node holds %q (%v), want %q", s, n.ID, got, has, e.ClientUUID)
				case (e == nil || !e.Enabled) && has:
					t.Errorf("subscription %d is off at node %d and the node holds a client", s, n.ID)
				}
			}
		}
	})
}
