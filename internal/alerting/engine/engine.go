package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

const subscriberID = "alert-engine"

// gracePeriodAfterStart suppresses fresh alert firings during the first
// N seconds after the engine starts. Prevents a restart storm where
// every node looks offline for 60s while agents reconnect.
const gracePeriodAfterStart = 2 * time.Minute

// evalTickInterval drives sustained-threshold rules that need time-based
// re-evaluation (e.g. high_cpu needs to check "has this persisted?").
const evalTickInterval = 30 * time.Second

// Engine runs rule evaluation. It subscribes to the EventBus for
// event-driven rules and ticks periodically for sustained-threshold
// rules that depend on accumulated state.
type Engine struct {
	bus       *events.EventBus
	repo      repository.AlertRepository
	startTime time.Time
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu sync.Mutex
	// crash-loop sliding window: nodeID → timestamps of recent xray.down events
	crashWindows map[uint][]time.Time
	// sustained threshold trackers: (ruleID, entityKey) → first-over timestamp
	firstOverAt map[string]time.Time
	// cache of latest stats per node (populated from EventNodeStatsUpdated)
	latestStats map[uint]events.NodeStatsPayload
	// cache of node online state (populated from online/offline events)
	nodeOnline map[uint]bool
	// nodeID → when the current outage began (the writer's OutageStartedAt),
	// so a rule's DurationSec grace measures from the real transition
	offlineSinceAt map[uint]time.Time
	// nodeID → the payload of the outage still in progress, re-evaluated on
	// the tick because no further event arrives while a node stays down
	latestOffline map[uint]events.NodeStatusPayload
	// cache of last known node_name/ip for richer alert messages
	nodeMeta map[uint]nodeMetaEntry
	// MTProto counting rules: "type:nodeID" → occurrences, oldest first
	mtprotoWindows map[string][]mtprotoHit
	// MTProto counting rules we fired: "ruleID:entity" → state the tick resolves from
	mtprotoFiring map[string]mtprotoFire
	// enabled rules, cached briefly for the MTProto node events that come in floods
	ruleCache   []*domain.Rule
	ruleCacheAt time.Time
}

type nodeMetaEntry struct {
	Name string
	IP   string
}

// NewEngine wires an engine to the bus + repo. Call Start to begin.
func NewEngine(bus *events.EventBus, repo repository.AlertRepository) *Engine {
	return &Engine{
		bus:            bus,
		repo:           repo,
		crashWindows:   make(map[uint][]time.Time),
		firstOverAt:    make(map[string]time.Time),
		latestStats:    make(map[uint]events.NodeStatsPayload),
		nodeOnline:     make(map[uint]bool),
		offlineSinceAt: make(map[uint]time.Time),
		latestOffline:  make(map[uint]events.NodeStatusPayload),
		nodeMeta:       make(map[uint]nodeMetaEntry),
		mtprotoWindows: make(map[string][]mtprotoHit),
		mtprotoFiring:  make(map[string]mtprotoFire),
	}
}

// Start begins subscribing and ticking. Re-hydrates firing state so a
// crashed master doesn't lose "this is already firing" knowledge.
func (e *Engine) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	e.startTime = time.Now()

	// Rehydrate firing state. No re-fire — resolves only.
	if firing, err := e.repo.ListFiringStates(ctx); err == nil {
		e.rehydrateMTProto(ctx, firing)
		logger.GetLogger().Infof("AlertEngine: rehydrated %d firing state rows", len(firing))
	}

	sub := e.bus.Subscribe(subscriberID)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.run(ctx, sub)
	}()

	logger.GetLogger().Info("AlertEngine started")
}

// Stop unsubscribes and waits for the run goroutine to exit.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.bus.Unsubscribe(subscriberID)
	e.wg.Wait()
	logger.GetLogger().Info("AlertEngine stopped")
}

func (e *Engine) run(ctx context.Context, sub events.Subscriber) {
	ticker := time.NewTicker(evalTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			e.handleEvent(ctx, ev)
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

// inGracePeriod returns true while the post-start window is still open.
// Rules use this to decide whether to fire; it never blocks resolves.
func (e *Engine) inGracePeriod() bool {
	return time.Since(e.startTime) < gracePeriodAfterStart
}

// handleEvent dispatches a single bus event to the matching evaluators.
// Each branch is intentionally side-by-side to make "what fires on X" easy
// to audit by scanning this file.
func (e *Engine) handleEvent(ctx context.Context, ev events.Event) {
	switch ev.Type {
	case events.EventNodeOffline:
		if p, ok := ev.Payload.(events.NodeStatusPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, p.IP)
			e.setOnline(p.NodeID, false)
			e.evalNodeOffline(ctx, p)
		}
	case events.EventNodeOnline:
		if p, ok := ev.Payload.(events.NodeStatusPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, p.IP)
			e.setOnline(p.NodeID, true)
			e.resolveNodeOffline(ctx, p)
		}
	case events.EventXrayDown, events.EventXrayCrashLoop:
		if p, ok := ev.Payload.(events.XrayStatusPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, p.IP)
			e.recordCrash(p.NodeID)
			e.evalCrashLoop(ctx, p)
		}
	case events.EventXrayUp:
		if p, ok := ev.Payload.(events.XrayStatusPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, p.IP)
			e.resolveCrashLoop(ctx, p)
		}
	case events.EventMTProtoTelemtCrashLoop, events.EventMTProtoTelemtUp,
		events.EventMTProtoViolation, events.EventMTProtoEgressFailed,
		events.EventMTProtoDisguiseDown, events.EventMTProtoDisguiseUp:
		if p, ok := ev.Payload.(events.MTProtoPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, "")
			e.handleMTProto(ctx, ev.Type, p)
		}
	case events.EventNodeDeleted, events.EventNodeUpdated:
		// A node that is gone or taken out of service on purpose is not an
		// outage: stop re-alerting on the tick.
		if p, ok := ev.Payload.(events.NodeLifecyclePayload); ok && (ev.Type == events.EventNodeDeleted || p.Planned) {
			e.forgetOfflineSince(p.NodeID)
		}
	case events.EventNodeStatsUpdated:
		if p, ok := ev.Payload.(events.NodeStatsPayload); ok {
			e.rememberMeta(p.NodeID, p.NodeName, "")
			e.cacheStats(p)
			e.evalStatsThresholds(ctx, p)
		}
	}
}

// tick re-evaluates sustained-threshold rules that might resolve (or
// deepen) without a new bus event arriving.
func (e *Engine) tick(ctx context.Context) {
	rules, err := e.repo.ListEnabledRules(ctx)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("AlertEngine: list rules failed")
		return
	}
	// Re-check CPU/Disk for any cached node.
	e.mu.Lock()
	stats := make([]events.NodeStatsPayload, 0, len(e.latestStats))
	for _, s := range e.latestStats {
		stats = append(stats, s)
	}
	e.mu.Unlock()

	// Nodes still down: their grace period may have elapsed since the offline
	// event arrived, and no further event will arrive while they stay down.
	e.mu.Lock()
	pending := make([]events.NodeStatusPayload, 0, len(e.latestOffline))
	for _, p := range e.latestOffline {
		pending = append(pending, p)
	}
	e.mu.Unlock()

	for _, rule := range rules {
		switch rule.RuleType {
		case domain.RuleTypeHighCPU, domain.RuleTypeHighDisk:
			for _, s := range stats {
				e.evalStatsForRule(ctx, rule, s)
			}
		case domain.RuleTypeNodeOffline:
			for _, p := range pending {
				e.evalNodeOfflineForRule(ctx, rule, p)
			}
		case domain.RuleTypeMTProtoAllowlistViolation, domain.RuleTypeMTProtoEgressDown:
			e.resolveQuietMTProto(ctx, rule)
		}
	}
}

// ----- helpers -----

func (e *Engine) rememberMeta(nodeID uint, name, ip string) {
	if nodeID == 0 {
		return
	}
	e.mu.Lock()
	m := e.nodeMeta[nodeID]
	if name != "" {
		m.Name = name
	}
	if ip != "" {
		m.IP = ip
	}
	e.nodeMeta[nodeID] = m
	e.mu.Unlock()
}

// rememberOfflineSince records when a node's current outage began, preferring
// the writer's OutageStartedAt (the real transition time) over arrival time.
// Kept so the tick can re-evaluate a still-down node once its rule's grace
// period elapses, the same way sustained CPU/disk rules work.
func (e *Engine) rememberOfflineSince(p events.NodeStatusPayload) {
	if p.NodeID == 0 {
		return
	}
	at := time.Now()
	if p.OutageStartedAt != nil && !p.OutageStartedAt.IsZero() {
		at = *p.OutageStartedAt
	}
	e.mu.Lock()
	if _, exists := e.offlineSinceAt[p.NodeID]; !exists {
		e.offlineSinceAt[p.NodeID] = at
	}
	e.latestOffline[p.NodeID] = p
	e.mu.Unlock()
}

func (e *Engine) offlineSince(nodeID uint) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.offlineSinceAt[nodeID]
}

func (e *Engine) forgetOfflineSince(nodeID uint) {
	e.mu.Lock()
	delete(e.offlineSinceAt, nodeID)
	delete(e.latestOffline, nodeID)
	e.mu.Unlock()
}

func (e *Engine) setOnline(nodeID uint, online bool) {
	e.mu.Lock()
	e.nodeOnline[nodeID] = online
	e.mu.Unlock()
}

func (e *Engine) cacheStats(p events.NodeStatsPayload) {
	e.mu.Lock()
	e.latestStats[p.NodeID] = p
	e.mu.Unlock()
}

func nodeEntityKey(nodeID uint) string {
	return fmt.Sprintf("node:%d", nodeID)
}

// ruleAppliesToNode honours the scope config.
func (e *Engine) ruleAppliesToNode(rule *domain.Rule, nodeID uint) bool {
	switch rule.Scope {
	case domain.ScopeGlobal, "":
		return true
	case domain.ScopeNodeIDs:
		for _, id := range rule.ScopedNodeIDs() {
			if id == nodeID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// rulesOfType returns all enabled rules with the given type. Caller
// filters further by scope. Errors are logged and swallowed — alerting
// should never break a normal control-plane code path.
func (e *Engine) rulesOfType(ctx context.Context, t domain.RuleType) []*domain.Rule {
	all, err := e.repo.ListEnabledRules(ctx)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("AlertEngine: list enabled rules failed")
		return nil
	}
	out := make([]*domain.Rule, 0)
	for _, r := range all {
		if r.RuleType == t {
			out = append(out, r)
		}
	}
	return out
}

// fireOrSkip is the shared fire+cooldown gate. Returns true if an event
// was actually dispatched (used for test-send feedback).
func (e *Engine) fireOrSkip(ctx context.Context, rule *domain.Rule, entityKey, title, message string, valueJSON string) bool {
	if e.inGracePeriod() && rule.RuleType == domain.RuleTypeNodeOffline {
		// Only suppress node_offline during grace. Other rules are less
		// vulnerable to the restart-storm scenario.
		return false
	}

	now := time.Now()
	state, _ := e.repo.GetState(ctx, rule.ID, entityKey)
	if state != nil && state.Firing && state.LastNotifiedAt != nil {
		if now.Sub(*state.LastNotifiedAt) < time.Duration(rule.CooldownSec)*time.Second {
			return false
		}
	}

	// Insert audit event.
	_ = e.repo.InsertEvent(ctx, &domain.Event{
		RuleID:    rule.ID,
		EntityKey: entityKey,
		Status:    domain.EventStatusFired,
		Title:     title,
		Message:   message,
		ValueJSON: valueJSON,
		CreatedAt: now,
	})

	// Upsert state (firing=true, last_notified_at=now).
	firstAt := now
	if state != nil && state.FirstTriggeredAt != nil {
		firstAt = *state.FirstTriggeredAt
	}
	_ = e.repo.UpsertState(ctx, &domain.State{
		RuleID:           rule.ID,
		EntityKey:        entityKey,
		Firing:           true,
		FirstTriggeredAt: &firstAt,
		LastNotifiedAt:   &now,
		LastSeenValue:    valueJSON,
		UpdatedAt:        now,
	})

	// best-effort; only the one column, since rule may be a cached copy
	_ = e.repo.MarkRuleFired(ctx, rule.ID, now)

	// Publish SystemAlert so the existing notification dispatcher routes
	// to Telegram/webhook per the user's settings.
	e.bus.Publish(events.Event{
		Type: events.EventSystemAlert,
		Payload: events.SystemAlertPayload{
			Level:   levelFor(rule.RuleType),
			Title:   title,
			Message: message,
		},
	})
	return true
}

// resolveIfFiring clears state and publishes a recovery notice. Silent
// no-op when the rule isn't currently firing for this entity.
func (e *Engine) resolveIfFiring(ctx context.Context, rule *domain.Rule, entityKey, title, message string) {
	state, err := e.repo.GetState(ctx, rule.ID, entityKey)
	if err != nil || state == nil || !state.Firing {
		return
	}

	now := time.Now()
	_ = e.repo.InsertEvent(ctx, &domain.Event{
		RuleID:    rule.ID,
		EntityKey: entityKey,
		Status:    domain.EventStatusResolved,
		Title:     title,
		Message:   message,
		CreatedAt: now,
	})

	_ = e.repo.UpsertState(ctx, &domain.State{
		RuleID:           rule.ID,
		EntityKey:        entityKey,
		Firing:           false,
		FirstTriggeredAt: state.FirstTriggeredAt,
		LastNotifiedAt:   &now,
		UpdatedAt:        now,
	})

	e.bus.Publish(events.Event{
		Type: events.EventSystemAlert,
		Payload: events.SystemAlertPayload{
			Level:   "info",
			Title:   title,
			Message: message,
		},
	})
}

// levelFor maps rule types to notification severity — tuned so Telegram
// channel enabledness by-level stays intuitive.
func levelFor(t domain.RuleType) string {
	switch t {
	case domain.RuleTypeNodeOffline, domain.RuleTypeNodeCrashLoop:
		return "error"
	case domain.RuleTypeMTProtoEgressDown, domain.RuleTypeMTProtoDisguiseDown:
		return "error"
	case domain.RuleTypeHighCPU, domain.RuleTypeHighDisk, domain.RuleTypeMTProtoAllowlistViolation:
		return "warning"
	}
	return "info"
}

// ----- Test fire (exposed via usecase) -----

// TestFire publishes a fake alert for the given rule so an operator can
// verify delivery without waiting for a real trigger. Skips DB side-
// effects so it doesn't pollute the audit log.
func (e *Engine) TestFire(rule *domain.Rule) {
	title := fmt.Sprintf("[Test] %s", rule.Name)
	msg := fmt.Sprintf("This is a test alert for rule '%s' (type %s). If you received this, delivery is working.", rule.Name, rule.RuleType)
	e.bus.Publish(events.Event{
		Type: events.EventSystemAlert,
		Payload: events.SystemAlertPayload{
			Level:   "info",
			Title:   title,
			Message: msg,
		},
	})
}

// ----- serialization helper -----

func valueAsJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// uintToKey avoids allocating fmt.Sprintf on hot paths. Shared util.
func uintToKey(id uint) string { return strconv.FormatUint(uint64(id), 10) }
