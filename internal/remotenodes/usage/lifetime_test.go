// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"testing"
	"time"
)

func TestLifetimeAddsEveryNodeAndTheBucketStillGrowing(t *testing.T) {
	r := newRig(t)
	a, b := r.agent(1), r.agent(2)
	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 10, 0), email(1), 100, 900)
	b.add(when(12, 10, 0), email(1), 5, 50)
	b.add(when(12, 10, 0), email(2), 1, 1)
	r.poll(1)
	r.poll(2)
	// nothing is complete yet, but the limit has to see what is being used
	if got := r.c.SubscriptionLifetime(1); got != (Totals{Up: 105, Down: 950}) {
		t.Fatalf("lifetime of 1 reads %+v", got)
	}
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)
	r.poll(2)
	// the first hour is counted for good and the new one is still growing
	if got := r.c.SubscriptionLifetime(1); got != (Totals{Up: 112, Down: 1020}) {
		t.Fatalf("lifetime of 1 reads %+v", got)
	}
	if got := r.c.SubscriptionLifetime(2); got != (Totals{Up: 1, Down: 1}) {
		t.Fatalf("lifetime of 2 reads %+v", got)
	}
	if got := r.c.SubscriptionLifetime(3); got != (Totals{}) {
		t.Fatalf("a subscription nobody used reads %+v", got)
	}
}

func TestLifetimeOutlivesTheDaysAndARestart(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 10, 0), email(1), 100, 900)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)
	must(t, r.flush())

	// forty days on, the first day is out of the history, and the count is not
	r.clk.Set(when(13, 5, 0).Add(40 * 24 * time.Hour))
	a.add(r.clk.Now(), email(1), 3, 30)
	r.poll(1)
	must(t, r.flush())
	if got := r.c.SubscriptionTotal(1); got.MonthUp > 10 {
		t.Fatalf("the month still holds %+v", got)
	}
	r.reopen()
	if got := r.c.SubscriptionLifetime(1); got.Up < 100 || got.Down < 900 {
		t.Fatalf("lifetime after a restart reads %+v", got)
	}
}

func TestRemovingANodeKeepsWhatItsSubscriptionsMovedThroughIt(t *testing.T) {
	r := newRig(t)
	a := r.agent(1)
	r.clk.Set(when(12, 10, 0))
	a.add(when(12, 10, 0), email(1), 100, 900)
	r.poll(1)
	r.clk.Set(when(13, 5, 0))
	a.add(when(13, 5, 0), email(1), 7, 70)
	r.poll(1)
	r.agent(2).add(when(13, 5, 0), email(1), 1, 2)
	r.poll(2)
	before := r.c.SubscriptionLifetime(1)

	r.c.Forget(1)
	// the hour still growing on node 1 goes with it, the rest stays
	got := r.c.SubscriptionLifetime(1)
	if want := (Totals{Up: 100 + 1, Down: 900 + 2}); got != want {
		t.Fatalf("after removing node 1 lifetime reads %+v (it was %+v), want %+v", got, before, want)
	}
	must(t, r.flush())
	r.reopen()
	if again := r.c.SubscriptionLifetime(1); again.Up < 100 || again.Down < 900 {
		t.Fatalf("after a restart it reads %+v", again)
	}
	r.c.ForgetSubscription(1)
	if gone := r.c.SubscriptionLifetime(1); gone != (Totals{}) {
		t.Fatalf("a deleted subscription still reads %+v", gone)
	}
}

func TestAFileFromBeforeTotalsStartsFromTheDays(t *testing.T) {
	doc := `{"version":1,"nodes":[{"id":1,"acked_through":0,"bridge":{"up":0,"down":0},"days":{"2026-10-29":{"up":4,"down":40},"2026-10-30":{"up":6,"down":60}},` +
		`"subscriptions":[{"id":1,"days":{"2026-10-29":{"up":4,"down":40},"2026-10-30":{"up":6,"down":60}}}]}]}`
	nodes, retired, err := decodeDoc([]byte(doc))
	must(t, err)
	if got := nodes[1].life[1]; got != (Totals{Up: 10, Down: 100}) {
		t.Fatalf("the total starts at %+v", got)
	}
	if len(retired) != 0 {
		t.Fatalf("retired = %+v", retired)
	}
	b, err := encodeDoc(nodes, map[uint]Totals{7: {Up: 1, Down: 2}})
	must(t, err)
	_, retired, err = decodeDoc(b)
	must(t, err)
	if retired[7] != (Totals{Up: 1, Down: 2}) {
		t.Fatalf("retired reads %+v after a round trip", retired)
	}
}
