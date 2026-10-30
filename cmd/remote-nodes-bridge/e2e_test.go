// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

const internetName = "internet.test"

func answers() map[string]netip.Addr {
	return map[string]netip.Addr{internetName + ".": netip.MustParseAddr("127.0.0.1")}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// A subscription's traffic goes client, portal, tunnel, bridge, exit and
// comes back, by address and by name, and the status page sees it.
func TestE2ETunnelCarriesTraffic(t *testing.T) {
	w := newWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	dns := newDNSServer(t, "127.0.0.1", answers())

	spec := w.spec(dns.addr())
	// A second node whose portal isn't there: the status has to tell them apart.
	spec.Nodes = append(spec.Nodes, w.node("n2", "127.0.0.1", freePort(t)))
	b := startBridge(t, spec)
	started := b.status().Started

	if n := b.node(t, "n1"); n.Up || n.BytesUp != 0 || n.BytesDown != 0 || n.Connections != 0 {
		t.Fatalf("before anything ran: %+v", n)
	}
	b.waitUp(t, "n1")
	n1 := b.node(t, "n1")
	if n1.Since.Before(started) || n1.Since.After(time.Now()) {
		t.Errorf("n1 up since %v, outside the run (%v to now)", n1.Since, started)
	}
	if n2 := b.node(t, "n2"); n2.Up || n2.Since.After(n1.Since) {
		t.Errorf("n2, whose portal doesn't exist: %+v", n2)
	}
	if st := b.status(); !st.Running || st.Xray == "" {
		t.Errorf("status %+v", st)
	}

	// By address: a megabyte each way, every byte as it was sent.
	payload := randomBytes(t, 1<<20)
	conn, err := w.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the tunnel is not what was sent")
	}
	if peers := echo.accepted(); len(peers) != 1 {
		t.Fatalf("the internet saw %v", peers)
	}
	eventually(t, 5*time.Second, "the open connection to be counted", func() bool {
		return b.node(t, "n1").Connections == 1
	})
	n1 = b.node(t, "n1")
	if n1.BytesUp < uint64(len(payload)) || n1.BytesDown < uint64(len(payload)) {
		t.Errorf("a megabyte each way counted as %d up, %d down", n1.BytesUp, n1.BytesDown)
	}
	if n2 := b.node(t, "n2"); n2.BytesUp != 0 || n2.BytesDown != 0 || n2.Connections != 0 {
		t.Errorf("n2 carried nothing but shows %+v", n2)
	}
	conn.Close()
	eventually(t, 10*time.Second, "the closed connection to stop counting", func() bool {
		return b.node(t, "n1").Connections == 0
	})

	// By name: only the test's resolver knows the name, so the lookup went
	// through Xray's resolver and the tagged DNS, not the system's.
	conn, err = w.viaSubscription(net.JoinHostPort(internetName, strconv.Itoa(echo.port())))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, []byte("by name")); string(got) != "by name" {
		t.Fatalf("got %q", got)
	}
	var asked []string
	for _, q := range dns.asked() {
		asked = append(asked, fmt.Sprintf("%s %v", q.name, q.typ))
		if q.typ != dnsmessage.TypeA {
			t.Errorf("the exit asked for %v: an IPv4 exit has no use for it", q.typ)
		}
	}
	if len(asked) == 0 || asked[0] != internetName+". TypeA" {
		t.Errorf("the resolver was asked %v", asked)
	}
}

// The flow the product runs. xray-core's Vision code races between its
// reader and its writer, so the detector rejects it in this process; CI runs
// this test without -race, as it does the panel's own reverse tunnel test.
func TestE2EVisionFlow(t *testing.T) {
	if raceDetectorOn() {
		t.Skip("upstream xray-core data race in the Vision flow (proxy/proxy.go); run without -race")
	}
	w := newWorld(t, bridgespec.VisionFlow)
	echo := newEchoServer(t, "127.0.0.1")
	b := startBridge(t, w.spec("9.9.9.9"))
	b.waitUp(t, "n1")

	payload := randomBytes(t, 1<<20)
	conn, err := w.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the tunnel is not what was sent")
	}
	if n := b.node(t, "n1"); n.BytesUp < uint64(len(payload)) || n.BytesDown < uint64(len(payload)) {
		t.Errorf("a megabyte each way counted as %d up, %d down", n.BytesUp, n.BytesDown)
	}
}

func raceDetectorOn() bool {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "-race" && s.Value == "true" {
				return true
			}
		}
	}
	return false
}

// With the shipped blocked list, which includes loopback, the same flow that
// worked above never reaches the internet. The request still enters the
// bridge, so the byte counter moves; the exit refuses it.
func TestE2EExitRefusesPrivateAddresses(t *testing.T) {
	w := newWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")

	spec := w.spec("9.9.9.9")
	spec.Exit.Blocked = nil // the default list
	b := startBridge(t, spec)
	b.waitUp(t, "n1")

	conn, err := w.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("hello, LAN"))
	eventually(t, 10*time.Second, "the request to reach the bridge", func() bool {
		return b.node(t, "n1").BytesUp > 0
	})

	conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	if n, err := conn.Read(make([]byte, 64)); n != 0 {
		t.Fatalf("got %d bytes back from a blocked address (%v)", n, err)
	}
	if peers := echo.accepted(); len(peers) != 0 {
		t.Fatalf("the exit connected to a blocked address: %v", peers)
	}
	if n := b.node(t, "n1"); n.Connections != 0 {
		t.Errorf("a refused flow counts as %d open connections", n.Connections)
	}
}

// The link between bridge and portal breaks and comes back: the tunnel reads
// down, then up, each stamped when it happened, and carries traffic again
// without anyone restarting the bridge.
func TestE2ETunnelRecovers(t *testing.T) {
	w := newWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	link := newTCPProxy(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(w.portalPort)))

	spec := w.spec("9.9.9.9")
	spec.Nodes[0].Port = uint16(link.port())
	b := startBridge(t, spec)
	b.waitUp(t, "n1")
	up1 := b.node(t, "n1").Since

	link.cut()
	eventually(t, 15*time.Second, "the tunnel to read as down", func() bool { return !b.node(t, "n1").Up })
	down := b.node(t, "n1").Since
	if !down.After(up1) {
		t.Errorf("down since %v, not after it came up at %v", down, up1)
	}

	link.restore()
	b.waitUp(t, "n1")
	if up2 := b.node(t, "n1").Since; !up2.After(down) {
		t.Errorf("up again since %v, not after it went down at %v", up2, down)
	}
	conn, err := w.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, []byte("again")); string(got) != "again" {
		t.Fatalf("got %q", got)
	}
}

// The point of the two source addresses: the tunnel dials from one and the
// exit, and its lookups, leave from the other. Linux lets a test bind them
// both on loopback; macOS has only 127.0.0.1.
func TestE2ESourceAddresses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs two loopback addresses to send from; only Linux has all of 127.0.0.0/8")
	}
	const dial, exit = "127.0.0.18", "127.0.0.17"
	for _, ip := range []string{dial, exit} {
		l, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
		if err != nil {
			t.Skipf("cannot use %s here: %v", ip, err)
		}
		l.Close()
	}

	w := newWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	dns := newDNSServer(t, "127.0.0.1", answers())
	portal := newTCPProxy(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(w.portalPort)))

	spec := w.spec(dns.addr())
	spec.DialAddr, spec.Exit.Addr = dial, exit
	spec.Nodes[0].Port = uint16(portal.port())
	b := startBridge(t, spec)
	b.waitUp(t, "n1")

	for _, target := range []string{echo.addr(), net.JoinHostPort(internetName, strconv.Itoa(echo.port()))} {
		conn, err := w.viaSubscription(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := roundTrip(t, conn, []byte("who am i")); string(got) != "who am i" {
			t.Fatalf("got %q", got)
		}
		conn.Close()
	}

	host := func(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }
	if peers := portal.accepted(); len(peers) == 0 {
		t.Error("the portal saw no connection from the bridge")
	} else {
		for _, p := range peers {
			if host(p) != dial {
				t.Errorf("the tunnel came from %s, want %s", p, dial)
			}
		}
	}
	if peers := echo.accepted(); len(peers) != 2 {
		t.Errorf("the internet saw %v", peers)
	} else {
		for _, p := range peers {
			if host(p) != exit {
				t.Errorf("traffic left from %s, want %s", p, exit)
			}
		}
	}
	queries := dns.asked()
	if len(queries) == 0 {
		t.Error("the resolver was never asked")
	}
	for _, q := range queries {
		if host(q.from) != exit {
			t.Errorf("a lookup left from %s, want %s", q.from, exit)
		}
	}
}

// tcpProxy stands between the bridge and a portal. It remembers who
// connected, and it can be cut: every connection dropped and the port closed,
// then opened again on the same address.
type tcpProxy struct {
	t      *testing.T
	target string
	addr   string

	mu    sync.Mutex
	l     net.Listener
	conns map[net.Conn]struct{}
	peers []string
}

func newTCPProxy(t *testing.T, target string) *tcpProxy {
	t.Helper()
	p := &tcpProxy{t: t, target: target, addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t))), conns: map[net.Conn]struct{}{}}
	p.restore()
	t.Cleanup(p.cut)
	return p
}

func (p *tcpProxy) port() int {
	_, port, _ := net.SplitHostPort(p.addr)
	n, _ := strconv.Atoi(port)
	return n
}

// restore listens again.
func (p *tcpProxy) restore() {
	l, err := net.Listen("tcp", p.addr)
	if err != nil {
		p.t.Fatal(err)
	}
	p.mu.Lock()
	p.l = l
	p.mu.Unlock()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.peers = append(p.peers, c.RemoteAddr().String())
			p.conns[c] = struct{}{}
			p.mu.Unlock()
			go p.forward(c)
		}
	}()
}

func (p *tcpProxy) forward(c net.Conn) {
	defer c.Close()
	up, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.conns[up] = struct{}{}
	p.mu.Unlock()
	defer up.Close()
	go func() {
		io.Copy(up, c)
		up.Close()
	}()
	io.Copy(c, up)
}

// cut closes the port and every connection through it.
func (p *tcpProxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.l != nil {
		p.l.Close()
		p.l = nil
	}
	for c := range p.conns {
		c.Close()
	}
	p.conns = map[net.Conn]struct{}{}
}

func (p *tcpProxy) accepted() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.peers...)
}
