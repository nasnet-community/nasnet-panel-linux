// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	// targetDialTimeout is how long the Reality target gets to answer a
	// connect, name lookup included.
	targetDialTimeout = 10 * time.Second
	// targetIdle ends a forwarded connection neither side has used for this
	// long. REALITY borrows the target for a handshake, or hands it a visitor
	// who is not a client; neither sits silent for minutes.
	targetIdle = 5 * time.Minute
	// maxTargetConns bounds the forwarded connections open at once. Every new
	// client connection borrows one for the length of its handshake.
	maxTargetConns = 256
)

// targetForwarder stands between REALITY and the Reality target.
//
// REALITY reaches its target with a plain dialer, from whichever address the
// system picks. On the router that is the veth's first address, which leaves
// by another link than the one the entry's clients arrive on. So the inbound's
// target is this listener on loopback, and each connection it takes is carried
// on to the real target from the entry's own address, name lookup included.
type targetForwarder struct {
	l      net.Listener
	target string // host:port
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	slots  chan struct{}

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// newTargetForwarder listens on a loopback port of its own choosing. Nothing
// is accepted until serve.
func newTargetForwarder(target string, from netip.Addr) (*targetForwarder, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &targetForwarder{
		l:      l,
		target: target,
		dial:   dialFrom(from),
		slots:  make(chan struct{}, maxTargetConns),
		conns:  map[net.Conn]struct{}{},
	}, nil
}

// dialFrom connects from one source address, and looks names up from it too:
// a lookup that left by another link could come back with an address meant for
// that link.
func dialFrom(from netip.Addr) func(ctx context.Context, network, addr string) (net.Conn, error) {
	ip := net.IP(from.AsSlice())
	bound := func(network string) net.Addr {
		if len(network) >= 3 && network[:3] == "udp" {
			return &net.UDPAddr{IP: ip}
		}
		return &net.TCPAddr{IP: ip}
	}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{LocalAddr: bound(network)}
			return d.DialContext(ctx, network, addr)
		},
	}
	// tcp4 or tcp6, so a name resolves to an address the source can reach.
	family := "tcp4"
	if from.Is6() {
		family = "tcp6"
	}
	return func(ctx context.Context, _, addr string) (net.Conn, error) {
		d := net.Dialer{LocalAddr: bound("tcp"), Resolver: resolver}
		return d.DialContext(ctx, family, addr)
	}
}

// addr is what the inbound's REALITY settings name as their target.
func (f *targetForwarder) addr() string { return f.l.Addr().String() }

// serve takes connections until close.
func (f *targetForwarder) serve() {
	for {
		c, err := f.l.Accept()
		if err != nil {
			return
		}
		select {
		case f.slots <- struct{}{}:
		default:
			c.Close() // full: REALITY sees a target that hung up
			continue
		}
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			<-f.slots
			c.Close()
			return
		}
		f.wg.Add(1)
		f.mu.Unlock()
		go func() {
			defer f.wg.Done()
			defer func() { <-f.slots }()
			f.forward(c)
		}()
	}
}

func (f *targetForwarder) forward(in net.Conn) {
	defer in.Close()
	if !f.hold(in) {
		return
	}
	defer f.release(in)

	ctx, cancel := context.WithTimeout(context.Background(), targetDialTimeout)
	out, err := f.dial(ctx, "tcp", f.target)
	cancel()
	if err != nil {
		return
	}
	defer out.Close()
	if !f.hold(out) {
		return
	}
	defer f.release(out)

	ended := make(chan bool, 2)
	pipe := func(dst, src net.Conn) {
		clean := copyIdle(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok && clean {
			tc.CloseWrite() // the other direction may still have something to deliver
		}
		ended <- clean
	}
	go pipe(out, in)
	go pipe(in, out)
	if !<-ended {
		// a reset or a timeout: nothing more will come the other way either
		in.Close()
		out.Close()
	}
	<-ended
}

// copyIdle copies until src ends, and says whether it ended by closing: a
// read that fails, a write that fails and targetIdle of silence do not count.
func copyIdle(dst, src net.Conn) (clean bool) {
	buf := make([]byte, 16<<10)
	for {
		src.SetReadDeadline(time.Now().Add(targetIdle))
		n, err := src.Read(buf)
		if n > 0 {
			dst.SetWriteDeadline(time.Now().Add(targetIdle))
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return false
			}
		}
		if err != nil {
			return err == io.EOF
		}
	}
}

// hold remembers c so close can end it. It reports false once the forwarder
// is closed; the caller closes c.
func (f *targetForwarder) hold(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.conns[c] = struct{}{}
	return true
}

func (f *targetForwarder) release(c net.Conn) {
	f.mu.Lock()
	delete(f.conns, c)
	f.mu.Unlock()
}

// close stops listening and ends every forwarded connection. It returns once
// they are gone.
func (f *targetForwarder) close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	for c := range f.conns {
		c.Close()
	}
	f.mu.Unlock()
	f.l.Close()
	f.wg.Wait()
}
