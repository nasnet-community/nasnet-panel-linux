// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"testing"
)

// The calls that change a subscription answer with the view the list shows,
// so the page can put it where the old one was.
func TestViewSubscriptionIsTheEntryTheListHas(t *testing.T) {
	r := newRig(t, "")
	r.populate()
	st := r.st.Read()
	live := newFakeLive()
	live.on[[2]uint{1, 1}] = SubscriptionUsage{Live: true, Online: true, Connections: 2, LastSeenAt: at(-1), TodayUp: 5, MonthDown: 9}
	live.total[1] = SubscriptionUsage{Live: true, Online: true, LastSeenAt: at(-1), TodayUp: 5, MonthDown: 9}

	for _, l := range []Live{nil, live} {
		list := subscriptionViews(st, l)
		for _, want := range list {
			got, ok := ViewSubscription(st, want.ID, l)
			if !ok {
				t.Fatalf("subscription %d is in the list and not found", want.ID)
			}
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				t.Errorf("subscription %d: %s, the list has %s", want.ID, a, b)
			}
		}
	}
	if _, ok := ViewSubscription(st, 99, live); ok {
		t.Error("a subscription nobody has was found")
	}
	if got, _ := ViewSubscription(st, 1, live); got.Online == nil || !*got.Online || got.Nodes[0].Connections == nil || *got.Nodes[0].Connections != 2 {
		t.Errorf("the live parts are not merged in: %+v", got)
	}
}
