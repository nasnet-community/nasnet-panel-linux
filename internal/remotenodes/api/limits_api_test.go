// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const noDate = "0001-01-01T00:00:00Z"

func TestALimitAndADateReachTheManagerAsTheyWereSent(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	var want []string
	for _, c := range []struct{ method, path, body, call string }{
		{"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"trafficLimit":5368709120,"expiresAt":"2027-01-02T03:04:05Z"}`,
			`create "Sara" [] limit=5368709120 expires=true`},
		{"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"trafficLimit":null,"expiresAt":null}`,
			`create "Sara" []`},
		// what is left out stays, null clears, and a value sets
		{"PATCH", "/api/subscriptions/3", `{"trafficLimit":5368709120}`,
			`update 3 name="" limit=true/false/5368709120 expires=false/false/` + noDate},
		{"PATCH", "/api/subscriptions/3", `{"trafficLimit":null}`,
			`update 3 name="" limit=true/true/0 expires=false/false/` + noDate},
		{"PATCH", "/api/subscriptions/3", `{"expiresAt":"2027-01-02T03:04:05Z"}`,
			`update 3 name="" limit=false/false/0 expires=true/false/2027-01-02T03:04:05Z`},
		{"PATCH", "/api/subscriptions/3", `{"expiresAt":"2027-01-02T06:34:05+03:30"}`,
			`update 3 name="" limit=false/false/0 expires=true/false/2027-01-02T03:04:05Z`},
		{"PATCH", "/api/subscriptions/3", `{"expiresAt":null}`,
			`update 3 name="" limit=false/false/0 expires=true/true/` + noDate},
		{"PATCH", "/api/subscriptions/3", `{"name":"Sara","trafficLimit":10,"expiresAt":null}`,
			`update 3 name="Sara" limit=true/false/10 expires=true/true/` + noDate},
		{"POST", "/api/subscriptions/3/reset-usage", ``, `reset 3`},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		if code >= 300 {
			t.Fatalf("%s %s %s = %d %s", c.method, c.path, c.body, code, body)
		}
		want = append(want, c.call)
	}
	if got := fake.called(); !slices.Equal(got, want) {
		t.Errorf("the manager was asked\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestABadLimitOrDateIsRefusedBeforeTheManager(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	for name, c := range map[string]subsReq{
		"nothing to change":            {"PATCH", "/api/subscriptions/3", `{}`},
		"a limit as a string":          {"PATCH", "/api/subscriptions/3", `{"trafficLimit":"5"}`},
		"a negative limit":             {"PATCH", "/api/subscriptions/3", `{"trafficLimit":-1}`},
		"a fractional limit":           {"PATCH", "/api/subscriptions/3", `{"trafficLimit":1.5}`},
		"a date that is not one":       {"PATCH", "/api/subscriptions/3", `{"expiresAt":"tomorrow"}`},
		"a date as a number":           {"PATCH", "/api/subscriptions/3", `{"expiresAt":5}`},
		"a date without a zone":        {"PATCH", "/api/subscriptions/3", `{"expiresAt":"2027-01-02T03:04:05"}`},
		"a field it doesn't take":      {"PATCH", "/api/subscriptions/3", `{"trafficLimit":1,"used":0}`},
		"create: a negative limit":     {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"trafficLimit":-5}`},
		"create: a date that is not":   {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"expiresAt":"soon"}`},
		"reset: a body it doesn't use": {"GET", "/api/subscriptions/3/reset-usage", ``},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		if code < 400 || code >= 500 {
			t.Errorf("%s: %s %s %s = %d %s", name, c.method, c.path, c.body, code, body)
		}
	}
	if got := fake.called(); len(got) != 0 {
		t.Errorf("the manager was asked %v", got)
	}
}

func TestTheSubscriptionViewCarriesItsLimitsAndWhatItUsed(t *testing.T) {
	live := newFakeLive()
	live.life[1] = 7 << 30
	r := newRigWith(t, "", withLive(live))
	r.populate()
	// the second one ended yesterday, and the hub has stopped it
	if err := r.st.Update(func(s *model.State) error {
		sub := s.FindSubscription(2)
		sub.ExpiresAt, sub.Stopped = now.AddDate(0, 0, -1), model.StopExpired
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	subs := decode[[]Subscription](t, r.get("/api/subscriptions"))
	if len(subs) != 2 {
		t.Fatalf("%d subscriptions", len(subs))
	}
	running, stopped := subs[0], subs[1]
	if running.TrafficLimit != 50<<30 || running.TrafficUsed != 7<<30 || running.ExpiresAt == nil || running.Stopped != "" {
		t.Errorf("the running one = %+v", running)
	}
	if stopped.TrafficLimit != 0 || stopped.Stopped != "expired" || stopped.ExpiresAt == nil {
		t.Errorf("the stopped one = %+v", stopped)
	}

	// a node's own list carries the same, so it can show and edit them too
	rows := decode[[]NodeSubscription](t, r.get("/api/nodes/1/subscriptions"))
	if len(rows) != 2 {
		t.Fatalf("%d rows on the node", len(rows))
	}
	if a := rows[0]; a.TrafficLimit != 50<<30 || a.TrafficUsed != 7<<30 || a.ExpiresAt == nil || !a.ExpiresAt.Equal(*running.ExpiresAt) {
		t.Errorf("the node's row of the running one = %+v", a)
	}
	if b := rows[1]; b.Stopped != "expired" || b.ExpiresAt == nil || b.TrafficLimit != 0 {
		t.Errorf("the node's row of the stopped one = %+v", b)
	}
}

func TestUsedIsWhatIsLeftAfterTheLastReset(t *testing.T) {
	for _, c := range []struct{ base, lifetime, want uint64 }{
		{0, 0, 0}, {0, 500, 500}, {200, 500, 300}, {500, 500, 0},
		// the usage file was lost: nothing is owed until the count passes the base again
		{500, 100, 0},
	} {
		if got := TrafficUsed(model.Subscription{TrafficBase: c.base}, c.lifetime); got != c.want {
			t.Errorf("base %d, lifetime %d: used %d, want %d", c.base, c.lifetime, got, c.want)
		}
	}
	// what a backup brought counts as used here, and a reset after it measures from the sum
	for _, c := range []struct{ carried, base, lifetime, want uint64 }{
		{600, 0, 0, 600}, {600, 0, 350, 950}, {600, 1050, 450, 0}, {600, 1050, 460, 10},
		{^uint64(0), 0, 5, ^uint64(0)},
	} {
		if got := TrafficUsed(model.Subscription{TrafficBase: c.base, TrafficCarried: c.carried}, c.lifetime); got != c.want {
			t.Errorf("carried %d, base %d, lifetime %d: used %d, want %d", c.carried, c.base, c.lifetime, got, c.want)
		}
	}
}
