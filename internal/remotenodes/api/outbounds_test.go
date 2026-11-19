// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// An outbound's login in the test hub: a VLESS id and a Trojan password,
// secrets like a node's private key.
const (
	outID       = "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f"
	outPassword = "MARKER-OUTBOUND-PASSWORD"
)

var outKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))

// addOutbounds gives a populated hub two outbounds: Frankfurt, which node 1
// leaves through, and Paris, which nothing does.
func (r *rig) addOutbounds() {
	r.t.Helper()
	err := r.st.Update(func(s *model.State) error {
		for _, o := range []model.Outbound{
			{Name: "Frankfurt", CreatedAt: now, Settings: model.OutboundSettings{
				Protocol: "vless", Host: "de.example.com", Port: 443, ID: outID, Flow: "xtls-rprx-vision",
				Network: "tcp", Security: "reality", ServerName: "www.example.com", Fingerprint: "chrome", PublicKey: outKey, ShortID: "ab12",
			}},
			{Name: "Paris", Fallback: true, CreatedAt: now, Settings: model.OutboundSettings{
				Protocol: "trojan", Host: "198.51.100.9", Port: 8443, Password: outPassword,
				Network: "ws", Path: "/ws", Security: "tls", ServerName: "cdn.example.com", ALPN: []string{"h2", "http/1.1"},
			}},
		} {
			if _, err := s.AddOutbound(o); err != nil {
				return err
			}
		}
		s.FindNode(1).OutboundID = 1
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

// fakeOutbounds stands in for the manager's side of the outbounds: it records
// every call as text and answers with the state's own view of the outbound,
// or the error the test set.
type fakeOutbounds struct {
	fakeSubs
	st Store
}

func (f *fakeOutbounds) view(id uint) (Outbound, error) {
	v, ok := ViewOutbound(f.st.Read(), id, nil)
	if !ok {
		return Outbound{}, ErrNoOutbound
	}
	return v, nil
}

func (f *fakeOutbounds) CreateOutbound(_ context.Context, req OutboundCreate) (Outbound, error) {
	if err := f.note("create %q fallback=%v %v", req.Name, req.Fallback, req.Settings); err != nil {
		return Outbound{}, err
	}
	return f.view(2)
}

func (f *fakeOutbounds) ChangeOutbound(_ context.Context, id uint, p OutboundPatch) (Outbound, error) {
	show := fmt.Sprintf("change %d", id)
	if p.Name != nil {
		show += fmt.Sprintf(" name=%q", *p.Name)
	}
	if p.Fallback != nil {
		show += fmt.Sprintf(" fallback=%v", *p.Fallback)
	}
	if p.Settings != nil {
		show += fmt.Sprintf(" settings=%v", *p.Settings)
	}
	if err := f.note("%s", show); err != nil {
		return Outbound{}, err
	}
	return f.view(id)
}

func (f *fakeOutbounds) RemoveOutbound(_ context.Context, id uint) error {
	return f.note("remove %d", id)
}

func (f *fakeOutbounds) TestOutbound(_ context.Context, id uint) (TestResult, error) {
	if err := f.note("test %d", id); err != nil {
		return TestResult{}, err
	}
	return TestResult{OK: true, DelayMs: 180, Status: 204}, nil
}

func (f *fakeOutbounds) TestOutboundSettings(_ context.Context, s OutboundSettings) (TestResult, error) {
	if err := f.note("test %v", s); err != nil {
		return TestResult{}, err
	}
	return TestResult{Error: "Context deadline exceeded."}, nil
}

func (f *fakeOutbounds) SetOutboundCheck(_ context.Context, url string) (OutboundCheck, error) {
	if err := f.note("check %q", url); err != nil {
		return OutboundCheck{}, err
	}
	return OutboundCheck{URL: url, Seconds: 30, DefaultURL: "https://www.google.com/generate_204"}, nil
}

func (f *fakeOutbounds) SetNodeRoute(_ context.Context, node, outbound uint) error {
	return f.note("route node %d via %d", node, outbound)
}

func (f *fakeOutbounds) SetLocalRoute(_ context.Context, outbound uint) (Local, error) {
	if err := f.note("route local via %d", outbound); err != nil {
		return Local{}, err
	}
	return Local{Enabled: true, State: LocalWorking, OutboundID: outbound}, nil
}

func withOutboundOps(o OutboundOps) func(*Config, *jobs.Engine) {
	return func(c *Config, _ *jobs.Engine) { c.Outbounds = o }
}

// outboundRig is a populated hub with two outbounds and a fake to carry out
// what changes them.
func outboundRig(t *testing.T, b string, mods ...func(*Config, *jobs.Engine)) (*rig, *fakeOutbounds) {
	t.Helper()
	fake := &fakeOutbounds{}
	r := newRigWith(t, b, withOps(append([]func(*Config, *jobs.Engine){withOutboundOps(fake)}, mods...)...))
	fake.st = r.st
	r.populate()
	r.addOutbounds()
	return r, fake
}

const settingsBody = `{"protocol":"trojan","host":"198.51.100.9","port":443,"password":"` + outPassword + `","security":"tls","serverName":"t.example.com"}`

// outboundRoutes is every call that changes something, for outbound 1 and
// node 1, with a body each takes.
func outboundRoutes() []subsReq {
	return []subsReq{
		{"POST", "/api/outbounds", `{"name":"Lyon","fallback":true,"settings":` + settingsBody + `}`},
		{"PATCH", "/api/outbounds/1", `{"name":"Berlin"}`},
		{"DELETE", "/api/outbounds/1", ""},
		{"POST", "/api/outbounds/1/test", ""},
		{"POST", "/api/outbounds/test", `{"settings":` + settingsBody + `}`},
		{"PUT", "/api/outbounds/check", `{"url":"https://example.com/204"}`},
		{"PUT", "/api/nodes/1/route", `{"outboundId":1}`},
		{"PUT", "/api/local/route", `{"outboundId":0}`},
	}
}

func TestWithoutOutboundOpsTheOutboundsCanOnlyBeRead(t *testing.T) {
	r := newRig(t, "")
	r.populate()
	r.addOutbounds()
	list := decode[OutboundList](t, r.get("/api/outbounds"))
	if len(list.Outbounds) != 2 || list.Limit != 16 {
		t.Fatalf("the list: %+v", list)
	}
	for _, c := range outboundRoutes() {
		code, _, body := r.do(c.method, c.path, c.body)
		wantError(t, c.method+" "+c.path, code, body, http.StatusNotFound, CodeNotFound)
	}
}

// The page is written against the contract, so these keys are its keys.
func TestTheOutboundsAreTheContractsShape(t *testing.T) {
	live := newFakeLive()
	r, _ := outboundRig(t, "", withLive(live))
	list := decode[map[string]any](t, r.get("/api/outbounds"))
	wantKeys(t, "the list", list, "outbounds limit check")
	wantKeys(t, "check", list["check"].(map[string]any), "url seconds defaultUrl")
	for _, o := range list["outbounds"].([]any) {
		o := o.(map[string]any)
		wantKeys(t, "outbound", o, "id name fallback createdAt protocol host port username method flow network path hostHeader mode security serverName fingerprint alpn publicKey shortId spiderX secretHint usedBy state")
		wantKeys(t, "outbound.usedBy", o["usedBy"].(map[string]any), "nodes router")
		wantKeys(t, "outbound.state", o["state"].(map[string]any), "status delayMs error onFallback lastTryAt lastSeenAt bytesUp bytesDown")
		if _, ok := o["alpn"].([]any); !ok {
			t.Errorf("alpn is %v, not a list", o["alpn"])
		}
		if _, ok := o["usedBy"].(map[string]any)["nodes"].([]any); !ok {
			t.Errorf("usedBy.nodes is not a list: %v", o["usedBy"])
		}
	}
	for _, c := range []struct {
		path string
		want string
	}{{"/api/nodes/1", "node"}, {"/api/local", "local"}} {
		if _, ok := decode[map[string]any](t, r.get(c.path))["outboundId"]; !ok {
			t.Errorf("the %s view has no outboundId", c.want)
		}
	}
	got := decode[map[string]any](t, func() []byte { _, _, b := r.do("POST", "/api/outbounds/1/test", ""); return b }())
	wantKeys(t, "a test that worked", got, "ok delayMs status")
	got = decode[map[string]any](t, func() []byte {
		_, _, b := r.do("POST", "/api/outbounds/test", `{"settings":`+settingsBody+`}`)
		return b
	}())
	wantKeys(t, "a test that didn't", got, "ok error")
}

func TestTheOutboundsCarryWhatTheStateHolds(t *testing.T) {
	live := newFakeLive()
	r, _ := outboundRig(t, "", withLive(live))
	list := decode[OutboundList](t, r.get("/api/outbounds"))
	if list.Check != (OutboundCheck{Seconds: 30, DefaultURL: "https://www.google.com/generate_204"}) || list.Limit != model.MaxOutbounds {
		t.Errorf("check %+v, limit %d", list.Check, list.Limit)
	}
	f, p := list.Outbounds[0], list.Outbounds[1]
	want := Outbound{
		ID: 1, Name: "Frankfurt", CreatedAt: now, Protocol: "vless", Host: "de.example.com", Port: 443, Flow: "xtls-rprx-vision",
		Network: "tcp", Security: "reality", ServerName: "www.example.com", Fingerprint: "chrome", ALPN: []string{}, PublicKey: outKey, ShortID: "ab12",
		SecretHint: "7f8b…4e5f", UsedBy: OutboundUsers{Nodes: []uint{1}}, State: OutboundState{Status: OutboundUnknown},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("Frankfurt\n got %+v\nwant %+v", f, want)
	}
	if p.SecretHint != "" || !p.Fallback || p.Network != "ws" || p.Path != "/ws" || !reflect.DeepEqual(p.ALPN, []string{"h2", "http/1.1"}) ||
		len(p.UsedBy.Nodes) != 0 || p.UsedBy.Router || p.State.Status != OutboundIdle {
		t.Errorf("Paris %+v", p)
	}
	// what the manager says wins over the view's own guess
	live.outs[1] = OutboundState{Status: OutboundUp, DelayMs: 120, LastSeenAt: at(-1), BytesUp: 9}
	if got := decode[OutboundList](t, r.get("/api/outbounds")).Outbounds[0].State; !reflect.DeepEqual(got, live.outs[1]) {
		t.Errorf("the live state: %+v", got)
	}
	if err := r.st.Update(func(s *model.State) error { s.ProbeURL = "https://example.com/204"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := decode[OutboundList](t, r.get("/api/outbounds")).Check.URL; got != "https://example.com/204" {
		t.Errorf("the check's address: %q", got)
	}
	// and the router's entry, switched on, leaves through one as a node does
	r.switchOnLocal()
	if err := r.st.Update(func(s *model.State) error { s.Local.OutboundID = 2; return nil }); err != nil {
		t.Fatal(err)
	}
	p = decode[OutboundList](t, r.get("/api/outbounds")).Outbounds[1]
	if !p.UsedBy.Router || p.State.Status != OutboundUnknown {
		t.Errorf("Paris, with the router through it: %+v", p)
	}
	if got := decode[Local](t, r.get("/api/local")); got.OutboundID != 2 {
		t.Errorf("the router's view: %+v", got.OutboundID)
	}
	if got := decode[Node](t, r.get("/api/nodes/1")); got.OutboundID != 1 {
		t.Errorf("node 1's view: %+v", got.OutboundID)
	}
}

func TestEveryOutboundCallReachesWhatCarriesItOut(t *testing.T) {
	for _, b := range []string{"", base} {
		r, fake := outboundRig(t, b)
		wantStatus := map[string]int{"POST /api/outbounds": http.StatusCreated, "DELETE /api/outbounds/1": http.StatusNoContent}
		for _, c := range outboundRoutes() {
			code, h, body := r.do(c.method, b+c.path, c.body)
			want := http.StatusOK
			if s, ok := wantStatus[c.method+" "+c.path]; ok {
				want = s
			}
			if code != want {
				t.Fatalf("%s %s = %d %s", c.method, c.path, code, body)
			}
			if code == http.StatusNoContent && (len(body) != 0 || h.Get("Cache-Control") != "no-store") {
				t.Errorf("a removal answers %q, cache %q", body, h.Get("Cache-Control"))
			}
		}
		// a patch of each field, and settings whole
		for _, body := range []string{`{"fallback":false}`, `{"settings":` + settingsBody + `}`} {
			if code, _, got := r.do("PATCH", b+"/api/outbounds/2", body); code != http.StatusOK {
				t.Fatalf("PATCH %s = %d %s", body, code, got)
			}
		}
		if code, _, body := r.do("PUT", b+"/api/outbounds/check", `{"url":""}`); code != http.StatusOK || decode[OutboundCheck](t, body).URL != "" {
			t.Fatalf("the check back to the default: %d %s", code, body)
		}
		trojan := `trojan to 198.51.100.9:443 over /tls (id none, password hidden)`
		want := []string{
			`create "Lyon" fallback=true ` + trojan, `change 1 name="Berlin"`, "remove 1", "test 1", "test " + trojan,
			`check "https://example.com/204"`, "route node 1 via 1", "route local via 0",
			"change 2 fallback=false", "change 2 settings=" + trojan, `check ""`,
		}
		if got := fake.called(); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls\n %q\nwant\n %q", got, want)
		}

		// the answers are the views
		_, _, body := r.do("PUT", b+"/api/nodes/1/route", `{"outboundId":2}`)
		if n := decode[Node](t, body); n.ID != 1 || n.Events == nil {
			t.Errorf("a node's route answers with the node: %s", body)
		}
		_, _, body = r.do("PUT", b+"/api/local/route", `{"outboundId":2}`)
		if l := decode[Local](t, body); l.OutboundID != 2 {
			t.Errorf("the router's route answers with the router: %s", body)
		}
		_, _, body = r.do("PATCH", b+"/api/outbounds/2", `{"name":"x"}`)
		if o := decode[Outbound](t, body); o.ID != 2 || o.Name != "Paris" {
			t.Errorf("a change answers with the outbound: %s", body)
		}
	}
}

func TestOutboundCallsRefuseWhatTheyCantUse(t *testing.T) {
	r, fake := outboundRig(t, "")
	for _, c := range []subsReq{
		{"POST", "/api/outbounds", ``},
		{"POST", "/api/outbounds", `{"name":"x","settings":{"protocol":"vless","port":"443"}}`},
		{"POST", "/api/outbounds", `{"name":"x","settings":{"protocol":"vless","uuid":"` + outID + `"}}`},
		{"POST", "/api/outbounds", `{"name":"x","tag":"out-1."}`},
		{"POST", "/api/outbounds", `{"name":"x"} {"name":"y"}`},
		{"PATCH", "/api/outbounds/1", `{}`},
		{"PATCH", "/api/outbounds/1", `{"name":"x","settings":null}` + "x"},
		{"PATCH", "/api/outbounds/1", `{"protocol":"vless"}`},
		{"POST", "/api/outbounds/test", `{}`},
		{"POST", "/api/outbounds/test", `{"protocol":"vless"}`},
		{"PUT", "/api/outbounds/check", `{}`},
		{"PUT", "/api/outbounds/check", `{"url":1}`},
		{"PUT", "/api/outbounds/check", `{"url":"","seconds":10}`},
		{"PUT", "/api/nodes/1/route", `{}`},
		{"PUT", "/api/nodes/1/route", `{"outboundId":-1}`},
		{"PUT", "/api/nodes/1/route", `{"outboundId":"1"}`},
		{"PUT", "/api/local/route", `{}`},
		{"PUT", "/api/local/route", `{"outbound":1}`},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		wantError(t, c.method+" "+c.path+" "+c.body, code, body, http.StatusBadRequest, CodeBadRequest)
	}
	for _, c := range []subsReq{
		{"PATCH", "/api/outbounds/9", `{"name":"x"}`},
		{"PATCH", "/api/outbounds/x", `{"name":"x"}`},
		{"DELETE", "/api/outbounds/9", ""},
		{"DELETE", "/api/outbounds/0", ""},
		{"DELETE", "/api/outbounds/check", ""},
		{"POST", "/api/outbounds/9/test", ""},
		// a path that names nothing is not found, whatever the body
		{"PATCH", "/api/outbounds/9", `{"nothing":true}`},
		{"PUT", "/api/nodes/9/route", `{"outboundId":1}`},
		{"PUT", "/api/nodes/9/route", `{}`},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		got := wantError(t, c.method+" "+c.path, code, body, http.StatusNotFound, CodeNotFound)
		if strings.Contains(c.path, "outbounds") && got["error"] != ErrNoOutbound.Msg {
			t.Errorf("%s %s says %q", c.method, c.path, got["error"])
		}
	}
	if got := fake.called(); len(got) != 0 {
		t.Fatalf("a refused request was carried out: %q", got)
	}
}

func TestOutboundCallsAnswerWithTheManagersCodes(t *testing.T) {
	r, fake := outboundRig(t, "")
	_, limit := func() (model.Outbound, error) {
		s := model.NewState()
		s.Outbounds = make([]model.Outbound, model.MaxOutbounds)
		return s.AddOutbound(model.Outbound{})
	}()
	for _, c := range []struct {
		err    error
		req    subsReq
		status int
		code   string
	}{
		{limit, outboundRoutes()[0], 409, CodeOutboundLimit},
		{fmt.Errorf("%w: %q", model.ErrNameTaken, "Lyon"), outboundRoutes()[0], 409, CodeNameTaken},
		{&model.InvalidError{Msg: "settings.serverName: \"x\" is not a host name"}, outboundRoutes()[0], 400, CodeBadRequest},
		{&Error{CodeOutboundInUse, "Frankfurt can't be removed while node 1 leaves through it."}, outboundRoutes()[2], 409, CodeOutboundInUse},
		{model.ErrOutboundInUse, outboundRoutes()[2], 409, CodeOutboundInUse},
		{&Error{CodeTestUnavailable, "This hub has no bridge program to try outbounds with."}, outboundRoutes()[3], 503, CodeTestUnavailable},
		{&Error{CodeTestUnavailable, "This hub has no bridge program to try outbounds with."}, outboundRoutes()[4], 503, CodeTestUnavailable},
		{ErrNoOutbound, outboundRoutes()[1], 404, CodeNotFound},
		{&model.InvalidError{Msg: "the check's url: \"ftp://x\" is not an http or https address"}, outboundRoutes()[5], 400, CodeBadRequest},
		{&model.InvalidError{Msg: "there is no outbound 9 to leave through"}, outboundRoutes()[6], 400, CodeBadRequest},
		{&model.InvalidError{Msg: "there is no outbound 9 to leave through"}, outboundRoutes()[7], 400, CodeBadRequest},
	} {
		fake.fail(c.err)
		code, _, body := r.do(c.req.method, c.req.path, c.req.body)
		got := wantError(t, c.req.method+" "+c.req.path, code, body, c.status, c.code)
		if msg, _ := got["error"].(string); !strings.HasSuffix(msg, ".") || msg[0] < 'A' || msg[0] > 'Z' {
			t.Errorf("%s: the sentence is %q", c.code, msg)
		}
	}
}

// secretHint is the contract's word for a few characters of an id, never the
// id; the test of names lets it through, and only it.
func TestTheHintIsTheOneSecretishNameAllowed(t *testing.T) {
	if secretish("secretHint") {
		t.Error("secretHint is taken for a secret")
	}
	for _, n := range []string{"secret", "secretKey", "hintSecret", "password", "id_secret"} {
		if !secretish(n) {
			t.Errorf("%q is not taken for a secret", n)
		}
	}
}

// No body the page reads carries an outbound's login, whether the view is
// the list's, a change's answer, or an error that repeated it.
func TestNoOutboundSecretReachesABrowser(t *testing.T) {
	for _, b := range []string{"", base} {
		r, fake := outboundRig(t, b, withLive(newFakeLive()))
		held := r.st.Read()
		if held.Outbounds[0].Settings.ID != outID || held.Outbounds[1].Settings.Password != outPassword {
			t.Fatal("the test hub does not hold the logins it is meant to hide")
		}
		bodies := map[string][]byte{}
		for _, p := range []string{"/api/outbounds", "/api/nodes", "/api/nodes/1", "/api/local", "/api/config"} {
			bodies[b+p] = r.get(b + p)
		}
		for _, c := range outboundRoutes() {
			_, _, bodies[c.method+" "+b+c.path] = r.do(c.method, b+c.path, c.body)
		}
		_, _, bodies["PATCH settings"] = r.do("PATCH", b+"/api/outbounds/2", `{"settings":`+settingsBody+`}`)
		// errors that repeat what was sent: the login is taken out on the way
		fake.fail(&model.InvalidError{Msg: "Xray does not take these settings: bad password " + outPassword})
		_, _, bodies["a refusal that repeats the password"] = r.do("POST", b+"/api/outbounds", `{"name":"x","settings":`+settingsBody+`}`)
		_, _, bodies["a test that repeats the password"] = r.do("POST", b+"/api/outbounds/test", `{"settings":`+settingsBody+`}`)
		fake.fail(&Error{CodeBadRequest, "the id " + outID + " is refused"})
		_, _, bodies["a change that repeats the id"] = r.do("PATCH", b+"/api/outbounds/1", `{"settings":{"protocol":"vless","host":"de.example.com","port":443,"id":"`+outID+`"}}`)

		if !strings.Contains(string(bodies[b+"/api/outbounds"]), "7f8b…4e5f") {
			t.Fatal("the id's hint should be shown")
		}
		for what, body := range bodies {
			for _, s := range []string{outID, outPassword} {
				if bytes.Contains(body, []byte(s)) {
					t.Errorf("%s carries %q: %s", what, s, body)
				}
			}
			var generic any
			if json.Unmarshal(body, &generic) == nil {
				walkKeys(generic, func(key string) {
					if secretish(key) {
						t.Errorf("%s has a key named %q", what, key)
					}
				})
			}
		}
		if strings.Contains(r.log.String(), outPassword) || strings.Contains(r.log.String(), outID) {
			t.Errorf("the log has a login:\n%s", r.log)
		}
	}
}
