// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// fakeAsks is what the router's script was left.
type fakeAsks struct {
	mu    sync.Mutex
	local bool
	host  string
	ports int
	err   error
}

func (a *fakeAsks) SetLocal(on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.local = on
	return nil
}

func (a *fakeAsks) SetHost(host string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.host = host
	return nil
}

func (a *fakeAsks) NewPort() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.ports++
	return nil
}

func (a *fakeAsks) left() (local bool, host string, ports int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.local, a.host, a.ports
}

// sites stands in for the check of a Reality target run on the router: every
// site works unless the test says what is wrong with it.
type sites struct {
	mu    sync.Mutex
	bad   map[string]string // by host:port, the reason code
	all   string            // a reason for every site
	asked []string
}

func (s *sites) probe(_ context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, req.Dest)
	reason := s.all
	if r, ok := s.bad[req.Dest]; ok {
		reason = r
	}
	if reason != "" {
		return &pb.RealityDestProbeResponse{Checks: []*pb.RealityDestCheck{{Reason: reason}}}, nil
	}
	return &pb.RealityDestProbeResponse{Checks: []*pb.RealityDestCheck{{
		Ok: true, TlsVersion: "TLS 1.3", KeyShare: "X25519", FlightBytes: 4000, HandshakeMs: 20,
	}}}, nil
}

func (s *sites) set(f func(*sites)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func (s *sites) checked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// entryRig is a bridge rig whose router can be asked for a port and whose
// hub can check Reality targets.
type entryRig struct {
	*bridgeRig
	asks  *fakeAsks
	sites *sites
}

func newEntryRig(t *testing.T, mods ...func(*Config)) *entryRig {
	t.Helper()
	a, s := &fakeAsks{}, &sites{}
	setup := func(c *Config) { c.Asks, c.LocalProbe = a, s.probe }
	return &entryRig{bridgeRig: newBridgeRig(t, append([]func(*Config){setup}, mods...)...), asks: a, sites: s}
}

const (
	cloudName = "a1b2c3d4e5f6.sn.mynetname.net"
	firstPort = 41873
)

// opened is the router's report once its script has opened port for the entry.
func opened(port uint16) func(*routerfacts.Facts) {
	return func(f *routerfacts.Facts) {
		f.LocalPort, f.LocalRules = port, true
		f.CloudName = cloudName
		f.PublicAddr = netip.MustParseAddr("198.51.100.24")
		f.DomesticAddr = netip.MustParseAddr("198.51.100.24")
	}
}

// on switches the entry on and lets it settle: a target chosen, the bridge
// started with it, and the router's port opened.
func (r *entryRig) on(t *testing.T) {
	t.Helper()
	_, err := r.m.SetLocalEnabled(context.Background(), true)
	must(t, err)
	synctest.Wait()
	r.flip(opened(firstPort))
	if got := r.m.Local(); got.State != api.LocalWorking {
		t.Fatalf("the entry did not come up: %+v", got)
	}
}

func (r *entryRig) sub(t *testing.T, name string) uint {
	t.Helper()
	return addSubscription(t, r.cfg.Store, name).ID
}

func (r *entryRig) here(t *testing.T, sub uint, on bool) {
	t.Helper()
	_, err := r.m.SetLocalSubscription(context.Background(), sub, on)
	must(t, err)
	r.advance(3 * time.Second)
}

func (r *entryRig) local() model.Local { return r.cfg.Store.Read().Local }

// clientHere is the subscription's client on the entry as the state has it,
// nil when it has none.
func (r *entryRig) clientHere(sub uint) *model.LocalClient {
	st := r.cfg.Store.Read()
	return st.FindLocalClient(sub)
}

func apiCode(err error) string {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSwitchingTheEntryOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		if got := r.m.Local(); got.State != api.LocalOff || got.Port != 0 {
			t.Fatalf("before it is switched on: %+v", got)
		}
		if r.bridge.last() != nil {
			t.Fatal("a bridge runs with no node and no entry")
		}

		v, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		if !v.Enabled || v.State != api.LocalStarting || v.PublicKey == "" || v.ShortID == "" {
			t.Fatalf("the answer to switching on: %+v", v)
		}
		l := r.local()
		must(t, l.Validate())
		if !l.Enabled || !l.HasKeys() || l.RealityPublicKey != v.PublicKey {
			t.Fatalf("the state after switching on: enabled %v, keys %v", l.Enabled, l.HasKeys())
		}
		if on, _, _ := r.asks.left(); !on {
			t.Error("the router was not asked to open a port")
		}

		// a target is chosen from the router, and the bridge starts with the entry and nothing else
		synctest.Wait()
		r.advance(3 * time.Second)
		l = r.local()
		if l.RealityTarget != "www.google.com:443" || l.RealitySNI != "" {
			t.Fatalf("the target chosen: %q, sni %q", l.RealityTarget, l.RealitySNI)
		}
		if got := r.sites.checked(); len(got) != len(agentops.DefaultTargets()) {
			t.Errorf("%d sites were tried, want the well-known ones", len(got))
		}
		spec := r.bridge.last()
		if spec == nil || spec.Local == nil || len(spec.Nodes) != 0 {
			t.Fatalf("the bridge's spec: %v", spec)
		}
		e := spec.Local
		if e.ListenAddr != "192.168.50.18" || e.Port != DefaultLocalPort || e.Target != "www.google.com:443" ||
			!slices.Equal(e.ServerNames, []string{"www.google.com"}) || e.Flow != bridgespec.VisionFlow ||
			e.PrivateKey != l.RealityPrivateKey || !slices.Equal(e.ShortIDs, []string{l.RealityShortID}) || len(e.Token) != 32 || len(e.Clients) != 0 {
			t.Errorf("the entry in the spec: %+v", e.String())
		}
		must(t, spec.Validate())
		if got := r.m.Entry().Bridge; got != "running" {
			t.Errorf("the entry says the bridge is %s", got)
		}

		// the router has not said it opened the port yet
		if got := r.m.Local(); got.State != api.LocalStarting || got.Reason != api.LocalWaitingRouter {
			t.Fatalf("before the router answers: %+v", got)
		}
		r.flip(opened(firstPort))
		got := r.m.Local()
		if got.State != api.LocalWorking || got.Port != firstPort || got.CloudName != cloudName || got.PublicAddr != "198.51.100.24" || got.UptimeSec <= 0 {
			t.Fatalf("once the port is open: %+v", got)
		}
		view := api.ViewLocal(r.cfg.Store.Read(), r.m)
		if view.Host != cloudName || view.HostSource != "cloud" || view.Port == nil || *view.Port != firstPort || view.BehindNAT {
			t.Errorf("the view: %+v", view)
		}
		if got := kinds(r.m.LocalEvents()); !slices.Equal(got, []string{EventLocalOn, EventTargetChanged, EventPortOpened}) {
			t.Errorf("events %v", got)
		}

		// switching it on again changes nothing: the same keys, no second choice
		before := len(r.sites.checked())
		_, err = r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()
		if again := r.local(); again != l || len(r.sites.checked()) != before {
			t.Error("switching it on a second time changed something")
		}
	})
}

// Off keeps everything: the keys, the target and who is on it, so on again
// gives each subscription the link it had.
func TestSwitchingTheEntryOffKeepsWhatItHad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		id := r.sub(t, "Sara")
		r.here(t, id, true)
		link, err := r.m.LocalLink(id)
		must(t, err)
		was := r.local()

		v, err := r.m.SetLocalEnabled(context.Background(), false)
		must(t, err)
		r.advance(3 * time.Second)
		if v.Enabled || v.State != api.LocalOff {
			t.Fatalf("the answer to switching off: %+v", v)
		}
		if r.bridge.last() != nil {
			t.Error("the bridge still runs with the entry off and no node")
		}
		if on, _, _ := r.asks.left(); on {
			t.Error("the router is still asked to keep the port open")
		}
		now := r.local()
		was.Enabled = false
		if now != was {
			t.Fatalf("switching off changed more than the switch:\n got %+v\nwant %+v", now, was)
		}
		if c := r.clientHere(id); c == nil || !c.Enabled {
			t.Fatal("the subscription's switch on the entry was lost")
		}
		// while it is off it hands out no link and takes no switch
		if _, err := r.m.LocalLink(id); apiCode(err) != api.CodeLocalOff {
			t.Errorf("a link while off: %v", err)
		}
		if _, err := r.m.SetLocalSubscription(context.Background(), id, false); apiCode(err) != api.CodeLocalOff {
			t.Errorf("a switch while off: %v", err)
		}

		_, err = r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		r.advance(3 * time.Second)
		again, err := r.m.LocalLink(id)
		must(t, err)
		if again != link {
			t.Fatal("on again, the subscription's link is another one")
		}
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(id)}) {
			t.Errorf("on again, the bridge holds %v", got)
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventLocalOff) {
			t.Errorf("events %v", got)
		}
	})
}

func TestASubscriptionOnTheEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		id := r.sub(t, "سارا")
		specs, _ := r.bridge.counts()

		v, err := r.m.SetLocalSubscription(context.Background(), id, true)
		must(t, err)
		if v.Local == nil || !v.Local.Enabled {
			t.Fatalf("the answer: %+v", v.Local)
		}
		r.advance(3 * time.Second)
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(id)}) {
			t.Fatalf("the bridge holds %v", got)
		}
		if n, _ := r.bridge.counts(); n != specs {
			t.Fatal("the bridge was started again for a change of clients")
		}

		// its link: the router's name and port, the entry's values, its own id and name
		link, err := r.m.LocalLink(id)
		must(t, err)
		if !link.Local || link.NodeID != 0 || link.NodeName != "" {
			t.Errorf("the link is marked %+v", link)
		}
		u, err := url.Parse(link.Vless)
		must(t, err)
		l, c := r.local(), r.clientHere(id)
		q := u.Query()
		if u.Scheme != "vless" || u.User.Username() != c.ClientUUID || u.Host != cloudName+":41873" || u.Fragment != "سارا" ||
			q.Get("security") != "reality" || q.Get("pbk") != l.RealityPublicKey || q.Get("sid") != l.RealityShortID ||
			q.Get("sni") != "www.google.com" || q.Get("flow") != "xtls-rprx-vision" || q.Get("type") != "tcp" {
			t.Errorf("the link: %s", strings.Replace(link.Vless, c.ClientUUID, "<id>", 1))
		}

		// off: the bridge drops it, the id is kept, and there is no link
		r.here(t, id, false)
		if got := r.bridge.emails(); len(got) != 0 {
			t.Fatalf("after switching off the bridge holds %v", got)
		}
		if _, err := r.m.LocalLink(id); apiCode(err) != api.CodeNotFound {
			t.Errorf("a link while switched off here: %v", err)
		}
		r.here(t, id, true)
		again, err := r.m.LocalLink(id)
		must(t, err)
		if again.Vless != link.Vless {
			t.Error("switched on again, its link is another one")
		}
		if got := kinds(r.m.LocalEvents()); !slices.Equal(got[len(got)-3:], []string{EventSubscriptionOn, EventSubscriptionOff, EventSubscriptionOn}) {
			t.Errorf("events %v", got)
		}

		// what isn't there
		if _, err := r.m.SetLocalSubscription(context.Background(), 99, true); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a subscription nobody has: %v", err)
		}
		if _, err := r.m.LocalLink(99); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("its link: %v", err)
		}
		// switching off one that was never on is nothing, and makes no record
		other := r.sub(t, "Ali")
		_, err = r.m.SetLocalSubscription(context.Background(), other, false)
		must(t, err)
		if r.clientHere(other) != nil {
			t.Error("switching off made a client")
		}
	})
}

// The bridge holds whoever the state says: a stopped subscription leaves, one
// that runs again comes back, a deleted one is gone, and a replaced link is
// another id under the same name.
func TestTheEntrysClientsFollowTheState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		sara, ali := r.sub(t, "Sara"), r.sub(t, "Ali")
		r.here(t, sara, true)
		r.here(t, ali, true)
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(sara), subEmail(ali)}) {
			t.Fatalf("the bridge holds %v", got)
		}
		specs, _ := r.bridge.counts()

		// an end date that comes: the subscription stops and leaves the entry
		ctx := context.Background()
		_, err := r.m.UpdateSubscription(ctx, sara, api.SubscriptionPatch{ExpiresAt: api.Opt[time.Time]{Set: true, Value: r.cfg.Now().Add(time.Hour)}})
		must(t, err)
		r.advance(61 * time.Minute)
		if r.stopped(sara) != model.StopExpired {
			t.Fatalf("stopped = %q", r.stopped(sara))
		}
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(ali)}) {
			t.Fatalf("with one expired the bridge holds %v", got)
		}
		if c := r.clientHere(sara); c == nil || !c.Enabled {
			t.Error("its switch on the entry was turned off")
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventSubscriptionExpired) {
			t.Errorf("events %v", got)
		}

		// a later date: it runs again and is back
		_, err = r.m.UpdateSubscription(ctx, sara, api.SubscriptionPatch{ExpiresAt: api.Opt[time.Time]{Set: true, Null: true}})
		must(t, err)
		r.advance(3 * time.Second)
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(sara), subEmail(ali)}) {
			t.Fatalf("with it running again the bridge holds %v", got)
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventSubscriptionResumed) {
			t.Errorf("events %v", got)
		}

		// a new link: the same name with another id, and the old link is not the new one
		old, err := r.m.LocalLink(ali)
		must(t, err)
		oldID := r.clientHere(ali).ClientUUID
		fresh, err := r.m.ReplaceLocalLink(ctx, ali)
		must(t, err)
		r.advance(3 * time.Second)
		newID := r.clientHere(ali).ClientUUID
		if newID == oldID || fresh.Vless == old.Vless || !strings.Contains(fresh.Vless, newID) {
			t.Fatal("the link was not replaced")
		}
		r.bridge.mu.Lock()
		held := slices.Clone(r.bridge.clients)
		r.bridge.mu.Unlock()
		if i := slices.IndexFunc(held, func(c bridgespec.Client) bool { return c.Email == subEmail(ali) }); i < 0 || held[i].UUID != newID {
			t.Errorf("the bridge holds %v for the replaced link", held)
		}

		// deleted: gone from the entry too
		must(t, r.m.DeleteSubscription(ctx, ali))
		r.advance(3 * time.Second)
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(sara)}) {
			t.Fatalf("after a delete the bridge holds %v", got)
		}
		if r.clientHere(ali) != nil {
			t.Error("the deleted subscription's client is still in the state")
		}

		if n, _ := r.bridge.counts(); n != specs {
			t.Errorf("the bridge was given %d more spec(s): none of this is worth a restart", n-specs)
		}
	})
}

// A bridge the supervisor started again holds the clients its spec came with,
// which may be old: it is sent the ones the state has now.
func TestABridgeThatStartedAgainIsSentTheClientsAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		id := r.sub(t, "Sara")
		r.here(t, id, true)
		sent := r.bridge.setCount()

		// nothing changed: nothing is sent again, however many looks go by
		r.advance(time.Minute)
		if got := r.bridge.setCount(); got != sent {
			t.Fatalf("the same clients were sent %d more time(s)", got-sent)
		}

		// the supervisor starts it again with the spec it holds, from before the subscription
		r.bridge.set(func(b *fakeBridge) {
			b.started = r.cfg.Now()
			b.clients = nil
		})
		r.advance(3 * time.Second)
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(id)}) {
			t.Fatalf("the bridge that started again holds %v", got)
		}

		// a bridge that refuses them is tried again on every look, and said once
		r.bridge.set(func(b *fakeBridge) { b.failClients = errors.New("the page is busy") })
		other := r.sub(t, "Ali")
		r.here(t, other, true)
		r.advance(10 * time.Second)
		if got := r.bridge.emails(); len(got) != 1 {
			t.Fatalf("a refused set was taken: %v", got)
		}
		if n := r.log.count("the entry's clients could not be sent", "the page is busy"); n != 1 {
			t.Errorf("the failure was logged %d times, want once", n)
		}
		r.bridge.set(func(b *fakeBridge) { b.failClients = nil })
		r.advance(3 * time.Second)
		if got := r.bridge.emails(); len(got) != 2 {
			t.Fatalf("once it takes them again the bridge holds %v", got)
		}
	})
}

// The entry beside a node: one bridge carries both, and the entry coming or
// going is a new spec, since it is another inbound.
func TestTheEntryBesideANode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		if spec := r.bridge.last(); spec == nil || spec.Local != nil || len(spec.Nodes) != 1 {
			t.Fatalf("before the entry: %v", spec)
		}
		r.on(t)
		spec := r.bridge.last()
		if spec.Local == nil || len(spec.Nodes) != 1 {
			t.Fatalf("with the entry: %v", spec)
		}
		must(t, spec.Validate())

		_, err := r.m.SetLocalEnabled(context.Background(), false)
		must(t, err)
		r.advance(3 * time.Second)
		if spec := r.bridge.last(); spec == nil || spec.Local != nil || len(spec.Nodes) != 1 {
			t.Fatalf("with the entry off again the node's tunnel must stay: %v", spec)
		}
	})
}

func TestTheEntrysLinkNeedsANameAndAPort(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		_, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()
		id := r.sub(t, "Sara")
		r.here(t, id, true)

		// the router has opened no port yet
		if _, err := r.m.LocalLink(id); apiCode(err) != api.CodeNoAddress {
			t.Fatalf("a link before the port is open: %v", err)
		}
		// a port, and no name: the router has no IP Cloud name and none is set
		r.flip(func(f *routerfacts.Facts) { opened(firstPort)(f); f.CloudName = "" })
		if got := r.m.Local(); got.State != api.LocalAttention || got.Reason != api.LocalNoAddress {
			t.Errorf("with no name: %+v", got)
		}
		if _, err := r.m.LocalLink(id); apiCode(err) != api.CodeNoAddress {
			t.Fatalf("a link with no name: %v", err)
		}

		// a name set by hand is what the link carries, and the router's script is told it
		host := "Home.Example.com"
		v, err := r.m.ChangeLocalSettings(context.Background(), api.LocalSettings{Host: &host})
		must(t, err)
		if v.Host != "home.example.com" || v.HostSource != "own" || v.State != api.LocalWorking {
			t.Fatalf("with a name set by hand: %+v", v)
		}
		if _, left, _ := r.asks.left(); left != "home.example.com" {
			t.Errorf("the router's script was left the name %q", left)
		}
		link, err := r.m.LocalLink(id)
		must(t, err)
		if u, _ := url.Parse(link.Vless); u.Host != "home.example.com:41873" {
			t.Errorf("the link goes to %s", u.Host)
		}

		// the router's own name comes back when the one set by hand is taken away
		r.flip(opened(firstPort))
		empty := ""
		v, err = r.m.ChangeLocalSettings(context.Background(), api.LocalSettings{Host: &empty})
		must(t, err)
		if v.Host != cloudName || v.HostSource != "cloud" {
			t.Errorf("with the name cleared: %q from %q", v.Host, v.HostSource)
		}
		if _, left, _ := r.asks.left(); left != "" {
			t.Errorf("the router's script still has the name %q", left)
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventHostChanged) {
			t.Errorf("events %v", got)
		}
	})
}

func TestChangingTheEntrysTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		ctx := context.Background()
		str := func(s string) *string { return &s }
		specs, _ := r.bridge.counts()

		// a target that doesn't work from the router is refused, and nothing changes
		r.sites.set(func(s *sites) { s.bad = map[string]string{"old.example.com:443": "tls_version"} })
		_, err := r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str("old.example.com")})
		if apiCode(err) != api.CodeRealityTargetFailed || !strings.Contains(err.Error(), "old.example.com:443") || !strings.Contains(err.Error(), "TLS 1.3") {
			t.Fatalf("a target without TLS 1.3: %v", err)
		}
		if got := r.local().RealityTarget; got != "www.google.com:443" {
			t.Fatalf("a refused target was saved: %q", got)
		}

		// one that works is saved as host:port, the server name follows it, and the bridge starts with it
		v, err := r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str(" WWW.Example.org ")})
		must(t, err)
		r.advance(3 * time.Second)
		if v.HandshakeTarget != "www.example.org:443" || v.SNI != "" || v.SNICustom {
			t.Fatalf("after a new target: %+v", v)
		}
		spec := r.bridge.last()
		if spec.Local.Target != "www.example.org:443" || !slices.Equal(spec.Local.ServerNames, []string{"www.example.org"}) {
			t.Errorf("the bridge's entry: %s to %s", spec.Local.ServerNames, spec.Local.Target)
		}
		if n, _ := r.bridge.counts(); n != specs+1 {
			t.Errorf("%d specs for one change of target", n-specs)
		}

		// a server name of its own stays when the target moves
		v, err = r.m.ChangeLocalSettings(ctx, api.LocalSettings{SNI: str("cdn.example.org")})
		must(t, err)
		if v.SNI != "cdn.example.org" || !v.SNICustom {
			t.Fatalf("with a server name of its own: %+v", v)
		}
		v, err = r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str("www.example.net:8443")})
		must(t, err)
		if v.HandshakeTarget != "www.example.net:8443" || v.SNI != "cdn.example.org" {
			t.Fatalf("the server name didn't stay: %+v", v)
		}
		// and an empty one goes back to the target's host
		v, err = r.m.ChangeLocalSettings(ctx, api.LocalSettings{SNI: str("")})
		must(t, err)
		if v.SNI != "" || v.SNICustom {
			t.Fatalf("with the server name cleared: %+v", v)
		}

		// an address has no name to put in the hello, unless one is given with it
		if _, err := r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str("203.0.113.9:443")}); err == nil || !strings.Contains(err.Error(), "server name") {
			t.Errorf("an address for a target: %v", err)
		}
		if _, err := r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str("203.0.113.9:443"), SNI: str("www.example.net")}); err != nil {
			t.Errorf("an address with a server name: %v", err)
		}

		// what can't be used at all, and what is no change
		for name, req := range map[string]api.LocalSettings{
			"a URL for a target":     {HandshakeTarget: str("https://www.example.com/")},
			"an address for the SNI": {SNI: str("203.0.113.9")},
			"a host that is none":    {Host: str("not a host")},
			"the settings it has":    {SNI: str("www.example.net")},
		} {
			var inv *model.InvalidError
			if _, err := r.m.ChangeLocalSettings(ctx, req); !errors.As(err, &inv) {
				t.Errorf("%s: %v", name, err)
			}
		}

		// an empty target has the router choose again
		v, err = r.m.ChangeLocalSettings(ctx, api.LocalSettings{HandshakeTarget: str("")})
		must(t, err)
		synctest.Wait()
		if got := r.local(); got.RealityTarget != "www.google.com:443" || got.RealitySNI != "" {
			t.Fatalf("after leaving the choice to the router: %q, sni %q", got.RealityTarget, got.RealitySNI)
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventTargetChanged) {
			t.Errorf("events %v", got)
		}
	})
}

// When no site works from the router the entry says so and tries again later.
func TestNoRealityTargetWorksFromTheRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.sites.set(func(s *sites) { s.all = "no_response" })
		r.start()
		defer r.stop()
		_, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()
		got := r.m.Local()
		if got.State != api.LocalAttention || got.Reason != api.LocalTargetFailed || !strings.Contains(got.Detail, "None of the") {
			t.Fatalf("with no site working: %+v", got)
		}
		if r.bridge.last() != nil {
			t.Error("the bridge runs an entry that has no target")
		}
		tried := len(r.sites.checked())

		// not again at once
		r.advance(time.Minute)
		if len(r.sites.checked()) != tried {
			t.Fatal("the choice was tried again within a minute")
		}
		// and again after a while, when the sites answer
		r.sites.set(func(s *sites) { s.all = "" })
		r.advance(5 * time.Minute)
		if got := r.local().RealityTarget; got == "" {
			t.Fatal("no target after the sites came back")
		}
		if got := r.m.Local(); got.Reason == api.LocalTargetFailed {
			t.Errorf("still failed: %+v", got)
		}
	})
}

// A hub that can't check targets can't choose one either, and says what to do.
func TestWithNoWayToCheckATarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t, func(c *Config) { c.LocalProbe = nil })
		r.start()
		defer r.stop()
		_, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()
		if got := r.m.Local(); got.Reason != api.LocalTargetFailed || !strings.Contains(got.Detail, "Set one in Settings") {
			t.Fatalf("got %+v", got)
		}
		if _, err := r.m.CheckLocalTarget(context.Background(), "www.example.com"); apiCode(err) != api.CodeLocalOff {
			t.Errorf("a check with nothing to run it: %v", err)
		}
		// one that is given is taken as it is
		target := "www.example.com"
		v, err := r.m.ChangeLocalSettings(context.Background(), api.LocalSettings{HandshakeTarget: &target})
		must(t, err)
		if v.HandshakeTarget != "www.example.com:443" {
			t.Fatalf("got %+v", v)
		}
	})
}

func TestCheckingATargetFromTheRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.sites.set(func(s *sites) { s.bad = map[string]string{"old.example.com:443": "tls_version"} })
		got, err := r.m.CheckLocalTarget(context.Background(), "www.example.com")
		must(t, err)
		if !got.OK || !got.TLS13 || !got.X25519 || got.HandshakeBytes != 4000 || got.LatencyMs != 20 {
			t.Errorf("a good target: %+v", got)
		}
		got, err = r.m.CheckLocalTarget(context.Background(), "old.example.com:443")
		must(t, err)
		if got.OK || got.Error != "It doesn't use TLS 1.3." {
			t.Errorf("a TLS 1.2 site: %+v", got)
		}
		var inv *model.InvalidError
		if _, err := r.m.CheckLocalTarget(context.Background(), "not a target"); !errors.As(err, &inv) {
			t.Errorf("what is no target: %v", err)
		}
	})
}

func TestANewPortIsAskedOfTheRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		if _, err := r.m.NewLocalPort(context.Background()); apiCode(err) != api.CodeLocalOff {
			t.Fatalf("a new port while off: %v", err)
		}
		r.on(t)
		id := r.sub(t, "Sara")
		r.here(t, id, true)
		old, err := r.m.LocalLink(id)
		must(t, err)

		v, err := r.m.NewLocalPort(context.Background())
		must(t, err)
		if _, _, asked := r.asks.left(); asked != 1 {
			t.Fatalf("the router was asked %d times", asked)
		}
		// until the router has moved it, the entry is on its way, with the port it still has
		if v.State != api.LocalStarting || v.Reason != api.LocalWaitingRouter || v.Port == nil || *v.Port != firstPort {
			t.Fatalf("while the router moves the port: %+v", v)
		}
		r.flip(opened(52310))
		got := r.m.Local()
		if got.State != api.LocalWorking || got.Port != 52310 {
			t.Fatalf("once it moved: %+v", got)
		}
		fresh, err := r.m.LocalLink(id)
		must(t, err)
		if u, _ := url.Parse(fresh.Vless); u.Port() != "52310" || fresh.Vless == old.Vless {
			t.Errorf("the link after the move goes to port %s", u.Port())
		}
		ev := r.m.LocalEvents()
		if ev[0].Kind != EventPortOpened || ev[0].Detail != "52310" || ev[1].Kind != EventPortRequested {
			t.Errorf("events %+v", ev[:2])
		}

		// a router that never moves it stops being waited for
		_, err = r.m.NewLocalPort(context.Background())
		must(t, err)
		r.advance(4 * time.Minute)
		if got := r.m.Local(); got.State != api.LocalWorking {
			t.Errorf("after the wait ran out with the old port still open: %+v", got)
		}
	})
}

func TestWithNoWayToAskTheRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t, func(c *Config) { c.Asks = nil })
		r.start()
		defer r.stop()
		_, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()
		if _, err := r.m.NewLocalPort(context.Background()); apiCode(err) != api.CodeLocalOff {
			t.Fatalf("a new port with nobody to ask: %v", err)
		}
		// the port never comes, and after a while that is said
		r.advance(4 * time.Minute)
		if got := r.m.Local(); got.State != api.LocalAttention || got.Reason != api.LocalNoPort {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestRotatingTheEntrysKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		if _, err := r.m.RotateLocalKeys(context.Background()); apiCode(err) != api.CodeLocalOff {
			t.Fatalf("rotating keys it never had: %v", err)
		}
		r.on(t)
		id := r.sub(t, "Sara")
		r.here(t, id, true)
		old, err := r.m.LocalLink(id)
		must(t, err)
		was := r.local()
		specs, _ := r.bridge.counts()

		v, err := r.m.RotateLocalKeys(context.Background())
		must(t, err)
		r.advance(3 * time.Second)
		now := r.local()
		must(t, now.Validate())
		if now.RealityPrivateKey == was.RealityPrivateKey || now.RealityPublicKey == was.RealityPublicKey || now.RealityShortID == was.RealityShortID || v.PublicKey != now.RealityPublicKey {
			t.Fatal("the keys were not replaced")
		}
		if n, _ := r.bridge.counts(); n != specs+1 || r.bridge.last().Local.PrivateKey != now.RealityPrivateKey {
			t.Errorf("the bridge was not started with the new keys (%d specs more)", n-specs)
		}
		// the subscription is still on it after the restart, under a link that changed
		if got := r.bridge.emails(); !slices.Equal(got, []string{subEmail(id)}) {
			t.Errorf("after the restart the bridge holds %v", got)
		}
		fresh, err := r.m.LocalLink(id)
		must(t, err)
		if fresh.Vless == old.Vless || !strings.Contains(fresh.Vless, now.RealityPublicKey) {
			t.Error("the link does not carry the new key")
		}
		if got := kinds(r.m.LocalEvents()); !slices.Contains(got, EventKeysRotated) {
			t.Errorf("events %v", got)
		}
	})
}

// What stops the entry, from the router's side and the bridge's.
func TestWhatTheEntrySaysWhenItCantWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		_, err := r.m.SetLocalEnabled(context.Background(), true)
		must(t, err)
		synctest.Wait()

		// the router never opens the port: waited for, and then said
		r.advance(2 * time.Minute)
		if got := r.m.Local(); got.State != api.LocalStarting || got.Reason != api.LocalWaitingRouter {
			t.Fatalf("two minutes in: %+v", got)
		}
		r.advance(2 * time.Minute)
		if got := r.m.Local(); got.State != api.LocalAttention || got.Reason != api.LocalNoPort {
			t.Fatalf("four minutes in: %+v", got)
		}
		r.flip(opened(firstPort))
		if got := r.m.Local(); got.State != api.LocalWorking {
			t.Fatalf("once it opened: %+v", got)
		}

		// the VPN goes: the router's entry gate is off, and so is the bridge
		r.flip(func(f *routerfacts.Facts) { opened(firstPort)(f); f.VPN = false })
		if got := r.m.Local(); got.State != api.LocalAttention || got.Reason != api.LocalEntryOff {
			t.Fatalf("with the VPN down: %+v", got)
		}
		if r.bridge.last() != nil {
			t.Error("the bridge still runs with the gate off")
		}
		r.flip(opened(firstPort))
		if got := r.m.Local(); got.State != api.LocalWorking {
			t.Fatalf("with the VPN back: %+v", got)
		}

		// the bridge's page stops answering
		r.bridge.set(func(b *fakeBridge) { b.pageDown = true })
		r.advance(3 * time.Second)
		if got := r.m.Local(); got.State != api.LocalAttention || got.Reason != api.LocalBridgeDown || got.UptimeSec != 0 {
			t.Fatalf("with the bridge not answering: %+v", got)
		}
	})
}

// A hub that starts with the entry on gives the router and the bridge the
// same time to answer as when it is switched on, and asks the router again.
func TestAHubThatStartsWithTheEntryOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		keys, err := agentops.NewRealityKeys()
		must(t, err)
		r := newEntryRig(t, func(c *Config) {
			must(t, c.Store.Update(func(s *model.State) error {
				s.Local = model.Local{
					Enabled: true, RealityTarget: "www.example.org:443",
					RealityPrivateKey: keys.PrivateKey, RealityPublicKey: keys.PublicKey, RealityShortID: keys.ShortID,
				}
				return nil
			}))
		})
		if on, _, _ := r.asks.left(); !on {
			t.Error("the router was not asked again at the start")
		}
		r.start()
		defer r.stop()
		if got := r.m.Local(); got.State != api.LocalStarting {
			t.Fatalf("at the start: %+v", got)
		}
		if spec := r.bridge.last(); spec == nil || spec.Local == nil || spec.Local.Target != "www.example.org:443" {
			t.Fatalf("the bridge at the start: %v", spec)
		}
		if len(r.sites.checked()) != 0 {
			t.Error("a target was looked for though it has one")
		}
		r.flip(opened(firstPort))
		if got := r.m.Local(); got.State != api.LocalWorking {
			t.Fatalf("got %+v", got)
		}
	})
}

// What the entry's clients move is counted like a node's subscribers', shows
// on the page, and counts against a subscription's limit.
func TestTheEntrysTrafficIsCounted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newEntryRig(t)
		r.start()
		defer r.stop()
		r.on(t)
		id := r.sub(t, "Sara")
		r.here(t, id, true)

		r.bridge.client(bridgespec.ClientStatus{Email: subEmail(id), BytesUp: 1000, BytesDown: 9000, Connections: 2, Addrs: []string{"198.51.100.7"}, LastSeen: r.cfg.Now()})
		r.advance(8 * time.Second)
		u := r.m.SubscriptionOn(id, api.RouterID)
		if !u.Live || !u.Online || u.Connections != 2 || u.TodayUp != 1000 || u.TodayDown != 9000 || u.LastSeenAt == nil {
			t.Fatalf("the subscription on the entry: %+v", u)
		}
		if got := r.m.SubscriptionLifetime(id); got != 10000 {
			t.Errorf("it has moved %d in all", got)
		}
		if got := r.m.Local().Usage; !got.Live || got.Online != 1 || got.Connections != 2 || got.TodayDown != 9000 {
			t.Errorf("the entry's own numbers: %+v", got)
		}
		v, ok := api.ViewSubscription(r.cfg.Store.Read(), id, r.m)
		if !ok || v.Local == nil || v.Local.Online == nil || !*v.Local.Online || v.TodayDown != 9000 || v.Online == nil || !*v.Online {
			t.Errorf("the subscription's view: %+v", v)
		}

		// more bytes, and a chart of them
		r.bridge.client(bridgespec.ClientStatus{Email: subEmail(id), BytesUp: 3000, BytesDown: 29000, Connections: 1, Addrs: []string{"198.51.100.7"}})
		r.advance(8 * time.Second)
		if got := r.m.SubscriptionLifetime(id); got != 32000 {
			t.Errorf("after more it has moved %d", got)
		}
		if got := r.m.Traffic(api.RouterID); len(got.Samples) == 0 {
			t.Error("the entry has no chart")
		}

		// a limit below what it has moved stops it, and it leaves the entry
		limit := uint64(30000)
		_, err := r.m.UpdateSubscription(context.Background(), id, api.SubscriptionPatch{TrafficLimit: api.Opt[uint64]{Set: true, Value: limit}})
		must(t, err)
		r.advance(3 * time.Second)
		if r.stopped(id) != model.StopLimit {
			t.Fatalf("stopped = %q", r.stopped(id))
		}
		if got := r.bridge.emails(); len(got) != 0 {
			t.Fatalf("the bridge still holds %v", got)
		}

		// switched off, nobody is connected to it any more
		_, err = r.m.SetLocalEnabled(context.Background(), false)
		must(t, err)
		r.advance(8 * time.Second)
		if got := r.m.Local().Usage; got.Online != 0 || got.Connections != 0 {
			t.Errorf("with the entry off it still shows somebody connected: %+v", got)
		}
		if got := r.m.SubscriptionLifetime(id); got != 32000 {
			t.Errorf("switching off changed what it had moved to %d", got)
		}
	})
}

// The clients are not part of what tells one spec from another, and the
// entry is.
func TestTheSpecsHashLeavesTheClientsOut(t *testing.T) {
	base := func() *bridgespec.Spec {
		return &bridgespec.Spec{
			Version: bridgespec.Version, DialAddr: "192.168.50.18", Exit: bridgespec.Exit{Addr: "192.168.50.17"},
			DNS: bridgespec.DNS{Servers: []string{"1.1.1.1"}}, StatusAddr: DefaultStatusAddr,
			Local: &bridgespec.Local{
				ListenAddr: "192.168.50.18", Port: 8443, PrivateKey: fixtureKey, ShortIDs: []string{"a1b2c3d4"},
				ServerNames: []string{"www.example.com"}, Target: "www.example.com:443", Flow: bridgespec.VisionFlow,
				Token: "0123456789abcdef0123456789abcdef",
			},
		}
	}
	a, b := base(), base()
	b.Local.Clients = []bridgespec.Client{{Email: subEmail(1), UUID: clientIDOf(1, 0)}}
	if specHash(a) != specHash(b) {
		t.Error("a client more is another hash, so it would restart the bridge")
	}
	if len(b.Local.Clients) != 1 {
		t.Error("hashing took the clients out of the spec itself")
	}
	c := base()
	c.Local.Target = "www.example.org:443"
	d := base()
	d.Local = nil
	d.Nodes = nil
	if specHash(a) == specHash(c) || specHash(a) == specHash(d) {
		t.Error("another target, or no entry, has the same hash")
	}
}

// The entry's clients are the subscriptions on it that are running, by email.
func TestWhoTheEntryShouldHold(t *testing.T) {
	st := model.NewState()
	for _, name := range []string{"a", "b", "c", "d"} {
		_, err := st.AddSubscription(model.Subscription{Name: name, CreatedAt: epoch})
		if err != nil {
			t.Fatal(err)
		}
	}
	st.Subscriptions[2].Stopped = model.StopLimit
	st.LocalClients = []model.LocalClient{
		{SubscriptionID: 4, ClientUUID: clientIDOf(4, 0), Enabled: true},
		{SubscriptionID: 1, ClientUUID: clientIDOf(1, 0), Enabled: true},
		{SubscriptionID: 2, ClientUUID: clientIDOf(2, 0), Enabled: false},
		{SubscriptionID: 3, ClientUUID: clientIDOf(3, 0), Enabled: true},
	}
	if got := localWanted(st); got != nil {
		t.Fatalf("with the entry off: %v", got)
	}
	st.Local.Enabled = true
	got := localWanted(st)
	want := []bridgespec.Client{{Email: subEmail(1), UUID: clientIDOf(1, 0)}, {Email: subEmail(4), UUID: clientIDOf(4, 0)}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if err := bridgespec.CheckClients(got, []string{"node-1"}); err != nil {
		t.Errorf("the bridge would refuse them: %v", err)
	}
}
