// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"net"
	"net/netip"
	"runtime"
	"strconv"
	"testing"
	"time"
)

var loopback = netip.MustParseAddr("127.0.0.1")

// startForwarder runs a forwarder to target that dials from `from`.
func startForwarder(t *testing.T, target string, from netip.Addr) *targetForwarder {
	t.Helper()
	f, err := newTargetForwarder(target, from)
	if err != nil {
		t.Fatal(err)
	}
	go f.serve()
	t.Cleanup(f.close)
	return f
}

func dialForwarder(t *testing.T, f *targetForwarder) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", f.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// endsSoon reports whether c is closed from the other side within d.
func endsSoon(c net.Conn, d time.Duration) bool {
	c.SetReadDeadline(time.Now().Add(d))
	_, err := c.Read(make([]byte, 1))
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return false
	}
	return err != nil
}

func TestForwarderListensOnLoopbackOnly(t *testing.T) {
	f := startForwarder(t, "127.0.0.1:9", loopback)
	ap, err := netip.ParseAddrPort(f.addr())
	if err != nil {
		t.Fatal(err)
	}
	if !ap.Addr().IsLoopback() || ap.Port() == 0 {
		t.Fatalf("listens on %s", ap)
	}
}

func TestForwarderCarriesBothWays(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	f := startForwarder(t, echo.addr(), loopback)
	c := dialForwarder(t, f)
	payload := randomBytes(t, 256<<10)
	if got := roundTrip(t, c, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the forwarder is not what was sent")
	}
}

// A name is looked up, not just an address dialed.
func TestForwarderReachesATargetByName(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	f := startForwarder(t, net.JoinHostPort("localhost", strconv.Itoa(echo.port())), loopback)
	c := dialForwarder(t, f)
	if got := roundTrip(t, c, []byte("by name")); string(got) != "by name" {
		t.Fatalf("got %q", got)
	}
}

// One side finishing its half must reach the other, and the answer must
// still come back: a TLS handshake ends its sending before its reading.
func TestForwarderPassesAHalfClose(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		all, _ := io.ReadAll(c) // returns only once the client's half is closed
		c.Write([]byte("got " + strconv.Itoa(len(all))))
	}()

	f := startForwarder(t, l.Addr().String(), loopback)
	c := dialForwarder(t, f)
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	c.(*net.TCPConn).CloseWrite()
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	reply, err := io.ReadAll(c)
	if err != nil || string(reply) != "got 5" {
		t.Fatalf("reply %q, %v", reply, err)
	}
}

// The reason it exists: the target sees the entry's address, not whichever
// one the system would pick. Linux lets a test bind a second loopback address.
func TestForwarderDialsFromItsAddress(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs a second loopback address to send from; only Linux has all of 127.0.0.0/8")
	}
	const from = "127.0.0.18"
	if l, err := net.Listen("tcp", net.JoinHostPort(from, "0")); err != nil {
		t.Skipf("cannot use %s here: %v", from, err)
	} else {
		l.Close()
	}
	echo := newEchoServer(t, "127.0.0.1")
	f := startForwarder(t, echo.addr(), netip.MustParseAddr(from))
	c := dialForwarder(t, f)
	if got := roundTrip(t, c, []byte("who am i")); string(got) != "who am i" {
		t.Fatalf("got %q", got)
	}
	peers := echo.accepted()
	if len(peers) != 1 {
		t.Fatalf("the target saw %v", peers)
	}
	if host, _, _ := net.SplitHostPort(peers[0]); host != from {
		t.Fatalf("the target was reached from %s, want %s", peers[0], from)
	}
}

// A target that isn't there ends the connection at once: REALITY must not be
// left holding a visitor.
func TestForwarderTargetDown(t *testing.T) {
	f := startForwarder(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t))), loopback)
	c := dialForwarder(t, f)
	if !endsSoon(c, 5*time.Second) {
		t.Fatal("the connection stayed open though the target refused")
	}
}

// When the target hangs up, so does the forwarder, and the other way round.
func TestForwarderEndsWithEitherSide(t *testing.T) {
	accepted := make(chan net.Conn, 2)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	f := startForwarder(t, l.Addr().String(), loopback)

	// the target goes first
	c := dialForwarder(t, f)
	far := <-accepted
	far.(*net.TCPConn).SetLinger(0) // a reset, not a tidy close
	far.Close()
	if !endsSoon(c, 5*time.Second) {
		t.Fatal("the visitor's side stayed open after the target reset")
	}

	// the visitor goes first
	c = dialForwarder(t, f)
	far = <-accepted
	defer far.Close()
	c.Close()
	if !endsSoon(far, 5*time.Second) {
		t.Fatal("the target's side stayed open after the visitor left")
	}
}

// Over its limit it turns a connection away rather than queue it, and the
// ones it has keep working.
func TestForwarderTurnsAwayOverItsLimit(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	f, err := newTargetForwarder(echo.addr(), loopback)
	if err != nil {
		t.Fatal(err)
	}
	f.slots = make(chan struct{}, 1)
	go f.serve()
	t.Cleanup(f.close)

	first := dialForwarder(t, f)
	if got := roundTrip(t, first, []byte("one")); string(got) != "one" {
		t.Fatalf("got %q", got)
	}
	second := dialForwarder(t, f)
	if !endsSoon(second, 5*time.Second) {
		t.Fatal("a connection over the limit was kept")
	}
	if got := roundTrip(t, first, []byte("still")); string(got) != "still" {
		t.Fatalf("the first connection: got %q", got)
	}
	// its place is free again once it ends
	first.Close()
	eventually(t, 5*time.Second, "the place to free up", func() bool { return len(f.slots) == 0 })
	third := dialForwarder(t, f)
	if got := roundTrip(t, third, []byte("three")); string(got) != "three" {
		t.Fatalf("got %q", got)
	}
}

// close ends what is open, stops listening, and comes back; twice is fine.
func TestForwarderClose(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	f, err := newTargetForwarder(echo.addr(), loopback)
	if err != nil {
		t.Fatal(err)
	}
	go f.serve()
	c := dialForwarder(t, f)
	if got := roundTrip(t, c, []byte("open")); string(got) != "open" {
		t.Fatalf("got %q", got)
	}

	closed := make(chan struct{})
	go func() {
		f.close()
		f.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("close did not return with a connection open")
	}
	if !endsSoon(c, 5*time.Second) {
		t.Fatal("the open connection outlived close")
	}
	if c2, err := net.DialTimeout("tcp", f.addr(), 2*time.Second); err == nil {
		c2.Close()
		t.Fatal("still listening after close")
	}
}
