// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// standIn is a port that answers, as the entry of a running Xray would.
func standIn(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
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

func input(t *testing.T, entry int, dest string) nodecfg.Input {
	t.Helper()
	k, err := agentops.NewRealityKeys()
	if err != nil {
		t.Fatal(err)
	}
	return nodecfg.Input{
		EntryPort: entry, RealityDest: dest, ServerNames: []string{strings.Split(dest, ":")[0]},
		PrivateKey: k.PrivateKey, ShortIDs: []string{k.ShortID},
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(1)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(7), ReverseTag: "r7"},
	}
}

// startXray starts the node's Xray through the agent first, so that when a
// push restarts it, the agent stops the one it started and not whichever other
// process is called xray (see startRig).
func (r *rig) startXray(c *agent.Client) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.cfg.Xray.ConfigPath), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(r.cfg.Xray.ConfigPath, []byte("{}"), 0o644); err != nil {
		r.t.Fatal(err)
	}
	if err := c.StartXray(bg); err != nil {
		r.t.Fatal(err)
	}
}

func TestPushConfigToTheRealAgent(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: "26.10.21"})
	c := r.connect()
	r.startXray(c)
	in := input(t, standIn(t), "www.google.com:443")
	want, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.env.PushConfig(bg, c, in); err != nil {
		t.Fatal(err)
	}
	// what the agent holds is what nodecfg built, and Xray runs on it
	got, err := os.ReadFile(r.cfg.Xray.ConfigPath)
	if err != nil || string(got) != string(want) {
		t.Errorf("the agent's config file isn't what was built (%v)", err)
	}
	if cur, _, err := c.GetCurrentConfig(bg); err != nil || cur != string(want) {
		t.Errorf("GetCurrentConfig: %v", err)
	}
	if st, err := c.GetStatus(bg); err != nil || !st.XrayRunning {
		t.Errorf("status %+v, %v", st, err)
	}
}

// Xray's own check on the node says no, and what it printed is what the page shows.
func TestPushConfigTheRealAgentRefusesSaysWhy(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: "26.10.21"})
	c := r.connect()
	r.startXray(c)
	before, _ := os.ReadFile(r.cfg.Xray.ConfigPath)

	err := r.env.PushConfig(bg, c, input(t, 443, "reject.example:443"))
	var rej *agentops.ConfigRejectedError
	if !errors.Is(err, agentops.ErrConfigRejected) || !errors.As(err, &rej) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "the handshake target reject.example is not allowed") {
		t.Errorf("the message doesn't carry Xray's words: %q", err)
	}
	// and the config the node runs is the one it had
	after, _ := os.ReadFile(r.cfg.Xray.ConfigPath)
	if string(after) != string(before) {
		t.Errorf("the config on the node changed to %q", after)
	}
	if st, err := c.GetStatus(bg); err != nil || !st.XrayRunning {
		t.Errorf("Xray stopped: %+v, %v", st, err)
	}
}

// Xray runs and the entry's port doesn't answer from here: nothing listens
// there in the test, and the real agent's Xray API isn't there to say whether
// Xray knows the entry, so it is Xray that is blamed. (With the API, the
// firewall is blamed instead; that is tested against the stand-in agent.)
func TestPushConfigBlamesWhatDoesNotAnswerOnTheEntryPort(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: "26.10.21"})
	r.env.Settle = time.Second
	c := r.connect()
	r.startXray(c)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().(*net.TCPAddr).Port
	l.Close()

	ctx, cancel := ctxWith(20 * time.Second)
	defer cancel()
	err := r.env.PushConfig(ctx, c, input(t, closed, "www.google.com:443"))
	if err == nil {
		t.Fatal("a port nothing listens on answered")
	}
	if !errors.Is(err, agentops.ErrXrayNotRunning) && !errors.Is(err, agentops.ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "port "+itoa(closed)) {
		t.Errorf("the message doesn't name the port: %q", err)
	}
}
