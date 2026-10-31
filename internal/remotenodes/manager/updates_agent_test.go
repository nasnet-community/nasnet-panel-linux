// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// The updates of an agent and of Xray go through agentops's real calls over
// real mutual TLS, to agentops's stand-in agent. The real agent can't take
// either in a test: an update of its own binary replaces the program that runs,
// and a pushed Xray has to be an ELF file for the host that then runs. The
// stand-in takes what it is sent and reports the version it was sent from then
// on, and a test tells it to refuse.

// buildFiles hands out agent and Xray builds of its own.
type buildFiles struct {
	agentVersion string
	agentPlain   []byte
	xrayPlain    []byte
	sig          []byte
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	must(t, w.Close())
	return out.Bytes()
}

type stubFiles struct {
	t *testing.T
	buildFiles
}

func (f *stubFiles) Agent(_ context.Context, arch string) (*artifacts.Agent, error) {
	gz := gzipped(f.t, f.agentPlain)
	return &artifacts.Agent{
		Version: f.agentVersion, Arch: arch, Gz: gz, GzSHA256: sha(gz), PlainSize: int64(len(f.agentPlain)), PlainSHA256: sha(f.agentPlain), Sig: f.sig,
	}, nil
}

func (f *stubFiles) Xray(_ context.Context, arch string) (*artifacts.Xray, error) {
	return &artifacts.Xray{Version: pin.Version, Arch: arch, Gz: gzipped(f.t, f.xrayPlain), PlainSize: int64(len(f.xrayPlain)), PlainSHA256: sha(f.xrayPlain)}, nil
}

// agentWorld is a manager whose operations are agentops's, a stand-in agent
// behind real TLS from the manager's CA, and a node that is that agent.
type agentWorld struct {
	t     *testing.T
	m     *Manager
	st    Store
	jobs  *jobs.Engine
	fa    *fakeagent.Agent
	files *stubFiles
	node  model.Node
}

func newAgentWorld(t *testing.T) *agentWorld {
	t.Helper()
	cfg, _ := baseConfig(t)
	w := &agentWorld{t: t, st: cfg.Store}
	w.files = &stubFiles{t: t, buildFiles: buildFiles{
		agentVersion: "v1.3.1", agentPlain: []byte("#!/bin/sh\necho an agent\n"), xrayPlain: append([]byte("\x7fELF"), make([]byte, 4096)...), sig: make([]byte, 64),
	}}
	w.jobs = jobs.New(jobs.Config{Now: cfg.Now, Log: cfg.Log, StepKey: guardKeys(t)})
	var m *Manager
	cfg.Jobs = w.jobs
	// the first look at the node's stats fails, since the stand-in has none, so the loop is told to wait: what a test needs is the link it holds
	cfg.PollEvery, cfg.StateEvery = time.Hour, time.Hour
	cfg.Connector = ConnectorFunc(func(ctx context.Context, n model.Node) (Client, error) {
		ops, err := m.Ops()
		if err != nil {
			return nil, err
		}
		return ops.Connect(ctx, n)
	})
	cfg.NewOps = func(ca *pki.CA) Ops {
		return EnvOps(&agentops.Env{
			Local: netip.MustParseAddr("127.0.0.1"), CA: ca, Files: w.files, DialTimeout: 3 * time.Second, Settle: 5 * time.Second, Log: cfg.Log,
		})
	}
	var err error
	m, err = New(cfg)
	must(t, err)
	w.m = m

	w.fa = fakeagent.Start(t, m.CA(), "127.0.0.1")
	w.fa.Configure(func(a *fakeagent.Agent) {
		a.AgentVersion, a.XrayLine = "v1.3.0", "Xray 26.10.10 (Xray, Penetrates Everything.) fake (go1.27 linux/amd64)"
	})
	w.node = addNode(t, w.st, func(n *model.Node) {
		n.Host, n.AgentPort, n.Arch, n.AgentVersion, n.XrayVersion = "127.0.0.1", w.fa.Port(), "amd64", "1.3.0", "26.10.10"
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = w.jobs.Close(cctx)
		cancel()
		<-done
		_ = m.Close(cctx)
	})
	// the loop holds a link to the node
	waitFor(t, "the node's link never came up", func() bool { _, ok := m.Client(w.node.ID); return ok })
	return w
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("after 15 s: %s", what)
		}
	}
}

// update runs an update to its end.
func (w *agentWorld) update(kind string) jobs.Job {
	w.t.Helper()
	id, err := w.m.UpdateNode(context.Background(), w.node.ID, kind)
	must(w.t, err)
	var j jobs.Job
	waitFor(w.t, "the update never ended", func() bool {
		var ok bool
		j, ok = w.jobs.Get(id)
		return ok && j.State != jobs.StateRunning
	})
	return j
}

func (w *agentWorld) saved() model.Node { return nodeOf(w.st, w.node.ID) }

func TestUpdatingTheAgentThroughTheRealCallsToAStandInAgent(t *testing.T) {
	w := newAgentWorld(t)
	j := w.update("agent")
	if j.State != jobs.StateOK || j.Kind != jobs.KindUpdateAgent || !slices.Equal(stepNames(j), agentops.UpdateAgentPlan()) {
		t.Fatalf("job = %+v", j)
	}
	// what the agent was sent: the gzipped build with the plain build's checksum and signature, and never forced
	ups := w.fa.Updates()
	if len(ups) != 1 {
		t.Fatalf("%d updates", len(ups))
	}
	u := ups[0]
	gz := gzipped(t, w.files.agentPlain)
	if u.Version != "v1.3.1" || u.Checksum != sha(w.files.agentPlain) || !bytes.Equal(u.Signature, w.files.sig) || !u.RestartAfter || u.Force || len(u.BinaryContent) != len(gz) {
		t.Errorf("the update was %q, checksum %s, signature %x, restart %v, force %v, %d bytes", u.Version, u.Checksum, u.Signature, u.RestartAfter, u.Force, len(u.BinaryContent))
	}
	// the state, the page and the event all say 1.3.1, and say it once
	if got := w.saved().AgentVersion; got != "1.3.1" {
		t.Errorf("the state has agent %q", got)
	}
	ev := w.m.NodeEvents(w.node.ID)
	if len(ev) != 1 || ev[0].Kind != EventAgentUpdated || ev[0].Detail != "1.3.0 to 1.3.1" {
		t.Errorf("events = %+v", ev)
	}
	if live := w.m.NodeLive(w.node.ID); live.Agent != nil && live.Agent.Version != "1.3.1" {
		t.Errorf("agent state = %+v", live.Agent)
	}

	// an agent that refuses, as one does that would go back, leaves the node as it is
	w.fa.Configure(func(a *fakeagent.Agent) {
		a.OnSelfUpdate = func(*pb.UpdateRequest) *pb.UpdateResponse {
			return &pb.UpdateResponse{Success: false, Message: "refusing to go back to an older version"}
		}
	})
	w.files.agentVersion = "v1.3.0"
	j = w.update("agent")
	if j.State != jobs.StateFailed || !strings.Contains(j.Error, "The agent refused the update") || !strings.Contains(j.Error, "Refusing to go back to an older version.") {
		t.Errorf("job = %q %v", j.Error, j.Steps)
	}
	if got := w.saved().AgentVersion; got != "1.3.1" || len(w.m.NodeEvents(w.node.ID)) != 1 {
		t.Errorf("agent %q, events %v", got, kinds(w.m.NodeEvents(w.node.ID)))
	}
}

func TestUpdatingXrayThroughTheRealCallsToAStandInAgent(t *testing.T) {
	w := newAgentWorld(t)
	j := w.update("xray")
	if j.State != jobs.StateOK || j.Kind != jobs.KindUpdateXray || !slices.Equal(stepNames(j), agentops.UpdateXrayPlan()) {
		t.Fatalf("job = %+v", j)
	}
	// the build was sent whole, gzipped, with the checksum of what is in it, and Xray was told to start afterwards
	pushes := w.fa.Pushes()
	if len(pushes) != 1 {
		t.Fatalf("%d pushes", len(pushes))
	}
	p := pushes[0]
	if p.Header.Version != pin.Version || p.Header.Sha256 != sha(w.files.xrayPlain) || !p.Header.RestartAfter || !p.Header.Gzip || !bytes.Equal(p.Build, w.files.xrayPlain) {
		t.Errorf("push = %+v", p.Header)
	}
	if got := w.saved().XrayVersion; got != pin.Version {
		t.Errorf("the state has Xray %q", got)
	}
	var changed []string
	for _, e := range w.m.NodeEvents(w.node.ID) {
		if e.Kind == EventXrayChanged {
			changed = append(changed, e.Detail)
		}
	}
	if !slices.Equal(changed, []string{"26.10.10 to " + pin.Version}) {
		t.Errorf("xray_changed events = %v", changed)
	}
	if u := w.m.NodeLive(w.node.ID).Updates; u.Xray {
		t.Errorf("updates = %+v", u)
	}

	// an agent too old to take a build says to update the agent first, and the node is as it was
	w.fa.Configure(func(a *fakeagent.Agent) {
		a.NoPushXray = true
		a.XrayLine = "Xray 26.10.10 (Xray, Penetrates Everything.) fake (go1.27 linux/amd64)"
	})
	editNode(t, w.st, w.node.ID, func(n *model.Node) { n.XrayVersion = "26.10.10" })
	j = w.update("xray")
	if j.State != jobs.StateFailed || !strings.Contains(j.Error, "too old to take an Xray build. Update the agent first.") {
		t.Errorf("job = %q %v", j.Error, j.Steps)
	}
	if got := w.saved().XrayVersion; got != "26.10.10" || len(w.fa.Pushes()) != 1 {
		t.Errorf("state has Xray %q, %d pushes", got, len(w.fa.Pushes()))
	}
}
