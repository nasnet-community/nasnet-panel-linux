package system

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func TestFakeBackend_SatisfiesTheSeam(t *testing.T) {
	var _ Backend = NewFakeBackend()
}

func TestFakeBackend_RuleAddDelIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := NewFakeBackend()

	r := Rule{Pref: 110, FwMark: netmark.GroupMark(netmark.GroupDomestic),
		FwMask: netmark.MaskGroup, Table: 201}

	for i := 0; i < 3; i++ {
		if err := f.RuleAdd(ctx, r); err != nil {
			t.Fatalf("RuleAdd #%d: %v", i, err)
		}
	}
	got, _ := f.RuleList(ctx)
	if len(got) != 1 {
		t.Fatalf("RuleAdd created %d rules for one spec", len(got))
	}

	if err := f.RuleDel(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := f.RuleDel(ctx, r); err != nil {
		t.Errorf("deleting an absent rule must succeed: %v", err)
	}
	got, _ = f.RuleList(ctx)
	if len(got) != 0 {
		t.Errorf("rules survived deletion: %+v", got)
	}
}

// Failover is one route operation and nothing else, so replace must overwrite
// rather than accumulate.
func TestFakeBackend_RouteReplaceOverwritesPerTableAndDest(t *testing.T) {
	ctx := context.Background()
	f := NewFakeBackend()

	if err := f.RouteReplace(ctx, Route{Table: 201, Dest: "default",
		Gateway: "192.168.1.1", OifName: "enp1s0"}); err != nil {
		t.Fatal(err)
	}
	if err := f.RouteReplace(ctx, Route{Table: 201, Dest: "default",
		Gateway: "192.168.1.254", OifName: "enp1s0"}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.RouteList(ctx, 201)
	if len(got) != 1 {
		t.Fatalf("got %d default routes in table 201, want 1", len(got))
	}
	if got[0].Gateway != "192.168.1.254" {
		t.Errorf("gateway = %q, want the replacement", got[0].Gateway)
	}

	// Deleting the default is exactly what marks an uplink down.
	if err := f.RouteDel(ctx, Route{Table: 201, Dest: "default"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = f.RouteList(ctx, 201); len(got) != 0 {
		t.Errorf("table 201 still has routes: %+v", got)
	}
}

func TestFakeBackend_Sysctl(t *testing.T) {
	ctx := context.Background()
	f := NewFakeBackend()
	if err := f.SysctlSet(ctx, "net.ipv4.conf.enp1s0.arp_ignore", "1"); err != nil {
		t.Fatal(err)
	}
	got, err := f.SysctlGet(ctx, "net.ipv4.conf.enp1s0.arp_ignore")
	if err != nil || got != "1" {
		t.Errorf("SysctlGet = %q, %v", got, err)
	}
	if _, err := f.SysctlGet(ctx, "net.ipv4.nope"); err == nil {
		t.Error("SysctlGet of an unset key should error")
	}
}

// A rule's identity is its whole spec, not just its preference: the pin rules at
// 50/51 and the group rules at 110/149 differ only in mark and action.
func TestRule_Equal(t *testing.T) {
	a := Rule{Pref: 51, FwMark: netmark.PinMark(1), FwMask: netmark.MaskPin, Blackhole: true}
	b := a
	if !a.Equal(b) {
		t.Error("identical rules compared unequal")
	}
	b.Blackhole = false
	b.Table = 201
	if a.Equal(b) {
		t.Error("a blackhole and a table lookup at the same pref compared equal")
	}
}

func TestFakeBackend_MultipathRouteRoundTrips(t *testing.T) {
	f := NewFakeBackend()
	ctx := context.Background()
	r := Route{Table: 203, Dest: "default", Nexthops: []Nexthop{
		{OifName: "nasnet-wg0", Weight: 3}, {OifName: "nasnet-wg1", Weight: 1},
	}}
	if err := f.RouteReplace(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Same (dest, metric) key: a rewrite swaps the set, never stacks a second default.
	r2 := Route{Table: 203, Dest: "default", Nexthops: []Nexthop{{OifName: "nasnet-wg1", Weight: 1}}}
	if err := f.RouteReplace(ctx, r2); err != nil {
		t.Fatal(err)
	}
	got, _ := f.RouteList(ctx, 203)
	if len(got) != 1 || !got[0].NexthopsEqual(r2) {
		t.Fatalf("routes = %+v", got)
	}
}
