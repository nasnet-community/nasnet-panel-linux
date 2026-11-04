// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// parsed is a link taken apart the way a client takes it.
type parsed struct {
	id, host, port, name string
	query                url.Values
}

func parseLink(t testing.TB, link string) parsed {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "vless" || u.User == nil {
		t.Fatalf("%q is not a vless link: %v", link, err)
	}
	return parsed{id: u.User.Username(), host: u.Hostname(), port: u.Port(), name: u.Fragment, query: u.Query()}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7f {
			return false
		}
	}
	return true
}

// A manager that never runs: the links come from the state alone.
func quietManager(t testing.TB, mods ...func(*Config)) (*Manager, Store) {
	t.Helper()
	cfg, _ := baseConfig(t)
	for _, mod := range mods {
		mod(&cfg)
	}
	m, err := New(cfg)
	must(t, err)
	return m, cfg.Store
}

func TestALinkHasTheNodesAddressAndTheClientsId(t *testing.T) {
	m, st := quietManager(t)
	n := addNode(t, st, func(n *model.Node) {
		n.Name, n.Host, n.EntryPort = "Tehran", "203.0.113.7", 8443
		n.RealityPublicKey, n.RealityShortID, n.RealitySNI = "PUBLIC-KEY", "0a1b2c3d", "cover.example.com"
	})
	sub, err := m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)

	l, err := m.Link(sub.ID, n.ID)
	must(t, err)
	if l.NodeID != n.ID || l.NodeName != "Tehran" {
		t.Errorf("link = %+v", l)
	}
	p := parseLink(t, l.Vless)
	want := idOnNode(t, st, sub.ID, n.ID).ClientUUID
	if p.id != want || p.host != "203.0.113.7" || p.port != "8443" || p.name != "Sara" {
		t.Errorf("parsed %+v, want id %s at 203.0.113.7:8443 called Sara", p, want)
	}
	for k, v := range map[string]string{
		"security": "reality", "type": "tcp", "encryption": "none", "flow": "xtls-rprx-vision",
		"sni": "cover.example.com", "pbk": "PUBLIC-KEY", "sid": "0a1b2c3d", "fp": "chrome",
	} {
		if got := p.query.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if len(p.query) != 8 {
		t.Errorf("the link has other parameters: %v", p.query)
	}
	// asking again gives the same
	if again, _ := m.Link(sub.ID, n.ID); again != l {
		t.Errorf("a second ask gave %+v", again)
	}
}

// Without an SNI of its own a node's links use the host of its handshake
// target, as the bridge does.
func TestALinkTakesTheHandshakeTargetsHostWhenThereIsNoSNI(t *testing.T) {
	m, st := quietManager(t)
	n := addNode(t, st, func(n *model.Node) { n.RealitySNI, n.RealityTarget = "", "www.cover.example:8443" })
	sub, err := m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)
	l, err := m.Link(sub.ID, n.ID)
	must(t, err)
	if got := parseLink(t, l.Vless).query.Get("sni"); got != "www.cover.example" {
		t.Errorf("sni = %q", got)
	}
}

func TestAddressesOfEveryKindMakeALinkAClientCanRead(t *testing.T) {
	m, st := quietManager(t)
	for host, want := range map[string]string{
		"203.0.113.9":       "203.0.113.9",
		"2001:db8::10":      "2001:db8::10",
		"node2.example.com": "node2.example.com",
	} {
		n := addNode(t, st, func(n *model.Node) { n.Host = host })
		sub, err := m.CreateSubscription(context.Background(), "s "+host, []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		l, err := m.Link(sub.ID, n.ID)
		must(t, err)
		p := parseLink(t, l.Vless)
		if p.host != want || p.port != "443" {
			t.Errorf("%s: parsed host %q port %q from %s", host, p.host, p.port, l.Vless)
		}
		// an IPv6 address is bracketed, or the port can't be told from it
		if strings.Contains(host, ":") && !strings.Contains(l.Vless, "@["+host+"]:443?") {
			t.Errorf("an IPv6 address is not in brackets: %s", l.Vless)
		}
	}
}

// A name in any script comes through a link: as the label a client shows, and
// in a form that is all ASCII, which is what a QR code and a chat message hold.
func TestNamesInAnyScriptComeThroughTheLinkIntact(t *testing.T) {
	m, st := quietManager(t)
	n := addNode(t, st)
	ctx := context.Background()
	names := []string{
		"علی‌رضا کریمی", // a zero-width non-joiner and a space
		"سارا",
		"Émile",
		"李雷",
		"😀 phone",
		"Sara & Ali #1 100% (home/office)?",
		"a+b=c;d,e",
		strings.Repeat("ن", model.MaxNameRunes),
	}
	for _, name := range names {
		sub, err := m.CreateSubscription(ctx, name, []uint{n.ID}, api.SubscriptionLimits{})
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		l, err := m.Link(sub.ID, n.ID)
		must(t, err)
		if !isASCII(l.Vless) {
			t.Errorf("%q: the link has bytes that are not ASCII: %s", name, l.Vless)
		}
		if got := parseLink(t, l.Vless).name; got != name {
			t.Errorf("%q came out of the link as %q", name, got)
		}
		// nothing in the label can end it early or start another part
		if strings.Count(l.Vless, "#") != 1 || strings.Count(l.Vless, "?") != 1 {
			t.Errorf("%q: the label broke the link's shape: %s", name, l.Vless)
		}
	}

	// and a rename shows in the link, with the same id
	sub, err := m.CreateSubscription(ctx, "temp", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)
	before, _ := m.Link(sub.ID, n.ID)
	_, err = m.RenameSubscription(ctx, sub.ID, "نام تازه")
	must(t, err)
	after, err := m.Link(sub.ID, n.ID)
	must(t, err)
	b, a := parseLink(t, before.Vless), parseLink(t, after.Vless)
	if a.name != "نام تازه" || a.id != b.id || a.host != b.host {
		t.Errorf("after a rename: %+v, before %+v", a, b)
	}
}

func TestThereIsNoLinkFromWhatIsNotThereOrSwitchedOff(t *testing.T) {
	m, st := quietManager(t)
	ctx := context.Background()
	on := addNode(t, st)
	never := addNode(t, st)
	dead := addNode(t, st, func(n *model.Node) { n.State = model.NodeInstalling })
	sub, err := m.CreateSubscription(ctx, "Sara", []uint{on.ID}, api.SubscriptionLimits{})
	must(t, err)
	_, err = m.SetEnabled(ctx, sub.ID, on.ID, false)
	must(t, err)

	var plain *api.Error
	for name, c := range map[string]struct{ sub, node uint }{
		"switched off there":      {sub.ID, on.ID},
		"never switched on there": {sub.ID, never.ID},
	} {
		_, err := m.Link(c.sub, c.node)
		if !errors.As(err, &plain) || plain.Code != api.CodeNotFound || !strings.Contains(plain.Msg, "switched off") {
			t.Errorf("%s: %v, want not_found saying it is switched off", name, err)
		}
	}
	for name, c := range map[string]struct{ sub, node uint }{
		"a subscription nobody has": {99, on.ID},
		"a node nobody has":         {sub.ID, 99},
		"a node whose install died": {sub.ID, dead.ID},
		"subscription 0":            {0, on.ID},
		"node 0":                    {sub.ID, 0},
	} {
		if _, err := m.Link(c.sub, c.node); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
	}
	// and switching it back on brings the very same link back
	was, _ := m.Link(sub.ID, on.ID) // fails: off
	if was.Vless != "" {
		t.Error("a link was given for a switch that is off")
	}
	_, err = m.SetEnabled(ctx, sub.ID, on.ID, true)
	must(t, err)
	l, err := m.Link(sub.ID, on.ID)
	must(t, err)
	if parseLink(t, l.Vless).id != idOnNode(t, st, sub.ID, on.ID).ClientUUID {
		t.Error("the link after switching on again doesn't carry the id the state keeps")
	}
}

// The link after off and on again is the one before, character for character.
func TestOffAndOnAgainGivesBackTheSameLink(t *testing.T) {
	m, st := quietManager(t)
	ctx := context.Background()
	n := addNode(t, st)
	sub, err := m.CreateSubscription(ctx, "سارا", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)
	first, err := m.Link(sub.ID, n.ID)
	must(t, err)
	for i := 0; i < 3; i++ {
		_, err = m.SetEnabled(ctx, sub.ID, n.ID, false)
		must(t, err)
		_, err = m.SetEnabled(ctx, sub.ID, n.ID, true)
		must(t, err)
	}
	again, err := m.Link(sub.ID, n.ID)
	must(t, err)
	if again != first {
		t.Errorf("the link changed:\n%s\n%s", first.Vless, again.Vless)
	}
}

func TestAnInstalledNodeThatLacksItsRealityValuesIsTheHubsFault(t *testing.T) {
	m, st := quietManager(t)
	n := addNode(t, st, func(n *model.Node) { n.RealityPublicKey = "" })
	sub, err := m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)
	_, err = m.Link(sub.ID, n.ID)
	var plain *api.Error
	if err == nil || errors.As(err, &plain) || errors.Is(err, model.ErrNotFound) || !strings.Contains(err.Error(), "link of subscription 1 on node 1") {
		t.Errorf("a link with no public key: %v", err)
	}
}

func TestReplacingALinkGivesANewIdAndTheNodeTakesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sub, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		old, err := r.m.Link(sub.ID, n.ID)
		must(t, err)
		oldID := parseLink(t, old.Vless).id
		calls := ag.callCount()

		fresh, err := r.m.ReplaceLink(ctx, sub.ID, n.ID)
		must(t, err)
		p := parseLink(t, fresh.Vless)
		if p.id == oldID || !lowerUUID.MatchString(p.id) || p.name != "Sara" || p.host != parseLink(t, old.Vless).host {
			t.Errorf("new link %s, old %s", fresh.Vless, old.Vless)
		}
		if fresh.NodeID != n.ID || fresh.NodeName != n.Name {
			t.Errorf("link = %+v", fresh)
		}
		// the state, the node and what is handed out now agree, and the old id is gone
		if got := idOnNode(t, r.cfg.Store, sub.ID, n.ID); got.ClientUUID != p.id || !got.Enabled {
			t.Errorf("the state holds %+v", got)
		}
		if got, ok := holds(ag, subEmail(sub.ID)); !ok || got != p.id {
			t.Errorf("the node holds %q (%v), want the new id %q", got, ok, p.id)
		}
		again, err := r.m.Link(sub.ID, n.ID)
		must(t, err)
		if again != fresh {
			t.Error("asking for the link after replacing it gives another")
		}
		// the node was told to take the old client off and put the new one on, in that order
		if got := ag.callsFrom(calls); !slices.Equal(got, []string{"RemoveUser " + subEmail(sub.ID), "AddUser " + subEmail(sub.ID)}) {
			t.Errorf("the node saw %v", got)
		}
		events := r.m.NodeEvents(n.ID)
		if got := kinds(events); !slices.Equal(got, []string{EventSubscriptionOn, EventSubscriptionOn}) || events[0].Detail != "Sara (new link)" {
			t.Errorf("events = %+v", events)
		}

		// every replacement is another id
		third, err := r.m.ReplaceLink(ctx, sub.ID, n.ID)
		must(t, err)
		if id := parseLink(t, third.Vless).id; id == p.id || id == oldID {
			t.Errorf("the third link reuses an id: %s", id)
		}
	})
}

// A link is replaced to cut off the old one, so it can't be done for a node that
// can't be asked: the reconcile couldn't tell the new id from the old.
func TestALinkIsNotReplacedWhileTheNodeCantBeReached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sub, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		old, _ := r.m.Link(sub.ID, n.ID)

		ag.setDown(true)
		r.advance(time.Minute)
		writes, events := r.cfg.Store.Writes(), len(r.m.NodeEvents(n.ID))
		_, err = r.m.ReplaceLink(ctx, sub.ID, n.ID)
		var refusal *api.Error
		if !errors.As(err, &refusal) || refusal.Code != api.CodeNodeUnreachable {
			t.Fatalf("with the node down: %v, want node_unreachable", err)
		}
		if got, _ := r.m.Link(sub.ID, n.ID); got != old {
			t.Error("the link changed although the node never took the new id")
		}
		if r.cfg.Store.Writes() != writes || len(r.m.NodeEvents(n.ID)) != events {
			t.Error("a refused replacement wrote the state or made an event")
		}

		// back, and it works
		ag.setDown(false)
		r.advance(time.Minute)
		if _, err := r.m.ReplaceLink(ctx, sub.ID, n.ID); err != nil {
			t.Errorf("after the node is back: %v", err)
		}
	})
}

func TestALinkIsNotReplacedForAManagerThatIsNotRunning(t *testing.T) {
	m, st := quietManager(t)
	n := addNode(t, st)
	sub, err := m.CreateSubscription(context.Background(), "Sara", []uint{n.ID}, api.SubscriptionLimits{})
	must(t, err)
	var refusal *api.Error
	if _, err := m.ReplaceLink(context.Background(), sub.ID, n.ID); !errors.As(err, &refusal) || refusal.Code != api.CodeNodeUnreachable {
		t.Errorf("replace with no connection: %v", err)
	}
}

func TestReplacingALinkIsRefusedForWhatIsNotThere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		on, other := addNode(t, r.cfg.Store), addNode(t, r.cfg.Store)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sub, err := r.m.CreateSubscription(ctx, "Sara", []uint{on.ID}, api.SubscriptionLimits{})
		must(t, err)
		_, err = r.m.SetEnabled(ctx, sub.ID, on.ID, false)
		must(t, err)

		var plain *api.Error
		if _, err := r.m.ReplaceLink(ctx, sub.ID, on.ID); !errors.As(err, &plain) || !strings.Contains(plain.Msg, "switched off") {
			t.Errorf("switched off: %v", err)
		}
		if _, err := r.m.ReplaceLink(ctx, sub.ID, other.ID); !errors.As(err, &plain) || !strings.Contains(plain.Msg, "switched off") {
			t.Errorf("never on: %v", err)
		}
		for _, c := range [][2]uint{{99, on.ID}, {sub.ID, 99}, {0, 0}} {
			if _, err := r.m.ReplaceLink(ctx, c[0], c[1]); !errors.Is(err, model.ErrNotFound) {
				t.Errorf("replace %v: %v", c, err)
			}
		}
	})
}

// addRefuser is a connector whose clients can be told to refuse the next
// AddUser, once, to leave a replacement half done.
type addRefuser struct {
	Connector
	armed atomic.Bool
}

func (a *addRefuser) Connect(ctx context.Context, n model.Node) (Client, error) {
	c, err := a.Connector.Connect(ctx, n)
	if err != nil {
		return nil, err
	}
	return &refusingConn{Client: c, a: a}, nil
}

type refusingConn struct {
	Client
	a *addRefuser
}

func (c *refusingConn) AddUser(ctx context.Context, tag, email, uuid, protocol, flow, enc string, level int32) error {
	if c.a.armed.CompareAndSwap(true, false) {
		return errors.New("the agent said no")
	}
	return c.Client.AddUser(ctx, tag, email, uuid, protocol, flow, enc, level)
}

// The node took the old client off and refused the new one. The state keeps
// the old id, the call says the node didn't take it, and the reconcile that the
// failure starts puts the old client back, so the link that is out still works.
func TestAReplacementTheNodeRefusesLeavesTheOldLinkAndRestoresIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		flaky := &addRefuser{}
		r := newRig(t, func(c *Config) { flaky.Connector = c.Connector; c.Connector = flaky })
		n := addNode(t, r.cfg.Store)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sub, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		old, _ := r.m.Link(sub.ID, n.ID)
		oldID := parseLink(t, old.Vless).id
		made, _ := r.net.opened(n.ID)

		flaky.armed.Store(true)
		_, err = r.m.ReplaceLink(ctx, sub.ID, n.ID)
		var refusal *api.Error
		if !errors.As(err, &refusal) || refusal.Code != api.CodeNodeUnreachable || !strings.Contains(refusal.Msg, "did not take") {
			t.Fatalf("the refused replacement: %v", err)
		}
		if got, _ := r.m.Link(sub.ID, n.ID); got != old {
			t.Error("the link changed although the node refused the new id")
		}
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventSubscriptionOn}) {
			t.Errorf("a refused replacement made an event: %v", got)
		}

		// the old client was taken off before the new one was refused, and the
		// reconnect the failure asks for puts it back
		r.advance(time.Minute)
		if ag.count("RemoveUser") != 1 {
			t.Errorf("the node saw %d removals, so the replacement was not left half done", ag.count("RemoveUser"))
		}
		if got, ok := holds(ag, subEmail(sub.ID)); !ok || got != oldID {
			t.Errorf("after the reconcile the node holds %q (%v), want the old id %q", got, ok, oldID)
		}
		if after, _ := r.net.opened(n.ID); after <= made {
			t.Error("the node was not asked to reconnect")
		}
	})
}

// The node took the new id and the state could not be written. The node gets
// the old id back, so what it holds is what the state says.
func TestAReplacementThatCantBeSavedPutsTheNodeBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var flaky *flakyStore
		r := newRig(t, func(c *Config) { flaky = &flakyStore{Store: c.Store}; c.Store = flaky })
		n := addNode(t, flaky)
		ag := r.net.agent(n.ID)
		r.start()
		defer r.stop()
		ctx := context.Background()
		sub, err := r.m.CreateSubscription(ctx, "Sara", []uint{n.ID}, api.SubscriptionLimits{})
		must(t, err)
		oldID := idOnNode(t, flaky, sub.ID, n.ID).ClientUUID

		flaky.failing.Store(true)
		_, err = r.m.ReplaceLink(ctx, sub.ID, n.ID)
		if err == nil || !strings.Contains(err.Error(), "disk is full") {
			t.Fatalf("the replacement with no disk: %v", err)
		}
		if got, ok := holds(ag, subEmail(sub.ID)); !ok || got != oldID {
			t.Errorf("the node holds %q (%v), want the old id %q back", got, ok, oldID)
		}
		flaky.failing.Store(false)
		if got := idOnNode(t, flaky, sub.ID, n.ID).ClientUUID; got != oldID {
			t.Error("the state changed")
		}
	})
}
