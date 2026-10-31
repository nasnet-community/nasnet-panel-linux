// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// everythingUp is a report with a Domestic link, a VPN, all three routes and
// a RouterOS that is new enough, from two minutes ago.
func everythingUp() Facts {
	return Facts{
		Domestic: true, VPN: true, VPNKind: "wireguard", VPNLabel: "wg-vpn",
		RouteForeign: true, RouteVPN: true, RouteDomestic: true,
		ROS: "7.24.5", Updated: now.Add(-2 * time.Minute),
	}
}

func TestEverythingUpTurnsTheEntryOn(t *testing.T) {
	e := mustParse(t, goodReport()).Entry(now, MinRouterOS)
	if !e.On || e.Reason != "" {
		t.Fatalf("on = %v, reason = %q", e.On, e.Reason)
	}
	if e.Domestic != LinkOnline || e.VPN != (VPN{LinkOnline, "wireguard", "wg-vpn"}) {
		t.Errorf("domestic = %q, vpn = %+v", e.Domestic, e.VPN)
	}
	if e.RouterOS != (RouterOS{"7.24.5", "7.20", true}) {
		t.Errorf("routerOS = %+v", e.RouterOS)
	}
	if e.Stale || e.CheckedAt == nil || !e.CheckedAt.Equal(now.Add(-2*time.Minute)) {
		t.Errorf("stale = %v, checkedAt = %v", e.Stale, e.CheckedAt)
	}
	if e.Bridge != BridgeStopped {
		t.Errorf("bridge = %q: this package can't know the process, so it says nothing is running", e.Bridge)
	}
}

// Entry is on only when all of six things hold, so every one of the 64 ways
// they can come out is tried.
func TestTheEntryIsOnOnlyWhenEverythingIs(t *testing.T) {
	for bits := 0; bits < 1<<6; bits++ {
		on := func(i int) bool { return bits&(1<<i) != 0 }
		f := Facts{
			Domestic: on(0), VPN: on(1),
			RouteForeign: on(2), RouteVPN: on(3), RouteDomestic: on(4),
			ROS: "7.19", Updated: now,
		}
		if on(5) {
			f.ROS = "7.20"
		}
		e := f.Entry(now, MinRouterOS)

		want := bits == 1<<6-1
		if e.On != want {
			t.Errorf("%06b: on = %v, want %v (reason %q)", bits, e.On, want, e.Reason)
		}
		if (e.Reason == "") != e.On {
			t.Errorf("%06b: on = %v with reason %q", bits, e.On, e.Reason)
		}
		if e.RouterOS.OK != on(5) {
			t.Errorf("%06b: routerOS.ok = %v", bits, e.RouterOS.OK)
		}
		// each link's word follows its flag, and a link that is down is told apart from one that was never there
		word := func(up, table bool) string {
			switch {
			case up:
				return LinkOnline
			case table:
				return LinkOffline
			}
			return LinkMissing
		}
		if want := word(on(0), on(4)); e.Domestic != want {
			t.Errorf("%06b: domestic = %q, want %q", bits, e.Domestic, want)
		}
		if want := word(on(1), on(3)); e.VPN.State != want {
			t.Errorf("%06b: vpn = %q, want %q", bits, e.VPN.State, want)
		}
	}
}

func TestWhatTheRouterHasIsToldFromWhatIsDown(t *testing.T) {
	for name, c := range map[string]struct {
		f                Facts
		domestic, vpnNow string
	}{
		"nothing at all":                             {Facts{}, LinkMissing, LinkMissing},
		"up links need nothing else":                 {Facts{Domestic: true, VPN: true}, LinkOnline, LinkOnline},
		"a table without its link":                   {Facts{RouteDomestic: true, RouteVPN: true}, LinkOffline, LinkOffline},
		"a VPN with a name is there":                 {Facts{VPNLabel: "wg-vpn"}, LinkMissing, LinkOffline},
		"a VPN with a kind is there":                 {Facts{VPNKind: "l2tp"}, LinkMissing, LinkOffline},
		"a VPN with a table is there":                {Facts{RouteVPN: true}, LinkMissing, LinkOffline},
		"the VPN's table is not the Domestic link's": {Facts{RouteVPN: true, RouteForeign: true}, LinkMissing, LinkOffline},
		"a Domestic table is there":                  {Facts{RouteDomestic: true}, LinkOffline, LinkMissing},
		"the foreign table says nothing":             {Facts{RouteForeign: true}, LinkMissing, LinkMissing},
	} {
		e := c.f.Entry(now, MinRouterOS)
		if e.Domestic != c.domestic || e.VPN.State != c.vpnNow {
			t.Errorf("%s: domestic %q, vpn %q; want %q and %q", name, e.Domestic, e.VPN.State, c.domestic, c.vpnNow)
		}
	}
}

func TestTheVPNKindAndNameGoToThePageAsTheyAre(t *testing.T) {
	f := everythingUp()
	f.VPN, f.VPNKind, f.VPNLabel = false, "openvpn", "ovpn-out1"
	e := f.Entry(now, MinRouterOS)
	if e.VPN != (VPN{LinkOffline, "openvpn", "ovpn-out1"}) {
		t.Fatalf("vpn = %+v", e.VPN)
	}
	f.VPNKind, f.VPNLabel, f.RouteVPN = "", "", false
	if e = f.Entry(now, MinRouterOS); e.VPN != (VPN{LinkMissing, "", ""}) {
		t.Fatalf("vpn = %+v", e.VPN)
	}
}

func TestARouterOSThatIsTooOldKeepsTheEntryOff(t *testing.T) {
	for ros, wantOK := range map[string]bool{
		"7.20": true, "7.24.5": true, "7.21rc1": true, "7.21beta3": true,
		"7.19.2": false, "7.20rc1": false, "6.49.10": false, "": false, "garbage": false,
	} {
		f := everythingUp()
		f.ROS = ros
		e := f.Entry(now, MinRouterOS)
		if e.On != wantOK || e.RouterOS.OK != wantOK || e.RouterOS.Version != ros || e.RouterOS.Min != "7.20" {
			t.Errorf("ros %q: on = %v, routerOS = %+v", ros, e.On, e.RouterOS)
		}
	}
	// the minimum is the caller's: a different one gives a different answer
	f := everythingUp()
	if e := f.Entry(now, "7.30"); e.On || e.RouterOS.Min != "7.30" {
		t.Errorf("7.24.5 against a minimum of 7.30: on = %v, routerOS = %+v", e.On, e.RouterOS)
	}
	// and one that isn't a version never passes
	if e := f.Entry(now, "newest"); e.On || e.RouterOS.OK {
		t.Errorf("a minimum that isn't a version let the entry on: %+v", e)
	}
}

func TestAReportIsStaleAfterFifteenMinutes(t *testing.T) {
	at := func(age time.Duration) Facts {
		f := everythingUp()
		f.Updated = now.Add(-age)
		return f
	}
	for name, c := range map[string]struct {
		f     Facts
		stale bool
	}{
		"just written":             {at(0), false},
		"a minute old":             {at(time.Minute), false},
		"a second short":           {at(StaleAfter - time.Second), false},
		"exactly fifteen minutes":  {at(StaleAfter), false},
		"a second over":            {at(StaleAfter + time.Second), true},
		"an hour old":              {at(time.Hour), true},
		"a few seconds ahead":      {at(-5 * time.Second), false},
		"fifteen minutes ahead":    {at(-StaleAfter), false},
		"a second further ahead":   {at(-StaleAfter - time.Second), true},
		"far in the future":        {at(-10000 * time.Hour), true},
		"no time at all":           {func() Facts { f := everythingUp(); f.Updated = time.Time{}; return f }(), true},
		"the beginning of the era": {at(now.Sub(time.Unix(0, 0))), true},
	} {
		e := c.f.Entry(now, MinRouterOS)
		if e.Stale != c.stale {
			t.Errorf("%s: stale = %v, want %v", name, e.Stale, c.stale)
		}
		// an old report is a warning, not a reason to switch the entry off
		if !e.On {
			t.Errorf("%s: the entry went off over the report's age (%q)", name, e.Reason)
		}
	}

	// older than fifteen minutes means by any amount, so a fraction of a second counts
	f := at(StaleAfter)
	if e := f.Entry(now.Add(time.Nanosecond), MinRouterOS); !e.Stale {
		t.Error("a nanosecond over fifteen minutes is not stale")
	}
}

func TestCheckedAtIsWhenTheScriptWrote(t *testing.T) {
	f := everythingUp()
	f.Updated = time.Date(2026, 10, 30, 14, 30, 0, 0, time.FixedZone("+0330", 3*3600+1800)) // 11:00 UTC
	e := f.Entry(now, MinRouterOS)
	if e.CheckedAt == nil || e.CheckedAt.Location() != time.UTC || !e.CheckedAt.Equal(time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("checkedAt = %v", e.CheckedAt)
	}

	f.Updated = time.Time{}
	if e = f.Entry(now, MinRouterOS); e.CheckedAt != nil {
		t.Fatalf("a report with no time has checkedAt %v", e.CheckedAt)
	}
}

func TestTheRoutesTable(t *testing.T) {
	got := Facts{RouteForeign: true, RouteDomestic: true}.Routes()
	want := []Route{
		{"192.168.50.16", "downloads", "foreign", true},
		{"192.168.50.17", "entry-exit", "vpn", false},
		{"192.168.50.18", "nodes", "domestic", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	// each call is its own slice, which a caller may change
	got[0].Ready = false
	if !(Facts{RouteForeign: true}.Routes()[0].Ready) {
		t.Fatal("the routes share memory between calls")
	}
	if e := (Facts{RouteVPN: true}).Entry(now, MinRouterOS); !reflect.DeepEqual(e.Routes, Facts{RouteVPN: true}.Routes()) {
		t.Fatalf("the entry's routes: %+v", e.Routes)
	}
}

func TestWhyTheEntryIsOff(t *testing.T) {
	for name, c := range map[string]struct {
		mod  func(*Facts)
		want string
	}{
		"no VPN":             {func(f *Facts) { f.VPN, f.VPNKind, f.VPNLabel, f.RouteVPN = false, "", "", false }, "the router has no VPN"},
		"VPN down":           {func(f *Facts) { f.VPN = false }, "the VPN is down"},
		"no Domestic":        {func(f *Facts) { f.Domestic, f.RouteDomestic = false, false }, "the router has no Domestic link"},
		"Domestic down":      {func(f *Facts) { f.Domestic = false }, "the Domestic link is down"},
		"old RouterOS":       {func(f *Facts) { f.ROS = "7.19.2" }, "RouterOS 7.19.2 is older than the 7.20 this plugin needs"},
		"no version":         {func(f *Facts) { f.ROS = "" }, "the router did not report its RouterOS version"},
		"odd version":        {func(f *Facts) { f.ROS = "banana" }, `RouterOS "banana" can't be compared with the 7.20 this plugin needs`},
		"no downloads route": {func(f *Facts) { f.RouteForeign = false }, "the router's route for downloads (192.168.50.16) is not ready"},
		"no exit route":      {func(f *Facts) { f.RouteVPN = false }, "the router's route for entry-exit (192.168.50.17) is not ready"},
		"no nodes route":     {func(f *Facts) { f.RouteDomestic = false }, "the router's route for nodes (192.168.50.18) is not ready"},
	} {
		f := everythingUp()
		c.mod(&f)
		e := f.Entry(now, MinRouterOS)
		if e.On || e.Reason != c.want {
			t.Errorf("%s: on = %v, reason %q, want %q", name, e.On, e.Reason, c.want)
		}
	}

	// every problem is listed, the version first, and a down link isn't blamed twice
	f := Facts{ROS: "7.9", RouteForeign: true, Updated: now}
	want := "RouterOS 7.9 is older than the 7.20 this plugin needs; the router has no Domestic link; the router has no VPN"
	if e := f.Entry(now, MinRouterOS); e.Reason != want {
		t.Errorf("reason = %q\n want    %q", e.Reason, want)
	}
	f = everythingUp()
	f.VPN, f.RouteVPN, f.RouteForeign = false, false, false
	want = "the VPN is down; the router's route for downloads (192.168.50.16) is not ready"
	if e := f.Entry(now, MinRouterOS); e.Reason != want {
		t.Errorf("reason = %q\n want    %q", e.Reason, want)
	}
}

// The page codes against the contract's Entry, so its JSON has those keys and
// no others.
func TestTheEntryIsTheContractsJSON(t *testing.T) {
	keys := func(m any) []string {
		t.Helper()
		var out []string
		for k := range m.(map[string]any) {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	decode := func(e Entry) map[string]any {
		t.Helper()
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	m := decode(mustParse(t, goodReport()).Entry(now, MinRouterOS))
	if got, want := keys(m), []string{"bridge", "checkedAt", "domestic", "on", "routerOS", "routes", "stale", "vpn"}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if got, want := keys(m["vpn"]), []string{"kind", "label", "state"}; !slices.Equal(got, want) {
		t.Errorf("vpn keys = %v, want %v", got, want)
	}
	if got, want := keys(m["routerOS"]), []string{"min", "ok", "version"}; !slices.Equal(got, want) {
		t.Errorf("routerOS keys = %v, want %v", got, want)
	}
	routes := m["routes"].([]any)
	if len(routes) != 3 {
		t.Fatalf("routes = %v", routes)
	}
	if got, want := keys(routes[0]), []string{"address", "ready", "usedFor", "via"}; !slices.Equal(got, want) {
		t.Errorf("route keys = %v, want %v", got, want)
	}
	if m["checkedAt"] != "2026-10-30T11:58:00Z" || m["on"] != true || m["stale"] != false || m["domestic"] != "online" || m["bridge"] != "stopped" {
		t.Errorf("values: %v", m)
	}
	if v := m["vpn"].(map[string]any); v["state"] != "online" || v["kind"] != "wireguard" || v["label"] != "wg-vpn" {
		t.Errorf("vpn = %v", v)
	}
	if r := m["routerOS"].(map[string]any); r["version"] != "7.24.5" || r["min"] != "7.20" || r["ok"] != true {
		t.Errorf("routerOS = %v", r)
	}
	if r := routes[2].(map[string]any); !maps.Equal(r, map[string]any{"address": "192.168.50.18", "usedFor": "nodes", "via": "domestic", "ready": true}) {
		t.Errorf("last route = %v", r)
	}

	// with nothing to go on, the time is null and the lists are still lists
	m = decode(Facts{}.Entry(now, MinRouterOS))
	if v, ok := m["checkedAt"]; !ok || v != nil {
		t.Errorf("checkedAt = %v (present %v), want null", v, ok)
	}
	if routes = m["routes"].([]any); len(routes) != 3 {
		t.Errorf("routes = %v", routes)
	}
	if _, leaked := m["reason"]; leaked || strings.Contains(string(mustJSON(t, Facts{}.Entry(now, MinRouterOS))), "Reason") {
		t.Error("the reason is in the JSON, which the contract doesn't have")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
