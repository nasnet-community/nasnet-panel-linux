// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"net/netip"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// restartTimeout is how long a node gets to restart its Xray: the agent waits
// for the old one to stop, starts the new one and looks again two seconds on.
const restartTimeout = 45 * time.Second

// When the router's address on the Domestic link changes, the connections
// each node holds from the old address are dead and nobody will ever answer
// them. A node's Xray doesn't know that: it keeps handing the subscriptions'
// requests to the tunnel connection of the old address until the kernel gives
// the connection up, which takes about fifteen minutes. A restart of Xray
// drops them at once, and the bridge's own new connection is the only one left.
//
// So a change of the address marks every registered node, and each node's loop
// restarts its Xray the next time it holds a connection and has had a good
// poll. A node that is out of reach keeps the mark until it is back. The
// address is only ever read from the router's report, and only the last one
// the report gave is remembered, in memory: a hub that starts knows no
// address, takes the first one it reads as the baseline, and so misses a
// change that happened while it was down. The kernel setting the install makes
// on each node (agentops, tcp_retries2) is what still bounds that.

// addrReset is what a node's Xray owes to the router's address changing.
type addrReset struct {
	seq  int // changes of the address seen since the hub started, for this node
	done int // how many of them a restart has answered for

	// The address before the first change not yet answered for, and the
	// newest, for the event.
	from, to netip.Addr
}

func (r addrReset) due() bool { return r.seq > r.done }

// noteDomesticAddr takes in the router's address on the Domestic link as the
// last report gave it. An address that differs from the last one given marks
// every registered node. Nothing else does: the first address a hub reads is
// no change, a report with no address (a link that is down, a script that
// predates the key, a value that was not an address) is no change, and the
// same address again is none.
func (m *Manager) noteDomesticAddr(addr netip.Addr) {
	if !addr.IsValid() {
		return
	}
	nodes := m.registeredIDs()

	m.mu.Lock()
	was := m.domAddr
	m.domAddr = addr
	if !was.IsValid() || was == addr {
		m.mu.Unlock()
		return
	}
	for _, id := range nodes {
		l := m.recordLocked(id)
		if !l.reset.due() {
			l.reset.from = was
		}
		l.reset.seq++
		l.reset.to = addr
	}
	m.mu.Unlock()
	m.log.Info("the router's address on the Domestic link changed, so each node's Xray is restarted",
		"from", was, "to", addr, "nodes", len(nodes))
}

// registeredIDs are the nodes whose install is done, as the state has them.
func (m *Manager) registeredIDs() []uint {
	var ids []uint
	st := m.cfg.Store.Read()
	for _, n := range st.Nodes {
		if n.State == model.NodeRegistered {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// resetDue is the number of the change a node's Xray is still to be restarted
// for, or 0 when there is none.
func (m *Manager) resetDue(l *nodeLive) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l.reset.due() {
		return l.reset.seq
	}
	return 0
}

// resetXray restarts a node's Xray for the changes of the router's address it
// has not answered for yet, and says whether it did. It holds the node's turn
// at its clients, like a config push,
// a switch and a reconcile do, so that it never lands in the middle of one: a
// push restarts Xray itself, and a switch that loses its client to the restart
// reports a failure. It is inside expectRestarts, so the restart is the hub's
// own doing and no crash loop. The caller puts the node's clients right
// afterwards: the restarted Xray has none of those added to it while it ran.
//
// A failure is logged and the mark stays, but the loop doesn't try again until
// it has a new connection or the address changes again: a restart that can't be
// done (the config on the node is bad) would otherwise be tried at every poll,
// and each try that did get as far as stopping Xray costs the clients a gap.
func (m *Manager) resetXray(ctx context.Context, l *nodeLive, c Client) bool {
	unlock, err := m.lockNodes(ctx, l.id)
	if err != nil {
		return false // the loop is stopping
	}
	// read under the turn: the restart answers for every change up to now, and the event says which
	m.mu.RLock()
	seq, from, to := l.reset.seq, l.reset.from, l.reset.to
	m.mu.RUnlock()

	done := m.expectRestarts(l.id)
	rctx, cancel := context.WithTimeout(ctx, restartTimeout)
	err = c.RestartXray(rctx, true)
	cancel()
	done()
	unlock()
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		m.log.Warn("restarting a node's Xray after the router's address changed failed; it is tried again with the next connection",
			"node", l.id, "err", err)
		return false
	}

	m.mu.Lock()
	l.reset.done = max(l.reset.done, seq)
	if l.reset.due() {
		l.reset.from = to // another change came in meanwhile, and starts from where this one ended
	} else {
		l.reset.from, l.reset.to = netip.Addr{}, netip.Addr{}
	}
	m.eventLocked(l.id, EventXrayRestarted, addrDetail(from, to))
	m.mu.Unlock()
	m.log.Info("restarted a node's Xray because the router's address on the Domestic link changed",
		"node", l.id, "from", from, "to", to)
	return true
}

// addrDetail is the event's text: "192.168.1.20 to 192.168.1.33".
func addrDetail(from, to netip.Addr) string {
	if !from.IsValid() || !to.IsValid() {
		return ""
	}
	return from.String() + " to " + to.String()
}
