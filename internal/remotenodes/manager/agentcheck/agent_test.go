// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// Everything the manager calls is the code it meets in production: the agent's
// server started the way cmd/node-agent starts it, with TLS that wants the
// hub's certificate and the managed profile, which turns down any call the
// plugin has no business making; the agent's client over that TLS; and an
// Xray with the node's own config behind the agent's API. Only the Xray
// binary is missing, since nothing here starts or restarts one.

// A fixture key, not a secret: 32 bytes, 0x01 to 0x20.
const realityKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"

const (
	uuidSub1   = "2f8e3f0b-3a38-4d27-9b0e-5c1d6a7e8f01"
	uuidSub2   = "9c4b7a52-61de-4f3a-a8c7-0d2e1b3f4a65"
	uuidSub3   = "5a1d9e37-8c42-4b06-91f5-3e7b2c4d8a19"
	uuidBridge = "d0b6f1c8-7e25-4a9d-b4f3-8a1c2e5d6b70"
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

// logBuf is what the manager logged. It is only shown if the test fails, since
// the manager goes on logging for a moment after the test has ended.
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

// startXray runs an Xray with the node's config, in this process, and says
// where its API listens.
func startXray(t *testing.T, ports []int) (cfgPath string, inst *core.Instance) {
	t.Helper()
	raw, err := nodecfg.Build(nodecfg.Input{
		EntryPort: ports[0], APIPort: ports[1],
		RealityDest: "www.example.com:443",
		ServerNames: []string{"www.example.com"},
		PrivateKey:  realityKey,
		ShortIDs:    []string{"a1b2c3d4"},
		// subscription 3 is on the node and nobody wants it; subscription 2 isn't there yet
		Clients: []nodecfg.Client{
			{UUID: uuidSub1, Email: nodecfg.SubscriptionEmail(1)},
			{UUID: uuidSub3, Email: nodecfg.SubscriptionEmail(3)},
		},
		Bridge: nodecfg.Bridge{UUID: uuidBridge, Email: nodecfg.BridgeEmail(1), ReverseTag: nodecfg.BridgeReverseTag(1)},
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
	inst, err = core.New(built)
	if err != nil {
		t.Fatalf("Xray can't build the config: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	if err := inst.Start(); err != nil {
		t.Fatalf("Xray won't start the config: %v", err)
	}
	cfgPath = filepath.Join(t.TempDir(), "config.json")
	must(t, os.WriteFile(cfgPath, raw, 0o600))

	// its API comes up a moment after Start returns
	api := fmt.Sprintf("127.0.0.1:%d", ports[1])
	eventually(t, "Xray's API never came up", func() bool {
		c, err := net.Dial("tcp", api)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
	return cfgPath, inst
}

// emails are the clients Xray holds on the entry, found by asking for the
// ones the test knows of in its own user manager rather than through the agent.
func emails(t *testing.T, inst *core.Instance) (held map[string]string) {
	t.Helper()
	ctx := context.Background()
	h, err := inst.GetFeature(inbound.ManagerType()).(inbound.Manager).GetHandler(ctx, nodecfg.EntryTag)
	must(t, err)
	users := h.(proxy.GetInbound).GetInbound().(proxy.UserManager)
	held = map[string]string{}
	for _, email := range []string{nodecfg.SubscriptionEmail(1), nodecfg.SubscriptionEmail(2), nodecfg.SubscriptionEmail(3), nodecfg.BridgeEmail(1)} {
		if u := users.GetUser(ctx, email); u != nil {
			held[email] = u.Account.(*vless.MemoryAccount).ID.String()
		}
	}
	return held
}

// world is a manager, a real agent and the Xray behind it.
type world struct {
	t    *testing.T
	mgr  *manager.Manager
	st   *store.Store
	coll *usage.Collector
	log  *logBuf
	xray *core.Instance
	node model.Node
}

// startWorld builds the manager, the node's Xray and its agent, puts the node
// and two subscriptions into the state, and runs the manager.
//
// The agent's traffic buffer is off. How the collector counts what the buffer
// holds is tested against the agent's real buffer in package usage, and a
// started agent with buffering on races its own Stop in the core traffic
// collector (internal/agent/server/core.go, which re-reads the field that Stop
// clears), which the race detector reports at the end of every such test.
func startWorld(t *testing.T) *world {
	t.Helper()
	skipIfXrayRuns(t)
	quiet(t)
	w := &world{t: t, log: &logBuf{}}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("what the manager logged:\n%s", w.log)
		}
	})

	var err error
	if w.st, err = store.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if w.coll, err = usage.Open(usage.Config{Logf: func(format string, args ...any) { fmt.Fprintf(w.log, "usage: "+format+"\n", args...) }}); err != nil {
		t.Fatal(err)
	}

	// the hub's side of the TLS, which is what the connector below dials with
	var caPEM, hubCert, hubKey string
	connect := manager.ConnectorFunc(func(ctx context.Context, n model.Node) (manager.Client, error) {
		return agent.NewClient(net.JoinHostPort(n.Host, strconv.Itoa(n.AgentPort)),
			&agent.TLSConfig{CACert: caPEM, ClientCert: hubCert, ClientKey: hubKey}, 5*time.Second, grpc.WithReturnConnectionError())
	})
	w.mgr, err = manager.New(manager.Config{
		Store: w.st, Usage: w.coll, Connector: connect, Log: slog.New(slog.NewTextHandler(w.log, nil)),
		PollEvery: 100 * time.Millisecond, StateEvery: 200 * time.Millisecond,
	})
	must(t, err)
	ca := w.mgr.CA()
	if ca == nil {
		t.Fatalf("the manager has no CA: %v", w.mgr.CAError())
	}
	caPEM = string(ca.CertPEM())
	now := time.Now()
	hub, err := ca.IssueHubClientCert(now)
	must(t, err)
	hubCert, hubKey = string(hub.CertPEM), string(hub.KeyPEM)
	nodeCert, err := ca.IssueNodeCert(1, "check", "127.0.0.1", now)
	must(t, err)

	ports := freePorts(t, 2)
	xrayConfig, inst := startXray(t, ports)
	w.xray = inst

	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, b, 0o600))
		return p
	}
	cfg := config.DefaultConfig()
	cfg.Profile = config.ProfileManaged
	cfg.TLS = config.TLSConfig{
		Enabled:    true,
		CACert:     write("ca.crt", ca.CertPEM()),
		ServerCert: write("server.crt", nodeCert.CertPEM),
		ServerKey:  write("server.key", nodeCert.KeyPEM),
	}
	// nothing outside the temp folder, and no Xray binary to start
	cfg.Process.ServiceMode = false
	cfg.Process.WatchdogEnabled = false
	cfg.Xray.AccessLogPath = ""
	cfg.Xray.BinaryPath = filepath.Join(dir, "xray")
	cfg.Xray.ConfigPath = xrayConfig
	cfg.Xray.APIAddr = fmt.Sprintf("127.0.0.1:%d", ports[1])
	cfg.Cores.DataDir = filepath.Join(dir, "cores")
	cfg.Traffic.BufferEnabled = false
	agentPort := 0
	for attempt := 0; agentPort == 0; attempt++ {
		if attempt == 5 {
			t.Fatal("no free port to start the agent on")
		}
		agentPort = tryStart(t, cfg)
	}

	// the node, as an install leaves it, with two subscriptions switched on
	must(t, w.st.Update(func(s *model.State) error {
		n, err := s.AddNode(model.Node{
			Name: "check", Host: "127.0.0.1", SSHPort: 22, AgentPort: agentPort, EntryPort: ports[0], State: model.NodeRegistered,
			CreatedAt: now, Arch: "arm64", AgentVersion: "1.2.0", XrayVersion: "26.10.21", BridgeUUID: uuidBridge,
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com", RealityPublicKey: realityKey, RealityShortID: "a1b2c3d4",
		})
		if err != nil {
			return err
		}
		w.node = n
		for i, u := range []string{uuidSub1, uuidSub2} {
			sub, err := s.AddSubscription(model.Subscription{Name: fmt.Sprintf("sub %d", i+1), CreatedAt: now})
			if err != nil {
				return err
			}
			s.Enablements = append(s.Enablements, model.Enablement{SubscriptionID: sub.ID, NodeID: n.ID, ClientUUID: u, Enabled: true})
		}
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.mgr.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		cctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := w.mgr.Close(cctx); err != nil {
			t.Errorf("closing the manager: %v", err)
		}
	})
	return w
}

// tryStart starts the agent on a port that was free a moment ago and returns
// it, or 0 if another process took it in between.
func tryStart(t *testing.T, cfg *config.Config) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	cfg.ListenAddr = l.Addr().String()
	l.Close()

	srv, err := server.NewServer(cfg)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-done:
			if errors.Is(err, syscall.EADDRINUSE) {
				return 0
			}
			t.Fatalf("the agent stopped: %v", err)
		default:
		}
		if c, err := net.Dial("tcp", cfg.ListenAddr); err == nil {
			c.Close()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				srv.Stop(ctx)
			})
			_, port, _ := net.SplitHostPort(cfg.ListenAddr)
			p, _ := strconv.Atoi(port)
			return p
		}
	}
	t.Fatal("the agent never listened")
	return 0
}

// The manager connects to the agent over mutual TLS with a certificate its
// own CA issued, polls it, reads its versions, and puts the node's clients
// right: the one nobody wants goes, the missing one comes with the id the
// state keeps for it, and the bridge's own client, which carries the reverse
// tag, is left alone. Every one of those calls goes through the managed
// profile, which turns down what isn't on its list.
func TestTheManagerConnectsPollsAndReconcilesARealAgent(t *testing.T) {
	w := startWorld(t)

	want := map[string]string{
		nodecfg.SubscriptionEmail(1): uuidSub1, nodecfg.SubscriptionEmail(2): uuidSub2, nodecfg.BridgeEmail(1): uuidBridge,
	}
	eventually(t, "the node's clients are not the ones wanted", func() bool {
		got := emails(t, w.xray)
		return len(got) == len(want) && got[nodecfg.SubscriptionEmail(1)] == uuidSub1 && got[nodecfg.SubscriptionEmail(2)] == uuidSub2 && got[nodecfg.BridgeEmail(1)] == uuidBridge
	})
	if got := emails(t, w.xray); !slices.Equal(sortedKeys(got), sortedKeys(want)) || got[nodecfg.SubscriptionEmail(2)] != uuidSub2 {
		t.Errorf("Xray holds %v, want %v", got, want)
	}

	eventually(t, "the node is never live", func() bool {
		l := w.mgr.NodeLive(w.node.ID)
		return l.Agent != nil && l.Usage.Live && l.LastSeenAt != nil
	})
	l := w.mgr.NodeLive(w.node.ID)
	if l.Agent.Version != server.Version {
		t.Errorf("the agent says it is %q, the real one is %q", l.Agent.Version, server.Version)
	}
	// an agent that can't run xray says "unknown", which is not a version
	if l.Agent.XrayVersion != "" {
		t.Errorf("the agent's Xray = %q", l.Agent.XrayVersion)
	}
	if got := w.st.Read().Nodes[0]; got.AgentVersion != server.Version || got.XrayVersion != "26.10.21" {
		t.Errorf("the state keeps agent %q and xray %q", got.AgentVersion, got.XrayVersion)
	}
	ev := w.mgr.NodeEvents(w.node.ID)
	if len(ev) != 1 || ev[0].Kind != manager.EventAgentUpdated || ev[0].Detail != "1.2.0 to "+server.Version {
		t.Errorf("events = %+v", ev)
	}
	// with no report from the router the entry is off, and so is the tunnel
	if e := w.mgr.Entry(); e.On || l.Tunnel.State != "off" || l.Tunnel.Reason != "entry_off" {
		t.Errorf("entry %+v, tunnel %+v", e, l.Tunnel)
	}

	// a poll with nothing buffered still counts as an answer
	if u := w.mgr.NodeLive(w.node.ID).Usage; !u.Live || u.UpdatedAt == nil {
		t.Errorf("usage = %+v", u)
	}
	if err := w.coll.Flush(context.Background()); err != nil {
		t.Errorf("flushing the counters: %v", err)
	}
	// the profile is in force: a call the plugin has no business making is turned
	// down, so the ones the manager made were on the list
	c, ok := w.mgr.Client(w.node.ID)
	if !ok {
		t.Fatal("no client to borrow")
	}
	if _, err := c.(*agent.Client).ExecuteCommand(context.Background(), "true", 1); status.Code(err) != codes.PermissionDenied {
		t.Errorf("ExecuteCommand on a managed agent: %v, want PermissionDenied", err)
	}

	for _, bad := range []string{"PermissionDenied", "permission denied", "could not all be reconciled", "level=ERROR"} {
		if strings.Contains(w.log.String(), bad) {
			t.Errorf("the log has %q:\n%s", bad, w.log)
		}
	}
}

// A new connection puts the clients right again: Xray, restarted, comes back
// with what its config file has and not what the hub added.
func TestANewConnectionToARealAgentReconcilesAgain(t *testing.T) {
	w := startWorld(t)
	eventually(t, "the node's clients are not the ones wanted", func() bool {
		got := emails(t, w.xray)
		return got[nodecfg.SubscriptionEmail(2)] == uuidSub2 && got[nodecfg.SubscriptionEmail(3)] == ""
	})
	c, ok := w.mgr.Client(w.node.ID)
	if !ok {
		t.Fatal("no client to borrow")
	}
	// a client lost, and one that nobody wants, put there by hand
	must(t, c.RemoveUser(context.Background(), nodecfg.EntryTag, nodecfg.SubscriptionEmail(2)))
	must(t, c.AddUser(context.Background(), nodecfg.EntryTag, nodecfg.SubscriptionEmail(3), uuidSub3, "vless", "xtls-rprx-vision", "none", 0))
	if got := emails(t, w.xray); got[nodecfg.SubscriptionEmail(2)] != "" || got[nodecfg.SubscriptionEmail(3)] != uuidSub3 {
		t.Fatalf("the change did not reach Xray: %v", got)
	}

	w.mgr.Reconnect(w.node.ID)
	eventually(t, "a new connection did not put the clients right", func() bool {
		got := emails(t, w.xray)
		return got[nodecfg.SubscriptionEmail(2)] == uuidSub2 && got[nodecfg.SubscriptionEmail(3)] == "" && got[nodecfg.BridgeEmail(1)] == uuidBridge
	})
	// the manager logs after it has finished, which can be a moment after Xray
	// already holds the right clients
	eventually(t, "the second reconciliation was never logged", func() bool {
		return strings.Count(w.log.String(), "reconciled a node's clients") >= 2
	})
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
