// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const (
	// A node that doesn't answer is tried again after a second, then two,
	// four and so on up to half a minute, and the wait starts over after a
	// poll that gets through.
	connectMin = time.Second
	connectMax = 30 * time.Second

	connectTimeout   = 15 * time.Second
	pollTimeout      = 10 * time.Second
	versionTimeout   = 10 * time.Second
	reconcileTimeout = 30 * time.Second

	// missesUntilUnreachable is how many polls in a row may get no answer. At
	// the third the node is called unreachable, once, and its connection is
	// given up for a new one.
	missesUntilUnreachable = 3

	// versionEvery is how often the agent is asked its version when nothing
	// else made it look. A restart of the agent or of Xray doesn't always
	// cost a poll, so this is what notices an upgrade that did not.
	versionEvery = time.Minute
)

// nodeLive is what the manager holds for one node, in memory only. A record
// is made when a node is first looked at or given an event, and dropped when
// the node leaves the state.
type nodeLive struct {
	id uint

	// The loop. cancel is set while one runs and done is closed when it has
	// ended; Sync is the only one to change them, under Manager.mu, and the
	// loop clears cancel when it ends by itself. stopping is a record whose
	// loop has been told to end for good and which waits to be dropped.
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
	target   string        // host:port the loop was started for, to see an edit
	kick     chan struct{} // asks the loop for a new connection at once

	// What follows is written by the loop and read for the page, under
	// Manager.mu.

	client      Client
	seen        time.Time // the last good poll
	misses      int       // polls and connections in a row that got no answer
	unreachable bool      // misses reached the limit; said once, until the node answers

	// The agent's last report, kept while the node is out of reach so that the
	// page still shows what it last said, and what is needed to read the next.
	agent       *api.AgentState
	agentVer    string // what the agent said it is on this run, as it is kept
	xrayVer     string
	versionSaid string // the last failure to write them that was logged

	haveStatus bool
	lastPID    int64
	lastStart  int64
	restarts   int         // restarts of Xray seen since the hub started, not counting the ones it caused
	restartAt  []time.Time // when, oldest first, for the last few minutes

	// The hub restarts a node's Xray or agent itself when it pushes a config or
	// updates one of them. A restart seen while expecting is above zero, or
	// before expectedUntil, is that: it is not counted towards a crash loop.
	expecting     int
	expectedUntil time.Time

	tunnel tunnelMark
	events eventRing
}

// recordLocked is the node's record, made if there is none. The caller holds
// mu for writing.
func (m *Manager) recordLocked(id uint) *nodeLive {
	l := m.nodes[id]
	if l == nil {
		l = &nodeLive{id: id, kick: make(chan struct{}, 1)}
		m.nodes[id] = l
	}
	return l
}

func targetOf(n model.Node) string { return net.JoinHostPort(n.Host, strconv.Itoa(n.AgentPort)) }

func nudge(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default: // one is waiting already
	}
}

// Sync takes in the state as it is now. A slice that has edited the state
// calls it, so that a node it added is polled, and one it removed is let go,
// without waiting for the next look; the manager also looks every few seconds
// by itself. It starts a loop for each registered node that has none, stops
// the loop of a node that has left the state, and sends a loop whose node has
// moved to its new address. Nothing starts while the CA can't be used.
func (m *Manager) Sync() {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	replaced := m.checkCA()
	run := m.running()
	if run == nil {
		return
	}
	st := m.cfg.Store.Read()
	usable := m.CA() != nil

	var retired []*nodeLive
	m.mu.Lock()
	for id, l := range m.nodes {
		n := st.FindNode(id)
		switch {
		case n == nil:
			// gone from the state: the loop ends, then the node is forgotten
			if !l.stopping {
				l.stopping = true
				if l.cancel != nil {
					l.cancel()
					l.cancel = nil
				}
				retired = append(retired, l)
			}
		case n.State != model.NodeRegistered:
			if l.cancel != nil {
				l.cancel()
				l.cancel = nil
			}
		case l.cancel != nil && (replaced || l.target != targetOf(*n)):
			// a new address, or a new CA that the old connection doesn't know
			l.target = targetOf(*n)
			nudge(l.kick)
		}
	}
	if usable {
		for _, n := range st.Nodes {
			if n.State != model.NodeRegistered {
				continue
			}
			if l := m.recordLocked(n.ID); l.cancel == nil && !l.stopping {
				m.startLocked(run, l, n)
			}
		}
	}
	m.mu.Unlock()

	for _, l := range retired {
		m.spawn(func(context.Context) { m.forget(l) })
	}
	// nodes came, went or moved, and with them what the bridge should carry
	m.kickBridge()
}

// startLocked starts the loop of a node. The caller holds mu.
func (m *Manager) startLocked(run context.Context, l *nodeLive, n model.Node) {
	ctx, cancel := context.WithCancel(run)
	done := make(chan struct{})
	started := m.spawn(func(context.Context) {
		defer close(done)
		defer cancel()
		m.runNode(ctx, l)
		m.mu.Lock()
		if !l.stopping && l.done == done {
			l.cancel = nil // ended by itself, so Sync may start another
		}
		m.mu.Unlock()
	})
	if !started {
		cancel()
		return
	}
	l.cancel, l.done, l.target = cancel, done, targetOf(n)
	m.log.Info("watching node", "node", n.ID, "name", n.Name, "at", l.target)
}

// forget finishes with a node that has left the state, once its loop has
// ended: the usage numbers go, so a poll in flight can't bring them back, and
// then the record. The loop closed its client on the way out.
func (m *Manager) forget(l *nodeLive) {
	m.mu.RLock()
	done := l.done
	m.mu.RUnlock()
	if done != nil {
		<-done
	}
	m.cfg.Usage.Forget(l.id)
	m.mu.Lock()
	if m.nodes[l.id] == l {
		delete(m.nodes, l.id)
	}
	m.mu.Unlock()
	m.log.Info("node forgotten", "node", l.id)
}

// inState is whether the state has a node with this id, registered or not.
func (m *Manager) inState(id uint) bool {
	st := m.cfg.Store.Read()
	return st.FindNode(id) != nil
}

// registered is the node as the state has it now, if it is still registered.
func (m *Manager) registered(id uint) (model.Node, bool) {
	st := m.cfg.Store.Read()
	if n := st.FindNode(id); n != nil && n.State == model.NodeRegistered {
		return *n, true
	}
	return model.Node{}, false
}

type waited int

const (
	waitedOut waited = iota
	waitKicked
	waitStopped
)

// wait lets d go by, or less if the loop is asked to reconnect or to stop.
func (m *Manager) wait(ctx context.Context, l *nodeLive, d time.Duration) waited {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return waitStopped
	case <-l.kick:
		return waitKicked
	case <-t.C:
		return waitedOut
	}
}

// runNode is a node's loop: connect, serve the connection until it is given
// up, connect again. It ends when ctx does or the node isn't registered any
// more.
func (m *Manager) runNode(ctx context.Context, l *nodeLive) {
	backoff := connectMin
	for ctx.Err() == nil {
		n, ok := m.registered(l.id)
		if !ok {
			return
		}
		// no connection is made while the CA can't be used; Sync looks again
		if m.CA() == nil {
			if m.wait(ctx, l, m.cfg.StateEvery) == waitStopped {
				return
			}
			continue
		}
		c, err := m.connect(ctx, n)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.miss(l, "connect", err)
			if m.wait(ctx, l, backoff) == waitStopped {
				return
			}
			backoff = min(2*backoff, connectMax)
			continue
		}
		m.attach(l, c)
		m.serve(ctx, l, c, &backoff)
		m.detach(l, c)
	}
}

func (m *Manager) connect(ctx context.Context, n model.Node) (Client, error) {
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	c, err := m.cfg.Connector.Connect(cctx, n)
	if err == nil && c == nil {
		err = errNoClient
	}
	if err == nil && ctx.Err() != nil {
		_ = c.Close() // the manager was stopped while the node was answering
		err = ctx.Err()
	}
	return c, err
}

// serve polls the node over a connection until it is given up: after
// missesUntilUnreachable polls in a row with no answer, when the loop is asked
// to reconnect, or when it is stopped.
//
// After the first good poll of a connection, and after any poll that follows
// a failed one or finds Xray restarted, the node's clients are brought into
// line with the state. A node that has been out of reach may have come back
// from a restart with the clients it had in its config file, and a restart is
// when a drift is likely.
func (m *Manager) serve(ctx context.Context, l *nodeLive, c Client, backoff *time.Duration) {
	t := &tap{Client: c}
	pending := true
	var versionAt time.Time
	for {
		restarted, err := m.poll(ctx, l, t)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			pending = true
			if m.miss(l, "poll", err) >= missesUntilUnreachable {
				return
			}
		default:
			m.seen(l)
			*backoff = connectMin
			pending = pending || restarted
			if now := m.now(); pending || now.Sub(versionAt) >= versionEvery {
				m.refreshVersion(ctx, l, c)
				versionAt = now
			}
			m.saveVersions(l)
			if pending {
				m.reconcile(ctx, l, c)
				pending = false
			}
		}
		if m.wait(ctx, l, m.cfg.PollEvery) != waitedOut {
			return
		}
	}
}

func (m *Manager) attach(l *nodeLive, c Client) {
	m.mu.Lock()
	l.client = c
	m.mu.Unlock()
}

// detach gives a connection up and closes it.
func (m *Manager) detach(l *nodeLive, c Client) {
	m.mu.Lock()
	if l.client == c {
		l.client = nil
	}
	m.mu.Unlock()
	if err := c.Close(); err != nil {
		m.log.Debug("closing a node's connection", "node", l.id, "err", err)
	}
}

// miss counts a poll or a connection that got no answer and returns how many
// there have been in a row. The third makes the node unreachable, which is
// said once.
func (m *Manager) miss(l *nodeLive, what string, err error) int {
	m.mu.Lock()
	l.misses++
	n := l.misses
	declared := n >= missesUntilUnreachable && !l.unreachable
	if declared {
		l.unreachable = true
		m.eventLocked(l.id, EventNodeUnreachable, err.Error())
	}
	m.mu.Unlock()
	if declared {
		m.log.Warn("node is not answering", "node", l.id, "after", what, "err", err)
	} else {
		m.log.Debug("node did not answer", "node", l.id, "what", what, "misses", n, "err", err)
	}
	return n
}

// seen notes a good poll: the node answered.
func (m *Manager) seen(l *nodeLive) {
	now := m.now()
	m.mu.Lock()
	back := l.unreachable
	l.seen, l.misses, l.unreachable = now, 0, false
	if back {
		m.eventLocked(l.id, EventNodeBack, "")
	}
	m.mu.Unlock()
	if back {
		m.log.Info("node is back", "node", l.id)
	}
}

// Client is the connection to a node's agent, to borrow for what needs one:
// pushing a config, updating the agent, switching a subscription. It is there
// only while the loop holds one, so false means the node isn't reachable now.
//
// It belongs to the loop, which closes it when the connection is given up or
// the node leaves; don't close it, and expect a call to fail when the node
// drops. The type is the narrow Client, so a caller that needs more asks the
// value for a wider interface, which the connector's clients satisfy.
func (m *Manager) Client(nodeID uint) (Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.nodes[nodeID]; l != nil && l.client != nil {
		return l.client, true
	}
	return nil, false
}

// Reconnect has a node's loop drop its connection and make a new one at once.
// A slice that restarts the agent, or moves its port, calls it so that the
// manager doesn't wait to find out. It does nothing for a node with no loop.
func (m *Manager) Reconnect(nodeID uint) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.nodes[nodeID]; l != nil && l.cancel != nil {
		nudge(l.kick)
	}
}

// ForgetSubscription drops what is counted for a subscription that has been
// deleted, on every node.
func (m *Manager) ForgetSubscription(subID uint) { m.cfg.Usage.ForgetSubscription(subID) }
