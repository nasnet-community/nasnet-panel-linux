// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
)

const outPassword = "trojan-password-marker"

// fakeProber stands in for the bridge program: it keeps every request and
// answers with what the test set.
type fakeProber struct {
	mu   sync.Mutex
	reqs []bridgespec.ProbeRequest
	res  bridgespec.ProbeResult
	err  error
}

func (p *fakeProber) Probe(_ context.Context, req bridgespec.ProbeRequest) (bridgespec.ProbeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	return p.res, p.err
}

func (p *fakeProber) answer(res bridgespec.ProbeResult, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.res, p.err = res, err
}

func (p *fakeProber) asked() []bridgespec.ProbeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.reqs)
}

// apiOut is realityOut as the page sends it.
func apiOut() api.OutboundSettings {
	s := realityOut()
	return api.OutboundSettings{
		Protocol: s.Protocol, Host: s.Host, Port: s.Port, ID: s.ID, Flow: s.Flow, Network: s.Network,
		Security: s.Security, ServerName: s.ServerName, PublicKey: s.PublicKey, ShortID: s.ShortID,
	}
}

// trojanAPI is a Trojan outbound by address, as the page sends it.
func trojanAPI() api.OutboundSettings {
	return api.OutboundSettings{Protocol: "trojan", Host: "198.51.100.9", Port: 443, Password: outPassword, Security: "tls", ServerName: "trojan.example.com"}
}

// outRig is a manager whose outbounds are tried by a fake program, with the
// three addresses held and a resolver that knows de.example.com.
type outRig struct {
	*rig
	probe *fakeProber
	dns   *fakeResolver
}

func newOutRig(t *testing.T, mods ...func(*Config)) *outRig {
	t.Helper()
	p, rs := &fakeProber{res: bridgespec.ProbeResult{OK: true}}, &fakeResolver{}
	rs.set("de.example.com", "198.51.100.7")
	setup := func(c *Config) { c.Prober, c.OutboundResolver, c.Addrs = p, rs, heldAddrs() }
	return &outRig{rig: newRig(t, append([]func(*Config){setup}, mods...)...), probe: p, dns: rs}
}

func addOutbound(t testing.TB, st Store, name string, s model.OutboundSettings) model.Outbound {
	t.Helper()
	var out model.Outbound
	must(t, st.Update(func(st *model.State) error {
		var err error
		out, err = st.AddOutbound(model.Outbound{Name: name, CreatedAt: epoch, Settings: s})
		return err
	}))
	return out
}

func outOf(st Store, id uint) model.Outbound {
	s := st.Read()
	return *s.FindOutbound(id)
}

func route(t testing.TB, st Store, node, outbound uint) {
	t.Helper()
	editNode(t, st, node, func(n *model.Node) { n.OutboundID = outbound })
}

func TestAnOutboundIsBuiltByXrayBeforeItIsKept(t *testing.T) {
	r := newOutRig(t)
	v, err := r.m.CreateOutbound(context.Background(), api.OutboundCreate{Name: "  Frankfurt ", Settings: apiOut()})
	must(t, err)
	if v.ID != 1 || v.Name != "Frankfurt" || v.Network != "tcp" || v.Security != "reality" || v.SecretHint != "7f8b…4e5f" ||
		v.State.Status != api.OutboundIdle || len(v.UsedBy.Nodes) != 0 || v.UsedBy.Router || !v.CreatedAt.Equal(epoch) {
		t.Fatalf("the answer: %+v", v)
	}
	asked := r.probe.asked()
	if len(asked) != 1 {
		t.Fatalf("%d probes", len(asked))
	}
	req := asked[0]
	if req.Connect || req.From != "192.168.150.80" || req.Outbound.Addr != "198.51.100.7" || req.Outbound.Tag != "" || req.Outbound.ID != outUUID || req.URL != "" {
		t.Errorf("the build check asked for %+v", req)
	}
	st := r.cfg.Store.Read()
	if len(st.Outbounds) != 1 || !reflect.DeepEqual(st.Outbounds[0].Settings, realityOut()) {
		t.Errorf("the state holds %+v", st.Outbounds)
	}
	// its host stays a name in the state: the bridge is given an address each time
	if st.Outbounds[0].Settings.Host != "de.example.com" {
		t.Errorf("the host was kept as %q", st.Outbounds[0].Settings.Host)
	}
	// and a Trojan one by address needs no lookup
	before := r.dns.lookups()
	v, err = r.m.CreateOutbound(context.Background(), api.OutboundCreate{Name: "Paris", Fallback: true, Settings: trojanAPI()})
	must(t, err)
	if v.SecretHint != "" || !v.Fallback || v.Network != "tcp" || len(v.ALPN) != 0 || v.ALPN == nil || r.dns.lookups() != before {
		t.Errorf("a password's hint, or a lookup made: %+v", v)
	}
}

func TestWhatKeepsAnOutboundFromBeingKept(t *testing.T) {
	for name, c := range map[string]struct {
		mod     func(*api.OutboundCreate, *outRig)
		probed  bool
		code    string // the API code it comes to
		says    string
		kept    bool
		notSaid string
	}{
		"a name that is none":      {mod: func(req *api.OutboundCreate, _ *outRig) { req.Name = "" }, code: api.CodeBadRequest, says: "the name is required"},
		"a name too long":          {mod: func(req *api.OutboundCreate, _ *outRig) { req.Name = strings.Repeat("a", 41) }, code: api.CodeBadRequest, says: "40"},
		"settings the hub refuses": {mod: func(req *api.OutboundCreate, _ *outRig) { req.Settings.ServerName = "not a name" }, code: api.CodeBadRequest, says: "settings.serverName"},
		"a host that won't resolve": {mod: func(req *api.OutboundCreate, r *outRig) {
			r.dns.fail("de.example.com", errors.New("no such host"))
		}, code: api.CodeBadRequest, says: "settings.host: de.example.com does not resolve from the router's Foreign link: no such host"},
		"an address of the other family": {mod: func(req *api.OutboundCreate, _ *outRig) { req.Settings.Host = "2001:db8::9" }, code: api.CodeBadRequest, says: "address family"},
		"settings Xray refuses": {mod: func(_ *api.OutboundCreate, r *outRig) {
			r.probe.answer(bridgespec.ProbeResult{Error: "Xray does not accept these settings: invalid user " + outUUID + "."}, nil)
		}, probed: true, code: api.CodeBadRequest, says: "Xray does not accept these settings: invalid user …", notSaid: outUUID},
		"a program that fails": {mod: func(_ *api.OutboundCreate, r *outRig) {
			r.probe.answer(bridgespec.ProbeResult{}, errors.New("the bridge program did not say how the try went (exit status 2)"))
		}, probed: true, code: api.CodeTestUnavailable, says: "could not be checked"},
		"no program here": {mod: func(_ *api.OutboundCreate, r *outRig) {
			r.probe.answer(bridgespec.ProbeResult{}, fmt.Errorf("%w: not found", bridgectl.ErrNoProgram))
		}, probed: true, kept: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := newOutRig(t)
			req := api.OutboundCreate{Name: "Frankfurt", Settings: apiOut()}
			c.mod(&req, r)
			_, err := r.m.CreateOutbound(context.Background(), req)
			if got := len(r.probe.asked()) > 0; got != c.probed {
				t.Errorf("probed %v, want %v", got, c.probed)
			}
			if kept := len(r.cfg.Store.Read().Outbounds) == 1; kept != c.kept {
				t.Fatalf("kept %v, want %v (err %v)", kept, c.kept, err)
			}
			if c.kept {
				must(t, err)
				return
			}
			if got := codeOf(err); got != c.code || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("err = %v (%s), want %s saying %q", err, got, c.code, c.says)
			}
			if c.notSaid != "" && strings.Contains(err.Error(), c.notSaid) {
				t.Errorf("the error repeats the login: %v", err)
			}
		})
	}
}

// codeOf is the API code an error comes to: an *api.Error's own, or the one
// the API gives the model's errors.
func codeOf(err error) string {
	var inv *model.InvalidError
	switch {
	case err == nil:
		return ""
	case apiCode(err) != "":
		return apiCode(err)
	case errors.As(err, &inv):
		return api.CodeBadRequest
	case errors.Is(err, model.ErrNameTaken):
		return api.CodeNameTaken
	case errors.Is(err, model.ErrOutboundLimit):
		return api.CodeOutboundLimit
	case errors.Is(err, model.ErrNotFound):
		return api.CodeNotFound
	}
	return "internal"
}

func TestATakenNameAndAFullListAreRefusedBeforeTheProgramIsAsked(t *testing.T) {
	r := newOutRig(t)
	addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
	if _, err := r.m.CreateOutbound(context.Background(), api.OutboundCreate{Name: "FRANKFURT", Settings: apiOut()}); codeOf(err) != api.CodeNameTaken {
		t.Fatalf("a taken name: %v", err)
	}
	for i := 2; i <= model.MaxOutbounds; i++ {
		addOutbound(t, r.cfg.Store, fmt.Sprint("out ", i), realityOut())
	}
	if _, err := r.m.CreateOutbound(context.Background(), api.OutboundCreate{Name: "one more", Settings: apiOut()}); codeOf(err) != api.CodeOutboundLimit {
		t.Fatalf("past the limit: %v", err)
	}
	if n := len(r.probe.asked()); n != 0 {
		t.Errorf("the program was asked %d times", n)
	}
}

func TestChangingAnOutbound(t *testing.T) {
	r := newOutRig(t)
	o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
	addOutbound(t, r.cfg.Store, "Paris", wsOut())
	ctx := context.Background()

	name, on := "Berlin", true
	v, err := r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{Name: &name, Fallback: &on})
	must(t, err)
	if v.Name != "Berlin" || !v.Fallback || len(r.probe.asked()) != 0 {
		t.Fatalf("renamed: %+v, %d probes", v, len(r.probe.asked()))
	}
	taken := "paris"
	if _, err := r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{Name: &taken}); codeOf(err) != api.CodeNameTaken {
		t.Fatalf("a taken name: %v", err)
	}
	if _, err := r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{}); codeOf(err) != api.CodeBadRequest {
		t.Fatalf("nothing to change: %v", err)
	}
	if _, err := r.m.ChangeOutbound(ctx, 9, api.OutboundPatch{Name: &name}); err != api.ErrNoOutbound {
		t.Fatalf("one nobody has: %v", err)
	}

	// new settings replace all of them, the login too, once Xray builds them
	next := trojanAPI()
	v, err = r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{Settings: &next})
	must(t, err)
	got := outOf(r.cfg.Store, o.ID).Settings
	if v.Protocol != "trojan" || got.ID != "" || got.Password != outPassword || got.Host != "198.51.100.9" || len(r.probe.asked()) != 1 || r.probe.asked()[0].Connect {
		t.Fatalf("new settings: view %+v, state %+v, probes %+v", v, got, r.probe.asked())
	}
	r.probe.answer(bridgespec.ProbeResult{Error: "no"}, nil)
	back := apiOut()
	if _, err := r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{Settings: &back, Name: &name}); codeOf(err) != api.CodeBadRequest {
		t.Fatalf("settings Xray refuses: %v", err)
	}
	if cur := outOf(r.cfg.Store, o.ID).Settings; cur.Protocol != "trojan" {
		t.Errorf("refused settings were kept: %+v", cur)
	}
}

func TestAnOutboundSomethingLeavesThroughStays(t *testing.T) {
	r := newOutRig(t)
	for range 2 {
		addNode(t, r.cfg.Store)
	}
	o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
	route(t, r.cfg.Store, 1, o.ID)
	route(t, r.cfg.Store, 2, o.ID)
	must(t, r.cfg.Store.Update(func(s *model.State) error { s.Local.OutboundID = o.ID; return nil }))
	ctx := context.Background()

	err := r.m.RemoveOutbound(ctx, o.ID)
	want := "Frankfurt can't be removed while node 1, node 2 and This router leave through it. Choose another way out for them first."
	if apiCode(err) != api.CodeOutboundInUse || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
	must(t, r.m.SetNodeRoute(ctx, 1, 0))
	_, err = r.m.SetLocalRoute(ctx, 0)
	must(t, err)
	err = r.m.RemoveOutbound(ctx, o.ID)
	if want := "Frankfurt can't be removed while node 2 leaves through it. Choose another way out for it first."; err == nil || err.Error() != want {
		t.Fatalf("err = %v\nwant  %s", err, want)
	}
	must(t, r.m.SetNodeRoute(ctx, 2, 0))
	must(t, r.m.RemoveOutbound(ctx, o.ID))
	if len(r.cfg.Store.Read().Outbounds) != 0 {
		t.Fatal("it is still there")
	}
	if err := r.m.RemoveOutbound(ctx, o.ID); err != api.ErrNoOutbound {
		t.Fatalf("again: %v", err)
	}
}

func TestTestingAnOutbound(t *testing.T) {
	r := newOutRig(t)
	o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
	must(t, r.cfg.Store.Update(func(s *model.State) error { s.ProbeURL = "https://example.com/204"; return nil }))
	ctx := context.Background()

	r.probe.answer(bridgespec.ProbeResult{OK: true, DelayMS: 180, Status: 204}, nil)
	got, err := r.m.TestOutbound(ctx, o.ID)
	must(t, err)
	if got != (api.TestResult{OK: true, DelayMs: 180, Status: 204}) {
		t.Fatalf("got %+v", got)
	}
	req := r.probe.asked()[0]
	if !req.Connect || req.URL != "https://example.com/204" || req.Outbound.Tag != "out-1." || req.Outbound.Addr != "198.51.100.7" || req.From != "192.168.150.80" {
		t.Errorf("the test asked for %+v", req)
	}

	for name, c := range map[string]struct {
		res  bridgespec.ProbeResult
		err  error
		want api.TestResult
		code string
	}{
		"no answer through it":  {res: bridgespec.ProbeResult{Error: "context deadline exceeded for " + outUUID}, want: api.TestResult{Error: "Context deadline exceeded for …."}},
		"a program that failed": {err: errors.New("the bridge program took longer than 15s to try the outbound, and was stopped"), want: api.TestResult{Error: "The bridge program took longer than 15s to try the outbound, and was stopped."}},
		"no program here":       {err: bridgectl.ErrNoProgram, code: api.CodeTestUnavailable},
	} {
		r.probe.answer(c.res, c.err)
		got, err := r.m.TestOutbound(ctx, o.ID)
		if apiCode(err) != c.code || got != c.want {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}

	r.dns.fail("de.example.com", errors.New("no such host"))
	other := addOutbound(t, r.cfg.Store, "Berlin", realityOut())
	got, err = r.m.TestOutbound(ctx, other.ID)
	if err != nil || got.OK || !strings.HasPrefix(got.Error, "The host de.example.com can't be reached: the host name does not resolve: no such host") {
		t.Errorf("a name that won't resolve: %+v, %v", got, err)
	}
	if _, err := r.m.TestOutbound(ctx, 9); err != api.ErrNoOutbound {
		t.Errorf("one nobody has: %v", err)
	}

	none := newOutRig(t, func(c *Config) { c.Prober = nil })
	addOutbound(t, none.cfg.Store, "Frankfurt", realityOut())
	if _, err := none.m.TestOutbound(ctx, 1); apiCode(err) != api.CodeTestUnavailable {
		t.Errorf("with no program: %v", err)
	}
	if _, err := none.m.TestOutboundSettings(ctx, apiOut()); apiCode(err) != api.CodeTestUnavailable {
		t.Errorf("unsaved, with no program: %v", err)
	}
}

func TestTestingSettingsThatAreNotSaved(t *testing.T) {
	r := newOutRig(t)
	ctx := context.Background()
	r.probe.answer(bridgespec.ProbeResult{OK: true, DelayMS: 90, Status: 204}, nil)
	got, err := r.m.TestOutboundSettings(ctx, trojanAPI())
	must(t, err)
	if got != (api.TestResult{OK: true, DelayMs: 90, Status: 204}) {
		t.Fatalf("got %+v", got)
	}
	if req := r.probe.asked()[0]; !req.Connect || req.Outbound.Tag != "" || req.Outbound.Password != outPassword || req.URL != "" {
		t.Errorf("asked for %+v", req)
	}
	bad := trojanAPI()
	bad.Port = 0
	if _, err := r.m.TestOutboundSettings(ctx, bad); codeOf(err) != api.CodeBadRequest || !strings.Contains(err.Error(), "settings.port") {
		t.Errorf("settings the hub refuses: %v", err)
	}
	r.dns.fail("de.example.com", errors.New("no such host"))
	got, err = r.m.TestOutboundSettings(ctx, apiOut())
	if err != nil || got.OK || !strings.Contains(got.Error, "does not resolve") {
		t.Errorf("a name that won't resolve: %+v, %v", got, err)
	}
	if len(r.cfg.Store.Read().Outbounds) != 0 {
		t.Error("a test kept something")
	}
}

func TestTheOutboundsCheck(t *testing.T) {
	r := newOutRig(t)
	ctx := context.Background()
	if _, err := r.m.SetOutboundCheck(ctx, "ftp://example.com/"); codeOf(err) != api.CodeBadRequest || !strings.Contains(err.Error(), "the check's url") {
		t.Fatalf("an address that is no web address: %v", err)
	}
	got, err := r.m.SetOutboundCheck(ctx, "  https://example.com/204 ")
	must(t, err)
	want := api.OutboundCheck{URL: "https://example.com/204", Seconds: 30, DefaultURL: bridgespec.DefaultProbeURL}
	if got != want || r.cfg.Store.Read().ProbeURL != want.URL {
		t.Fatalf("got %+v", got)
	}
	if got, _ = r.m.SetOutboundCheck(ctx, ""); got.URL != "" || got.DefaultURL == "" {
		t.Fatalf("back to the default: %+v", got)
	}
}

func TestRoutes(t *testing.T) {
	r := newOutRig(t)
	addNode(t, r.cfg.Store)
	o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
	ctx := context.Background()
	if err := r.m.SetNodeRoute(ctx, 9, 0); codeOf(err) != api.CodeNotFound {
		t.Errorf("a node nobody has: %v", err)
	}
	if err := r.m.SetNodeRoute(ctx, 1, 9); codeOf(err) != api.CodeBadRequest || !strings.Contains(err.Error(), "there is no outbound 9") {
		t.Errorf("an outbound nobody has: %v", err)
	}
	must(t, r.m.SetNodeRoute(ctx, 1, o.ID))
	if got := nodeOf(r.cfg.Store, 1).OutboundID; got != o.ID {
		t.Fatalf("the node leaves through %d", got)
	}
	if _, err := r.m.SetLocalRoute(ctx, 9); codeOf(err) != api.CodeBadRequest {
		t.Errorf("the router through an outbound nobody has: %v", err)
	}
	v, err := r.m.SetLocalRoute(ctx, o.ID)
	must(t, err)
	if v.OutboundID != o.ID || r.cfg.Store.Read().Local.OutboundID != o.ID {
		t.Fatalf("the router's view: %+v", v)
	}
	ov, _ := api.ViewOutbound(r.cfg.Store.Read(), o.ID, r.m)
	if !reflect.DeepEqual(ov.UsedBy, api.OutboundUsers{Nodes: []uint{1}, Router: true}) {
		t.Errorf("used by %+v", ov.UsedBy)
	}
}

// What comes back from the bridge program never carries an outbound's login.
func TestTheLoginIsNeverInWhatComesBack(t *testing.T) {
	r := newOutRig(t)
	ctx := context.Background()
	r.probe.answer(bridgespec.ProbeResult{Error: "the server refused password " + outPassword}, nil)
	got, err := r.m.TestOutboundSettings(ctx, trojanAPI())
	if err != nil || strings.Contains(got.Error, outPassword) {
		t.Errorf("a test's result: %+v, %v", got, err)
	}
	_, err = r.m.CreateOutbound(ctx, api.OutboundCreate{Name: "Paris", Settings: trojanAPI()})
	if err == nil || strings.Contains(err.Error(), outPassword) {
		t.Errorf("a refusal: %v", err)
	}
	r.probe.answer(bridgespec.ProbeResult{}, errors.New("panic: "+outPassword))
	_, err = r.m.CreateOutbound(ctx, api.OutboundCreate{Name: "Paris", Settings: trojanAPI()})
	if err == nil || strings.Contains(err.Error(), outPassword) {
		t.Errorf("a program that failed: %v", err)
	}
	if strings.Contains(r.log.String(), outPassword) || strings.Contains(r.log.String(), outUUID) {
		t.Errorf("the log has the login:\n%s", r.log)
	}
}

// A name with several addresses keeps the one it had while it still has it,
// so the spec doesn't move from one to another; it moves when the name does.
func TestAnOutboundsNameKeepsItsAddressWhileItHasIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		o := outbound(1, "Frankfurt", realityOut())
		ctx := context.Background()
		look := func() string {
			a, err := r.m.outAddrOf(ctx, o)
			if err != nil {
				return err.Error()
			}
			return a.String()
		}
		r.dns.set("de.example.com", "198.51.100.7", "198.51.100.8")
		if got := look(); got != "198.51.100.7" {
			t.Fatalf("first answer %s", got)
		}
		r.dns.set("de.example.com", "198.51.100.8", "198.51.100.7")
		time.Sleep(resolveTTL + time.Second)
		if got := look(); got != "198.51.100.7" {
			t.Fatalf("with the same addresses in another order: %s", got)
		}
		r.dns.set("de.example.com", "198.51.100.9", "198.51.100.8")
		time.Sleep(resolveTTL + time.Second)
		if got := look(); got != "198.51.100.9" {
			t.Fatalf("once the name moved: %s", got)
		}
		r.dns.fail("de.example.com", errors.New("server failure"))
		time.Sleep(resolveTTL + time.Second)
		if got := look(); got != "198.51.100.9" {
			t.Fatalf("a failed lookup keeps the last address: %s", got)
		}
		r.dns.mu.Lock()
		calls := slices.Clone(r.dns.calls)
		r.dns.mu.Unlock()
		if len(calls) != 4 || calls[0] != "ip4 de.example.com" {
			t.Errorf("lookups %v", calls)
		}
		if r.log.count("could not resolve an outbound's host name", "outbound=1", "keeping=198.51.100.9") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
	})
}

func TestARouteReachesTheBridgeAtOnceAndAnUnusedOutboundNever(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		o := addOutbound(t, r.cfg.Store, "Frankfurt", trojanOut())
		r.start()
		defer r.stop()
		ctx := context.Background()
		specs := func() int { n, _ := r.bridge.counts(); return n }
		if specs() != 1 || len(r.bridge.last().Outbounds) != 0 {
			t.Fatalf("before any route: %d specs, %+v", specs(), r.bridge.last())
		}

		must(t, r.m.SetNodeRoute(ctx, 2, o.ID))
		synctest.Wait()
		spec := r.bridge.last()
		if specs() != 2 || len(spec.Outbounds) != 1 || spec.Outbounds[0].Tag != "out-1." || spec.Nodes[1].Via != "out-1." ||
			spec.Nodes[0].Via != "" || spec.OutboundAddr != "192.168.150.80" {
			t.Fatalf("after the route: %d specs, %+v", specs(), spec)
		}
		must(t, spec.Validate())

		// one nothing leaves through comes and goes with no restart
		v, err := r.m.CreateOutbound(ctx, api.OutboundCreate{Name: "Paris", Settings: trojanAPI()})
		must(t, err)
		name := "Lyon"
		_, err = r.m.ChangeOutbound(ctx, v.ID, api.OutboundPatch{Name: &name})
		must(t, err)
		_, err = r.m.SetOutboundCheck(ctx, "https://example.com/204")
		must(t, err)
		synctest.Wait()
		if specs() != 3 || r.bridge.last().Probe.URL != "https://example.com/204" {
			t.Fatalf("the check of the outbound in use is in the spec: %d specs, %+v", specs(), r.bridge.last().Probe)
		}
		must(t, r.m.RemoveOutbound(ctx, v.ID))
		r.advance(10 * time.Second)
		if specs() != 3 {
			t.Fatalf("an unused outbound restarted the bridge: %d specs", specs())
		}

		// the one in use, changed, is a new spec
		on := true
		_, err = r.m.ChangeOutbound(ctx, o.ID, api.OutboundPatch{Fallback: &on})
		must(t, err)
		synctest.Wait()
		if specs() != 4 || !r.bridge.last().Outbounds[0].Fallback {
			t.Fatalf("its fallback switched: %d specs, %+v", specs(), r.bridge.last().Outbounds)
		}
		// and back through the VPN
		must(t, r.m.SetNodeRoute(ctx, 2, 0))
		synctest.Wait()
		if spec := r.bridge.last(); specs() != 5 || len(spec.Outbounds) != 0 || spec.OutboundAddr != "" || spec.Probe != (bridgespec.Probe{}) {
			t.Fatalf("back through the VPN: %d specs, %+v", specs(), spec)
		}
	})
}

// trojanOut is trojanAPI as the state keeps it.
func trojanOut() model.OutboundSettings {
	s, err := model.CleanOutboundSettings(trojanAPI().Model())
	if err != nil {
		panic(err)
	}
	return s
}

func TestAnOutboundsStateFollowsTheBridge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		o := addOutbound(t, r.cfg.Store, "Frankfurt", trojanOut())
		r.start()
		defer r.stop()
		ctx := context.Background()
		state := func() api.OutboundState { return r.m.OutboundLive(o.ID) }
		if got := state(); got.Status != api.OutboundIdle {
			t.Fatalf("nothing leaves through it: %+v", got)
		}
		must(t, r.m.SetNodeRoute(ctx, 1, o.ID))
		synctest.Wait()
		if got := state(); got.Status != api.OutboundUnknown {
			t.Fatalf("before the bridge has checked it: %+v", got)
		}
		r.bridge.outbound("out-1.", bridgespec.OutboundStatus{Checked: true, Alive: true, DelayMS: 120, LastTry: epoch, LastSeen: epoch, BytesUp: 5, BytesDown: 7})
		r.advance(3 * time.Second)
		got := state()
		if got.Status != api.OutboundUp || got.DelayMs != 120 || got.BytesUp != 5 || got.BytesDown != 7 || got.LastSeenAt == nil || !got.LastSeenAt.Equal(epoch) || got.Error != "" {
			t.Fatalf("checked and answering: %+v", got)
		}
		r.bridge.outbound("out-1.", bridgespec.OutboundStatus{Checked: true, Error: "i/o timeout", Fallback: true, LastTry: epoch.Add(time.Minute), LastSeen: epoch})
		r.advance(3 * time.Second)
		if got := state(); got.Status != api.OutboundDown || got.Error != "i/o timeout" || !got.OnFallback || got.DelayMs != 0 {
			t.Fatalf("checked and not answering: %+v", got)
		}
		view, _ := api.ViewOutbound(r.cfg.Store.Read(), o.ID, r.m)
		if view.State.Status != api.OutboundDown || !reflect.DeepEqual(view.UsedBy.Nodes, []uint{1}) {
			t.Errorf("the view: %+v", view)
		}
		// the bridge stops with the entry, and what it said goes with it
		r.flip(func(f *routerfacts.Facts) { f.VPN, f.RouteVPN = false, false })
		if got := state(); got.Status != api.OutboundUnknown || got.BytesUp != 0 {
			t.Fatalf("with the bridge stopped: %+v", got)
		}
	})
}

func TestANameThatWontResolveIsDownAndSaysWhy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		addNode(t, r.cfg.Store)
		addNode(t, r.cfg.Store)
		o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
		route(t, r.cfg.Store, 2, o.ID)
		r.dns.fail("de.example.com", errors.New("no such host"))
		r.start()
		defer r.stop()

		got := r.m.OutboundLive(o.ID)
		if got.Status != api.OutboundDown || got.OnFallback || !strings.Contains(got.Error, "The host name does not resolve: no such host") {
			t.Fatalf("without its fallback: %+v", got)
		}
		if tags := r.bridge.tags(); !slices.Equal(tags, []string{"node-1"}) {
			t.Fatalf("the bridge carries %v", tags)
		}
		if r.log.count("a node is left out of the bridge", "node=2", "Frankfurt: the host name does not resolve") != 1 ||
			r.log.count("an outbound can't be used, and what leaves through it is left out", "outbound=1") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		on := true
		_, err := r.m.ChangeOutbound(context.Background(), o.ID, api.OutboundPatch{Fallback: &on})
		must(t, err)
		synctest.Wait()
		if got := r.m.OutboundLive(o.ID); got.Status != api.OutboundDown || !got.OnFallback {
			t.Fatalf("with its fallback: %+v", got)
		}
		if spec := r.bridge.last(); len(spec.Nodes) != 2 || spec.Nodes[1].Via != "" || len(spec.Outbounds) != 0 {
			t.Fatalf("with its fallback node 2 leaves through the exit: %+v", spec)
		}

		// the name comes back, and so does the outbound
		r.dns.set("de.example.com", "198.51.100.7")
		r.dns.fail("de.example.com", nil)
		r.advance(resolveRetry + 3*time.Second)
		if spec := r.bridge.last(); len(spec.Outbounds) != 1 || spec.Nodes[1].Via != "out-1." {
			t.Fatalf("once it resolves: %+v", spec)
		}
		if got := r.m.OutboundLive(o.ID); got.Status != api.OutboundUnknown {
			t.Errorf("once it resolves: %+v", got)
		}
	})
}

// The bridge binds the Foreign link's address for the outbounds. One that no
// interface here holds keeps the bridge from running with an outbound, as the
// exit's and the dial address do.
func TestTheOutboundsNeedTheForeignLinksAddressHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		addrs := netroles.Defaults().Check(func(a netip.Addr) bool { return a.String() != "192.168.150.80" }, nil)
		r := newBridgeRig(t, func(c *Config) { c.Addrs = addrs })
		addNode(t, r.cfg.Store)
		o := addOutbound(t, r.cfg.Store, "Frankfurt", trojanOut())
		r.start()
		defer r.stop()
		if spec := r.bridge.last(); spec == nil {
			t.Fatal("with no outbound in use the Foreign link doesn't matter")
		}
		must(t, r.m.SetNodeRoute(context.Background(), 1, o.ID))
		synctest.Wait()
		if spec := r.bridge.last(); spec != nil {
			t.Fatalf("the bridge runs with an outbound it can't reach from the Foreign link: %+v", spec)
		}
		if r.log.count("the bridge is stopped", "ADDR_FOREIGN 192.168.150.80 is not assigned to any interface here") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
	})
}

func TestTheRoutersEntryThroughAnOutbound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		o := addOutbound(t, r.cfg.Store, "Frankfurt", realityOut())
		gone := realityOut()
		gone.Host = "gone.example.com"
		g := addOutbound(t, r.cfg.Store, "Berlin", gone)
		r.dns.set("de.example.com", "198.51.100.7")
		r.dns.fail("gone.example.com", errors.New("no such host"))
		r.start()
		defer r.stop()
		r.on(t)
		ctx := context.Background()
		v, err := r.m.SetLocalRoute(ctx, o.ID)
		must(t, err)
		synctest.Wait()
		if spec := r.bridge.last(); v.OutboundID != o.ID || spec.Local == nil || spec.Local.Via != "out-1." || len(spec.Outbounds) != 1 {
			t.Fatalf("view %+v, spec %+v", v, spec)
		}
		if got := r.m.OutboundLive(o.ID); got.Status != api.OutboundUnknown {
			t.Errorf("the router leaves through it: %+v", got)
		}

		// one that can't be used, with no fallback: the entry is left out, and says why
		_, err = r.m.SetLocalRoute(ctx, g.ID)
		must(t, err)
		r.advance(3 * time.Second)
		if spec := r.bridge.last(); spec != nil {
			t.Fatalf("the entry alone, left out: %+v", spec)
		}
		got := r.m.Local()
		if got.State != api.LocalAttention || got.Reason != api.LocalBridgeDown ||
			got.Detail != "The outbound it leaves through can't be used: Berlin: the host name does not resolve: no such host." {
			t.Fatalf("the entry: %+v", got)
		}
		if r.log.count("the router's own entry is left out of the bridge", "Berlin") != 1 {
			t.Errorf("log:\n%s", r.log)
		}
		// back through the VPN, and it runs again
		_, err = r.m.SetLocalRoute(ctx, 0)
		must(t, err)
		r.advance(3 * time.Second)
		if spec := r.bridge.last(); spec == nil || spec.Local == nil || spec.Local.Via != "" || len(spec.Outbounds) != 0 {
			t.Fatalf("back through the VPN: %+v", spec)
		}
		if got := r.m.Local(); got.State != api.LocalWorking {
			t.Errorf("the entry: %+v", got)
		}
	})
}
