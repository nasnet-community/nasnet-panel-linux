// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	appstats "github.com/xtls/xray-core/app/stats"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// statusBridge is a bridge with no Xray instance behind it: the tracker and
// the counters are all the status page reads.
func statusBridge(t *testing.T, clk *clock) (*bridge, *appstats.Manager) {
	t.Helper()
	mgr, err := appstats.NewManager(context.Background(), &appstats.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := testSpec()
	return &bridge{spec: s, stats: mgr, tracker: newTracker(s, clk.Now), started: clk.Now()}, mgr
}

func setCounter(t *testing.T, mgr *appstats.Manager, name string, v int64) {
	t.Helper()
	c, err := mgr.RegisterCounter(name)
	if err != nil {
		t.Fatal(err)
	}
	c.Set(v)
}

func TestStatusReadsTunnelsAndCounters(t *testing.T) {
	clk := newClock()
	b, mgr := statusBridge(t, clk)
	start := clk.Now()

	// Counters exist only once a flow has run: absent reads as zero.
	setCounter(t, mgr, "user>>>rvs-link-1>>>traffic>>>uplink", 1234)
	setCounter(t, mgr, "user>>>rvs-link-1>>>traffic>>>downlink", 5678)

	// One open connection through node 1.
	c, _ := tcpPair(t)
	flow := b.tracker.wrap(session.ContextWithInbound(context.Background(), &session.Inbound{Tag: "rvs-from-n1"}),
		nil, xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443), c)
	defer flow.Close()

	clk.Advance(time.Minute)
	st := b.status()
	if !st.Running || st.Xray != core.Version() || !st.Started.Equal(start) || len(st.Nodes) != 2 {
		t.Fatalf("status %+v", st)
	}
	want := []bridgespec.NodeStatus{
		{Tag: "rvs-link-1", Since: start, BytesUp: 1234, BytesDown: 5678, Connections: 1},
		{Tag: "rvs-link-2", Since: start},
	}
	if !reflect.DeepEqual(st.Nodes, want) {
		t.Fatalf("nodes\n got %+v\nwant %+v", st.Nodes, want)
	}

	b.stopping.Store(true)
	if b.status().Running {
		t.Error("still running while stopping")
	}
}

func TestStatusWithoutCounters(t *testing.T) {
	clk := newClock()
	b, _ := statusBridge(t, clk)
	b.stats = nil // no stats feature at all
	for _, n := range b.status().Nodes {
		if n.BytesUp != 0 || n.BytesDown != 0 {
			t.Errorf("%+v", n)
		}
	}
}

func TestStatusPage(t *testing.T) {
	clk := newClock()
	b, mgr := statusBridge(t, clk)
	setCounter(t, mgr, "user>>>rvs-link-2>>>traffic>>>uplink", 99)
	srv := httptest.NewServer(b.handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + bridgespec.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache control %q", cc)
	}
	var st bridgespec.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Nodes) != 2 || st.Nodes[1].Tag != "rvs-link-2" || st.Nodes[1].BytesUp != 99 {
		t.Errorf("served %+v", st)
	}

	// Nothing else is served, and nothing can be changed through it.
	for _, tt := range []struct {
		method, path string
		want         int
	}{
		{http.MethodHead, bridgespec.StatusPath, http.StatusOK},
		{http.MethodPost, bridgespec.StatusPath, http.StatusMethodNotAllowed},
		{http.MethodPut, bridgespec.StatusPath, http.StatusMethodNotAllowed},
		{http.MethodDelete, bridgespec.StatusPath, http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/status/extra", http.StatusNotFound},
		{http.MethodGet, "/debug/pprof/", http.StatusNotFound},
		{http.MethodGet, "/metrics", http.StatusNotFound},
	} {
		req, _ := http.NewRequest(tt.method, srv.URL+tt.path, nil)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != tt.want {
			t.Errorf("%s %s: %d, want %d", tt.method, tt.path, r.StatusCode, tt.want)
		}
	}
}

// The hub compares the version the status page reports with the one it
// installs on the nodes: the two ends of a reverse tunnel have to be one build.
func TestBridgeRunsThePinnedXray(t *testing.T) {
	if core.Version() != pin.Version {
		t.Fatalf("the bridge links Xray %s, the pin is %s", core.Version(), pin.Version)
	}
}

// fakeChecks stands in for the core's observatory.
type fakeChecks []*observatory.OutboundStatus

func (f fakeChecks) GetObservation(context.Context) (proto.Message, error) {
	return &observatory.ObservationResult{Status: f}, nil
}

func TestStatusReadsTheOutbounds(t *testing.T) {
	clk := newClock()
	b, mgr := statusBridge(t, clk)
	all := testOutbounds()
	up, down, unchecked, plain := all["socks"], all["trojan"], all["shadowsocks"], all["http"]
	up.Tag, down.Tag, unchecked.Tag, plain.Tag = "out-1.", "out-2.", "out-3.", "out-4."
	down.Fallback, unchecked.Fallback = true, true
	b.spec.Outbounds = []bridgespec.Outbound{up, down, unchecked, plain}
	b.spec.OutboundAddr = "192.168.50.16"

	setCounter(t, mgr, "outbound>>>out-1.>>>traffic>>>uplink", 100)
	setCounter(t, mgr, "outbound>>>out-1.>>>traffic>>>downlink", 2000)
	setCounter(t, mgr, "outbound>>>out-2.>>>traffic>>>uplink", 7)
	tried, seen := clk.Now().Add(-10*time.Second), clk.Now().Add(-time.Minute)
	b.checks = fakeChecks{
		// the observatory's own order, and what it writes for one that is down
		{OutboundTag: "out-4.", Alive: false, Delay: 99999999, LastErrorReason: "the outbound out-4. is dead", LastTryTime: tried.Unix()},
		{OutboundTag: "out-2.", Alive: false, Delay: 99999999, LastErrorReason: "the outbound out-2. is dead", LastTryTime: tried.Unix(), LastSeenTime: seen.Unix()},
		{OutboundTag: "out-1.", Alive: true, Delay: 180, LastTryTime: tried.Unix(), LastSeenTime: tried.Unix()},
	}

	want := []bridgespec.OutboundStatus{
		{Tag: "out-1.", Checked: true, Alive: true, DelayMS: 180, LastTry: time.Unix(tried.Unix(), 0), LastSeen: time.Unix(tried.Unix(), 0), BytesUp: 100, BytesDown: 2000},
		{Tag: "out-2.", Checked: true, Error: "The check failed: the outbound is dead.", LastTry: time.Unix(tried.Unix(), 0), LastSeen: time.Unix(seen.Unix(), 0), Fallback: true, BytesUp: 7},
		// not checked yet: taken to answer, so nothing falls back
		{Tag: "out-3."},
		// down, but with no fallback to take
		{Tag: "out-4.", Checked: true, Error: "The check failed: the outbound is dead.", LastTry: time.Unix(tried.Unix(), 0)},
	}
	if got := b.status().Outbounds; !reflect.DeepEqual(got, want) {
		t.Fatalf("outbounds\n got %+v\nwant %+v", got, want)
	}

	// with no observatory to ask, all of them read as unchecked
	b.checks = nil
	for _, o := range b.status().Outbounds {
		if o.Checked || o.Alive || o.Fallback {
			t.Errorf("with nothing checked: %+v", o)
		}
	}
}

// A spec with no outbound has none in its status, and says nothing of them.
func TestStatusWithoutOutbounds(t *testing.T) {
	b, _ := statusBridge(t, newClock())
	if st := b.status(); st.Outbounds != nil {
		t.Errorf("outbounds %+v", st.Outbounds)
	}
	data, err := json.Marshal(b.status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "outbounds") {
		t.Errorf("the status page mentions outbounds: %s", data)
	}
}

// The core's own account of a failed check, as the end-to-end runs saw it,
// comes out as a short sentence: no tag, no address it asked for, no package
// paths.
func TestCheckReasonsAreShortSentences(t *testing.T) {
	const wrap = "with outbound handler report underlying connection failed"
	dead := func(tag, url, why string) string {
		return "the outbound " + tag + " is dead: GET request failed:app/observatory: outbound failed to relay connection > Get \"" + url + "\": " + why + wrap
	}
	for _, tt := range []struct {
		name, raw, want string
	}{
		// a server that is gone, a port nothing listens on, a wrong password,
		// a server that hangs up at once: the core says the same for them all
		{"a dead server", dead("out-1.", "http://127.0.0.1:53040/check", "io: read/write on closed pipe"),
			"The outbound gave up before an answer came."},
		{"a timeout", dead("out-2.", "https://www.google.com/generate_204", "context deadline exceeded (Client.Timeout exceeded while awaiting headers)"),
			"No answer within 5 seconds."},
		{"a target that refused the server", dead("out-10.", "http://127.0.0.1:53500/x", "EOF"),
			"The server closed the connection."},
		{"a certificate nobody trusts", dead("out-1.", "https://127.0.0.1:53410/x", "tls: failed to verify certificate: x509: certificate signed by unknown authority"),
			"The check failed: tls: failed to verify certificate: x509: certificate signed by unknown authority."},
		{"something that isn't HTTP", dead("out-1.", "http://127.0.0.1:53409/x", `net/http: HTTP/1.x transport connection broken: malformed HTTP status code "there"`),
			`The check failed: HTTP/1.x transport connection broken: malformed HTTP status code "there".`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tag := strings.Fields(tt.raw)[2]
			got := checkReason(tt.raw, tag)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
			if strings.Contains(got, tag) || strings.Contains(got, "http://") || strings.Contains(got, "https://") ||
				strings.Contains(got, "app/") || len(got) > 250 {
				t.Errorf("%q is not a short reason", got)
			}
		})
	}
	// whatever the core writes next, the result stays short
	long := checkReason(strings.Repeat("x", 1000), "out-1.")
	if len(long) > 250 {
		t.Errorf("%d characters", len(long))
	}
}

// What the status page says of a failed check is that short reason.
func TestStatusGivesTheShortReason(t *testing.T) {
	b, _ := statusBridge(t, newClock())
	o := testOutbounds()["socks"]
	o.Tag = "out-1."
	b.spec.Outbounds, b.spec.OutboundAddr = []bridgespec.Outbound{o}, "192.168.50.16"
	b.checks = fakeChecks{{OutboundTag: o.Tag, LastErrorReason: "the outbound out-1. is dead: GET request failed:app/observatory: " +
		"outbound failed to relay connection > Get \"http://127.0.0.1:53040/check\": io: read/write on closed pipewith outbound handler report underlying connection failed"}}
	if got := b.status().Outbounds[0].Error; got != "The outbound gave up before an answer came." {
		t.Errorf("error %q", got)
	}
}

// An outbound left out is listed in its place, down for good, with Xray's
// reason, and falls back if its switch says so; the check knows nothing of it.
func TestStatusListsARefusedOutbound(t *testing.T) {
	b, _ := statusBridge(t, newClock())
	s := refusedSpec()
	b.spec.Outbounds, b.spec.OutboundAddr = s.Outbounds, s.OutboundAddr
	b.refused = refusedOutbounds(s)
	b.checks = fakeChecks{{OutboundTag: "out-1.", Alive: true, Delay: 40, LastTryTime: 1, LastSeenTime: 1}}
	got := b.status().Outbounds
	if len(got) != 3 || got[0].Tag != "out-1." || !got[0].Alive || got[1].Tag != "out-2." || got[2].Tag != "out-3." {
		t.Fatalf("outbounds %+v", got)
	}
	for i, fallback := range map[int]bool{1: true, 2: false} {
		o := got[i]
		if !o.Checked || o.Alive || o.Fallback != fallback || o.Error != b.refused[o.Tag] || o.DelayMS != 0 || !o.LastTry.IsZero() {
			t.Errorf("%s: %+v", o.Tag, o)
		}
	}
}
