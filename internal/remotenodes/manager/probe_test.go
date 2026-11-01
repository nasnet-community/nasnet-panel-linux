// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func probe(t *testing.T, r *opsRig, host string, port int) (api.ProbeResult, error) {
	t.Helper()
	return r.m.Probe(context.Background(), api.ProbeRequest{Host: host, SSHPort: port, SSHUser: "root"})
}

func TestAServerNoNodeIsOnIsNew(t *testing.T) {
	r := newOpsRig(t)
	got, err := probe(t, r, " Frankfurt.Example.COM ", 0)
	if err != nil || got.Fingerprint != fingerprintA || got.Status != api.ProbeNew {
		t.Fatalf("Probe = %+v, %v", got, err)
	}
	// the host in the form the rest keeps it, and the default port
	if want := []string{"frankfurt.example.com:22"}; !slices.Equal(r.ops.probes, want) {
		t.Errorf("the server was probed as %v, want %v", r.ops.probes, want)
	}
}

// The key pinned for a node is the one a server must show again. The
// comparison is for the node on that host and that SSH port, the key was agreed to
// before the install began, and a node that has none says nothing.
func TestAProbeComparesTheKeyWithTheOnePinnedForTheNodeThere(t *testing.T) {
	r := newOpsRig(t)
	addNode(t, r.cfg.Store, func(n *model.Node) { n.Host, n.SSHPort, n.SSHHostKey = "203.0.113.5", 22, fingerprintA })
	addNode(t, r.cfg.Store, func(n *model.Node) { n.Host, n.SSHPort, n.SSHHostKey = "203.0.113.6", 2222, fingerprintB })
	addNode(t, r.cfg.Store, func(n *model.Node) {
		n.Host, n.SSHPort, n.SSHHostKey, n.State = "203.0.113.7", 22, fingerprintB, model.NodeInstalling
	})
	r.ops.fingerprint = fingerprintA

	for _, c := range []struct {
		host string
		port int
		want string
	}{
		{"203.0.113.5", 22, api.ProbeKnown},     // the pinned key
		{"203.0.113.6", 2222, api.ProbeChanged}, // another
		{"203.0.113.7", 0, api.ProbeChanged},    // a node that was started and never finished has one pinned too
		{"203.0.113.5", 2222, api.ProbeNew},     // the same host on another port is no node of ours
		{"203.0.113.8", 22, api.ProbeNew},
	} {
		got, err := probe(t, r, c.host, c.port)
		if err != nil || got.Status != c.want || got.Fingerprint != fingerprintA {
			t.Errorf("%s:%d: %+v, %v, want %s", c.host, c.port, got, err, c.want)
		}
	}
	// a node with no key on record can't be compared with
	editNode(t, r.cfg.Store, 1, func(n *model.Node) { n.SSHHostKey = "" })
	if got, err := probe(t, r, "203.0.113.5", 22); err != nil || got.Status != api.ProbeNew {
		t.Errorf("a node with no key pinned: %+v, %v", got, err)
	}
}

func TestAProbeRefusesWhatCantBeAServer(t *testing.T) {
	r := newOpsRig(t)
	for name, req := range map[string]api.ProbeRequest{
		"no host":         {SSHPort: 22},
		"a URL":           {Host: "https://example.com", SSHPort: 22},
		"a port too big":  {Host: "203.0.113.5", SSHPort: 70000},
		"a negative port": {Host: "203.0.113.5", SSHPort: -2},
		"a bad user":      {Host: "203.0.113.5", SSHUser: "two words"},
	} {
		_, err := r.m.Probe(context.Background(), req)
		var inv *model.InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%s: err = %v, want an invalid value", name, err)
		}
	}
	if len(r.ops.allCalls()) != 0 {
		t.Errorf("the server was probed anyway: %v", r.ops.allCalls())
	}
}

// An address the router can't reach, say one that isn't on the domestic list,
// doesn't refuse the connection: it never answers. That is a dial that times
// out, which errors.Is also takes for a context deadline. It must still come out
// as a node that can't be reached, with the sentence for it, and not as a fault
// of the hub's own.
func TestAServerThatNeverAnswersIsANodeThatCantBeReached(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, timeout := (&net.Dialer{}).DialContext(ctx, "tcp", "203.0.113.5:22")
	if !errors.Is(timeout, context.DeadlineExceeded) || !strings.Contains(timeout.Error(), "i/o timeout") {
		t.Fatalf("the dial's error is %v, which is not the trap this test is about", timeout)
	}

	r := newOpsRig(t)
	r.ops.failAt("Probe", &agentops.UnreachableError{What: "ssh", Host: "203.0.113.5", Port: 22, Err: timeout})
	_, err := probe(t, r, "203.0.113.5", 22)
	var e *api.Error
	if !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable || !strings.Contains(e.Msg, "Can't reach SSH on 203.0.113.5 port 22") || !strings.Contains(e.Msg, "i/o timeout") {
		t.Errorf("a server that never answers: %v", err)
	}

	// and the same for the check of a Reality target, which goes over the agent's link
	if opFailure(&agentops.UnreachableError{What: "agent", Host: "203.0.113.5", Port: 9090, Err: timeout}).(*api.Error).Code != api.CodeNodeUnreachable {
		t.Error("an agent that never answers is not a node that can't be reached")
	}

	// a context that ended on its own is still passed on as it is, for the API to leave unanswered
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		if got := opFailure(ctxErr); got != ctxErr {
			t.Errorf("%v became %v", ctxErr, got)
		}
	}
}

func TestAProbeThatFailsIsWordedForThePage(t *testing.T) {
	r := newOpsRig(t)
	r.ops.failAt("Probe", &agentops.UnreachableError{What: "ssh", Host: "203.0.113.5", Port: 22, Err: errors.New("connection refused")})
	_, err := probe(t, r, "203.0.113.5", 22)
	var e *api.Error
	if !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable || !strings.Contains(e.Msg, "203.0.113.5") || !strings.Contains(e.Msg, "connection refused") {
		t.Errorf("an unreachable server: %v", err)
	}
	r.ops.failAt("Probe", errors.New("something else"))
	if _, err := probe(t, r, "203.0.113.5", 22); !errors.As(err, &e) || e.Code != api.CodeInternal {
		t.Errorf("another failure: %v", err)
	}

	// with no certificate authority there is nothing to dial with
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Probe(context.Background(), api.ProbeRequest{Host: "203.0.113.5"}); !errors.As(err, &e) || e.Code != api.CodeInternal {
		t.Errorf("a manager with no operations: %v", err)
	}
}
