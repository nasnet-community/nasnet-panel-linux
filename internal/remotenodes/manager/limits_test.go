// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
)

// meter is the collector with what a subscription has moved set by the test.
type meter struct {
	Usage
	mu    sync.Mutex
	moved map[uint]usage.Totals
}

func (u *meter) SubscriptionLifetime(id uint) usage.Totals {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.moved[id]
}

// set says the subscription has moved this much in all.
func (u *meter) set(id uint, up, down uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.moved[id] = usage.Totals{Up: up, Down: down}
}

// limitsRig is a manager with one node, started, and a meter.
func limitsRig(t *testing.T) (*rig, *meter, model.Node) {
	t.Helper()
	mt := &meter{moved: map[uint]usage.Totals{}}
	r := newRig(t, func(c *Config) { mt.Usage = c.Usage; c.Usage = mt })
	n := addNode(t, r.cfg.Store)
	r.start()
	r.advance(2 * time.Second)
	return r, mt, n
}

func (r *rig) subscribe(t *testing.T, n model.Node, name string, l api.SubscriptionLimits) uint {
	t.Helper()
	v, err := r.m.CreateSubscription(context.Background(), name, []uint{n.ID}, l)
	must(t, err)
	return v.ID
}

func (r *rig) stopped(id uint) string {
	st := r.cfg.Store.Read()
	return st.FindSubscription(id).Stopped
}

func TestAnExpiredSubscriptionIsTakenOffItsNodes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, _, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{ExpiresAt: r.cfg.Now().Add(time.Hour)})
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); !ok {
			t.Fatal("the node doesn't hold the client of a subscription that hasn't expired")
		}

		r.advance(59 * time.Minute)
		if r.stopped(id) != "" {
			t.Fatalf("stopped (%q) before its date", r.stopped(id))
		}
		r.advance(2 * time.Minute)
		if got := r.stopped(id); got != model.StopExpired {
			t.Fatalf("stopped = %q after its date", got)
		}
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); ok {
			t.Error("the node still holds the client of an expired subscription")
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Contains(got, EventSubscriptionExpired) {
			t.Errorf("events = %v", got)
		}
		// the switch is untouched and the link is still there to see
		if !clientIsOn(r.cfg.Store, id, n.ID) {
			t.Error("the switch on the node was turned off")
		}
		if _, err := r.m.Link(id, n.ID); err != nil {
			t.Errorf("the link of an expired subscription: %v", err)
		}
		v, err := r.m.subscriptionView(id)
		must(t, err)
		if v.Stopped != model.StopExpired || v.ExpiresAt == nil {
			t.Errorf("view = %+v", v)
		}
	})
}

func TestADateInThePastIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, _, n := limitsRig(t)
		defer r.stop()
		var inv *model.InvalidError
		_, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{ExpiresAt: r.cfg.Now().Add(-time.Minute)})
		if !errors.As(err, &inv) {
			t.Fatalf("create with a past date: %v", err)
		}
		if _, err := r.m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{TrafficLimit: model.MaxTrafficLimit + 1}); !errors.As(err, &inv) {
			t.Fatalf("create with a huge limit: %v", err)
		}
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{})
		past := r.cfg.Now().Add(-time.Hour)
		if _, err := r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{ExpiresAt: api.Opt[time.Time]{Set: true, Value: past}}); !errors.As(err, &inv) {
			t.Fatalf("update to a past date: %v", err)
		}
		zero := api.Opt[uint64]{Set: true, Value: 0}
		if _, err := r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{TrafficLimit: zero}); !errors.As(err, &inv) {
			t.Fatalf("update to a limit of zero: %v", err)
		}
		if st := r.cfg.Store.Read(); len(st.Subscriptions) != 1 || !st.Subscriptions[0].ExpiresAt.IsZero() {
			t.Errorf("a refused change left %+v", st.Subscriptions)
		}
	})
}

func TestReachingTheLimitStopsItAndAHigherOneStartsItAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, mt, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{TrafficLimit: 1000})
		before := idOnNode(t, r.cfg.Store, id, n.ID).ClientUUID

		mt.set(id, 100, 800) // 900 of 1000
		r.advance(30 * time.Second)
		if r.stopped(id) != "" {
			t.Fatalf("stopped (%q) under its limit", r.stopped(id))
		}
		mt.set(id, 100, 900) // 1000 of 1000
		r.advance(30 * time.Second)
		if got := r.stopped(id); got != model.StopLimit {
			t.Fatalf("stopped = %q at its limit", got)
		}
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); ok {
			t.Error("the node still holds the client of a subscription at its limit")
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Contains(got, EventSubscriptionLimit) {
			t.Errorf("events = %v", got)
		}
		v, err := r.m.subscriptionView(id)
		must(t, err)
		if v.TrafficUsed != 1000 || v.TrafficLimit != 1000 || v.Stopped != model.StopLimit {
			t.Errorf("view = %+v", v)
		}

		// a higher limit puts the same client back
		_, err = r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Value: 5000}})
		must(t, err)
		if r.stopped(id) != "" {
			t.Fatalf("still stopped (%q) after the limit went up", r.stopped(id))
		}
		if got, ok := holds(r.net.agent(n.ID), subEmail(id)); !ok || got != before {
			t.Errorf("the node holds %q (%v), want the old client %q", got, ok, before)
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Contains(got, EventSubscriptionResumed) {
			t.Errorf("events = %v", got)
		}

		// and no limit at all is null
		mt.set(id, 1<<30, 1<<30)
		_, err = r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Null: true}})
		must(t, err)
		r.advance(time.Minute)
		if r.stopped(id) != "" {
			t.Errorf("stopped (%q) with no limit", r.stopped(id))
		}
	})
}

func TestALimitBelowWhatWasUsedStopsItAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, mt, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{})
		mt.set(id, 500, 500)
		v, err := r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Value: 800}})
		must(t, err)
		if v.Stopped != model.StopLimit {
			t.Fatalf("view = %+v", v)
		}
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); ok {
			t.Error("the node still holds the client")
		}
	})
}

func TestResettingTheCountStartsItAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, mt, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{TrafficLimit: 1000})
		mt.set(id, 600, 600)
		r.advance(30 * time.Second)
		if r.stopped(id) != model.StopLimit {
			t.Fatalf("stopped = %q", r.stopped(id))
		}

		v, err := r.m.ResetUsage(context.Background(), id)
		must(t, err)
		if v.Stopped != "" || v.TrafficUsed != 0 {
			t.Fatalf("after the reset: %+v", v)
		}
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); !ok {
			t.Error("the client wasn't put back")
		}
		// it counts from the reset, and what it moves next is what counts
		mt.set(id, 700, 700) // 200 since
		r.advance(30 * time.Second)
		if r.stopped(id) != "" {
			t.Fatalf("stopped (%q) with 200 of 1000 used", r.stopped(id))
		}
		if v, _ = r.m.subscriptionView(id); v.TrafficUsed != 200 {
			t.Errorf("used = %d, want 200", v.TrafficUsed)
		}
		mt.set(id, 1100, 1100) // 1000 since
		r.advance(30 * time.Second)
		if r.stopped(id) != model.StopLimit {
			t.Errorf("stopped = %q at the limit again", r.stopped(id))
		}
	})
}

func TestAnExpiredSubscriptionStaysStoppedWhenTheCountIsReset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, _, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{ExpiresAt: r.cfg.Now().Add(time.Minute)})
		r.advance(2 * time.Minute)
		if r.stopped(id) != model.StopExpired {
			t.Fatalf("stopped = %q", r.stopped(id))
		}
		v, err := r.m.ResetUsage(context.Background(), id)
		must(t, err)
		if v.Stopped != model.StopExpired {
			t.Errorf("the reset started an expired subscription: %+v", v)
		}
		// moving the date on is what starts it
		later := r.cfg.Now().Add(24 * time.Hour)
		v, err = r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{ExpiresAt: api.Opt[time.Time]{Set: true, Value: later}})
		must(t, err)
		if v.Stopped != "" {
			t.Errorf("a later date left it %q", v.Stopped)
		}
		if _, ok := holds(r.net.agent(n.ID), subEmail(id)); !ok {
			t.Error("the client wasn't put back")
		}
	})
}

func TestAStoppedSubscriptionStaysOffTheNodes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, mt, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{TrafficLimit: 1000})
		mt.set(id, 1000, 0)
		r.advance(30 * time.Second)
		agent := r.net.agent(n.ID)
		if _, ok := holds(agent, subEmail(id)); ok {
			t.Fatal("the client is still on the node")
		}

		// the hub forgetting the bytes (a restart before the first poll) doesn't let it through
		mt.set(id, 0, 0)
		r.advance(time.Minute)
		if r.stopped(id) != model.StopLimit {
			t.Fatalf("stopped = %q after the count dipped", r.stopped(id))
		}
		// a rename is no reason to look again
		name := "Sara B"
		if _, err := r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{Name: &name}); err != nil {
			t.Fatal(err)
		}
		if r.stopped(id) != model.StopLimit {
			t.Fatalf("a rename started it: %q", r.stopped(id))
		}

		// the node's own reconcile reads the same state and leaves it off
		r.m.Reconnect(n.ID)
		r.advance(time.Minute)
		if _, ok := holds(agent, subEmail(id)); ok {
			t.Error("a reconcile put the client of a stopped subscription back")
		}

		// switching it on again, or giving it a new link, doesn't start it
		if _, err := r.m.SetEnabled(context.Background(), id, n.ID, false); err != nil {
			t.Fatal(err)
		}
		if _, err := r.m.SetEnabled(context.Background(), id, n.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, ok := holds(agent, subEmail(id)); ok {
			t.Error("switching it on put the client of a stopped subscription on the node")
		}
		was := idOnNode(t, r.cfg.Store, id, n.ID).ClientUUID
		if _, err := r.m.ReplaceLink(context.Background(), id, n.ID); err != nil {
			t.Fatal(err)
		}
		now := idOnNode(t, r.cfg.Store, id, n.ID).ClientUUID
		if now == was {
			t.Error("the link was not replaced")
		}
		if _, ok := holds(agent, subEmail(id)); ok {
			t.Error("replacing the link put the client of a stopped subscription on the node")
		}

		// and what a push of the whole config carries leaves it out too
		st := r.cfg.Store.Read()
		for _, c := range clientsOf(st, n.ID) {
			if c.Email == subEmail(id) {
				t.Error("the node's config would carry a stopped subscription")
			}
		}
		// lifting the reason puts the client on with the id the link has now
		if _, err := r.m.ResetUsage(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if got, ok := holds(agent, subEmail(id)); !ok || got != now {
			t.Errorf("the node holds %q (%v), want %q", got, ok, now)
		}
	})
}

func TestADeletedSubscriptionNeedsNoLimitCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, mt, n := limitsRig(t)
		defer r.stop()
		id := r.subscribe(t, n, "Sara", api.SubscriptionLimits{TrafficLimit: 10})
		must(t, r.m.DeleteSubscription(context.Background(), id))
		mt.set(id, 100, 100)
		r.advance(time.Minute) // the check runs with nothing to find
		if st := r.cfg.Store.Read(); len(st.Subscriptions) != 0 {
			t.Errorf("the state holds %+v", st.Subscriptions)
		}
	})
}
