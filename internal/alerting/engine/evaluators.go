package engine

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// ----- node_offline -----

func (e *Engine) evalNodeOffline(ctx context.Context, p events.NodeStatusPayload) {
	// Operator-caused stretches (disable, maintenance) are recorded in the
	// node's history but are not faults — alerting on them trains people to
	// ignore the alert.
	if p.Planned {
		e.forgetOfflineSince(p.NodeID)
		return
	}
	e.rememberOfflineSince(p)
	rules := e.rulesOfType(ctx, domain.RuleTypeNodeOffline)
	for _, rule := range rules {
		e.evalNodeOfflineForRule(ctx, rule, p)
	}
}

// evalNodeOfflineForRule honours the rule's DurationSec as a grace period,
// measured from when the node actually went down (OutageStartedAt) rather than
// from when the event reached us — the connectivity writer debounces offline
// transitions, and measuring from arrival would silently add that window to
// every configured grace. Re-checked on the engine tick so a node that stays
// down eventually fires without a new event arriving.
func (e *Engine) evalNodeOfflineForRule(ctx context.Context, rule *domain.Rule, p events.NodeStatusPayload) {
	if !e.ruleAppliesToNode(rule, p.NodeID) {
		return
	}
	since := e.offlineSince(p.NodeID)
	if grace := time.Duration(rule.Threshold.DurationSec) * time.Second; grace > 0 {
		if since.IsZero() || time.Since(since) < grace {
			return
		}
	}
	entity := nodeEntityKey(p.NodeID)
	title := fmt.Sprintf("Node offline: %s", displayName(p.NodeName, p.NodeID))
	msg := fmt.Sprintf("Node %s (%s) is not responding.", displayName(p.NodeName, p.NodeID), p.IP)
	if !since.IsZero() {
		msg = msg + fmt.Sprintf(" Down for %s.", time.Since(since).Round(time.Second))
	}
	if p.Message != "" {
		msg = msg + " Detail: " + p.Message
	}
	e.fireOrSkip(ctx, rule, entity, title, msg, valueAsJSON(p))
}

func (e *Engine) resolveNodeOffline(ctx context.Context, p events.NodeStatusPayload) {
	e.forgetOfflineSince(p.NodeID)
	rules := e.rulesOfType(ctx, domain.RuleTypeNodeOffline)
	entity := nodeEntityKey(p.NodeID)
	for _, rule := range rules {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		title := fmt.Sprintf("Recovered: %s is back online", displayName(p.NodeName, p.NodeID))
		msg := fmt.Sprintf("Node %s (%s) is responding again.", displayName(p.NodeName, p.NodeID), p.IP)
		e.resolveIfFiring(ctx, rule, entity, title, msg)
	}
}

// ----- node_crash_loop -----

// recordCrash appends a timestamp to the in-memory crash window and
// prunes anything older than the widest configured WindowSec. Cheap
// because xray downs aren't that frequent.
func (e *Engine) recordCrash(nodeID uint) {
	now := time.Now()
	cutoff := now.Add(-30 * time.Minute) // prune floor; evaluators re-check per-rule
	e.mu.Lock()
	defer e.mu.Unlock()
	w := e.crashWindows[nodeID]
	pruned := w[:0]
	for _, t := range w {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	pruned = append(pruned, now)
	e.crashWindows[nodeID] = pruned
}

// crashCountWithin returns how many crashes are currently in the window.
func (e *Engine) crashCountWithin(nodeID uint, windowSec int) int {
	if windowSec <= 0 {
		windowSec = 300
	}
	cutoff := time.Now().Add(-time.Duration(windowSec) * time.Second)
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, t := range e.crashWindows[nodeID] {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

func (e *Engine) evalCrashLoop(ctx context.Context, p events.XrayStatusPayload) {
	rules := e.rulesOfType(ctx, domain.RuleTypeNodeCrashLoop)
	entity := nodeEntityKey(p.NodeID)
	for _, rule := range rules {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		threshold := rule.Threshold.Count
		if threshold <= 0 {
			threshold = 5
		}
		count := e.crashCountWithin(p.NodeID, rule.Threshold.WindowSec)
		if count < threshold {
			continue
		}
		title := fmt.Sprintf("Xray crash loop: %s", displayName(p.NodeName, p.NodeID))
		msg := fmt.Sprintf("Xray on %s has crashed %d times in the last %ds (threshold %d).", displayName(p.NodeName, p.NodeID), count, rule.Threshold.WindowSec, threshold)
		if p.ErrorLog != "" {
			msg = msg + "\nLast error: " + p.ErrorLog
		}
		e.fireOrSkip(ctx, rule, entity, title, msg, valueAsJSON(p))
	}
}

func (e *Engine) resolveCrashLoop(ctx context.Context, p events.XrayStatusPayload) {
	rules := e.rulesOfType(ctx, domain.RuleTypeNodeCrashLoop)
	entity := nodeEntityKey(p.NodeID)
	for _, rule := range rules {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		title := fmt.Sprintf("Xray recovered: %s", displayName(p.NodeName, p.NodeID))
		msg := fmt.Sprintf("Xray on %s is up again.", displayName(p.NodeName, p.NodeID))
		e.resolveIfFiring(ctx, rule, entity, title, msg)
	}
}

// ----- MTProto nodes -----

// mtprotoWindowCap bounds a window: a misbehaving agent can send thousands a second.
const mtprotoWindowCap = 4096

type mtprotoFire struct {
	ruleID   uint
	nodeID   uint
	until    time.Time // end of the cooldown: events before it skip the database
	restored bool      // firing before a restart: quiet is measured from the engine's start
}

// mtprotoHit is n occurrences at one moment.
type mtprotoHit struct {
	at time.Time
	n  int
}

// rehydrateMTProto lets the tick resolve counting alerts left firing by the last run.
func (e *Engine) rehydrateMTProto(ctx context.Context, firing []*domain.State) {
	rules, err := e.repo.ListEnabledRules(ctx)
	if err != nil {
		return
	}
	byID := make(map[uint]*domain.Rule, len(rules))
	for _, r := range rules {
		byID[r.ID] = r
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range firing {
		r := byID[s.RuleID]
		if r == nil || (r.RuleType != domain.RuleTypeMTProtoAllowlistViolation && r.RuleType != domain.RuleTypeMTProtoEgressDown) {
			continue
		}
		var nodeID uint
		if _, err := fmt.Sscanf(s.EntityKey, "node:%d", &nodeID); err != nil {
			continue
		}
		until := time.Now()
		if s.LastNotifiedAt != nil {
			until = s.LastNotifiedAt.Add(time.Duration(r.CooldownSec) * time.Second)
		}
		e.mtprotoFiring[uintToKey(r.ID)+":"+s.EntityKey] = mtprotoFire{ruleID: r.ID, nodeID: nodeID, until: until, restored: true}
	}
}

func (e *Engine) handleMTProto(ctx context.Context, t events.EventType, p events.MTProtoPayload) {
	switch t {
	case events.EventMTProtoTelemtCrashLoop:
		e.evalMTProtoCrashLoop(ctx, p)
	case events.EventMTProtoTelemtUp:
		e.resolveMTProtoCrashLoop(ctx, p)
	case events.EventMTProtoViolation:
		e.evalMTProtoViolation(ctx, p)
	case events.EventMTProtoEgressFailed:
		e.evalMTProtoEgress(ctx, p)
	case events.EventMTProtoDisguiseDown:
		e.evalMTProtoDisguise(ctx, p, true)
	case events.EventMTProtoDisguiseUp:
		e.evalMTProtoDisguise(ctx, p, false)
	}
}

// evalMTProtoCrashLoop fires at once: the agent already counted the exits.
func (e *Engine) evalMTProtoCrashLoop(ctx context.Context, p events.MTProtoPayload) {
	name := displayName(p.NodeName, p.NodeID)
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeNodeCrashLoop) {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		msg := fmt.Sprintf("The proxy on %s keeps crashing.", name)
		if p.Detail != "" {
			msg += "\nLast exit: " + p.Detail
		}
		e.fireOrSkip(ctx, rule, nodeEntityKey(p.NodeID), "Proxy crash loop: "+name, msg, valueAsJSON(p))
	}
}

// resolveMTProtoCrashLoop: Detail says why the proxy is stopped, when the loop ended with it stopped.
func (e *Engine) resolveMTProtoCrashLoop(ctx context.Context, p events.MTProtoPayload) {
	name := displayName(p.NodeName, p.NodeID)
	title, msg := "Proxy recovered: "+name, fmt.Sprintf("The proxy on %s is running again.", name)
	if p.Detail != "" {
		title, msg = "Proxy no longer crash-looping: "+name, fmt.Sprintf("The proxy on %s stopped crash-looping. It is not running: %s", name, p.Detail)
	}
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeNodeCrashLoop) {
		if e.ruleAppliesToNode(rule, p.NodeID) {
			e.resolveIfFiring(ctx, rule, nodeEntityKey(p.NodeID), title, msg)
		}
	}
}

func (e *Engine) evalMTProtoViolation(ctx context.Context, p events.MTProtoPayload) {
	e.evalMTProtoCount(ctx, domain.RuleTypeMTProtoAllowlistViolation, p, func(name string, n int, rule *domain.Rule) (string, string) {
		return "Refused destinations: " + name, fmt.Sprintf("%s asked for %d destinations outside the Telegram address list in the last %ds (threshold %d). Last: %s",
			name, n, rule.Threshold.WindowSec, rule.Threshold.Count, p.Detail)
	})
}

func (e *Engine) evalMTProtoEgress(ctx context.Context, p events.MTProtoPayload) {
	e.evalMTProtoCount(ctx, domain.RuleTypeMTProtoEgressDown, p, func(name string, n int, rule *domain.Rule) (string, string) {
		msg := fmt.Sprintf("The panel failed to reach Telegram for %s %d times in the last %ds (threshold %d).", name, n, rule.Threshold.WindowSec, rule.Threshold.Count)
		if p.Error != "" {
			msg += "\nLast error: " + p.Error
		}
		return "Telegram unreachable for " + name, msg
	})
}

// evalMTProtoCount stays in memory during a cooldown, so a flood is not a query per event.
func (e *Engine) evalMTProtoCount(ctx context.Context, t domain.RuleType, p events.MTProtoPayload, text func(name string, n int, rule *domain.Rule) (string, string)) {
	e.recordMTProto(t, p.NodeID, max(p.Count, 1))
	entity := nodeEntityKey(p.NodeID)
	now := time.Now()
	for _, rule := range e.cachedRulesOfType(ctx, t) {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		key := uintToKey(rule.ID) + ":" + entity
		e.mu.Lock()
		f, firing := e.mtprotoFiring[key]
		e.mu.Unlock()
		if firing && now.Before(f.until) {
			continue
		}
		threshold := rule.Threshold.Count
		if threshold <= 0 {
			threshold = 1
		}
		n := e.mtprotoCountWithin(t, p.NodeID, rule.Threshold.WindowSec)
		if n < threshold {
			continue
		}
		title, msg := text(displayName(p.NodeName, p.NodeID), n, rule)
		// not sent means the stored state is still cooling down (say, from before a restart)
		until := now.Add(30 * time.Second)
		if e.fireOrSkip(ctx, rule, entity, title, msg, valueAsJSON(p)) {
			until = now.Add(time.Duration(rule.CooldownSec) * time.Second)
		}
		e.mu.Lock()
		e.mtprotoFiring[key] = mtprotoFire{ruleID: rule.ID, nodeID: p.NodeID, until: until}
		e.mu.Unlock()
	}
}

// resolveQuietMTProto clears a counting alert once its node has a quiet window.
func (e *Engine) resolveQuietMTProto(ctx context.Context, rule *domain.Rule) {
	type quiet struct {
		key    string
		nodeID uint
	}
	window := time.Duration(max(rule.Threshold.WindowSec, 0)) * time.Second
	if window == 0 {
		window = 300 * time.Second
	}
	var done []quiet
	e.mu.Lock()
	watched := time.Since(e.startTime) >= window
	for key, f := range e.mtprotoFiring {
		// a restored alert's events died with the old process: wait a whole window
		if f.ruleID == rule.ID && (!f.restored || watched) {
			done = append(done, quiet{key, f.nodeID})
		}
	}
	e.mu.Unlock()
	for _, q := range done {
		if e.mtprotoCountWithin(rule.RuleType, q.nodeID, rule.Threshold.WindowSec) > 0 {
			continue
		}
		e.mu.Lock()
		delete(e.mtprotoFiring, q.key)
		name := displayName(e.nodeMeta[q.nodeID].Name, q.nodeID)
		e.mu.Unlock()
		title, msg := "Recovered: Telegram reachable for "+name, fmt.Sprintf("No failed Telegram connections for %s in the last %ds.", name, rule.Threshold.WindowSec)
		if rule.RuleType == domain.RuleTypeMTProtoAllowlistViolation {
			title, msg = "Recovered: no refused destinations on "+name, fmt.Sprintf("%s asked for nothing outside the Telegram address list in the last %ds.", name, rule.Threshold.WindowSec)
		}
		e.resolveIfFiring(ctx, rule, nodeEntityKey(q.nodeID), title, msg)
	}
}

func (e *Engine) evalMTProtoDisguise(ctx context.Context, p events.MTProtoPayload, down bool) {
	name := displayName(p.NodeName, p.NodeID)
	entity := nodeEntityKey(p.NodeID)
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeMTProtoDisguiseDown) {
		if !e.ruleAppliesToNode(rule, p.NodeID) {
			continue
		}
		if !down {
			e.resolveIfFiring(ctx, rule, entity, "Recovered: cover site on "+name, fmt.Sprintf("The cover site %s works from %s again.", p.Detail, name))
			continue
		}
		msg := fmt.Sprintf("The cover site %s no longer works from %s.", p.Detail, name)
		if p.Error != "" {
			msg += "\n" + p.Error
		}
		e.fireOrSkip(ctx, rule, entity, "Cover site down: "+name, msg, valueAsJSON(p))
	}
}

func mtprotoWindowKey(t domain.RuleType, nodeID uint) string {
	return string(t) + ":" + uintToKey(nodeID)
}

// recordMTProto prunes to the crash window's 30 min floor.
func (e *Engine) recordMTProto(t domain.RuleType, nodeID uint, n int) {
	now := time.Now()
	cutoff := now.Add(-30 * time.Minute)
	k := mtprotoWindowKey(t, nodeID)
	e.mu.Lock()
	defer e.mu.Unlock()
	w := e.mtprotoWindows[k]
	w = append(w[sort.Search(len(w), func(i int) bool { return w[i].at.After(cutoff) }):], mtprotoHit{at: now, n: n})
	if len(w) > mtprotoWindowCap {
		w = w[len(w)-mtprotoWindowCap:]
	}
	e.mtprotoWindows[k] = w
}

func (e *Engine) mtprotoCountWithin(t domain.RuleType, nodeID uint, windowSec int) int {
	if windowSec <= 0 {
		windowSec = 300
	}
	cutoff := time.Now().Add(-time.Duration(windowSec) * time.Second)
	e.mu.Lock()
	defer e.mu.Unlock()
	w := e.mtprotoWindows[mtprotoWindowKey(t, nodeID)]
	n := 0
	for _, h := range w[sort.Search(len(w), func(i int) bool { return w[i].at.After(cutoff) }):] {
		n += h.n
	}
	return n
}

// cachedRulesOfType is rulesOfType behind a one-second cache.
func (e *Engine) cachedRulesOfType(ctx context.Context, t domain.RuleType) []*domain.Rule {
	e.mu.Lock()
	all, at := e.ruleCache, e.ruleCacheAt
	e.mu.Unlock()
	if at.IsZero() || time.Since(at) > time.Second {
		var err error
		if all, err = e.repo.ListEnabledRules(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("AlertEngine: list enabled rules failed")
			return nil
		}
		e.mu.Lock()
		e.ruleCache, e.ruleCacheAt = all, time.Now()
		e.mu.Unlock()
	}
	var out []*domain.Rule
	for _, r := range all {
		if r.RuleType == t {
			out = append(out, r)
		}
	}
	return out
}

// ----- high_cpu / high_disk -----

// evalStatsThresholds routes a stats payload to every matching rule.
// Called on every NodeStatsUpdated event.
func (e *Engine) evalStatsThresholds(ctx context.Context, p events.NodeStatsPayload) {
	rules := e.rulesOfType(ctx, domain.RuleTypeHighCPU)
	for _, r := range rules {
		e.evalStatsForRule(ctx, r, p)
	}
	rules = e.rulesOfType(ctx, domain.RuleTypeHighDisk)
	for _, r := range rules {
		e.evalStatsForRule(ctx, r, p)
	}
}

// evalStatsForRule applies a single sustained-threshold rule to one
// node's latest stats. Maintains a "first over" timestamp so the rule
// only fires once the condition has persisted for DurationSec.
func (e *Engine) evalStatsForRule(ctx context.Context, rule *domain.Rule, p events.NodeStatsPayload) {
	if !e.ruleAppliesToNode(rule, p.NodeID) {
		return
	}

	var actual float64
	var label string
	switch rule.RuleType {
	case domain.RuleTypeHighCPU:
		actual = p.CPUPercent
		label = "CPU"
	case domain.RuleTypeHighDisk:
		actual = p.DiskPercent
		label = "Disk"
	default:
		return
	}

	threshold := rule.Threshold.Value
	if threshold <= 0 {
		threshold = 90
	}
	duration := time.Duration(rule.Threshold.DurationSec) * time.Second
	entity := nodeEntityKey(p.NodeID)
	trackerKey := uintToKey(rule.ID) + ":" + entity

	e.mu.Lock()
	over := actual >= threshold
	firstOver, tracked := e.firstOverAt[trackerKey]
	if over && !tracked {
		e.firstOverAt[trackerKey] = time.Now()
		e.mu.Unlock()
		return
	}
	if !over && tracked {
		delete(e.firstOverAt, trackerKey)
		e.mu.Unlock()
		// Condition cleared — resolve if we were firing.
		title := fmt.Sprintf("Recovered: %s on %s", label, displayName(p.NodeName, p.NodeID))
		msg := fmt.Sprintf("%s on %s back under %.0f%% (now %.1f%%).", label, displayName(p.NodeName, p.NodeID), threshold, actual)
		e.resolveIfFiring(ctx, rule, entity, title, msg)
		return
	}
	e.mu.Unlock()

	if !over {
		return
	}
	// Over threshold long enough?
	if duration > 0 && time.Since(firstOver) < duration {
		return
	}
	title := fmt.Sprintf("High %s on %s", label, displayName(p.NodeName, p.NodeID))
	msg := fmt.Sprintf("%s on %s is %.1f%% (threshold %.0f%% for %ds).", label, displayName(p.NodeName, p.NodeID), actual, threshold, rule.Threshold.DurationSec)
	e.fireOrSkip(ctx, rule, entity, title, msg, fmt.Sprintf(`{"value":%.2f,"threshold":%.2f}`, actual, threshold))
}

// displayName falls back to "node #N" when we don't have a friendly name
// (e.g. stats event without a Name field). Keeps alerts readable.
func displayName(name string, id uint) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("node #%d", id)
}

// ----- reverse_tunnel_down -----

func (e *Engine) evalTunnelDown(ctx context.Context, p events.ReverseTunnelPayload) {
	e.mu.Lock()
	if prev, ok := e.tunnelsDown[p.TunnelID]; ok && prev.Since.Before(p.Since) {
		p.Since = prev.Since
	}
	e.tunnelsDown[p.TunnelID] = p
	e.mu.Unlock()
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeReverseTunnelDown) {
		e.evalTunnelDownForRule(ctx, rule, p)
	}
}

// evalTunnelDownForRule honours DurationSec as a grace period from when the
// portal stopped seeing the bridge; config pushes restart both ends and drop
// the tunnel for a few seconds.
func (e *Engine) evalTunnelDownForRule(ctx context.Context, rule *domain.Rule, p events.ReverseTunnelPayload) {
	if !e.ruleAppliesToNode(rule, p.PortalNodeID) {
		return
	}
	if grace := time.Duration(rule.Threshold.DurationSec) * time.Second; time.Since(p.Since) < grace {
		return
	}
	// A portal that is offline itself is the node-offline alert's to report.
	e.mu.Lock()
	online, known := e.nodeOnline[p.PortalNodeID]
	e.mu.Unlock()
	if known && !online {
		return
	}
	title := fmt.Sprintf("Reverse tunnel down: %s", p.Name)
	msg := fmt.Sprintf("The bridge of reverse tunnel %s hasn't been connected to its portal for %s.", p.Name, time.Since(p.Since).Round(time.Second))
	e.fireOrSkip(ctx, rule, tunnelEntityKey(p.TunnelID), title, msg, valueAsJSON(p))
}

func (e *Engine) resolveTunnelDown(ctx context.Context, p events.ReverseTunnelPayload) {
	e.mu.Lock()
	delete(e.tunnelsDown, p.TunnelID)
	e.mu.Unlock()
	if p.Removed {
		e.clearTunnelDown(ctx, p)
		return
	}
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeReverseTunnelDown) {
		if !e.ruleAppliesToNode(rule, p.PortalNodeID) {
			continue
		}
		title := fmt.Sprintf("Recovered: reverse tunnel %s is up", p.Name)
		msg := fmt.Sprintf("The bridge of reverse tunnel %s is connected again", p.Name)
		if p.BridgeIP != "" {
			msg += " from " + p.BridgeIP
		}
		e.resolveIfFiring(ctx, rule, tunnelEntityKey(p.TunnelID), title, msg+".")
	}
}

// clearTunnelDown closes a deleted tunnel's alert without a recovery notice:
// nothing recovered, the tunnel is gone.
func (e *Engine) clearTunnelDown(ctx context.Context, p events.ReverseTunnelPayload) {
	entity := tunnelEntityKey(p.TunnelID)
	for _, rule := range e.rulesOfType(ctx, domain.RuleTypeReverseTunnelDown) {
		state, err := e.repo.GetState(ctx, rule.ID, entity)
		if err != nil || state == nil || !state.Firing {
			continue
		}
		now := time.Now()
		_ = e.repo.InsertEvent(ctx, &domain.Event{
			RuleID:    rule.ID,
			EntityKey: entity,
			Status:    domain.EventStatusResolved,
			Title:     fmt.Sprintf("Removed: reverse tunnel %s", p.Name),
			Message:   fmt.Sprintf("Reverse tunnel %s was deleted.", p.Name),
			CreatedAt: now,
		})
		_ = e.repo.UpsertState(ctx, &domain.State{
			RuleID:           rule.ID,
			EntityKey:        entity,
			Firing:           false,
			FirstTriggeredAt: state.FirstTriggeredAt,
			LastNotifiedAt:   &now,
			UpdatedAt:        now,
		})
	}
}

func tunnelEntityKey(tunnelID uint) string {
	return fmt.Sprintf("tunnel:%d", tunnelID)
}
