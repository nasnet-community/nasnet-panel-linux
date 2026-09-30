package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
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
