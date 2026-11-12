// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/subscriptions"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
)

// The router's own entry is an inbound in the bridge that subscriptions
// connect to straight, "This router" on the page. The manager runs it.
var _ api.LocalOps = (*Manager)(nil)

// What the bridge should have for it follows from the state alone: the entry
// is in the bridge's spec while it is switched on and has its keys and a
// target, and its clients are the subscriptions switched on for it that are
// running. So nothing here tells the bridge of a change as it is made. A
// change goes into the state and the bridge's loop is kicked, and the loop
// sends the bridge whatever the state now asks for (see syncLocalClients).
// A subscription that stops at its limit, one that is deleted, a link that is
// replaced: all of them are a different set of clients, and no more.

const (
	// DefaultLocalPort is where the entry listens inside the container. The
	// router forwards its public port there, and the catalog's script names
	// the same number.
	DefaultLocalPort = 8443

	// localPortGrace is how long the router's script gets to open the port,
	// or to move it, before that is called a problem. It runs once a minute.
	localPortGrace = 3 * time.Minute
	// localTargetRetry is how long after no Reality target was found the
	// choice is tried again.
	localTargetRetry = 5 * time.Minute
	// localTargetWait bounds one choice among the candidates.
	localTargetWait = 90 * time.Second
)

// localLive is what the manager keeps of the entry in memory, under Manager.mu
// unless it says otherwise.
type localLive struct {
	token string       // what the bridge takes a change of clients with; made once, never changed
	src   *localSource // its counts, as the usage collector reads them

	facts    routerfacts.Facts // the router's last report
	factsSet bool
	since    time.Time // when it was last switched on, or found on when the hub started
	everOn   bool      // it was on at some time in this run, so its counts are still read
	events   eventRing

	targetErr string    // why no Reality target is in use, "" when one is or none was tried
	choosing  bool      // a choice is under way
	triedAt   time.Time // when the last choice ended
	movingAt  time.Time // when a new port was asked for
	movedFrom uint16    // and the port it had then

	// under bridgeMu: what was last sent to the bridge, as its start time and
	// the hash of the clients, and the last failure to send that was logged
	pushed   string
	pushSaid string
	asksSaid string
}

func newLocalToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("manager: no random bytes for the bridge's token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// localWanted is the clients the entry should have: the subscriptions
// switched on for it that are running, by email.
func localWanted(st model.State) []bridgespec.Client {
	if !st.Local.Enabled {
		return nil
	}
	var out []bridgespec.Client
	for _, c := range st.LocalClients {
		if !c.Enabled {
			continue
		}
		if sub := st.FindSubscription(c.SubscriptionID); sub != nil && sub.Running() {
			out = append(out, bridgespec.Client{Email: nodecfg.SubscriptionEmail(int(c.SubscriptionID)), UUID: c.ClientUUID})
		}
	}
	slices.SortFunc(out, func(a, b bridgespec.Client) int { return strings.Compare(a.Email, b.Email) })
	return out
}

func clientsHash(clients []bridgespec.Client) string {
	h := sha256.New()
	for _, c := range clients {
		fmt.Fprintf(h, "%s=%s\n", c.Email, c.UUID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// localServerName is the name a client of the entry puts in its hello: the
// SNI, or the host of the Reality target when there is none of its own.
func localServerName(l model.Local) string {
	if l.RealitySNI != "" {
		return l.RealitySNI
	}
	return hostOf(l.RealityTarget)
}

// localBlock is the entry as the bridge's spec has it, or nil while it is
// switched off or not ready: without its keys, or with no target yet.
func (m *Manager) localBlock(st model.State) *bridgespec.Local {
	l := st.Local
	if !l.Enabled || !l.HasKeys() || l.RealityTarget == "" {
		return nil
	}
	return &bridgespec.Local{
		ListenAddr:  m.cfg.Addrs.Nodes.IP.String(),
		Port:        uint16(m.cfg.LocalPort),
		PrivateKey:  l.RealityPrivateKey,
		ShortIDs:    []string{l.RealityShortID},
		ServerNames: []string{localServerName(l)},
		Target:      l.RealityTarget,
		Flow:        bridgespec.VisionFlow,
		Clients:     localWanted(st),
		Token:       m.local.token,
	}
}

// syncLocalClients sends the bridge the entry's clients when they are not
// what it was last sent, or when it is another bridge than the one that was
// sent them: one that the supervisor started again holds the clients its spec
// came with, which may be old by now. The bridge's loop calls it after every
// look, holding bridgeMu.
func (m *Manager) syncLocalClients() {
	if m.cfg.Bridge == nil {
		return
	}
	m.mu.RLock()
	st, has := m.bv.status, m.bv.local
	m.mu.RUnlock()
	if !has || st.State != bridgectl.StateRunning || st.Bridge == nil || st.Bridge.Local == nil {
		return
	}
	want := localWanted(m.cfg.Store.Read())
	key := st.Bridge.Started.UTC().Format(time.RFC3339Nano) + "|" + clientsHash(want)
	if key == m.local.pushed {
		return
	}
	res, err := m.cfg.Bridge.SetClients(want)
	if err != nil {
		if said := err.Error(); said != m.local.pushSaid {
			m.local.pushSaid = said
			m.log.Warn("the entry's clients could not be sent to the bridge; it is tried again", "err", err)
		}
		return
	}
	m.local.pushed, m.local.pushSaid = key, ""
	if len(res.Added)+len(res.Removed)+len(res.Replaced) > 0 {
		m.log.Info("the entry's clients changed", "added", len(res.Added), "removed", len(res.Removed),
			"replaced", len(res.Replaced), "connections_closed", res.Closed)
	}
}

// noteLocalFacts takes in what the router's report says of the entry, and
// leaves the router what the entry needs of it.
func (m *Manager) noteLocalFacts(f routerfacts.Facts) {
	m.mu.Lock()
	was, had := m.local.facts.LocalPort, m.local.factsSet
	m.local.facts, m.local.factsSet = f, true
	if f.LocalPort != 0 && f.LocalRules && f.LocalPort != was && (had || m.local.everOn) {
		m.localEventLocked(EventPortOpened, strconv.Itoa(int(f.LocalPort)))
	}
	if m.local.movedFrom != 0 && f.LocalPort != m.local.movedFrom {
		m.local.movedFrom, m.local.movingAt = 0, time.Time{}
	}
	m.mu.Unlock()
	m.syncAsks()
}

// syncAsks leaves the router's script whether to forward a port to the entry
// and the name set by hand, if any. It says the same again on every look at
// the report, which costs nothing while nothing changed and puts the files
// back if they were lost.
func (m *Manager) syncAsks() {
	if m.cfg.Asks == nil {
		return
	}
	l := m.cfg.Store.Read().Local
	host := ""
	if l.Enabled {
		host = l.Host
	}
	err := m.cfg.Asks.SetLocal(l.Enabled)
	if err == nil {
		err = m.cfg.Asks.SetHost(host)
	}
	said := ""
	if err != nil {
		said = err.Error()
	}
	m.mu.Lock()
	changed := said != m.local.asksSaid
	m.local.asksSaid = said
	m.mu.Unlock()
	if changed && err != nil {
		m.log.Warn("what the entry needs of the router could not be left for its script", "err", err)
	}
}

func (m *Manager) localEventLocked(kind, detail string) {
	m.local.events.add(api.NodeEvent{At: m.now().UTC(), Kind: kind, Detail: oneLine(detail)})
}

func (m *Manager) localEvent(kind, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localEventLocked(kind, detail)
}

// LocalEvents is the entry's recent events, newest first. Never nil.
func (m *Manager) LocalEvents() []api.NodeEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.local.events.newestFirst()
}

func addrText(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

// Local is what the manager knows of the entry right now: how it is doing and
// why, what the router says of its own side, and the numbers of its
// subscriptions. It never waits for the bridge or the router.
func (m *Manager) Local() api.LocalLive {
	st := m.cfg.Store.Read()
	m.mu.RLock()
	f := m.local.facts
	out := api.LocalLive{
		CloudName:    f.CloudName,
		PublicAddr:   addrText(f.PublicAddr),
		DomesticAddr: addrText(f.DomesticAddr),
		Port:         int(f.LocalPort),
	}
	out.State, out.Reason, out.Detail = m.localStateLocked(st)
	if m.bv.local && m.bv.up() {
		if up := m.now().Sub(m.bv.status.Bridge.Started); up > 0 {
			out.UptimeSec = int64(up / time.Second)
		}
	}
	m.mu.RUnlock()
	out.Usage = nodeUsage(m.cfg.Usage.Node(usage.RouterID))
	return out
}

// localStateLocked is the entry's state and the reason for it, from what
// would stop it first to what would stop it last.
func (m *Manager) localStateLocked(st model.State) (state, reason, detail string) {
	l, f := st.Local, m.local.facts
	young := m.now().Sub(m.local.since) < localPortGrace
	switch {
	case !l.Enabled:
		return api.LocalOff, "", ""
	case l.RealityTarget == "":
		if m.local.targetErr != "" && !m.local.choosing {
			return api.LocalAttention, api.LocalTargetFailed, m.local.targetErr
		}
		return api.LocalStarting, api.LocalChoosingTarget, ""
	case !m.entry.On:
		return api.LocalAttention, api.LocalEntryOff, ""
	case !f.LocalRules || f.LocalPort == 0:
		if young {
			return api.LocalStarting, api.LocalWaitingRouter, ""
		}
		return api.LocalAttention, api.LocalNoPort, ""
	case m.local.movedFrom != 0 && m.now().Sub(m.local.movingAt) < localPortGrace:
		// a new port was asked for and the router still has the old one
		return api.LocalStarting, api.LocalWaitingRouter, ""
	case l.Host == "" && f.CloudName == "":
		return api.LocalAttention, api.LocalNoAddress, ""
	case !(m.bv.local && m.bv.up()):
		if young {
			return api.LocalStarting, "", ""
		}
		return api.LocalAttention, api.LocalBridgeDown, ""
	}
	return api.LocalWorking, "", ""
}

func (m *Manager) localView() api.Local { return api.ViewLocal(m.cfg.Store.Read(), m) }

// watchLocal reads the entry's counts every PollEvery and sees to it that it
// has a Reality target. When ctx is done it reads them once more and closes
// the hour, so that the usage file gets what the entry moved in it.
func (m *Manager) watchLocal(ctx context.Context) {
	t := time.NewTicker(m.cfg.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			m.pollLocal(last)
			m.local.src.seal()
			m.pollLocal(last)
			cancel()
			return
		case <-t.C:
			m.pollLocal(ctx)
			m.ensureLocalTarget(ctx)
		}
	}
}

// pollLocal has the usage collector read the entry, as it reads a node. Once
// the entry has been on it goes on being read while it is off, so that the
// collector sees it stop answering and the page stops showing it as live.
func (m *Manager) pollLocal(ctx context.Context) {
	on := m.cfg.Store.Read().Local.Enabled
	m.mu.Lock()
	if on {
		m.local.everOn = true
	}
	read := m.local.everOn
	m.mu.Unlock()
	if !read && !m.local.src.pending() {
		return
	}
	// a failure is the entry not answering, which the collector counts itself
	_, _ = m.cfg.Usage.Poll(ctx, usage.RouterID, m.local.src)
}

// localStatus is the bridge's word on the entry, for the usage source.
func (m *Manager) localStatus() (*bridgespec.LocalStatus, time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.bv.local || !m.bv.up() || m.bv.status.Bridge.Local == nil {
		return nil, time.Time{}, false
	}
	return m.bv.status.Bridge.Local, m.bv.status.Bridge.Started, true
}

// ensureLocalTarget starts a choice of Reality target when the entry is on
// and has none, unless one is under way or one failed a short while ago.
func (m *Manager) ensureLocalTarget(ctx context.Context) {
	l := m.cfg.Store.Read().Local
	if !l.Enabled || l.RealityTarget != "" {
		return
	}
	m.mu.Lock()
	wait := m.local.choosing || (!m.local.triedAt.IsZero() && m.now().Sub(m.local.triedAt) < localTargetRetry)
	if !wait {
		m.local.choosing = true
	}
	m.mu.Unlock()
	if wait {
		return
	}
	if !m.spawn(m.chooseLocalTarget) {
		m.mu.Lock()
		m.local.choosing = false
		m.mu.Unlock()
	}
}

// chooseLocalTarget tries the well-known sites from the router and takes the
// best for the entry's Reality target. What it finds wrong is kept for the
// page to say.
func (m *Manager) chooseLocalTarget(ctx context.Context) {
	problem, best := "", ""
	if m.cfg.LocalProbe == nil {
		problem = "This hub can't check Reality targets, so it could not choose one. Set one in Settings."
	} else {
		cctx, cancel := context.WithTimeout(ctx, localTargetWait)
		choice, err := agentops.ProbeTargetsHere(cctx, m.cfg.LocalProbe, nil)
		cancel()
		switch {
		case ctx.Err() != nil:
			m.mu.Lock()
			m.local.choosing = false
			m.mu.Unlock()
			return
		case err != nil:
			problem = oneLine(err.Error())
		case choice.Best == "":
			problem = fmt.Sprintf("None of the %d sites tried works as a Reality target from this router. Set one in Settings.", len(choice.Results))
		default:
			best = choice.Best
		}
	}
	if best != "" {
		err := m.cfg.Store.Update(func(s *model.State) error {
			if !s.Local.Enabled || s.Local.RealityTarget != "" {
				best = "" // switched off, or given a target, while this looked
				return nil
			}
			s.Local.RealityTarget, s.Local.RealitySNI = best, ""
			return nil
		})
		if err != nil {
			problem, best = "The Reality target that was chosen could not be saved.", ""
			m.log.Error("the entry's Reality target could not be saved", "err", err)
		}
	}
	m.mu.Lock()
	m.local.choosing, m.local.triedAt, m.local.targetErr = false, m.now(), problem
	if best != "" {
		m.localEventLocked(EventTargetChanged, best)
	}
	m.mu.Unlock()
	switch {
	case best != "":
		m.log.Info("the entry has its Reality target", "target", best)
		m.kickBridge()
	case problem != "":
		m.log.Warn("no Reality target could be chosen for the entry; it is tried again", "why", problem)
	}
}

// SetLocalEnabled switches the entry on or off. The first time it is switched
// on its REALITY keys are made; a target is chosen in the background, and the
// router's script is asked to open a port, which it does within a minute. Off
// keeps the keys, the target and every subscription's switch, so switching it
// on again gives the links it had.
func (m *Manager) SetLocalEnabled(ctx context.Context, enabled bool) (api.Local, error) {
	var keys agentops.RealityKeys
	if enabled {
		var err error
		if keys, err = agentops.NewRealityKeys(); err != nil {
			return api.Local{}, fmt.Errorf("making the entry's keys: %w", err)
		}
	}
	changed := false
	err := m.cfg.Store.Update(func(s *model.State) error {
		changed = s.Local.Enabled != enabled
		if enabled && !s.Local.HasKeys() {
			s.Local.RealityPrivateKey, s.Local.RealityPublicKey, s.Local.RealityShortID = keys.PrivateKey, keys.PublicKey, keys.ShortID
		}
		s.Local.Enabled = enabled
		return nil
	})
	if err != nil {
		return api.Local{}, err
	}
	if changed {
		m.mu.Lock()
		kind := EventLocalOff
		if enabled {
			kind = EventLocalOn
			m.local.since, m.local.everOn = m.now(), true
			m.local.targetErr, m.local.triedAt = "", time.Time{}
		}
		m.localEventLocked(kind, "")
		m.mu.Unlock()
		m.log.Info("the router's own entry switched", "on", enabled)
	}
	m.syncAsks()
	m.kickBridge()
	if enabled {
		m.ensureLocalTarget(ctx)
	}
	return m.localView(), nil
}

// ChangeLocalSettings changes the name the entry's links carry, its Reality
// target and its server name. A target that is given is tried from the router
// first and refused if it doesn't work; an empty one has the router choose
// again. Each of them is in every link, so every link changes.
func (m *Manager) ChangeLocalSettings(ctx context.Context, req api.LocalSettings) (api.Local, error) {
	cur := m.cfg.Store.Read().Local
	next := cur
	if req.Host != nil {
		next.Host = ""
		if h := strings.TrimSpace(*req.Host); h != "" {
			host, err := model.CleanHost(h)
			if err != nil {
				return api.Local{}, err
			}
			next.Host = host
		}
	}
	custom := cur.RealitySNI != "" && cur.RealitySNI != hostOf(cur.RealityTarget)
	moved := false
	if req.HandshakeTarget != nil {
		next.RealityTarget = ""
		if t := strings.TrimSpace(*req.HandshakeTarget); t != "" {
			target, err := cleanTarget(t)
			if err != nil {
				return api.Local{}, err
			}
			next.RealityTarget = target
		}
		moved = next.RealityTarget != cur.RealityTarget
	}
	switch {
	case req.SNI != nil && strings.TrimSpace(*req.SNI) != "":
		sni, err := cleanSNI(strings.TrimSpace(*req.SNI))
		if err != nil {
			return api.Local{}, err
		}
		next.RealitySNI = sni
	case req.SNI != nil, moved && !custom, next.RealityTarget == "":
		next.RealitySNI = "" // the target's host
	}
	if next.RealityTarget != "" {
		if _, err := netip.ParseAddr(localServerName(next)); err == nil {
			return api.Local{}, invalidf("the Reality target is an address, so the server name has to be given as well")
		}
	}
	if err := next.Validate(); err != nil {
		return api.Local{}, err
	}
	if next == cur {
		return api.Local{}, invalidf("those are its settings already")
	}
	if moved && next.RealityTarget != "" && m.cfg.LocalProbe != nil {
		chk, err := agentops.CheckTargetHere(ctx, m.cfg.LocalProbe, next.RealityTarget)
		if err != nil {
			return api.Local{}, opFailure(err)
		}
		if !chk.OK {
			return api.Local{}, &api.Error{Code: api.CodeRealityTargetFailed,
				Msg: clipText(fmt.Sprintf("The site %s doesn't work as a Reality target from this router: %s", next.RealityTarget, chk.Error))}
		}
	}

	err := m.cfg.Store.Update(func(s *model.State) error {
		s.Local.Host, s.Local.RealityTarget, s.Local.RealitySNI = next.Host, next.RealityTarget, next.RealitySNI
		return nil
	})
	if err != nil {
		return api.Local{}, err
	}
	m.mu.Lock()
	if next.Host != cur.Host {
		m.localEventLocked(EventHostChanged, next.Host)
	}
	if moved {
		// a target taken away is chosen again at once, whatever the last try said
		m.local.targetErr, m.local.triedAt = "", time.Time{}
		if next.RealityTarget != "" {
			m.localEventLocked(EventTargetChanged, next.RealityTarget)
		}
	}
	m.mu.Unlock()
	m.log.Info("the entry's settings changed", "host", next.Host != cur.Host, "target", moved, "sni", next.RealitySNI != cur.RealitySNI)
	m.syncAsks()
	m.kickBridge()
	m.ensureLocalTarget(ctx)
	return m.localView(), nil
}

// NewLocalPort asks the router's script for another port on the Domestic
// link. It picks one within a minute, and every link of the entry changes
// with it.
func (m *Manager) NewLocalPort(ctx context.Context) (api.Local, error) {
	if !m.cfg.Store.Read().Local.Enabled {
		return api.Local{}, api.ErrLocalOff
	}
	if m.cfg.Asks == nil {
		return api.Local{}, &api.Error{Code: api.CodeLocalOff, Msg: "This hub has no way to ask the router for a port."}
	}
	if err := m.cfg.Asks.NewPort(); err != nil {
		return api.Local{}, fmt.Errorf("asking the router for a new port: %w", err)
	}
	m.mu.Lock()
	m.local.movedFrom, m.local.movingAt = m.local.facts.LocalPort, m.now()
	m.localEventLocked(EventPortRequested, "")
	m.mu.Unlock()
	m.log.Info("a new port was asked of the router for its own entry")
	return m.localView(), nil
}

// RotateLocalKeys gives the entry a new REALITY key pair and short id. Both
// are in every link, so every link changes, and the bridge starts again with
// them, which drops what is connected for a moment.
func (m *Manager) RotateLocalKeys(ctx context.Context) (api.Local, error) {
	keys, err := agentops.NewRealityKeys()
	if err != nil {
		return api.Local{}, fmt.Errorf("making the entry's keys: %w", err)
	}
	err = m.cfg.Store.Update(func(s *model.State) error {
		if !s.Local.HasKeys() {
			return api.ErrLocalOff // never switched on, so there is nothing to rotate
		}
		s.Local.RealityPrivateKey, s.Local.RealityPublicKey, s.Local.RealityShortID = keys.PrivateKey, keys.PublicKey, keys.ShortID
		return nil
	})
	if err != nil {
		return api.Local{}, err
	}
	m.localEvent(EventKeysRotated, "")
	m.log.Info("the entry's keys were rotated")
	m.kickBridge()
	return m.localView(), nil
}

// CheckLocalTarget tries a Reality target from the router and says what it
// found, as CheckHandshake does from a node.
func (m *Manager) CheckLocalTarget(ctx context.Context, target string) (api.HandshakeCheck, error) {
	target, err := cleanTarget(target)
	if err != nil {
		return api.HandshakeCheck{}, err
	}
	if m.cfg.LocalProbe == nil {
		return api.HandshakeCheck{}, &api.Error{Code: api.CodeLocalOff, Msg: "This hub can't check Reality targets."}
	}
	got, err := agentops.CheckTargetHere(ctx, m.cfg.LocalProbe, target)
	if err != nil {
		return api.HandshakeCheck{}, opFailure(err)
	}
	return api.HandshakeCheck{
		OK: got.OK, TLS13: got.TLS13, X25519: got.X25519, HandshakeBytes: got.HandshakeBytes, LatencyMs: got.LatencyMs, Error: got.Error,
	}, nil
}

// SetLocalSubscription switches a subscription on or off for the entry. Off
// keeps its client id in the state, so on again gives the same link. The
// entry has to be switched on: while it is off its switches stay as they were.
func (m *Manager) SetLocalSubscription(ctx context.Context, subID uint, enabled bool) (api.Subscription, error) {
	name, changed := "", false
	err := m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(subID)
		if sub == nil {
			return notFound("subscription", subID)
		}
		if !s.Local.Enabled {
			return api.ErrLocalOff
		}
		name = sub.Name
		c := s.FindLocalClient(subID)
		switch {
		case c == nil && !enabled:
			return nil // never on here, so nothing to switch off
		case c == nil:
			id, err := newClientID()
			if err != nil {
				return err
			}
			s.LocalClients = append(s.LocalClients, model.LocalClient{SubscriptionID: subID, ClientUUID: id, Enabled: true})
			changed = true
		case c.Enabled != enabled:
			c.Enabled, changed = enabled, true
		}
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	if changed {
		kind := EventSubscriptionOff
		if enabled {
			kind = EventSubscriptionOn
		}
		m.localEvent(kind, name)
		m.log.Info("subscription switched on the router's own entry", "subscription", subID, "on", enabled)
	}
	m.kickBridge()
	return m.subscriptionView(subID)
}

// What the entry can't make a link of yet.
var (
	errLocalSwitchedOff = &api.Error{Code: api.CodeNotFound, Msg: "This subscription is switched off on this router, so it has no link here."}
	errLocalNoTarget    = &api.Error{Code: api.CodeLocalOff, Msg: "This router's entry has no Reality target yet, so it has no links yet."}
	errLocalNoPort      = &api.Error{Code: api.CodeNoAddress, Msg: "The router has not opened a port for its own entry yet, so there is none to put in a link."}
	errLocalNoName      = &api.Error{Code: api.CodeNoAddress, Msg: "There is no name to put in the link: the router has no IP Cloud name, and none is set in Settings."}
)

// LocalLink is the address a subscription's client uses to reach the router's
// own entry: the name set by hand or the router's IP Cloud name, the port the
// router opened, the entry's REALITY values and the client id the state keeps.
func (m *Manager) LocalLink(subID uint) (api.Link, error) {
	st := m.cfg.Store.Read()
	sub, c, err := localLinkParts(st, subID)
	if err != nil {
		return api.Link{}, err
	}
	return m.buildLocalLink(st, sub, c.ClientUUID)
}

func localLinkParts(st model.State, subID uint) (model.Subscription, model.LocalClient, error) {
	sub := st.FindSubscription(subID)
	if sub == nil {
		return model.Subscription{}, model.LocalClient{}, notFound("subscription", subID)
	}
	if !st.Local.Enabled {
		return model.Subscription{}, model.LocalClient{}, api.ErrLocalOff
	}
	c := st.FindLocalClient(subID)
	if c == nil || !c.Enabled {
		return model.Subscription{}, model.LocalClient{}, errLocalSwitchedOff
	}
	return *sub, *c, nil
}

func (m *Manager) buildLocalLink(st model.State, sub model.Subscription, clientID string) (api.Link, error) {
	l := st.Local
	m.mu.RLock()
	f := m.local.facts
	m.mu.RUnlock()
	host := l.Host
	if host == "" {
		host = f.CloudName
	}
	switch {
	case l.RealityTarget == "":
		return api.Link{}, errLocalNoTarget
	case f.LocalPort == 0:
		return api.Link{}, errLocalNoPort
	case host == "":
		return api.Link{}, errLocalNoName
	}
	vless, err := subscriptions.BuildLink(subscriptions.LinkParams{
		UUID: clientID, Host: host, Port: int(f.LocalPort), ServerName: localServerName(l),
		PublicKey: l.RealityPublicKey, ShortID: l.RealityShortID, Name: sub.Name,
	})
	if err != nil {
		return api.Link{}, fmt.Errorf("the link of subscription %d on the router's own entry: %w", sub.ID, err)
	}
	return api.Link{Local: true, Vless: vless}, nil
}

// ReplaceLocalLink gives a subscription a new client id on the entry: the
// link handed out before stops working, what it has open is closed, and the
// answer is the link that takes its place. Unlike a node's, it needs nothing
// to be reachable: the id is the state's, and the bridge is sent what the
// state holds whenever it runs.
func (m *Manager) ReplaceLocalLink(ctx context.Context, subID uint) (api.Link, error) {
	fresh, err := newClientID()
	if err != nil {
		return api.Link{}, err
	}
	var sub model.Subscription
	err = m.cfg.Store.Update(func(s *model.State) error {
		got, _, err := localLinkParts(*s, subID)
		if err != nil {
			return err
		}
		sub = got
		s.FindLocalClient(subID).ClientUUID = fresh
		return nil
	})
	if err != nil {
		return api.Link{}, err
	}
	m.localEvent(EventSubscriptionOn, sub.Name+" (new link)")
	m.log.Info("link replaced on the router's own entry", "subscription", subID)
	m.kickBridge()
	return m.buildLocalLink(m.cfg.Store.Read(), sub, fresh)
}

// localLimitEvent puts on the entry's list that a subscription on it stopped
// at its limit or its end date, or runs again.
func (m *Manager) localLimitEvent(st model.State, subID uint) {
	sub := st.FindSubscription(subID)
	c := st.FindLocalClient(subID)
	if sub == nil || c == nil || !c.Enabled || !st.Local.Enabled {
		return
	}
	kind := EventSubscriptionResumed
	switch {
	case sub.Running():
	case sub.Stopped == model.StopExpired:
		kind = EventSubscriptionExpired
	default:
		kind = EventSubscriptionLimit
	}
	m.localEvent(kind, sub.Name)
}
