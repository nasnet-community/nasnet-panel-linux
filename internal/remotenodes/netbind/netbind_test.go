// SPDX-License-Identifier: AGPL-3.0-or-later

package netbind

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"
)

func TestADialerBoundToAnAddress(t *testing.T) {
	d := Dialer(netip.MustParseAddr("192.168.50.16"))
	la, ok := d.LocalAddr.(*net.TCPAddr)
	if !ok || la.IP.String() != "192.168.50.16" || la.Port != 0 {
		t.Fatalf("local address %v", d.LocalAddr)
	}
	if d.Resolver == nil || !d.Resolver.PreferGo || d.Resolver.Dial == nil {
		t.Fatal("DNS lookups are not bound to the address")
	}
	if d.Timeout <= 0 {
		t.Error("no dial timeout")
	}
	// an IPv4 address written as IPv6 is still IPv4 to the dialer
	if la := Dialer(netip.MustParseAddr("::ffff:192.168.50.16")).LocalAddr.(*net.TCPAddr); len(la.IP.To4()) != 4 {
		t.Errorf("a mapped address stayed IPv6: %v", la.IP)
	}
	// no address, or one that binds to nothing in particular, leaves the choice to the system
	for _, none := range []netip.Addr{{}, netip.IPv4Unspecified(), netip.IPv6Unspecified()} {
		if d := Dialer(none); d.LocalAddr != nil || d.Resolver != nil {
			t.Errorf("%v: bound to %v", none, d.LocalAddr)
		}
	}
}

// DNS goes out from the same address. The resolver's own dial is what the
// lookups use, so it is held to the address for UDP and for TCP.
func TestDNSLookupsAreBoundToo(t *testing.T) {
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()

	local := netip.MustParseAddr("127.0.0.1")
	dial := Dialer(local).Resolver.Dial

	conn, err := dial(context.Background(), "udp", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if ip := conn.LocalAddr().(*net.UDPAddr).IP.String(); ip != "127.0.0.1" {
		t.Errorf("udp lookup leaves from %s", ip)
	}
	conn2, err := dial(context.Background(), "tcp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if ip := conn2.LocalAddr().(*net.TCPAddr).IP.String(); ip != "127.0.0.1" {
		t.Errorf("tcp lookup leaves from %s", ip)
	}

	// and from an address that isn't there, neither works, which shows the binding
	nowhere := Dialer(netip.MustParseAddr("192.0.2.1")).Resolver.Dial
	for _, c := range []struct{ network, addr string }{{"udp", udp.LocalAddr().String()}, {"tcp", tcp.Addr().String()}} {
		if conn, err := nowhere(context.Background(), c.network, c.addr); !errors.Is(err, syscall.EADDRNOTAVAIL) {
			if conn != nil {
				conn.Close()
			}
			t.Errorf("%s lookup from an address that isn't ours: %v", c.network, err)
		}
	}
}

// Like the HTTP one: from 127.0.0.2 to a server on 127.0.0.1, where the system
// would have used 127.0.0.1, so the source that arrives is the client's choice.
func TestADNSLookupLeavesFromAnotherAddressWhenToldTo(t *testing.T) {
	probe, err := net.ListenPacket("udp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this system has no 127.0.0.2 on its loopback: %v", err)
	}
	probe.Close()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	conn, err := Dialer(netip.MustParseAddr("127.0.0.2")).Resolver.Dial(context.Background(), "udp", server.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("q"))
	buf := make([]byte, 8)
	server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, from, err := server.ReadFrom(buf)
	if err != nil || from.(*net.UDPAddr).IP.String() != "127.0.0.2" {
		t.Fatalf("the lookup came from %v, %v", from, err)
	}
}

// A connection made with the dialer leaves from the address it was given, and
// one that can't be made from it fails instead of going out some other way.
func TestADialedConnectionLeavesFromTheAddress(t *testing.T) {
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
			c.Close()
		}
	}()

	conn, err := Dialer(netip.MustParseAddr("127.0.0.1")).DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if ip := conn.LocalAddr().(*net.TCPAddr).IP.String(); ip != "127.0.0.1" {
		t.Errorf("the connection leaves from %s", ip)
	}
	conn.Close()

	_, err = Dialer(netip.MustParseAddr("192.0.2.1")).DialContext(context.Background(), "tcp", l.Addr().String()) // TEST-NET-1, nobody's
	if !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Fatalf("a connection from an address that isn't ours: %v", err)
	}
}

// Linux owns all of 127/8, so another address there shows that the choice is
// the dialer's. Elsewhere the check can't be made.
func TestADialerCanLeaveFromAnotherLoopbackAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this system has no 127.0.0.2 on its loopback, so a second local address can't be tried: %v", err)
	}
	probe.Close()
	seen := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		seen <- c.RemoteAddr().(*net.TCPAddr).IP.String()
		c.Close()
	}()
	conn, err := Dialer(netip.MustParseAddr("127.0.0.2")).DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case got := <-seen:
		if got != "127.0.0.2" {
			t.Fatalf("the server saw %s, want 127.0.0.2", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the server saw nothing")
	}
}
