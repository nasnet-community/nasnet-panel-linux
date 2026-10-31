// SPDX-License-Identifier: AGPL-3.0-or-later

package subscheck

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	// What a node's Xray registers for this config. Not main/distro/all: its
	// JSON loader drags in modules the panel doesn't otherwise need.
	"github.com/sirupsen/logrus"
	_ "github.com/xtls/xray-core/app/commander"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/dns"
	_ "github.com/xtls/xray-core/app/geodata"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/log/command"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/command"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/router"
	_ "github.com/xtls/xray-core/app/stats"
	_ "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/dokodemo"
	_ "github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/vless"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	"google.golang.org/grpc"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// A fixture key, not a secret: 32 bytes, 0x01 to 0x20.
const (
	realityKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"
	uuidBridge = "d0b6f1c8-7e25-4a9d-b4f3-8a1c2e5d6b70"
	passphrase = "correct horse battery staple"
)

// skipIfXrayRuns keeps these tests off a machine that runs Xray: the agent's
// process manager stops any process named xray it did not start itself.
func skipIfXrayRuns(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("pgrep", "-x", "xray").Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		t.Skip("a process named xray runs on this machine, and the agent's process manager would stop it")
	}
}

func quiet(t *testing.T) {
	prev := logrus.GetLevel()
	logrus.SetLevel(logrus.PanicLevel)
	t.Cleanup(func() { logrus.SetLevel(prev) })
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// eventually waits, in real time, for a condition the manager makes true.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("after 30 s: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func freePorts(t *testing.T, n int) []int {
	t.Helper()
	var ports []int
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("can't open a loopback port here: %v", err)
		}
		defer l.Close() // held until all are chosen, so none repeats
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

// logBuf is what a manager logged. It is only shown if the test fails, since a
// manager goes on logging for a moment after its test has ended.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// publicKeyOf is the REALITY public key of a private one, in the base64url
// that links carry.
func publicKeyOf(t *testing.T, private string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(private)
	must(t, err)
	key, err := ecdh.X25519().NewPrivateKey(raw)
	must(t, err)
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}

// node is a real agent in front of a real Xray with the entry inbound, as an
// installed node has them: it trusts one CA, and the one client it holds is the
// bridge's.
type node struct {
	xray       *core.Instance
	cfg        *config.Config // the agent's, to start it again
	agentPort  int
	entryPort  int
	stopAgent  func()
	publicKey  string
	bridgeUUID string
}

// startNode starts a node that trusts ca.
func startNode(t *testing.T, ca *pki.CA, id uint, now time.Time) *node {
	t.Helper()
	skipIfXrayRuns(t)
	quiet(t)
	ports := freePorts(t, 2)
	n := &node{entryPort: ports[0], publicKey: publicKeyOf(t, realityKey), bridgeUUID: uuidBridge}

	raw, err := nodecfg.Build(nodecfg.Input{
		EntryPort: ports[0], APIPort: ports[1],
		RealityDest: "www.example.com:443",
		ServerNames: []string{"www.example.com"},
		PrivateKey:  realityKey,
		ShortIDs:    []string{"a1b2c3d4"},
		Bridge:      nodecfg.Bridge{UUID: uuidBridge, Email: nodecfg.BridgeEmail(int(id)), ReverseTag: nodecfg.BridgeReverseTag(int(id))},
		// a short list keeps the test quick
		DomesticCIDRs: []string{"5.22.0.0/17"},
	})
	must(t, err)
	// keep the test off the network's interfaces
	var cfg map[string]any
	must(t, json.Unmarshal(raw, &cfg))
	cfg["inbounds"].([]any)[1].(map[string]any)["listen"] = "127.0.0.1"
	raw, err = json.Marshal(cfg)
	must(t, err)

	var parsed conf.Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Xray can't read the config: %v", err)
	}
	built, err := parsed.Build()
	if err != nil {
		t.Fatalf("Xray refuses the config: %v", err)
	}
	if n.xray, err = core.New(built); err != nil {
		t.Fatalf("Xray can't build the config: %v", err)
	}
	t.Cleanup(func() { n.xray.Close() })
	if err := n.xray.Start(); err != nil {
		t.Fatalf("Xray won't start the config: %v", err)
	}
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, b, 0o600))
		return p
	}
	cfgPath := write("config.json", raw)
	api := fmt.Sprintf("127.0.0.1:%d", ports[1])
	eventually(t, "Xray's API never came up", func() bool {
		c, err := net.Dial("tcp", api)
		if err == nil {
			c.Close()
		}
		return err == nil
	})

	nodeCert, err := ca.IssueNodeCert(id, "check", "127.0.0.1", now)
	must(t, err)
	acfg := config.DefaultConfig()
	acfg.Profile = config.ProfileManaged
	acfg.TLS = config.TLSConfig{
		Enabled:    true,
		CACert:     write("ca.crt", ca.CertPEM()),
		ServerCert: write("server.crt", nodeCert.CertPEM),
		ServerKey:  write("server.key", nodeCert.KeyPEM),
	}
	// nothing outside the temp folder, and no Xray binary to start. The traffic
	// buffer is off: a started agent with it on races its own Stop in the core
	// traffic collector, which the race detector reports at the end of the test.
	acfg.Process.ServiceMode = false
	acfg.Process.WatchdogEnabled = false
	acfg.Xray.AccessLogPath = ""
	acfg.Xray.BinaryPath = filepath.Join(dir, "xray")
	acfg.Xray.ConfigPath = cfgPath
	acfg.Xray.APIAddr = api
	acfg.Cores.DataDir = filepath.Join(dir, "cores")
	acfg.Traffic.BufferEnabled = false
	n.cfg = acfg
	for attempt := 0; n.agentPort == 0; attempt++ {
		if attempt == 5 {
			t.Fatal("no free port to start the agent on")
		}
		l, err := net.Listen("tcp", "127.0.0.1:0")
		must(t, err)
		addr := l.Addr().String()
		l.Close()
		if stop := startAgent(t, acfg, addr); stop != nil {
			_, port, _ := net.SplitHostPort(addr)
			n.agentPort, _ = strconv.Atoi(port)
			n.stopAgent = stop
		}
	}
	return n
}

// restartAgent starts the agent again where it was, as a node's does after a
// restart. Xray behind it has been up all along.
func (n *node) restartAgent(t *testing.T) {
	t.Helper()
	for attempt := 0; ; attempt++ {
		if stop := startAgent(t, n.cfg, fmt.Sprintf("127.0.0.1:%d", n.agentPort)); stop != nil {
			n.stopAgent = stop
			return
		}
		if attempt == 20 {
			t.Fatal("the agent's port stayed in use")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startAgent starts the agent on addr and returns what stops it, or nil if the
// port was taken.
func startAgent(t *testing.T, cfg *config.Config, addr string) (stop func()) {
	t.Helper()
	cfg.ListenAddr = addr
	srv, err := server.NewServer(cfg)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-done:
			if errors.Is(err, syscall.EADDRINUSE) {
				return nil
			}
			t.Fatalf("the agent stopped: %v", err)
		default:
		}
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			var once sync.Once
			stop := func() {
				once.Do(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					srv.Stop(ctx)
				})
			}
			t.Cleanup(stop)
			return stop
		}
	}
	t.Fatal("the agent never listened")
	return nil
}

// clients are the subscription clients Xray holds on the entry, by email, found
// through its own user manager and not through the agent, along with the
// bridge's one.
func (n *node) clients(t *testing.T, subs ...uint) map[string]string {
	t.Helper()
	ctx := context.Background()
	h, err := n.xray.GetFeature(inbound.ManagerType()).(inbound.Manager).GetHandler(ctx, nodecfg.EntryTag)
	must(t, err)
	users := h.(proxy.GetInbound).GetInbound().(proxy.UserManager)
	held := map[string]string{}
	emails := []string{nodecfg.BridgeEmail(1)}
	for _, s := range subs {
		emails = append(emails, nodecfg.SubscriptionEmail(int(s)))
	}
	for _, email := range emails {
		if u := users.GetUser(ctx, email); u != nil {
			held[email] = u.Account.(*vless.MemoryAccount).ID.String()
		}
	}
	return held
}

// hub is a manager with the state it keeps, the way the hub runs one, and a
// connector that dials the real agent from the CA the manager holds when it is
// asked, which is what the real connector has to do.
type hub struct {
	t    *testing.T
	st   *store.Store
	mgr  *manager.Manager
	log  *logBuf
	coll *usage.Collector
}

func newHub(t *testing.T) *hub {
	t.Helper()
	h := &hub{t: t, log: &logBuf{}}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("what a manager logged:\n%s", h.log)
		}
	})
	var err error
	if h.st, err = store.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if h.coll, err = usage.Open(usage.Config{Logf: func(format string, args ...any) { fmt.Fprintf(h.log, "usage: "+format+"\n", args...) }}); err != nil {
		t.Fatal(err)
	}
	h.mgr, err = manager.New(manager.Config{
		Store: h.st, Usage: h.coll, Connector: manager.ConnectorFunc(h.dial), Log: slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		PollEvery: 100 * time.Millisecond, StateEvery: 200 * time.Millisecond,
	})
	must(t, err)
	return h
}

// dial is the connector: the agent's client, over mutual TLS, with a client
// certificate made from the CA that is the manager's at this moment.
func (h *hub) dial(ctx context.Context, n model.Node) (manager.Client, error) {
	ca := h.mgr.CA()
	if ca == nil {
		return nil, errors.New("the manager has no CA")
	}
	hubCert, err := ca.IssueHubClientCert(time.Now())
	if err != nil {
		return nil, err
	}
	return agent.NewClient(net.JoinHostPort(n.Host, strconv.Itoa(n.AgentPort)),
		&agent.TLSConfig{CACert: string(ca.CertPEM()), ClientCert: string(hubCert.CertPEM), ClientKey: string(hubCert.KeyPEM)},
		2*time.Second, grpc.WithReturnConnectionError())
}

// addNode puts the node into the state as an install leaves one.
func (h *hub) addNode(n *node) model.Node {
	h.t.Helper()
	var out model.Node
	must(h.t, h.st.Update(func(s *model.State) error {
		var err error
		out, err = s.AddNode(model.Node{
			Name: "check", Host: "127.0.0.1", SSHPort: 22, AgentPort: n.agentPort, EntryPort: n.entryPort, State: model.NodeRegistered,
			CreatedAt: time.Now(), Arch: "arm64", AgentVersion: "1.2.0", XrayVersion: "26.10.21", BridgeUUID: n.bridgeUUID,
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com", RealityPublicKey: n.publicKey, RealityShortID: "a1b2c3d4",
		})
		return err
	}))
	return out
}

// run starts the manager and stops it with the test.
func (h *hub) run() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.mgr.Run(ctx)
		close(done)
	}()
	h.t.Cleanup(func() {
		cancel()
		<-done
		cctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := h.mgr.Close(cctx); err != nil {
			h.t.Errorf("closing the manager: %v", err)
		}
	})
}

// connected is whether the manager has talked to the node.
func (h *hub) connected(id uint) bool {
	l := h.mgr.NodeLive(id)
	return l.Agent != nil && l.Usage.Live && l.LastSeenAt != nil
}
