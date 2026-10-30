// SPDX-License-Identifier: AGPL-3.0-or-later

package subscriptions

// These run the real thing inside the test: Xray with the node's own config,
// the agent's server in front of it, and the hub's clients in front of that.
// They hold what the idempotence check depends on, the words Xray and the
// agent use, against the code that says them, so an Xray bump that rewords
// them fails here and not on somebody's router.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	// What a node's Xray registers for this config. Not main/distro/all: its
	// JSON loader drags in modules the panel doesn't otherwise need.
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
	"google.golang.org/grpc/credentials"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/middleware"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/embedded"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// A fixture key, not a secret: 32 bytes, 0x01 to 0x20.
const realityKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"

const bridgeTag = "rvs-to-node-7"

// liveNode is a running Xray with the node's config and the agent's server on
// it. agents holds the hub's two ways in: the embedded client, which calls the
// server's methods, and the real gRPC client over mutual TLS to a server that
// runs the managed profile, as every node of this plugin does.
type liveNode struct {
	inst   *core.Instance
	agents map[string]Agent
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

func startLiveNode(t *testing.T) *liveNode {
	t.Helper()
	ports := freePorts(t, 2)
	raw, err := nodecfg.Build(nodecfg.Input{
		EntryPort: ports[0], APIPort: ports[1],
		RealityDest: "www.example.com:443",
		ServerNames: []string{"www.example.com"},
		PrivateKey:  realityKey,
		ShortIDs:    []string{"a1b2c3d4"},
		Clients:     []nodecfg.Client{{UUID: uuidA, Email: sub1}, {UUID: uuidB, Email: sub2}},
		Bridge:      nodecfg.Bridge{UUID: uuidC, Email: bridge, ReverseTag: bridgeTag},
		// a short list keeps the test quick
		DomesticCIDRs: []string{"5.22.0.0/17"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// keep the test off the network's interfaces
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["inbounds"].([]any)[1].(map[string]any)["listen"] = "127.0.0.1"
	if raw, err = json.Marshal(cfg); err != nil {
		t.Fatal(err)
	}

	var parsed conf.Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Xray can't read the config: %v", err)
	}
	built, err := parsed.Build()
	if err != nil {
		t.Fatalf("Xray refuses the config: %v", err)
	}
	inst, err := core.New(built)
	if err != nil {
		t.Fatalf("Xray can't build the config: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	if err := inst.Start(); err != nil {
		t.Fatalf("Xray won't start the config: %v", err)
	}

	// the agent in front of it, built the way its main builds it, with the
	// pieces that would touch the machine left out
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ac := config.DefaultConfig()
	ac.Profile = config.ProfileManaged
	ac.Xray.BinaryPath = filepath.Join(dir, "xray") // never run: nothing here starts or restarts Xray
	ac.Xray.ConfigPath = cfgPath
	ac.Xray.APIAddr = fmt.Sprintf("127.0.0.1:%d", ports[1])
	ac.Xray.AccessLogPath = ""
	ac.Process.ServiceMode = false
	ac.Process.WatchdogEnabled = false
	ac.Traffic.BufferEnabled = false
	ac.Cores.Embedded = true
	srv, err := server.NewServer(ac)
	if err != nil {
		t.Fatal(err)
	}

	n := &liveNode{inst: inst, agents: map[string]Agent{"embedded": embedded.New(srv)}}
	n.agents["grpc"] = grpcClient(t, srv)
	n.waitForAPI(t)
	return n
}

// grpcClient serves srv over mutual TLS behind the managed profile's check and
// returns the hub's own client for it.
func grpcClient(t *testing.T, srv *server.Server) *agent.Client {
	t.Helper()
	ca, err := certs.GenerateCA(nil)
	if err != nil {
		t.Fatal(err)
	}
	node, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID: 7, NodeName: "subscriptions-test", IPAddresses: []string{"127.0.0.1"}, ValidMonths: 1,
	}, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := certs.GenerateMasterCert(nil, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(node.Certificate, node.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca.Certificate)

	profile := middleware.NewProfileMiddleware(config.ProfileManaged)
	gs := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots,
			MinVersion: tls.VersionTLS12, NextProtos: []string{"h2"},
		})),
		grpc.ChainUnaryInterceptor(profile.UnaryServerInterceptor()),
	)
	pb.RegisterNodeAgentServer(gs, srv)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't open a loopback port here: %v", err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	c, err := agent.NewClient(lis.Addr().String(), &agent.TLSConfig{
		CACert: string(ca.Certificate), ClientCert: string(hub.Certificate), ClientKey: string(hub.PrivateKey),
	}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// waitForAPI waits for Xray's API server, which comes up a moment after Start
// returns.
func (n *liveNode) waitForAPI(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		_, err := n.agents["embedded"].ListUsers(ctx, nodecfg.EntryTag)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("Xray's API never came up: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// credential is what the running Xray holds for the email, read from the
// inbound's own user manager rather than through the agent.
type credential struct {
	uuid, flow, reverseTag string
}

func (n *liveNode) credential(t *testing.T, email string) (credential, bool) {
	t.Helper()
	ctx := context.Background()
	h, err := n.inst.GetFeature(inbound.ManagerType()).(inbound.Manager).GetHandler(ctx, nodecfg.EntryTag)
	if err != nil {
		t.Fatal(err)
	}
	users := h.(proxy.GetInbound).GetInbound().(proxy.UserManager)
	u := users.GetUser(ctx, email)
	if u == nil {
		return credential{}, false
	}
	acc := u.Account.(*vless.MemoryAccount)
	c := credential{uuid: acc.ID.String(), flow: acc.Flow}
	if acc.Reverse != nil {
		c.reverseTag = acc.Reverse.Tag
	}
	return c, true
}

func (n *liveNode) emails(t *testing.T, a Agent) []string {
	t.Helper()
	users, err := a.ListUsers(context.Background(), nodecfg.EntryTag)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, u := range users {
		out = append(out, u.Email)
	}
	slices.Sort(out)
	return out
}

// everyAgent runs a scenario on a fresh node through each way the hub has of
// reaching it.
func everyAgent(t *testing.T, scenario func(t *testing.T, n *liveNode, a Agent)) {
	for _, name := range []string{"embedded", "grpc"} {
		t.Run(name, func(t *testing.T) {
			n := startLiveNode(t)
			scenario(t, n, n.agents[name])
		})
	}
}

// The agent's ListUsers sends the email and the level. Reconcile's note about
// UUIDs rests on this: if it ever sends the UUID too, that note, and the
// comparison Reconcile skips today, want another look.
func TestRealAgentListsEmailsAndNoUUIDs(t *testing.T) {
	everyAgent(t, func(t *testing.T, n *liveNode, a Agent) {
		users, err := a.ListUsers(context.Background(), nodecfg.EntryTag)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := n.emails(t, a), sorted(sub1, sub2, bridge); !slices.Equal(got, want) {
			t.Fatalf("the entry lists %v, want %v", got, want)
		}
		for _, u := range users {
			if u.Uuid != "" || u.Flow != "" || u.Protocol != "" || u.Encryption != "" {
				t.Errorf("%s: ListUsers sends %+v; the agent was thought to send the email and level only", u.Email, u)
			}
		}
	})
}

func TestRealAgentEnableAndDisable(t *testing.T) {
	everyAgent(t, func(t *testing.T, n *liveNode, a Agent) {
		ctx := context.Background()

		must(t, Enable(ctx, a, Client{Email: sub3, UUID: uuidA}))
		got, ok := n.credential(t, sub3)
		if !ok || got != (credential{uuid: uuidA, flow: DefaultFlow}) {
			t.Fatalf("Xray holds %+v (there: %v) for %s", got, ok, sub3)
		}

		// again, and again with another UUID: the same, and nothing changes
		must(t, Enable(ctx, a, Client{Email: sub3, UUID: uuidA}))
		must(t, Enable(ctx, a, Client{Email: sub3, UUID: uuidB}))
		if got, _ := n.credential(t, sub3); got.uuid != uuidA {
			t.Fatalf("Enable changed the credential to %s", got.uuid)
		}

		// the words themselves, from the real chain
		err := a.AddUser(ctx, nodecfg.EntryTag, sub3, uuidA, "vless", DefaultFlow, "none", 0)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(xrayExists, sub3)) || !alreadyThere(err, sub3) {
			t.Fatalf("adding a client twice answers %v", err)
		}
		t.Logf("a second add reads: %v", err)

		must(t, Disable(ctx, a, sub3))
		if _, ok := n.credential(t, sub3); ok {
			t.Fatal("the client is still on the node")
		}
		must(t, Disable(ctx, a, sub3))
		err = a.RemoveUser(ctx, nodecfg.EntryTag, sub3)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(xrayMissing, sub3)) || !alreadyGone(err, sub3) {
			t.Fatalf("removing a missing client answers %v", err)
		}
		t.Logf("a second remove reads: %v", err)

		// off and on again is the same link
		must(t, Enable(ctx, a, Client{Email: sub3, UUID: uuidA}))
		if got, _ := n.credential(t, sub3); got.uuid != uuidA || got.flow != DefaultFlow {
			t.Fatalf("after off and on Xray holds %+v", got)
		}
	})
}

// An inbound the node doesn't have is a fault the caller must hear about, even
// though its message says "not found" too.
func TestRealAgentMissingInboundIsNotSuccess(t *testing.T) {
	everyAgent(t, func(t *testing.T, n *liveNode, a Agent) {
		ctx := context.Background()
		err := a.RemoveUser(ctx, "nope", sub1)
		if err == nil || alreadyGone(err, sub1) {
			t.Fatalf("removing from a missing inbound answers %v", err)
		}
		t.Logf("a remove from a missing inbound reads: %v", err)
		err = a.AddUser(ctx, "nope", sub3, uuidA, "vless", DefaultFlow, "none", 0)
		if err == nil || alreadyThere(err, sub3) {
			t.Fatalf("adding to a missing inbound answers %v", err)
		}
		t.Logf("an add to a missing inbound reads: %v", err)
	})
}

func TestRealAgentReplace(t *testing.T) {
	everyAgent(t, func(t *testing.T, n *liveNode, a Agent) {
		must(t, Replace(context.Background(), a, Client{Email: sub1, UUID: uuidC}))
		if got, ok := n.credential(t, sub1); !ok || got.uuid != uuidC || got.flow != DefaultFlow {
			t.Fatalf("Xray holds %+v (there: %v) after the replace", got, ok)
		}
		if got, _ := n.credential(t, sub2); got.uuid != uuidB {
			t.Fatalf("another client changed: %+v", got)
		}
	})
}

func TestRealAgentReconcile(t *testing.T) {
	everyAgent(t, func(t *testing.T, n *liveNode, a Agent) {
		ctx := context.Background()
		bridgeBefore, ok := n.credential(t, bridge)
		if !ok || bridgeBefore.reverseTag != bridgeTag {
			t.Fatalf("the bridge's client on the node is %+v", bridgeBefore)
		}

		d, err := Reconcile(ctx, a, []Client{{sub1, uuidA}, {sub3, uuidC}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(d.Added, []string{sub3}) || !slices.Equal(d.Removed, []string{sub2}) ||
			len(d.Replaced) != 0 || !slices.Equal(d.Foreign, []string{bridge}) {
			t.Fatalf("diff = %+v", d)
		}
		if got, want := n.emails(t, a), sorted(sub1, sub3, bridge); !slices.Equal(got, want) {
			t.Fatalf("the entry lists %v, want %v", got, want)
		}
		if got, _ := n.credential(t, sub3); got != (credential{uuid: uuidC, flow: DefaultFlow}) {
			t.Fatalf("the added client is %+v", got)
		}
		if got, _ := n.credential(t, sub1); got.uuid != uuidA {
			t.Fatalf("the right client changed to %+v", got)
		}
		if got, _ := n.credential(t, bridge); got != bridgeBefore {
			t.Fatalf("the bridge's client went from %+v to %+v", bridgeBefore, got)
		}

		// nothing left to do, and then everybody off: the bridge is the only one left
		if d, err := Reconcile(ctx, a, []Client{{sub1, uuidA}, {sub3, uuidC}}); err != nil || len(d.Added)+len(d.Removed)+len(d.Replaced) != 0 {
			t.Fatalf("second pass: %+v, %v", d, err)
		}
		d, err = Reconcile(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := n.emails(t, a), []string{bridge}; !slices.Equal(got, want) {
			t.Fatalf("with nobody wanted the entry lists %v, want %v (diff %+v)", got, want, d)
		}
		if got, _ := n.credential(t, bridge); got != bridgeBefore {
			t.Fatalf("the bridge's client went from %+v to %+v", bridgeBefore, got)
		}
	})
}

// The managed profile is what every node of this plugin runs. The calls this
// package makes, and the ones the usage counter makes, have to be on its
// allow-list, or none of it works on a real node.
func TestTheManagedProfileAllowsTheCallsThePluginMakes(t *testing.T) {
	p := middleware.NewProfileMiddleware(config.ProfileManaged)
	for _, method := range []string{
		pb.NodeAgent_AddUser_FullMethodName,
		pb.NodeAgent_RemoveUser_FullMethodName,
		pb.NodeAgent_ListUsers_FullMethodName,
		pb.NodeAgent_GetStatsBundle_FullMethodName,
		pb.NodeAgent_AckBufferedTraffic_FullMethodName,
	} {
		if err := p.Check(method); err != nil {
			t.Errorf("%s: %v", method, err)
		}
	}
}
