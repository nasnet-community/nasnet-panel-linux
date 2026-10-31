// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"regexp"
	"slices"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// The manager is what the API reads the live parts of its views from.
var _ api.Live = (*Manager)(nil)

// Xray is the one Xray version this plugin installs on nodes and runs in the
// bridge.
func (m *Manager) Xray() string { return pin.Version }

// NodeLive is what the manager knows of a node: when it last answered, the
// agent's last report, the node's tunnel, the numbers of its subscriptions,
// whether an update is due and the progress of its install if one is running.
// It never waits for the node, or for the release server: the updates are
// worked out from what the last look at the release found.
func (m *Manager) NodeLive(id uint) api.NodeLive {
	var out api.NodeLive
	m.mu.RLock()
	out.Tunnel = m.tunnelLocked(id)
	if l := m.nodes[id]; l != nil {
		out.LastSeenAt = timePtr(l.seen)
		if l.agent != nil {
			a := *l.agent
			out.Agent = &a
		}
	}
	m.mu.RUnlock()
	out.Usage = nodeUsage(m.cfg.Usage.Node(id))
	out.Installing = m.installing(id)
	out.Updates = m.updatesOf(id)
	return out
}

// SubscriptionOn is what a subscription used on a node, and whether it is
// connected there.
func (m *Manager) SubscriptionOn(subID, nodeID uint) api.SubscriptionUsage {
	return subUsage(m.cfg.Usage.Subscription(subID, nodeID))
}

// SubscriptionTotal is what a subscription used over every node.
func (m *Manager) SubscriptionTotal(subID uint) api.SubscriptionUsage {
	return subUsage(m.cfg.Usage.SubscriptionTotal(subID))
}

// Traffic is a node's chart: a sample for each step of the last fifteen
// minutes that a poll fell in.
func (m *Manager) Traffic(nodeID uint) api.Traffic {
	pts := m.cfg.Usage.Traffic(nodeID)
	t := api.Traffic{StepSec: int(usage.Step / time.Second), Samples: make([]api.TrafficSample, len(pts))}
	for i, p := range pts {
		t.Samples[i] = api.TrafficSample{At: p.At.UTC(), Up: p.Up, Down: p.Down}
	}
	return t
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func nodeUsage(u usage.NodeUsage) api.NodeUsage {
	return api.NodeUsage{
		Live: u.Live, Online: u.Online, Connections: u.Connections, RateUp: u.RateUp, RateDown: u.RateDown,
		TodayUp: u.TodayUp, TodayDown: u.TodayDown, MonthUp: u.MonthUp, MonthDown: u.MonthDown, UpdatedAt: timePtr(u.UpdatedAt),
	}
}

func subUsage(u usage.SubUsage) api.SubscriptionUsage {
	return api.SubscriptionUsage{
		Live: u.Live, Online: u.Online, Connections: u.Connections, LastSeenAt: timePtr(u.LastSeen),
		TodayUp: u.TodayUp, TodayDown: u.TodayDown, MonthUp: u.MonthUp, MonthDown: u.MonthDown,
	}
}

// installing is how far the node's install has got, while one runs.
func (m *Manager) installing(id uint) *api.Installing {
	if m.cfg.Jobs == nil {
		return nil
	}
	j, ok := m.cfg.Jobs.Running(id)
	if !ok || j.Kind != jobs.KindInstall || j.State != jobs.StateRunning {
		return nil
	}
	return installingOf(j)
}

var percent = regexp.MustCompile(`^([0-9]{1,3})%`)

// installingOf counts an install's steps the way the page does: the step that
// is running, or else the one after those that are done. The key and the label
// are that step's, and the share done is the number a step's text starts with
// ("62% · 8.4 of 13.6 MB"), if there is one.
func installingOf(j jobs.Job) *api.Installing {
	in := &api.Installing{JobID: j.ID, Steps: len(j.Steps)}
	at := slices.IndexFunc(j.Steps, func(s jobs.Step) bool { return s.State == jobs.StepRunning })
	if at >= 0 {
		if m := percent.FindStringSubmatch(j.Steps[at].Detail); m != nil {
			p := 0
			for _, c := range m[1] {
				p = p*10 + int(c-'0')
			}
			p = min(p, 100)
			in.Percent = &p
		}
	} else {
		at = slices.IndexFunc(j.Steps, func(s jobs.Step) bool { return s.State != jobs.StepOK })
	}
	switch {
	case at >= 0:
		in.Step, in.Key, in.Label = at+1, j.Steps[at].Key, j.Steps[at].Name
	default:
		in.Step = len(j.Steps) // every step is done
	}
	return in
}
