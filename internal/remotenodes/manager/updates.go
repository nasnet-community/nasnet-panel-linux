// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

const (
	// releaseTTL is how long what a look at the release found is trusted. The
	// release is asked about once an hour, in the background, and the page
	// never waits for it.
	releaseTTL = time.Hour
	// releaseRetry is how long after a look that failed the next is made.
	releaseRetry = 5 * time.Minute
	// releaseThrottle is how soon after a look a person who asks again is
	// told the same thing: asking is a request to the release server, and a
	// button pressed twice is not worth two.
	releaseThrottle = 30 * time.Second
	// releaseFirst is how long after the manager starts the first look is
	// made, which lets the router's routes settle after a boot.
	releaseFirst   = 30 * time.Second
	releaseTimeout = 30 * time.Second
)

// minAgent is the oldest agent the plugin works with, as a release.
var minAgent = release.MustParse("v" + artifacts.MinAgentVersion)

// latest is what a look at the release found for one architecture.
type latest struct {
	version string    // the agent the release carries; "" when there is none the plugin can push
	asked   time.Time // when it was last asked, whether or not that worked
	err     string    // why the last look failed, "" when it didn't
	said    string    // the failure that was last logged, so it is said once
}

// releases is what the manager remembers of the release, by architecture, and
// the gate that lets one look run at a time.
type releases struct {
	mu     sync.Mutex
	byArch map[string]*latest
	gate   chan struct{}
}

// fresh is whether what is held for arch needs no new look yet. A look that
// failed is tried again sooner than one that worked, and one a person asks for
// is made unless one was made a moment ago.
func (r *releases) fresh(arch string, now time.Time, force bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.byArch[arch]
	if l == nil {
		return false
	}
	age := now.Sub(l.asked)
	switch {
	case force:
		return age < releaseThrottle
	case l.err != "":
		return age < releaseRetry
	}
	return age < releaseTTL
}

// failing is whether the last look for any architecture failed.
func (r *releases) failing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.byArch {
		if l.err != "" {
			return true
		}
	}
	return false
}

// agentIn is the agent the latest release carries for arch, as it was last
// found, however long ago.
func (r *releases) agentIn(arch string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l := r.byArch[arch]; l != nil {
		return l.version
	}
	return ""
}

// askRelease finds out which agent the latest release carries for arch, unless
// what is held is fresh enough. Only one look runs at a time. It is an error
// only when the look failed, and what was found before is kept through that.
func (m *Manager) askRelease(ctx context.Context, arch string, force bool) error {
	if m.cfg.Downloads == nil || arch == "" || m.rel.fresh(arch, m.now(), force) {
		return nil
	}
	select {
	case m.rel.gate <- struct{}{}:
		defer func() { <-m.rel.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if m.rel.fresh(arch, m.now(), force) { // another look was made while this one waited
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	version, err := m.cfg.Downloads.LatestAgent(lctx, arch)
	if ctx.Err() != nil {
		return ctx.Err() // the one who asked has gone, which says nothing of the release
	}
	return m.noteRelease(arch, version, err)
}

// noteRelease keeps what a look found, and says what changed. A release that
// is older than the plugin works with, or has no agent it can push, is not a
// failure: there is nothing to offer, and no reason to ask again before the
// hour is up.
func (m *Manager) noteRelease(arch, version string, err error) error {
	m.rel.mu.Lock()
	l := m.rel.byArch[arch]
	if l == nil {
		l = &latest{}
		m.rel.byArch[arch] = l
	}
	l.asked = m.now()
	was, said := l.version, l.said
	var failure error
	switch {
	case err == nil:
		l.version, l.err, l.said = version, "", ""
	case errors.Is(err, artifacts.ErrAgentTooOld), errors.Is(err, artifacts.ErrNoGzAgents):
		l.version, l.err, l.said = "", "", ""
	default:
		failure = err
		l.err, l.said = err.Error(), err.Error()
	}
	m.rel.mu.Unlock()

	switch {
	case failure != nil && failure.Error() != said:
		m.log.Warn("couldn't look up the latest release", "arch", arch, "err", failure)
	case failure == nil && version != "" && version != was:
		m.log.Info("the latest release carries an agent", "arch", arch, "version", version)
	}
	return failure
}

// watchReleases looks up the release for each architecture the nodes are of,
// every hour, or after five minutes when a look failed.
func (m *Manager) watchReleases(ctx context.Context) {
	if m.cfg.Downloads == nil {
		return
	}
	for wait := releaseFirst; sleep(ctx, wait); {
		for _, arch := range m.arches() {
			_ = m.askRelease(ctx, arch, false) // a failure is held, and logged, with what it was about
		}
		wait = releaseTTL
		if m.rel.failing() {
			wait = releaseRetry
		}
	}
}

// arches are the architectures of the nodes that are set up.
func (m *Manager) arches() []string {
	var out []string
	for _, n := range m.cfg.Store.Read().Nodes {
		if n.State == model.NodeRegistered && n.Arch != "" && !slices.Contains(out, n.Arch) {
			out = append(out, n.Arch)
		}
	}
	return out
}

// knownAgent is the agent version known for a node, as knownXray is for Xray:
// what its agent said on this run, or else what the state kept.
func (m *Manager) knownAgent(n model.Node) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.nodes[n.ID]; l != nil && l.agentVer != "" {
		return l.agentVer
	}
	return agentVersionOf(n.AgentVersion)
}

// updatesOf is what the page is told of the updates a node is due. Xray is due
// when the node's differs from the pin, whichever way, since the plugin
// installs the one build and refuses a tunnel on another. The agent is due when
// the latest release carries a newer one that the plugin can push. Neither
// waits for anything: the release is what the last look found.
func (m *Manager) updatesOf(id uint) api.Updates {
	st := m.cfg.Store.Read()
	n := st.FindNode(id)
	if n == nil || n.State != model.NodeRegistered {
		return api.Updates{}
	}
	u := api.Updates{XrayVersion: pin.Version}
	if have := m.knownXray(*n); have != "" && have != pin.Version {
		u.Xray = true
	}
	if v := m.rel.agentIn(n.Arch); v != "" {
		u.AgentVersion = strings.TrimPrefix(v, "v")
		u.Agent = agentIsOlder(m.knownAgent(*n), v)
	}
	return u
}

// agentIsOlder is whether the release is newer than the agent a node runs, and
// one the plugin works with. An agent whose version is no release (a build
// from the source, say) can't be compared, and is not said to be behind.
func agentIsOlder(have, latest string) bool {
	l, err := release.Parse(latest)
	if err != nil {
		return false
	}
	h, err := release.Parse("v" + strings.TrimPrefix(have, "v"))
	if err != nil {
		return false
	}
	return l.Compare(h) > 0 && l.Compare(minAgent) >= 0
}

// CheckUpdates looks up the latest release again, so that the updates the page
// shows for the node are as fresh as the release server's answer. A look made
// a moment ago stands. When the router can't read the release the person is
// told, and what was found before is kept.
func (m *Manager) CheckUpdates(ctx context.Context, id uint) error {
	n, err := m.nodeFor(id)
	if err != nil {
		return err
	}
	if n.State != model.NodeRegistered {
		return neverInstalled()
	}
	if err := m.askRelease(ctx, n.Arch, true); err != nil && ctx.Err() == nil {
		return &api.Error{Code: api.CodeInternal, Msg: sentence("The router couldn't read the latest release: " + strings.TrimPrefix(err.Error(), "artifacts: "))}
	}
	return nil
}

// UpdateNode updates a node's "agent" or its "xray", as a job, which gives the
// id. It never forces: an agent that would be going backwards refuses, and a
// node that is on the release is sent nothing. One node is updated at a time.
func (m *Manager) UpdateNode(_ context.Context, id uint, kind string) (string, error) {
	if kind != "agent" && kind != "xray" {
		return "", invalidf("what to update is the agent or xray")
	}
	n, err := m.settled(id)
	if err != nil {
		return "", err
	}
	ops, err := m.operate()
	if err != nil {
		return "", err
	}
	if _, err := m.link(n); err != nil {
		return "", err
	}
	spec := jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: id, Target: n.Name, Steps: agentops.UpdateAgentPlan()}
	run := m.runUpdateAgent
	if kind == "xray" {
		spec.Kind, spec.Steps, run = jobs.KindUpdateXray, agentops.UpdateXrayPlan(), m.runUpdateXray
	}
	return m.startJob(spec, m.usesDownloads(func(ctx context.Context, r *jobs.Run) error {
		return run(ctx, r, ops, id, spec.Steps[0])
	}))
}

func (m *Manager) runUpdateAgent(ctx context.Context, r *jobs.Run, ops Ops, id uint, first string) error {
	turn, err := m.takeTurn(ctx, r, first)
	if err != nil {
		return err
	}
	defer turn()
	n, err := m.nodeFor(id)
	if err != nil {
		return err
	}
	c, err := m.link(n)
	if err != nil {
		return err
	}
	v, err := ops.UpdateAgent(ctx, c, n, progressTo(r))
	if err != nil {
		return err
	}
	m.heard(id, v)
	m.log.Info("a node's agent was updated", "node", id, "agent", v.Agent)
	return nil
}

func (m *Manager) runUpdateXray(ctx context.Context, r *jobs.Run, ops Ops, id uint, first string) error {
	turn, err := m.takeTurn(ctx, r, first)
	if err != nil {
		return err
	}
	defer turn()
	n, err := m.nodeFor(id)
	if err != nil {
		return err
	}
	c, err := m.link(n)
	if err != nil {
		return err
	}
	v, err := ops.UpdateXray(ctx, c, n, progressTo(r))
	if err != nil {
		return err
	}
	m.heard(id, v)
	// the node may be in the bridge now, and the tunnel may come up
	m.Sync()
	m.log.Info("a node's Xray was updated", "node", id, "xray", v.Xray)
	return nil
}

// takeTurn waits for the other update that is running, if there is one, and
// gives the way to let the next one go. The first step says it is waiting.
func (m *Manager) takeTurn(ctx context.Context, r *jobs.Run, first string) (release func(), err error) {
	free := func() { <-m.updateGate }
	select {
	case m.updateGate <- struct{}{}:
		return free, nil
	default:
	}
	r.Begin(first)
	r.Detail(first, "Waiting for another update to finish")
	select {
	case m.updateGate <- struct{}{}:
		r.Detail(first, "")
		return free, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// heard takes in the versions that an update left on a node, which is how
// the state, the page and the events hear of them without waiting for the
// node's own loop to find out: it reports a change once, whichever finds it.
func (m *Manager) heard(id uint, v agentops.Versions) {
	m.mu.Lock()
	l := m.recordLocked(id)
	m.mu.Unlock()
	m.noteVersions(l, agentVersionOf(v.Agent), xrayVersionOf(v.Xray))
	m.saveVersions(l)
}
