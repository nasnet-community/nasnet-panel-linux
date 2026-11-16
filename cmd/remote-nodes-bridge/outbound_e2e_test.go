// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The outbounds, end to end: the built bridge, nodes whose traffic it sends
// to an outbound, and behind each outbound a real server of that protocol (an
// Xray instance with the matching inbound) whose own exit reaches the loopback
// internet. A tcpProxy in front of each server counts what the bridge sent it,
// so a test can tell which way a connection left, and it can be cut.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// testCA signs the TLS servers of the tests. The test build of the bridge
// trusts it through BRIDGE_TEST_ROOTS.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "bridge test authority"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert: cert, key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

// issue makes a server certificate for name, as PEM.
func (ca *testCA) issue(t *testing.T, name string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// realityTarget is a TLS 1.3 server on X25519, what REALITY needs of its target.
func realityTarget(t *testing.T) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// checkServer is what the bridge's check asks for: it answers status.
func checkServer(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(status) }))
	t.Cleanup(srv.Close)
	return srv.URL + "/check"
}

// farEnd is the server an outbound connects to, behind a proxy that counts.
type farEnd struct {
	out   bridgespec.Outbound // pointed at the proxy, with no tag yet
	proxy *tcpProxy
}

// The kinds of server the end-to-end tests run, by the outbound that uses them.
var farEndKinds = []string{
	"vless over reality",
	"vless over xhttp and reality",
	"vless over xhttp, plain",
	"vless over ws, plain",
	"vless over httpupgrade, plain",
	"vmess over tcp",
	"trojan over tls",
	"trojan over ws and tls",
	"shadowsocks",
	"shadowsocks 2022",
	"socks with a login",
	"http with a login",
	"http over tls",
}

// newFarEnd starts a server of one kind and the proxy in front of it. Its own
// exit reaches anything, the loopback internet included, as a server elsewhere
// reaches the internet.
func newFarEnd(t *testing.T, kind string, ca *testCA) *farEnd {
	t.Helper()
	port := freePort(t)
	id := newUUID()
	in := map[string]any{"tag": "in", "listen": "127.0.0.1", "port": port}
	var out bridgespec.Outbound

	tlsStream := func(network string) map[string]any {
		cert, key := ca.issue(t, "far.test")
		return map[string]any{"network": network, "security": "tls", "tlsSettings": map[string]any{
			"certificates": []any{map[string]any{"certificate": []string{cert}, "key": []string{key}}},
		}}
	}
	realityStream := func(network string) (map[string]any, string) {
		private, public := realityKeys(t)
		return map[string]any{"network": network, "security": "reality", "realitySettings": map[string]any{
			"target": realityTarget(t), "serverNames": []string{"localhost"}, "privateKey": private, "shortIds": []string{"ab12"},
		}}, public
	}
	vlessIn := map[string]any{"decryption": "none", "clients": []any{map[string]any{"id": id}}}

	switch kind {
	case "vless over reality", "vless over xhttp and reality":
		network := bridgespec.NetTCP
		if kind != "vless over reality" {
			network = bridgespec.NetXHTTP
		}
		stream, public := realityStream(network)
		if network == bridgespec.NetXHTTP {
			stream["xhttpSettings"] = map[string]any{"path": "/x"}
		}
		in["protocol"], in["settings"], in["streamSettings"] = "vless", vlessIn, stream
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoVLESS, ID: id, Security: bridgespec.SecReality,
			ServerName: "localhost", PublicKey: public, ShortID: "ab12"}
		if network == bridgespec.NetXHTTP {
			out.Network, out.Path = bridgespec.NetXHTTP, "/x"
		}
	case "vless over xhttp, plain":
		in["protocol"], in["settings"] = "vless", vlessIn
		in["streamSettings"] = map[string]any{"network": "xhttp", "xhttpSettings": map[string]any{"path": "/x"}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoVLESS, ID: id, Network: bridgespec.NetXHTTP, Path: "/x"}
	case "vless over ws, plain":
		in["protocol"], in["settings"] = "vless", vlessIn
		in["streamSettings"] = map[string]any{"network": "ws", "wsSettings": map[string]any{"path": "/ws"}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoVLESS, ID: id, Network: bridgespec.NetWS, Path: "/ws?ed=2048"}
	case "vless over httpupgrade, plain":
		in["protocol"], in["settings"] = "vless", vlessIn
		in["streamSettings"] = map[string]any{"network": "httpupgrade", "httpupgradeSettings": map[string]any{"path": "/up"}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoVLESS, ID: id, Network: bridgespec.NetHTTPUpgrade, Path: "/up"}
	case "vmess over tcp":
		in["protocol"], in["settings"] = "vmess", map[string]any{"clients": []any{map[string]any{"id": id}}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoVMess, ID: id, Method: "aes-128-gcm"}
	case "trojan over tls", "trojan over ws and tls":
		network := bridgespec.NetTCP
		if kind != "trojan over tls" {
			network = bridgespec.NetWS
		}
		stream := tlsStream(network)
		if network == bridgespec.NetWS {
			stream["wsSettings"] = map[string]any{"path": "/t"}
		}
		in["protocol"], in["settings"], in["streamSettings"] = "trojan", map[string]any{"clients": []any{map[string]any{"password": "far-secret"}}}, stream
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoTrojan, Password: "far-secret", Security: bridgespec.SecTLS, ServerName: "far.test"}
		if network == bridgespec.NetWS {
			out.Network, out.Path = bridgespec.NetWS, "/t"
		}
	case "shadowsocks":
		in["protocol"], in["settings"] = "shadowsocks", map[string]any{"method": "chacha20-ietf-poly1305", "password": "far-secret", "network": "tcp,udp"}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoShadowsocks, Method: "chacha20-ietf-poly1305", Password: "far-secret"}
	case "shadowsocks 2022":
		key := base64.StdEncoding.EncodeToString(randomBytes(t, 16))
		in["protocol"], in["settings"] = "shadowsocks", map[string]any{"method": "2022-blake3-aes-128-gcm", "password": key, "network": "tcp,udp"}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoShadowsocks, Method: "2022-blake3-aes-128-gcm", Password: key}
	case "socks with a login":
		in["protocol"], in["settings"] = "socks", map[string]any{"auth": "password", "accounts": []any{map[string]any{"user": "far", "pass": "far-secret"}}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoSOCKS, Username: "far", Password: "far-secret"}
	case "http with a login":
		in["protocol"], in["settings"] = "http", map[string]any{"accounts": []any{map[string]any{"user": "far", "pass": "far-secret"}}}
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoHTTP, Username: "far", Password: "far-secret"}
	case "http over tls":
		// Xray's TLS offers h2 as well as HTTP/1.1, and its HTTP proxy speaks
		// HTTP/1.1 only: the bridge has to ask for that alone
		in["protocol"], in["settings"], in["streamSettings"] = "http", map[string]any{}, tlsStream("tcp")
		out = bridgespec.Outbound{Protocol: bridgespec.ProtoHTTP, Security: bridgespec.SecTLS, ServerName: "far.test"}
	default:
		t.Fatalf("no far end of kind %q", kind)
	}

	startInstance(t, map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": []any{in},
		"outbounds": []any{map[string]any{
			"protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow"}}},
		}},
	})
	proxy := newTCPProxy(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	out.Addr, out.Port = "127.0.0.1", uint16(proxy.port())
	if err := bridgespec.CheckOutbound(out); err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
	return &farEnd{out: out, proxy: proxy}
}

// outbound finds one outbound in the status page.
func (rb *runningBridge) outbound(t *testing.T, tag string) bridgespec.OutboundStatus {
	t.Helper()
	for _, o := range rb.status().Outbounds {
		if o.Tag == tag {
			return o
		}
	}
	t.Fatalf("no outbound %s in the status", tag)
	return bridgespec.OutboundStatus{}
}

// routedWorld is a node's portal and subscription, with the outbound its
// traffic is to leave through, if any.
type routedWorld struct {
	*world
	tag string
	end *farEnd // nil: the exit
}

// outboundBridge starts a bridge with a node for each far end, sent through
// it, and one more node that leaves through the exit. The check asks a server
// of the test's, as seldom as it can; edit changes the spec before it runs.
func outboundBridge(t *testing.T, ends []*farEnd, edit func(*bridgespec.Spec)) (*runningBridge, []routedWorld) {
	t.Helper()
	var worlds []routedWorld
	var spec *bridgespec.Spec
	for i := 0; i <= len(ends); i++ {
		w := newWorld(t, "")
		if spec == nil {
			spec = w.spec("9.9.9.9")
			spec.Nodes = nil
		}
		rw := routedWorld{world: w, tag: fmt.Sprintf("n%d", i+1)}
		n := w.node(rw.tag, "127.0.0.1", w.portalPort)
		if i < len(ends) {
			rw.end = ends[i]
			o := ends[i].out
			o.Tag = fmt.Sprintf("out-%d.", i+1)
			n.Via = o.Tag
			spec.Outbounds = append(spec.Outbounds, o)
		}
		spec.Nodes = append(spec.Nodes, n)
		worlds = append(worlds, rw)
	}
	spec.OutboundAddr = "127.0.0.1"
	spec.Probe = bridgespec.Probe{URL: checkServer(t, http.StatusNoContent), Seconds: bridgespec.MaxProbeSeconds}
	if edit != nil {
		edit(spec)
	}
	b := startBridge(t, spec)
	for _, w := range worlds {
		b.waitUp(t, w.tag)
	}
	return b, worlds
}

// A node's traffic leaves through its outbound, of every protocol, transport
// and security the bridge carries, and comes back; a node with no outbound
// still leaves through the exit; and the status page counts each outbound's
// bytes and says its check got an answer.
func TestE2EOutboundsCarryTraffic(t *testing.T) {
	ca := newTestCA(t)
	t.Setenv("BRIDGE_TEST_ROOTS", ca.pem)
	echo := newEchoServer(t, "127.0.0.1")
	var ends []*farEnd
	for _, kind := range farEndKinds {
		ends = append(ends, newFarEnd(t, kind, ca))
	}
	b, worlds := outboundBridge(t, ends, nil)

	payload := randomBytes(t, 256<<10)
	for i, kind := range farEndKinds {
		w := worlds[i]
		t.Run(kind, func(t *testing.T) {
			before := w.end.proxy.carried()
			conn, err := w.viaSubscription(echo.addr())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
				t.Fatal("what came back through the outbound is not what was sent")
			}
			// the far end counts a read once its write back is done, a moment
			// after the round trip has come back here
			moved := w.end.proxy.carried() - before
			for deadline := time.Now().Add(5 * time.Second); moved < 2*int64(len(payload)) && time.Now().Before(deadline); moved = w.end.proxy.carried() - before {
				time.Sleep(50 * time.Millisecond)
			}
			if moved < 2*int64(len(payload)) {
				t.Fatalf("the far end carried %d bytes of a round trip of %d each way: the traffic left some other way", moved, len(payload))
			}
		})
	}

	t.Run("a node with no outbound", func(t *testing.T) {
		w := worlds[len(worlds)-1]
		var before int64
		for _, e := range ends {
			before += e.proxy.carried()
		}
		conn, err := w.viaSubscription(echo.addr())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if got := roundTrip(t, conn, payload); !bytes.Equal(got, payload) {
			t.Fatal("what came back through the exit is not what was sent")
		}
		var after int64
		for _, e := range ends {
			after += e.proxy.carried()
		}
		if after != before {
			t.Errorf("the far ends carried %d bytes of a node that leaves through the exit", after-before)
		}
	})

	t.Run("the status page", func(t *testing.T) {
		// Xray adds what went out through an outbound to its counter when a
		// write is done, so the counts trail the traffic, and on amd64 the
		// upload of Shadowsocks and of VMess over tcp reads low or none at all:
		// only the download is held to what went through.
		counted := func(st bridgespec.Status) bool {
			for _, o := range st.Outbounds {
				if o.BytesDown < uint64(len(payload))/2 {
					return false
				}
			}
			return true
		}
		st := b.status()
		for deadline := time.Now().Add(10 * time.Second); !counted(st) && time.Now().Before(deadline); st = b.status() {
			time.Sleep(200 * time.Millisecond)
		}
		if len(st.Outbounds) != len(ends) {
			t.Fatalf("%d outbounds in the status, want %d", len(st.Outbounds), len(ends))
		}
		for i, o := range st.Outbounds {
			if o.Tag != fmt.Sprintf("out-%d.", i+1) {
				t.Errorf("outbound %d is %s: not in the spec's order", i, o.Tag)
			}
			if !o.Checked || !o.Alive || o.DelayMS <= 0 || o.Error != "" || o.LastTry.IsZero() || o.LastSeen.IsZero() || o.Fallback {
				t.Errorf("%s (%s) after a check that got an answer: %+v", o.Tag, farEndKinds[i], o)
			}
			// what came back over the connection to the server, as Xray counts
			// it: the protocol's own bytes in
			if o.BytesDown < uint64(len(payload))/2 {
				t.Errorf("%s (%s) carried %d each way, counts %d up and %d down", o.Tag, farEndKinds[i], len(payload), o.BytesUp, o.BytesDown)
			}
		}
	})
}

// The blocked ranges are refused whichever way a node leaves: through its
// outbound they go to the exit first, and the exit refuses them. Only literal
// addresses are caught, so a name the far end resolves to loopback still gets
// through it, which shows that route works at all.
func TestE2EOutboundKeepsBlockedRangesBlocked(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	end := newFarEnd(t, "socks with a login", newTestCA(t))
	b, worlds := outboundBridge(t, []*farEnd{end}, func(s *bridgespec.Spec) { s.Exit.Blocked = nil }) // the default list
	eventually(t, 10*time.Second, "the first check", func() bool { return b.outbound(t, "out-1.").Checked })
	viaOutbound, viaExit := worlds[0], worlds[1]

	refused := func(w routedWorld, target string) bool {
		conn, err := w.viaSubscription(target)
		if err != nil {
			return true
		}
		defer conn.Close()
		conn.Write([]byte("hello, LAN"))
		conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		n, _ := conn.Read(make([]byte, 64))
		return n == 0
	}
	before := end.proxy.carried()
	if !refused(viaOutbound, echo.addr()) {
		t.Error("a node sent through an outbound reached a blocked address")
	}
	if !refused(viaExit, echo.addr()) {
		t.Error("a node that leaves through the exit reached a blocked address")
	}
	if peers := echo.accepted(); len(peers) != 0 {
		t.Fatalf("a blocked address was reached: %v", peers)
	}
	if moved := end.proxy.carried() - before; moved != 0 {
		t.Errorf("the far end carried %d bytes meant for a blocked address", moved)
	}

	conn, err := viaOutbound.viaSubscription(net.JoinHostPort("localhost", strconv.Itoa(echo.port())))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := roundTrip(t, conn, []byte("by name")); string(got) != "by name" {
		t.Fatalf("got %q", got)
	}
	if end.proxy.carried() == before {
		t.Error("the far end carried nothing of a connection sent through it")
	}
}

// While an outbound with a fallback doesn't answer its check, new connections
// leave through the exit, and they go back once it answers again. One without
// a fallback stays down with it.
func TestE2EOutboundFallsBackToTheExit(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	ca := newTestCA(t)
	withFallback, without := newFarEnd(t, "socks with a login", ca), newFarEnd(t, "shadowsocks", ca)
	b, worlds := outboundBridge(t, []*farEnd{withFallback, without}, func(s *bridgespec.Spec) {
		s.Probe.Seconds = bridgespec.MinProbeSeconds
		s.Outbounds[0].Fallback = true
	})
	eventually(t, 15*time.Second, "both outbounds to answer their check", func() bool {
		a, b := b.outbound(t, "out-1."), b.outbound(t, "out-2.")
		return a.Checked && a.Alive && b.Checked && b.Alive
	})

	before := withFallback.proxy.carried()
	if !carriesTo(worlds[0], echo.addr()) {
		t.Fatal("nothing got through the outbound while it answered")
	}
	if withFallback.proxy.carried() == before {
		t.Fatal("the connection did not go through the outbound")
	}

	withFallback.proxy.cut()
	without.proxy.cut()
	eventually(t, 20*time.Second, "the check to find both down", func() bool {
		a, b := b.outbound(t, "out-1."), b.outbound(t, "out-2.")
		return !a.Alive && !b.Alive
	})
	if o := b.outbound(t, "out-1."); !o.Checked || !o.Fallback || o.Error == "" || o.DelayMS != 0 || !o.LastTry.After(o.LastSeen) {
		t.Errorf("the outbound with a fallback, down: %+v", o)
	}
	if o := b.outbound(t, "out-2."); o.Fallback {
		t.Errorf("the outbound without a fallback says it falls back: %+v", o)
	}
	seen := len(echo.accepted())
	if !carriesTo(worlds[0], echo.addr()) {
		t.Fatal("with its outbound down, a node with a fallback got nowhere")
	}
	if len(echo.accepted()) == seen {
		t.Fatal("the internet saw nothing of the connection that got through")
	}
	if carriesTo(worlds[1], echo.addr()) {
		t.Fatal("a node whose outbound has no fallback got through while it was down")
	}

	withFallback.proxy.restore()
	eventually(t, 20*time.Second, "the check to find it up again", func() bool {
		o := b.outbound(t, "out-1.")
		return o.Alive && !o.Fallback
	})
	before = withFallback.proxy.carried()
	if !carriesTo(worlds[0], echo.addr()) {
		t.Fatal("nothing got through once the outbound answered again")
	}
	if withFallback.proxy.carried() == before {
		t.Fatal("the connection stayed with the exit after the outbound came back")
	}
}

// carriesTo reports whether a round trip to target works through w.
func carriesTo(w routedWorld, target string) bool {
	conn, err := w.viaSubscription(target)
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

// The bridge's three addresses in use, with outbounds: the tunnels dial from
// one, the exit leaves from another, and the outbounds connect from the
// third, plain tcp and xhttp alike, the check's connections included. Linux
// lets a test bind them on loopback.
func TestE2EOutboundSourceAddress(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs three loopback addresses to send from; only Linux has all of 127.0.0.0/8")
	}
	const dial, exit, from = "127.0.0.18", "127.0.0.17", "127.0.0.19"
	for _, ip := range []string{dial, exit, from} {
		l, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
		if err != nil {
			t.Skipf("cannot use %s here: %v", ip, err)
		}
		l.Close()
	}
	echo := newEchoServer(t, "127.0.0.1")
	ca := newTestCA(t)
	ends := []*farEnd{newFarEnd(t, "socks with a login", ca), newFarEnd(t, "vless over xhttp, plain", ca)}

	var worlds []*world
	var spec *bridgespec.Spec
	for i, end := range ends {
		w := newWorld(t, "")
		portal := newTCPProxy(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(w.portalPort)))
		n := w.node(fmt.Sprintf("n%d", i+1), "127.0.0.1", portal.port())
		o := end.out
		o.Tag = fmt.Sprintf("out-%d.", i+1)
		n.Via = o.Tag
		if spec == nil {
			spec = w.spec("9.9.9.9")
			spec.Nodes = nil
		}
		spec.Nodes = append(spec.Nodes, n)
		spec.Outbounds = append(spec.Outbounds, o)
		worlds = append(worlds, w)
	}
	spec.DialAddr, spec.Exit.Addr, spec.OutboundAddr = dial, exit, from
	spec.Probe = bridgespec.Probe{URL: checkServer(t, http.StatusNoContent), Seconds: bridgespec.MaxProbeSeconds}
	b := startBridge(t, spec)
	for i, w := range worlds {
		b.waitUp(t, fmt.Sprintf("n%d", i+1))
		conn, err := w.viaSubscription(echo.addr())
		if err != nil {
			t.Fatal(err)
		}
		if got := roundTrip(t, conn, []byte("who am i")); string(got) != "who am i" {
			t.Fatalf("got %q", got)
		}
		conn.Close()
	}
	host := func(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }
	for i, end := range ends {
		peers := end.proxy.accepted()
		if len(peers) == 0 {
			t.Errorf("outbound %d: its server saw no connection", i+1)
		}
		for _, p := range peers {
			if host(p) != from {
				t.Errorf("outbound %d: a connection came from %s, want %s", i+1, p, from)
			}
		}
	}
}

// An outbound Xray won't take doesn't stop the bridge: it starts without it,
// the other outbound and the exit carry traffic, what was meant for the one
// left out goes to the exit if it has a fallback and nowhere if not, and the
// status page and the log say so.
func TestE2EARefusedOutboundIsLeftOut(t *testing.T) {
	echo := newEchoServer(t, "127.0.0.1")
	good := newFarEnd(t, "socks with a login", newTestCA(t))
	// to a public address without tls Xray takes neither VLESS nor Trojan
	plainVLESS := bridgespec.Outbound{Protocol: bridgespec.ProtoVLESS, Addr: "198.51.100.7", Port: 80, ID: newUUID()}
	plainTrojan := bridgespec.Outbound{Protocol: bridgespec.ProtoTrojan, Addr: "198.51.100.7", Port: 80, Password: "never-shown"}
	b, worlds := outboundBridge(t, []*farEnd{good, {out: plainVLESS}, {out: plainTrojan}}, func(s *bridgespec.Spec) {
		s.Outbounds[1].Fallback = true
	})
	withFallback, without, viaExit := worlds[1], worlds[2], worlds[3]

	before := good.proxy.carried()
	if !carriesTo(worlds[0], echo.addr()) || good.proxy.carried() == before {
		t.Error("the outbound Xray takes carries nothing")
	}
	if !carriesTo(viaExit, echo.addr()) {
		t.Error("a node that leaves through the exit got nowhere")
	}
	seen := len(echo.accepted())
	if !carriesTo(withFallback, echo.addr()) {
		t.Error("the node of an outbound left out with a fallback did not leave through the exit")
	}
	if len(echo.accepted()) != seen+1 {
		t.Errorf("the internet saw %d connection(s) of it, want 1", len(echo.accepted())-seen)
	}
	seen = len(echo.accepted())
	if carriesTo(without, echo.addr()) {
		t.Error("the node of an outbound left out without a fallback got through")
	}
	if len(echo.accepted()) != seen {
		t.Error("the internet saw a connection of a node whose outbound was left out without a fallback")
	}

	st := b.status()
	if len(st.Outbounds) != 3 || !st.Outbounds[0].Alive {
		t.Fatalf("outbounds %+v", st.Outbounds)
	}
	for i, fallback := range map[int]bool{1: true, 2: false} {
		o := st.Outbounds[i]
		if !o.Checked || o.Alive || o.Fallback != fallback || !strings.HasPrefix(o.Error, "Xray does not accept these settings: ") {
			t.Errorf("%s: %+v", o.Tag, o)
		}
	}
	out := b.out.String()
	for _, tag := range []string{"out-2.", "out-3."} {
		if !strings.Contains(out, "outbound "+tag+" left out: Xray does not accept these settings: ") {
			t.Errorf("the log does not say %s was left out:\n%s", tag, out)
		}
	}
	if strings.Contains(out, plainVLESS.ID) || strings.Contains(out, plainTrojan.Password) {
		t.Errorf("the log gives a secret away:\n%s", out)
	}
}
