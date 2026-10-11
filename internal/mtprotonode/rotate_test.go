package mtprotonode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/mtproto-remote-plugin/reality"
	"github.com/nasnet-community/mtproto-remote-plugin/realityserver"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// cutDialer keeps its connections so a test can drop them all, as a blip would.
type cutDialer struct {
	mu   sync.Mutex
	raws []net.Conn
}

func (c *cutDialer) dial(ctx context.Context, n, a string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, n, a)
	if err == nil {
		c.mu.Lock()
		c.raws = append(c.raws, conn)
		c.mu.Unlock()
	}
	return conn, err
}

func (c *cutDialer) cut() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.raws {
		conn.Close()
	}
	c.raws = nil
}

func (c *cutDialer) dialers() Dialers {
	return Dialers{Link: func(*WAN) link.DialFunc { return c.dial }, DC: c.dial}
}

// keyAgent switches keys on keys.rotate like the real agent: the new REALITY key
// only, both tokens until the panel finishes.
type keyAgent struct {
	row  LiteNode
	dest string

	mu      sync.Mutex
	ag      *link.Agent
	ra      *mtprotoAgent
	token   []byte
	rotates [][]byte
	// afterSwitch runs once the n-th rotation is applied, before its answer
	afterSwitch func(n int)
}

func newKeyAgent(t *testing.T) *keyAgent {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cover := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cover.EnableHTTP2 = true
	cover.Config.ErrorLog = log.New(io.Discard, "", 0)
	cover.StartTLS()
	t.Cleanup(cover.Close)
	priv, pub, _ := reality.GenerateKey()
	sid := []byte{4, 2}
	k := &keyAgent{dest: cover.Listener.Addr().String(), token: make([]byte, 32)}
	rand.Read(k.token)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ra := &mtprotoAgent{running: true, checkOK: true}
	k.ra = ra
	hs := ra.handlers()
	hs[ctrl.MKeysRotate] = k.rotate
	k.ag = link.NewAgent(link.AgentConfig{AgentID: "agent-1", Tokens: [][]byte{k.token}, Listener: ln, Reality: k.server(priv, sid), Handlers: hs})
	go k.ag.Serve(ctx)
	t.Cleanup(k.ag.Close)
	for i := 0; !k.ag.Warm() && i < 300; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	k.row = LiteNode{
		NodeID: 1, AgentID: "agent-1", HubPort: ln.Addr().(*net.TCPAddr).Port, CapturePort: 9443, SSHPort: 22, SSHUser: "root",
		RealityPub: hex.EncodeToString(pub), ShortID: hex.EncodeToString(sid), Token: hex.EncodeToString(k.token),
		Disguise: "example.com", MaxStreams: 100,
	}
	return k
}

func (k *keyAgent) server(priv, sid []byte) realityserver.ServerConfig {
	var s8 [8]byte
	copy(s8[:], sid)
	return realityserver.ServerConfig{Dest: k.dest, ServerNames: []string{"example.com"}, PrivateKey: priv, ShortIDs: [][8]byte{s8}, MaxTimeDiff: time.Minute}
}

func (k *keyAgent) rotate(_ context.Context, b json.RawMessage, _ io.Reader) (any, error) {
	var req ctrl.KeysRotate
	json.Unmarshal(b, &req)
	if req.Finish {
		return nil, nil
	}
	k.ag.SetTokens([][]byte{req.Token, k.token})
	k.ag.Reconfigure(k.server(req.PrivateKey, req.ShortIDs[0]))
	k.mu.Lock()
	k.rotates = append(k.rotates, req.Token)
	n, after := len(k.rotates)-1, k.afterSwitch
	k.mu.Unlock()
	if after != nil {
		after(n)
	}
	return nil, nil
}

func (k *keyAgent) seen() [][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([][]byte(nil), k.rotates...)
}

// sharedDB is one in-memory database a restarted manager finds again.
func sharedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&LiteNode{}, &UsageBaseline{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// panelOver is a manager and the MTProto node service over the hub's own store and links.
func panelOver(t *testing.T, st Store, d Dialers, linkWait time.Duration) (*Manager, *relayops.Service, func()) {
	t.Helper()
	m := NewManager(Config{
		Store: st, Dialers: d,
		Nodes:       &fakeNodes{node: nodeDomain.Node{ID: 1, Name: "vps-1", IP: "127.0.0.1", Kind: nodeDomain.NodeKindLite, IsActive: true}},
		Allow:       &tgnet.Refresher{List: tgnet.NewWithPorts([]netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, 443)},
		ConnsPerWAN: 1, Tick: time.Hour, WatchEvery: 100 * time.Millisecond, StatusEvery: 100 * time.Millisecond, StatsEvery: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); m.Stop() }) }
	t.Cleanup(stop)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	svc := relayops.NewService(relayops.ServiceConfig{Store: NewRecordStore(st), Links: m.Links(), LinkWait: linkWait})
	return m, svc, stop
}

func linkUp(m *Manager) bool { v, ok := m.View(1); return ok && v.Snapshot.Link.Up > 0 }

func waitJobDone(t *testing.T, svc *relayops.Service, id string) relayops.JobView {
	t.Helper()
	var v relayops.JobView
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if v, _ = svc.Job(id); v.State != relayops.JobRunning {
			return v
		}
	}
	t.Fatalf("the job never finished: %+v", v)
	return v
}

func TestARotationWhoseReplyIsLostIsFinishedThroughTheHubAdapters(t *testing.T) {
	k := newKeyAgent(t)
	cd := &cutDialer{}
	k.afterSwitch = func(n int) {
		if n == 0 {
			cd.cut() // applied, and the answer is lost with the connection
		}
	}
	st := NewStore(sharedDB(t))
	if err := st.Insert(context.Background(), &k.row); err != nil {
		t.Fatal(err)
	}
	m, svc, _ := panelOver(t, st, cd.dialers(), 30*time.Second)
	waitFor(t, func() bool { return linkUp(m) }, "the link")

	id, err := svc.StartRotate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if v := waitJobDone(t, svc, id); v.State != relayops.JobOK {
		t.Fatalf("job = %+v", v)
	}
	seen := k.seen()
	if len(seen) != 2 || !bytes.Equal(seen[0], seen[1]) {
		t.Fatalf("the agent saw %d rotations; the repeat must be the same one", len(seen))
	}
	after, _ := st.Get(context.Background(), 1)
	if after.Token != hex.EncodeToString(seen[0]) || after.NextSeed != "" {
		t.Fatalf("stored = %+v", after)
	}
	waitFor(t, func() bool { return linkUp(m) }, "the link on the new credentials")
}

func TestARotationLostAcrossAPanelRestartIsFinishedAfterIt(t *testing.T) {
	k := newKeyAgent(t)
	applied, release := make(chan struct{}), make(chan struct{})
	k.afterSwitch = func(n int) {
		if n == 0 {
			close(applied)
			<-release // the panel goes away before this answer
		}
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	db := sharedDB(t)
	st := NewStore(db)
	if err := st.Insert(context.Background(), &k.row); err != nil {
		t.Fatal(err)
	}
	d := (&cutDialer{}).dialers()
	m, svc, stop := panelOver(t, st, d, time.Second)
	waitFor(t, func() bool { return linkUp(m) }, "the link")
	id, err := svc.StartRotate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-applied:
	case <-time.After(20 * time.Second):
		t.Fatal("the agent never got the rotation")
	}
	stop()
	if v := waitJobDone(t, svc, id); v.State != relayops.JobFailed {
		t.Fatalf("job = %+v", v)
	}
	close(release)
	pending, _ := st.Get(context.Background(), 1)
	if pending.NextSeed == "" || pending.Token != k.row.Token {
		t.Fatalf("the pending rotation was not kept: %+v", pending)
	}

	// a fresh panel: the agent takes only the new keys, so the link needs the stored seed
	m2, svc2, _ := panelOver(t, NewStore(db), d, 30*time.Second)
	waitFor(t, func() bool { return linkUp(m2) }, "the link on the pending credentials")
	id, err = svc2.StartRotate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if v := waitJobDone(t, svc2, id); v.State != relayops.JobOK {
		t.Fatalf("job after the restart = %+v", v)
	}
	seen := k.seen()
	if len(seen) != 2 || !bytes.Equal(seen[0], seen[1]) {
		t.Fatalf("the agent saw %d rotations; the restart must resend the same one", len(seen))
	}
	after, _ := st.Get(context.Background(), 1)
	if after.Token != hex.EncodeToString(seen[0]) || after.NextSeed != "" {
		t.Fatalf("stored = %+v", after)
	}
	waitFor(t, func() bool { return linkUp(m2) }, "the link on the new credentials")
}
