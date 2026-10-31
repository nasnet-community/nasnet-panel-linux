// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// startRetry is how long a bridge that couldn't be started (the program is
// missing, say) is left alone before the next try.
const startRetry = 30 * time.Second

// bridgeView is what the manager knows of the bridge and of which nodes it
// could carry, under Manager.mu.
type bridgeView struct {
	gaps    map[uint]gap       // for every node in the state at the last look: what keeps it out of the spec
	xray    map[uint]xrayState // and how its Xray stands against the pin
	ready   int                // how many nodes are ready
	applied map[uint]string    // the nodes in the spec the supervisor holds, by id, with their tags
	status  bridgectl.Status   // the supervisor's last status
}

// up is whether the bridge process runs and its status page answers.
func (v bridgeView) up() bool {
	return v.status.State == bridgectl.StateRunning && v.status.Bridge != nil && v.status.Bridge.Running
}

// plan is what the bridge should be doing.
type plan struct {
	spec  *bridgespec.Spec // nil when it should not run
	why   string           // when it should not, though a node might be ready
	gaps  map[uint]gap
	xray  map[uint]xrayState
	ready int
}

// kickBridge asks for a look at what the bridge should be doing. It never
// waits: one is already due if a kick is waiting.
func (m *Manager) kickBridge() { nudge(m.bridgeKick) }

// watchBridge keeps the supervisor in step with the state and the entry gate,
// and reads how the tunnels are doing. It looks every BridgeEvery, and at once
// when the state, the gate or a node's Xray has changed, which is what a kick
// is for.
func (m *Manager) watchBridge(ctx context.Context) {
	t := time.NewTicker(m.cfg.BridgeEvery)
	defer t.Stop()
	for {
		m.refreshBridge(ctx)
		select {
		case <-ctx.Done():
			return
		case <-m.bridgeKick:
		case <-t.C:
		}
	}
}

// refreshBridge works out what the bridge should be doing, makes it so, and
// reads how it is doing. Only one runs at a time.
func (m *Manager) refreshBridge(ctx context.Context) {
	m.bridgeMu.Lock()
	defer m.bridgeMu.Unlock()
	p := m.planBridge(ctx)
	if ctx.Err() != nil {
		return
	}
	m.applyBridge(p)
	m.readBridge()
}

// knownXray is the Xray version known for a node: what its agent said on this
// run, or else what the state kept of an earlier one. The bridge doesn't
// wait for the agent to answer again after a restart of the hub, since the
// tunnel doesn't go through it.
func (m *Manager) knownXray(n model.Node) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.nodes[n.ID]; l != nil && l.xrayVer != "" {
		return l.xrayVer
	}
	return xrayVersionOf(n.XrayVersion)
}

// planBridge decides which nodes the bridge should carry and whether it should
// run at all. It runs when the entry is on, there is a node that is ready, and
// both of its addresses are held here: the bridge can only bind a source
// address that is the router's, and one it can't bind would leave from
// whichever the system picks, which for the exit means the wrong link.
func (m *Manager) planBridge(ctx context.Context) plan {
	st := m.cfg.Store.Read()
	dial, exit := m.cfg.Addrs.Nodes, m.cfg.Addrs.Exit
	spec, gaps, err := buildSpec(specInput{
		nodes: st.Nodes,
		xray:  m.knownXray,
		addr: func(n model.Node) (netip.Addr, error) {
			return m.addrOf(ctx, n, dial.IP)
		},
		dial: dial.IP, exit: exit.IP, dns: m.cfg.DNS, statusAddr: m.cfg.StatusAddr,
	})
	p := plan{gaps: gaps, xray: map[uint]xrayState{}}
	for _, n := range st.Nodes {
		p.xray[n.ID] = checkXray(m.knownXray(n))
		if gaps[n.ID].ok() {
			p.ready++
		}
	}
	m.forgetResolved(st)

	switch {
	case err != nil:
		p.why = "its spec is not valid: " + err.Error()
	case m.cfg.Bridge == nil:
		p.why = "there is no bridge program set up"
	case spec == nil:
		p.why = "no node is ready for it"
	case !m.entryOn():
		p.why = "the router's entry is off"
	case !exit.Local:
		p.why = fmt.Sprintf("%s %s is not assigned to any interface here, and its exit must not leave from another address", exit.Name, exit.IP)
	case !dial.Local:
		p.why = fmt.Sprintf("%s %s is not assigned to any interface here, so it could not dial the nodes from it", dial.Name, dial.IP)
	default:
		p.spec = spec
	}
	return p
}

// applyBridge makes the supervisor's spec the plan's: it starts the bridge or
// gives it the new spec when the spec has changed, and stops it when there is
// none. A spec with the same hash as the one it has is left alone, so a look
// that finds nothing new costs the tunnels nothing.
func (m *Manager) applyBridge(p plan) {
	m.mu.Lock()
	m.bv.gaps, m.bv.xray, m.bv.ready = p.gaps, p.xray, p.ready
	m.mu.Unlock()
	m.logGaps(p)
	if m.cfg.Bridge == nil {
		return
	}
	if p.spec == nil {
		m.stopBridge(p.why)
		return
	}
	m.whySaid = ""

	h := specHash(p.spec)
	if h == m.appliedHash || (h == m.failedHash && m.now().Before(m.retryAt)) {
		return
	}
	err := m.cfg.Bridge.Update(p.spec)
	// An update that failed on a bridge that was running leaves the supervisor
	// holding the new spec and trying again by itself; only one that was
	// stopped is the manager's to try again.
	held := err == nil || m.cfg.Bridge.Status().State != bridgectl.StateStopped
	if held {
		m.appliedHash, m.failedHash = h, ""
		tags := map[uint]string{}
		for _, n := range p.spec.Nodes {
			id, _ := idOfTag(n.Tag)
			tags[id] = n.Tag
		}
		m.mu.Lock()
		m.bv.applied = tags
		m.mu.Unlock()
	} else {
		m.appliedHash, m.failedHash, m.retryAt = "", h, m.now().Add(startRetry)
		m.mu.Lock()
		m.bv.applied = nil
		m.mu.Unlock()
	}
	switch {
	case err == nil:
		m.startSaid = ""
		m.log.Info("the bridge has its spec", "portals", len(p.spec.Nodes))
	case err.Error() != m.startSaid:
		m.startSaid = err.Error()
		m.log.Error("the bridge could not be started", "portals", len(p.spec.Nodes), "err", err, "supervisor_retries", held)
	}
}

// stopBridge stops the bridge, if it was started, and says why once.
func (m *Manager) stopBridge(why string) {
	if m.appliedHash != "" {
		m.cfg.Bridge.Stop()
		m.appliedHash, m.failedHash, m.startSaid = "", "", ""
		m.mu.Lock()
		m.bv.applied = nil
		m.mu.Unlock()
		m.log.Info("the bridge is stopped", "why", why)
		m.whySaid = why
		return
	}
	m.failedHash, m.startSaid = "", ""
	if why != m.whySaid {
		m.whySaid = why
		m.log.Info("the bridge is not running", "why", why)
	}
}

// logGaps says, once for each change, which nodes are ready for the bridge
// and what keeps the others out.
func (m *Manager) logGaps(p plan) {
	for id, g := range p.gaps {
		if g.kind == gapNotRegistered {
			delete(m.gapSaid, id)
			continue
		}
		if now := g.String(); m.gapSaid[id] != now {
			m.gapSaid[id] = now
			if g.ok() {
				m.log.Info("a node is ready for the bridge", "node", id)
			} else {
				m.log.Info("a node is left out of the bridge", "node", id, "why", now)
			}
		}
	}
	for id := range m.gapSaid {
		if _, there := p.gaps[id]; !there {
			delete(m.gapSaid, id)
		}
	}
}

// readBridge reads the supervisor's status, which asks the bridge's own page,
// and notes the tunnels' changes.
func (m *Manager) readBridge() {
	if m.cfg.Bridge == nil {
		return
	}
	st := m.cfg.Bridge.Status()
	m.mu.Lock()
	m.bv.status = st
	m.tunnelEventsLocked()
	m.mu.Unlock()
}

// bridgeWordLocked is the entry's word for the bridge. It is running when the
// process is up and carries a node, waiting when the entry is on and no node is
// ready to carry, and stopped in every other case: the entry is off, the
// program can't be started, or it is between restarts. The caller holds mu.
func (m *Manager) bridgeWordLocked() string {
	switch {
	case m.cfg.Bridge == nil:
		return "stopped"
	case m.bv.status.State == bridgectl.StateRunning && len(m.bv.applied) > 0:
		return "running"
	case m.entry.On && m.bv.ready == 0:
		return "waiting"
	}
	return "stopped"
}
