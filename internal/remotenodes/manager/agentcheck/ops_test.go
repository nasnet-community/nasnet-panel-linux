// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// These tests drive the node operations (adding a node, changing its settings,
// rotating its keys, checking a target, renewing its certificate, taking it out)
// through the manager, the job engine and agentops's real Env, against the real
// agent: its own server, started as cmd/node-agent starts it, with mutual TLS
// from the manager's CA and the managed profile.
//
// What can't run for real is stood in for, and only that:
//
//   - The install and the removal go over SSH, to a server, and there is none
//     here: agentops tests them against a server in a sandbox. The stand-in does
//     what the install leaves, which is an agent that runs, and hands the manager
//     the certificate that agent was started with.
//   - The agent insists that an Xray build it is pushed is an ELF file for the
//     host, so the node's Xray is a script that reports the pinned version and
//     the push is skipped, as it is for a node that has the build. Pushing a
//     build is tested against agentops's stand-in agent, in package manager.
//   - Xray is that script, which checks a config and runs until it is stopped.
//     What the agent does with a config is real: it validates it with the script,
//     writes it, and restarts. The config on the node is read back from its file.

// xrayScript is a node's Xray: it reports the version it is given, passes any
// config check, and runs until it is stopped.
func xrayScript(version string) string {
	return `#!/bin/sh
case "$1" in
version) echo "Xray ` + version + ` (Xray, Penetrates Everything.) test (go1.27 linux/amd64)" ;;
run) exec sleep 120 ;;
esac
`
}

// standIn is a port that answers, as the entry of a running Xray would.
func standIn(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't open a loopback port here: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// closedPort is a loopback port that nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't open a loopback port here: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// tlsTarget serves cfg on a loopback port and gives localhost:port as the
// Reality target it is, so that the hello carries a name.
func tlsTarget(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("can't open a loopback port here: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				tls.Server(c, cfg).Handshake()
			}()
		}
	}()
	return fmt.Sprintf("localhost:%d", l.Addr().(*net.TCPAddr).Port)
}

func selfSigned(t *testing.T, name string) tls.Certificate {
	t.Helper()
	var key crypto.Signer
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	must(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// goodTarget is a site the real agent finds usable: TLS 1.3, X25519, a small handshake.
func goodTarget(t *testing.T) string {
	t.Helper()
	return tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, "good.example.test")}, MinVersion: tls.VersionTLS13})
}

// oldTarget answers with TLS 1.2 only, which won't do.
func oldTarget(t *testing.T) string {
	t.Helper()
	return tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, "old.example.test")}, MaxVersion: tls.VersionTLS12})
}

// sshStandIn is the operations of the hub with the two that go over SSH
// replaced, and the choice of Reality targets pointed at sites this machine
// can reach. Everything else is agentops's, against the real agent.
type sshStandIn struct {
	manager.Ops
	cert    *pki.Issued // what the agent runs with, which an install leaves
	targets []string

	mu         sync.Mutex
	probes     []string
	installs   []agentops.Login
	uninstalls []agentops.Login
	removedAs  []model.Node // the node as the removal was given it
}

func (s *sshStandIn) Probe(_ context.Context, host string, port int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes = append(s.probes, fmt.Sprintf("%s:%d", host, port))
	return "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq", nil
}

func steps(progress agentops.Progress, plan []string) {
	for _, name := range plan {
		progress(agentops.Step{Name: name, State: agentops.StateRunning})
		progress(agentops.Step{Name: name, State: agentops.StateOK})
	}
}

func (s *sshStandIn) InstallAgent(_ context.Context, node model.Node, login agentops.Login, opts agentops.InstallOptions, progress agentops.Progress) (agentops.InstallResult, error) {
	s.mu.Lock()
	s.installs = append(s.installs, login)
	s.mu.Unlock()
	steps(progress, agentops.InstallPlanFor(opts))
	return agentops.InstallResult{
		Changed: true, Arch: "amd64", AgentVersion: server.Version,
		CertSerial: s.cert.Serial, CertNotBefore: s.cert.NotBefore, CertNotAfter: s.cert.NotAfter,
	}, nil
}

func (s *sshStandIn) Uninstall(_ context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error {
	s.mu.Lock()
	s.uninstalls = append(s.uninstalls, login)
	s.removedAs = append(s.removedAs, node)
	s.mu.Unlock()
	steps(progress, agentops.UninstallPlan())
	return nil
}

func (s *sshStandIn) Provision(ctx context.Context, c manager.Client, in agentops.ProvisionInput, progress agentops.Progress) (agentops.ProvisionResult, error) {
	in.Targets = s.targets
	return s.Ops.Provision(ctx, c, in, progress)
}

// opsWorld is a manager running the hub's operations over the real Env, a real
// agent for the node it will add, and everything a test looks at.
type opsWorld struct {
	t      *testing.T
	mgr    *manager.Manager
	st     *store.Store
	engine *jobs.Engine
	log    *logBuf
	ssh    *sshStandIn
	cfg    *config.Config // the agent's, and with it where its files are
	entry  int            // the entry port the node is set up with, which answers
	port   int            // the agent's
}

func startOpsWorld(t *testing.T) *opsWorld {
	t.Helper()
	skipIfXrayRuns(t)
	quiet(t)
	w := &opsWorld{t: t, log: &logBuf{}}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("what the hub logged:\n%s", w.log)
		}
	})
	logger := slog.New(slog.NewTextHandler(w.log, nil))

	var err error
	if w.st, err = store.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	coll, err := usage.Open(usage.Config{Logf: func(format string, args ...any) { fmt.Fprintf(w.log, "usage: "+format+"\n", args...) }})
	must(t, err)
	w.engine = jobs.New(jobs.Config{Log: logger})
	w.ssh = &sshStandIn{}

	var mgr *manager.Manager
	mgr, err = manager.New(manager.Config{
		Store: w.st, Usage: coll, Jobs: w.engine, Log: logger,
		PollEvery: 100 * time.Millisecond, StateEvery: 200 * time.Millisecond,
		Connector: manager.ConnectorFunc(func(ctx context.Context, n model.Node) (manager.Client, error) {
			ops, err := mgr.Ops()
			if err != nil {
				return nil, err
			}
			return ops.Connect(ctx, n)
		}),
		NewOps: func(ca *pki.CA) manager.Ops {
			w.ssh.Ops = manager.EnvOps(&agentops.Env{
				Local: netip.MustParseAddr("127.0.0.1"), CA: ca, DialTimeout: 3 * time.Second, Settle: 2 * time.Second, Log: logger,
				// a renewal ends later than the certificate the agent started with, and is already valid
				Now: func() time.Time { return time.Now().Add(3 * time.Second) },
			})
			return w.ssh
		},
	})
	must(t, err)
	w.mgr = mgr
	ca := mgr.CA()
	if ca == nil {
		t.Fatalf("the manager has no CA: %v", mgr.CAError())
	}

	// the agent an install leaves: node 1's certificate, and the Xray that reports the pin
	node, err := ca.IssueNodeCert(1, "check", "127.0.0.1", time.Now())
	must(t, err)
	w.ssh.cert = node
	dir := t.TempDir()
	write := func(name string, b []byte, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, b, mode))
		return p
	}
	w.cfg = config.DefaultConfig()
	w.cfg.Profile = config.ProfileManaged
	w.cfg.TLS = config.TLSConfig{
		Enabled:    true,
		CACert:     write("ca.crt", ca.CertPEM(), 0o600),
		ServerCert: write("server.crt", node.CertPEM, 0o600),
		ServerKey:  write("server.key", node.KeyPEM, 0o600),
	}
	// nothing outside the temp folder
	w.cfg.Process.ServiceMode = false
	w.cfg.Process.WatchdogEnabled = false
	w.cfg.Traffic.BufferEnabled = false
	w.cfg.Xray.AccessLogPath = ""
	w.cfg.Xray.APITimeout = 1 // there is no Xray API here, and the agent waits this long to find that out
	w.cfg.Xray.BinaryPath = write("xray", []byte(xrayScript(pin.Version)), 0o755)
	w.cfg.Xray.ConfigPath = filepath.Join(dir, "xray-conf", "config.json")
	w.cfg.Cores.DataDir = filepath.Join(dir, "cores")
	agentPort := 0
	for attempt := 0; agentPort == 0; attempt++ {
		if attempt == 5 {
			t.Fatal("no free port to start the agent on")
		}
		agentPort = tryStart(t, w.cfg)
	}
	w.port, w.entry = agentPort, standIn(t)
	must(t, w.st.Update(func(s *model.State) error {
		s.Settings = model.Settings{AgentPort: agentPort, EntryPort: w.entry}
		return nil
	}))

	// Xray is started through the agent first, so that when a push restarts it
	// the agent stops the one it started and not whichever other process is
	// called xray
	must(t, os.MkdirAll(filepath.Dir(w.cfg.Xray.ConfigPath), 0o755))
	must(t, os.WriteFile(w.cfg.Xray.ConfigPath, []byte("{}"), 0o644))
	ops, err := mgr.Ops()
	must(t, err)
	c, err := ops.Connect(context.Background(), model.Node{ID: 1, Name: "check", Host: "127.0.0.1", AgentPort: agentPort})
	must(t, err)
	must(t, c.(*agent.Client).StartXray(context.Background()))
	c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := w.engine.Close(cctx); err != nil {
			t.Errorf("closing the job engine: %v", err)
		}
		cancel()
		<-done
		if err := mgr.Close(cctx); err != nil {
			t.Errorf("closing the manager: %v", err)
		}
	})
	return w
}

// job is a job once it has ended.
func (w *opsWorld) job(id string) jobs.Job {
	w.t.Helper()
	var j jobs.Job
	eventually(w.t, "job "+id+" never ended", func() bool {
		var ok bool
		j, ok = w.engine.Get(id)
		return ok && j.State != jobs.StateRunning
	})
	return j
}

func (w *opsWorld) ok(id string) jobs.Job {
	w.t.Helper()
	j := w.job(id)
	if j.State != jobs.StateOK {
		w.t.Fatalf("job %s ended %s: %s\nsteps %+v", id, j.State, j.Error, j.Steps)
	}
	return j
}

func (w *opsWorld) failed(id string) jobs.Job {
	w.t.Helper()
	j := w.job(id)
	if j.State != jobs.StateFailed {
		w.t.Fatalf("job %s ended %s, want it failed", id, j.State)
	}
	return j
}

func (w *opsWorld) node() model.Node {
	w.t.Helper()
	st := w.st.Read()
	if len(st.Nodes) != 1 {
		w.t.Fatalf("%d nodes", len(st.Nodes))
	}
	return st.Nodes[0]
}

// onTheNode is the config file the agent holds.
func (w *opsWorld) onTheNode() string {
	w.t.Helper()
	b, err := os.ReadFile(w.cfg.Xray.ConfigPath)
	must(w.t, err)
	return string(b)
}

// wanted is the config the state says the node should run: the one the
// operations build, which is what the agent has to hold after each of them.
func (w *opsWorld) wanted() string {
	w.t.Helper()
	st := w.st.Read()
	n := st.Nodes[0]
	var clients []nodecfg.Client
	for _, e := range st.Enablements {
		if e.NodeID == n.ID && e.Enabled {
			clients = append(clients, nodecfg.Client{UUID: e.ClientUUID, Email: nodecfg.SubscriptionEmail(int(e.SubscriptionID))})
		}
	}
	sni := n.RealitySNI
	cfg, err := nodecfg.Build(nodecfg.Input{
		EntryPort: n.EntryPort, RealityDest: n.RealityTarget, ServerNames: []string{sni},
		PrivateKey: n.RealityPrivateKey, ShortIDs: []string{n.RealityShortID}, Clients: clients,
		Bridge: nodecfg.Bridge{UUID: n.BridgeUUID, Email: nodecfg.BridgeEmail(int(n.ID)), ReverseTag: nodecfg.BridgeReverseTag(int(n.ID))},
	})
	must(w.t, err)
	return string(cfg)
}

// addTheNode adds the node through the manager, with the sites the real agent
// may choose from, and waits for the job.
func (w *opsWorld) addTheNode(targets ...string) jobs.Job {
	w.t.Helper()
	w.ssh.targets = targets
	id, err := w.mgr.AddNode(context.Background(), api.InstallRequest{
		Login: api.Login{SSHPort: 22, SSHUser: "root", Password: "MARKER-hunter2", HostKey: "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"},
		Name:  "Frankfurt", Host: "127.0.0.1",
	})
	must(w.t, err)
	return w.ok(id)
}

// A node is added with the real agent doing everything after the install: its
// link, the check of the targets, the keys, the config and the entry port. What
// the manager saves is what the agent was left with, the agent holds the config
// the state makes, the node is then watched, and taking it out forgets it.
func TestAddingAndRemovingANodeThroughTheRealAgent(t *testing.T) {
	w := startOpsWorld(t)
	good := goodTarget(t)

	j := w.addTheNode(oldTarget(t), good)
	if len(j.Steps) != 15 {
		t.Errorf("%d steps: %+v", len(j.Steps), j.Steps)
	}
	for _, s := range j.Steps {
		if s.State != jobs.StepOK {
			t.Errorf("step %+v", s)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", j), "hunter2") {
		t.Errorf("the login shows in the job: %+v", j)
	}

	// what the manager saved of what the agent said and chose
	n := w.node()
	if n.State != model.NodeRegistered || n.Arch != "amd64" || n.AgentVersion != server.Version || n.XrayVersion != pin.Version {
		t.Errorf("node = %+v", n)
	}
	if n.RealityTarget != good || n.RealitySNI != "localhost" || n.EntryPort != w.entry || n.AgentPort != w.port {
		t.Errorf("target %q (want %q), name %q, entry %d (want %d), agent port %d (want %d)", n.RealityTarget, good, n.RealitySNI, n.EntryPort, w.entry, n.AgentPort, w.port)
	}
	keys := agentops.RealityKeys{PrivateKey: n.RealityPrivateKey, PublicKey: n.RealityPublicKey, ShortID: n.RealityShortID}
	if err := keys.Valid(); err != nil {
		t.Errorf("keys: %v", err)
	}
	if !n.CertNotAfter.Equal(w.ssh.cert.NotAfter) || n.SSHHostKey == "" || n.BridgeUUID == "" {
		t.Errorf("certificate to %v, host key %q, bridge %q", n.CertNotAfter, n.SSHHostKey, n.BridgeUUID)
	}
	if len(w.ssh.installs) != 1 || w.ssh.installs[0].Password != "MARKER-hunter2" {
		t.Errorf("the install was given %+v", w.ssh.installs)
	}

	// the agent holds exactly the config the state makes, and runs Xray on it
	if got, want := w.onTheNode(), w.wanted(); got != want {
		t.Errorf("the config on the node isn't the one the state makes (%d bytes, want %d)", len(got), len(want))
	}
	if !strings.Contains(w.onTheNode(), `"port": `+fmt.Sprint(w.entry)) {
		t.Error("the config doesn't have the entry port")
	}

	// the loop has the node, over its own link; the agent's version comes in a
	// call of its own after the first stats, so it is waited for too
	eventually(t, "the node is never watched", func() bool {
		l := w.mgr.NodeLive(n.ID)
		_, linked := w.mgr.Client(n.ID)
		return l.Agent != nil && l.Agent.Xray.Running && l.Agent.Version != "" && l.Agent.XrayVersion != "" && l.Usage.Live && linked
	})
	if l := w.mgr.NodeLive(n.ID); l.Agent.Version != server.Version || l.Agent.XrayVersion != pin.Version || l.Installing != nil {
		t.Errorf("live = %+v, agent %+v", l, l.Agent)
	}
	if got := w.mgr.NodeEvents(n.ID); len(got) != 0 {
		t.Errorf("events = %+v: the node is what the install saved", got)
	}

	// taking it out, with a login: the stand-in server gets it, and the hub forgets the node
	id, err := w.mgr.RemoveNode(context.Background(), n.ID, api.RemoveRequest{Login: &api.Login{SSHUser: "admin", Password: "MARKER-hunter2", HostKey: "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"}})
	must(t, err)
	rm := w.ok(id)
	if rm.Target != "Frankfurt" || rm.NodeID != n.ID || len(w.ssh.uninstalls) != 1 || w.ssh.removedAs[0].SSHUser != "admin" || w.ssh.removedAs[0].Host != "127.0.0.1" {
		t.Errorf("removal %+v, uninstalls %v, as %+v", rm, w.ssh.uninstalls, w.ssh.removedAs)
	}
	if len(w.st.Read().Nodes) != 0 {
		t.Error("the node is still in the state")
	}
	eventually(t, "the node's link was never let go", func() bool {
		_, linked := w.mgr.Client(n.ID)
		return !linked && !w.mgr.NodeLive(n.ID).Usage.Live
	})
}

// An install whose targets don't work stops at the step that chooses one, with
// the agent's answer in a sentence, and the node it leaves is one that never
// finished, which force forgets.
func TestAnInstallWhoseTargetsDoNotWorkStopsThereAndTheNodeCanBeForgotten(t *testing.T) {
	w := startOpsWorld(t)
	w.ssh.targets = []string{oldTarget(t), fmt.Sprintf("localhost:%d", closedPort(t))}
	id, err := w.mgr.AddNode(context.Background(), api.InstallRequest{
		Login: api.Login{Password: "pw", HostKey: "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"}, Name: "Frankfurt", Host: "127.0.0.1",
	})
	must(t, err)
	j := w.failed(id)
	if !strings.HasPrefix(j.Error, "None of the 2 sites tried works as a Reality target from this server") {
		t.Errorf("job error %q", j.Error)
	}
	states := map[string]jobs.StepState{}
	for _, s := range j.Steps {
		states[s.Name] = s.State
	}
	if states[agentops.StepChooseTarget] != jobs.StepFailed || states[agentops.StepCheckXray] != jobs.StepOK || states[agentops.StepCreateKeys] != jobs.StepPending {
		t.Errorf("steps %+v", j.Steps)
	}
	if n := w.node(); n.State != model.NodeInstalling || n.RealityPrivateKey != "" || n.RealityTarget != "" {
		t.Errorf("node = %+v", n)
	}
	if got := w.onTheNode(); got != "{}" {
		t.Errorf("the agent was given a config: %.80s", got)
	}
	if _, ok := w.mgr.Client(1); ok {
		t.Error("a node that never finished is being watched")
	}

	id, err = w.mgr.RemoveNode(context.Background(), 1, api.RemoveRequest{Force: true})
	must(t, err)
	w.ok(id)
	if len(w.st.Read().Nodes) != 0 || len(w.ssh.uninstalls) != 0 {
		t.Errorf("nodes %v, uninstalls %v", w.st.Read().Nodes, w.ssh.uninstalls)
	}
}

// The settings, the keys and the targets, against the agent that does them for
// real: what it is pushed is what the state makes, and a change it can't carry
// is taken back so that the node runs what it did.
func TestChangingSettingsAndKeysThroughTheRealAgent(t *testing.T) {
	w := startOpsWorld(t)
	good, other := goodTarget(t), goodTarget(t)
	w.addTheNode(good)
	n := w.node()
	eventually(t, "the node is never watched", func() bool { _, ok := w.mgr.Client(n.ID); return ok })
	ctx := context.Background()

	// the node itself says what it makes of a target
	old := oldTarget(t)
	for name, c := range map[string]struct {
		target string
		ok     bool
		say    string
	}{
		"a good site":            {other, true, ""},
		"one with only TLS 1.2":  {old, false, "doesn't use TLS 1.3"},
		"one nothing listens on": {fmt.Sprintf("localhost:%d", closedPort(t)), false, "connect"},
	} {
		got, err := w.mgr.CheckHandshake(ctx, n.ID, c.target)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.OK != c.ok || (c.ok && (!got.TLS13 || !got.X25519 || got.HandshakeBytes <= 0)) || (c.say != "" && !strings.Contains(got.Error, c.say)) {
			t.Errorf("%s: %+v", name, got)
		}
	}

	// a new site and a new entry port, which answers
	entry2 := standIn(t)
	id, err := w.mgr.ChangeSettings(ctx, n.ID, api.SettingsRequest{HandshakeTarget: &other, EntryPort: &entry2})
	must(t, err)
	w.ok(id)
	if got := w.node(); got.RealityTarget != other || got.EntryPort != entry2 || got.RealitySNI != "localhost" {
		t.Errorf("node = %+v", got)
	}
	if w.onTheNode() != w.wanted() || !strings.Contains(w.onTheNode(), fmt.Sprint(entry2)) {
		t.Error("the agent doesn't hold the config the new settings make")
	}

	// a site the node can't use stops the change before the node is touched
	before := w.onTheNode()
	id, err = w.mgr.ChangeSettings(ctx, n.ID, api.SettingsRequest{HandshakeTarget: &old})
	must(t, err)
	j := w.failed(id)
	if !strings.Contains(j.Error, "doesn't work as a Reality target") || w.onTheNode() != before || w.node().RealityTarget != other {
		t.Errorf("job error %q, config changed %v, target %q", j.Error, w.onTheNode() != before, w.node().RealityTarget)
	}

	// an entry port that nothing answers on: the agent takes the config, the
	// check from the router fails, and the old config is put back
	shut := closedPort(t)
	id, err = w.mgr.ChangeSettings(ctx, n.ID, api.SettingsRequest{EntryPort: &shut})
	must(t, err)
	j = w.failed(id)
	// the agent's Xray has no API here to say that it knows the entry, so it is Xray that is blamed, as agentops's own test says
	if !strings.Contains(j.Error, fmt.Sprintf("port %d", shut)) || !strings.HasSuffix(j.Error, "The node is back on its previous settings.") {
		t.Errorf("job error %q", j.Error)
	}
	if got := w.onTheNode(); got != before || got != w.wanted() || w.node().EntryPort != entry2 {
		t.Errorf("the old config was not put back: node holds the new one %v, state port %d", strings.Contains(got, fmt.Sprint(shut)), w.node().EntryPort)
	}
	if st, err := w.status(n.ID); err != nil || !st.XrayRunning {
		t.Errorf("Xray after the change that was taken back: %+v, %v", st, err)
	}

	// new keys, through the real agent: the file, the state and the bridge's view all agree
	keys0 := w.node()
	id, err = w.mgr.RotateKeys(ctx, n.ID)
	must(t, err)
	w.ok(id)
	got := w.node()
	if got.RealityPrivateKey == keys0.RealityPrivateKey || got.RealityPublicKey == keys0.RealityPublicKey || got.RealityShortID == keys0.RealityShortID {
		t.Error("a value was kept that should have changed")
	}
	if err := (agentops.RealityKeys{PrivateKey: got.RealityPrivateKey, PublicKey: got.RealityPublicKey, ShortID: got.RealityShortID}).Valid(); err != nil {
		t.Errorf("the new keys: %v", err)
	}
	if w.onTheNode() != w.wanted() || !strings.Contains(w.onTheNode(), got.RealityPrivateKey) || strings.Contains(w.onTheNode(), keys0.RealityPrivateKey) {
		t.Error("the agent doesn't hold the config the new keys make")
	}
	if ev := w.mgr.NodeEvents(n.ID); len(ev) == 0 || ev[0].Kind != manager.EventKeysRotated {
		t.Errorf("events = %+v", ev)
	}
	if strings.Contains(w.log.String(), got.RealityPrivateKey) || strings.Contains(w.log.String(), keys0.RealityPrivateKey) {
		t.Errorf("a REALITY key is in the log:\n%s", w.log)
	}
}

// status is the node's own report, over the link the manager holds.
func (w *opsWorld) status(id uint) (*pb.NodeStatus, error) {
	c, ok := w.mgr.Client(id)
	if !ok {
		return nil, fmt.Errorf("no link to node %d", id)
	}
	return c.(*agent.Client).GetStatus(context.Background())
}

// A certificate is renewed by the real agent, which installs it and shows it on
// a new link; the manager saves its dates and says so, and the old link, which
// the loop holds, goes on working.
func TestRenewingACertificateThroughTheRealAgent(t *testing.T) {
	w := startOpsWorld(t)
	w.addTheNode(goodTarget(t))
	n := w.node()
	eventually(t, "the node is never watched", func() bool { _, ok := w.mgr.Client(n.ID); return ok })

	pair := func() *x509.Certificate {
		p, err := tls.LoadX509KeyPair(w.cfg.TLS.ServerCert, w.cfg.TLS.ServerKey)
		must(t, err)
		leaf, err := x509.ParseCertificate(p.Certificate[0])
		must(t, err)
		return leaf
	}
	old := pair()

	id, err := w.mgr.RenewCert(context.Background(), n.ID)
	must(t, err)
	j := w.ok(id)
	if got := []string{j.Steps[0].Name, j.Steps[1].Name}; !slices.Equal(got, []string{manager.StepRenewCert, manager.StepSaveNode}) {
		t.Errorf("steps %v", got)
	}
	now := pair()
	if now.SerialNumber.Cmp(old.SerialNumber) == 0 || !now.NotAfter.After(old.NotAfter) || !strings.HasPrefix(now.Subject.CommonName, "node-1-") {
		t.Errorf("the agent's certificate: serial %v (was %v), ends %v (was %v), CN %q", now.SerialNumber, old.SerialNumber, now.NotAfter, old.NotAfter, now.Subject.CommonName)
	}
	if got := w.node(); !got.CertNotAfter.Equal(now.NotAfter.UTC()) || !got.CertNotBefore.Equal(now.NotBefore.UTC()) {
		t.Errorf("the state has %v to %v, the agent %v to %v", got.CertNotBefore, got.CertNotAfter, now.NotBefore, now.NotAfter)
	}
	if ev := w.mgr.NodeEvents(n.ID); len(ev) != 1 || ev[0].Kind != manager.EventCertRenewed {
		t.Errorf("events = %+v", ev)
	}
	// the loop's own link carries on, and a new one is made with the new certificate
	if _, err := w.status(n.ID); err != nil {
		t.Errorf("the loop's link after the renewal: %v", err)
	}
	w.mgr.Reconnect(n.ID)
	eventually(t, "no link was made after the renewal", func() bool {
		_, err := w.status(n.ID)
		l := w.mgr.NodeLive(n.ID)
		return err == nil && l.Agent != nil
	})
}
