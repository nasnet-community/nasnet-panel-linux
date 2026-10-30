// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// clock is a time source a test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// tcpPair is two ends of a loopback connection.
func tcpPair(t testing.TB) (client, server *net.TCPConn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- c.(*net.TCPConn)
	}()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { c.Close(); server.Close() })
	return c.(*net.TCPConn), server
}

func portalOf(s *bridgespec.Spec, i int) (xnet.Address, xnet.Destination) {
	n := s.Nodes[i]
	return xnet.ParseAddress(s.DialAddr), xnet.TCPDestination(xnet.ParseAddress(n.Addr), xnet.Port(n.Port))
}

func tunnelState(t *testing.T, tr *tracker, i int) (up bool, since time.Time) {
	t.Helper()
	tr.refresh()
	up, since, _ = tr.nodes[i].snapshot()
	return
}

func TestTunnelIsUpOnceItHasLastedAndBeenHeardFrom(t *testing.T) {
	clk := newClock()
	s := testSpec()
	tr := newTracker(s, clk.Now)
	start := clk.Now()

	if up, since := tunnelState(t, tr, 0); up || !since.Equal(start) {
		t.Fatalf("a tunnel never connected: up=%v since=%v, want down since the start", up, since)
	}

	client, server := tcpPair(t)
	src, dest := portalOf(s, 0)
	conn := tr.wrap(context.Background(), src, dest, client)
	link, ok := conn.(*linkConn)
	if !ok {
		t.Fatalf("a connection to the portal comes back as %T", conn)
	}

	// Just opened, nothing heard.
	if up, _ := tunnelState(t, tr, 0); up {
		t.Error("up the moment it opened")
	}

	// The portal speaks, but the link is young.
	server.Write([]byte("x"))
	if _, err := link.Read(make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(upAfter - time.Second)
	if up, _ := tunnelState(t, tr, 0); up {
		t.Errorf("up after %s, which is less than %s", upAfter-time.Second, upAfter)
	}

	// Old enough, and heard from just now: up, since this moment.
	clk.Advance(time.Second)
	up, since := tunnelState(t, tr, 0)
	if !up || !since.Equal(clk.Now()) {
		t.Fatalf("up=%v since=%v, want up since %v", up, since, clk.Now())
	}
	firstUp := since

	// Quiet, but within the keepalive allowance (counted from the last
	// byte): still up, and since doesn't move.
	clk.Advance(staleAfter - upAfter)
	if up, since := tunnelState(t, tr, 0); !up || !since.Equal(firstUp) {
		t.Errorf("quiet but not stale: up=%v since=%v", up, since)
	}

	// Silent for too long: the connection is open but the tunnel is not.
	clk.Advance(2 * time.Second)
	up, since = tunnelState(t, tr, 0)
	if up || !since.Equal(clk.Now()) {
		t.Fatalf("stale: up=%v since=%v, want down since %v", up, since, clk.Now())
	}

	// It speaks again.
	server.Write([]byte("y"))
	if _, err := link.Read(make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if up, _ := tunnelState(t, tr, 0); !up {
		t.Error("not up again after the portal spoke")
	}

	// Closing the connection takes the tunnel down.
	clk.Advance(time.Second)
	if err := link.Close(); err != nil {
		t.Fatal(err)
	}
	up, since = tunnelState(t, tr, 0)
	if up || !since.Equal(clk.Now()) {
		t.Fatalf("closed: up=%v since=%v", up, since)
	}
	link.Close() // a second close is harmless

	// The other node never moved.
	if up, since := tunnelState(t, tr, 1); up || !since.Equal(start) {
		t.Errorf("node 2: up=%v since=%v", up, since)
	}
}

// A connection that the portal refuses a moment after the handshake started
// must not show as up, even though it was heard from.
func TestShortLivedConnectionsNeverCount(t *testing.T) {
	clk := newClock()
	s := testSpec()
	tr := newTracker(s, clk.Now)
	start := clk.Now()

	client, server := tcpPair(t)
	src, dest := portalOf(s, 0)
	link := tr.wrap(context.Background(), src, dest, client).(*linkConn)
	server.Write([]byte("server hello"))
	link.Read(make([]byte, 32))
	clk.Advance(time.Second)
	if up, _ := tunnelState(t, tr, 0); up {
		t.Fatal("up after one second")
	}
	link.Close()
	clk.Advance(upAfter)
	if up, since := tunnelState(t, tr, 0); up || !since.Equal(start) {
		t.Fatalf("up=%v since=%v: a refused handshake changed the state", up, since)
	}
}

// With several connections the tunnel is up while any of them is.
func TestTunnelWithTwoConnections(t *testing.T) {
	clk := newClock()
	s := testSpec()
	tr := newTracker(s, clk.Now)
	src, dest := portalOf(s, 0)

	open := func() (*linkConn, *net.TCPConn) {
		c, srv := tcpPair(t)
		l := tr.wrap(context.Background(), src, dest, c).(*linkConn)
		srv.Write([]byte("x"))
		l.Read(make([]byte, 1))
		return l, srv
	}
	a, _ := open()
	clk.Advance(upAfter)
	if up, _ := tunnelState(t, tr, 0); !up {
		t.Fatal("not up with one connection")
	}
	b, _ := open()
	a.Close()
	if up, _ := tunnelState(t, tr, 0); up {
		// b is new: the tunnel is down until it has lasted.
		t.Fatal("up on a connection that is a moment old")
	}
	clk.Advance(upAfter)
	if up, _ := tunnelState(t, tr, 0); !up {
		t.Fatal("not up on the second connection")
	}
	b.Close()
	if up, _ := tunnelState(t, tr, 0); up {
		t.Fatal("up with no connection")
	}
}

func TestConnectionsThroughATunnel(t *testing.T) {
	clk := newClock()
	s := testSpec()
	tr := newTracker(s, clk.Now)
	via := func(tag string) context.Context {
		return session.ContextWithInbound(context.Background(), &session.Inbound{Tag: tag, Name: "vless-reverse"})
	}
	internet := xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443)
	flows := func(i int) int {
		_, _, n := tr.nodes[i].snapshot()
		return n
	}

	c1, _ := tcpPair(t)
	f1 := tr.wrap(via("rvs-from-n1"), nil, internet, c1)
	if _, ok := f1.(*flowConn); !ok || flows(0) != 1 || flows(1) != 0 {
		t.Fatalf("one flow on node 1: %T, counts %d/%d", f1, flows(0), flows(1))
	}
	c2, _ := tcpPair(t)
	f2 := tr.wrap(via("rvs-from-n1"), nil, internet, c2)
	c3, _ := tcpPair(t)
	f3 := tr.wrap(via("rvs-from-n2"), nil, internet, c3)
	if flows(0) != 2 || flows(1) != 1 {
		t.Fatalf("counts %d/%d, want 2/1", flows(0), flows(1))
	}

	f1.Close()
	f1.Close() // never counts down twice
	if flows(0) != 1 {
		t.Fatalf("node 1 has %d after one close", flows(0))
	}
	f2.Close()
	f3.Close()
	if flows(0) != 0 || flows(1) != 0 {
		t.Fatalf("counts %d/%d after closing all", flows(0), flows(1))
	}
}

// Anything that isn't a node's TCP traffic comes back as the very connection
// the core made: no wrapper, nothing counted.
func TestOtherSocketsAreLeftAlone(t *testing.T) {
	clk := newClock()
	s := testSpec()
	tr := newTracker(s, clk.Now)
	src, portal := portalOf(s, 0)
	internet := xnet.TCPDestination(xnet.ParseAddress("93.184.216.34"), 443)
	reverse := session.ContextWithInbound(context.Background(), &session.Inbound{Tag: "rvs-from-n1"})
	other := session.ContextWithInbound(context.Background(), &session.Inbound{Tag: "somebody-else"})

	tests := []struct {
		name string
		ctx  context.Context
		src  xnet.Address
		dest xnet.Destination
	}{
		{"flow with no inbound", context.Background(), nil, internet},
		{"flow on another inbound", other, nil, internet},
		{"the portal, from another address", context.Background(), xnet.ParseAddress("192.168.50.99"), portal},
		{"the portal, from no address", context.Background(), nil, portal},
		{"another port of the portal", context.Background(), src, xnet.TCPDestination(portal.Address, portal.Port+1)},
		{"a domain", context.Background(), src, xnet.TCPDestination(xnet.ParseAddress("portal.example"), portal.Port)},
		{"UDP to the portal's address", reverse, src, xnet.UDPDestination(portal.Address, portal.Port)},
		{"UDP on a node's tag", reverse, nil, xnet.UDPDestination(xnet.ParseAddress("1.1.1.1"), 53)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := tcpPair(t)
			if got := tr.wrap(tt.ctx, tt.src, tt.dest, c); got != net.Conn(c) {
				t.Fatalf("came back as %T", got)
			}
			tr.refresh()
			for i := range tr.nodes {
				if up, _, n := tr.nodes[i].snapshot(); up || n != 0 {
					t.Fatalf("node %d: up=%v flows=%d", i, up, n)
				}
			}
		})
	}

	// A connection that isn't TCP at all is also passed on as it is.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if got := tr.wrap(reverse, nil, internet, a); got != net.Conn(a) {
		t.Fatalf("a pipe came back as %T", got)
	}
}

// The wrapper sits in the core's system dialer: dial a real listener through
// it and the connection arrives as the tracker's.
func TestSystemDialerWraps(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello"))
			defer c.Close()
		}
	}()
	port := uint16(l.Addr().(*net.TCPAddr).Port)

	s := testSpec()
	s.DialAddr, s.Exit.Addr = "127.0.0.1", "127.0.0.1"
	s.Nodes = s.Nodes[:1]
	s.Nodes[0].Addr, s.Nodes[0].Port = "127.0.0.1", port
	s.Exit.Blocked = []string{"10.0.0.0/8"}
	s.DNS.Servers = []string{"1.1.1.1"}
	tr := newTracker(s, time.Now)
	useTracker(tr)
	t.Cleanup(func() { useTracker(nil) })

	dest := xnet.TCPDestination(xnet.ParseAddress("127.0.0.1"), xnet.Port(port))
	conn, err := dialer.Dial(context.Background(), xnet.ParseAddress("127.0.0.1"), dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	link, ok := conn.(*linkConn)
	if !ok {
		t.Fatalf("a tunnel dial came back as %T", conn)
	}
	buf := make([]byte, 5)
	if _, err := link.Read(buf); err != nil || string(buf) != "hello" {
		t.Fatalf("read %q, %v", buf, err)
	}
	link.Close()

	// With no tracker the same dial is a plain connection.
	useTracker(nil)
	conn, err = dialer.Dial(context.Background(), xnet.ParseAddress("127.0.0.1"), dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, ok := conn.(*net.TCPConn); !ok {
		t.Fatalf("with no tracker the dial came back as %T", conn)
	}

	// A failed dial is just an error.
	l.Close()
	if _, err := dialer.Dial(context.Background(), xnet.ParseAddress("127.0.0.1"), dest, nil); err == nil {
		t.Fatal("dialed a closed port")
	}
}
