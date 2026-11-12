// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The router's own entry, end to end: the built bridge with an entry in its
// spec, a subscription's REALITY client pointed straight at it, a Reality
// target, and the loopback internet of the tunnel tests.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// entryWorld is what stands around the router's entry in a test: its keys, a
// Reality target, and one subscription that is switched on for it.
type entryWorld struct {
	t *testing.T

	privateKey, publicKey string
	shortID               string
	subUUID               string
	flow                  string

	listen    string // the entry's address
	entryPort int
	target    *httptest.Server // the Reality target
}

func newEntryWorld(t *testing.T, flow string) *entryWorld {
	t.Helper()
	w := &entryWorld{
		t:         t,
		flow:      flow,
		shortID:   "0123456789abcdef",
		subUUID:   newUUID(),
		listen:    "127.0.0.1",
		entryPort: freePort(t),
	}
	w.privateKey, w.publicKey = realityKeys(t)

	// A TLS 1.3 server on X25519, which is what REALITY needs of its target.
	w.target = httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		io.WriteString(rw, "the target's own page")
	}))
	w.target.TLS = &tls.Config{MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	w.target.StartTLS()
	t.Cleanup(w.target.Close)
	return w
}

// local is the entry as the hub would put it in a spec, with the one
// subscription on it.
func (w *entryWorld) local() *bridgespec.Local {
	return &bridgespec.Local{
		ListenAddr:  w.listen,
		Port:        uint16(w.entryPort),
		PrivateKey:  w.privateKey,
		ShortIDs:    []string{w.shortID},
		ServerNames: []string{"localhost"},
		Target:      w.target.Listener.Addr().String(),
		Flow:        w.flow,
		Clients:     []bridgespec.Client{{Email: nodecfg.SubscriptionEmail(1), UUID: w.subUUID}},
		Token:       "0123456789abcdef0123456789abcdef",
	}
}

// spec is a bridge spec with the entry and no node, everything on loopback
// and the loopback internet open.
func (w *entryWorld) spec(dns string) *bridgespec.Spec {
	return &bridgespec.Spec{
		Version:    bridgespec.Version,
		Local:      w.local(),
		DialAddr:   "127.0.0.1",
		Exit:       bridgespec.Exit{Addr: "127.0.0.1", Blocked: withoutLoopback()},
		DNS:        bridgespec.DNS{Servers: []string{dns}},
		StatusAddr: "127.0.0.1:18080",
		LogLevel:   "warning",
	}
}

// client runs a REALITY client for the entry that logs in with id, and
// returns a dialer through it.
func (w *entryWorld) client(id string) func(target string) (net.Conn, error) {
	w.t.Helper()
	socksPort := freePort(w.t)
	startInstance(w.t, map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag": "socks", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks",
			"settings": map[string]any{"auth": "noauth", "udp": false},
		}},
		"outbounds": []any{map[string]any{
			"protocol": "vless",
			"settings": map[string]any{
				"address": w.listen, "port": w.entryPort, "id": id, "flow": w.flow, "encryption": "none",
			},
			"streamSettings": map[string]any{
				"network":  "tcp",
				"security": "reality",
				"realitySettings": map[string]any{
					"serverName": "localhost", "fingerprint": "chrome", "publicKey": w.publicKey, "shortId": w.shortID,
				},
			},
		}},
	})
	return func(target string) (net.Conn, error) {
		d, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), nil, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return d.Dial("tcp", target)
	}
}

// A subscription connects to the router itself and its traffic leaves through
// the exit, by address and by name, with no node and no tunnel anywhere.
func TestE2ELocalEntryCarriesTraffic(t *testing.T) {
	w := newEntryWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	dns := newDNSServer(t, "127.0.0.1", answers())
	b := startBridge(t, w.spec(dns.addr()))
	if st := b.status(); !st.Running || len(st.Nodes) != 0 {
		t.Fatalf("status %+v", st)
	}
	via := w.client(w.subUUID)

	payload := randomBytes(t, 1<<20)
	conn, err := via(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the entry is not what was sent")
	}
	conn.Close()
	if peers := echo.accepted(); len(peers) != 1 {
		t.Fatalf("the internet saw %v", peers)
	}

	// By name: only the test's resolver knows it, so the lookup went through
	// the exit's resolver, not the system's.
	conn, err = via(net.JoinHostPort(internetName, strconv.Itoa(echo.port())))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, []byte("by name")); string(got) != "by name" {
		t.Fatalf("got %q", got)
	}
	if asked := dns.asked(); len(asked) == 0 || asked[0].name != internetName+"." {
		t.Errorf("the resolver was asked %v", asked)
	}
}

// The flow the product runs; see TestE2EVisionFlow for why not under -race.
func TestE2ELocalEntryVisionFlow(t *testing.T) {
	if raceDetectorOn() {
		t.Skip("upstream xray-core data race in the Vision flow (proxy/proxy.go); run without -race")
	}
	w := newEntryWorld(t, bridgespec.VisionFlow)
	echo := newEchoServer(t, "127.0.0.1")
	startBridge(t, w.spec("9.9.9.9"))
	via := w.client(w.subUUID)

	payload := randomBytes(t, 1<<20)
	conn, err := via(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the entry is not what was sent")
	}
}

// carries reports whether a connection made by via delivers bytes to target
// and back.
func carries(via func(string) (net.Conn, error), target string) bool {
	conn, err := via(target)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		return false
	}
	got := make([]byte, 4)
	_, err = io.ReadFull(conn, got)
	return err == nil && string(got) == "ping"
}

// An id the entry doesn't know gets nothing through, and nothing of it
// reaches the internet.
func TestE2ELocalEntryRefusesAnUnknownID(t *testing.T) {
	w := newEntryWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	startBridge(t, w.spec("9.9.9.9"))

	if !carries(w.client(w.subUUID), echo.addr()) {
		t.Fatal("the subscription itself can't get through, so the refusal below would prove nothing")
	}
	before := len(echo.accepted())
	if carries(w.client(newUUID()), echo.addr()) {
		t.Fatal("a client with an id the entry was never given got through")
	}
	if after := len(echo.accepted()); after != before {
		t.Fatalf("the internet saw %d more connection(s) from the unknown client", after-before)
	}
}

// Anyone who is not a client is shown the Reality target itself: its
// certificate and its page, carried by the bridge's forwarder.
func TestE2ELocalEntryShowsOthersTheTarget(t *testing.T) {
	w := newEntryWorld(t, "")
	startBridge(t, w.spec("9.9.9.9"))

	entry := net.JoinHostPort(w.listen, strconv.Itoa(w.entryPort))
	var conn *tls.Conn
	// the status page serves a moment before the inbound listens
	eventually(t, 10*time.Second, "the entry to answer a TLS hello", func() bool {
		d := tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 5 * time.Second},
			Config:    &tls.Config{ServerName: "localhost", InsecureSkipVerify: true},
		}
		c, err := d.Dial("tcp", entry)
		if err != nil {
			return false
		}
		conn = c.(*tls.Conn)
		return true
	})
	defer conn.Close()

	want := w.target.Certificate()
	got := conn.ConnectionState().PeerCertificates
	if len(got) == 0 || !bytes.Equal(got[0].Raw, want.Raw) {
		t.Fatal("the certificate shown is not the target's own")
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	page, err := io.ReadAll(conn)
	if err != nil && len(page) == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "the target's own page") {
		t.Fatalf("got %q, want the target's page", page)
	}
}

// The entry and a tunnel in one bridge: each carries its own subscription,
// and the tunnel's numbers don't count what came in through the entry.
func TestE2ELocalEntryBesideATunnel(t *testing.T) {
	tun := newWorld(t, "")
	w := newEntryWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")

	spec := tun.spec("9.9.9.9")
	spec.Local = w.local()
	b := startBridge(t, spec)
	b.waitUp(t, "n1")
	before := b.node(t, "n1")

	payload := randomBytes(t, 256<<10)
	conn, err := w.client(w.subUUID)(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the entry is not what was sent")
	}
	conn.Close()
	if n := b.node(t, "n1"); n.BytesUp != before.BytesUp || n.BytesDown != before.BytesDown || !n.Up {
		t.Errorf("the tunnel's numbers moved with the entry's traffic: %+v, before %+v", n, before)
	}

	conn, err = tun.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the tunnel is not what was sent")
	}
	if n := b.node(t, "n1"); n.BytesUp < before.BytesUp+uint64(len(payload)) {
		t.Errorf("the tunnel carried %d bytes but counts %d up", len(payload), n.BytesUp-before.BytesUp)
	}
}

// The entry's three addresses in use: clients come in on one, their traffic
// and its lookups leave from the exit's, and the Reality target is reached
// from the entry's own. Linux lets a test bind them on loopback.
func TestE2ELocalEntrySourceAddresses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs two loopback addresses to send from; only Linux has all of 127.0.0.0/8")
	}
	const listen, exit = "127.0.0.18", "127.0.0.17"
	for _, ip := range []string{listen, exit} {
		l, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
		if err != nil {
			t.Skipf("cannot use %s here: %v", ip, err)
		}
		l.Close()
	}

	w := newEntryWorld(t, "")
	w.listen = listen
	echo := newEchoServer(t, "127.0.0.1")
	dns := newDNSServer(t, "127.0.0.1", answers())
	target := newTCPProxy(t, w.target.Listener.Addr().String())

	spec := w.spec(dns.addr())
	spec.DialAddr, spec.Exit.Addr = listen, exit
	spec.Local.Target = net.JoinHostPort("127.0.0.1", strconv.Itoa(target.port()))
	startBridge(t, spec)

	via := w.client(w.subUUID)
	for _, to := range []string{echo.addr(), net.JoinHostPort(internetName, strconv.Itoa(echo.port()))} {
		conn, err := via(to)
		if err != nil {
			t.Fatal(err)
		}
		if got := roundTrip(t, conn, []byte("who am i")); string(got) != "who am i" {
			t.Fatalf("got %q", got)
		}
		conn.Close()
	}

	host := func(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }
	if peers := target.accepted(); len(peers) == 0 {
		t.Error("the Reality target was never reached")
	} else {
		for _, p := range peers {
			if host(p) != listen {
				t.Errorf("the Reality target was reached from %s, want %s", p, listen)
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
	for _, q := range dns.asked() {
		if host(q.from) != exit {
			t.Errorf("a lookup left from %s, want %s", q.from, exit)
		}
	}
}

// putClients sends the entry a set of clients the way the hub does.
func (rb *runningBridge) putClients(t *testing.T, token string, clients ...bridgespec.Client) bridgespec.ClientsResult {
	t.Helper()
	resp, body := putClients(t, strings.TrimSuffix(rb.url, bridgespec.StatusPath), token, bridgespec.ClientsRequest{Clients: clients})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the bridge answered %s: %s", resp.Status, body)
	}
	var res bridgespec.ClientsResult
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// client finds one client in the status page.
func (rb *runningBridge) client(t *testing.T, email string) bridgespec.ClientStatus {
	t.Helper()
	st := rb.status()
	if st.Local == nil {
		t.Fatal("the status has no entry")
	}
	for _, c := range st.Local.Clients {
		if c.Email == email {
			return c
		}
	}
	t.Fatalf("no client %s in the status: %+v", email, st.Local.Clients)
	return bridgespec.ClientStatus{}
}

// The hub puts a subscription on the entry, takes one off and gives one a
// new id while the bridge runs: no restart, the others' connections stay, and
// what the one taken off had open is closed.
func TestE2ELocalEntryClientsChangeWhileItRuns(t *testing.T) {
	w := newEntryWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	b := startBridge(t, w.spec("9.9.9.9"))
	started := b.status().Started
	token := w.local().Token
	first := bridgespec.Client{Email: nodecfg.SubscriptionEmail(1), UUID: w.subUUID}
	second := bridgespec.Client{Email: nodecfg.SubscriptionEmail(2), UUID: newUUID()}
	viaFirst, viaSecond := w.client(first.UUID), w.client(second.UUID)

	// the first one is on from the start, with a connection it keeps open
	held, err := viaFirst(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	payload := randomBytes(t, 64<<10)
	if got := roundTrip(t, held, payload); !bytes.Equal(got, payload) {
		t.Fatal("what came back through the entry is not what was sent")
	}
	if carries(viaSecond, echo.addr()) {
		t.Fatal("a subscription that is not on the entry got through")
	}
	eventually(t, 5*time.Second, "the open connection to be counted", func() bool {
		return b.client(t, first.Email).Connections == 1
	})
	c := b.client(t, first.Email)
	if !c.On || c.BytesUp < uint64(len(payload)) || c.BytesDown < uint64(len(payload)) ||
		len(c.Addrs) != 1 || c.Addrs[0] != "127.0.0.1" || c.LastSeen.Before(started) {
		t.Errorf("the first subscription with a connection open: %+v", c)
	}

	// put the second on
	res := b.putClients(t, token, first, second)
	if len(res.Added) != 1 || res.Added[0] != second.Email || len(res.Removed)+len(res.Replaced) != 0 || res.Closed != 0 {
		t.Fatalf("adding one: %+v", res)
	}
	if !carries(viaSecond, echo.addr()) {
		t.Fatal("the subscription just put on can't get through")
	}
	if got := roundTrip(t, held, []byte("still here")); string(got) != "still here" {
		t.Fatal("the first subscription's open connection did not survive the change")
	}

	// take the first off: its open connection ends, and it can't come back
	res = b.putClients(t, token, second)
	if len(res.Removed) != 1 || res.Removed[0] != first.Email || res.Closed != 1 {
		t.Fatalf("removing one: %+v", res)
	}
	if !endsSoon(held, 10*time.Second) {
		t.Fatal("the connection of the subscription taken off stayed open")
	}
	if carries(viaFirst, echo.addr()) {
		t.Fatal("the subscription taken off still gets through")
	}
	if !carries(viaSecond, echo.addr()) {
		t.Fatal("the other subscription stopped getting through")
	}
	c = b.client(t, first.Email)
	if c.On || c.Connections != 0 || c.BytesUp < uint64(len(payload)) {
		t.Errorf("the subscription taken off: %+v", c)
	}

	// a new id for the second: the old one stops, the new one works
	renewed := bridgespec.Client{Email: second.Email, UUID: newUUID()}
	res = b.putClients(t, token, renewed)
	if len(res.Replaced) != 1 || res.Replaced[0] != second.Email || len(res.Added)+len(res.Removed) != 0 {
		t.Fatalf("replacing an id: %+v", res)
	}
	if carries(viaSecond, echo.addr()) {
		t.Fatal("the old id still gets through")
	}
	if !carries(w.client(renewed.UUID), echo.addr()) {
		t.Fatal("the new id can't get through")
	}

	if st := b.status(); !st.Started.Equal(started) {
		t.Errorf("the bridge started again at %v: the changes were to cost no restart", st.Started)
	}
}

// A change of the entry's clients costs a tunnel nothing.
func TestE2ELocalEntryClientChangeLeavesATunnelAlone(t *testing.T) {
	tun := newWorld(t, "")
	w := newEntryWorld(t, "")
	echo := newEchoServer(t, "127.0.0.1")
	spec := tun.spec("9.9.9.9")
	spec.Local = w.local()
	b := startBridge(t, spec)
	b.waitUp(t, "n1")
	before := b.node(t, "n1")

	held, err := tun.viaSubscription(echo.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if got := roundTrip(t, held, []byte("before")); string(got) != "before" {
		t.Fatalf("got %q", got)
	}

	b.putClients(t, spec.Local.Token) // everybody off the entry
	b.putClients(t, spec.Local.Token, bridgespec.Client{Email: nodecfg.SubscriptionEmail(7), UUID: newUUID()})

	if got := roundTrip(t, held, []byte("after")); string(got) != "after" {
		t.Fatal("a connection through the tunnel did not survive a change of the entry's clients")
	}
	if n := b.node(t, "n1"); !n.Up || !n.Since.Equal(before.Since) {
		t.Errorf("the tunnel was disturbed: %+v, before %+v", n, before)
	}
}

// udpEcho sends back every datagram it gets.
func udpEcho(t *testing.T) (port int) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], from)
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// udpClient runs a client for the entry whose local UDP port is carried to
// 127.0.0.1:to, and returns that local port's address.
func (w *entryWorld) udpClient(id string, to int) string {
	w.t.Helper()
	port := freePort(w.t)
	startInstance(w.t, map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag": "udp", "listen": "127.0.0.1", "port": port, "protocol": "dokodemo-door",
			"settings": map[string]any{"address": "127.0.0.1", "port": to, "network": "udp"},
		}},
		"outbounds": []any{map[string]any{
			"protocol": "vless",
			"settings": map[string]any{
				"address": w.listen, "port": w.entryPort, "id": id, "flow": w.flow, "encryption": "none",
			},
			"streamSettings": map[string]any{
				"network":  "tcp",
				"security": "reality",
				"realitySettings": map[string]any{
					"serverName": "localhost", "fingerprint": "chrome", "publicKey": w.publicKey, "shortId": w.shortID,
				},
			},
		}},
	})
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// echoes reports whether a datagram sent to addr comes back within d.
func echoes(addr, text string, d time.Duration) bool {
	c, err := net.Dial("udp", addr)
	if err != nil {
		return false
	}
	defer c.Close()
	if _, err := c.Write([]byte(text)); err != nil {
		return false
	}
	c.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	return err == nil && string(buf[:n]) == text
}

// UDP goes through the entry as well, is counted for its subscription, and
// stops with it.
func TestE2ELocalEntryCarriesAndCutsUDP(t *testing.T) {
	w := newEntryWorld(t, "")
	b := startBridge(t, w.spec("9.9.9.9"))
	email := nodecfg.SubscriptionEmail(1)
	local := w.udpClient(w.subUUID, udpEcho(t))

	eventually(t, 15*time.Second, "a datagram to come back through the entry", func() bool {
		return echoes(local, "ping", time.Second)
	})
	eventually(t, 5*time.Second, "the UDP flow to be counted", func() bool {
		return b.client(t, email).Connections >= 1
	})

	res := b.putClients(t, w.local().Token)
	if len(res.Removed) != 1 || res.Closed < 1 {
		t.Fatalf("taking it off: %+v", res)
	}
	eventually(t, 10*time.Second, "its UDP flow to end", func() bool {
		return b.client(t, email).Connections == 0
	})
	if echoes(local, "again", 3*time.Second) {
		t.Fatal("a datagram still came back after the subscription was taken off")
	}
}
