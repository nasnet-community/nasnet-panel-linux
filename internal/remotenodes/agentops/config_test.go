// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// standIn is a port that answers, as an Xray's entry would, and the port to ask.
func standIn(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		t.Fatal(err)
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

// closedPort is one nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func configInput(t *testing.T, entryPort int) nodecfg.Input {
	t.Helper()
	k, err := NewRealityKeys()
	if err != nil {
		t.Fatal(err)
	}
	return nodecfg.Input{
		EntryPort: entryPort, RealityDest: "www.google.com:443", ServerNames: []string{"www.google.com"},
		PrivateKey: k.PrivateKey, ShortIDs: []string{k.ShortID},
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(1)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(1), ReverseTag: "r1"},
	}
}

func TestPushConfigValidatesPushesAndChecksTheEntry(t *testing.T) {
	r := newLinkRig(t)
	in := configInput(t, standIn(t))
	want, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var validated, pushed string
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnValidate = func(c string) *pb.ValidationResult {
			mu.Lock()
			validated = c
			mu.Unlock()
			return &pb.ValidationResult{Valid: true}
		}
		a.OnConfig = func(c string) *pb.CommandResponse {
			mu.Lock()
			pushed = c
			mu.Unlock()
			return &pb.CommandResponse{Success: true}
		}
	})
	c := r.connect()
	r.ag.Configure(func(a *fakeagent.Agent) {}) // the calls below are what counts
	before := len(r.ag.Calls())
	if err := r.env.PushConfig(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if validated != string(want) || pushed != string(want) {
		t.Error("what the node validated and was pushed isn't what nodecfg builds")
	}
	// it is checked on the node first, then pushed, then looked at
	if got, wantCalls := r.ag.Calls()[before:], []string{"ValidateConfig", "PushConfig", "GetStatus"}; !slices.Equal(got, wantCalls) {
		t.Errorf("calls %v, want %v", got, wantCalls)
	}
}

func TestPushConfigWithABuildErrorNeverCallsTheNode(t *testing.T) {
	r := newLinkRig(t)
	in := configInput(t, 443)
	in.PrivateKey = "not a key"
	c := r.connect()
	before := len(r.ag.Calls())
	err := r.env.PushConfig(context.Background(), c, in)
	if !errors.Is(err, nodecfg.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if after := len(r.ag.Calls()); after != before {
		t.Errorf("the node was called %d times", after-before)
	}
}

// Xray's own check refuses it, the agent says what it printed, and the hub
// shows that and doesn't push.
func TestPushConfigSurfacesTheNodesValidationMessage(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnValidate = func(string) *pb.ValidationResult {
			return &pb.ValidationResult{Errors: []string{"Xray 26.10.21 (Xray, Penetrates Everything.)\nFailed to start: main: failed to load config files: infra/conf: REALITY: the handshake target isn't allowed\n"}}
		}
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, 443))
	var rej *ConfigRejectedError
	if !errors.Is(err, ErrConfigRejected) || !errors.As(err, &rej) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "the handshake target isn't allowed") || !strings.HasPrefix(err.Error(), "The node's Xray refused the configuration") {
		t.Errorf("the message: %q", err)
	}
	if slices.Contains(r.ag.Calls(), "PushConfig") {
		t.Error("a config the node refused was pushed")
	}
}

func TestPushConfigCutsALongMessageToItsEnd(t *testing.T) {
	r := newLinkRig(t)
	long := strings.Repeat("banner line of no use\n", 100) + "the reason, last"
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnValidate = func(string) *pb.ValidationResult { return &pb.ValidationResult{Errors: []string{long}} }
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, 443))
	if err == nil || len(err.Error()) > 900 || !strings.Contains(err.Error(), "the reason, last") {
		t.Fatalf("err = %q", err)
	}
}

// The push itself can fail where the check passed: the node's Xray wouldn't
// start on it.
func TestPushConfigSaysWhenXrayWouldNotStartOnIt(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnConfig = func(string) *pb.CommandResponse {
			return &pb.CommandResponse{Success: false, Message: "Config written but restart failed: failed to start service xray: exit status 1"}
		}
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, 443))
	if !errors.Is(err, ErrXrayNotRunning) || !strings.Contains(err.Error(), "failed to start service xray") {
		t.Fatalf("err = %v", err)
	}
	// and a push the node refuses for the config's sake is the config's fault, not Xray's start
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnConfig = func(string) *pb.CommandResponse {
			return &pb.CommandResponse{Success: false, Message: "Failed to update config: config validation failed: bad listen"}
		}
	})
	err = r.env.PushConfig(context.Background(), r.connect(), configInput(t, 443))
	if !errors.Is(err, ErrConfigRejected) || !strings.Contains(err.Error(), "bad listen") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushConfigStartsXrayWhenThePushLeftItStopped(t *testing.T) {
	r := newLinkRig(t)
	in := configInput(t, standIn(t))
	var calls int
	var mu sync.Mutex
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnStatus = func() *pb.NodeStatus {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return &pb.NodeStatus{XrayRunning: calls > 1} // stopped at the first look, running at the next
		}
	})
	c := r.connect()
	if err := r.env.PushConfig(context.Background(), c, in); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.ag.Calls(), "StartXray") {
		t.Errorf("Xray wasn't started: %v", r.ag.Calls())
	}
}

func TestPushConfigWhenXrayWillNotRun(t *testing.T) {
	r := newLinkRig(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnStatus = func() *pb.NodeStatus { return &pb.NodeStatus{XrayRunning: false} }
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, standIn(t)))
	if !errors.Is(err, ErrXrayNotRunning) {
		t.Fatalf("err = %v", err)
	}
}

// With Xray up and the entry known to it, a port that doesn't answer is the
// firewall's, and the message says which port to open.
func TestPushConfigNamesTheEntryPortWhenItDoesNotAnswer(t *testing.T) {
	r := newLinkRig(t)
	port := closedPort(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnUsers = func(tag string) (*pb.UserList, error) {
			if tag != nodecfg.EntryTag {
				t.Errorf("ListUsers asked for %q", tag)
			}
			return &pb.UserList{}, nil // the inbound is there
		}
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, port))
	var u *UnreachableError
	if !errors.Is(err, ErrUnreachable) || !errors.As(err, &u) || u.What != "entry" || u.Port != port {
		t.Fatalf("err = %v", err)
	}
	if want := "TCP port " + itoa(port); !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "firewall") {
		t.Errorf("the message: %q", err)
	}
}

// With the entry not known to Xray, the port is quiet for another reason.
func TestPushConfigBlamesXrayWhenItDoesNotKnowTheEntry(t *testing.T) {
	r := newLinkRig(t)
	port := closedPort(t)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnUsers = func(string) (*pb.UserList, error) {
			return nil, status.Error(codes.Unknown, "failed to list users: inbound not found")
		}
	})
	err := r.env.PushConfig(context.Background(), r.connect(), configInput(t, port))
	if !errors.Is(err, ErrXrayNotRunning) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "port "+itoa(port)) {
		t.Errorf("the message: %q", err)
	}
}

func TestPushConfigLooksAtTheEntryFromTheBoundAddress(t *testing.T) {
	// an address that can't be bound shows the check goes through the bound dialer
	r := newLinkRig(t)
	c := r.connect()
	in := configInput(t, standIn(t))
	other := &Env{CA: r.ca, Files: r.files, Local: mustAddr("192.0.2.1"), Settle: r.env.Settle} // nobody's address
	err := other.PushConfig(context.Background(), c, in)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "assign requested address") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushConfigToAnAgentThatIsGoneSaysTheNodeDoesNotAnswer(t *testing.T) {
	r := newLinkRig(t)
	c := r.connect()
	r.ag.Stop()
	err := r.env.PushConfig(context.Background(), c, configInput(t, 443))
	var u *UnreachableError
	if !errors.Is(err, ErrUnreachable) || !errors.As(err, &u) || u.What != "agent" || u.Port != r.node.AgentPort {
		t.Fatalf("err = %v", err)
	}
}
