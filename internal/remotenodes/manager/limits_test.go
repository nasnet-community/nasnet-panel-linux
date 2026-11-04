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

// meter is the collector with what a subscription has moved set by the test,
// and, for the subscriptions the test names, what a node says of it.
type meter struct {
	Usage
	mu    sync.Mutex
	moved map[uint]usage.Totals
	on    map[[2]uint]usage.SubUsage
}

func (u *meter) Subscription(sub, node uint) usage.SubUsage {
	u.mu.Lock()
	v, ok := u.on[[2]uint{sub, node}]
	u.mu.Unlock()
	if ok {
		return v
	}
	return u.Usage.Subscription(sub, node)
}

// seenOn says what the node reports of the subscription: whether it is
// connected, and the bytes of its last thirty days.
func (u *meter) seenOn(sub, node uint, online bool, bytes uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.on == nil {
		u.on = map[[2]uint]usage.SubUsage{}
	}
	u.on[[2]uint{sub, node}] = usage.SubUsage{Live: true, Online: online, Connections: map[bool]int{true: 1}[online], MonthDown: bytes}
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

func TestABackupTakesWhatEachSubscriptionUsedWithIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		a, mtA, n := limitsRig(t)
		defer a.stop()
		near := a.subscribe(t, n, "Sara", api.SubscriptionLimits{TrafficLimit: 1000})
		over := a.subscribe(t, n, "Ali", api.SubscriptionLimits{TrafficLimit: 1000})
		// Sara was reset once at 300 and has used 600 since; Ali is past his limit
		mtA.set(near, 300, 0)
		_, err := a.m.ResetUsage(ctx, near)
		must(t, err)
		mtA.set(near, 500, 400)
		mtA.set(over, 900, 300)
		a.advance(30 * time.Second)
		if a.stopped(over) != model.StopLimit || a.stopped(near) != "" {
			t.Fatalf("before the backup: Sara %q, Ali %q", a.stopped(near), a.stopped(over))
		}
		_, file, err := a.m.ExportBackup(ctx, backupPass)
		must(t, err)
		// the live state keeps its own numbers: only the file carries them
		if st := a.cfg.Store.Read(); st.FindSubscription(near).TrafficCarried != 0 || st.FindSubscription(near).TrafficBase != 300 {
			t.Errorf("the export changed the live state: %+v", *st.FindSubscription(near))
		}

		// a fresh install, whose counters start at zero
		mtB := &meter{moved: map[uint]usage.Totals{}}
		b := newRig(t, func(c *Config) { mtB.Usage = c.Usage; c.Usage = mtB })
		_, err = b.m.ImportBackup(ctx, backupPass, file)
		must(t, err)
		b.start()
		defer b.stop()
		b.advance(30 * time.Second)

		v, err := b.m.subscriptionView(near)
		must(t, err)
		if v.TrafficUsed != 600 || v.Stopped != "" {
			t.Errorf("Sara after the restore: used %d (want 600), stopped %q", v.TrafficUsed, v.Stopped)
		}
		if v, _ := b.m.subscriptionView(over); v.TrafficUsed != 1200 || v.Stopped != model.StopLimit {
			t.Errorf("Ali after the restore: used %d (want 1200), stopped %q", v.TrafficUsed, v.Stopped)
		}
		// what it moves here adds to what it brought, and the limit is met where it would have been
		mtB.set(near, 300, 50) // 600 brought + 350 here = 950 of 1000
		b.advance(30 * time.Second)
		if b.stopped(near) != "" {
			t.Errorf("Sara at 950 of 1000 is %q", b.stopped(near))
		}
		mtB.set(near, 300, 150) // 600 + 450 = 1050
		b.advance(30 * time.Second)
		if b.stopped(near) != model.StopLimit {
			t.Errorf("Sara at 1050 of 1000 is %q, want stopped for her limit", b.stopped(near))
		}
		// a reset counts from the restored total too
		v, err = b.m.ResetUsage(ctx, near)
		must(t, err)
		if v.TrafficUsed != 0 || v.Stopped != "" {
			t.Errorf("after a reset: %+v", v)
		}
		mtB.set(near, 300, 160)
		if v, _ := b.m.subscriptionView(near); v.TrafficUsed != 10 {
			t.Errorf("10 bytes after the reset read as %d", v.TrafficUsed)
		}
	})
}

// a manager with operations, a node with real keys, two subscriptions on it, and a meter
func cutRig(t *testing.T) (*setUp, *meter) {
	t.Helper()
	mt := &meter{moved: map[uint]usage.Totals{}}
	s := &setUp{keys: realKeys(t)}
	s.opsRig = newOpsRig(t, func(c *Config) {
		s.disk = newScripted(c.Store)
		c.Store = s.disk
		mt.Usage = c.Usage
		c.Usage = mt
	})
	s.node = addNode(t, s.cfg.Store, withKeys(s.keys))
	ali, sara := addSubscription(t, s.cfg.Store, "ali"), addSubscription(t, s.cfg.Store, "sara")
	enable(t, s.cfg.Store, ali.ID, s.node.ID, true)
	enable(t, s.cfg.Store, sara.ID, s.node.ID, true)
	return s, mt
}

func TestAStoppedSubscriptionsOpenConnectionsAreEndedByRestartingTheNodesXray(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, mt := cutRig(t)
		s.start()
		defer s.stop()
		s.advance(2 * time.Second)
		ctx := context.Background()
		_, err := s.m.UpdateSubscription(ctx, 1, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Value: 1000}})
		must(t, err)
		pushes := s.ops.called("PushConfig")
		agent := s.net.agent(s.node.ID)
		restarts := agent.count("RestartXray")

		// ali goes over while a connection of his is open on the node
		mt.set(1, 600, 600)
		mt.seenOn(1, s.node.ID, true, 1200)
		s.advance(15 * time.Second)
		if s.stopped(1) != model.StopLimit {
			t.Fatalf("stopped = %q", s.stopped(1))
		}
		if got := s.ops.called("PushConfig") - pushes; got != 1 {
			t.Fatalf("%d pushes to end the open connection, want 1", got)
		}
		// the push alone may be applied without a restart: the restart is asked for
		if got := agent.count("RestartXray validate=true") - restarts; got != 1 {
			t.Fatalf("%d restarts to end the open connection, want 1", got)
		}
		// the config the restart runs has sara and not ali
		s.ops.mu.Lock()
		last := s.ops.pushes[len(s.ops.pushes)-1].in
		s.ops.mu.Unlock()
		if len(last.Clients) != 1 || last.Clients[0].Email != subEmail(2) {
			t.Errorf("the pushed config has the clients %+v", last.Clients)
		}
		if got := kinds(s.m.NodeEvents(s.node.ID)); !slices.Contains(got, EventConnectionsCut) {
			t.Errorf("events = %v", got)
		}
		if ev := s.m.NodeEvents(s.node.ID)[0]; ev.Kind != EventConnectionsCut || ev.Detail != "ali" {
			t.Errorf("the newest event is %+v", ev)
		}

		// still connected: not again within a minute, and again after it
		s.advance(40 * time.Second)
		if got := s.ops.called("PushConfig") - pushes; got != 1 {
			t.Fatalf("%d pushes within a minute, want 1", got)
		}
		s.advance(30 * time.Second)
		if got := s.ops.called("PushConfig") - pushes; got != 2 {
			t.Fatalf("%d pushes after a minute, want 2", got)
		}
		if got := agent.count("RestartXray") - restarts; got != 2 {
			t.Fatalf("%d restarts after a minute, want 2", got)
		}

		// gone quiet: nothing more
		mt.seenOn(1, s.node.ID, false, 1200)
		s.advance(3 * time.Minute)
		if got := s.ops.called("PushConfig") - pushes; got != 2 {
			t.Fatalf("%d pushes once it was quiet, want 2", got)
		}
		// not connected as far as the node says, but its bytes still grow: that is a connection too
		mt.seenOn(1, s.node.ID, false, 5000)
		s.advance(15 * time.Second)
		if got := s.ops.called("PushConfig") - pushes; got != 3 {
			t.Fatalf("%d pushes after its bytes grew, want 3", got)
		}

		// a subscription that runs again is no reason for a restart, connected or not
		_, err = s.m.ResetUsage(ctx, 1)
		must(t, err)
		mt.seenOn(1, s.node.ID, true, 9000)
		before := s.ops.called("PushConfig")
		s.advance(3 * time.Minute)
		if got := s.ops.called("PushConfig") - before; got != 0 {
			t.Errorf("%d pushes for a running subscription", got)
		}
	})
}

func TestAQuietStoppedSubscriptionRestartsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, mt := cutRig(t)
		s.start()
		defer s.stop()
		s.advance(2 * time.Second)
		_, err := s.m.UpdateSubscription(context.Background(), 1, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Value: 1000}})
		must(t, err)
		pushes, restarts := s.ops.called("PushConfig"), s.net.agent(s.node.ID).count("RestartXray")
		mt.set(1, 600, 600)
		mt.seenOn(1, s.node.ID, false, 1200)
		s.advance(5 * time.Minute)
		if s.stopped(1) != model.StopLimit {
			t.Fatalf("stopped = %q", s.stopped(1))
		}
		if got := s.ops.called("PushConfig") - pushes; got != 0 {
			t.Errorf("%d pushes for a stopped subscription with nothing open", got)
		}
		if got := s.net.agent(s.node.ID).count("RestartXray") - restarts; got != 0 {
			t.Errorf("%d restarts for a stopped subscription with nothing open", got)
		}
	})
}
