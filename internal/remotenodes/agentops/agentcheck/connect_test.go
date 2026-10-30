// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

var bg = context.Background()

func itoa(n int) string { return strconv.Itoa(n) }

func ctxWith(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(bg, d)
}

// connect is what the manager does to get its client.
func (r *rig) connect() *agent.Client {
	r.t.Helper()
	c, err := r.env.Connect(bg, r.node)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	return c
}

func TestConnectToARealManagedAgent(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	c, err := r.env.Connect(bg, r.node)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// the client it hands back is the one the manager will poll with, and the
	// agent is held to its profile: the read-only call the proof is made with
	// is refused to it, and the ones the hub is there for are answered
	if _, err := c.GetSSHStatus(bg); status.Code(err) != codes.PermissionDenied {
		t.Errorf("GetSSHStatus: %v, want PermissionDenied", err)
	}
	if _, err := c.ExecuteCommand(bg, "id", 5); status.Code(err) != codes.PermissionDenied {
		t.Errorf("ExecuteCommand: %v, want PermissionDenied", err)
	}
	info, err := c.GetVersion(bg)
	if err != nil {
		t.Fatal(err)
	}
	if agentops.ParseXrayVersion(info.XrayVersion) != "26.10.10" {
		t.Errorf("the agent reports Xray %q", info.XrayVersion)
	}
}

// An agent that predates the managed profile ignores the line in its config
// and runs every call. One that was set up by hand with the full profile does
// the same. Either has to be refused: whatever the hub sends it, it would
// answer a shell too.
func TestConnectRefusesARealAgentThatRunsTheFullProfile(t *testing.T) {
	for _, p := range []config.Profile{config.ProfileFull, ""} {
		r := startRig(t, options{profile: p})
		c, err := r.env.Connect(bg, r.node)
		if err == nil {
			c.Close()
			t.Fatalf("profile %q: an agent that runs everything was taken", p)
		}
		var nm *agentops.NotManagedError
		if !errors.Is(err, agentops.ErrNotManaged) || !errors.As(err, &nm) {
			t.Fatalf("profile %q: err = %v", p, err)
		}
	}
}

func TestConnectRefusesARealAgentOfAnotherHub(t *testing.T) {
	// the agent's certificate is from a CA that isn't the hub's
	r := startRig(t, options{profile: config.ProfileManaged, ca: newCA(t)})
	r.env.DialTimeout = 600 * time.Millisecond // the refusal is at once, and the dial retries until this
	_, err := r.env.Connect(bg, r.node)
	if !errors.Is(err, agentops.ErrAgentCert) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "another hub") {
		t.Errorf("the message doesn't say what it may be: %q", err)
	}
}
