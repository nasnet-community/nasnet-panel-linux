// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The router's own entry in the test hub: its keys and the id of the one
// subscription that is switched on for it. The private key and the id are
// secrets like a node's.
var (
	localPrivateKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xa5}, 32))
	localPublicKey  = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 32))
)

const (
	localUUID    = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	localShortID = "89abcdef01234567"
)

// switchOnLocal switches the entry on in a populated hub, with subscription 1
// on it and subscription 2 switched off there.
func (r *rig) switchOnLocal() {
	r.t.Helper()
	err := r.st.Update(func(s *model.State) error {
		s.Local = model.Local{
			Enabled: true, RealityTarget: "www.example.org:443",
			RealityPublicKey: localPublicKey, RealityShortID: localShortID, RealityPrivateKey: localPrivateKey,
		}
		s.LocalClients = []model.LocalClient{
			{SubscriptionID: 1, ClientUUID: localUUID, Enabled: true},
			{SubscriptionID: 2, ClientUUID: "9a8b7c6d-5e4f-4a3b-8c2d-000000000002", Enabled: false},
		}
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

// workingLocal is what a manager says of an entry that works.
func workingLocal() LocalLive {
	return LocalLive{
		State: LocalWorking, CloudName: "a1b2c3d4e5f6.sn.mynetname.net", PublicAddr: "198.51.100.24", DomesticAddr: "198.51.100.24",
		Port: 41873, UptimeSec: 3600,
		Usage: NodeUsage{Live: true, Online: 1, Connections: 2, RateUp: 3, RateDown: 4, TodayUp: 5, TodayDown: 6, MonthUp: 7, MonthDown: 8, UpdatedAt: at(-1)},
	}
}

// fakeLocal stands in for the manager's side of the entry: it records every
// call as text and answers with a view of its own, or the error the test set.
type fakeLocal struct{ fakeSubs }

func (f *fakeLocal) answer(format string, a ...any) (Local, error) {
	if err := f.note(format, a...); err != nil {
		return Local{}, err
	}
	return Local{Enabled: true, State: LocalWorking}, nil
}

func (f *fakeLocal) SetLocalEnabled(_ context.Context, on bool) (Local, error) {
	return f.answer("enable %v", on)
}

func (f *fakeLocal) ChangeLocalSettings(_ context.Context, s LocalSettings) (Local, error) {
	show := func(p *string) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprintf("%q", *p)
	}
	return f.answer("settings host=%s target=%s sni=%s", show(s.Host), show(s.HandshakeTarget), show(s.SNI))
}

func (f *fakeLocal) NewLocalPort(context.Context) (Local, error)    { return f.answer("new-port") }
func (f *fakeLocal) RotateLocalKeys(context.Context) (Local, error) { return f.answer("rotate-keys") }

func (f *fakeLocal) CheckLocalTarget(_ context.Context, target string) (HandshakeCheck, error) {
	if err := f.note("check %q", target); err != nil {
		return HandshakeCheck{}, err
	}
	return HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 4000, LatencyMs: 20}, nil
}

func (f *fakeLocal) SetLocalSubscription(_ context.Context, sub uint, on bool) (Subscription, error) {
	if err := f.note("switch %d %v", sub, on); err != nil {
		return Subscription{}, err
	}
	v := viewOf(sub, "sara", false)
	v.Local = &SubscriptionLocal{Enabled: on}
	return v, nil
}

func (f *fakeLocal) LocalLink(sub uint) (Link, error) {
	if err := f.note("link %d", sub); err != nil {
		return Link{}, err
	}
	return Link{Local: true, Vless: "vless://for-the-router"}, nil
}

func (f *fakeLocal) ReplaceLocalLink(_ context.Context, sub uint) (Link, error) {
	if err := f.note("replace %d", sub); err != nil {
		return Link{}, err
	}
	return Link{Local: true, Vless: "vless://for-the-router-again"}, nil
}

func withLocalOps(l LocalOps) func(*Config, *jobs.Engine) {
	return func(c *Config, _ *jobs.Engine) { c.Local = l }
}

// An entry that was never switched on reads as off, and says nothing more.
func TestTheEntryOffReadsAsOff(t *testing.T) {
	r := newRigWith(t, "", withLive(newFakeLive()))
	r.populate()
	got := decode[Local](t, r.get("/api/local"))
	want := Local{State: LocalOff, Events: &[]NodeEvent{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	cfg := decode[AppConfig](t, r.get("/api/config"))
	if cfg.Local != (LocalBrief{State: LocalOff}) {
		t.Errorf("config says %+v", cfg.Local)
	}
	for _, s := range decode[[]Subscription](t, r.get("/api/subscriptions")) {
		if s.Local != nil {
			t.Errorf("%s has a place on an entry that is off: %+v", s.Name, s.Local)
		}
	}
	if !strings.Contains(string(r.get("/api/subscriptions")), `"local": null`) && !strings.Contains(string(r.get("/api/subscriptions")), `"local":null`) {
		t.Error("a subscription should say local is null, not leave it out")
	}
}

func TestTheEntryOnShowsWhatTheStateAndTheManagerKnow(t *testing.T) {
	live := newFakeLive()
	live.xray, live.local = "26.10.21", workingLocal()
	live.localEv = []NodeEvent{{At: *at(-5), Kind: "local_on"}}
	live.on[[2]uint{1, RouterID}] = SubscriptionUsage{Live: true, Online: true, Connections: 2, LastSeenAt: at(-1), TodayUp: 5, TodayDown: 6, MonthUp: 7, MonthDown: 8}
	live.on[[2]uint{2, RouterID}] = SubscriptionUsage{Live: true, MonthUp: 1}
	r := newRigWith(t, "", withLive(live))
	r.populate()
	r.switchOnLocal()

	got := decode[Local](t, r.get("/api/local"))
	port := 41873
	want := Local{
		Enabled: true, State: LocalWorking,
		Host: "a1b2c3d4e5f6.sn.mynetname.net", HostSource: "cloud", CloudName: "a1b2c3d4e5f6.sn.mynetname.net",
		PublicAddr: "198.51.100.24", DomesticAddr: "198.51.100.24", Port: &port,
		HandshakeTarget: "www.example.org:443", PublicKey: localPublicKey, ShortID: localShortID,
		SubscriptionCount: 1, Usage: workingLocal().Usage, XrayVersion: "26.10.21", UptimeSec: 3600,
		Events: &[]NodeEvent{{At: *at(-5), Kind: "local_on"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if cfg := decode[AppConfig](t, r.get("/api/config")); cfg.Local != (LocalBrief{Enabled: true, State: LocalWorking}) {
		t.Errorf("config says %+v", cfg.Local)
	}

	subs := decode[[]Subscription](t, r.get("/api/subscriptions"))
	on, conns := true, 2
	first := SubscriptionLocal{Enabled: true, Online: &on, Connections: &conns, LastSeenAt: at(-1), TodayUp: 5, TodayDown: 6, MonthUp: 7, MonthDown: 8}
	if subs[0].Local == nil || !reflect.DeepEqual(*subs[0].Local, first) {
		t.Errorf("the subscription that is on it: %+v", subs[0].Local)
	}
	// switched off there: its past use stays, and whether it is connected isn't a question
	if l := subs[1].Local; l == nil || l.Enabled || l.Online != nil || l.Connections != nil || l.MonthUp != 1 {
		t.Errorf("the subscription that is off on it: %+v", l)
	}
	if subs[0].Online == nil || !*subs[0].Online {
		t.Errorf("a subscription connected to the router alone should read as online: %v", subs[0].Online)
	}

	rows := decode[[]NodeSubscription](t, r.get("/api/local/subscriptions"))
	if len(rows) != 2 || !rows[0].Enabled || rows[0].Online == nil || !*rows[0].Online || rows[0].TodayUp != 5 || rows[1].Enabled || rows[1].Online != nil {
		t.Errorf("the entry's own list: %+v", rows)
	}
	if rows[0].TrafficLimit != 50<<30 || rows[0].ExpiresAt == nil {
		t.Errorf("the entry's list leaves out the subscription's own limits: %+v", rows[0])
	}

	live.traffic[RouterID] = Traffic{StepSec: 5, Samples: []TrafficSample{{At: *at(-1), Up: 7, Down: 8}}}
	if tr := decode[Traffic](t, r.get("/api/local/traffic")); tr.StepSec != 5 || len(tr.Samples) != 1 || tr.Samples[0].Down != 8 {
		t.Errorf("the entry's chart: %+v", tr)
	}
}

// What the links carry: the name set by hand before the router's own, and
// neither when there is neither. A public address that is not the link's own
// means another router in front.
func TestTheEntrysAddressAndWhereItComesFrom(t *testing.T) {
	live := newFakeLive()
	live.local = workingLocal()
	r := newRigWith(t, "", withLive(live))
	r.populate()
	r.switchOnLocal()

	live.local.PublicAddr, live.local.DomesticAddr = "198.51.100.24", "192.168.1.20"
	if got := decode[Local](t, r.get("/api/local")); !got.BehindNAT {
		t.Error("a public address that isn't the link's own should read as behind another router")
	}
	live.local.PublicAddr = ""
	if got := decode[Local](t, r.get("/api/local")); got.BehindNAT {
		t.Error("with no public address known there is nothing to compare")
	}

	if err := r.st.Update(func(s *model.State) error { s.Local.Host = "home.example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := decode[Local](t, r.get("/api/local")); got.Host != "home.example.com" || got.HostSource != "own" || got.OwnHost != "home.example.com" {
		t.Errorf("a name set by hand: %q from %q", got.Host, got.HostSource)
	}

	live.local.CloudName, live.local.Port = "", 0
	if err := r.st.Update(func(s *model.State) error { s.Local.Host = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	got := decode[Local](t, r.get("/api/local"))
	if got.Host != "" || got.HostSource != "" || got.Port != nil {
		t.Errorf("with no name and no port: host %q from %q, port %v", got.Host, got.HostSource, got.Port)
	}
	if !strings.Contains(string(r.get("/api/local")), `"port": null`) && !strings.Contains(string(r.get("/api/local")), `"port":null`) {
		t.Error("no port should be null, not 0")
	}
}

// With no manager the entry can still be read: on is on its way, and no more.
func TestTheEntryWithNothingRunning(t *testing.T) {
	r := newRig(t, "")
	r.populate()
	r.switchOnLocal()
	got := decode[Local](t, r.get("/api/local"))
	if !got.Enabled || got.State != LocalStarting || got.Port != nil || got.Host != "" || got.Events == nil || len(*got.Events) != 0 {
		t.Fatalf("got %+v", got)
	}
	if tr := string(r.get("/api/local/traffic")); !strings.Contains(tr, `"samples":[]`) && !strings.Contains(tr, `"samples": []`) {
		t.Errorf("the chart with nothing running: %s", tr)
	}
	// and it can't be changed
	for _, c := range localRoutes() {
		if code, _, body := r.do(c.method, c.path, c.body); code != http.StatusNotFound {
			t.Errorf("%s %s with nothing to carry it out: %d %s", c.method, c.path, code, body)
		}
	}
}

// localRoutes is every call that changes the entry, for subscription 3.
func localRoutes() []subsReq {
	return []subsReq{
		{"PUT", "/api/local", `{"enabled":true}`},
		{"PATCH", "/api/local/settings", `{"host":"home.example.com"}`},
		{"POST", "/api/local/new-port", ""},
		{"POST", "/api/local/rotate-keys", ""},
		{"POST", "/api/local/handshake-check", `{"target":"www.example.com"}`},
		{"PUT", "/api/subscriptions/3/local", `{"enabled":true}`},
		{"GET", "/api/subscriptions/3/local/link", ""},
		{"POST", "/api/subscriptions/3/local/replace-link", ""},
	}
}

func TestTheEntrysCallsReachWhatCarriesThemOut(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := &fakeLocal{}
		r := newRigWith(t, b, withLocalOps(fake))
		r.populate()
		for _, c := range localRoutes() {
			if code, _, body := r.do(c.method, b+c.path, c.body); code != http.StatusOK {
				t.Fatalf("%s %s = %d %s", c.method, c.path, code, body)
			}
		}
		// a host cleared, a target left to the router, an SNI given
		if code, _, body := r.do("PATCH", b+"/api/local/settings", `{"host":"","handshakeTarget":"","sni":"example.com"}`); code != http.StatusOK {
			t.Fatalf("settings = %d %s", code, body)
		}
		if code, _, body := r.do("PUT", b+"/api/local", `{"enabled":false}`); code != http.StatusOK {
			t.Fatalf("off = %d %s", code, body)
		}
		want := []string{
			"enable true", `settings host="home.example.com" target=- sni=-`, "new-port", "rotate-keys", `check "www.example.com"`,
			"switch 3 true", "link 3", "replace 3", `settings host="" target="" sni="example.com"`, "enable false",
		}
		if got := fake.called(); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls %q\nwant  %q", got, want)
		}

		// a link is the one answer that holds a secret, and it is not kept by anything on the way
		_, h, body := r.do("GET", b+"/api/subscriptions/3/local/link", "")
		if got := decode[Link](t, body); !got.Local || got.Vless != "vless://for-the-router" || h.Get("Cache-Control") != "no-store" {
			t.Errorf("link %+v, cache %q", got, h.Get("Cache-Control"))
		}
		if strings.Contains(r.log.String(), "vless://") {
			t.Error("a link reached the log")
		}
	}
}

func TestTheEntrysCallsRefuseWhatTheyCantUse(t *testing.T) {
	fake := &fakeLocal{}
	r := newRigWith(t, "", withLocalOps(fake))
	r.populate()
	for _, c := range []subsReq{
		{"PUT", "/api/local", `{}`},
		{"PUT", "/api/local", `{"enabled":"yes"}`},
		{"PUT", "/api/local", `{"enabled":true,"port":443}`},
		{"PATCH", "/api/local/settings", `{}`},
		{"PATCH", "/api/local/settings", `{"port":443}`},
		{"POST", "/api/local/handshake-check", `{"site":"www.example.com"}`},
		{"PUT", "/api/subscriptions/3/local", `{}`},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		wantError(t, c.method+" "+c.path+" "+c.body, code, body, http.StatusBadRequest, CodeBadRequest)
	}
	for _, path := range []string{"/api/subscriptions/0/local", "/api/subscriptions/x/local"} {
		code, _, body := r.do("PUT", path, `{"enabled":true}`)
		wantError(t, path, code, body, http.StatusNotFound, CodeNotFound)
	}
	if got := fake.called(); len(got) != 0 {
		t.Fatalf("a refused request was carried out: %q", got)
	}

	// what the manager refuses comes back with its code
	fake.fail(ErrLocalOff)
	code, _, body := r.do("POST", "/api/local/new-port", "")
	wantError(t, "new port while off", code, body, http.StatusConflict, CodeLocalOff)
	fake.fail(&Error{CodeNoAddress, "There is no name to put in the link."})
	code, _, body = r.do("GET", "/api/subscriptions/3/local/link", "")
	wantError(t, "a link with no name", code, body, http.StatusConflict, CodeNoAddress)
}

// A new subscription can be switched on for the entry as it is made. If the
// entry is off it is refused before anything is made.
func TestANewSubscriptionCanStartOnTheEntry(t *testing.T) {
	subs, local := &fakeSubs{}, &fakeLocal{}
	r := newRigWith(t, "", withOps(withSubs(subs), withLocalOps(local)))
	r.populate()

	code, _, body := r.do("POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"local":true}`)
	wantError(t, "on an entry that is off", code, body, http.StatusConflict, CodeLocalOff)
	if len(subs.called())+len(local.called()) != 0 {
		t.Fatalf("something was made for it: %q %q", subs.called(), local.called())
	}

	r.switchOnLocal()
	code, _, body = r.do("POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[1],"local":true}`)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	if got := decode[Subscription](t, body); got.Local == nil || !got.Local.Enabled {
		t.Errorf("the answer is the subscription before it was switched on: %+v", got)
	}
	if got := subs.called(); !reflect.DeepEqual(got, []string{`create "Sara" [1]`}) {
		t.Errorf("subscription calls %q", got)
	}
	if got := local.called(); !reflect.DeepEqual(got, []string{"switch 7 true"}) {
		t.Errorf("entry calls %q", got)
	}

	// without the flag the entry is left alone
	if code, _, body = r.do("POST", "/api/subscriptions", `{"name":"Ali","nodeIds":[1]}`); code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	if got := local.called(); len(got) != 1 {
		t.Errorf("entry calls %q", got)
	}
}
