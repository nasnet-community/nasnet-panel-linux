// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// A small internet to run the bridge in, all on loopback with ephemeral
// ports: a real Xray portal with a REALITY inbound and the bridge's reverse
// client, a subscription's REALITY client behind a SOCKS port, a server that
// plays the internet, and a resolver. The portal's config is built by hand
// here; the hub's node config builder will replace it when that lands.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

const (
	// The portal's name for the tunnel: the routing there sends a
	// subscription's traffic to it. The bridge has its own name for the other
	// end, the reverse tag in its spec.
	portalReverseTag = "rvs-to-n1"
	subscriptionID   = "sub@test"
)

// startInstance runs an Xray instance built from loader JSON. The stop it
// returns may be called any time, and again; the test also calls it at the end.
func startInstance(t *testing.T, cfg map[string]any) (stop func()) {
	t.Helper()
	inst, err := core.New(load(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop = func() { once.Do(func() { inst.Close() }) }
	t.Cleanup(stop)
	return stop
}

// world is the portal side of a test: everything but the bridge.
type world struct {
	t *testing.T

	privateKey, publicKey string
	shortID               string
	bridgeUUID, subUUID   string
	flow                  string // of the bridge's client and the subscription's

	portalPort int    // where the portal listens
	socksPort  int    // the subscription's SOCKS proxy
	dest       string // the portal inbound's REALITY dest

	stopPortal func()
}

// newWorld starts a portal and a subscription's client. The portal listens on
// portalPort; tests point the bridge at it, or at something in front of it.
//
// flow is the VLESS flow the clients use, empty or Vision. Vision is what the
// product runs, but xray-core's Vision code races between its reader and
// writer, which the race detector rejects in this process, so most tests run
// without it and one runs Vision when the detector is off.
func newWorld(t *testing.T, flow string) *world {
	t.Helper()
	w := &world{
		t:          t,
		flow:       flow,
		shortID:    "0123456789abcdef",
		bridgeUUID: newUUID(),
		subUUID:    newUUID(),
		portalPort: freePort(t),
		socksPort:  freePort(t),
	}
	w.privateKey, w.publicKey = realityKeys(t)

	// The inbound's REALITY dest: a TLS 1.3 server on X25519, which is what
	// the handshake needs from it.
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		io.WriteString(rw, "dest")
	}))
	dest.TLS = &tls.Config{MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	dest.StartTLS()
	t.Cleanup(dest.Close)

	w.dest = dest.Listener.Addr().String()
	w.startPortal()

	startInstance(t, map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag": "socks", "listen": "127.0.0.1", "port": w.socksPort, "protocol": "socks",
			"settings": map[string]any{"auth": "noauth", "udp": false},
		}},
		"outbounds": []any{map[string]any{
			"protocol": "vless",
			"settings": map[string]any{
				"address": "127.0.0.1", "port": w.portalPort, "id": w.subUUID, "flow": w.flow, "encryption": "none",
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
	return w
}

// startPortal runs the portal. A test can stop it and start it again on the
// same port.
func (w *world) startPortal() {
	w.t.Helper()
	w.stopPortal = startInstance(w.t, map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag": "entry", "listen": "127.0.0.1", "port": w.portalPort, "protocol": "vless",
			"settings": map[string]any{
				"decryption": "none",
				"clients": []any{
					map[string]any{"id": w.subUUID, "flow": w.flow, "email": subscriptionID},
					// The bridge's client: it carries the reverse tag.
					map[string]any{"id": w.bridgeUUID, "flow": w.flow, "email": "bridge@test", "reverse": map[string]any{"tag": portalReverseTag}},
				},
			},
			"streamSettings": map[string]any{
				"network":  "tcp",
				"security": "reality",
				"realitySettings": map[string]any{
					"target":      w.dest,
					"serverNames": []string{"localhost"},
					"privateKey":  w.privateKey,
					"shortIds":    []string{w.shortID},
				},
			},
		}},
		"outbounds": []any{map[string]any{"tag": "blocked", "protocol": "blackhole"}},
		"routing": map[string]any{"rules": []any{map[string]any{
			"type": "field", "user": []string{subscriptionID}, "outboundTag": portalReverseTag,
		}}},
	})
}

// node is the bridge's spec entry for the portal at addr.
func (w *world) node(tag, addr string, port int) bridgespec.Node {
	return bridgespec.Node{
		Tag:        tag,
		Addr:       addr,
		Port:       uint16(port),
		UUID:       w.bridgeUUID,
		PublicKey:  w.publicKey,
		ShortID:    w.shortID,
		ServerName: "localhost",
		Flow:       w.flow,
		ReverseTag: "rvs-from-" + tag,
	}
}

// spec is a bridge spec for the portal, everything on 127.0.0.1 and the
// internet (loopback) open: the default blocked list without 127.0.0.0/8.
func (w *world) spec(dns string) *bridgespec.Spec {
	return &bridgespec.Spec{
		Version:    bridgespec.Version,
		Nodes:      []bridgespec.Node{w.node("n1", "127.0.0.1", w.portalPort)},
		DialAddr:   "127.0.0.1",
		Exit:       bridgespec.Exit{Addr: "127.0.0.1", Blocked: withoutLoopback()},
		DNS:        bridgespec.DNS{Servers: []string{dns}},
		StatusAddr: "127.0.0.1:18080",
		LogLevel:   "warning",
	}
}

// The bridge runs as the built program, as it does on the router. In process
// it would work too, but xray-core has a benign data race between a reverse
// outbound's constructor and its first dial two seconds later, which the race
// detector reports whenever the bridge lives that long.
var (
	binOnce sync.Once
	binDir  string
	binPath string
	binErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// bridgeBinary builds the program once per test run.
func bridgeBinary(t testing.TB) string {
	t.Helper()
	binOnce.Do(func() {
		if binDir, binErr = os.MkdirTemp("", "remote-nodes-bridge-test-"); binErr != nil {
			return
		}
		binPath = filepath.Join(binDir, "remote-nodes-bridge")
		goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
		if out, err := exec.Command(goTool, "build", "-o", binPath, ".").CombinedOutput(); err != nil {
			binErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

// lockedBuffer collects a process's output while the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// runningBridge is the bridge program with a spec on its stdin.
type runningBridge struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    *lockedBuffer
	url    string
	exited chan struct{}
	err    error // how it ended; valid once exited is closed
}

// launch starts the bridge program with nothing on its stdin yet.
func launch(t *testing.T) *runningBridge {
	t.Helper()
	rb := &runningBridge{t: t, out: &lockedBuffer{}, exited: make(chan struct{})}
	rb.cmd = exec.Command(bridgeBinary(t))
	rb.cmd.Stdout, rb.cmd.Stderr = rb.out, rb.out
	var err error
	if rb.stdin, err = rb.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	if err := rb.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		rb.err = rb.cmd.Wait()
		close(rb.exited)
	}()
	// However a test ended, the bridge has to stop when its stdin closes, and
	// stop cleanly, with its tunnels up and traffic running if that is where
	// the test left it. A test that waited for the exit itself has looked at
	// how it ended.
	t.Cleanup(func() {
		select {
		case <-rb.exited:
		default:
			rb.stdin.Close()
			select {
			case <-rb.exited:
				if rb.err != nil {
					t.Errorf("the bridge did not stop cleanly when its stdin closed: %v", rb.err)
				}
			case <-time.After(10 * time.Second):
				rb.cmd.Process.Kill()
				<-rb.exited
				t.Errorf("the bridge was still running 10 s after its stdin closed")
			}
		}
		if t.Failed() {
			t.Logf("bridge output:\n%s", rb.out)
		}
	})
	return rb
}

// startProcess starts the bridge program and sends it spec, with the status
// page moved to a free port. It does not wait for the page.
func startProcess(t *testing.T, spec *bridgespec.Spec) *runningBridge {
	t.Helper()
	spec.StatusAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	rb := launch(t)
	rb.url = "http://" + spec.StatusAddr + bridgespec.StatusPath
	if err := bridgespec.Write(rb.stdin, spec); err != nil {
		t.Fatal(err)
	}
	return rb
}

// startBridge runs the bridge for spec and waits until it serves its status.
func startBridge(t *testing.T, spec *bridgespec.Spec) *runningBridge {
	t.Helper()
	rb := startProcess(t, spec)
	eventually(t, 20*time.Second, "the status page", func() bool {
		select {
		case <-rb.exited:
			t.Fatalf("the bridge exited before it served: %v\n%s", rb.err, rb.out)
		default:
		}
		_, err := rb.fetch()
		return err == nil
	})
	return rb
}

// fetch reads the status page.
func (rb *runningBridge) fetch() (bridgespec.Status, error) {
	var st bridgespec.Status
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(rb.url)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("status page answered %s", resp.Status)
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

func (rb *runningBridge) status() bridgespec.Status {
	rb.t.Helper()
	st, err := rb.fetch()
	if err != nil {
		rb.t.Fatal(err)
	}
	return st
}

func (rb *runningBridge) node(t *testing.T, tag string) bridgespec.NodeStatus {
	t.Helper()
	for _, n := range rb.status().Nodes {
		if n.Tag == tag {
			return n
		}
	}
	t.Fatalf("no node %s in the status", tag)
	return bridgespec.NodeStatus{}
}

// waitUp waits for the tunnel to read as connected: the core starts dialing
// a couple of seconds after it starts, and a tunnel counts once it has
// lasted a few more.
func (rb *runningBridge) waitUp(t *testing.T, tag string) {
	t.Helper()
	eventually(t, 30*time.Second, "tunnel "+tag+" to come up", func() bool { return rb.node(t, tag).Up })
}

// viaSubscription opens a connection to target through the subscription's
// client, so it travels client, portal, tunnel, bridge, exit.
func (w *world) viaSubscription(target string) (net.Conn, error) {
	d, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(w.socksPort)), nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	return d.Dial("tcp", target)
}

// echoServer plays the internet: it sends back what it gets and remembers
// who connected.
type echoServer struct {
	l     net.Listener
	mu    sync.Mutex
	peers []string
}

func newEchoServer(t *testing.T, ip string) *echoServer {
	t.Helper()
	l, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", ip, err)
	}
	e := &echoServer{l: l}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			e.mu.Lock()
			e.peers = append(e.peers, c.RemoteAddr().String())
			e.mu.Unlock()
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	return e
}

func (e *echoServer) addr() string { return e.l.Addr().String() }
func (e *echoServer) port() int    { return e.l.Addr().(*net.TCPAddr).Port }

func (e *echoServer) accepted() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.peers...)
}

// dnsServer answers A queries for the names it was given and records who
// asked for what.
type dnsServer struct {
	pc      net.PacketConn
	answers map[string]netip.Addr

	mu      sync.Mutex
	queries []dnsQuery
}

type dnsQuery struct {
	name string
	typ  dnsmessage.Type
	from string
}

func newDNSServer(t *testing.T, ip string, answers map[string]netip.Addr) *dnsServer {
	t.Helper()
	pc, err := net.ListenPacket("udp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", ip, err)
	}
	d := &dnsServer{pc: pc, answers: answers}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			d.mu.Lock()
			d.queries = append(d.queries, dnsQuery{name: q.Name.String(), typ: q.Type, from: from.String()})
			d.mu.Unlock()

			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionAvailable: true},
				Questions: []dnsmessage.Question{q},
			}
			if ip, ok := answers[q.Name.String()]; ok && q.Type == dnsmessage.TypeA {
				reply.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: ip.As4()},
				}}
			}
			if out, err := reply.Pack(); err == nil {
				pc.WriteTo(out, from)
			}
		}
	}()
	return d
}

func (d *dnsServer) addr() string { return d.pc.LocalAddr().String() }

func (d *dnsServer) asked() []dnsQuery {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dnsQuery(nil), d.queries...)
}

// roundTrip sends payload through c while reading it back, and returns what
// came back.
func roundTrip(t *testing.T, c net.Conn, payload []byte) []byte {
	t.Helper()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	go func() {
		c.Write(payload)
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("read %d bytes back: %v", len(payload), err)
	}
	return got
}
