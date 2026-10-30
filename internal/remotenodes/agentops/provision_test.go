// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// a node whose entry answers, an agent that likes the sites it is asked about, and what is needed to provision it
type provisionRig struct {
	*linkRig
	in     ProvisionInput
	mu     sync.Mutex
	config string // what was pushed
	probed []string
}

func newProvisionRig(t *testing.T) *provisionRig {
	t.Helper()
	r := newLinkRig(t)
	r.node.EntryPort = standIn(t)
	p := &provisionRig{linkRig: r}
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			p.mu.Lock()
			p.probed = append(p.probed, req.Dest)
			p.mu.Unlock()
			return &pb.RealityDestProbeResponse{Dest: req.Dest, Checks: []*pb.RealityDestCheck{good(req.ServerNames[0], 4000+len(req.Dest), 25)}}
		}
		a.OnConfig = func(c string) *pb.CommandResponse {
			p.mu.Lock()
			p.config = c
			p.mu.Unlock()
			return &pb.CommandResponse{Success: true}
		}
	})
	p.in = ProvisionInput{
		Node:    r.node,
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(2)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(1), ReverseTag: "r1"},
	}
	return p
}

func (p *provisionRig) provision() (ProvisionResult, error) {
	p.t().Helper()
	return p.env.Provision(context.Background(), p.connect(), p.in, p.progress)
}

func (p *provisionRig) pushed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config
}

func TestProvisionMakesANewNodeAnEntry(t *testing.T) {
	p := newProvisionRig(t)
	res, err := p.provision()
	if err != nil {
		t.Fatal(err)
	}
	// the steps, in the order of the plan, and every one done
	if got, want := p.stepNames(), ProvisionPlan(); !slices.Equal(got, want) {
		t.Fatalf("steps %q, want %q", got, want)
	}
	for _, name := range ProvisionPlan() {
		if last := p.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}

	// what the manager keeps
	keys := RealityKeys{PrivateKey: res.PrivateKey, PublicKey: res.PublicKey, ShortID: res.ShortID}
	if err := keys.Valid(); err != nil {
		t.Fatalf("the keys it made: %v", err)
	}
	if res.HandshakeTarget == "" || res.SNI != strings.Split(res.HandshakeTarget, ":")[0] {
		t.Errorf("target %q, name %q", res.HandshakeTarget, res.SNI)
	}
	if res.AgentVersion != "v1.3.0" || res.XrayVersion != pin.Version {
		t.Errorf("versions agent %q, Xray %q", res.AgentVersion, res.XrayVersion)
	}
	if !res.Check.OK || !res.Check.TLS13 || !res.Check.X25519 {
		t.Errorf("check %+v", res.Check)
	}

	// Xray was put on first, not started, and the node was told the config built from all of it
	if pushes := p.ag.Pushes(); len(pushes) != 1 || pushes[0].Header.RestartAfter || pushes[0].Header.Version != pin.Version {
		t.Errorf("pushes %+v", pushes)
	}
	want, err := nodecfg.Build(nodecfg.Input{
		EntryPort: p.node.EntryPort, RealityDest: res.HandshakeTarget, ServerNames: []string{res.SNI},
		PrivateKey: res.PrivateKey, ShortIDs: []string{res.ShortID}, Clients: p.in.Clients, Bridge: p.in.Bridge,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.pushed() != string(want) {
		t.Error("the config the node was given isn't built from what the result says")
	}
	// the order of what the node was asked
	calls := p.ag.Calls()
	order := []string{"PushXrayBinary", "ProbeRealityDest", "ValidateConfig", "PushConfig"}
	last := -1
	for _, name := range order {
		i := slices.Index(calls, name)
		if i < 0 || i < last {
			t.Fatalf("%s out of order in %v", name, calls)
		}
		last = i
	}
	if d := p.lastStep(StepApplyConfig).Detail; !strings.Contains(d, "Xray "+pin.Version) || !strings.Contains(d, itoa(p.node.EntryPort)) {
		t.Errorf("the last step says %q", d)
	}
}

func TestProvisionLeavesXrayAloneWhenTheNodeHasThePinnedBuild(t *testing.T) {
	p := newProvisionRig(t)
	p.ag.Configure(func(a *fakeagent.Agent) {
		a.XrayLine = "Xray " + pin.Version + " (Xray, Penetrates Everything.) 1b1647c (go1.27.1 linux/amd64)"
	})
	if _, err := p.provision(); err != nil {
		t.Fatal(err)
	}
	if len(p.ag.Pushes()) != 0 || len(p.files.asked) != 0 {
		t.Errorf("sent %d builds, fetched %v", len(p.ag.Pushes()), p.files.asked)
	}
	// a job's steps don't go missing: each one of the plan is done
	for _, name := range ProvisionPlan() {
		if last := p.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
}

func TestProvisionUsesTheTargetItIsToldAndNoOther(t *testing.T) {
	p := newProvisionRig(t)
	p.in.Target = "www.debian.org"
	res, err := p.provision()
	if err != nil {
		t.Fatal(err)
	}
	if res.HandshakeTarget != "www.debian.org:443" || res.SNI != "www.debian.org" {
		t.Errorf("target %q, name %q", res.HandshakeTarget, res.SNI)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Equal(p.probed, []string{"www.debian.org:443"}) {
		t.Errorf("the node was asked about %v", p.probed)
	}
}

func TestProvisionUsesTheNameItIsToldForTheHello(t *testing.T) {
	p := newProvisionRig(t)
	p.in.Target, p.in.SNI = "192.0.2.7:8443", "www.example.com"
	res, err := p.provision()
	if err != nil {
		t.Fatal(err)
	}
	if res.HandshakeTarget != "192.0.2.7:8443" || res.SNI != "www.example.com" {
		t.Errorf("target %q, name %q", res.HandshakeTarget, res.SNI)
	}
	if !strings.Contains(p.pushed(), `"www.example.com"`) || !strings.Contains(p.pushed(), `"192.0.2.7:8443"`) {
		t.Error("the config doesn't carry them")
	}
}

func TestProvisionFromTheCandidatesItIsGiven(t *testing.T) {
	p := newProvisionRig(t)
	p.in.Targets = []string{"b.example", "a.example"}
	res, err := p.provision()
	if err != nil {
		t.Fatal(err)
	}
	// both have the same handshake, the first given wins
	if res.HandshakeTarget != "b.example:443" {
		t.Errorf("target %q", res.HandshakeTarget)
	}
}

func TestProvisionKeepsTheKeysItIsGiven(t *testing.T) {
	p := newProvisionRig(t)
	k, _ := NewRealityKeys()
	p.in.Keys = k
	res, err := p.provision()
	if err != nil {
		t.Fatal(err)
	}
	if res.PrivateKey != k.PrivateKey || res.PublicKey != k.PublicKey || res.ShortID != k.ShortID {
		t.Errorf("the keys changed: %+v", res)
	}
	if !strings.Contains(p.pushed(), k.PrivateKey) {
		t.Error("the config doesn't carry them")
	}
}

// Whatever is wrong with the input is found before the node is asked a thing.
func TestProvisionRefusesABadInputBeforeTouchingTheNode(t *testing.T) {
	k, _ := NewRealityKeys()
	other, _ := NewRealityKeys()
	for name, change := range map[string]func(*ProvisionInput){
		"keys that don't pair": func(in *ProvisionInput) {
			in.Keys = RealityKeys{PrivateKey: k.PrivateKey, PublicKey: other.PublicKey, ShortID: k.ShortID}
		},
		"an entry port that's no port": func(in *ProvisionInput) { in.Node.EntryPort = 0 },
		"a target that's a URL":        func(in *ProvisionInput) { in.Target = "https://www.example.com" },
		"a name that's an address":     func(in *ProvisionInput) { in.SNI = "192.0.2.7" },
		"a target that's an address":   func(in *ProvisionInput) { in.Target = "192.0.2.7:443" },
		"a client with no uuid":        func(in *ProvisionInput) { in.Clients = []nodecfg.Client{{Email: "x@y"}} },
		"a bridge with a reserved tag": func(in *ProvisionInput) { in.Bridge.ReverseTag = "entry" },
		"two clients with one email":   func(in *ProvisionInput) { in.Clients = append(in.Clients, in.Clients[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			p := newProvisionRig(t)
			c := p.connect()
			before := len(p.ag.Calls())
			change(&p.in)
			_, err := p.env.Provision(context.Background(), c, p.in, p.progress)
			var inv *model.InvalidError
			if err == nil || !(errors.As(err, &inv) || errors.Is(err, nodecfg.ErrInvalid)) {
				t.Fatalf("err = %v", err)
			}
			if n := len(p.ag.Calls()) - before; n != 0 {
				t.Errorf("the node was called %d times: %v", n, p.ag.Calls()[before:])
			}
			if len(p.steps) != 0 {
				t.Errorf("steps were reported: %+v", p.steps)
			}
		})
	}
}

func TestProvisionWithATargetThatDoesNotWorkStopsThere(t *testing.T) {
	p := newProvisionRig(t)
	p.in.Target = "www.example.com"
	p.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			return &pb.RealityDestProbeResponse{Checks: []*pb.RealityDestCheck{{ServerName: req.ServerNames[0], Reason: "tls_version", TlsVersion: "TLS 1.2"}}}
		}
	})
	res, err := p.provision()
	var nt *NoTargetError
	if !errors.Is(err, ErrNoTarget) || !errors.As(err, &nt) || len(nt.Results) != 1 {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "The site www.example.com:443 doesn't work") || !strings.Contains(err.Error(), "doesn't use TLS 1.3") {
		t.Errorf("the message: %q", err)
	}
	if last := p.lastStep(StepChooseTarget); last.State != StateFailed || last.Detail != err.Error() {
		t.Errorf("step %+v", last)
	}
	if slices.Contains(p.ag.Calls(), "PushConfig") || res.PrivateKey != "" {
		t.Error("the node was set up with a target that doesn't work")
	}
}

func TestProvisionWithNoTargetThatWorksSaysSo(t *testing.T) {
	p := newProvisionRig(t)
	p.ag.Configure(func(a *fakeagent.Agent) { a.OnProbe = nil }) // fails every name
	_, err := p.provision()
	var nt *NoTargetError
	if !errors.Is(err, ErrNoTarget) || !errors.As(err, &nt) || len(nt.Results) != len(DefaultTargets()) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "None of the 8 sites") {
		t.Errorf("the message: %q", err)
	}
	if slices.Contains(p.ag.Calls(), "PushConfig") {
		t.Error("a config was pushed")
	}
}

func TestProvisionToAnAgentThatCannotTakeXrayStopsAtOnce(t *testing.T) {
	p := newProvisionRig(t)
	p.ag.Configure(func(a *fakeagent.Agent) { a.NoPushXray = true })
	_, err := p.provision()
	if !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("err = %v", err)
	}
	for _, name := range []string{"ProbeRealityDest", "ValidateConfig", "PushConfig"} {
		if slices.Contains(p.ag.Calls(), name) {
			t.Errorf("%s was called", name)
		}
	}
}

// A node whose entry doesn't answer is told with the port, and what was made
// so far comes back, so the manager can try again with the same keys.
func TestProvisionWhenTheEntryDoesNotAnswerKeepsWhatItMade(t *testing.T) {
	p := newProvisionRig(t)
	p.node.EntryPort = closedPort(t)
	p.in.Node.EntryPort = p.node.EntryPort
	res, err := p.provision()
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "TCP port "+itoa(p.node.EntryPort)) {
		t.Fatalf("err = %v", err)
	}
	keys := RealityKeys{PrivateKey: res.PrivateKey, PublicKey: res.PublicKey, ShortID: res.ShortID}
	if res.HandshakeTarget == "" || keys.Valid() != nil {
		t.Errorf("result %+v", res)
	}
	if last := p.lastStep(StepApplyConfig); last.State != StateFailed {
		t.Errorf("step %+v", last)
	}
}
