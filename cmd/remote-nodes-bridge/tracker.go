// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/transport/internet"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// How a tunnel is judged. Its connection to the portal must have lasted
// upAfter, so a handshake that is refused a moment later doesn't flicker as
// "up", and must have delivered something within staleAfter. The portal sends
// a keepalive every ten seconds or so, so a quiet tunnel still passes.
//
// A connection silent for deadAfter is closed. The core waits on a connection
// until it fails, and one that died without a reset (the machine slept, a NAT
// forgot it) never fails, so without this the tunnel stays down until a
// restart. Closing it makes the core dial the portal again. It is well past
// staleAfter, so a stall that clears in time still comes back on its own.
const (
	upAfter    = 3 * time.Second
	staleAfter = 40 * time.Second
	deadAfter  = 90 * time.Second
)

// tracker tells what the Xray core keeps to itself: whether each tunnel is
// connected and how many connections run through it. It watches the sockets
// the bridge opens, at the system dialer where all of them pass.
//
// A socket to a portal, from the dial address, is a tunnel's connection. A
// TCP socket opened for a flow that came in on a node's reverse tag is one of
// that node's connections. Everything else goes through untouched, and so do
// UDP sockets: the core's packet code needs the dialer's own type.
type tracker struct {
	now  func() time.Time
	dial netip.Addr

	// Built once and never changed, so the dial path reads them lock-free.
	nodes    []*tunnel
	byPortal map[netip.AddrPort]*tunnel
	byTag    map[string]*tunnel // by reverse tag
}

// tunnel is one node's state.
type tunnel struct {
	flows atomic.Int64 // TCP connections open through the tunnel

	mu    sync.Mutex
	links map[*linkConn]struct{} // connections to the portal
	up    bool
	since time.Time
}

func newTracker(s *bridgespec.Spec, now func() time.Time) *tracker {
	t := &tracker{
		now:      now,
		dial:     netip.MustParseAddr(s.DialAddr).Unmap(),
		byPortal: make(map[netip.AddrPort]*tunnel, len(s.Nodes)),
		byTag:    make(map[string]*tunnel, len(s.Nodes)),
	}
	for _, n := range s.Nodes {
		tn := &tunnel{links: map[*linkConn]struct{}{}, since: now()}
		t.nodes = append(t.nodes, tn)
		t.byPortal[netip.AddrPortFrom(netip.MustParseAddr(n.Addr).Unmap(), n.Port)] = tn
		t.byTag[n.ReverseTag] = tn
	}
	return t
}

// refresh works out which tunnels are up now and stamps the ones that changed.
// The bridge calls it every second and before it answers a status request.
func (t *tracker) refresh() {
	now := t.now()
	for _, tn := range t.nodes {
		tn.refresh(now)
	}
}

func (tn *tunnel) refresh(now time.Time) {
	var dead []*linkConn
	tn.mu.Lock()
	up := false
	for l := range tn.links {
		heard := l.lastRead.Load()
		last := l.opened
		if heard != 0 {
			last = time.Unix(0, heard)
		}
		switch {
		case now.Sub(last) > deadAfter:
			dead = append(dead, l)
		case heard != 0 && now.Sub(l.opened) >= upAfter && now.Sub(last) <= staleAfter:
			up = true
		}
	}
	if up != tn.up {
		tn.up, tn.since = up, now
	}
	tn.mu.Unlock()
	// outside the lock: Close takes it to drop the link
	for _, l := range dead {
		l.Close()
	}
}

// snapshot reads one tunnel's state as of the last refresh.
func (tn *tunnel) snapshot() (up bool, since time.Time, flows int) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	return tn.up, tn.since, int(tn.flows.Load())
}

// wrap hands back conn as the core should see it: counted if it belongs to a
// tunnel, as it came otherwise.
func (t *tracker) wrap(ctx context.Context, src xnet.Address, dest xnet.Destination, conn net.Conn) net.Conn {
	tc, ok := conn.(*net.TCPConn)
	if !ok || dest.Network != xnet.Network_TCP {
		return conn
	}
	if tn := t.tunnelOf(src, dest); tn != nil {
		return tn.newLink(t, tc)
	}
	if in := session.InboundFromContext(ctx); in != nil {
		if tn := t.byTag[in.Tag]; tn != nil {
			tn.flows.Add(1)
			return &flowConn{TCPConn: tc, tn: tn}
		}
	}
	return conn
}

// tunnelOf finds the node whose portal dest is, if src is the dial address.
func (t *tracker) tunnelOf(src xnet.Address, dest xnet.Destination) *tunnel {
	if src == nil || !src.Family().IsIP() || !dest.Address.Family().IsIP() {
		return nil
	}
	from, ok := netip.AddrFromSlice(src.IP())
	if !ok || from.Unmap() != t.dial {
		return nil
	}
	to, ok := netip.AddrFromSlice(dest.Address.IP())
	if !ok {
		return nil
	}
	return t.byPortal[netip.AddrPortFrom(to.Unmap(), uint16(dest.Port))]
}

func (tn *tunnel) newLink(t *tracker, tc *net.TCPConn) *linkConn {
	l := &linkConn{TCPConn: tc, now: t.now, tn: tn, opened: t.now()}
	tn.mu.Lock()
	tn.links[l] = struct{}{}
	tn.mu.Unlock()
	return l
}

// linkConn is a connection to a portal. It notes when something last arrived.
type linkConn struct {
	*net.TCPConn
	now      func() time.Time
	tn       *tunnel
	opened   time.Time
	lastRead atomic.Int64 // unix nanoseconds; 0 until the first byte
	once     sync.Once
}

func (l *linkConn) Read(p []byte) (int, error) {
	n, err := l.TCPConn.Read(p)
	if n > 0 {
		l.lastRead.Store(l.now().UnixNano())
	}
	return n, err
}

func (l *linkConn) Close() error {
	l.once.Do(func() {
		l.tn.mu.Lock()
		delete(l.tn.links, l)
		l.tn.mu.Unlock()
	})
	return l.TCPConn.Close()
}

// flowConn is a connection a node's subscriptions opened. Embedding the TCP
// connection keeps the rest of its methods for the core.
type flowConn struct {
	*net.TCPConn
	tn   *tunnel
	once sync.Once
}

func (f *flowConn) Close() error {
	f.once.Do(func() { f.tn.flows.Add(-1) })
	return f.TCPConn.Close()
}

// systemDialer is the process's system dialer. Xray reads it without a lock,
// so it is installed once, before any instance runs, and the tracker behind
// it is swapped.
type systemDialer struct {
	base internet.DefaultSystemDialer
	cur  atomic.Pointer[tracker]
}

var (
	dialer      systemDialer
	installOnce sync.Once
)

// useTracker routes the bridge's sockets through t; nil puts them back as the
// core makes them.
func useTracker(t *tracker) {
	installOnce.Do(func() { internet.UseAlternativeSystemDialer(&dialer) })
	dialer.cur.Store(t)
}

func (d *systemDialer) Dial(ctx context.Context, src xnet.Address, dest xnet.Destination, so *internet.SocketConfig) (net.Conn, error) {
	conn, err := d.base.Dial(ctx, src, dest, so)
	if err != nil {
		return nil, err
	}
	if t := d.cur.Load(); t != nil {
		conn = t.wrap(ctx, src, dest, conn)
	}
	return conn, nil
}

func (d *systemDialer) DestIpAddress() net.IP { return d.base.DestIpAddress() }
