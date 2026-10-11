package mtprotonode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/mtproto-remote-plugin/reality"
	"github.com/nasnet-community/mtproto-remote-plugin/realityserver"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// --- fakes ---

type memStore struct {
	mu   sync.Mutex
	rows map[uint]LiteNode
	base map[uint]map[string]hubnode.Usage
}

func newMemStore() *memStore {
	return &memStore{rows: map[uint]LiteNode{}, base: map[uint]map[string]hubnode.Usage{}}
}
func (s *memStore) Get(_ context.Context, id uint) (*LiteNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}
func (s *memStore) Save(_ context.Context, n *LiteNode) error {
	s.mu.Lock()
	s.rows[n.NodeID] = *n
	s.mu.Unlock()
	return nil
}
func (s *memStore) Insert(ctx context.Context, n *LiteNode) error {
	if _, err := s.Get(ctx, n.NodeID); err == nil {
		return errors.New("duplicate node")
	}
	return s.Save(ctx, n)
}
func (s *memStore) UpdateInstall(_ context.Context, n *LiteNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[n.NodeID]
	if !ok {
		return ErrNotFound
	}
	r.AgentID, r.HubPort, r.CapturePort, r.SSHPort, r.SSHUser, r.HostKey = n.AgentID, n.HubPort, n.CapturePort, n.SSHPort, n.SSHUser, n.HostKey
	r.RealityPub, r.ShortID, r.Token, r.NextSeed, r.Arch = n.RealityPub, n.ShortID, n.Token, n.NextSeed, n.Arch
	s.rows[n.NodeID] = r
	return nil
}
func (s *memStore) UpdateObserved(_ context.Context, id uint, av, tv string, seen time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return nil // update-only: never resurrects a deleted node
	}
	r.AgentVersion, r.TelemtVersion, r.LastSeenAt = av, tv, &seen
	s.rows[id] = r
	return nil
}
func (s *memStore) UpdateCreds(_ context.Context, id uint, pub, sid, tok, seed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[id]; ok {
		r.RealityPub, r.ShortID, r.Token, r.NextSeed = pub, sid, tok, seed
		s.rows[id] = r
	}
	return nil
}
func (s *memStore) UpdateDisguise(_ context.Context, id uint, d string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[id]; ok {
		r.Disguise = d
		s.rows[id] = r
	}
	return nil
}
func (s *memStore) UpdateSettings(_ context.Context, id uint, set Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrNotFound
	}
	if set.MaxStreams != nil {
		r.MaxStreams = *set.MaxStreams
	}
	if set.SSHPort != nil {
		r.SSHPort = *set.SSHPort
	}
	if set.SSHUser != nil {
		r.SSHUser = *set.SSHUser
	}
	if set.ProxyStopped != nil {
		r.ProxyStopped = *set.ProxyStopped
	}
	s.rows[id] = r
	return nil
}
func (s *memStore) Delete(_ context.Context, id uint) error {
	s.mu.Lock()
	delete(s.rows, id)
	s.mu.Unlock()
	return nil
}
func (s *memStore) List(context.Context) ([]*LiteNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*LiteNode
	for _, r := range s.rows {
		r := r
		out = append(out, &r)
	}
	return out, nil
}
func (s *memStore) Baseline(_ context.Context, id uint) (map[string]hubnode.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]hubnode.Usage{}
	for k, v := range s.base[id] {
		out[k] = v
	}
	return out, nil
}
func (s *memStore) SaveBaseline(_ context.Context, id uint, cum map[string]hubnode.Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base[id] == nil {
		s.base[id] = map[string]hubnode.Usage{}
	}
	for k, v := range cum {
		s.base[id][k] = v
	}
	return nil
}

type health struct {
	online bool
	reason string
	detail string
}

type fakeNodes struct {
	mu       sync.Mutex
	node     nodeDomain.Node
	observed []health
	ingested [][]*agent.TrafficRecord
	ingErr   error
	gone     bool // the nodes row was deleted
	gets     int
	stats    []*nodeDomain.NodeStat
}

func (f *fakeNodes) GetNode(context.Context, uint) (*nodeDomain.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.gone {
		return nil, gorm.ErrRecordNotFound
	}
	n := f.node
	return &n, nil
}
func (f *fakeNodes) set(fn func(*fakeNodes)) { f.mu.Lock(); fn(f); f.mu.Unlock() }
func (f *fakeNodes) observations() int       { f.mu.Lock(); defer f.mu.Unlock(); return len(f.observed) }
func (f *fakeNodes) ObserveNodeHealth(_ context.Context, _ *nodeDomain.Node, online bool, reason, detail string) {
	f.mu.Lock()
	f.observed = append(f.observed, health{online, reason, detail})
	f.mu.Unlock()
}
func (f *fakeNodes) IngestTraffic(ctx context.Context, _ uint, recs []*agent.TrafficRecord, inTx func(context.Context) error) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ingErr != nil {
		return 0, f.ingErr
	}
	if inTx != nil {
		if err := inTx(ctx); err != nil {
			return 0, err
		}
	}
	f.ingested = append(f.ingested, recs)
	return 0, nil
}
func (f *fakeNodes) RecordNodeStat(_ context.Context, s *nodeDomain.NodeStat) error {
	f.mu.Lock()
	f.stats = append(f.stats, s)
	f.mu.Unlock()
	return nil
}
func (f *fakeNodes) lastHealth() (health, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.observed) == 0 {
		return health{}, false
	}
	return f.observed[len(f.observed)-1], true
}
func (f *fakeNodes) usage() (up, down int64, names map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names = map[string]bool{}
	for _, recs := range f.ingested {
		for _, r := range recs {
			for _, c := range r.CoreUsage {
				up += c.Uplink
				down += c.Downlink
				names[c.Email] = true
				if c.Protocol != "mtproto" {
					panic("wrong protocol " + c.Protocol)
				}
			}
		}
	}
	return
}

type bus struct {
	mu sync.Mutex
	ev []events.Event
}

func (b *bus) Publish(e events.Event)      { b.mu.Lock(); b.ev = append(b.ev, e); b.mu.Unlock() }
func (b *bus) has(t events.EventType) bool { return b.count(t) > 0 }
func (b *bus) count(t events.EventType) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.ev {
		if e.Type == t {
			n++
		}
	}
	return n
}

// mtprotoAgent answers the control calls from memory.
type mtprotoAgent struct {
	mu        sync.Mutex
	states    []ctrl.State
	applied   string
	checkOK   bool
	checked   []string
	addrs     []string
	users     []ctrl.UserStat
	running   bool
	crashLoop bool
	held      string
	ports     []ctrl.LinkPort
}

func (a *mtprotoAgent) linkPorts() []ctrl.LinkPort {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ctrl.LinkPort(nil), a.ports...)
}

func (a *mtprotoAgent) handlers() map[string]ctrl.Handler {
	return map[string]ctrl.Handler{
		ctrl.MLinkPort: func(_ context.Context, b json.RawMessage, _ io.Reader) (any, error) {
			var r ctrl.LinkPort
			json.Unmarshal(b, &r)
			a.mu.Lock()
			a.ports = append(a.ports, r)
			a.mu.Unlock()
			return nil, nil
		},
		ctrl.MStateApply: func(_ context.Context, b json.RawMessage, _ io.Reader) (any, error) {
			var s ctrl.State
			json.Unmarshal(b, &s)
			a.mu.Lock()
			a.states = append(a.states, s)
			a.applied = s.Rev
			a.mu.Unlock()
			return ctrl.StateAck{Rev: s.Rev}, nil
		},
		ctrl.MStatus: func(context.Context, json.RawMessage, io.Reader) (any, error) {
			a.mu.Lock()
			defer a.mu.Unlock()
			var st ctrl.Status
			st.AgentVersion, st.TelemtVersion, st.StateRev = "9.9.9", "3.4.25", a.applied
			st.Telemt.Running, st.Telemt.CrashLoop, st.Telemt.Held = a.running, a.crashLoop, a.held
			st.Disguise.Domain = "example.com"
			return st, nil
		},
		ctrl.MStats: func(context.Context, json.RawMessage, io.Reader) (any, error) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return ctrl.Stats{Users: append([]ctrl.UserStat(nil), a.users...)}, nil
		},
		ctrl.MDisguiseCheck: func(_ context.Context, b json.RawMessage, _ io.Reader) (any, error) {
			var c ctrl.DisguiseCheck
			json.Unmarshal(b, &c)
			a.mu.Lock()
			defer a.mu.Unlock()
			a.checked = append(a.checked, c.Domain)
			if a.checkOK {
				return ctrl.DisguiseResult{OK: true, TLS13: true, X25519: true, H2: true, Addrs: a.addrs}, nil
			}
			return ctrl.DisguiseResult{Err: "no h2", Addrs: a.addrs}, nil
		},
	}
}

func (a *mtprotoAgent) set(fn func(*mtprotoAgent)) { a.mu.Lock(); fn(a); a.mu.Unlock() }
func (a *mtprotoAgent) stateCount() int            { a.mu.Lock(); defer a.mu.Unlock(); return len(a.states) }
func (a *mtprotoAgent) lastState() ctrl.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.states) == 0 {
		return ctrl.State{}
	}
	return a.states[len(a.states)-1]
}

type env struct {
	agent  *mtprotoAgent
	lnPort int
	row    LiteNode
	cancel context.CancelFunc
	stop   func() // takes the agent off the network
	// serveOn has the same agent answer on another listener too
	serveOn func(ln net.Listener)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dest.EnableHTTP2 = true
	dest.StartTLS()
	t.Cleanup(dest.Close)
	priv, pub, _ := reality.GenerateKey()
	token := make([]byte, 32)
	rand.Read(token)
	sid := [8]byte{4, 2}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ra := &mtprotoAgent{running: true, checkOK: true}
	serve := func(ln net.Listener) *link.Agent {
		ag := link.NewAgent(link.AgentConfig{
			AgentID: "agent-1", Tokens: [][]byte{token}, Listener: ln,
			Reality:  realityserver.ServerConfig{Dest: dest.Listener.Addr().String(), ServerNames: []string{"example.com"}, PrivateKey: priv, ShortIDs: [][8]byte{sid}, MaxTimeDiff: time.Minute},
			Handlers: ra.handlers(),
		})
		go ag.Serve(ctx)
		for i := 0; !ag.Warm() && i < 300; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		return ag
	}
	ag := serve(ln)
	port := ln.Addr().(*net.TCPAddr).Port
	return &env{agent: ra, lnPort: port, cancel: cancel, stop: ag.Close, serveOn: func(ln net.Listener) { serve(ln) }, row: LiteNode{
		NodeID: 1, AgentID: "agent-1", HubPort: port, CapturePort: 9443, SSHPort: 22, SSHUser: "root",
		RealityPub: hex.EncodeToString(pub), ShortID: hex.EncodeToString(sid[:]), Token: hex.EncodeToString(token),
		Disguise: "example.com", MaxStreams: 100,
	}}
}

type fakeWANs struct {
	mu  sync.Mutex
	set []WAN
}

func (f *fakeWANs) DomesticWANs(context.Context) ([]WAN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WAN(nil), f.set...), nil
}

type managed struct {
	m     *Manager
	st    *memStore
	nodes *fakeNodes
	bus   *bus
	recon chan uint
	wans  *fakeWANs
	stop  func() // the panel shutting down
}

func (e *env) manager(t *testing.T) *managed { t.Helper(); return e.managerWith(t, nil) }

func (e *env) managerWith(t *testing.T, tweak func(*Config)) *managed {
	t.Helper()
	st := newMemStore()
	st.Save(context.Background(), &e.row)
	nodes := &fakeNodes{node: nodeDomain.Node{ID: 1, Name: "vps-1", IP: "127.0.0.1", Kind: nodeDomain.NodeKindLite, IsActive: true}}
	b := &bus{}
	recon := make(chan uint, 16)
	wans := &fakeWANs{}
	cfg := Config{
		Store: st, Nodes: nodes, WANs: wans, Dialers: plainDialers(), Bus: b,
		Allow: &tgnet.Refresher{List: tgnet.NewWithPorts([]netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, 443)},
		Reconcile: func(_ context.Context, id uint) error {
			select {
			case recon <- id:
			default:
			}
			return nil
		},
		ConnsPerWAN: 1, Tick: 100 * time.Millisecond, WatchEvery: 100 * time.Millisecond,
		StatusEvery: 100 * time.Millisecond, StatsEvery: 100 * time.Millisecond, DisguiseEvery: 100 * time.Millisecond,
		Ranges: func() Ranges { return PrefixRanges([]string{"127.0.0.0/8"}) },
	}
	if tweak != nil {
		tweak(&cfg)
	}
	m := NewManager(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); m.Stop() }) }
	t.Cleanup(stop)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return &managed{m: m, st: st, nodes: nodes, bus: b, recon: recon, wans: wans, stop: stop}
}

// push hands the node these users, the way a reconcile does.
func (mg *managed) push(t *testing.T, names ...string) {
	t.Helper()
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
	var users []*pb.CoreUser
	for _, n := range names {
		users = append(users, u(n, n, true))
	}
	p := payload(users...)
	p.Settings = []byte(`{"tls_domain":"example.com"}`)
	if err := mg.m.Pusher(1).ApplyCoreState(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	for i := 0; i < 700; i++ {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestManager(t *testing.T) {
	e := newEnv(t)

	t.Run("a stored node comes up, is reported online, and is reconciled", func(t *testing.T) {
		mg := e.manager(t)
		select {
		case id := <-mg.recon:
			if id != 1 {
				t.Fatalf("reconciled node %d", id)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no first reconcile")
		}
		waitFor(t, func() bool { h, ok := mg.nodes.lastHealth(); return ok && h.online }, "an online observation")
		h, _ := mg.nodes.lastHealth()
		if h.reason != nodeDomain.NodeOnlineReasonAgentReachable {
			t.Errorf("reason = %q", h.reason)
		}
		v, ok := mg.m.View(1)
		if !ok || v.Snapshot.Link.Up == 0 {
			t.Fatalf("view = %+v ok=%v", v, ok)
		}
		waitFor(t, func() bool {
			r, _ := mg.st.Get(context.Background(), 1)
			return r != nil && r.AgentVersion == "9.9.9" && r.LastSeenAt != nil
		}, "the agent's version to be recorded")
	})

	t.Run("the pusher turns the hub's payload into the agent's state", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
		p := payload(u("alice@example.com", "alice", true))
		p.Settings = []byte(`{"tls_domain":"example.com"}`)
		if err := mg.m.Pusher(1).ApplyCoreState(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		st := e.agent.lastState()
		if len(st.Users) != 1 || st.Users[0].Name != "alice@example.com" || st.Disguise != "example.com" || len(st.DCNets) != 1 || st.MaxStreams != 100 {
			t.Fatalf("agent got %+v", st)
		}
		cs, err := mg.m.Pusher(1).GetCoreStatus(context.Background(), "mtproto")
		if err != nil || !cs.Running || len(cs.InboundIds) != 1 || cs.InboundIds[0] != "7" {
			t.Fatalf("status = %+v err=%v", cs, err)
		}
		// other protocols are not run here
		ssh := payload()
		ssh.CoreType = "ssh"
		if err := mg.m.Pusher(1).ApplyCoreState(context.Background(), ssh); err == nil {
			t.Error("an enabled ssh core was accepted")
		}
		ssh.Enabled = false
		if err := mg.m.Pusher(1).ApplyCoreState(context.Background(), ssh); err != nil {
			t.Errorf("tearing down a core the node never had should be a no-op: %v", err)
		}
	})

	t.Run("an inbound the hub deleted is pruned down to no users", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
		pu := mg.m.Pusher(1)
		p := payload(u("alice@example.com", "alice", true))
		p.Settings = []byte(`{"tls_domain":"example.com"}`)
		if err := pu.ApplyCoreState(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		teardown := &pbTeardown{}
		if err := pu.ApplyCoreState(context.Background(), teardown.payload("7")); err != nil {
			t.Fatal(err)
		}
		if st := e.agent.lastState(); len(st.Users) != 0 || st.Disguise != "example.com" {
			t.Fatalf("after prune: %+v", st)
		}
	})

	t.Run("a new cover site is checked from the VPS first, then the SNI follows", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
		change := func(domain string) error {
			p := payload(u("a", "a", true))
			p.Settings = []byte(`{"tls_domain":"` + domain + `"}`)
			return mg.m.Pusher(1).ApplyCoreState(context.Background(), p)
		}
		e.agent.set(func(a *mtprotoAgent) { a.checkOK = false })
		if err := change("bad.example.com"); err == nil {
			t.Fatal("an unusable cover site was accepted")
		}
		if d := e.agent.lastState().Disguise; d != "example.com" {
			t.Fatalf("the agent was told to use %q, a site that failed its check", d)
		}
		e.agent.set(func(a *mtprotoAgent) { a.checkOK = true })
		if err := change("other.example.com"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			r, _ := mg.st.Get(context.Background(), 1)
			return r != nil && r.Disguise == "other.example.com"
		}, "the new domain to be stored")
		if sni := mg.m.entry(1).node.Hub().Creds().Reality.ServerName; sni != "other.example.com" {
			t.Fatalf("later connections would present %q", sni)
		}
		// restore for the other subtests
		e.agent.set(func(a *mtprotoAgent) { a.checked = nil })
	})

	t.Run("usage is booked through the node usecase and the baseline is kept", func(t *testing.T) {
		e.agent.set(func(a *mtprotoAgent) { a.users = []ctrl.UserStat{{Name: "alice", OctetsUp: 1000, OctetsDown: 4000}} })
		mg := e.manager(t)
		mg.push(t, "alice")
		waitFor(t, func() bool {
			b, _ := mg.st.Baseline(context.Background(), 1)
			return b["alice"].Up == 1000
		}, "the first report to set the baseline")
		e.agent.set(func(a *mtprotoAgent) { a.users = []ctrl.UserStat{{Name: "alice", OctetsUp: 1600, OctetsDown: 4500}} })
		waitFor(t, func() bool { up, _, _ := mg.nodes.usage(); return up == 600 }, "+600 up to be ingested")
		up, down, names := mg.nodes.usage()
		if up != 600 || down != 500 || !names["alice"] {
			t.Fatalf("ingested up=%d down=%d names=%v", up, down, names)
		}
		b, _ := mg.st.Baseline(context.Background(), 1)
		if b["alice"].Up != 1600 {
			t.Fatalf("baseline not advanced: %+v", b)
		}
	})

	t.Run("a failed ingest is retried, not lost", func(t *testing.T) {
		e.agent.set(func(a *mtprotoAgent) { a.users = []ctrl.UserStat{{Name: "carol", OctetsUp: 10, OctetsDown: 10}} })
		mg := e.manager(t)
		mg.push(t, "carol")
		waitFor(t, func() bool { b, _ := mg.st.Baseline(context.Background(), 1); return b["carol"].Up == 10 }, "a baseline")
		mg.nodes.mu.Lock()
		mg.nodes.ingErr = errors.New("database is locked")
		mg.nodes.mu.Unlock()
		e.agent.set(func(a *mtprotoAgent) { a.users = []ctrl.UserStat{{Name: "carol", OctetsUp: 110, OctetsDown: 10}} })
		time.Sleep(400 * time.Millisecond)
		mg.nodes.mu.Lock()
		mg.nodes.ingErr = nil
		mg.nodes.mu.Unlock()
		waitFor(t, func() bool { up, _, _ := mg.nodes.usage(); return up == 100 }, "the delta once the database recovers")
		time.Sleep(300 * time.Millisecond)
		if up, _, _ := mg.nodes.usage(); up != 100 {
			t.Fatalf("booked %d, want exactly 100", up)
		}
	})

	t.Run("telemt trouble becomes events", func(t *testing.T) {
		e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = true, false })
		mg := e.manager(t)
		waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Agent != nil }, "a status report")
		p := payload(u("a", "a", true))
		p.Settings = []byte(`{"tls_domain":"other.example.com"}`)
		mg.m.Pusher(1).ApplyCoreState(context.Background(), p)
		e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = false, true })
		waitFor(t, func() bool {
			return mg.bus.has(events.EventMTProtoTelemtDown) && mg.bus.has(events.EventMTProtoTelemtCrashLoop)
		}, "telemt events")
		e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = true, false })
		waitFor(t, func() bool { return mg.bus.has(events.EventMTProtoTelemtUp) }, "telemt_up")
	})

	t.Run("the cover site is re-checked, flagged when hosted elsewhere, and its changes become events", func(t *testing.T) {
		e.agent.set(func(a *mtprotoAgent) { a.checkOK, a.addrs = true, []string{"127.0.0.1"} })
		defer e.agent.set(func(a *mtprotoAgent) { a.checkOK, a.addrs = true, nil })
		mg := e.manager(t)
		cover := func() *ctrl.DisguiseResult { v, _ := mg.m.View(1); return v.Snapshot.Disguise }
		waitFor(t, func() bool { c := cover(); return c != nil && c.Domestic != nil && *c.Domestic }, "a check inside the ranges")
		e.agent.set(func(a *mtprotoAgent) { a.addrs = []string{"127.0.0.1", "198.51.100.7"} })
		waitFor(t, func() bool { c := cover(); return c != nil && c.Domestic != nil && !*c.Domestic }, "an address outside the ranges to be flagged")
		if !cover().OK {
			t.Fatal("hosted elsewhere is a warning, not a failure")
		}
		e.agent.set(func(a *mtprotoAgent) { a.checkOK = false })
		waitFor(t, func() bool { return mg.bus.has(events.EventMTProtoDisguiseDown) }, "disguise_down")
		ups := mg.bus.count(events.EventMTProtoDisguiseUp)
		e.agent.set(func(a *mtprotoAgent) { a.checkOK = true })
		waitFor(t, func() bool { return mg.bus.count(events.EventMTProtoDisguiseUp) > ups }, "disguise_up")
		mg.bus.mu.Lock()
		defer mg.bus.mu.Unlock()
		for _, ev := range mg.bus.ev {
			if p, ok := ev.Payload.(events.MTProtoPayload); ok && ev.Type == events.EventMTProtoDisguiseDown && (p.Detail != "example.com" || p.Error != "no h2" || p.NodeName != "vps-1") {
				t.Fatalf("down payload = %+v", p)
			}
		}
	})

	t.Run("a changed WAN set restarts the node with pinned connections", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
		first := mg.m.entry(1)
		mg.wans.mu.Lock()
		mg.wans.set = []WAN{{Slot: "domestic", IfName: "eth0", Pin: 1}, {Slot: "domestic2", IfName: "eth1", Pin: 6}}
		mg.wans.mu.Unlock()
		waitFor(t, func() bool { return mg.m.entry(1) != first }, "the node to be re-wired")
		waitFor(t, func() bool { v, _ := mg.m.View(1); return len(v.Snapshot.Link.Conns) == 2 }, "one connection per WAN")
		v, _ := mg.m.View(1)
		if v.Snapshot.Link.Conns[0].Name != "domestic-0" || v.Snapshot.Link.Conns[1].Name != "domestic2-0" {
			t.Fatalf("conns = %+v", v.Snapshot.Link.Conns)
		}
	})

	t.Run("removing a node stops it", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
		mg.st.Delete(context.Background(), 1)
		waitFor(t, func() bool { return !mg.m.IsRunning(1) }, "the sync to drop the deleted node")
		if _, ok := mg.m.View(1); ok {
			t.Error("a removed node still has a view")
		}
		if err := mg.m.Call(context.Background(), 1, ctrl.MStatus, nil, nil); !errors.Is(err, ErrNotRunning) {
			t.Errorf("call on a removed node: %v", err)
		}
	})

	t.Run("an agent that goes away is reported offline", func(t *testing.T) {
		mg := e.manager(t)
		waitFor(t, func() bool { h, ok := mg.nodes.lastHealth(); return ok && h.online }, "online")
		e.stop() // the agent leaves the network
		waitFor(t, func() bool { h, ok := mg.nodes.lastHealth(); return ok && !h.online }, "an offline observation")
		h, _ := mg.nodes.lastHealth()
		if h.reason != nodeDomain.NodeOfflineReasonAgentUnreachable || h.detail == "" {
			t.Fatalf("health = %+v", h)
		}
	})
}

// pbTeardown builds the payload the core usecase's prune step sends for an
// inbound the hub no longer has.
type pbTeardown struct{}

func (pbTeardown) payload(id string) *pb.CoreStatePayload {
	return &pb.CoreStatePayload{CoreType: "mtproto", InboundId: id, Enabled: false}
}

// liveConns counts the link connections that are open right now.
type liveConns struct{ n atomic.Int64 }

type countedConn struct {
	net.Conn
	c    *liveConns
	once sync.Once
}

func (cc *countedConn) Close() error {
	cc.once.Do(func() { cc.c.n.Add(-1) })
	return cc.Conn.Close()
}

func (c *liveConns) dialers() Dialers {
	d := plainDialers()
	inner := d.Link(nil)
	f := func(ctx context.Context, n, a string) (net.Conn, error) {
		conn, err := inner(ctx, n, a)
		if err != nil {
			return nil, err
		}
		c.n.Add(1)
		return &countedConn{Conn: conn, c: c}, nil
	}
	return Dialers{Link: func(*WAN) link.DialFunc { return f }, DC: d.DC}
}

func TestConcurrentStartsLeaveOneRunningNode(t *testing.T) {
	e := newEnv(t)
	live := &liveConns{}
	mg := e.managerWith(t, func(c *Config) { c.Dialers = live.dialers(); c.Tick = time.Hour })
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
	for round := 0; round < 30; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = mg.m.Ensure(context.Background(), 1) }()
		}
		wg.Wait()
	}
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link after the restarts")
	// the replaced nodes need a moment to hang up
	deadline := time.Now().Add(5 * time.Second)
	for live.n.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := live.n.Load(); n != 1 {
		t.Fatalf("%d link connections are open, want 1: a replaced node is still running", n)
	}
}

// gatedDialers fail every link dial while down is set.
func gatedDialers(down *atomic.Bool) Dialers {
	d := plainDialers()
	inner := d.Link(nil)
	f := func(ctx context.Context, n, a string) (net.Conn, error) {
		if down.Load() {
			return nil, errors.New("network is unreachable")
		}
		return inner(ctx, n, a)
	}
	return Dialers{Link: func(*WAN) link.DialFunc { return f }, DC: d.DC}
}

func TestDeletingTheTunnelWhileTheLinkIsDownDropsItsUsers(t *testing.T) {
	e := newEnv(t)
	var down atomic.Bool
	down.Store(true)
	mg := e.managerWith(t, func(c *Config) { c.Dialers = gatedDialers(&down) })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p := payload(u("alice@example.com", "alice", true))
	p.Settings = []byte(`{"tls_domain":"example.com"}`)
	if err := mg.m.Pusher(1).ApplyCoreState(ctx, p); err == nil {
		t.Fatal("a push with the link down reported success")
	}
	// the tunnel is deleted before the link ever comes back
	_ = mg.m.Pusher(1).ApplyCoreState(ctx, pbTeardown{}.payload("7")) // queued: the link is still down
	down.Store(false)
	waitFor(t, func() bool { return e.agent.stateCount() > 0 }, "the queued state to reach the agent")
	time.Sleep(300 * time.Millisecond)
	if st := e.agent.lastState(); len(st.Users) != 0 {
		t.Fatalf("the agent got %d users from a deleted tunnel: %+v", len(st.Users), st.Users)
	}
}

// A tunnel deleted while the panel was down leaves nothing to tear down; the agent still ends with no users.
func TestARestartWithNoTunnelLeavesTheAgentNoUsers(t *testing.T) {
	e := newEnv(t)
	first := e.manager(t)
	first.push(t, "alice", "bob")
	waitFor(t, func() bool { return len(e.agent.lastState().Users) == 2 }, "the users on the agent")
	time.Sleep(300 * time.Millisecond) // anything still queued lands now
	first.stop()
	if st := e.agent.lastState(); len(st.Users) != 2 {
		t.Fatalf("before the restart the agent has %+v", st)
	}

	var down atomic.Bool
	down.Store(true)
	before := e.agent.stateCount()
	mg := e.managerWith(t, func(c *Config) { c.Dialers = gatedDialers(&down) }) // its reconcile finds no tunnel
	select {
	case <-mg.recon:
	case <-time.After(3 * time.Second):
		t.Fatal("no first reconcile")
	}
	down.Store(false)
	waitFor(t, func() bool { return e.agent.stateCount() > before }, "a state to reach the agent")
	st := e.agent.lastState()
	if len(st.Users) != 0 || st.Disguise != "example.com" || len(st.DCNets) == 0 || st.Port == 0 || st.Rev != Rev(st) {
		t.Fatalf("the agent got %+v", st)
	}
}

// The prune must not race a tunnel the reconcile did push.
func TestARestartWithATunnelKeepsItsUsers(t *testing.T) {
	e := newEnv(t)
	var mg *managed
	ready := make(chan struct{})
	mg = e.managerWith(t, func(c *Config) {
		c.Reconcile = func(ctx context.Context, id uint) error {
			<-ready
			p := payload(u("alice", "alice", true))
			p.Settings = []byte(`{"tls_domain":"example.com"}`)
			return mg.m.Pusher(id).ApplyCoreState(ctx, p)
		}
	})
	close(ready)
	waitFor(t, func() bool { _, ok := mg.m.View(1); return ok && e.agent.stateCount() > 0 }, "the tunnel's state")
	time.Sleep(500 * time.Millisecond)
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	for _, st := range e.agent.states {
		if len(st.Users) != 1 {
			t.Fatalf("the agent was handed %+v", st)
		}
	}
}

func TestTheCoreStatusSaysWhyTelemtIsHeld(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) { a.running, a.held = false, "waiting for the panel's first state" })
	mg := e.manager(t)
	var detail string
	waitFor(t, func() bool {
		cs, err := mg.m.Pusher(1).GetCoreStatus(context.Background(), "mtproto")
		if err == nil {
			detail = cs.Detail
		}
		return strings.Contains(detail, "held")
	}, "the status report")
	if detail != "telemt is held stopped: waiting for the panel's first state" {
		t.Fatalf("detail = %q", detail)
	}
}

func TestAFailingCoverSiteDoesNotHoldBackRevocations(t *testing.T) {
	e := newEnv(t)
	mg := e.manager(t)
	mg.push(t, "alice", "bob")
	e.agent.set(func(a *mtprotoAgent) { a.checkOK = false })
	defer e.agent.set(func(a *mtprotoAgent) { a.checkOK = true })

	// bob is revoked in the same reconcile that asks for a site the VPS cannot use
	p := payload(u("alice", "alice", true))
	p.Settings = []byte(`{"tls_domain":"bad.example.com"}`)
	err := mg.m.Pusher(1).ApplyCoreState(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "bad.example.com") {
		t.Fatalf("err = %v, want the cover site's problem", err)
	}
	st := e.agent.lastState()
	if len(st.Users) != 1 || st.Users[0].Name != "alice" {
		t.Fatalf("the revocation was held back: agent has %+v", st.Users)
	}
	if st.Disguise != "example.com" {
		t.Fatalf("the agent was moved to an unchecked cover site: %q", st.Disguise)
	}
	if v, _ := mg.m.View(1); !strings.Contains(v.CoverPending, "bad.example.com") {
		t.Fatalf("the pending cover site is not shown: %q", v.CoverPending)
	}
}

func TestANewCoverSiteIsCheckedBeforeTheTunnelIsSaved(t *testing.T) {
	e := newEnv(t)
	mg := e.manager(t)
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
	ctx := context.Background()
	if err := mg.m.CheckCover(ctx, 1, "example.com"); err != nil {
		t.Fatalf("the site in use needs no check: %v", err)
	}
	e.agent.set(func(a *mtprotoAgent) { a.checkOK, a.checked = false, nil })
	defer e.agent.set(func(a *mtprotoAgent) { a.checkOK = true })
	if err := mg.m.CheckCover(ctx, 1, "bad.example.com"); err == nil || !strings.Contains(err.Error(), "no h2") {
		t.Fatalf("err = %v, want the check's reason", err)
	}
	e.agent.set(func(a *mtprotoAgent) { a.checkOK = true })
	if err := mg.m.CheckCover(ctx, 1, "good.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := mg.m.CheckCover(ctx, 99, "good.example.com"); err == nil {
		t.Fatal("a node that is not running cannot vouch for a site")
	}
}

// Disabling an MTProto node stops it, and nothing reports it online until it is
// enabled again.
func TestADisabledNodeIsStoppedAndStaysOffline(t *testing.T) {
	e := newEnv(t)
	mg := e.manager(t)
	waitFor(t, func() bool { h, ok := mg.nodes.lastHealth(); return ok && h.online }, "online")
	mg.nodes.set(func(f *fakeNodes) { f.node.IsActive = false })
	waitFor(t, func() bool { return !mg.m.IsRunning(1) }, "the disabled node to stop")
	seen := mg.nodes.observations()
	time.Sleep(400 * time.Millisecond)
	if n := mg.nodes.observations(); n != seen {
		t.Fatalf("a disabled node's health was written %d more times", n-seen)
	}
	if err := mg.m.Ensure(context.Background(), 1); err == nil {
		t.Fatal("a disabled node was started")
	}
	mg.nodes.set(func(f *fakeNodes) { f.node.IsActive = true })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "the node to come back once enabled")
}

func TestANodeWhoseRowIsGoneIsStopped(t *testing.T) {
	e := newEnv(t)
	mg := e.manager(t)
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	mg.nodes.set(func(f *fakeNodes) { f.gone = true })
	waitFor(t, func() bool { return !mg.m.IsRunning(1) }, "the orphaned node to stop")
	time.Sleep(300 * time.Millisecond)
	if mg.m.IsRunning(1) {
		t.Fatal("the sync started it again")
	}
}

// With the foreign exit down every datacentre dial fails: hundreds a second must
// not each cost database reads and a slot on the shared event bus.
func TestMTProtoEventFloodsAreCoalesced(t *testing.T) {
	e := newEnv(t)
	mg := e.managerWith(t, func(c *Config) { c.WatchEvery = time.Hour })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	en := mg.m.entry(1)
	time.Sleep(200 * time.Millisecond) // the watcher's first look
	mg.nodes.set(func(f *fakeNodes) { f.gets = 0 })
	for i := 0; i < 500; i++ {
		mg.m.onEvent(en, hubnode.Event{Kind: hubnode.EventDialFailure, Detail: "149.154.167.51:443", Err: errors.New("i/o timeout"), At: time.Now()})
		mg.m.onEvent(en, hubnode.Event{Kind: hubnode.EventViolation, Detail: "198.51.100.1:443", At: time.Now()})
	}
	total := func(typ events.EventType) (n, sum int) {
		mg.bus.mu.Lock()
		defer mg.bus.mu.Unlock()
		for _, ev := range mg.bus.ev {
			if ev.Type != typ {
				continue
			}
			p := ev.Payload.(events.MTProtoPayload)
			if p.NodeName != "vps-1" {
				t.Errorf("payload lost the node's name: %+v", p)
			}
			n++
			sum += max(p.Count, 1)
		}
		return
	}
	waitFor(t, func() bool {
		_, a := total(events.EventMTProtoEgressFailed)
		_, b := total(events.EventMTProtoViolation)
		return a == 500 && b == 500
	}, "every occurrence to be accounted for")
	for _, typ := range []events.EventType{events.EventMTProtoEgressFailed, events.EventMTProtoViolation} {
		if n, _ := total(typ); n > 3 {
			t.Errorf("%s: %d events for one burst, want at most a few", typ, n)
		}
	}
	mg.nodes.mu.Lock()
	gets := mg.nodes.gets
	mg.nodes.mu.Unlock()
	if gets > 0 {
		t.Errorf("the burst cost %d node reads", gets)
	}
}

// A status poll can catch telemt running in the middle of a crash loop: that
// is not a recovery, or the crash-loop alert would flap.
func TestTelemtUpWaitsForTheCrashLoopFlagToClear(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = false, true })
	defer e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = true, false })
	mg := e.manager(t)
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Agent != nil }, "a status report")
	e.agent.set(func(a *mtprotoAgent) { a.running = true }) // restarted, still flagged
	time.Sleep(500 * time.Millisecond)
	if n := mg.bus.count(events.EventMTProtoTelemtUp); n != 0 {
		t.Fatalf("%d telemt_up events while the agent still reports a crash loop", n)
	}
	e.agent.set(func(a *mtprotoAgent) { a.crashLoop = false })
	waitFor(t, func() bool { return mg.bus.has(events.EventMTProtoTelemtUp) }, "telemt_up once the loop clears")
	time.Sleep(300 * time.Millisecond)
	if n := mg.bus.count(events.EventMTProtoTelemtUp); n != 1 {
		t.Fatalf("%d telemt_up events for one recovery", n)
	}
}

// After a panel restart nothing transitions, so the first healthy report has to
// clear a crash-loop alert left firing.
func TestTheFirstHealthyReportCountsAsTelemtUp(t *testing.T) {
	e := newEnv(t)
	mg := e.manager(t)
	waitFor(t, func() bool { return mg.bus.has(events.EventMTProtoTelemtUp) }, "telemt_up from the first report")
}

// The link's own counter is the shared allowlist's, so it counts every MTProto
// node; the overview needs this node's.
func TestRefusedDestinationsAreCountedPerNode(t *testing.T) {
	e := newEnv(t)
	mg := e.managerWith(t, func(c *Config) { c.WatchEvery = time.Hour })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	en := mg.m.entry(1)
	for i := 0; i < 3; i++ {
		mg.m.onEvent(en, hubnode.Event{Kind: hubnode.EventViolation, Detail: "198.51.100.1:443"})
	}
	v, _ := mg.m.View(1)
	if v.Violations != 3 {
		t.Fatalf("violations = %d, want 3", v.Violations)
	}
	other := &entry{id: 2, cancel: func() {}}
	mg.m.onEvent(other, hubnode.Event{Kind: hubnode.EventViolation})
	if v, _ := mg.m.View(1); v.Violations != 3 {
		t.Fatalf("another node's refusal was counted here: %d", v.Violations)
	}
}

// listHook runs a step right after the sync's List, before it acts on the rows.
type listHook struct {
	Store
	after atomic.Pointer[func()]
}

func (h *listHook) List(ctx context.Context) ([]*LiteNode, error) {
	rows, err := h.Store.List(ctx)
	if f := h.after.Swap(nil); f != nil {
		(*f)()
	}
	return rows, err
}

func hooked(h **listHook) func(*Config) {
	return func(c *Config) {
		c.Tick = time.Hour
		*h = &listHook{Store: c.Store}
		c.Store = *h
	}
}

// An install's Ensure that lands while a sync is running must not be pruned by it.
func TestANodeStartedDuringASyncSurvivesIt(t *testing.T) {
	e := newEnv(t)
	var h *listHook
	mg := e.managerWith(t, hooked(&h))
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	ctx := context.Background()
	fresh := func() {
		row := e.row
		row.NodeID = 2
		mg.st.Save(ctx, &row)
		if err := mg.m.Ensure(ctx, 2); err != nil {
			t.Error(err)
		}
	}
	h.after.Store(&fresh)
	if err := mg.m.sync(ctx); err != nil {
		t.Fatal(err)
	}
	if !mg.m.IsRunning(2) {
		t.Fatal("the sync dropped the node an install had just started")
	}
}

// A rotation saved between the sync's read and its rewire must not be undone by the stale row.
func TestARewireUsesTheCredentialsSavedSinceTheSyncRead(t *testing.T) {
	e := newEnv(t)
	var h *listHook
	mg := e.managerWith(t, hooked(&h))
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	ctx := context.Background()
	token := strings.Repeat("ab", 32)
	rotate := func() {
		r, _ := mg.st.Get(ctx, 1)
		r.Token = token
		mg.st.Save(ctx, r)
		if err := mg.m.Ensure(ctx, 1); err != nil {
			t.Error(err)
		}
		mg.wans.mu.Lock()
		mg.wans.set = []WAN{{Slot: "domestic", IfName: "eth0", Pin: 1}}
		mg.wans.mu.Unlock()
	}
	h.after.Store(&rotate)
	if err := mg.m.sync(ctx); err != nil {
		t.Fatal(err)
	}
	en := mg.m.entry(1)
	if en == nil {
		t.Fatal("the node is not running")
	}
	en.mu.Lock()
	got := en.lite.Token
	en.mu.Unlock()
	if got != token {
		t.Fatalf("the rewired node runs on the old token")
	}
}

// flakyList fails the first n Lists, as a locked database would.
type flakyList struct {
	Store
	fails atomic.Int32
}

func (f *flakyList) List(ctx context.Context) ([]*LiteNode, error) {
	if f.fails.Add(-1) >= 0 {
		return nil, errors.New("database is locked")
	}
	return f.Store.List(ctx)
}

func TestAFailedFirstSyncIsRetriedOnTheTick(t *testing.T) {
	e := newEnv(t)
	st := newMemStore()
	st.Save(context.Background(), &e.row)
	fl := &flakyList{Store: st}
	fl.fails.Store(1)
	m := NewManager(Config{
		Store: fl, Nodes: &fakeNodes{node: nodeDomain.Node{ID: 1, Name: "vps-1", IP: "127.0.0.1", Kind: nodeDomain.NodeKindLite, IsActive: true}},
		Dialers: plainDialers(), Allow: &tgnet.Refresher{List: tgnet.New(nil)},
		ConnsPerWAN: 1, Tick: 100 * time.Millisecond, WatchEvery: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); m.Stop() })
	_ = m.Start(ctx)
	waitFor(t, func() bool { return m.IsRunning(1) }, "a later tick to start the node")
}

// Removing every user stops telemt for good; the crash-loop alert must still clear.
func TestACrashLoopThatEndsWithTelemtStoppedStillClears(t *testing.T) {
	e := newEnv(t)
	e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop = false, true })
	defer e.agent.set(func(a *mtprotoAgent) { a.running, a.crashLoop, a.held = true, false, "" })
	mg := e.manager(t)
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Agent != nil }, "a status report")
	e.agent.set(func(a *mtprotoAgent) { a.crashLoop, a.held = false, "no users" })
	waitFor(t, func() bool { return mg.bus.has(events.EventMTProtoTelemtUp) }, "the crash loop to clear")
	mg.bus.mu.Lock()
	defer mg.bus.mu.Unlock()
	for _, ev := range mg.bus.ev {
		if p := ev.Payload.(events.MTProtoPayload); ev.Type == events.EventMTProtoTelemtUp && p.Detail != "no users" {
			t.Fatalf("payload = %+v, want why telemt is stopped", p)
		}
	}
}

func TestTheCoreStatusOfADownLinkNamesTheNode(t *testing.T) {
	e := newEnv(t)
	var down atomic.Bool
	down.Store(true)
	mg := e.managerWith(t, func(c *Config) { c.Dialers = gatedDialers(&down) })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	cs, err := mg.m.Pusher(1).GetCoreStatus(context.Background(), "mtproto")
	if err != nil || cs.Detail != "the node's link is down" {
		t.Fatalf("status = %+v err=%v", cs, err)
	}
}

// A VPS can invent user names every poll: only names the panel pushed may get a baseline row.
func TestOnlyPushedUsersGetABaseline(t *testing.T) {
	e := newEnv(t)
	long := strings.Repeat("x", 300)
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice", OctetsUp: 10}, {Name: "ghost", OctetsUp: 5}, {Name: long, OctetsUp: 1}}
	})
	defer e.agent.set(func(a *mtprotoAgent) { a.users = nil })
	mg := e.manager(t)
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link")
	time.Sleep(400 * time.Millisecond) // several polls before anything was pushed
	if b, _ := mg.st.Baseline(context.Background(), 1); len(b) != 0 {
		t.Fatalf("saved before anything was pushed: %v", keys(b))
	}
	mg.push(t, "alice")
	e.agent.set(func(a *mtprotoAgent) {
		a.users = []ctrl.UserStat{{Name: "alice", OctetsUp: 30}, {Name: "ghost", OctetsUp: 50}, {Name: "ghost-2", OctetsUp: 1}, {Name: long, OctetsUp: 9}}
	})
	waitFor(t, func() bool { up, _, _ := mg.nodes.usage(); return up == 20 }, "alice's traffic to be booked")
	waitFor(t, func() bool { b, _ := mg.st.Baseline(context.Background(), 1); return b["alice"].Up == 30 }, "alice's baseline")
	if b, _ := mg.st.Baseline(context.Background(), 1); len(b) != 1 {
		t.Fatalf("baseline rows for names never pushed: %v", keys(b))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k[:min(len(k), 20)])
	}
	return out
}
