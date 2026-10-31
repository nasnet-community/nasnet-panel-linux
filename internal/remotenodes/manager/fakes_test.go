// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// fakeAgent is a node's agent. What it holds survives a reconnection, as the
// real one's clients do, and a connection to it fails while it is down.
type fakeAgent struct {
	mu sync.Mutex

	down    bool
	nostats bool // answers a poll with a bundle that has no status in it

	users        map[string]string // email -> uuid, the clients on the entry
	agentVersion string
	xrayLine     string // the first line of `xray version`
	running      bool
	pid, started int64
	uptime       int64
	listErr      error // the next ListUsers fails with it, once
	restartErr   error // every RestartXray fails with it, until it is cleared
	buffered     []*agent.TrafficRecord
	online       map[string]map[string]int64 // email -> client address -> when, as the agent lists who is connected

	calls []string
	polls int
}

func newAgent() *fakeAgent {
	return &fakeAgent{
		users: map[string]string{}, agentVersion: "1.3.0", running: true, pid: 100, started: 1000, uptime: 600,
		xrayLine: "Xray " + pin.Version + " (Xray, Penetrates Everything.) 1b1647c (go1.26.1 linux/amd64)",
	}
}

func (a *fakeAgent) set(f func(*fakeAgent)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f(a)
}

func (a *fakeAgent) setDown(down bool) { a.set(func(a *fakeAgent) { a.down = down }) }

func (a *fakeAgent) count(prefix string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (a *fakeAgent) pollCount() int { return a.count("GetStatsBundle") }

func (a *fakeAgent) emails() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for e := range a.users {
		out = append(out, e)
	}
	slices.Sort(out)
	return out
}

// callsFrom is the calls made since n calls were made.
func (a *fakeAgent) callsFrom(n int) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.calls[n:])
}

func (a *fakeAgent) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

// fakeConn is one connection to a fakeAgent. A closed one fails, as a real
// one does.
type fakeConn struct {
	a      *fakeAgent
	closed atomic.Bool
}

var errDown = errors.New("agent unreachable")

// do runs a call on the agent, if the connection and the agent are up.
func (c *fakeConn) do(call string, f func(a *fakeAgent) error) error {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	c.a.calls = append(c.a.calls, call)
	if c.closed.Load() {
		return errors.New("the connection is closed")
	}
	if c.a.down {
		return errDown
	}
	return f(c.a)
}

func (c *fakeConn) GetStatsBundle(ctx context.Context, online bool) (*agent.StatsBundle, error) {
	var b *agent.StatsBundle
	err := c.do("GetStatsBundle", func(a *fakeAgent) error {
		a.polls++
		b = &agent.StatsBundle{Buffered: &agent.BufferedTrafficStats{Records: a.buffered}, OnlineIPs: a.online}
		if !a.nostats {
			b.Status = &pb.NodeStatus{
				XrayRunning: a.running, XrayVersion: a.xrayLine, XrayPid: a.pid, UptimeSeconds: a.uptime, LastRestartTime: a.started,
			}
		}
		return nil
	})
	return b, err
}

func (c *fakeConn) AckBufferedTraffic(ctx context.Context, through int64) error {
	return c.do("AckBufferedTraffic", func(*fakeAgent) error { return nil })
}

func (c *fakeConn) GetVersion(ctx context.Context) (*agent.VersionInfo, error) {
	var v *agent.VersionInfo
	err := c.do("GetVersion", func(a *fakeAgent) error {
		v = &agent.VersionInfo{AgentVersion: a.agentVersion, XrayVersion: a.xrayLine}
		return nil
	})
	return v, err
}

func (c *fakeConn) AddUser(ctx context.Context, tag, email, uuid, protocol, flow, enc string, level int32) error {
	return c.do("AddUser "+email, func(a *fakeAgent) error {
		if _, ok := a.users[email]; ok {
			return fmt.Errorf("User %s already exists.", email)
		}
		a.users[email] = uuid
		return nil
	})
}

func (c *fakeConn) RemoveUser(ctx context.Context, tag, email string) error {
	return c.do("RemoveUser "+email, func(a *fakeAgent) error {
		if _, ok := a.users[email]; !ok {
			return fmt.Errorf("User %s not found.", email)
		}
		delete(a.users, email)
		return nil
	})
}

func (c *fakeConn) ListUsers(ctx context.Context, tag string) ([]*pb.UserInfo, error) {
	var out []*pb.UserInfo
	err := c.do("ListUsers", func(a *fakeAgent) error {
		if a.listErr != nil {
			err := a.listErr
			a.listErr = nil
			return err
		}
		for e := range a.users {
			out = append(out, &pb.UserInfo{Email: e})
		}
		return nil
	})
	return out, err
}

// RestartXray is a new Xray process: the pid and the start time move, and the
// subscriptions' clients, which were added to the old one while it ran, are
// gone, which is what the hub puts right afterwards. The bridge's client is in
// the config file and stays. One that fails leaves the old process as it was.
func (c *fakeConn) RestartXray(ctx context.Context, validateConfig bool) error {
	return c.do(fmt.Sprintf("RestartXray validate=%t", validateConfig), func(a *fakeAgent) error {
		if a.restartErr != nil {
			return a.restartErr
		}
		a.pid++
		a.started += 60
		a.uptime = 1
		for email := range a.users {
			if _, ok := nodecfg.ParseSubscriptionEmail(email); ok {
				delete(a.users, email)
			}
		}
		return nil
	})
}

func (c *fakeConn) Close() error {
	c.closed.Store(true)
	return nil
}

// attempt is one try to connect to a node.
type attempt struct {
	after time.Duration // since the test started
	node  uint
	host  string
	port  int
	ok    bool
}

// fakeNet is the connector: it hands out connections to the nodes' fake
// agents, and keeps a record of every try.
type fakeNet struct {
	t0 time.Time

	mu       sync.Mutex
	agents   map[uint]*fakeAgent
	attempts []attempt
	conns    []*fakeConn
	conn     map[uint][]*fakeConn
}

func newNet() *fakeNet {
	return &fakeNet{t0: time.Now(), agents: map[uint]*fakeAgent{}, conn: map[uint][]*fakeConn{}}
}

// agent is the node's agent, made when first asked for.
func (f *fakeNet) agent(id uint) *fakeAgent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agents[id] == nil {
		f.agents[id] = newAgent()
	}
	return f.agents[id]
}

func (f *fakeNet) Connect(ctx context.Context, n model.Node) (Client, error) {
	a := f.agent(n.ID)
	a.mu.Lock()
	down := a.down
	a.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, attempt{after: time.Since(f.t0), node: n.ID, host: n.Host, port: n.AgentPort, ok: !down})
	if down {
		return nil, errors.New("connection refused")
	}
	c := &fakeConn{a: a}
	f.conns = append(f.conns, c)
	f.conn[n.ID] = append(f.conn[n.ID], c)
	return c, nil
}

// times are the moments of the tries for a node, since the test started.
func (f *fakeNet) times(node uint) []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []time.Duration
	for _, a := range f.attempts {
		if a.node == node {
			out = append(out, a.after)
		}
	}
	return out
}

func (f *fakeNet) tries(node uint) int { return len(f.times(node)) }

func (f *fakeNet) lastAttempt() attempt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts[len(f.attempts)-1]
}

// opened is how many connections a node has had made, and open how many of
// them are still not closed.
func (f *fakeNet) opened(node uint) (made, open int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conn[node] {
		made++
		if !c.closed.Load() {
			open++
		}
	}
	return made, open
}

// rig is a manager on a fake network, run inside a synctest bubble: time
// only moves when the test sleeps, so a test can say that a minute went by.
type rig struct {
	t   *testing.T
	cfg Config
	log *logBuf
	net *fakeNet
	m   *Manager

	cancel context.CancelFunc
	done   chan struct{}
}

func newRig(t *testing.T, mods ...func(*Config)) *rig {
	t.Helper()
	cfg, log := baseConfig(t)
	r := &rig{t: t, cfg: cfg, log: log, net: newNet()}
	r.cfg.Connector = r.net
	for _, mod := range mods {
		mod(&r.cfg)
	}
	m, err := New(r.cfg)
	must(t, err)
	r.m = m
	return r
}

// start runs the manager and lets it settle.
func (r *rig) start() *rig {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go func() {
		r.m.Run(ctx)
		close(r.done)
	}()
	synctest.Wait()
	return r
}

// stop closes the manager and waits for Run to return, which a bubble needs
// before it can end.
func (r *rig) stop() {
	r.t.Helper()
	must(r.t, r.m.Close(context.Background()))
	if r.done != nil {
		<-r.done
		r.cancel()
	}
}

// advance lets d go by and everything that is due happen.
func (r *rig) advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// the node and subscription fixtures

const fixtureKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA" // 32 bytes, 0x01 to 0x20

func bridgeIDOf(id uint) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", id) }

func clientIDOf(sub, node uint) string {
	return fmt.Sprintf("11111111-0000-4000-8000-%06d%06d", sub, node)
}

// addNode registers a node that would be ready for the bridge: it has the
// REALITY values, a bridge client and the pinned Xray.
func addNode(t testing.TB, st Store, mods ...func(*model.Node)) model.Node {
	t.Helper()
	var out model.Node
	must(t, st.Update(func(s *model.State) error {
		id := s.NextNodeID
		n := model.Node{
			Name: fmt.Sprintf("node %d", id), Host: fmt.Sprintf("203.0.113.%d", id), SSHPort: 22, AgentPort: 9090, EntryPort: 443,
			State: model.NodeRegistered, CreatedAt: epoch, Arch: "arm64",
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com", RealityPublicKey: fixtureKey,
			RealityShortID: "a1b2c3d4", RealityPrivateKey: "private-key-marker",
			AgentVersion: "1.3.0", XrayVersion: pin.Version, BridgeUUID: bridgeIDOf(id),
		}
		for _, mod := range mods {
			mod(&n)
		}
		var err error
		out, err = s.AddNode(n)
		return err
	}))
	return out
}

func addSubscription(t testing.TB, st Store, name string) model.Subscription {
	t.Helper()
	var out model.Subscription
	must(t, st.Update(func(s *model.State) error {
		var err error
		out, err = s.AddSubscription(model.Subscription{Name: name, CreatedAt: epoch})
		return err
	}))
	return out
}

// enable switches a subscription on or off at a node, keeping one client id
// for the pair as the real thing does.
func enable(t testing.TB, st Store, sub, node uint, on bool) {
	t.Helper()
	must(t, st.Update(func(s *model.State) error {
		if e := s.FindEnablement(sub, node); e != nil {
			e.Enabled = on
			return nil
		}
		s.Enablements = append(s.Enablements, model.Enablement{SubscriptionID: sub, NodeID: node, ClientUUID: clientIDOf(sub, node), Enabled: on})
		return nil
	}))
}

func editNode(t testing.TB, st Store, id uint, f func(*model.Node)) {
	t.Helper()
	must(t, st.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return model.ErrNotFound
		}
		f(n)
		return nil
	}))
}

func removeNode(t testing.TB, st Store, id uint) {
	t.Helper()
	must(t, st.Update(func(s *model.State) error { return s.RemoveNode(id) }))
}

func nodeOf(st Store, id uint) model.Node {
	s := st.Read()
	return *s.FindNode(id)
}

// peek is what the manager holds for a node, without the page's view of it.
type peek struct {
	seen        time.Time
	misses      int
	unreachable bool
	connected   bool
	agent       *api.AgentState
	agentVer    string
	xrayVer     string
	restarts    int
	resetDue    bool // the router's address changed and this node's Xray is yet to be restarted for it
}

func (m *Manager) peek(id uint) (peek, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l := m.nodes[id]
	if l == nil {
		return peek{}, false
	}
	p := peek{
		seen: l.seen, misses: l.misses, unreachable: l.unreachable, connected: l.client != nil,
		agentVer: l.agentVer, xrayVer: l.xrayVer, restarts: l.restarts, resetDue: l.reset.due(),
	}
	if l.agent != nil {
		a := *l.agent
		p.agent = &a
	}
	return p, true
}

// kinds are the kinds of a node's events, oldest first.
func kinds(events []api.NodeEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[len(events)-1-i] = e.Kind
	}
	return out
}

func secs(n ...int) []time.Duration {
	out := make([]time.Duration, len(n))
	for i, s := range n {
		out[i] = time.Duration(s) * time.Second
	}
	return out
}
