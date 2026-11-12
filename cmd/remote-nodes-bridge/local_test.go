// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	appstats "github.com/xtls/xray-core/app/stats"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/proxy/vless"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

const (
	sub1ID = "11111111-2222-4333-8444-555555555551"
	sub2ID = "11111111-2222-4333-8444-555555555552"
	sub3ID = "11111111-2222-4333-8444-555555555553"
)

var sub1, sub2, sub3 = nodecfg.SubscriptionEmail(1), nodecfg.SubscriptionEmail(2), nodecfg.SubscriptionEmail(3)

func statusManager(t *testing.T) *appstats.Manager {
	t.Helper()
	mgr, err := appstats.NewManager(context.Background(), &appstats.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

// closeSpy is a client's connection to the entry that says when it is closed.
type closeSpy struct {
	net.Conn
	mu     sync.Mutex
	closed int
}

func (c *closeSpy) Close() error {
	c.mu.Lock()
	c.closed++
	c.mu.Unlock()
	return nil
}

func (c *closeSpy) closes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// inboundOf is the session of a client logged in on the entry as email with
// id, from addr, over conn.
func inboundOf(email, id, addr string, conn net.Conn) *session.Inbound {
	u, err := localUser(bridgespec.Client{Email: email, UUID: id}, "")
	if err != nil {
		panic(err)
	}
	mu, err := u.ToMemoryUser()
	if err != nil {
		panic(err)
	}
	return &session.Inbound{
		Tag:    bridgespec.LocalTag,
		Source: xnet.TCPDestination(xnet.ParseAddress(addr), 50000),
		User:   mu,
		Conn:   conn,
	}
}

func flowsFor(clk *clock, clients map[string]string) *localFlows {
	f := newLocalFlows(clk.Now)
	f.allow(clients)
	return f
}

func TestFlowsCountAClientsConnections(t *testing.T) {
	clk := newClock()
	f := flowsFor(clk, map[string]string{sub1: sub1ID, sub2: sub2ID})
	if n, addrs, last := f.snapshot(sub1); n != 0 || addrs != nil || !last.IsZero() {
		t.Fatalf("before anything: %d %v %v", n, addrs, last)
	}

	phone, laptop := &closeSpy{}, &closeSpy{}
	first := clk.Now()
	// two flows over one connection, one over another, from two addresses
	a := f.open(inboundOf(sub1, sub1ID, "198.51.100.7", phone))
	b := f.open(inboundOf(sub1, sub1ID, "198.51.100.7", phone))
	clk.Advance(time.Minute)
	c := f.open(inboundOf(sub1, sub1ID, "203.0.113.9", laptop))
	if a == nil || b == nil || c == nil {
		t.Fatal("a client on the entry was turned away")
	}
	n, addrs, last := f.snapshot(sub1)
	if n != 2 || !reflect.DeepEqual(addrs, []string{"198.51.100.7", "203.0.113.9"}) || !last.Equal(first.Add(time.Minute)) {
		t.Fatalf("two connections open: %d %v %v", n, addrs, last)
	}
	if n, _, _ := f.snapshot(sub2); n != 0 {
		t.Fatalf("the other client shows %d", n)
	}

	// a connection counts until its last flow ends; ending one twice is once
	a()
	a()
	if n, _, _ := f.snapshot(sub1); n != 2 {
		t.Fatalf("one of two flows on the phone ended: %d connections", n)
	}
	b()
	if n, addrs, _ := f.snapshot(sub1); n != 1 || !reflect.DeepEqual(addrs, []string{"203.0.113.9"}) {
		t.Fatalf("the phone is done: %d %v", n, addrs)
	}
	c()
	n, addrs, last = f.snapshot(sub1)
	if n != 0 || len(addrs) != 0 || !last.Equal(first.Add(time.Minute)) {
		t.Fatalf("all ended: %d %v, last seen %v", n, addrs, last)
	}
	if phone.closes()+laptop.closes() != 0 {
		t.Error("following a flow closed the client's connection")
	}
}

// Two connections from one address are two connections and one address.
func TestFlowsTellConnectionsFromAddresses(t *testing.T) {
	f := flowsFor(newClock(), map[string]string{sub1: sub1ID})
	f.open(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{}))
	f.open(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{}))
	if n, addrs, _ := f.snapshot(sub1); n != 2 || !reflect.DeepEqual(addrs, []string{"198.51.100.7"}) {
		t.Fatalf("%d %v", n, addrs)
	}
}

func TestCutClosesAClientsConnectionsOnly(t *testing.T) {
	f := flowsFor(newClock(), map[string]string{sub1: sub1ID, sub2: sub2ID})
	phone, laptop, other := &closeSpy{}, &closeSpy{}, &closeSpy{}
	done := f.open(inboundOf(sub1, sub1ID, "198.51.100.7", phone))
	f.open(inboundOf(sub1, sub1ID, "198.51.100.7", phone))
	f.open(inboundOf(sub1, sub1ID, "203.0.113.9", laptop))
	f.open(inboundOf(sub2, sub2ID, "192.0.2.5", other))

	if n := f.cut(sub1); n != 2 {
		t.Fatalf("cut %d connections, want 2", n)
	}
	if phone.closes() != 1 || laptop.closes() != 1 {
		t.Errorf("closed the phone %d times and the laptop %d, want once each", phone.closes(), laptop.closes())
	}
	if other.closes() != 0 {
		t.Error("the other client's connection was closed too")
	}
	if n, _, _ := f.snapshot(sub1); n != 0 {
		t.Errorf("%d connections after the cut", n)
	}
	if n, _, _ := f.snapshot(sub2); n != 1 {
		t.Errorf("the other client shows %d", n)
	}
	// a flow that ends after the cut, as they all do, must not count below zero
	done()
	if n, _, _ := f.snapshot(sub1); n != 0 {
		t.Errorf("%d after a cut flow ended", n)
	}
	if n := f.cut(sub1); n != 0 {
		t.Errorf("a second cut found %d", n)
	}
	if n := f.cut("nobody@remote-nodes"); n != 0 {
		t.Errorf("cutting a client never seen found %d", n)
	}
}

// A flow from a login that is no longer good is turned away and its
// connection closed: the client was taken off, or put back under another id,
// between its login and this flow.
func TestFlowsTurnAwayAClientThatIsOff(t *testing.T) {
	f := flowsFor(newClock(), map[string]string{sub1: sub1ID})
	for name, in := range map[string]*session.Inbound{
		"taken off":        inboundOf(sub2, sub2ID, "192.0.2.5", &closeSpy{}),
		"under another id": inboundOf(sub1, sub3ID, "192.0.2.5", &closeSpy{}),
	} {
		if done := f.open(in); done != nil {
			t.Errorf("%s: let in", name)
		}
		if got := in.Conn.(*closeSpy).closes(); got != 1 {
			t.Errorf("%s: its connection was closed %d times, want once", name, got)
		}
		if n, _, _ := f.snapshot(in.User.Email); n != 0 {
			t.Errorf("%s: counted", name)
		}
	}
	// the id in capitals is the same id
	if done := f.open(inboundOf(sub1, strings.ToUpper(sub1ID), "192.0.2.5", &closeSpy{})); done == nil {
		t.Error("an id in capitals was turned away")
	}
}

// What the entry's clients open comes back from the dialer followed: TCP as
// a wrapper that still is a TCP connection to the core, UDP as the dialer's
// own wrapper around a socket that says when it closes.
func TestWrapFollowsTheEntrysFlows(t *testing.T) {
	clk := newClock()
	s := localSpec()
	tr := newTracker(s, clk.Now)
	tr.local.allow(map[string]string{sub1: sub1ID})
	internet := xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443)
	ctx := func(in *session.Inbound) context.Context {
		return session.ContextWithInbound(context.Background(), in)
	}

	c, _ := tcpPair(t)
	got := tr.wrap(ctx(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{})), nil, internet, c)
	tcp, ok := got.(*localTCP)
	if !ok || tcp.TCPConn != c {
		t.Fatalf("TCP came back as %T", got)
	}
	if n, _, _ := tr.local.snapshot(sub1); n != 1 {
		t.Fatalf("%d connections with a TCP flow open", n)
	}
	got.Close()
	if n, _, _ := tr.local.snapshot(sub1); n != 0 {
		t.Fatalf("%d connections after the flow closed", n)
	}

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dest := &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 53}
	made := &xnet.PacketConnWrapper{PacketConn: pc, Dest: dest}
	got = tr.wrap(ctx(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{})), nil, xnet.UDPDestination(xnet.ParseAddress("1.1.1.1"), 53), made)
	udp, ok := got.(*xnet.PacketConnWrapper)
	if !ok || udp == made || udp.Dest != net.Addr(dest) {
		t.Fatalf("UDP came back as %T, want the dialer's wrapper around a followed socket", got)
	}
	if lp, ok := udp.PacketConn.(*localPacket); !ok || lp.PacketConn != pc {
		t.Fatalf("the socket inside is %T", udp.PacketConn)
	}
	if n, _, _ := tr.local.snapshot(sub1); n != 1 {
		t.Fatalf("%d connections with a UDP flow open", n)
	}
	got.Close()
	if n, _, _ := tr.local.snapshot(sub1); n != 0 {
		t.Fatalf("%d connections after the UDP flow closed", n)
	}
	if _, err := pc.WriteTo([]byte("x"), dest); err == nil {
		t.Error("the socket under the UDP flow is still open after Close")
	}

	// the tunnels are untouched by any of it
	for i := range tr.nodes {
		if _, _, n := tr.nodes[i].snapshot(); n != 0 {
			t.Errorf("node %d counts %d of the entry's flows", i, n)
		}
	}
}

func TestWrapLeavesOtherLocalSocketsAlone(t *testing.T) {
	clk := newClock()
	tr := newTracker(localSpec(), clk.Now)
	tr.local.allow(map[string]string{sub1: sub1ID})
	internet := xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443)

	// a flow on the entry with no user: nothing to count it under
	c, _ := tcpPair(t)
	noUser := session.ContextWithInbound(context.Background(), &session.Inbound{Tag: bridgespec.LocalTag})
	if got := tr.wrap(noUser, nil, internet, c); got != net.Conn(c) {
		t.Fatalf("a flow with no user came back as %T", got)
	}
	// a kind of socket whose close can't be seen: passed on, not counted
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	in := inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{})
	if got := tr.wrap(session.ContextWithInbound(context.Background(), in), nil, internet, a); got != net.Conn(a) {
		t.Fatalf("a pipe came back as %T", got)
	}
	if n, _, _ := tr.local.snapshot(sub1); n != 0 {
		t.Fatalf("counted %d", n)
	}

	// without an entry in the spec the tag means nothing
	plain := newTracker(testSpec(), clk.Now)
	c2, _ := tcpPair(t)
	if got := plain.wrap(session.ContextWithInbound(context.Background(), in), nil, internet, c2); got != net.Conn(c2) {
		t.Fatalf("with no entry it came back as %T", got)
	}
}

// A flow of a client that is off comes back closed, with the client's own
// connection closed too.
func TestWrapClosesAFlowOfAClientThatIsOff(t *testing.T) {
	tr := newTracker(localSpec(), newClock().Now)
	tr.local.allow(map[string]string{})
	c, far := tcpPair(t)
	spy := &closeSpy{}
	in := inboundOf(sub1, sub1ID, "198.51.100.7", spy)
	got := tr.wrap(session.ContextWithInbound(context.Background(), in), nil, xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443), c)
	if _, followed := got.(*localTCP); followed {
		t.Fatal("followed a flow of a client that is off")
	}
	if spy.closes() != 1 {
		t.Errorf("the client's connection was closed %d times", spy.closes())
	}
	far.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := far.Read(make([]byte, 1)); err == nil {
		t.Error("the flow's own socket is still open")
	}
}

// fakeUsers is an inbound's user manager that remembers what it was told.
type fakeUsers struct {
	mu    sync.Mutex
	users map[string]string // email to id
	log   []string
	fail  map[string]error // by "add <email>" or "remove <email>"
}

func newFakeUsers(clients ...bridgespec.Client) *fakeUsers {
	f := &fakeUsers{users: map[string]string{}, fail: map[string]error{}}
	for _, c := range clients {
		f.users[c.Email] = c.UUID
	}
	return f
}

func (f *fakeUsers) AddUser(_ context.Context, u *protocol.MemoryUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["add "+u.Email]; err != nil {
		return err
	}
	if _, has := f.users[u.Email]; has {
		return fmt.Errorf("User %s already exists.", u.Email)
	}
	acct := u.Account.(*vless.MemoryAccount)
	f.users[u.Email] = acct.ID.String()
	f.log = append(f.log, "add "+u.Email+" flow="+acct.Flow)
	return nil
}

func (f *fakeUsers) RemoveUser(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["remove "+email]; err != nil {
		return err
	}
	if _, has := f.users[email]; !has {
		return fmt.Errorf("User %s not found.", email)
	}
	delete(f.users, email)
	f.log = append(f.log, "remove "+email)
	return nil
}

func (f *fakeUsers) has() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.users {
		out[k] = v
	}
	return out
}

// testEntry is the entry of localSpec (subscriptions 1 and 2, Vision) over a
// fake inbound that starts with the same two.
func testEntry(t *testing.T) (*entry, *fakeUsers) {
	t.Helper()
	s := localSpec()
	e := newEntry(s, newLocalFlows(newClock().Now))
	users := newFakeUsers(s.Local.Clients...)
	e.users = users
	return e, users
}

func client(email, id string) bridgespec.Client { return bridgespec.Client{Email: email, UUID: id} }

func TestSetClients(t *testing.T) {
	e, users := testEntry(t)
	ctx := context.Background()

	// the set it already has: nothing to do
	res, err := e.set(ctx, []bridgespec.Client{client(sub2, sub2ID), client(sub1, strings.ToUpper(sub1ID))})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added)+len(res.Removed)+len(res.Replaced) != 0 || res.Closed != 0 || len(users.log) != 0 {
		t.Fatalf("the same set again: %+v, inbound told %v", res, users.log)
	}

	// 1 leaves, 2 gets another id, 3 is new
	open1, open2 := &closeSpy{}, &closeSpy{}
	e.flows.open(inboundOf(sub1, sub1ID, "198.51.100.7", open1))
	e.flows.open(inboundOf(sub2, sub2ID, "203.0.113.9", open2))
	res, err = e.set(ctx, []bridgespec.Client{client(sub3, sub3ID), client(sub2, sub1ID)})
	if err != nil {
		t.Fatal(err)
	}
	want := bridgespec.ClientsResult{Added: []string{sub3}, Removed: []string{sub1}, Replaced: []string{sub2}, Closed: 2}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("got %+v, want %+v", res, want)
	}
	if got := users.has(); !reflect.DeepEqual(got, map[string]string{sub2: sub1ID, sub3: sub3ID}) {
		t.Fatalf("the inbound has %v", got)
	}
	// off before on, and each new login with the entry's flow
	wantLog := []string{
		"remove " + sub1, "remove " + sub2,
		"add " + sub2 + " flow=" + bridgespec.VisionFlow, "add " + sub3 + " flow=" + bridgespec.VisionFlow,
	}
	if !reflect.DeepEqual(users.log, wantLog) {
		t.Fatalf("the inbound was told %v, want %v", users.log, wantLog)
	}
	if open1.closes() != 1 || open2.closes() != 1 {
		t.Errorf("the removed client's connection closed %d times, the replaced one's %d, want once each", open1.closes(), open2.closes())
	}

	// what may be connected follows: 1 is out, 2 only under its new id
	if e.flows.open(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{})) != nil {
		t.Error("the removed client is still let in")
	}
	if e.flows.open(inboundOf(sub2, sub2ID, "203.0.113.9", &closeSpy{})) != nil {
		t.Error("the replaced client is still let in under its old id")
	}
	if e.flows.open(inboundOf(sub2, sub1ID, "203.0.113.9", &closeSpy{})) == nil {
		t.Error("the replaced client is turned away under its new id")
	}

	// nobody at all
	res, err = e.set(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{sub2, sub3}) || len(users.has()) != 0 {
		t.Fatalf("emptying it: %+v, inbound has %v", res, users.has())
	}
}

// A set that can't stand as a whole changes nothing.
func TestSetClientsRefusesABadSet(t *testing.T) {
	for name, set := range map[string][]bridgespec.Client{
		"an id that is no UUID": {client(sub1, "nope")},
		"one email twice":       {client(sub1, sub1ID), client(sub1, sub2ID)},
		"one id twice":          {client(sub1, sub1ID), client(sub2, sub1ID)},
		"a tunnel's tag":        {client("rvs-link-1", sub3ID)},
		"an email with a space": {client("sub 1", sub1ID)},
		"too many":              make([]bridgespec.Client, bridgespec.MaxLocalClients+1),
	} {
		t.Run(name, func(t *testing.T) {
			e, users := testEntry(t)
			_, err := e.set(context.Background(), set)
			if !errors.Is(err, errBadClients) {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), sub1ID) || strings.Contains(err.Error(), sub2ID) {
				t.Errorf("the error carries an id: %v", err)
			}
			if len(users.log) != 0 {
				t.Errorf("the inbound was told %v", users.log)
			}
		})
	}
}

// A step that fails leaves the clients as far as it got, known as they are,
// and the same set sent again finishes the job.
func TestSetClientsGoesOnAfterAFailure(t *testing.T) {
	e, users := testEntry(t)
	users.fail["add "+sub3] = errors.New("the inbound is busy")
	want := []bridgespec.Client{client(sub2, sub2ID), client(sub3, sub3ID)}

	res, err := e.set(context.Background(), want)
	if err == nil || !strings.Contains(err.Error(), "the inbound is busy") || errors.Is(err, errBadClients) {
		t.Fatalf("got %v", err)
	}
	if !reflect.DeepEqual(res.Removed, []string{sub1}) || len(res.Added) != 0 {
		t.Fatalf("what it had done by then: %+v", res)
	}
	if e.flows.open(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{})) != nil {
		t.Error("the client it did take off is still let in")
	}
	if e.flows.open(inboundOf(sub3, sub3ID, "198.51.100.7", &closeSpy{})) != nil {
		t.Error("the client it failed to add is let in")
	}

	delete(users.fail, "add "+sub3)
	res, err = e.set(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Added, []string{sub3}) || len(res.Removed) != 0 {
		t.Fatalf("the second try: %+v", res)
	}
	if got := users.has(); !reflect.DeepEqual(got, map[string]string{sub2: sub2ID, sub3: sub3ID}) {
		t.Fatalf("the inbound has %v", got)
	}
}

// The inbound not having a client it is asked to drop is where the caller
// wanted to end up.
func TestSetClientsToleratesAClientAlreadyGone(t *testing.T) {
	e, users := testEntry(t)
	users.mu.Lock()
	delete(users.users, sub1)
	users.mu.Unlock()
	res, err := e.set(context.Background(), []bridgespec.Client{client(sub2, sub2ID)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{sub1}) {
		t.Fatalf("%+v", res)
	}
}

func TestStatusListsTheEntrysClients(t *testing.T) {
	clk := newClock()
	mgr := statusManager(t)
	s := localSpec()
	tr := newTracker(s, clk.Now)
	e := newEntry(s, tr.local)
	e.users = newFakeUsers(s.Local.Clients...)
	b := &bridge{spec: s, stats: mgr, tracker: tr, entry: e, started: clk.Now()}

	setCounter(t, mgr, "user>>>"+sub1+">>>traffic>>>uplink", 1000)
	setCounter(t, mgr, "user>>>"+sub1+">>>traffic>>>downlink", 9000)
	e.flows.open(inboundOf(sub1, sub1ID, "198.51.100.7", &closeSpy{}))
	seen := clk.Now()
	clk.Advance(time.Minute)

	want := []bridgespec.ClientStatus{
		{Email: sub1, On: true, BytesUp: 1000, BytesDown: 9000, Connections: 1, Addrs: []string{"198.51.100.7"}, LastSeen: seen},
		{Email: sub2, On: true},
	}
	if got := b.status().Local; got == nil || !reflect.DeepEqual(got.Clients, want) {
		t.Fatalf("clients\n got %+v\nwant %+v", got, want)
	}

	// taken off, a client stays listed with what it moved, as off
	if _, err := e.set(context.Background(), []bridgespec.Client{client(sub2, sub2ID), client(sub3, sub3ID)}); err != nil {
		t.Fatal(err)
	}
	want = []bridgespec.ClientStatus{
		{Email: sub1, BytesUp: 1000, BytesDown: 9000, LastSeen: seen},
		{Email: sub2, On: true},
		{Email: sub3, On: true},
	}
	if got := b.status().Local.Clients; !reflect.DeepEqual(got, want) {
		t.Fatalf("after a change\n got %+v\nwant %+v", got, want)
	}

	// and a bridge with no entry says nothing of one
	plain, _ := statusBridge(t, clk)
	if plain.status().Local != nil {
		t.Error("a bridge with no entry reports one")
	}
}

func putClients(t *testing.T, url, token string, body any) (*http.Response, string) {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPut, url+bridgespec.ClientsPath, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set(bridgespec.TokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp, buf.String()
}

func TestClientsEndpoint(t *testing.T) {
	clk := newClock()
	s := localSpec()
	tr := newTracker(s, clk.Now)
	e := newEntry(s, tr.local)
	users := newFakeUsers(s.Local.Clients...)
	e.users = users
	b := &bridge{spec: s, stats: statusManager(t), tracker: tr, entry: e, started: clk.Now()}
	srv := httptest.NewServer(b.handler())
	defer srv.Close()
	token := s.Local.Token
	set := bridgespec.ClientsRequest{Clients: []bridgespec.Client{client(sub2, sub2ID), client(sub3, sub3ID)}}

	// only the hub, which has the token, may change them
	for _, bad := range []string{"", "wrong", token + "x", strings.ToUpper(token)} {
		if resp, _ := putClients(t, srv.URL, bad, set); resp.StatusCode != http.StatusForbidden {
			t.Errorf("token %q: %s", bad, resp.Status)
		}
	}
	if len(users.log) != 0 {
		t.Fatalf("a refused request changed the inbound: %v", users.log)
	}

	for name, body := range map[string]string{
		"not JSON":         `clients`,
		"an unknown field": `{"clients":[],"restart":true}`,
		"a bad client":     `{"clients":[{"email":"sub-1@remote-nodes","uuid":"nope"}]}`,
		"too large":        `{"clients":[],"pad":"` + strings.Repeat("x", bridgespec.MaxSize) + `"}`,
	} {
		if resp, _ := putClients(t, srv.URL, token, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %s", name, resp.Status)
		}
	}
	if len(users.log) != 0 {
		t.Fatalf("a bad request changed the inbound: %v", users.log)
	}

	resp, body := putClients(t, srv.URL, token, set)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%s %q: %s", resp.Status, resp.Header.Get("Content-Type"), body)
	}
	var res bridgespec.ClientsResult
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res, bridgespec.ClientsResult{Added: []string{sub3}, Removed: []string{sub1}, Replaced: []string{}}) {
		t.Fatalf("result %+v", res)
	}
	if strings.Contains(body, sub3ID) || strings.Contains(body, token) {
		t.Errorf("the answer carries an id or the token: %s", body)
	}

	// the inbound failing is the bridge's fault, not the request's
	users.fail["add "+sub1] = errors.New("the inbound is busy")
	back := bridgespec.ClientsRequest{Clients: []bridgespec.Client{client(sub1, sub1ID)}}
	if resp, _ := putClients(t, srv.URL, token, back); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("a failing inbound: %s", resp.Status)
	}

	// it is a PUT and nothing else
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req, _ := http.NewRequest(method, srv.URL+bridgespec.ClientsPath, nil)
		req.Header.Set(bridgespec.TokenHeader, token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %s", method, r.Status)
		}
	}
}

// A bridge with no entry has nothing to change: the path isn't there.
func TestNoEntryNoClientsEndpoint(t *testing.T) {
	b, _ := statusBridge(t, newClock())
	srv := httptest.NewServer(b.handler())
	defer srv.Close()
	if resp, _ := putClients(t, srv.URL, "0123456789abcdef0123456789abcdef", bridgespec.ClientsRequest{}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("%s", resp.Status)
	}
}
