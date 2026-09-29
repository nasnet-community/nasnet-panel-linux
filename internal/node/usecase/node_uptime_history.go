package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Reading side of the connectivity history: outages paired out of the raw
// transition events, and the daily rollup that outlives them.

// NodeOutage is one offline stretch, paired from the transition events.
// EndedAt is nil while the outage is still running.
type NodeOutage struct {
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	DurationSec int        `json:"duration_sec"`
	Reason      string     `json:"reason,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	// Planned marks an operator-caused stretch (disable, maintenance). Shown,
	// but kept out of downtime totals.
	Planned bool `json:"planned"`
	// Ongoing is true when the node is still down; DurationSec then measures
	// against now and keeps growing between requests.
	Ongoing bool `json:"ongoing"`
}

// NodeUptimeSummary is the rollup view for one node over a window.
type NodeUptimeSummary struct {
	Days         []*domain.NodeDailyUptime `json:"days"`
	ObservedSec  int                       `json:"observed_sec"`
	DowntimeSec  int                       `json:"downtime_sec"`
	PlannedSec   int                       `json:"planned_sec"`
	OutageCount  int                       `json:"outage_count"`
	UptimePct    float64                   `json:"uptime_pct"`
	LastOutageAt *time.Time                `json:"last_outage_at,omitempty"`
}

// GetNodeDisconnections returns a node's outages, newest first, capped at
// limit. Pairing happens here rather than in the client so the history list,
// the rollup and any future report agree on what "one outage" means.
func (u *nodeUsecase) GetNodeDisconnections(ctx context.Context, nodeID uint, since time.Time, limit int) ([]*NodeOutage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	events, err := u.nodeRepo.GetUptimeEvents(ctx, nodeID, since)
	if err != nil {
		return nil, err
	}
	outages := pairOutages(events, time.Now())
	// The built-in node's xray stops with the panel, so a hub-down window is
	// an outage of its own there (remote nodes keep serving; for them it is
	// only unobserved).
	if node, nerr := u.nodeRepo.GetNode(ctx, nodeID); nerr == nil && node.IsLocal() {
		if gaps, gerr := u.GetHubGaps(ctx, since); gerr == nil {
			outages = mergePanelDown(outages, gaps)
		}
	}

	// Newest first, then cap.
	for i, j := 0, len(outages)-1; i < j; i, j = i+1, j-1 {
		outages[i], outages[j] = outages[j], outages[i]
	}
	if len(outages) > limit {
		outages = outages[:limit]
	}
	return outages, nil
}

// pairOutages walks transition events oldest→newest and pairs each offline
// with the next online. A trailing offline is an ongoing outage measured
// against `now`. Consecutive offlines (possible only from legacy rows written
// by the old multi-writer paths) keep the first timestamp — the earliest
// evidence of the outage is the honest start.
func pairOutages(events []*domain.NodeUptimeEvent, now time.Time) []*NodeOutage {
	outages := make([]*NodeOutage, 0)
	var open *NodeOutage
	for _, ev := range events {
		if ev == nil {
			continue
		}
		switch ev.Status {
		case domain.NodeStatusOffline:
			if open != nil {
				continue
			}
			open = &NodeOutage{
				StartedAt: ev.Timestamp,
				Reason:    ev.Reason,
				Detail:    ev.Detail,
				Planned:   domain.IsPlannedOfflineReason(ev.Reason),
			}
		case domain.NodeStatusOnline:
			if open == nil {
				continue // recovery with no recorded start (pre-retention outage)
			}
			end := ev.Timestamp
			open.EndedAt = &end
			open.DurationSec = int(end.Sub(open.StartedAt).Seconds())
			if open.DurationSec < 0 {
				open.DurationSec = 0
			}
			outages = append(outages, open)
			open = nil
		}
	}
	if open != nil {
		open.Ongoing = true
		open.DurationSec = int(now.Sub(open.StartedAt).Seconds())
		if open.DurationSec < 0 {
			open.DurationSec = 0
		}
		outages = append(outages, open)
	}
	return outages
}

// GetNodeUptimeSummary aggregates the daily rollup over the last `days` days.
// UptimePct is measured against observed time only — hours in which the panel
// itself was down are excluded rather than counted as uptime nobody watched.
func (u *nodeUsecase) GetNodeUptimeSummary(ctx context.Context, nodeID uint, days int) (*NodeUptimeSummary, error) {
	if days <= 0 || days > 730 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	rows, err := u.nodeRepo.GetNodeDailyUptime(ctx, nodeID, since)
	if err != nil {
		return nil, err
	}
	// Rollup rows cover finished days; add today so far, computed live and
	// not stored, so the summary reflects an outage the moment it ends.
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	if len(rows) == 0 || rows[len(rows)-1].Date.UTC().Before(todayStart) {
		if node, nerr := u.nodeRepo.GetNode(ctx, nodeID); nerr == nil {
			if gaps, gerr := u.GetHubGaps(ctx, todayStart); gerr == nil {
				now := time.Now()
				if today, terr := u.rollupOneNode(ctx, node, todayStart, now, overlapSeconds(gaps, todayStart, now), gaps); terr == nil {
					rows = append(rows, today)
				}
			}
		}
	}
	out := &NodeUptimeSummary{Days: rows}
	for _, r := range rows {
		out.ObservedSec += r.ObservedSec
		out.DowntimeSec += r.DowntimeSec
		out.PlannedSec += r.PlannedSec
		out.OutageCount += r.OutageCount
	}
	if out.ObservedSec > 0 {
		up := out.ObservedSec - out.DowntimeSec
		if up < 0 {
			up = 0
		}
		out.UptimePct = float64(up) / float64(out.ObservedSec) * 100
	}
	if node, err := u.nodeRepo.GetNode(ctx, nodeID); err == nil && node.LastDisconnectedAt != nil {
		out.LastOutageAt = node.LastDisconnectedAt
	}
	return out, nil
}

// === Daily rollup ===

// RollupNodeUptime computes and stores one UTC day of connectivity for every
// node. Idempotent: re-running a day replaces its row, which is what lets the
// boot backfill and the daily task overlap safely.
func (u *nodeUsecase) RollupNodeUptime(ctx context.Context, day time.Time) error {
	dayStart := day.UTC().Truncate(24 * time.Hour)
	dayEnd := dayStart.AddDate(0, 0, 1)

	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return err
	}
	gaps, err := u.GetHubGaps(ctx, dayStart)
	if err != nil {
		return err
	}
	unobserved := overlapSeconds(gaps, dayStart, dayEnd)

	for _, node := range nodes {
		row, err := u.rollupOneNode(ctx, node, dayStart, dayEnd, unobserved, gaps)
		if err != nil {
			logger.GetLogger().WithError(err).Warnf("[uptime-rollup] node %d day %s failed", node.ID, dayStart.Format("2006-01-02"))
			continue
		}
		if err := u.nodeRepo.UpsertNodeDailyUptime(ctx, row); err != nil {
			logger.GetLogger().WithError(err).Warnf("[uptime-rollup] storing node %d day %s failed", node.ID, dayStart.Format("2006-01-02"))
		}
	}
	return nil
}

// rollupOneNode measures a single node's day. The state at midnight comes from
// the last event before the window — without it a day whose first event is
// "online" is ambiguous between "was down since yesterday" and "was up all
// along, untouched".
func (u *nodeUsecase) rollupOneNode(ctx context.Context, node *domain.Node, dayStart, dayEnd time.Time, unobservedSec int, gaps []domain.TimeWindow) (*domain.NodeDailyUptime, error) {
	nodeID := node.ID
	prior, err := u.nodeRepo.GetLastUptimeEventBefore(ctx, nodeID, dayStart)
	if err != nil {
		return nil, err
	}
	events, err := u.nodeRepo.GetUptimeEventsRange(ctx, nodeID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	// Seed a synthetic opening event so the walk starts in the right state.
	if prior != nil && prior.Status == domain.NodeStatusOffline {
		opening := &domain.NodeUptimeEvent{
			NodeID:    nodeID,
			Status:    domain.NodeStatusOffline,
			Reason:    prior.Reason,
			Timestamp: dayStart,
		}
		events = append([]*domain.NodeUptimeEvent{opening}, events...)
	}

	outages := pairOutages(events, dayEnd)
	if node.IsLocal() {
		return localDayUptime(nodeID, dayStart, dayEnd, outages, gaps), nil
	}

	downtime, planned, count := 0, 0, 0
	for _, outage := range outages {
		end := dayEnd
		if outage.EndedAt != nil && outage.EndedAt.Before(dayEnd) {
			end = *outage.EndedAt
		}
		start := outage.StartedAt
		if start.Before(dayStart) {
			start = dayStart
		}
		secs := int(end.Sub(start).Seconds())
		if secs <= 0 {
			continue
		}
		// Time inside a hub-down window isn't observed, so it can't be counted
		// as downtime either — otherwise an outage spanning a panel restart
		// would be charged against a denominator that already excluded it.
		if overlap := overlapSeconds(gaps, start, end); overlap > 0 {
			secs -= overlap
			if secs <= 0 {
				continue
			}
		}
		if outage.Planned {
			planned += secs
			continue
		}
		downtime += secs
		count++
	}

	observed := int(dayEnd.Sub(dayStart).Seconds()) - unobservedSec
	if observed < 0 {
		observed = 0
	}
	// Planned stretches are neither downtime nor uptime — they leave the
	// denominator so the percentage answers "was it reachable when it was
	// supposed to be".
	observed -= planned
	if observed < 0 {
		observed = 0
	}
	if downtime > observed {
		downtime = observed
	}

	return &domain.NodeDailyUptime{
		NodeID:      nodeID,
		Date:        dayStart,
		ObservedSec: observed,
		DowntimeSec: downtime,
		PlannedSec:  planned,
		OutageCount: count,
	}, nil
}

// BackfillNodeUptimeRollup fills any days missing between the newest rolled-up
// day and yesterday, capped at maxDays so a long-stopped panel doesn't grind
// through a year of events at boot.
func (u *nodeUsecase) BackfillNodeUptimeRollup(ctx context.Context, maxDays int) error {
	if maxDays <= 0 {
		maxDays = 30
	}
	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	latest, err := u.nodeRepo.GetLatestNodeDailyUptimeDate(ctx)
	if err != nil {
		return err
	}
	start := yesterday.AddDate(0, 0, -maxDays+1)
	if !latest.IsZero() {
		next := latest.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
		if next.After(start) {
			start = next
		}
	}
	for day := start; !day.After(yesterday); day = day.AddDate(0, 0, 1) {
		if err := u.RollupNodeUptime(ctx, day); err != nil {
			return err
		}
	}
	return nil
}

// overlapSeconds totals how much of [from, to) the given windows cover.
// Windows are assumed non-overlapping and ordered (GetHubGaps builds them so).
func overlapSeconds(windows []domain.TimeWindow, from, to time.Time) int {
	total := 0
	for _, w := range windows {
		start, end := w.From, w.To
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if secs := int(end.Sub(start).Seconds()); secs > 0 {
			total += secs
		}
	}
	return total
}

// === Built-in node: panel-down time is downtime ===

// localDayUptime measures the built-in node's day. Its xray is the panel's
// child process, so a window with no panel run is downtime (panel_down), not
// unobserved time, and it counts even after a clean shutdown for an update.
// Overlapping outages and gaps are merged so one stretch counts once; a
// planned stretch the panel was also down for stays planned.
func localDayUptime(nodeID uint, dayStart, dayEnd time.Time, outages []*NodeOutage, gaps []domain.TimeWindow) *domain.NodeDailyUptime {
	var down, planned []domain.TimeWindow
	for _, o := range outages {
		end := dayEnd
		if o.EndedAt != nil && o.EndedAt.Before(dayEnd) {
			end = *o.EndedAt
		}
		w, ok := clipWindow(domain.TimeWindow{From: o.StartedAt, To: end}, dayStart, dayEnd)
		if !ok {
			continue
		}
		if o.Planned {
			planned = append(planned, w)
		} else {
			down = append(down, w)
		}
	}
	for _, g := range gaps {
		if w, ok := clipWindow(g, dayStart, dayEnd); ok {
			down = append(down, w)
		}
	}
	down, planned = mergeWindows(down), mergeWindows(planned)

	plannedSec := windowsSeconds(planned)
	downSec := windowsSeconds(down) - intersectSeconds(down, planned)
	observed := int(dayEnd.Sub(dayStart).Seconds()) - plannedSec
	if observed < 0 {
		observed = 0
	}
	if downSec > observed {
		downSec = observed
	}
	return &domain.NodeDailyUptime{
		NodeID:      nodeID,
		Date:        dayStart,
		ObservedSec: observed,
		DowntimeSec: downSec,
		PlannedSec:  plannedSec,
		OutageCount: len(down),
	}
}

// mergePanelDown folds hub-down windows into the built-in node's outage list:
// a gap that overlaps a recorded outage widens it, one that doesn't becomes a
// panel_down outage of its own. Returns oldest first.
func mergePanelDown(outages []*NodeOutage, gaps []domain.TimeWindow) []*NodeOutage {
	for _, g := range gaps {
		merged := false
		for _, o := range outages {
			end := time.Now()
			if o.EndedAt != nil {
				end = *o.EndedAt
			}
			if g.From.Before(end) && o.StartedAt.Before(g.To) {
				if g.From.Before(o.StartedAt) {
					o.StartedAt = g.From
				}
				if o.EndedAt != nil && g.To.After(*o.EndedAt) {
					to := g.To
					o.EndedAt = &to
				}
				o.DurationSec = int(end.Sub(o.StartedAt).Seconds())
				if o.EndedAt != nil {
					o.DurationSec = int(o.EndedAt.Sub(o.StartedAt).Seconds())
				}
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		to := g.To
		outages = append(outages, &NodeOutage{
			StartedAt:   g.From,
			EndedAt:     &to,
			DurationSec: int(g.To.Sub(g.From).Seconds()),
			Reason:      domain.NodeOfflineReasonPanelDown,
			Detail:      "the panel was not running",
		})
	}
	sort.Slice(outages, func(i, j int) bool { return outages[i].StartedAt.Before(outages[j].StartedAt) })
	return outages
}

func clipWindow(w domain.TimeWindow, from, to time.Time) (domain.TimeWindow, bool) {
	if w.From.Before(from) {
		w.From = from
	}
	if w.To.After(to) {
		w.To = to
	}
	return w, w.To.After(w.From)
}

// mergeWindows sorts and joins overlapping or touching windows.
func mergeWindows(ws []domain.TimeWindow) []domain.TimeWindow {
	if len(ws) < 2 {
		return ws
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].From.Before(ws[j].From) })
	out := []domain.TimeWindow{ws[0]}
	for _, w := range ws[1:] {
		last := &out[len(out)-1]
		if !w.From.After(last.To) {
			if w.To.After(last.To) {
				last.To = w.To
			}
			continue
		}
		out = append(out, w)
	}
	return out
}

func windowsSeconds(ws []domain.TimeWindow) int {
	total := 0
	for _, w := range ws {
		total += int(w.To.Sub(w.From).Seconds())
	}
	return total
}

func intersectSeconds(a, b []domain.TimeWindow) int {
	total := 0
	for _, x := range a {
		total += overlapSeconds(b, x.From, x.To)
	}
	return total
}
