// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
)

// fakeLive answers from tables the test fills in. A node, subscription or
// pair that isn't in a table reads as the zero value, which is what a manager
// says of one it knows nothing about.
type fakeLive struct {
	entry   Entry
	xray    string
	nodes   map[uint]NodeLive
	events  map[uint][]NodeEvent
	on      map[[2]uint]SubscriptionUsage
	total   map[uint]SubscriptionUsage
	life    map[uint]uint64
	traffic map[uint]Traffic
}

func (f *fakeLive) Entry() Entry                                    { return f.entry }
func (f *fakeLive) Xray() string                                    { return f.xray }
func (f *fakeLive) NodeLive(id uint) NodeLive                       { return f.nodes[id] }
func (f *fakeLive) NodeEvents(id uint) []NodeEvent                  { return f.events[id] }
func (f *fakeLive) SubscriptionOn(sub, node uint) SubscriptionUsage { return f.on[[2]uint{sub, node}] }
func (f *fakeLive) SubscriptionTotal(sub uint) SubscriptionUsage    { return f.total[sub] }
func (f *fakeLive) SubscriptionLifetime(sub uint) uint64            { return f.life[sub] }
func (f *fakeLive) Traffic(node uint) Traffic                       { return f.traffic[node] }

func newFakeLive() *fakeLive {
	return &fakeLive{
		nodes: map[uint]NodeLive{}, events: map[uint][]NodeEvent{}, on: map[[2]uint]SubscriptionUsage{},
		total: map[uint]SubscriptionUsage{}, life: map[uint]uint64{}, traffic: map[uint]Traffic{},
	}
}

// forNode sets what is known of a node and returns the fake, for chaining.
func (f *fakeLive) forNode(id uint, l NodeLive) *fakeLive { f.nodes[id] = l; return f }

func withLive(l Live) func(*Config, *jobs.Engine) {
	return func(c *Config, _ *jobs.Engine) { c.Live = l }
}

// at is a time that many minutes from the test's clock.
func at(minute int) *time.Time {
	t := now.Add(time.Duration(minute) * time.Minute)
	return &t
}

func ptr[T any](v T) *T { return &v }

var wantLiveEntry = Entry{
	On: true, Domestic: "online", VPN: VPN{State: "online", Kind: "wireguard", Label: "wg-vpn"}, Bridge: "running",
	RouterOS: RouterOS{Version: "7.24.5", Min: "7.20", OK: true},
	Routes: []Route{
		{Address: "192.168.50.16", UsedFor: "downloads", Via: "foreign", Ready: true},
		{Address: "192.168.50.17", UsedFor: "entry-exit", Via: "vpn", Ready: true},
		{Address: "192.168.50.18", UsedFor: "nodes", Via: "domestic", Ready: true},
	},
	CheckedAt: at(-2),
}

// Without a manager the new routes answer from the model alone, in the same
// shapes the manager fills in.
func TestWithoutLiveTheNodeRoutesAnswerFromTheModel(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		r.populate()

		n := decode[Node](t, r.get(b+"/api/nodes/1"))
		if n.ID != 1 || n.Name != "تهران" || !n.Registered || n.Tunnel.State != "off" || n.Agent != nil || n.LastSeenAt != nil || n.Installing != nil {
			t.Errorf("node 1 = %+v", n)
		}
		if n.Events == nil || len(*n.Events) != 0 {
			t.Errorf("node 1 has events %v, want an empty list", n.Events)
		}
		// the key is there as [], not left out and not null
		raw := decode[map[string]json.RawMessage](t, r.get(b+"/api/nodes/1"))
		if string(raw["events"]) != "[]" {
			t.Errorf(`"events" = %s, want []`, raw["events"])
		}
		for _, id := range []string{"2"} { // still installing: it has a page too
			if m := decode[Node](t, r.get(b+"/api/nodes/"+id)); m.ID != 2 || m.Registered {
				t.Errorf("node %s = %+v", id, m)
			}
		}
		// the list leaves events to the page of one node
		for _, row := range decode[[]map[string]json.RawMessage](t, r.get(b+"/api/nodes")) {
			if _, has := row["events"]; has {
				t.Errorf("a node in the list has events: %s", row["events"])
			}
		}

		rows := decode[[]NodeSubscription](t, r.get(b+"/api/nodes/1/subscriptions"))
		if len(rows) != 2 || rows[0].Name != "علی" || !rows[0].Enabled || rows[1].Name != "sara" || rows[1].Enabled {
			t.Errorf("node 1's subscriptions = %+v", rows)
		}
		for _, row := range rows {
			if row.Online != nil || row.Connections != nil || row.LastSeenAt != nil || row.TodayUp+row.TodayDown+row.MonthUp+row.MonthDown != 0 {
				t.Errorf("a row says what nobody knows: %+v", row)
			}
		}
		// a node with none switched on lists every subscription all the same
		if rows := decode[[]NodeSubscription](t, r.get(b+"/api/nodes/2/subscriptions")); len(rows) != 2 || rows[0].Enabled || rows[1].Enabled {
			t.Errorf("node 2's subscriptions = %+v", rows)
		}

		if got := string(r.get(b + "/api/nodes/1/traffic")); got != `{"stepSec":5,"samples":[]}`+"\n" {
			t.Errorf("traffic = %q", got)
		}
	}
}

func TestAnUnknownNodeIsNotFoundOnEveryNodeRoute(t *testing.T) {
	r := newRigWith(t, base, withLive(newFakeLive()))
	r.populate()
	for _, p := range []string{"/api/nodes/9", "/api/nodes/0", "/api/nodes/abc", "/api/nodes/-1", "/api/nodes/1.5", "/api/nodes/99999999999999"} {
		for _, suffix := range []string{"", "/subscriptions", "/traffic"} {
			for _, prefix := range []string{"", base} {
				code, _, body := r.call("GET", prefix+p+suffix, map[string]string{UIHeader: "1"})
				got := decode[map[string]any](t, body)
				if code != http.StatusNotFound || got["code"] != "not_found" || got["error"] == "" || len(got) != 2 {
					t.Errorf("GET %s = %d %s", prefix+p+suffix, code, body)
				}
			}
		}
	}
	// and none is open without the page's header
	for _, suffix := range []string{"", "/subscriptions", "/traffic"} {
		if code, _, _ := r.call("GET", "/api/nodes/1"+suffix, nil); code != http.StatusForbidden {
			t.Errorf("GET /api/nodes/1%s without the header = %d", suffix, code)
		}
	}
}

func TestLiveFillsTheLivePartsOfEveryView(t *testing.T) {
	live := newFakeLive()
	live.entry, live.xray = wantLiveEntry, "26.10.21"
	live.forNode(1, NodeLive{
		LastSeenAt: at(-1),
		Tunnel:     TunnelState{State: "up", Since: at(-60), RTTMs: 0, Streams: 3, BytesToSubs: 900, BytesFromSubs: 100},
		Agent: &AgentState{
			Version: "1.3.0", XrayVersion: "26.10.21", ReportedAt: *at(-1),
			Xray: XrayState{Running: true, Restarts: 2, UptimeSec: 600},
		},
		Updates: Updates{Agent: true, AgentVersion: "1.3.1"},
		Usage:   NodeUsage{Live: true, Online: 1, Connections: 3, RateUp: 10, RateDown: 20, TodayUp: 1, TodayDown: 2, MonthUp: 3, MonthDown: 4, UpdatedAt: at(-1)},
	}).forNode(2, NodeLive{
		Installing: &Installing{JobID: "abc", Step: 2, Steps: 8, Key: "upload_agent", Label: "Upload the agent", Percent: ptr(40)},
	})
	live.events[1] = []NodeEvent{{At: *at(-1), Kind: "tunnel_up", Detail: ""}, {At: *at(-30), Kind: "node_back", Detail: "x"}}
	live.traffic[1] = Traffic{StepSec: 5, Samples: []TrafficSample{{At: *at(-1), Up: 7, Down: 8}}}

	for _, b := range []string{"", base} {
		r := newRigWith(t, b, withLive(live))
		r.populate()

		cfg := decode[AppConfig](t, r.get(b+"/api/config"))
		if cfg.Xray != "26.10.21" || !cfg.Entry.On || cfg.Entry.VPN.Label != "wg-vpn" || len(cfg.Entry.Routes) != 3 || cfg.Entry.Bridge != "running" ||
			cfg.Entry.CheckedAt == nil || !cfg.Entry.CheckedAt.Equal(*wantLiveEntry.CheckedAt) {
			t.Errorf("config = %+v", cfg)
		}
		// what the model holds is still the model's
		if cfg.NodeCount != 2 || cfg.SubscriptionCount != 2 || cfg.Version != "1.2.3" {
			t.Errorf("config counts = %+v", cfg)
		}

		nodes := decode[[]Node](t, r.get(b+"/api/nodes"))
		n := nodes[0]
		if n.Tunnel.State != "up" || n.Tunnel.Streams != 3 || n.Tunnel.BytesToSubs != 900 || n.LastSeenAt == nil || !n.LastSeenAt.Equal(*at(-1)) ||
			n.Agent == nil || n.Agent.Version != "1.3.0" || n.Agent.Xray.Restarts != 2 || !n.Updates.Agent || n.Updates.AgentVersion != "1.3.1" ||
			!n.Usage.Live || n.Usage.Connections != 3 || n.Usage.RateDown != 20 || n.Usage.UpdatedAt == nil || n.Installing != nil || n.Events != nil {
			t.Errorf("node 1 = %+v", n)
		}
		// the model's own fields didn't move
		if n.Name != "تهران" || n.XrayVersion != "26.10.21" || n.SubscriptionCount != 1 || n.Cert.ValidDays != 355 {
			t.Errorf("node 1 lost what the model says: %+v", n)
		}
		m := nodes[1]
		if m.Installing == nil || m.Installing.JobID != "abc" || m.Installing.Step != 2 || m.Installing.Steps != 8 || m.Installing.Key != "upload_agent" || m.Installing.Label != "Upload the agent" ||
			m.Installing.Percent == nil || *m.Installing.Percent != 40 {
			t.Errorf("node 2 = %+v", m)
		}
		// a node the manager says nothing of stays off, and the page reads that
		if m.Tunnel.State != "off" || m.Agent != nil {
			t.Errorf("node 2's tunnel = %+v", m.Tunnel)
		}

		one := decode[Node](t, r.get(b+"/api/nodes/1"))
		if one.Events == nil || len(*one.Events) != 2 || (*one.Events)[0].Kind != "tunnel_up" || (*one.Events)[1].Detail != "x" {
			t.Errorf("node 1's events = %v", one.Events)
		}
		if one.Tunnel.State != "up" || one.Usage.Online != 1 {
			t.Errorf("the one-node view is missing the live parts: %+v", one)
		}
		// an installing node's page carries its progress and an empty list of events
		two := decode[Node](t, r.get(b+"/api/nodes/2"))
		if two.Installing == nil || two.Events == nil || len(*two.Events) != 0 {
			t.Errorf("node 2's page = %+v", two)
		}
		// the step it is on goes by its key, with the English name beside it
		if in := two.Installing; in.Key != "upload_agent" || in.Label != "Upload the agent" {
			t.Errorf("node 2's installing = %+v", in)
		}

		tr := decode[Traffic](t, r.get(b+"/api/nodes/1/traffic"))
		if tr.StepSec != 5 || len(tr.Samples) != 1 || tr.Samples[0].Up != 7 || tr.Samples[0].Down != 8 || !tr.Samples[0].At.Equal(*at(-1)) {
			t.Errorf("traffic = %+v", tr)
		}
		// a node with no samples says [] and gets the step the chart is drawn with
		if got := string(r.get(b + "/api/nodes/2/traffic")); got != `{"stepSec":5,"samples":[]}`+"\n" {
			t.Errorf("an empty chart = %q", got)
		}
	}
}

func TestAnEntryWithNoRoutesStillSaysSo(t *testing.T) {
	live := newFakeLive()
	live.entry = Entry{Domestic: "missing", VPN: VPN{State: "missing"}, Bridge: "stopped"} // Routes nil
	r := newRigWith(t, "", withLive(live))
	body := r.get("/api/config")
	if !bytes.Contains(body, []byte(`"routes":[]`)) {
		t.Errorf("routes should be [], not null: %s", body)
	}
}

// What a subscription shows is made from its rows: connected anywhere is
// online; otherwise it is known to be offline only if every node it is on has
// answered, and unknown while one hasn't.
func TestASubscriptionIsOnlineByItsRows(t *testing.T) {
	type use = SubscriptionUsage
	for name, c := range map[string]struct {
		on     map[[2]uint]use
		online string // "true", "false" or "null"
		node1  string // sub 1 on node 1: true, false or null
	}{
		"connected on its node":              {map[[2]uint]use{{1, 1}: {Live: true, Online: true, Connections: 2}}, "true", "true"},
		"on its node and nobody there":       {map[[2]uint]use{{1, 1}: {Live: true}}, "false", "false"},
		"its node doesn't answer":            {map[[2]uint]use{{1, 1}: {Live: false}}, "null", "null"},
		"nothing is known":                   {nil, "null", "null"},
		"switched off on node 2, node 1 up":  {map[[2]uint]use{{1, 1}: {Live: true, Online: true}, {1, 2}: {Live: false}}, "true", "true"},
		"a connection on a switched off row": {map[[2]uint]use{{1, 1}: {Live: true}, {2, 1}: {Live: true, Online: true}}, "false", "false"},
	} {
		t.Run(name, func(t *testing.T) {
			live := newFakeLive()
			live.on = c.on
			r := newRigWith(t, "", withLive(live))
			r.populate()
			subs := decode[[]map[string]json.RawMessage](t, r.get("/api/subscriptions"))
			if got := string(subs[0]["online"]); got != c.online {
				t.Errorf("subscription 1 online = %s, want %s", got, c.online)
			}
			var nodes []map[string]json.RawMessage
			if err := json.Unmarshal(subs[0]["nodes"], &nodes); err != nil {
				t.Fatal(err)
			}
			if got := string(nodes[0]["online"]); got != c.node1 {
				t.Errorf("subscription 1 on node 1 online = %s, want %s", got, c.node1)
			}
		})
	}
}

// Sara's subscription is switched off everywhere, so it is not connected, and
// that is known: it doesn't have to wait for a node to answer.
func TestASubscriptionSwitchedOffEverywhereIsNotOnline(t *testing.T) {
	live := newFakeLive()
	r := newRigWith(t, "", withLive(live))
	r.populate()
	subs := decode[[]map[string]json.RawMessage](t, r.get("/api/subscriptions"))
	if got := string(subs[1]["online"]); got != "false" {
		t.Errorf("sara online = %s", got)
	}
}

func TestSubscriptionRowsCarryTheirNumbers(t *testing.T) {
	live := newFakeLive()
	live.on[[2]uint{1, 1}] = SubscriptionUsage{Live: true, Online: true, Connections: 2, LastSeenAt: at(-1), TodayUp: 1, TodayDown: 2, MonthUp: 3, MonthDown: 4}
	// sara is switched off on node 1, and what she used there while she was on is still hers
	live.on[[2]uint{2, 1}] = SubscriptionUsage{Live: true, LastSeenAt: at(-90), TodayUp: 5, MonthDown: 6}
	live.total[1] = SubscriptionUsage{Live: true, Online: true, Connections: 2, LastSeenAt: at(-1), TodayUp: 10, TodayDown: 20, MonthUp: 30, MonthDown: 40}
	live.total[2] = SubscriptionUsage{LastSeenAt: at(-90), TodayUp: 5, MonthDown: 6}

	r := newRigWith(t, "", withLive(live))
	r.populate()

	subs := decode[[]Subscription](t, r.get("/api/subscriptions"))
	ali, sara := subs[0], subs[1]
	if ali.Online == nil || !*ali.Online || ali.LastSeenAt == nil || !ali.LastSeenAt.Equal(*at(-1)) || ali.TodayUp != 10 || ali.TodayDown != 20 || ali.MonthUp != 30 || ali.MonthDown != 40 {
		t.Errorf("ali = %+v", ali)
	}
	row := ali.Nodes[0]
	if row.Online == nil || !*row.Online || row.Connections == nil || *row.Connections != 2 || row.TodayUp != 1 || row.TodayDown != 2 || row.MonthUp != 3 || row.MonthDown != 4 {
		t.Errorf("ali's row = %+v", row)
	}
	if sara.Online == nil || *sara.Online || sara.TodayUp != 5 || sara.MonthDown != 6 {
		t.Errorf("sara = %+v", sara)
	}
	srow := sara.Nodes[0]
	if srow.Enabled || srow.Online != nil || srow.Connections != nil || srow.TodayUp != 5 || srow.MonthDown != 6 || srow.LastSeenAt == nil {
		t.Errorf("sara's row = %+v", srow)
	}

	rows := decode[[]NodeSubscription](t, r.get("/api/nodes/1/subscriptions"))
	if r0 := rows[0]; !r0.Enabled || r0.Online == nil || !*r0.Online || r0.Connections == nil || *r0.Connections != 2 || r0.TodayUp != 1 || r0.LastSeenAt == nil {
		t.Errorf("node 1's first row = %+v", r0)
	}
	if r1 := rows[1]; r1.Enabled || r1.Online != nil || r1.Connections != nil || r1.TodayUp != 5 || r1.MonthDown != 6 {
		t.Errorf("node 1's second row = %+v", r1)
	}
}

// The contract's keys, for the shapes this file adds.
func TestTheNewShapesAreTheContracts(t *testing.T) {
	live := newFakeLive()
	live.traffic[1] = Traffic{StepSec: 5, Samples: []TrafficSample{{At: *at(0), Up: 1, Down: 2}}}
	r := newRigWith(t, "", withLive(live))
	r.populate()

	tr := decode[map[string]any](t, r.get("/api/nodes/1/traffic"))
	wantKeys(t, "traffic", tr, "stepSec samples")
	wantKeys(t, "traffic sample", tr["samples"].([]any)[0].(map[string]any), "at up down")

	rows := decode[[]map[string]any](t, r.get("/api/nodes/1/subscriptions"))
	for _, row := range rows {
		wantKeys(t, "a node's subscription", row, "id name enabled stopped online connections lastSeenAt todayUp todayDown monthUp monthDown")
	}

	one := decode[map[string]any](t, r.get("/api/nodes/1"))
	wantKeys(t, "one node", one, "id name host sshPort sshUser registered createdAt entryPort handshakeTarget sni sniCustom publicKey shortId arch agentVersion xrayVersion lastSeenAt subscriptionCount tunnel agent cert updates usage events")
}

// The live parts come only through Live: a manager that answers with a
// secret-looking node must not make the views any more telling.
func TestLiveDoesNotTouchWhatTheModelKeepsSecret(t *testing.T) {
	live := newFakeLive()
	live.entry, live.xray = wantLiveEntry, "26.10.21"
	live.forNode(1, NodeLive{LastSeenAt: at(-1), Tunnel: TunnelState{State: "up"}, Agent: &AgentState{Version: "1.3.0"}})
	r := newRigWith(t, base, withLive(live))
	r.populate()
	for _, p := range []string{"/api/config", "/api/nodes", "/api/nodes/1", "/api/subscriptions", "/api/nodes/1/subscriptions"} {
		body := r.get(base + p)
		for _, s := range secrets() {
			if strings.Contains(string(body), s) {
				t.Errorf("GET %s carries %q", p, s)
			}
		}
	}
}
