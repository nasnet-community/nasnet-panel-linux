// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	appstats "github.com/xtls/xray-core/app/stats"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"

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
