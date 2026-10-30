// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

const loopback = "127.0.0.1"

// linkRig is a hub Env and a stand-in agent that trusts its CA.
type linkRig struct {
	tt    *testing.T
	env   *Env
	ca    *pki.CA
	node  model.Node
	ag    *fakeagent.Agent
	files *fakeFiles
	steps []Step
}

func (r *linkRig) progress(s Step) { r.steps = append(r.steps, s) }

// stepNames is the steps that began, in order, once each.
func (r *linkRig) stepNames() []string {
	var names []string
	for _, s := range r.steps {
		if s.State == StateRunning && (len(names) == 0 || names[len(names)-1] != s.Name) {
			names = append(names, s.Name)
		}
	}
	return names
}

func (r *linkRig) lastStep(name string) Step {
	var last Step
	for _, s := range r.steps {
		if s.Name == name {
			last = s
		}
	}
	return last
}

// connect is what the manager does to get its client.
func (r *linkRig) connect() *agent.Client {
	r.t().Helper()
	c, err := r.env.Connect(context.Background(), r.node)
	if err != nil {
		r.t().Fatal(err)
	}
	r.t().Cleanup(func() { c.Close() })
	return c
}

func newLinkRig(t *testing.T) *linkRig {
	t.Helper()
	ca := testCA(t)
	ag := fakeagent.Start(t, ca, loopback)
	files := newFakeFiles(t)
	r := &linkRig{
		env: &Env{
			CA: ca, Files: files, Local: netip.MustParseAddr(loopback), DialTimeout: 3 * time.Second,
			Settle: 700 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		ca:    ca,
		node:  model.Node{ID: 1, Name: "Frankfurt", Host: loopback, AgentPort: ag.Port(), EntryPort: 443, SSHPort: 22},
		ag:    ag,
		files: files,
	}
	r.tt = t
	return r
}

func (r *linkRig) t() *testing.T { return r.tt }

func TestConnectProvesTheManagedProfile(t *testing.T) {
	r := newLinkRig(t)
	c, err := r.env.Connect(context.Background(), r.node)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got, want := r.ag.Calls(), []string{"GetVersion", "GetSSHStatus"}; !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
	if info, err := c.GetVersion(context.Background()); err != nil || info.AgentVersion != "v1.3.0" || info.Arch != "amd64" {
		t.Fatalf("the client it returns doesn't work: %+v, %v", info, err)
	}
}

func TestConnectRefusesAnAgentThatRunsEverything(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) { a.Full, a.AgentVersion = true, "v1.2.0" })
	c, err := r.env.Connect(context.Background(), r.node)
	if err == nil {
		c.Close()
		t.Fatal("an agent that answers GetSSHStatus was accepted as managed")
	}
	var nm *NotManagedError
	if !errors.Is(err, ErrNotManaged) || !errors.As(err, &nm) || nm.AgentVersion != "v1.2.0" {
		t.Fatalf("err = %v", err)
	}
}

// The proof is a refusal by the profile. An agent that can't answer proves
// nothing, either way, and one that fails the call on its own is not refusing it.
func TestWhatCountsAsProofOfTheManagedProfile(t *testing.T) {
	for _, c := range []struct {
		name    string
		answer  func() (*pb.SSHStatus, error)
		managed bool   // Connect succeeds
		notMan  bool   // and if not, it is ErrNotManaged
		hint    string // else the error says
	}{
		{"refused by the profile", func() (*pb.SSHStatus, error) { return nil, status.Error(codes.PermissionDenied, "managed") }, true, false, ""},
		{"answered", func() (*pb.SSHStatus, error) { return &pb.SSHStatus{}, nil }, false, true, ""},
		{"failed on its own", func() (*pb.SSHStatus, error) { return nil, errors.New("failed to get ssh status") }, false, true, ""},
		{"not implemented", func() (*pb.SSHStatus, error) { return nil, status.Error(codes.Unimplemented, "no") }, false, true, ""},
		{"unavailable", func() (*pb.SSHStatus, error) { return nil, status.Error(codes.Unavailable, "going away") }, false, false, "doesn't answer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newLinkRig(t)
			r.ag.Configure(func(a *fakeagent.Agent) { a.OnSSHStatus = c.answer })
			cl, err := r.env.Connect(context.Background(), r.node)
			if err == nil {
				cl.Close()
			}
			switch {
			case c.managed && err != nil:
				t.Fatalf("refused: %v", err)
			case !c.managed && err == nil:
				t.Fatal("accepted")
			case !c.managed && c.notMan != errors.Is(err, ErrNotManaged):
				t.Fatalf("ErrNotManaged = %v for %v", errors.Is(err, ErrNotManaged), err)
			case c.hint != "" && !strings.Contains(err.Error(), c.hint):
				t.Fatalf("err = %v, want it to say %q", err, c.hint)
			}
		})
	}
}

func TestConnectToNothingIsUnreachableAndNamesThePort(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Stop() // the port is closed now
	started := time.Now()
	_, err := r.env.Connect(context.Background(), r.node)
	var u *UnreachableError
	if !errors.Is(err, ErrUnreachable) || !errors.As(err, &u) || u.What != "agent" || u.Port != r.node.AgentPort {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "TCP port") || !strings.Contains(err.Error(), "firewall") {
		t.Errorf("the message doesn't say what to look at: %q", err)
	}
	// a closed port is told at once, not after the whole dial timeout
	if took := time.Since(started); took > time.Second {
		t.Errorf("took %v to find a closed port out", took)
	}
}

func TestConnectNotesACertificateItDoesNotTrust(t *testing.T) {
	// an agent that belongs to another hub shows a certificate from another CA
	other := fakeagent.Start(t, testCA(t), loopback)
	r := newLinkRig(t)
	r.node.AgentPort = other.Port()
	r.env.DialTimeout = 600 * time.Millisecond // the failure is at once, and the dial retries until this
	_, err := r.env.Connect(context.Background(), r.node)
	if !errors.Is(err, ErrAgentCert) {
		t.Fatalf("err = %v, want ErrAgentCert", err)
	}
	if errors.Is(err, ErrUnreachable) {
		t.Error("a certificate that isn't trusted was called unreachable")
	}
	if !strings.Contains(err.Error(), "another hub") {
		t.Errorf("the message doesn't say what it may be: %q", err)
	}
}

func TestConnectStopsWhenItsContextDoes(t *testing.T) {
	// a listener that takes the connection and never speaks TLS
	l, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	r := newLinkRig(t)
	r.node.AgentPort = l.Addr().(*net.TCPAddr).Port
	r.env.DialTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = r.env.Connect(ctx, r.node)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Fatalf("took %v to notice that it was cancelled", took)
	}
}

func TestConnectNeedsABoundNodeAndACA(t *testing.T) {
	r := newLinkRig(t)
	for name, node := range map[string]model.Node{
		"no port":      {ID: 1, Host: loopback},
		"a bad host":   {ID: 1, Host: "not a host", AgentPort: 9090},
		"a host: port": {ID: 1, Host: "1.2.3.4:5", AgentPort: 9090},
	} {
		var inv *model.InvalidError
		if _, err := r.env.Connect(context.Background(), node); !errors.As(err, &inv) {
			t.Errorf("%s: err = %v, want a model.InvalidError", name, err)
		}
	}
	if _, err := (&Env{}).Connect(context.Background(), r.node); !errors.Is(err, ErrNoCA) {
		t.Errorf("without a CA: %v", err)
	}
}

func TestVerifyAgentChecksVersionAndProfile(t *testing.T) {
	r := newLinkRig(t)
	want := &artifacts.Agent{Version: "v1.3.0", Arch: "amd64"}
	ctx := context.Background()

	v, err := r.env.verifyAgent(ctx, r.node, want)
	if err != nil || v.Agent != "v1.3.0" {
		t.Fatalf("the right agent: %+v, %v", v, err)
	}
	// the file may say it without the v
	if _, err := r.env.verifyAgent(ctx, r.node, &artifacts.Agent{Version: "1.3.0"}); err != nil {
		t.Errorf("1.3.0 against v1.3.0: %v", err)
	}

	var mm *VersionMismatchError
	_, err = r.env.verifyAgent(ctx, r.node, &artifacts.Agent{Version: "v1.4.0"})
	if !errors.Is(err, ErrVersionMismatch) || !errors.As(err, &mm) || mm.What != "agent" || mm.Want != "v1.4.0" || mm.Got != "v1.3.0" {
		t.Errorf("another version: %v", err)
	}

	r.ag.Configure(func(a *fakeagent.Agent) { a.Full = true })
	if _, err = r.env.verifyAgent(ctx, r.node, want); !errors.Is(err, ErrNotManaged) {
		t.Errorf("a full agent: %v", err)
	}
}

// The agent has only just been started, and may not be listening for a moment.
func TestVerifyAgentWaitsForAnAgentThatIsStillStarting(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Stop()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		r.ag.Restart("v1.3.0", 0)
	}()
	v, err := r.env.verifyAgent(context.Background(), r.node, &artifacts.Agent{Version: "v1.3.0"})
	if err != nil || v.Agent != "v1.3.0" {
		t.Fatalf("%+v, %v", v, err)
	}
}

func TestVerifyAgentGivesUpOnACertificateAtOnce(t *testing.T) {
	other := fakeagent.Start(t, testCA(t), loopback)
	r := newLinkRig(t)
	r.node.AgentPort = other.Port()
	r.env.DialTimeout = 600 * time.Millisecond
	started := time.Now()
	_, err := r.env.verifyAgent(context.Background(), r.node, &artifacts.Agent{Version: "v1.3.0"})
	if !errors.Is(err, ErrAgentCert) {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("waited %v for a certificate that time won't mend", took)
	}
}

// Every node-bound connection leaves from Local. An address the machine doesn't
// have can't be bound, which shows the link tries to use it on any system.
func TestTheLinkLeavesFromTheLocalAddress(t *testing.T) {
	r := newLinkRig(t)
	r.env.Local = netip.MustParseAddr("192.0.2.1") // TEST-NET-1, nobody's
	_, err := r.env.Connect(context.Background(), r.node)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "assign requested address") {
		t.Errorf("not refused for the address: %v", err)
	}
	if calls := r.ag.Calls(); len(calls) != 0 {
		t.Errorf("the agent heard from the hub anyway: %v", calls)
	}
}

// With a second loopback address (Linux has all of 127/8) the agent shows
// where a call came from.
func TestTheAgentSeesTheLocalAddress(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this system has no 127.0.0.2 on its loopback: %v", err)
	}
	probe.Close()
	r := newLinkRig(t)
	r.env.Local = netip.MustParseAddr("127.0.0.2")
	c, err := r.env.Connect(context.Background(), r.node)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	for _, p := range r.ag.Peers() {
		if host, _, _ := net.SplitHostPort(p); host != "127.0.0.2" {
			t.Errorf("a call came from %s, want 127.0.0.2", p)
		}
	}
	if len(r.ag.Peers()) == 0 {
		t.Error("no calls")
	}
}

func TestParseXrayVersion(t *testing.T) {
	for in, want := range map[string]string{
		"Xray 26.10.21 (Xray, Penetrates Everything.) 1b1647c (go1.27.1 linux/amd64)": "26.10.21",
		"Xray v26.10.21 (Xray, Penetrates Everything.)":                               "26.10.21",
		"  xray   1.8.4 (x)": "1.8.4",
		"unknown":            "",
		"":                   "",
		"Xray":               "",
		"Xray (no version)":  "",
		"Something 1.2.3":    "",
	} {
		if got := ParseXrayVersion(in); got != want {
			t.Errorf("ParseXrayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameVersionIgnoresTheLeadingV(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"v1.3.0", "v1.3.0", true}, {"v1.3.0", "1.3.0", true}, {" v1.3.0\n", "v1.3.0", true},
		{"v1.3.0", "v1.3.1", false}, {"dev", "v1.3.0", false}, {"", "v1.3.0", false},
	} {
		if got := sameVersion(c.a, c.b); got != c.same {
			t.Errorf("sameVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestPollGivesUpWithTheLastThingItSaid(t *testing.T) {
	boom := errors.New("not yet")
	var calls int
	err := poll(context.Background(), 120*time.Millisecond, 30*time.Millisecond, func(context.Context) (bool, error) {
		calls++
		return false, boom
	})
	if err != boom || calls < 3 {
		t.Fatalf("err = %v after %d calls", err, calls)
	}
	// done wins, and a fatal error ends it at once
	if err := poll(context.Background(), time.Second, time.Millisecond, func(context.Context) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	calls = 0
	err = poll(context.Background(), time.Second, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		return false, stop(boom)
	})
	if err != boom || calls != 1 {
		t.Fatalf("a fatal error: %v after %d calls", err, calls)
	}
	// and with nothing said, it is the clock or the context
	if err := poll(context.Background(), 20*time.Millisecond, 5*time.Millisecond, func(context.Context) (bool, error) { return false, nil }); !errors.Is(err, errPollTimeout) {
		t.Fatalf("a quiet timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := poll(ctx, time.Second, time.Millisecond, func(context.Context) (bool, error) { return false, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context: %v", err)
	}
}
