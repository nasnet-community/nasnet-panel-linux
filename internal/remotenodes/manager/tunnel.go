// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// The words of a tunnel's state and of the reason it isn't up, as the API
// contract has them.
const (
	TunnelUp   = "up"
	TunnelDown = "down"
	TunnelOff  = "off"

	ReasonXrayMismatch = "xray_mismatch"
	ReasonEntryOff     = "entry_off"
	ReasonBridgeDown   = "bridge_down"
	ReasonAgentDown    = "agent_down"
)

// tunnelIn is everything that decides what a node's tunnel is doing.
type tunnelIn struct {
	registered bool // the install has finished
	entryOn    bool
	xray       xrayState
	ready      bool // it could be in the bridge's spec
	applied    bool // it is in the spec the bridge was last given
	bridgeUp   bool // the bridge process runs and its status page answers
	node       *bridgespec.NodeStatus
	agentDown  bool // the agent has stopped answering
}

// deriveTunnel says what a node's tunnel is doing and, when it is not up, why.
// Off is a tunnel that is not meant to carry anything just now. Down is one
// that should, and doesn't. The first rule that fits wins:
//
//	the install has not finished      off   (no reason: there is nothing to carry yet)
//	the entry is off                  off   entry_off, for every node, since the bridge is stopped
//	the node's Xray is not the pin    off   xray_mismatch
//	the bridge holds the node, runs, and says the tunnel is up       up
//	the bridge holds the node, runs, and says it is not              down  agent_down if the agent has stopped answering
//	anything else                     down  agent_down if the agent has stopped answering,
//	                                        else bridge_down if the node is ready but the bridge isn't carrying it,
//	                                        else no reason: the node can't be in the bridge yet, and the log says why
//
// A node that is up stays up when its agent is out of reach: the tunnel doesn't
// go through the agent, so agent_down is a reason for a tunnel that is down.
// And it comes before bridge_down, since a node that can't be reached is the
// first thing to look at.
func deriveTunnel(in tunnelIn) api.TunnelState {
	off := func(reason string) api.TunnelState { return api.TunnelState{State: TunnelOff, Reason: reason} }
	switch {
	case !in.registered:
		return off("")
	case !in.entryOn:
		return off(ReasonEntryOff)
	case in.xray == xrayMismatch:
		return off(ReasonXrayMismatch)
	}

	t := api.TunnelState{State: TunnelDown}
	if in.agentDown {
		t.Reason = ReasonAgentDown
	}
	if !(in.ready && in.applied && in.bridgeUp && in.node != nil) {
		if t.Reason == "" && in.ready {
			t.Reason = ReasonBridgeDown
		}
		return t
	}
	n := in.node
	t.Streams, t.BytesToSubs, t.BytesFromSubs = n.Connections, n.BytesDown, n.BytesUp
	if !n.Since.IsZero() {
		since := n.Since.UTC()
		t.Since = &since
	}
	if n.Up {
		t.State, t.Reason = TunnelUp, ""
	}
	return t
}

// tunnelMark is what a node's tunnel last was, to tell a change from a poll
// that finds the same.
type tunnelMark struct {
	state  string
	reason string
	set    bool
}

// tunnelLocked is the node's tunnel now, from what the manager last saw of
// the bridge. The caller holds mu.
//
// Of the contract's fields, the bridge's status page gives the open
// connections and the bytes each way, and when the tunnel last changed. It has
// no round-trip time, no count of connections carried so far and no count of
// failed dials, and those stay at zero.
func (m *Manager) tunnelLocked(id uint) api.TunnelState {
	g, known := m.bv.gaps[id]
	in := tunnelIn{
		registered: known && g.kind != gapNotRegistered,
		entryOn:    m.entry.On,
		xray:       m.bv.xray[id],
		ready:      known && g.ok(),
		bridgeUp:   m.bv.up(),
	}
	if l := m.nodes[id]; l != nil {
		in.agentDown = l.unreachable
	}
	if tag, ok := m.bv.applied[id]; ok {
		in.applied = true
		if page := m.bv.status.Bridge; page != nil {
			for i := range page.Nodes {
				if page.Nodes[i].Tag == tag {
					in.node = &page.Nodes[i]
				}
			}
		}
	}
	return deriveTunnel(in)
}

// tunnelEventsLocked notes each node's tunnel and records a change between up
// and not up: tunnel_up when it comes up and tunnel_down, with the reason when
// there is one, when it stops. A tunnel that goes from off to down is not
// news: it was not up. The caller holds mu.
func (m *Manager) tunnelEventsLocked() {
	for id, g := range m.bv.gaps {
		if g.kind == gapNotRegistered {
			continue
		}
		l := m.recordLocked(id)
		cur := m.tunnelLocked(id)
		switch {
		case cur.State == TunnelUp && l.tunnel.state != TunnelUp:
			m.eventLocked(id, EventTunnelUp, "")
		case cur.State != TunnelUp && l.tunnel.state == TunnelUp:
			m.eventLocked(id, EventTunnelDown, cur.Reason)
		}
		l.tunnel = tunnelMark{state: cur.State, reason: cur.Reason, set: true}
	}
}
