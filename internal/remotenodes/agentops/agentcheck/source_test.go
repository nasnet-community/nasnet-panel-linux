// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
)

// peerLog collects where the real agent says each call came from. Its request
// log names the peer of every call, so a hook on the log shows the address the
// hub connected from, as the agent saw it.
type peerLog struct {
	mu    sync.Mutex
	peers []string
}

func (p *peerLog) Levels() []logrus.Level { return logrus.AllLevels }

func (p *peerLog) Fire(e *logrus.Entry) error {
	if v, ok := e.Data["peer"].(string); ok {
		p.mu.Lock()
		p.peers = append(p.peers, v)
		p.mu.Unlock()
	}
	return nil
}

func (p *peerLog) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.peers...)
}

// watchPeers has the agent's request log (which startRig turns down) say where
// its calls come from, for the rest of the test.
func watchPeers(t *testing.T) *peerLog {
	t.Helper()
	log := &peerLog{}
	std := logrus.StandardLogger()
	oldHooks, oldLevel, oldOut := std.ReplaceHooks(make(logrus.LevelHooks)), std.GetLevel(), std.Out
	std.AddHook(log)
	std.SetLevel(logrus.InfoLevel)
	std.SetOutput(io.Discard)
	t.Cleanup(func() {
		std.ReplaceHooks(oldHooks)
		std.SetLevel(oldLevel)
		std.SetOutput(oldOut)
	})
	return log
}

// Every call the hub makes to a node leaves from Env.Local, and the real
// agent, which sees the connection, says so. The second address needs the
// loopback to own all of 127/8, as Linux's does and macOS's doesn't.
func TestTheRealAgentSeesTheAddressTheHubConnectsFrom(t *testing.T) {
	for _, local := range []string{"127.0.0.1", "127.0.0.2"} {
		t.Run(local, func(t *testing.T) {
			if local != "127.0.0.1" {
				l, err := net.Listen("tcp", local+":0")
				if err != nil {
					t.Skipf("this system has no %s on its loopback, so a second local address can't be tried: %v", local, err)
				}
				l.Close()
			}
			r := startRig(t, options{profile: config.ProfileManaged})
			peers := watchPeers(t)
			r.env.Local = netip.MustParseAddr(local)

			c := r.connect()
			if _, err := c.GetVersion(bg); err != nil {
				t.Fatal(err)
			}
			seen := peers.seen()
			if len(seen) < 3 { // the version and the proof Connect makes, and the one just asked
				t.Fatalf("the agent logged %d calls: %v", len(seen), seen)
			}
			for _, p := range seen {
				if host, _, err := net.SplitHostPort(p); err != nil || host != local {
					t.Errorf("a call came from %s, want %s", p, local)
				}
			}
		})
	}
}

// An address the machine doesn't have can't be bound, which shows on any
// system that the link is made from the one it is told to use: the dial fails
// at once and nothing reaches the agent.
func TestTheRealAgentIsNotReachedFromAnAddressTheMachineLacks(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	peers := watchPeers(t)
	r.env.Local = netip.MustParseAddr("192.0.2.1") // TEST-NET-1, nobody's

	_, err := r.env.Connect(bg, r.node)
	if !errors.Is(err, agentops.ErrUnreachable) || !strings.Contains(err.Error(), "assign requested address") {
		t.Fatalf("err = %v", err)
	}
	if seen := peers.seen(); len(seen) != 0 {
		t.Errorf("the agent heard from the hub anyway: %v", seen)
	}
}
