package usecase

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Connectivity transitions have exactly one writer: ObserveNodeHealth. Every
// detector (heartbeat streams, the stats sweep, the monitor poll, admin
// actions) reports observations here, and only this file flips is_online,
// stamps the transition columns, writes a node_uptime_event and publishes the
// SSE/alerting event. Before this, five call sites flipped the flag
// independently and only one of them recorded history — so the uptime timeline
// was missing most transitions.
//
// Offline observations are debounced (node_offline_debounce_sec): a node must
// keep failing for the whole window before the outage is committed, which
// keeps one flapping uplink from writing a row per bounce. The committed
// timestamp is when the failure *started*, not when it was confirmed, so
// durations stay honest.

const (
	// defaultOfflineDebounce mirrors the seeded node_offline_debounce_sec.
	defaultOfflineDebounce = 30 * time.Second
	// offlineDebounceCacheTTL keeps the settings read off the hot path.
	offlineDebounceCacheTTL = 30 * time.Second
	// detailMaxLen matches NodeUptimeEvent.Detail's column width.
	detailMaxLen = 255
)

// nodeConnState mirrors the persisted connectivity state so the fast
// detectors don't re-read the node row on every observation. The DB stays
// authoritative — this is only a cache, seeded from the node on first use.
type nodeConnState struct {
	// observe serialises one node's observations from the decision through
	// the commit, so two detectors can't both record the same transition.
	observe sync.Mutex
	online  bool
	// candidateSince is the start of an unconfirmed outage; zero = none.
	candidateSince time.Time
}

// connStateFor returns the cached state for a node, seeding it from the row
// the caller already holds. A hub restart inside a debounce window recovers
// the real outage start from OfflineCandidateSince.
func (u *nodeUsecase) connStateFor(node *domain.Node) *nodeConnState {
	u.connMu.Lock()
	defer u.connMu.Unlock()
	if u.connStates == nil {
		u.connStates = make(map[uint]*nodeConnState)
	}
	if st, ok := u.connStates[node.ID]; ok {
		return st
	}
	st := &nodeConnState{online: node.IsOnline}
	if node.OfflineCandidateSince != nil {
		st.candidateSince = *node.OfflineCandidateSince
	}
	u.connStates[node.ID] = st
	return st
}

// offlineDebounce reads node_offline_debounce_sec, cached briefly. 0 disables
// debouncing (transitions commit on first failed observation).
func (u *nodeUsecase) offlineDebounce(ctx context.Context) time.Duration {
	u.connMu.Lock()
	if !u.debounceCachedAt.IsZero() && time.Since(u.debounceCachedAt) < offlineDebounceCacheTTL {
		d := u.debounceCache
		u.connMu.Unlock()
		return d
	}
	u.connMu.Unlock()

	d := defaultOfflineDebounce
	if u.settingUC != nil {
		if v, err := u.settingUC.GetByKey(ctx, "node_offline_debounce_sec"); err == nil && v != "" {
			if secs, convErr := strconv.Atoi(strings.TrimSpace(v)); convErr == nil && secs >= 0 {
				d = time.Duration(secs) * time.Second
			}
		}
	}

	u.connMu.Lock()
	u.debounceCache, u.debounceCachedAt = d, time.Now()
	u.connMu.Unlock()
	return d
}

// ObserveNodeHealth reports one detection result for a node. Safe to call on
// every poll — it only writes when the observation changes the committed
// state, or when a pending outage crosses the debounce window.
//
// reason must be one of the domain.NodeOfflineReason*/NodeOnlineReason*
// constants; detail carries the underlying error for display only.
// Planned reasons (admin disable, maintenance, hub shutdown) skip the
// debounce — an operator action isn't a flap, and delaying it would misreport
// when the node actually left service.
func (u *nodeUsecase) ObserveNodeHealth(ctx context.Context, node *domain.Node, online bool, reason, detail string) {
	if node == nil || node.ID == 0 {
		return
	}
	st := u.connStateFor(node)
	st.observe.Lock()
	defer st.observe.Unlock()
	now := time.Now()
	// Detectors often report right after an RPC used up their deadline; the
	// writes that record the observation must not fail because of it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if online {
		u.observeOnline(ctx, node, st, reason, now)
		return
	}
	u.observeOffline(ctx, node, st, reason, detail, now)
}

func (u *nodeUsecase) observeOnline(ctx context.Context, node *domain.Node, st *nodeConnState, reason string, now time.Time) {
	u.connMu.Lock()
	hadCandidate := !st.candidateSince.IsZero()
	wasOnline := st.online
	st.candidateSince = time.Time{}
	u.connMu.Unlock()

	// A failure that recovered inside the debounce window never became an
	// outage — drop the candidate and record nothing.
	if hadCandidate {
		if err := u.nodeRepo.SetOfflineCandidate(ctx, node.ID, nil); err != nil {
			logger.GetLogger().WithError(err).Warnf("[connectivity] clear offline candidate for node %d failed", node.ID)
		}
	}
	if wasOnline {
		return
	}
	u.commitTransition(ctx, node, st, true, reason, "", now)
}

func (u *nodeUsecase) observeOffline(ctx context.Context, node *domain.Node, st *nodeConnState, reason, detail string, now time.Time) {
	planned := domain.IsPlannedOfflineReason(reason) || reason == domain.NodeOfflineReasonHubShutdown

	u.connMu.Lock()
	wasOnline := st.online
	candidate := st.candidateSince
	u.connMu.Unlock()

	if !wasOnline {
		return // already recorded as down; nothing new to say
	}
	if planned {
		u.commitTransition(ctx, node, st, false, reason, detail, now)
		return
	}

	debounce := u.offlineDebounce(ctx)
	if debounce <= 0 {
		u.commitTransition(ctx, node, st, false, reason, detail, now)
		return
	}

	if candidate.IsZero() {
		// First failure — start the window, persist it so a restart mid-window
		// still knows when this outage began.
		u.connMu.Lock()
		st.candidateSince = now
		u.connMu.Unlock()
		if err := u.nodeRepo.SetOfflineCandidate(ctx, node.ID, &now); err != nil {
			logger.GetLogger().WithError(err).Warnf("[connectivity] set offline candidate for node %d failed", node.ID)
		}
		return
	}
	if now.Sub(candidate) < debounce {
		return // still inside the window; not an outage yet
	}
	// Confirmed: attribute it to when the failure started.
	u.commitTransition(ctx, node, st, false, reason, detail, candidate)
}

// commitTransition persists a confirmed transition and announces it. `at` is
// the moment the state actually changed (outage start for offline).
func (u *nodeUsecase) commitTransition(ctx context.Context, node *domain.Node, st *nodeConnState, online bool, reason, detail string, at time.Time) {
	log := logger.GetLogger()
	status := domain.NodeStatusOffline
	if online {
		status = domain.NodeStatusOnline
	}
	if len(detail) > detailMaxLen {
		detail = detail[:detailMaxLen]
	}
	// Stored in UTC: SQLite compares times as text (see the repository).
	at = at.UTC()

	if err := u.nodeRepo.UpdateNodeConnectivity(ctx, node.ID, online, at, reason); err != nil {
		log.WithError(err).Warnf("[connectivity] persist %s for node %d failed", status, node.ID)
		return // don't record history we failed to commit
	}
	if err := u.nodeRepo.CreateUptimeEvent(ctx, &domain.NodeUptimeEvent{
		NodeID:    node.ID,
		Status:    status,
		Reason:    reason,
		Detail:    detail,
		Timestamp: at,
	}); err != nil {
		log.WithError(err).Warnf("[connectivity] record %s event for node %d failed", status, node.ID)
	}

	u.connMu.Lock()
	st.online = online
	st.candidateSince = time.Time{}
	u.connMu.Unlock()

	// Keep the caller's copy consistent — several detectors branch on it
	// right after reporting.
	node.IsOnline = online

	u.publishConnectivityEvent(node, online, reason, detail, at)
	log.WithFields(map[string]interface{}{
		"node":   node.Name,
		"reason": reason,
		"at":     at.Format(time.RFC3339),
	}).Infof("[connectivity] node %s", status)
}

// publishConnectivityEvent emits node.online / node.offline. OutageStartedAt
// carries the real transition time so the alerting engine can measure its
// grace from when the node went down, not from when the debounce confirmed it.
func (u *nodeUsecase) publishConnectivityEvent(node *domain.Node, online bool, reason, detail string, at time.Time) {
	if u.eventBus == nil {
		return
	}
	evType := events.EventNodeOffline
	if online {
		evType = events.EventNodeOnline
	}
	startedAt := at
	u.eventBus.Publish(events.Event{
		Type:      evType,
		Timestamp: time.Now(),
		Payload: events.NodeStatusPayload{
			NodeID:          node.ID,
			NodeName:        node.Name,
			IP:              node.IP,
			IsOnline:        online,
			Message:         detail,
			Reason:          reason,
			Planned:         !online && domain.IsPlannedOfflineReason(reason),
			OutageStartedAt: &startedAt,
		},
	})
}

// ObserveNodeMaintenance records entering/leaving maintenance as a planned
// stretch. Leaving maintenance records nothing: whether the node is actually
// reachable is for the detectors to say, not for the operator's click.
func (u *nodeUsecase) ObserveNodeMaintenance(ctx context.Context, nodeID uint, active bool) {
	if !active {
		return
	}
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		logger.GetLogger().WithError(err).Warnf("[connectivity] load node %d for maintenance transition failed", nodeID)
		return
	}
	u.ObserveNodeHealth(ctx, node, false, domain.NodeOfflineReasonMaintenance, "maintenance mode enabled")
}

// HasLiveHeartbeatSession reports whether a node currently has a heartbeat
// stream (direct) or a passive reverse session. The monitor uses it to stay
// out of the way: two detectors on the same node race and produce paired
// flaps that no debounce can fully absorb.
func (u *nodeUsecase) HasLiveHeartbeatSession(nodeID uint) bool {
	if u.heartbeatMgr == nil {
		return false
	}
	_, _, ok := u.heartbeatMgr.GetSessionInfo(nodeID)
	return ok
}

// === Hub lifecycle ===

// HubLifecycle brackets one run of the hub so the UI can grey out windows
// where nothing was watching the nodes, instead of stretching the last known
// state across them.
type HubLifecycle struct {
	mu      sync.Mutex
	repo    hubLifecycleRepo
	eventID uint
	stop    chan struct{}
}

type hubLifecycleRepo interface {
	CreateHubLifecycleEvent(ctx context.Context, event *domain.HubLifecycleEvent) error
	TouchHubLifecycleEvent(ctx context.Context, id uint, at time.Time) error
	CloseHubLifecycleEvent(ctx context.Context, id uint, at time.Time) error
	CloseStaleHubLifecycleEvents(ctx context.Context, at time.Time) (int64, error)
}

// hubAliveInterval is how often a run stamps that it is still alive. A run
// that ends uncleanly (power loss, kill -9) is closed at its last stamp, so at
// most this much of the outage is mistaken for panel uptime — which matters
// most for the built-in node, whose xray goes down with the panel.
const hubAliveInterval = 30 * time.Second

// OpenHubLifecycle closes any run left open by an unclean exit at its last
// liveness stamp (now, for rows from before stamps existed — leaving it open
// would swallow every later gap), then opens this run and keeps it stamped
// until ctx ends or Close.
func OpenHubLifecycle(ctx context.Context, repo hubLifecycleRepo, version string) *HubLifecycle {
	log := logger.GetLogger()
	if closed, err := repo.CloseStaleHubLifecycleEvents(ctx, time.Now().UTC()); err != nil {
		log.WithError(err).Warn("[connectivity] closing stale hub lifecycle rows failed")
	} else if closed > 0 {
		log.Warnf("[connectivity] closed %d hub lifecycle row(s) left open by an unclean shutdown", closed)
	}
	// Alive from the start: a run that dies before its first 30 s stamp is
	// then closed at its start, not at the next boot (which would count the
	// whole outage as panel uptime).
	started := time.Now().UTC()
	ev := &domain.HubLifecycleEvent{StartedAt: started, LastAliveAt: &started, Version: version}
	if err := repo.CreateHubLifecycleEvent(ctx, ev); err != nil {
		log.WithError(err).Warn("[connectivity] opening hub lifecycle row failed")
		return &HubLifecycle{repo: repo}
	}
	h := &HubLifecycle{repo: repo, eventID: ev.ID, stop: make(chan struct{})}
	go h.keepAlive(ctx, h.stop)
	return h
}

// keepAlive stamps the open run every hubAliveInterval.
func (h *HubLifecycle) keepAlive(ctx context.Context, stop <-chan struct{}) {
	t := time.NewTicker(hubAliveInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			h.mu.Lock()
			id := h.eventID
			h.mu.Unlock()
			if id == 0 {
				return
			}
			wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := h.repo.TouchHubLifecycleEvent(wctx, id, time.Now().UTC()); err != nil {
				logger.GetLogger().WithError(err).Debug("[connectivity] stamping hub lifecycle row failed")
			}
			cancel()
		}
	}
}

// Close stamps this run's end. Idempotent — shutdown paths double-fire.
func (h *HubLifecycle) Close(ctx context.Context) {
	if h == nil {
		return
	}
	h.mu.Lock()
	id := h.eventID
	h.eventID = 0
	if h.stop != nil {
		close(h.stop)
		h.stop = nil
	}
	h.mu.Unlock()
	if id == 0 {
		return
	}
	if err := h.repo.CloseHubLifecycleEvent(ctx, id, time.Now().UTC()); err != nil {
		logger.GetLogger().WithError(err).Warn("[connectivity] closing hub lifecycle row failed")
	}
}

// GetHubGaps returns the windows since `since` in which no hub run was
// active — the periods the timeline must render as unknown rather than
// claiming uptime nobody observed.
func (u *nodeUsecase) GetHubGaps(ctx context.Context, since time.Time) ([]domain.TimeWindow, error) {
	runs, err := u.nodeRepo.GetHubLifecycleEvents(ctx, since)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	gaps := make([]domain.TimeWindow, 0)
	cursor := since
	// Before the first recorded run there is no lifecycle data at all — most
	// obviously for history that predates this feature. Treating that as
	// unobserved would grey out weeks of perfectly good timeline, so the window
	// starts at the first run we know about. That is the first run ever, not
	// the first in the window: a panel that was down across `since` still
	// shows the part of that gap inside the window.
	first, err := u.nodeRepo.FirstHubLifecycleStart(ctx)
	if err != nil {
		return nil, err
	}
	if first.IsZero() {
		return gaps, nil
	}
	if first.After(cursor) {
		cursor = first
	}
	for _, run := range runs {
		start := run.StartedAt
		// A run killed without a clean stop is closed at the next boot, often
		// in the same second that boot starts: that seam is not a gap.
		if start.Sub(cursor) >= minHubGap {
			gaps = append(gaps, domain.TimeWindow{From: cursor, To: start})
		}
		end := now
		if run.StoppedAt != nil {
			end = *run.StoppedAt
		}
		if end.After(cursor) {
			cursor = end
		}
	}
	if now.Sub(cursor) >= minHubGap {
		gaps = append(gaps, domain.TimeWindow{From: cursor, To: now})
	}
	return gaps, nil
}

// minHubGap: shorter seams between runs are bookkeeping, not downtime.
const minHubGap = time.Second
