package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

type traceOpts struct {
	vpnActive bool
	resolve   map[string]string
	domestic  []string
}

// mapDoH resolves only what a test names, so an unexpected lookup is a failure
// rather than a silent default.
type mapDoH struct{ m map[string]string }

func (f mapDoH) Resolve(_ context.Context, host string) (netip.Addr, error) {
	if ip, ok := f.m[host]; ok {
		return netip.MustParseAddr(ip), nil
	}
	return netip.Addr{}, fmt.Errorf("no answer for %s", host)
}

func newTraceFixture(t *testing.T, o traceOpts) *networkUsecase {
	t.Helper()
	u := newFlowFixture(t, flowOpts{vpnActive: o.vpnActive, wgFresh: true})

	uplinks, err := u.uplinks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	vpn := VPNRouteState{Active: o.vpnActive}

	be := u.Backend.(*system.FakeBackend)
	be.Rules = AllRules(flowGroups(), uplinks, vpn)
	be.Routes = []system.Route{
		{Table: 201, Dest: "default", Gateway: "192.0.2.1", OifName: "eth0"},
		{Table: 202, Dest: "default", Gateway: "100.64.0.1", OifName: "eth1"},
	}
	if o.vpnActive {
		be.Routes = append(be.Routes, vpnRoutes(uplinks)...)
	}
	// The kernel agrees with the walk unless a test says otherwise.
	be.RouteGetFn = func(dst string, mark uint32) (*system.Route, error) {
		rules := append([]system.Rule(nil), be.Rules...)
		_, route, _ := walkPolicy(rules, routesByTable(be.Routes), dst, mark)
		if route == nil {
			return nil, errors.New("RTNETLINK answers: Invalid argument")
		}
		return route, nil
	}

	members := map[string][]string{}
	if len(o.domestic) > 0 {
		members["ir_v4"] = o.domestic
	} else {
		members["ir_v4"] = []string{"5.144.128.1"}
	}
	u.Nftr = &system.FakeNft{Members: members, Objects: system.NftObjects{
		Chains: []string{"mangle_pre"}, Sets: []string{"ir_v4", "ir_dom_v4"},
	}}
	u.DoH = mapDoH{m: o.resolve}
	return u
}

func routesByTable(routes []system.Route) map[int][]system.Route {
	out := map[int][]system.Route{}
	for _, r := range routes {
		out[r.Table] = append(out[r.Table], r)
	}
	return out
}

func TestTraceForeignIPGoesThroughVPN(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: true})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "142.250.185.78", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if v.FinalVerdict != "delivered-vpn" {
		t.Fatalf("verdict %q, steps: %+v", v.FinalVerdict, v.Steps)
	}
	want := []string{"src-lan", "mark-foreign", "table-203", "wg", "table-202",
		"uplink-secondary", "world-foreign"}
	if !reflect.DeepEqual(v.PathNodes, want) {
		t.Fatalf("path %v", v.PathNodes)
	}
	if len(v.PathEdges) == 0 {
		t.Fatal("no edges to highlight")
	}
}

func TestTraceDomesticIPStaysDomestic(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: true})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "5.144.128.1", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if v.FinalVerdict != "delivered-domestic" {
		t.Fatalf("%q steps %+v", v.FinalVerdict, v.Steps)
	}
	if v.PathNodes[len(v.PathNodes)-1] != "world-domestic" {
		t.Fatalf("path %v", v.PathNodes)
	}
}

func TestTraceForeignWithVPNDownIsDropped(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: false})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "142.250.185.78", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if v.FinalVerdict != "dropped" {
		t.Fatalf("%q steps %+v", v.FinalVerdict, v.Steps)
	}
	if v.PathNodes[len(v.PathNodes)-1] != "killswitch" {
		t.Fatalf("path %v", v.PathNodes)
	}
	last := v.Steps[len(v.Steps)-1]
	if last.Verdict != "drop" {
		t.Fatalf("last step %+v", last)
	}
}

func TestTraceDomainResolvesViaDoH(t *testing.T) {
	u := newTraceFixture(t, traceOpts{
		vpnActive: true, resolve: map[string]string{"youtube.com": "142.250.185.78"},
	})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "youtube.com", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if v.ResolvedIP != "142.250.185.78" {
		t.Fatalf("resolved %q", v.ResolvedIP)
	}
	if v.Steps[0].Title != "Resolve" {
		t.Fatalf("first step %+v", v.Steps[0])
	}
}

func TestTraceRouterSourceUsesFallback(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: true})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "142.250.185.78", Source: "router"})
	if err != nil {
		t.Fatal(err)
	}
	// Unmarked traffic takes the fallback, which prefers the tunnel when it is up.
	if v.FinalVerdict != "delivered-vpn" {
		t.Fatalf("%q steps %+v", v.FinalVerdict, v.Steps)
	}
	if v.PathNodes[0] != "src-router" {
		t.Fatalf("path %v", v.PathNodes)
	}
}

func TestTraceXrayForeignIsMarkedRegardlessOfDestination(t *testing.T) {
	// The managed outbound stamps the group mark itself, so a domestic address
	// from the foreign outbound still rides the tunnel.
	u := newTraceFixture(t, traceOpts{vpnActive: true})
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "5.144.128.1", Source: "xray-foreign"})
	if err != nil {
		t.Fatal(err)
	}
	if v.FinalVerdict != "delivered-vpn" {
		t.Fatalf("%q steps %+v", v.FinalVerdict, v.Steps)
	}
}

func TestTraceRejectsGarbage(t *testing.T) {
	u := newTraceFixture(t, traceOpts{})
	for _, bad := range []TraceRequest{
		{Dest: "", Source: "lan"},
		{Dest: "1.1.1.1", Source: "nope"},
		{Dest: "not a host!", Source: "lan"},
	} {
		if _, err := u.TraceFlow(t.Context(), bad); !errors.Is(err, ErrBadTraceInput) {
			t.Fatalf("%+v accepted", bad)
		}
	}
}

func TestTraceReportsKernelDisagreement(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: true})
	be := u.Backend.(*system.FakeBackend)
	be.RouteGetFn = func(string, uint32) (*system.Route, error) {
		return &system.Route{Table: 201, OifName: "eth0"}, nil
	}
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "142.250.185.78", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range v.Steps {
		if s.Title == "Kernel check" && s.Verdict == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("disagreement not reported: %+v", v.Steps)
	}
}

// The walk finding nothing while the kernel routes it is the dangerous half of
// a disagreement: the page is about to call the packet dropped.
func TestTraceWarnsWhenOnlyTheKernelRoutes(t *testing.T) {
	u := newTraceFixture(t, traceOpts{vpnActive: false})
	be := u.Backend.(*system.FakeBackend)
	be.RouteGetFn = func(string, uint32) (*system.Route, error) {
		return &system.Route{Table: 254, OifName: "eth0"}, nil
	}
	v, err := u.TraceFlow(t.Context(), TraceRequest{Dest: "142.250.185.78", Source: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Steps {
		if s.Title == "Kernel check" {
			if s.Verdict != "warn" {
				t.Fatalf("kernel routes it but the walk did not, verdict %q: %+v", s.Verdict, s.Evidence)
			}
			return
		}
	}
	t.Fatal("no kernel check step")
}

func TestLookupTablePrefersTheLongerPrefix(t *testing.T) {
	routes := []system.Route{
		{Table: 203, Dest: "default", OifName: "nasnet-wg0"},
		{Table: 203, Dest: "192.168.100.0/24", OifName: "eth1", Scope: "link"},
	}
	got, plen, ok := lookupTable(routes, "192.168.100.1")
	if !ok || got.OifName != "eth1" || plen != 24 {
		t.Fatalf("got %+v /%d ok=%v", got, plen, ok)
	}
	got, plen, ok = lookupTable(routes, "8.8.8.8")
	if !ok || got.OifName != "nasnet-wg0" || plen != 0 {
		t.Fatalf("got %+v /%d ok=%v", got, plen, ok)
	}
}
