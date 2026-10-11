package mtprotonode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// Nodes is the node usecase's shared health and traffic writers, so history, alerts and quotas just work.
type Nodes interface {
	GetNode(ctx context.Context, id uint) (*nodeDomain.Node, error)
	ObserveNodeHealth(ctx context.Context, node *nodeDomain.Node, online bool, reason, detail string)
	IngestTraffic(ctx context.Context, nodeID uint, records []*agent.TrafficRecord, inTx func(ctx context.Context) error) (int64, error)
	// RecordNodeStat keeps the history the node page charts; the stats sweep never sees these nodes.
	RecordNodeStat(ctx context.Context, stat *nodeDomain.NodeStat) error
}

// WANSource lists the domestic uplinks connections can be pinned to.
type WANSource interface {
	DomesticWANs(ctx context.Context) ([]WAN, error)
}

// Publisher is the event bus.
type Publisher interface{ Publish(events.Event) }

type Config struct {
	Store   Store
	Nodes   Nodes
	WANs    WANSource // nil: one unpinned set of connections
	Dialers Dialers
	Allow   *tgnet.Refresher
	Bus     Publisher
	// Reconcile is the core usecase's ReconcileNodeByID.
	Reconcile   func(ctx context.Context, nodeID uint) error
	ConnsPerWAN int           // default 2
	Tick        time.Duration // how often to look for new, removed or re-wired nodes (30 s)
	WatchEvery  time.Duration // how often a node's health is reported (10 s)
	StatusEvery time.Duration // agent status poll (hubnode default 10 s)
	StatsEvery  time.Duration // usage poll (hubnode default 30 s)
	// DisguiseEvery is the cover-site re-check (hubnode default 6 h).
	DisguiseEvery time.Duration
	// Ranges a cover site should sit in; nil means never warn.
	Ranges func() Ranges
	Log    logrus.FieldLogger
}

// Manager runs one hubnode.Node per lite node.
type Manager struct {
	cfg Config

	mu      sync.Mutex
	ctx     context.Context
	entries map[uint]*entry
	turns   map[uint]*sync.Mutex // per node: a start and a usage booking never overlap
	refused map[uint]uint64      // the link's own count is the shared allowlist's, for every node
	feeds   map[uint]*feed       // per node, not per entry: a restart keeps what happened
	since   time.Time
	wg      sync.WaitGroup
}

type entry struct {
	id      uint
	node    *hubnode.Node
	ctx     context.Context
	cancel  context.CancelFunc
	wanSig  string
	started time.Time
	feed    *feed

	mu           sync.Mutex
	name         string // the node's display name, for events
	lite         LiteNode
	inboundID    string
	desired      *ctrl.State     // what the panel last asked for, pushed or not
	pushed       map[string]bool // every user name the panel has handed this node; nil until the first
	coverPending string
	vetted       string // a cover site that just passed its check
	vettedAt     time.Time
	looping      *bool // the agent's crash-loop flag, as of the last status
	bursts       map[events.EventType]*burst
	seen         time.Time // when lite's versions/last-seen were last written

	// the link's counters at the last status, by the sample's clock
	ctrAt          time.Time
	ctrTo, ctrFrom uint64
	upBps, downBps uint64
	rateKnown      bool
	rateSeen       time.Time // wall clock: polls that stop leave no stale rate
	statAt         time.Time // the last history row, by the sample's clock
	active         int       // pushed users who moved traffic in the last usage report
	activeAt       time.Time
}

// gone: a replaced entry's last callbacks must not land in the node's feed
func (e *entry) gone() bool { return e.ctx != nil && e.ctx.Err() != nil }

// wantLocked records a state the panel is about to push, on the node's current stream limit
// and stop: one saved while a cover check ran must not be lost.
func (e *entry) wantLocked(s ctrl.State) ctrl.State {
	if s.MaxStreams != e.lite.MaxStreams || s.Stopped != e.lite.ProxyStopped {
		s.MaxStreams, s.Stopped = e.lite.MaxStreams, e.lite.ProxyStopped
		s.Rev = Rev(s)
	}
	e.desired = &s
	if e.pushed == nil {
		e.pushed = map[string]bool{}
	}
	for _, u := range s.Users {
		e.pushed[u.Name] = true
	}
	return s
}

func NewManager(cfg Config) *Manager {
	if cfg.ConnsPerWAN == 0 {
		cfg.ConnsPerWAN = 2
	}
	if cfg.Tick == 0 {
		cfg.Tick = 30 * time.Second
	}
	if cfg.WatchEvery == 0 {
		cfg.WatchEvery = 10 * time.Second
	}
	// hubnode's own defaults, spelled out: freshness is judged against them
	if cfg.StatusEvery == 0 {
		cfg.StatusEvery = 10 * time.Second
	}
	if cfg.StatsEvery == 0 {
		cfg.StatsEvery = 30 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = logrus.New()
	}
	return &Manager{cfg: cfg, entries: map[uint]*entry{}, turns: map[uint]*sync.Mutex{}, refused: map[uint]uint64{},
		feeds: map[uint]*feed{}, since: time.Now()}
}

// feed is a node's activity, made on first use.
func (m *Manager) feed(id uint) *feed {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.feeds[id]
	if f == nil {
		f = &feed{}
		m.feeds[id] = f
	}
	return f
}

func (m *Manager) turn(id uint) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.turns[id]
	if t == nil {
		t = &sync.Mutex{}
		m.turns[id] = t
	}
	return t
}

// Start launches every lite node and keeps the set current until ctx ends.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx, m.since = ctx, time.Now()
	m.mu.Unlock()
	// one failed read at boot must not leave every node undriven until a restart
	if err := m.sync(ctx); err != nil {
		m.cfg.Log.WithError(err).Warn("MTProto nodes: first sync failed; retrying on the tick")
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(m.cfg.Tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := m.sync(ctx); err != nil {
					m.cfg.Log.WithError(err).Warn("MTProto nodes: sync failed")
				}
			}
		}
	}()
	return nil
}

// Stop ends every node and waits for them.
func (m *Manager) Stop() {
	m.mu.Lock()
	for _, e := range m.entries {
		e.cancel()
	}
	m.entries = map[uint]*entry{}
	m.mu.Unlock()
	m.wg.Wait()
}

func wanSig(ws []WAN) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = fmt.Sprintf("%s/%s/%d", w.Slot, w.IfName, w.Pin)
	}
	return strings.Join(parts, ",")
}

func (m *Manager) wans(ctx context.Context) ([]WAN, bool) {
	if m.cfg.WANs == nil {
		return nil, true
	}
	ws, err := m.cfg.WANs.DomesticWANs(ctx)
	if err != nil {
		return nil, false
	}
	return ws, true
}

func (m *Manager) sync(ctx context.Context) error {
	// only these may be pruned: an install's Ensure can land after the List
	m.mu.Lock()
	before := make(map[uint]*entry, len(m.entries))
	for id, e := range m.entries {
		before[id] = e
	}
	m.mu.Unlock()
	rows, err := m.cfg.Store.List(ctx)
	if err != nil {
		return err
	}
	ws, wansOK := m.wans(ctx)
	sig := wanSig(ws)
	// a WAN set we could not read is not a reason to bounce a working node
	current := func(e *entry) bool { return !wansOK || e.wanSig == sig }
	want := map[uint]bool{}
	for _, row := range rows {
		want[row.NodeID] = true
		if e := m.entry(row.NodeID); e != nil && current(e) {
			continue
		}
		if err := m.start(ctx, row.NodeID, ws, sig, current); err != nil {
			log := m.cfg.Log.WithError(err).WithField("node_id", row.NodeID)
			if errors.Is(err, ErrDisabled) || errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrNotFound) {
				log.Debug("MTProto nodes: node not started")
			} else {
				log.Warn("MTProto nodes: could not start node")
			}
		}
	}
	m.mu.Lock()
	for id, e := range before {
		if !want[id] && m.entries[id] == e {
			e.cancel()
			delete(m.entries, id)
			delete(m.feeds, id) // its row is gone
		}
	}
	m.mu.Unlock()
	return nil
}

// Ensure (re)starts one node from its stored row, after an install or repair.
func (m *Manager) Ensure(ctx context.Context, nodeID uint) error {
	ws, _ := m.wans(ctx)
	return m.start(ctx, nodeID, ws, wanSig(ws), nil)
}

// drop stops an entry unless a newer one has replaced it.
func (m *Manager) drop(e *entry) {
	m.mu.Lock()
	if m.entries[e.id] == e {
		delete(m.entries, e.id)
	}
	m.mu.Unlock()
	e.cancel()
}

// Remove stops a node and forgets it.
func (m *Manager) Remove(nodeID uint) {
	m.halt(nodeID)
	m.mu.Lock()
	delete(m.feeds, nodeID)
	m.mu.Unlock()
}

// halt stops a node but keeps its feed, for when it is enabled again.
func (m *Manager) halt(nodeID uint) {
	m.mu.Lock()
	delete(m.refused, nodeID)
	if e := m.entries[nodeID]; e != nil {
		e.cancel()
		delete(m.entries, nodeID)
	}
	m.mu.Unlock()
}

// start builds a node's entry; keep, when set, leaves a running entry it accepts alone.
func (m *Manager) start(ctx context.Context, id uint, ws []WAN, sig string, keep func(*entry) bool) error {
	turn := m.turn(id)
	turn.Lock()
	defer turn.Unlock()
	if e := m.entry(id); e != nil && keep != nil && keep(e) {
		return nil // an Ensure got here first
	}
	// read under the turn: credentials a rotation saved since the caller looked must win
	row, err := m.cfg.Store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			m.Remove(id)
		}
		return err
	}
	node, err := m.cfg.Nodes.GetNode(ctx, row.NodeID)
	if err == nil && !node.IsActive {
		err = ErrDisabled
	}
	if err != nil {
		if errors.Is(err, ErrDisabled) {
			m.halt(row.NodeID)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			m.Remove(row.NodeID)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// the nodes row goes first on removal; this finishes one that stopped halfway
			if derr := m.cfg.Store.Delete(ctx, row.NodeID); derr != nil {
				m.cfg.Log.WithError(derr).WithField("node_id", row.NodeID).Warn("MTProto nodes: could not forget a node whose row is gone")
			}
		}
		return err
	}
	cr, err := row.record().LinkCreds()
	if err != nil {
		return err
	}
	// with the turn held the old entry is not mid-booking, and cancelled it never books again
	m.mu.Lock()
	if old := m.entries[row.NodeID]; old != nil {
		old.cancel()
		delete(m.entries, row.NodeID)
	}
	m.mu.Unlock()
	base, err := m.cfg.Store.Baseline(ctx, row.NodeID)
	if err != nil {
		return err
	}

	var conns []link.ConnSpec
	if len(ws) == 0 {
		for i := 0; i < m.cfg.ConnsPerWAN; i++ {
			conns = append(conns, link.ConnSpec{Name: fmt.Sprintf("default-%d", i), Dial: m.cfg.Dialers.Link(nil)})
		}
	}
	for _, w := range ws {
		w := w
		for i := 0; i < m.cfg.ConnsPerWAN; i++ {
			conns = append(conns, link.ConnSpec{Name: fmt.Sprintf("%s-%d", w.Slot, i), Dial: m.cfg.Dialers.Link(&w)})
		}
	}

	nctx, cancel := context.WithCancel(m.ctxOr(ctx))
	e := &entry{id: row.NodeID, ctx: nctx, cancel: cancel, wanSig: sig, lite: *row, name: node.Name, started: time.Now(), feed: m.feed(row.NodeID)}

	e.node = hubnode.New(hubnode.Config{
		Link: link.HubConfig{
			AgentID: row.AgentID, Token: cr.Token, Reality: cr.Reality,
			Addr:       net.JoinHostPort(node.IP, strconv.Itoa(row.HubPort)),
			Conns:      conns,
			Allow:      m.cfg.Allow.List,
			DialDC:     m.cfg.Dialers.DC,
			MaxStreams: max(row.MaxStreams, MaxStreamLimit), // the agent holds the node's own limit and takes a new one live
		},
		Baseline:      base,
		StatusEvery:   m.cfg.StatusEvery,
		StatsEvery:    m.cfg.StatsEvery,
		DisguiseEvery: m.cfg.DisguiseEvery,
		OnUsage:       func(r hubnode.UsageReport) error { return m.onUsage(nctx, e, r) },
		OnApplied:     func(s ctrl.State) { m.onApplied(nctx, e, s) },
		OnEvent:       func(ev hubnode.Event) { m.onEvent(e, ev) },
		OnStatus:      func(s hubnode.Snapshot) { m.onStatus(nctx, e, s) },
	})
	e.node.SetCreds(cr) // the config has no room for a pending rotation's set

	m.mu.Lock()
	m.entries[row.NodeID] = e
	m.mu.Unlock()

	m.wg.Add(2)
	go func() { defer m.wg.Done(); _ = e.node.Run(nctx) }()
	go func() { defer m.wg.Done(); m.watch(nctx, e) }()
	if m.cfg.Reconcile != nil {
		go func() {
			if err := m.cfg.Reconcile(nctx, row.NodeID); err != nil {
				m.cfg.Log.WithError(err).WithField("node_id", row.NodeID).Debug("MTProto nodes: first reconcile pending")
				return
			}
			m.noTunnel(nctx, e)
		}()
	}
	return nil
}

// noTunnel: a tunnel deleted while the panel was down has no teardown, so ask for no users.
func (m *Manager) noTunnel(ctx context.Context, e *entry) {
	if ctx.Err() != nil {
		return
	}
	// held across SetDesired, so a tunnel's push can't land before this one
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.desired != nil {
		return
	}
	st := ctrl.State{Port: 443, Disguise: e.lite.Disguise, MaxStreams: e.lite.MaxStreams, Stopped: e.lite.ProxyStopped}
	for _, pf := range m.cfg.Allow.List.Prefixes() {
		st.DCNets = append(st.DCNets, pf.String())
	}
	st.Rev = Rev(st)
	e.desired = &st
	e.node.SetDesired(st)
}

func (m *Manager) ctxOr(ctx context.Context) context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil {
		return m.ctx
	}
	return ctx
}

// watch reports health either way: the writer debounces outages.
func (m *Manager) watch(ctx context.Context, e *entry) {
	t := time.NewTicker(m.cfg.WatchEvery)
	defer t.Stop()
	for {
		m.observe(ctx, e)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Manager) observe(ctx context.Context, e *entry) {
	node, err := m.cfg.Nodes.GetNode(ctx, e.id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			m.drop(e)
		}
		return
	}
	if !node.IsActive {
		m.drop(e) // disabled on purpose: no health to report until it is enabled
		return
	}
	e.mu.Lock()
	e.name = node.Name
	e.mu.Unlock()
	snap := e.node.Snapshot()
	if snap.Link.Up > 0 {
		m.noteLink(ctx, e, true, "", false)
		m.cfg.Nodes.ObserveNodeHealth(ctx, node, true, nodeDomain.NodeOnlineReasonAgentReachable, "")
		return
	}
	detail, failed := "no link connection", false
	for _, c := range snap.Link.Conns {
		if c.LastErr != "" {
			detail, failed = c.LastErr, true
			break
		}
	}
	m.noteLink(ctx, e, false, detail, failed)
	m.cfg.Nodes.ObserveNodeHealth(ctx, node, false, nodeDomain.NodeOfflineReasonAgentUnreachable, detail)
}

// dialGrace is how long a new entry may dial before an unanswered link counts as down.
const dialGrace = 3 * time.Second

// noteLink feeds the link's state to the node's feed; a fresh entry still dialling has nothing to report.
func (m *Manager) noteLink(ctx context.Context, e *entry, up bool, why string, failed bool) {
	if ctx.Err() != nil {
		return // a replaced entry's link going quiet is not an outage
	}
	if !up && !failed && time.Since(e.started) < max(2*m.cfg.WatchEvery, dialGrace) {
		return
	}
	e.feed.linkSeen(up, why)
}

func (m *Manager) onStatus(ctx context.Context, e *entry, s hubnode.Snapshot) {
	m.sample(ctx, e, s)
	if s.Agent == nil {
		return
	}
	e.feed.proxySeen(s.Agent)
	// the first report counts as a change; a loop ending with telemt stopped (no users) clears too
	looping := s.Agent.Telemt.CrashLoop
	e.mu.Lock()
	prev := e.looping
	e.looping = &looping
	e.mu.Unlock()
	if !looping && (prev == nil || *prev) && m.cfg.Bus != nil {
		var ev hubnode.Event
		if !s.Agent.Telemt.Running {
			ev.Detail = s.Agent.Telemt.Held
			if ev.Detail == "" {
				ev.Detail = "no reason given"
			}
		}
		m.publish(e, events.EventMTProtoTelemtUp, ev, 1)
	}
	e.mu.Lock()
	changed := e.lite.AgentVersion != s.Agent.AgentVersion || e.lite.TelemtVersion != s.Agent.TelemtVersion
	stale := time.Since(e.seen) > 5*time.Minute
	if changed {
		e.lite.AgentVersion, e.lite.TelemtVersion = s.Agent.AgentVersion, s.Agent.TelemtVersion
	}
	if !changed && !stale {
		e.mu.Unlock()
		return
	}
	now := time.Now()
	e.lite.LastSeenAt, e.seen = &now, now
	agentVer, telemtVer := e.lite.AgentVersion, e.lite.TelemtVersion
	e.mu.Unlock()
	if err := m.cfg.Store.UpdateObserved(ctx, e.id, agentVer, telemtVer, now); err != nil {
		m.cfg.Log.WithError(err).Warn("MTProto nodes: could not record the agent's versions")
	}
}

// statEvery is the most often a node's history gets a row.
const statEvery = 10 * time.Second

// sample turns the link's byte counters into rates, and keeps a history row every statEvery.
func (m *Manager) sample(ctx context.Context, e *entry, s hubnode.Snapshot) {
	at := s.AgentAt
	if at.IsZero() {
		at = time.Now()
	}
	to, from := s.Link.BytesToDC, s.Link.BytesFromDC
	e.mu.Lock()
	switch {
	case e.ctrAt.IsZero():
	case to < e.ctrTo || from < e.ctrFrom:
		e.rateKnown = false // a new link counts from zero: start over rather than go negative
	case at.After(e.ctrAt):
		dt := at.Sub(e.ctrAt).Seconds()
		e.upBps, e.downBps = uint64(float64(to-e.ctrTo)/dt), uint64(float64(from-e.ctrFrom)/dt)
		e.rateKnown, e.rateSeen = true, time.Now()
	}
	e.ctrAt, e.ctrTo, e.ctrFrom = at, to, from
	var row *nodeDomain.NodeStat
	if e.rateKnown && at.Sub(e.statAt) >= statEvery {
		e.statAt = at
		row = &nodeDomain.NodeStat{
			NodeID: e.id, UpSpeed: e.upBps, DownSpeed: e.downBps, TcpCount: uint64(max(s.Link.Streams, 0)),
			CreatedAt: at.UTC(),
		}
		if n := m.activeLocked(e); n != nil {
			row.OnlineUsers = *n
		}
	}
	e.mu.Unlock()
	if row == nil || ctx.Err() != nil {
		return
	}
	if err := m.cfg.Nodes.RecordNodeStat(ctx, row); err != nil {
		m.cfg.Log.WithError(err).WithField("node_id", e.id).Debug("MTProto node: could not record its stats")
	}
}

// activeLocked is nil once no usage report came for three polls: an old count would mislead.
func (m *Manager) activeLocked(e *entry) *int {
	if e.activeAt.IsZero() || time.Since(e.activeAt) > 3*m.cfg.StatsEvery {
		return nil
	}
	n := e.active
	return &n
}

// onApplied moves the SNI with a new cover site, or the agent would take us for a stranger.
func (m *Manager) onApplied(ctx context.Context, e *entry, s ctrl.State) {
	e.mu.Lock()
	// a state superseded while it was in flight is not what the node runs for long
	if e.desired == nil || e.desired.Rev == s.Rev {
		e.feed.applied(s.Rev, len(s.Users))
	}
	if s.Disguise == "" || s.Disguise == e.lite.Disguise {
		e.mu.Unlock()
		return
	}
	e.lite.Disguise = s.Disguise
	row := e.lite
	e.mu.Unlock()
	if cr, err := row.record().LinkCreds(); err == nil {
		e.node.SetCreds(cr)
	}
	if err := m.cfg.Store.UpdateDisguise(ctx, e.id, s.Disguise); err != nil {
		m.cfg.Log.WithError(err).Warn("MTProto nodes: could not record the new disguise domain")
	}
}

var errNotPushed = errors.New("nothing pushed to this node yet")

// onUsage books and saves the baseline in one transaction, so a failed save can't count twice.
func (m *Manager) onUsage(ctx context.Context, e *entry, r hubnode.UsageReport) error {
	turn := m.turn(e.id)
	turn.Lock()
	defer turn.Unlock()
	if err := ctx.Err(); err != nil {
		return err // a newer entry owns this node's counters
	}
	// a VPS can report any name; only users we gave it get a baseline row
	e.mu.Lock()
	var ours map[string]hubnode.Usage
	if e.pushed != nil {
		ours = make(map[string]hubnode.Usage, min(len(e.pushed), len(r.Cumulative)))
		for name, u := range r.Cumulative {
			if e.pushed[name] {
				ours[name] = u
			}
		}
		e.active = 0
		for name, d := range r.Deltas {
			if e.pushed[name] && d.Up+d.Down > 0 {
				e.active++
			}
		}
		e.activeAt = time.Now()
	}
	e.mu.Unlock()
	save := func(ctx context.Context) error { return m.cfg.Store.SaveBaseline(ctx, e.id, ours) }
	if ours == nil {
		if len(r.Deltas) > 0 {
			return errNotPushed // keep the counters until we know whose they are
		}
		return nil
	}
	if len(r.Deltas) > 0 {
		rec := &agent.TrafficRecord{Timestamp: time.Now().Unix(), UserUplink: map[string]int64{}, UserDownlink: map[string]int64{}}
		for name, d := range r.Deltas {
			rec.TotalUplink += int64(d.Up)
			rec.TotalDownlink += int64(d.Down)
			// a VPS can report any name; only users we gave it are charged
			e.mu.Lock()
			ours := e.pushed[name]
			e.mu.Unlock()
			if !ours {
				m.cfg.Log.WithField("node_id", e.id).WithField("user", name).Debug("MTProto nodes: usage for a user this node was never given")
				continue
			}
			rec.UserUplink[name] += int64(d.Up)
			rec.UserDownlink[name] += int64(d.Down)
			rec.CoreUsage = append(rec.CoreUsage, agent.CoreUsageEntry{Protocol: "mtproto", Email: name, Uplink: int64(d.Up), Downlink: int64(d.Down)})
		}
		_, err := m.cfg.Nodes.IngestTraffic(ctx, e.id, []*agent.TrafficRecord{rec}, save)
		return err
	}
	return save(ctx)
}

func (m *Manager) onEvent(e *entry, ev hubnode.Event) {
	if ev.Kind == hubnode.EventViolation {
		m.mu.Lock()
		m.refused[e.id]++
		m.mu.Unlock()
	}
	if !e.gone() {
		switch ev.Kind {
		case hubnode.EventDisguiseUp:
			e.feed.coverSeen(true, ev.Detail)
		case hubnode.EventDisguiseDown:
			e.feed.coverSeen(false, errText(ev.Err))
		case hubnode.EventPushFailed:
			e.feed.pushFailed(errText(ev.Err))
		}
	}
	var typ events.EventType
	switch ev.Kind {
	case hubnode.EventTelemtDown:
		typ = events.EventMTProtoTelemtDown
	case hubnode.EventCrashLoop:
		typ = events.EventMTProtoTelemtCrashLoop
	case hubnode.EventDialFailure:
		typ = events.EventMTProtoEgressFailed
	case hubnode.EventViolation:
		typ = events.EventMTProtoViolation
	case hubnode.EventPushFailed:
		typ = events.EventMTProtoPushFailed
	case hubnode.EventDisguiseDown:
		typ = events.EventMTProtoDisguiseDown
	case hubnode.EventDisguiseUp:
		typ = events.EventMTProtoDisguiseUp
	default:
		return // telemt_up comes from onStatus, which also sees the crash-loop flag
	}
	if (typ == events.EventMTProtoEgressFailed || typ == events.EventMTProtoViolation) && !m.coalesce(e, typ, ev) {
		return
	}
	m.emit(e, typ, ev, 1)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// emit hands one event, or a burst of n, to the node's feed and the bus.
func (m *Manager) emit(e *entry, typ events.EventType, ev hubnode.Event, n int) {
	switch typ {
	case events.EventMTProtoViolation:
		// the refused destination stays out of the feed: it is a client's choice, not the node's
		e.feed.flood(Event{At: ev.At, Kind: kindRefused, Level: levelWarn, Count: n})
	case events.EventMTProtoEgressFailed:
		why := errText(ev.Err)
		if ev.Detail != "" && !strings.Contains(why, ev.Detail) {
			why = ev.Detail + ": " + why
		}
		e.feed.flood(Event{At: ev.At, Kind: kindEgressFailed, Level: levelWarn, Count: n, Detail: why})
	}
	if m.cfg.Bus != nil {
		m.publish(e, typ, ev, n)
	}
}

// burstEvery is the most often one node publishes one kind of flood event.
const burstEvery = time.Second

type burst struct {
	sent  time.Time
	held  int
	last  hubnode.Event
	timer *time.Timer
}

// coalesce folds a burst into one event a second that carries the count.
func (m *Manager) coalesce(e *entry, typ events.EventType, ev hubnode.Event) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bursts == nil {
		e.bursts = map[events.EventType]*burst{}
	}
	b := e.bursts[typ]
	if b == nil {
		b = &burst{}
		e.bursts[typ] = b
	}
	now := time.Now()
	if b.timer == nil && now.Sub(b.sent) >= burstEvery {
		b.sent = now
		return true
	}
	b.held++
	b.last = ev
	if b.timer == nil {
		b.timer = time.AfterFunc(b.sent.Add(burstEvery).Sub(now), func() { m.flush(e, typ) })
	}
	return false
}

func (m *Manager) flush(e *entry, typ events.EventType) {
	e.mu.Lock()
	b := e.bursts[typ]
	n, ev := b.held, b.last
	b.held, b.timer, b.sent = 0, nil, time.Now()
	e.mu.Unlock()
	if n > 0 {
		m.emit(e, typ, ev, n)
	}
}

func (m *Manager) publish(e *entry, typ events.EventType, ev hubnode.Event, n int) {
	p := events.MTProtoPayload{NodeID: e.id, Detail: ev.Detail}
	if n > 1 {
		p.Count = n
	}
	if ev.Err != nil {
		p.Error = ev.Err.Error()
	}
	e.mu.Lock()
	p.NodeName = e.name
	e.mu.Unlock()
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	m.cfg.Bus.Publish(events.Event{Type: typ, Timestamp: at, Payload: p})
}

// --- views for the API ---

func (m *Manager) entry(id uint) *entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries[id]
}

// ReservedPorts: the link, capture listener, sshd and telemt's metrics.
func (m *Manager) ReservedPorts(ctx context.Context, nodeID uint) []int {
	row, err := m.cfg.Store.Get(ctx, nodeID)
	if err != nil {
		return []int{22, 8443, 9443, 14300}
	}
	return []int{row.SSHPort, row.HubPort, row.CapturePort, 14300}
}

// IsRunning reports whether a node is being driven.
func (m *Manager) IsRunning(id uint) bool { return m.entry(id) != nil }

// View is a node's live state for the UI.
type View struct {
	Lite         LiteNode
	Snapshot     hubnode.Snapshot
	CoverPending string // why the tunnel's cover site is not in use yet
	Violations   uint64
	// nil while unknown
	Users       *int // in the state the panel last asked for
	ActiveUsers *int // of those, the ones who moved traffic in the last usage report
	Throughput  *Throughput
}

// Throughput is bytes per second to Telegram (what users send) and back.
type Throughput struct {
	UpBps   uint64 `json:"up_bps"`
	DownBps uint64 `json:"down_bps"`
}

// measured is what the manager works out itself, each part nil while it can't tell.
func (m *Manager) measured(e *entry) (users, active *int, tp *Throughput) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.desired != nil {
		n := len(e.desired.Users)
		users = &n
	}
	active = m.activeLocked(e)
	if e.rateKnown && time.Since(e.rateSeen) < 3*m.cfg.StatusEvery {
		tp = &Throughput{UpBps: e.upBps, DownBps: e.downBps}
	}
	return
}

func (m *Manager) View(id uint) (View, bool) {
	e := m.entry(id)
	if e == nil {
		return View{}, false
	}
	users, active, tp := m.measured(e)
	e.mu.Lock()
	lite, pending := e.lite, e.coverPending
	e.mu.Unlock()
	m.mu.Lock()
	refused := m.refused[id]
	m.mu.Unlock()
	snap := e.node.Snapshot()
	if snap.Disguise != nil {
		d := *snap.Disguise // the node's copy is shared
		d.Domestic = m.Domestic(d.Addrs)
		snap.Disguise = &d
	}
	return View{Lite: lite, Snapshot: snap, CoverPending: pending, Violations: refused, Users: users, ActiveUsers: active, Throughput: tp}, true
}

// Row is a node's stored row.
func (m *Manager) Row(ctx context.Context, id uint) (*LiteNode, error) {
	return m.cfg.Store.Get(ctx, id)
}

// Rows is every MTProto node the panel has, running or not.
func (m *Manager) Rows(ctx context.Context) ([]*LiteNode, error) { return m.cfg.Store.List(ctx) }

// Events is a node's recent activity, newest first.
func (m *Manager) Events(id uint, limit int) EventFeed {
	m.mu.Lock()
	f, since := m.feeds[id], m.since
	m.mu.Unlock()
	return EventFeed{Since: since, Events: f.list(limit)}
}

// Record adds to a node's feed what happened outside the manager, a finished job.
func (m *Manager) Record(id uint, ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// a node removed meanwhile must not get its feed back
	if _, err := m.cfg.Store.Get(ctx, id); err != nil {
		return
	}
	m.feed(id).add(ev)
}

// The stream limits the panel accepts: the agent's memory budget was measured at the top one.
const (
	MinStreamLimit = 100
	MaxStreamLimit = 5000
)

// Settings are what the operator may change after the install; nil leaves a field alone.
type Settings struct {
	MaxStreams   *int
	SSHPort      *int
	SSHUser      *string
	ProxyStopped *bool
}

// UpdateSettings saves a node's settings; a new stream limit or a stop goes to the agent, which applies it live.
func (m *Manager) UpdateSettings(ctx context.Context, id uint, s Settings) error {
	// the turn SetCreds takes: a start can't build from the row between the write and the entry's update
	turn := m.turn(id)
	turn.Lock()
	defer turn.Unlock()
	if err := m.cfg.Store.UpdateSettings(ctx, id, s); err != nil {
		return err
	}
	e := m.entry(id)
	if e == nil {
		return nil // the next start reads the row
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.SSHPort != nil {
		e.lite.SSHPort = *s.SSHPort
	}
	if s.SSHUser != nil {
		e.lite.SSHUser = *s.SSHUser
	}
	changed := false
	if s.MaxStreams != nil && *s.MaxStreams != e.lite.MaxStreams {
		e.lite.MaxStreams, changed = *s.MaxStreams, true
	}
	if s.ProxyStopped != nil && *s.ProxyStopped != e.lite.ProxyStopped {
		e.lite.ProxyStopped, changed = *s.ProxyStopped, true
	}
	if !changed || e.desired == nil {
		return nil // the first push carries it
	}
	// pushed in the background: a failure shows as drift and in the feed
	e.node.SetDesired(e.wantLocked(*e.desired))
	return nil
}

// Domestic is nil when it can't tell.
func (m *Manager) Domestic(addrs []string) *bool {
	if m.cfg.Ranges == nil {
		return nil
	}
	return domestic(m.cfg.Ranges(), addrs)
}

// CheckCover vets a new cover site from the node before the tunnel is saved.
func (m *Manager) CheckCover(ctx context.Context, id uint, domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	e := m.entry(id)
	current := ""
	if e != nil {
		e.mu.Lock()
		current = e.lite.Disguise
		e.mu.Unlock()
	} else if row, err := m.cfg.Store.Get(ctx, id); err == nil {
		current = row.Disguise
	}
	if domain == "" || domain == current {
		return nil
	}
	if e == nil {
		return fmt.Errorf("%s can't be checked while the node is not connected: %w", domain, ErrNotRunning)
	}
	return m.vetCover(ctx, e, domain)
}

// vetCover asks the agent whether it can use a site as cover.
func (m *Manager) vetCover(ctx context.Context, e *entry, domain string) error {
	e.mu.Lock()
	fresh := e.vetted == domain && time.Since(e.vettedAt) < 2*time.Minute
	e.mu.Unlock()
	if fresh {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res ctrl.DisguiseResult
	if err := e.node.Call(cctx, ctrl.MDisguiseCheck, ctrl.DisguiseCheck{Domain: domain}, &res); err != nil {
		return fmt.Errorf("could not check %s from the node: %w", domain, err)
	}
	if !res.OK {
		return fmt.Errorf("%s can't be used as a cover site from this node: %s", domain, res.Err)
	}
	e.mu.Lock()
	e.vetted, e.vettedAt = domain, time.Now()
	e.mu.Unlock()
	return nil
}

// Call runs a control method on a node's agent.
func (m *Manager) Call(ctx context.Context, id uint, method string, req, resp any) error {
	e := m.entry(id)
	if e == nil {
		return ErrNotRunning
	}
	return e.node.Call(ctx, method, req, resp)
}

// CallRaw streams a raw body after the request, for agent updates.
func (m *Manager) CallRaw(ctx context.Context, id uint, method string, req any, body io.Reader, resp any) error {
	e := m.entry(id)
	if e == nil {
		return ErrNotRunning
	}
	return e.node.Hub().CallRaw(ctx, method, req, body, resp)
}

// Redial proves rotated credentials work by reconnecting with them.
func (m *Manager) Redial(ctx context.Context, id uint) error { return m.Ensure(ctx, id) }

// SetCreds swaps a node's link credentials during a key rotation or after a repair.
func (m *Manager) SetCreds(ctx context.Context, rec *relayops.Record) error {
	// a start reading the row right now would otherwise build over this swap
	turn := m.turn(rec.NodeID)
	turn.Lock()
	defer turn.Unlock()
	e := m.entry(rec.NodeID)
	if e == nil {
		return ErrNotRunning
	}
	e.mu.Lock()
	next := e.lite
	next.RealityPub, next.ShortID, next.Token, next.NextSeed = rec.RealityPub, rec.ShortID, rec.Token, rec.NextSeed
	e.mu.Unlock()
	cr, err := next.record().LinkCreds()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.lite = next
	e.mu.Unlock()
	e.node.SetCreds(cr)
	return m.cfg.Store.UpdateCreds(ctx, rec.NodeID, rec.RealityPub, rec.ShortID, rec.Token, rec.NextSeed)
}

// AdoptCover takes the cover site a repair found on the box, so the redial presents it.
func (m *Manager) AdoptCover(ctx context.Context, nodeID uint, domain string) error {
	// the turn SetCreds takes: a start can't read the row between the write and the swap
	turn := m.turn(nodeID)
	turn.Lock()
	defer turn.Unlock()
	if err := m.cfg.Store.UpdateDisguise(ctx, nodeID, domain); err != nil {
		return err
	}
	e := m.entry(nodeID)
	if e == nil {
		return nil // the next start reads it from the row
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next := e.lite
	next.Disguise = domain
	cr, err := next.record().LinkCreds()
	if err != nil {
		return err
	}
	e.lite.Disguise = domain
	e.node.SetCreds(cr)
	return nil
}

// SSHDial is how jobs reach a VPS: the links' own path, a domestic line in router mode.
func (m *Manager) SSHDial() link.DialFunc {
	if m.cfg.Dialers.Link == nil {
		return nil
	}
	return m.cfg.Dialers.Link(nil)
}

// PrefixesChanged re-pushes so capture rules follow a refreshed allowlist.
func (m *Manager) PrefixesChanged() {
	m.mu.Lock()
	ids := make([]uint, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	ctx := m.ctx
	m.mu.Unlock()
	if m.cfg.Reconcile == nil || ctx == nil {
		return
	}
	for _, id := range ids {
		id := id
		go func() { _ = m.cfg.Reconcile(ctx, id) }()
	}
}

var (
	ErrNotRunning = errors.New("this MTProto node is not running")
	ErrDisabled   = errors.New("this MTProto node is disabled; enable it first")
)
