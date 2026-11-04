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

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

var lowerUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// holds is the client id a node's agent has for an email, if it has one.
func holds(a *fakeAgent, email string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.users[email]
	return id, ok
}

func subEmail(id uint) string { return nodecfg.SubscriptionEmail(int(id)) }

// idOnNode is what the state says a subscription's client id on a node is.
func idOnNode(t testing.TB, st Store, sub, node uint) model.Enablement {
	t.Helper()
	s := st.Read()
	e := s.FindEnablement(sub, node)
	if e == nil {
		t.Fatalf("the state has no client for subscription %d on node %d", sub, node)
	}
	return *e
}

// hasClient is whether the state has a record of the subscription's client on
// the node, on or off.
func hasClient(st Store, sub, node uint) bool {
	s := st.Read()
	return s.FindEnablement(sub, node) != nil
}

// clientIsOn is whether the state says the subscription is switched on there.
func clientIsOn(st Store, sub, node uint) bool {
	s := st.Read()
	e := s.FindEnablement(sub, node)
	return e != nil && e.Enabled
}

// usedUsage is the collector with a note of which subscriptions were forgotten.
type usedUsage struct {
	Usage
	mu        sync.Mutex
	forgotten []uint
}

func (u *usedUsage) ForgetSubscription(id uint) {
	u.mu.Lock()
	u.forgotten = append(u.forgotten, id)
	u.mu.Unlock()
	u.Usage.ForgetSubscription(id)
}

func (u *usedUsage) forgot() []uint {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.forgotten)
}

func TestCreatingASubscriptionSwitchesItOnAtTheNodesNamed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n1, n2, n3 := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)

		// a name with spaces round it is trimmed, and a node named twice counts once
		v, err := r.m.CreateSubscription(context.Background(), "  Sara ", []uint{n2.ID, n1.ID, n2.ID}, api.SubscriptionLimits{})
		must(t, err)
		if v.ID != 1 || v.Name != "Sara" || !v.CreatedAt.Equal(r.cfg.Now().UTC().Truncate(time.Second)) {
			t.Fatalf("view = %+v", v)
		}
		on := map[uint]bool{}
		for _, row := range v.Nodes {
			on[row.NodeID] = row.Enabled
		}
		if len(v.Nodes) != 3 || !on[n1.ID] || !on[n2.ID] || on[n3.ID] {
			t.Errorf("the view's nodes = %+v", v.Nodes)
		}

		// each node got a client of its own, and it is the one the state keeps
		st := r.cfg.Store.Read()
		if len(st.Subscriptions) != 1 || len(st.Enablements) != 2 {
			t.Fatalf("the state holds %d subscriptions and %d clients", len(st.Subscriptions), len(st.Enablements))
		}
		ids := map[string]bool{}
		for _, n := range []model.Node{n1, n2} {
			e := idOnNode(t, r.cfg.Store, 1, n.ID)
			if !e.Enabled || !lowerUUID.MatchString(e.ClientUUID) {
				t.Errorf("client on node %d = %+v", n.ID, e)
			}
			ids[e.ClientUUID] = true
			if got, ok := holds(r.net.agent(n.ID), subEmail(1)); !ok || got != e.ClientUUID {
				t.Errorf("node %d's agent holds %q, the state says %q", n.ID, got, e.ClientUUID)
			}
			if got := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventSubscriptionOn}) {
				t.Errorf("node %d's events = %v", n.ID, got)
			}
			if ev := r.m.NodeEvents(n.ID)[0]; ev.Detail != "Sara" {
				t.Errorf("node %d's event says %q", n.ID, ev.Detail)
			}
		}
		if len(ids) != 2 {
			t.Error("two nodes share one client id")
		}
		if _, ok := holds(r.net.agent(n3.ID), subEmail(1)); ok || len(r.m.NodeEvents(n3.ID)) != 0 {
			t.Error("a node that was not named was touched")
		}
	})
}

func TestASubscriptionWithNoNodesIsOffEverywhere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		v, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{}, api.SubscriptionLimits{})
		must(t, err)
		if len(v.Nodes) != 1 || v.Nodes[0].Enabled || len(r.cfg.Store.Read().Enablements) != 0 {
			t.Errorf("view = %+v", v)
		}
		if r.net.agent(n.ID).count("AddUser") != 0 {
			t.Error("a node was told of a subscription that is on nowhere")
		}
	})
}

// A node the router can't reach keeps the choice, and the loop that
// reconciles it on the way back applies it.
func TestAClientForANodeThatCantBeReachedComesWhenItIsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n1, n2 := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
		ag2 := r.net.agent(n2.ID)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)
		ag2.setDown(true)
		r.advance(time.Minute)
		if p, _ := r.m.peek(n2.ID); !p.unreachable || p.connected {
			t.Fatalf("node 2 = %+v", p)
		}

		v, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{n1.ID, n2.ID}, api.SubscriptionLimits{})
		must(t, err)
		if !v.Nodes[1].Enabled {
			t.Errorf("the view says node 2 is off: %+v", v.Nodes)
		}
		if _, ok := holds(r.net.agent(n1.ID), subEmail(1)); !ok {
			t.Error("the node that answers has no client")
		}
		want := idOnNode(t, r.cfg.Store, 1, n2.ID)
		if !want.Enabled {
			t.Fatal("the state does not keep the wish")
		}
		if kinds := kinds(r.m.NodeEvents(n2.ID)); !slices.Contains(kinds, EventSubscriptionOn) {
			t.Errorf("no event for the node that can't be reached: %v", kinds)
		}

		ag2.setDown(false)
		r.advance(time.Minute)
		if got, ok := holds(ag2, subEmail(1)); !ok || got != want.ClientUUID {
			t.Errorf("after it came back the agent holds %q (%v), want %q", got, ok, want.ClientUUID)
		}
	})
}

// The agent drops while the manager still holds the connection, so the send
// fails. The call still stands, the failure is logged without the client id,
// and the node is brought into line when it connects again.
func TestASendThatFailsIsLoggedAndTheNodeIsReconnected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		r.advance(2 * time.Second)
		made, _ := r.net.opened(n.ID)

		ag.setDown(true)
		if _, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{}); err != nil {
			t.Fatalf("a failed send failed the call: %v", err)
		}
		if r.log.count("did not go through", "node=1") != 1 {
			t.Errorf("the failure was not logged once:\n%s", r.log)
		}
		want := idOnNode(t, r.cfg.Store, 1, n.ID).ClientUUID
		if strings.Contains(r.log.String(), want) {
			t.Errorf("the log has the client id in it:\n%s", r.log)
		}

		ag.setDown(false)
		r.advance(time.Minute)
		if got, ok := holds(ag, subEmail(1)); !ok || got != want {
			t.Errorf("the agent holds %q, want %q", got, want)
		}
		if after, _ := r.net.opened(n.ID); after <= made {
			t.Error("the node was not asked to reconnect")
		}
	})
}

func TestCreatingASubscriptionIsRefusedWithTheRightErrors(t *testing.T) {
	ctx := context.Background()
	install := jobs.Job{ID: "job-7", Kind: jobs.KindInstall, State: jobs.StateRunning, Steps: []jobs.Step{{Name: "upload", State: jobs.StepRunning}}}

	t.Run("names", func(t *testing.T) {
		cfg, _ := baseConfig(t)
		addNode(t, cfg.Store)
		m, err := New(cfg)
		must(t, err)
		for name, bad := range map[string]string{
			"empty": "", "blank": " \t ", "a line break": "Sa\nra", "too long": strings.Repeat("x", model.MaxNameRunes+1),
			"a control character": "Sa\x07ra", "not UTF-8": "Sa\xffra",
		} {
			var inv *model.InvalidError
			if _, err := m.CreateSubscription(ctx, bad, nil, api.SubscriptionLimits{}); !errors.As(err, &inv) {
				t.Errorf("%s: %v, want an invalid name", name, err)
			}
		}
		if len(cfg.Store.Read().Subscriptions) != 0 {
			t.Error("a refused name left a subscription behind")
		}
	})

	t.Run("a name that is taken, whatever its case", func(t *testing.T) {
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		_, err = m.CreateSubscription(ctx, "Émile", nil, api.SubscriptionLimits{})
		must(t, err)
		for _, again := range []string{"Émile", "émile", "ÉMILE", " émile "} {
			if _, err := m.CreateSubscription(ctx, again, nil, api.SubscriptionLimits{}); !errors.Is(err, model.ErrNameTaken) {
				t.Errorf("%q: %v, want the name taken", again, err)
			}
		}
		// Persian has no case and still counts as one name
		_, err = m.CreateSubscription(ctx, "علی", nil, api.SubscriptionLimits{})
		must(t, err)
		if _, err := m.CreateSubscription(ctx, "علی", nil, api.SubscriptionLimits{}); !errors.Is(err, model.ErrNameTaken) {
			t.Errorf("a Persian name twice: %v", err)
		}
		if got := len(cfg.Store.Read().Subscriptions); got != 2 {
			t.Errorf("%d subscriptions", got)
		}
	})

	t.Run("the limit", func(t *testing.T) {
		cfg, _ := baseConfig(t)
		n := addNode(t, cfg.Store)
		m, err := New(cfg)
		must(t, err)
		for i := 0; i < model.MaxSubscriptions; i++ {
			_, err := m.CreateSubscription(ctx, fmt.Sprint("sub ", i), []uint{n.ID}, api.SubscriptionLimits{})
			must(t, err)
		}
		before := cfg.Store.Read()
		if _, err := m.CreateSubscription(ctx, "one too many", []uint{n.ID}, api.SubscriptionLimits{}); !errors.Is(err, model.ErrSubscriptionLimit) {
			t.Fatalf("the eleventh: %v", err)
		}
		after := cfg.Store.Read()
		if len(after.Subscriptions) != len(before.Subscriptions) || len(after.Enablements) != len(before.Enablements) || after.NextSubscriptionID != before.NextSubscriptionID {
			t.Error("a refused subscription changed the state")
		}
	})

	t.Run("nodes", func(t *testing.T) {
		cfg, _ := baseConfig(t)
		good := addNode(t, cfg.Store)
		dead := addNode(t, cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		running := addNode(t, cfg.Store)
		cfg.Jobs = fakeJobs{running.ID: install}
		m, err := New(cfg)
		must(t, err)

		for name, c := range map[string]struct {
			ids  []uint
			want error
		}{
			"a node nobody has":                   {[]uint{good.ID, 9}, model.ErrNotFound},
			"node 0":                              {[]uint{0}, model.ErrNotFound},
			"a node whose install died":           {[]uint{dead.ID}, model.ErrNotFound},
			"a node being installed":              {[]uint{good.ID, running.ID}, jobs.ErrBusy},
			"a node being installed, named first": {[]uint{running.ID, good.ID}, jobs.ErrBusy},
		} {
			_, err := m.CreateSubscription(ctx, "Sara", c.ids, api.SubscriptionLimits{})
			if !errors.Is(err, c.want) {
				t.Errorf("%s: %v, want %v", name, err, c.want)
			}
		}
		var busy *jobs.BusyError
		if _, err := m.CreateSubscription(ctx, "Sara", []uint{running.ID}, api.SubscriptionLimits{}); !errors.As(err, &busy) || busy.JobID != "job-7" || busy.NodeID != running.ID {
			t.Errorf("the busy error doesn't name the job: %v", err)
		}
		if st := cfg.Store.Read(); len(st.Subscriptions) != 0 || len(st.Enablements) != 0 || st.NextSubscriptionID != 1 {
			t.Errorf("a refused call left something behind: %+v", st)
		}
	})
}

func TestRenamingKeepsTheLinksAndTheNodesAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		a, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		_, err = r.m.CreateSubscription(ctx, "Ali", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		was := idOnNode(t, r.cfg.Store, a.ID, n.ID).ClientUUID
		ag := r.net.agent(n.ID)
		calls, events := ag.callCount(), len(r.m.NodeEvents(n.ID))

		v, err := r.m.RenameSubscription(ctx, a.ID, "  سارا ")
		must(t, err)
		if v.Name != "سارا" || v.ID != a.ID {
			t.Errorf("view = %+v", v)
		}
		if got := idOnNode(t, r.cfg.Store, a.ID, n.ID).ClientUUID; got != was {
			t.Error("renaming changed the client id")
		}
		if ag.callCount() != calls || len(r.m.NodeEvents(n.ID)) != events {
			t.Errorf("a rename reached the node: %v", ag.callsFrom(calls))
		}

		// its own name in another case is fine, another subscription's is not
		if _, err := r.m.RenameSubscription(ctx, a.ID, "سارا"); err != nil {
			t.Errorf("renaming to the same name: %v", err)
		}
		if _, err := r.m.RenameSubscription(ctx, a.ID, "ALI"); !errors.Is(err, model.ErrNameTaken) {
			t.Errorf("a name another subscription has: %v", err)
		}
		if got := r.cfg.Store.Read().Subscriptions[0].Name; got != "سارا" {
			t.Errorf("a refused rename changed the name to %q", got)
		}
		self, err := r.m.RenameSubscription(ctx, a.ID, "SARA")
		must(t, err)
		if self.Name != "SARA" {
			t.Errorf("the case of its own name can't be changed: %q", self.Name)
		}

		var inv *model.InvalidError
		if _, err := r.m.RenameSubscription(ctx, a.ID, " "); !errors.As(err, &inv) {
			t.Errorf("an empty name: %v", err)
		}
		if _, err := r.m.RenameSubscription(ctx, 99, "x"); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a subscription nobody has: %v", err)
		}
	})
}

func TestDeletingASubscriptionTakesItOffEveryNodeAndForgetsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		used := &usedUsage{}
		r := newRig(t, func(c *Config) { used.Usage = c.Usage; c.Usage = used })
		n1, n2, n3 := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n1.ID, n2.ID}, api.SubscriptionLimits{})
		must(t, err)
		ali, err := r.m.CreateSubscription(ctx, "Ali", []uint{n1.ID}, api.SubscriptionLimits{})
		must(t, err)

		// switched off at node 2, and the client is still there: an off that
		// didn't reach it once
		enable(t, r.cfg.Store, sara.ID, n2.ID, false)
		ag2 := r.net.agent(n2.ID)
		ag2.set(func(a *fakeAgent) { a.users[subEmail(sara.ID)] = "11111111-1111-4111-8111-111111111111" })

		must(t, r.m.DeleteSubscription(ctx, sara.ID))
		st := r.cfg.Store.Read()
		if st.FindSubscription(sara.ID) != nil || len(st.Subscriptions) != 1 || st.Subscriptions[0].ID != ali.ID {
			t.Errorf("subscriptions left: %+v", st.Subscriptions)
		}
		for _, e := range st.Enablements {
			if e.SubscriptionID == sara.ID {
				t.Errorf("a client of the deleted subscription is still in the state: %+v", e)
			}
		}
		for _, n := range []model.Node{n1, n2, n3} {
			if _, ok := holds(r.net.agent(n.ID), subEmail(sara.ID)); ok {
				t.Errorf("node %d still holds the client", n.ID)
			}
		}
		if _, ok := holds(r.net.agent(n1.ID), subEmail(ali.ID)); !ok {
			t.Error("another subscription's client went with it")
		}
		if got := used.forgot(); !slices.Equal(got, []uint{sara.ID}) {
			t.Errorf("forgotten: %v", got)
		}
		// it was on at node 1 only when it went, so only node 1 says so
		if got := kinds(r.m.NodeEvents(n1.ID)); got[len(got)-1] != EventSubscriptionOff {
			t.Errorf("node 1's events = %v", got)
		}
		if got := kinds(r.m.NodeEvents(n3.ID)); len(got) != 0 {
			t.Errorf("node 3's events = %v", got)
		}
		// and node 2, where it was off already, has nothing to say of it
		if got := kinds(r.m.NodeEvents(n2.ID)); !slices.Equal(got, []string{EventSubscriptionOn}) {
			t.Errorf("node 2's events = %v", got)
		}

		if err := r.m.DeleteSubscription(ctx, sara.ID); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("deleting it again: %v", err)
		}
		// ids are not handed out again
		again, err := r.m.CreateSubscription(ctx, "Sara", []uint{n1.ID}, api.SubscriptionLimits{})
		must(t, err)
		if again.ID == sara.ID {
			t.Error("a deleted subscription's id came back")
		}
	})
}

// A node that is away when a subscription is deleted still has its client. The
// reconcile on the way back finds nobody wants it and takes it off.
func TestADeletedSubscriptionIsTakenOffANodeThatWasAway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sara, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		if _, ok := holds(ag, subEmail(sara.ID)); !ok {
			t.Fatal("the node has no client to begin with")
		}

		ag.setDown(true)
		r.advance(time.Minute)
		must(t, r.m.DeleteSubscription(ctx, sara.ID))
		if _, ok := holds(ag, subEmail(sara.ID)); !ok {
			t.Fatal("the client left a node that was down")
		}
		if len(r.cfg.Store.Read().Subscriptions) != 0 {
			t.Error("the state still has the subscription")
		}

		ag.setDown(false)
		r.advance(time.Minute)
		if _, ok := holds(ag, subEmail(sara.ID)); ok {
			t.Error("the client is still on the node after it was back")
		}
	})
}

func TestTheViewsTheCallsReturnAreTheOnesTheListShows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		v, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		list, _ := api.ViewSubscription(r.cfg.Store.Read(), v.ID, r.m)
		got, err := json.Marshal(v)
		must(t, err)
		want, err := json.Marshal(list)
		must(t, err)
		if string(got) != string(want) {
			t.Errorf("the call returned %s, the list shows %s", got, want)
		}
	})
}
