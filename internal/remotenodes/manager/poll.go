// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/subscriptions"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

const (
	// A node whose Xray restarted crashRestarts times within crashWindow is
	// crash-looping. The hub sees a restart when the process id or the start
	// time moves between two polls, so a pair of restarts inside one poll counts
	// as one, and the real number may be higher.
	crashWindow   = 10 * time.Minute
	crashRestarts = 3
)

var errNoClient = errors.New("the connector returned no client")

// tap hands the usage collector the client and keeps the bundle that the
// collector's poll fetched, which the collector doesn't give back: it is what
// the node's state is read from. The collector keeps the tap to acknowledge
// traffic through, so a connection has one tap for all its polls.
type tap struct {
	Client
	mu   sync.Mutex
	last *agent.StatsBundle
}

func (t *tap) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*agent.StatsBundle, error) {
	b, err := t.Client.GetStatsBundle(ctx, includeOnlineIPs)
	if err == nil {
		t.mu.Lock()
		t.last = b
		t.mu.Unlock()
	}
	return b, err
}

// take is the bundle of the last poll, once.
func (t *tap) take() *agent.StatsBundle {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.last
	t.last = nil
	return b
}

// poll asks the node for its stats through the usage collector, which counts
// the traffic, and reads the node's state from what came back. restarted is
// whether Xray has restarted since the poll before.
func (m *Manager) poll(ctx context.Context, l *nodeLive, t *tap) (restarted bool, err error) {
	pctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	if _, err := m.cfg.Usage.Poll(pctx, l.id, t); err != nil {
		return false, err
	}
	if b := t.take(); b != nil {
		return m.noteBundle(l, b), nil
	}
	return false, nil
}

// noteBundle reads what the agent reported into the node's state. What the
// agent doesn't report has no source, and stays at its zero value: the
// agent's own uptime and memory, why Xray last exited, and whether the node
// runs the settings the hub last pushed. A bundle without a status leaves the
// last report as it was.
func (m *Manager) noteBundle(l *nodeLive, b *agent.StatsBundle) (restarted bool) {
	st := b.Status
	if st == nil {
		return false
	}
	now := m.now()
	xray := xrayVersionOf(st.GetXrayVersion())

	m.mu.Lock()
	if l.haveStatus {
		pid, started := st.GetXrayPid(), st.GetLastRestartTime()
		// 0 and below mean the agent didn't say, which in service mode is
		// always the start time
		movedPID := l.lastPID > 0 && pid > 0 && pid != l.lastPID
		movedStart := l.lastStart > 0 && started > 0 && started != l.lastStart
		if movedPID || movedStart {
			// the node's clients are looked at again either way: a restarted Xray has
			// none that were added to it while it ran
			restarted = true
			if l.expecting == 0 && !now.Before(l.expectedUntil) {
				l.restarts++
				l.restartAt = append(l.restartAt, now)
			}
		}
	}
	l.haveStatus, l.lastPID, l.lastStart = true, st.GetXrayPid(), st.GetLastRestartTime()
	l.restartAt = slices.DeleteFunc(l.restartAt, func(at time.Time) bool { return now.Sub(at) > crashWindow })

	if xray == "" {
		xray = l.xrayVer
	}
	l.agent = &api.AgentState{
		Version:     l.agentVer,
		XrayVersion: xray,
		ReportedAt:  now.UTC(),
		Xray: api.XrayState{
			Running:   st.GetXrayRunning(),
			Restarts:  l.restarts,
			UptimeSec: st.GetUptimeSeconds(),
			CrashLoop: len(l.restartAt) >= crashRestarts,
		},
	}
	m.mu.Unlock()
	if restarted {
		m.log.Info("a node's Xray restarted", "node", l.id)
	}
	m.noteVersions(l, "", xray)
	return restarted
}

// restartGrace is how long after one of the hub's own operations a restart is
// still taken as its doing: the poll that sees it can be a poll behind.
const restartGrace = 30 * time.Second

// expectRestarts says the hub is about to restart a node's Xray or its agent,
// as a config push or an update does. What restarts until done is called, and
// for restartGrace after, is the hub's own doing and doesn't count towards a
// crash loop. Three of those in ten minutes (rotate the keys, change a port,
// update the agent) used to read as "Xray keeps crashing".
func (m *Manager) expectRestarts(id uint) (done func()) {
	m.mu.Lock()
	l := m.recordLocked(id)
	l.expecting++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		l.expecting--
		l.expectedUntil = m.now().Add(restartGrace)
	}
}

// refreshVersion asks the agent its version, which a bundle doesn't carry.
func (m *Manager) refreshVersion(ctx context.Context, l *nodeLive, c Client) {
	vctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	info, err := c.GetVersion(vctx)
	if err != nil || info == nil {
		m.log.Debug("asking a node its version", "node", l.id, "err", err)
		return
	}
	m.noteVersions(l, agentVersionOf(info.AgentVersion), xrayVersionOf(info.XrayVersion))
}

// noteVersions takes in what the agent says its versions are; "" is not said.
// A version that differs from the last one heard (or, on a first report, from
// the one in the state) is an event.
func (m *Manager) noteVersions(l *nodeLive, agentVer, xrayVer string) {
	if agentVer == "" && xrayVer == "" {
		return
	}
	st := m.cfg.Store.Read()
	n := st.FindNode(l.id)
	if n == nil {
		return
	}
	stored := *n

	m.mu.Lock()
	if agentVer != "" {
		if was := firstOf(l.agentVer, stored.AgentVersion); was != "" && was != agentVer {
			m.eventLocked(l.id, EventAgentUpdated, was+" to "+agentVer)
		}
		l.agentVer = agentVer
	}
	xrayMoved := false
	if xrayVer != "" {
		if was := firstOf(l.xrayVer, stored.XrayVersion); was != "" && was != xrayVer {
			m.eventLocked(l.id, EventXrayChanged, was+" to "+xrayVer)
		}
		xrayMoved = l.xrayVer != xrayVer
		l.xrayVer = xrayVer
	}
	if l.agent != nil {
		l.agent.Version, l.agent.XrayVersion = l.agentVer, l.xrayVer
	}
	m.mu.Unlock()
	if xrayMoved {
		// whether the node may be in the bridge depends on it
		m.kickBridge()
	}
}

// saveVersions writes the versions the agent has reported into the state when
// they differ from what it holds, both in one write. That is rare: on a first
// contact, and when someone updates the agent or Xray. A failure is logged
// once and tried again with the next poll.
func (m *Manager) saveVersions(l *nodeLive) {
	st := m.cfg.Store.Read()
	n := st.FindNode(l.id)
	if n == nil {
		return
	}
	m.mu.RLock()
	agentVer, xrayVer := l.agentVer, l.xrayVer
	m.mu.RUnlock()
	if agentVer == "" || agentVer == n.AgentVersion {
		agentVer = ""
	}
	if xrayVer == "" || xrayVer == n.XrayVersion {
		xrayVer = ""
	}
	if agentVer == "" && xrayVer == "" {
		return
	}
	err := m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(l.id)
		if n == nil {
			return nil // removed in the meantime
		}
		if agentVer != "" {
			n.AgentVersion = agentVer
		}
		if xrayVer != "" {
			n.XrayVersion = xrayVer
		}
		return nil
	})
	m.mu.Lock()
	said := l.versionSaid
	l.versionSaid = ""
	if err != nil {
		l.versionSaid = err.Error()
	}
	m.mu.Unlock()
	if err != nil && err.Error() != said {
		m.log.Error("keeping a node's versions failed; it is tried again with the next poll", "node", l.id, "err", err)
	}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// reconcile brings the clients on the node's entry into line with the
// subscriptions that are switched on there. A failure is logged and left to the
// next connection; it never stops the loop. A quiet pass, one nothing but the
// clock asked for, is logged at info only when it changed something.
func (m *Manager) reconcile(ctx context.Context, l *nodeLive, c Client, quiet bool) {
	// a switch made while this works from what it read would be undone by it
	unlock, err := m.lockNodes(ctx, l.id)
	if err != nil {
		return // the loop is stopping
	}
	defer unlock()
	st := m.cfg.Store.Read()
	var want []subscriptions.Client
	for _, e := range st.Enablements {
		if e.NodeID == l.id && e.Enabled {
			want = append(want, subscriptions.Client{Email: nodecfg.SubscriptionEmail(int(e.SubscriptionID)), UUID: e.ClientUUID})
		}
	}
	slices.SortFunc(want, func(a, b subscriptions.Client) int {
		ia, _ := nodecfg.ParseSubscriptionEmail(a.Email)
		ib, _ := nodecfg.ParseSubscriptionEmail(b.Email)
		return ia - ib
	})
	rctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()
	diff, err := subscriptions.Reconcile(rctx, c, want)
	if ctx.Err() != nil {
		return
	}
	level := slog.LevelInfo
	if quiet && err == nil && len(diff.Added)+len(diff.Removed)+len(diff.Replaced) == 0 {
		level = slog.LevelDebug
	}
	m.log.Log(ctx, level, "reconciled a node's clients", "node", l.id, "wanted", len(want),
		"added", diff.Added, "removed", diff.Removed, "replaced", diff.Replaced, "foreign", diff.Foreign)
	if err != nil {
		m.log.Warn("a node's clients could not all be reconciled; the next connection tries again", "node", l.id, "err", err)
	}
}
