package usecase

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// HeartbeatManager: persistent bidi streams to agent nodes (replaces
// polling). Pongs carry NodeStatus + ConfigHash for drift detection.
type HeartbeatManager struct {
	usecase *nodeUsecase

	mu       sync.Mutex
	sessions map[uint]*heartbeatSession // nodeID -> active session

	pingInterval       time.Duration
	missedPongMax      int           // consecutive missed pongs before marking offline
	baseReconnectDelay time.Duration // base delay before reconnecting after failure
	maxReconnectDelay  time.Duration // maximum delay between reconnect attempts

	stopChan chan struct{}
	syncNow  chan struct{} // signals immediate session sync
	wg       sync.WaitGroup
	stopOnce sync.Once // guards Stop against duplicate callers (scheduler+root both invoke it during shutdown)
}

// heartbeatSession tracks the state of a single node's heartbeat stream.
type heartbeatSession struct {
	nodeID uint
	cancel context.CancelFunc

	mu                  sync.RWMutex
	lastPong            time.Time
	lastRTT             int64
	configHash          string
	sequence            int64
	consecutiveFailures int
	uuidMismatch        bool   // true if agent UUID doesn't match node UUID
	agentReportedUUID   string // last UUID reported by agent

	// Xray crash tracking
	lastXrayRunning         *bool     // nil = unknown (first pong)
	crashCount              int       // crashes since last stability reset
	lastCrashTime           time.Time // when the most recent crash was detected
	crashLoopNotified       bool      // true after crash loop summary sent
	lastCrashLoopNotifyTime time.Time // when the last crash loop notification was sent
	lastStableTime          time.Time // last time xray was seen running (for stability reset)

	// Crash recovery command tracking
	recoveryCommandCount    int       // times recovery command has been executed (accumulates across crash cycles)
	lastRecoveryCommandTime time.Time // when the recovery command last ran
	recoveryExhausted       bool      // true when max_attempts reached
	pendingRecovery         bool      // true when recovery was skipped due to cooldown; retried on next pong after cooldown expires

	settingsCache     *xrayMonitorSettings
	settingsCacheTime time.Time
}

// xrayMonitorSettings caches xray monitoring settings with a TTL.
type xrayMonitorSettings struct {
	crashLoopThreshold  int
	crashLoopCooldown   time.Duration
	autoDisableEnabled  bool
	autoDisableMaxFails int
	stabilityPeriod     time.Duration
}

// NewHeartbeatManager creates a new heartbeat manager.
func NewHeartbeatManager(uc *nodeUsecase) *HeartbeatManager {
	return &HeartbeatManager{
		usecase:            uc,
		sessions:           make(map[uint]*heartbeatSession),
		pingInterval:       2 * time.Second,
		missedPongMax:      3,
		baseReconnectDelay: 5 * time.Second,
		maxReconnectDelay:  2 * time.Minute,
		stopChan:           make(chan struct{}),
		syncNow:            make(chan struct{}, 1),
	}
}

// Start begins heartbeat monitoring for all active agent nodes.
// It also watches for new/removed nodes periodically.
func (hm *HeartbeatManager) Start(ctx context.Context) {
	log := logger.GetLogger()
	log.Info("[HeartbeatManager] Starting")

	// Initial sync
	hm.syncSessions(ctx)

	// Periodically sync sessions (detect new/removed nodes)
	hm.wg.Add(1)
	go func() {
		defer hm.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				hm.syncSessions(ctx)
			case <-hm.syncNow:
				hm.syncSessions(ctx)
			case <-hm.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop gracefully shuts down all heartbeat sessions. Idempotent: both
// runServe's shutdown sequence and Scheduler.Stop reach here, and the
// second call used to panic with "close of closed channel" on stopChan.
func (hm *HeartbeatManager) Stop() {
	hm.stopOnce.Do(func() {
		logger.GetLogger().Info("[HeartbeatManager] Stopping")
		close(hm.stopChan)

		hm.mu.Lock()
		for _, s := range hm.sessions {
			s.cancel()
		}
		hm.sessions = make(map[uint]*heartbeatSession)
		hm.mu.Unlock()

		// Wait for sessions with a timeout to avoid blocking shutdown indefinitely
		done := make(chan struct{})
		go func() {
			hm.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// All sessions exited cleanly
		case <-time.After(5 * time.Second):
			logger.GetLogger().Warn("[HeartbeatManager] Timed out waiting for sessions to stop")
		}
		logger.GetLogger().Info("[HeartbeatManager] Stopped")
	})
}

// reconnectBackoff calculates an exponential backoff delay with jitter for reconnects.
func (hm *HeartbeatManager) reconnectBackoff(failures int) time.Duration {
	exp := min(failures, 5)
	delay := hm.baseReconnectDelay * time.Duration(1<<exp)
	if delay > hm.maxReconnectDelay {
		delay = hm.maxReconnectDelay
	}
	jitter := time.Duration(rand.Int63n(int64(delay / 2)))
	return delay/2 + jitter
}

// GetSessionInfo returns the last known RTT and config hash for a node.
func (hm *HeartbeatManager) GetSessionInfo(nodeID uint) (lastRTT int64, configHash string, ok bool) {
	hm.mu.Lock()
	s, exists := hm.sessions[nodeID]
	hm.mu.Unlock()

	if !exists {
		return 0, "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRTT, s.configHash, true
}

// StopNode cancels and removes the heartbeat session for a specific node.
// It is idempotent — calling it for a node without an active session is a no-op.
func (hm *HeartbeatManager) StopNode(nodeID uint) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	s, exists := hm.sessions[nodeID]
	if !exists {
		return
	}

	logger.GetLogger().WithField("node_id", nodeID).Info("[HeartbeatManager] Stopping session for deleted node")
	s.cancel()
	delete(hm.sessions, nodeID)
}

// syncSessions ensures there's a heartbeat session for every active agent node.
func (hm *HeartbeatManager) syncSessions(ctx context.Context) {
	log := logger.GetLogger()

	nodes, err := hm.usecase.nodeRepo.ListActiveNodes(ctx)
	if err != nil {
		log.WithError(err).Error("[HeartbeatManager] Failed to list active nodes")
		return
	}

	// Build set of active agent node IDs
	activeIDs := make(map[uint]bool)
	for _, node := range nodes {
		activeIDs[node.ID] = true
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Start sessions for new nodes
	for _, node := range nodes {
		if _, exists := hm.sessions[node.ID]; !exists {
			hm.startSession(ctx, node)
		}
	}

	// Stop sessions for removed/inactive nodes
	for id, s := range hm.sessions {
		if !activeIDs[id] {
			log.WithField("node_id", id).Info("[HeartbeatManager] Removing session for inactive node")
			s.cancel()
			delete(hm.sessions, id)
		}
	}
}

// startSession launches a heartbeat goroutine for a node. Must be called with hm.mu held.
func (hm *HeartbeatManager) startSession(ctx context.Context, node *domain.Node) {
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &heartbeatSession{
		nodeID: node.ID,
		cancel: cancel,
	}
	hm.sessions[node.ID] = s

	hm.wg.Add(1)
	go hm.runSession(sessionCtx, node, s)
}

// runSession maintains a persistent heartbeat stream for a single node,
// reconnecting on failure with backoff.
func (hm *HeartbeatManager) runSession(ctx context.Context, node *domain.Node, s *heartbeatSession) {
	defer hm.wg.Done()
	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)

	// Spread initial connections across the fleet: if the master is just
	// coming up, every session would otherwise dial simultaneously. The
	// reconnect backoff already jitters retries; this covers the cold start.
	if hm.baseReconnectDelay > 0 {
		jitter := time.Duration(rand.Int63n(int64(hm.baseReconnectDelay)))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		default:
		}

		err := hm.runHeartbeatLoop(ctx, node, s)
		if err != nil {
			log.WithError(err).Debug("[HeartbeatManager] Heartbeat stream ended")
			s.consecutiveFailures++
		}

		// Mark node offline on stream failure
		if updateErr := hm.usecase.nodeRepo.UpdateNodeStatus(ctx, node.ID, false, time.Now()); updateErr != nil {
			log.WithError(updateErr).Error("[HeartbeatManager] Failed to update offline status")
		}

		// Wait before reconnecting with exponential backoff + jitter
		backoff := hm.reconnectBackoff(s.consecutiveFailures)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		}
	}
}

// runHeartbeatLoop establishes a stream and runs the ping/pong loop.
// Returns when the stream breaks or the context is cancelled.
func (hm *HeartbeatManager) runHeartbeatLoop(ctx context.Context, node *domain.Node, s *heartbeatSession) error {
	log := logger.GetLogger().WithField("node_id", node.ID)

	// Use an UNPOOLED client for this long-lived bidi stream. The shared
	// pool TTL-evicts cached conns every few minutes; if heartbeat rode
	// that conn, any other RPC crossing the TTL boundary would Close()
	// it and kill the stream, producing a periodic online/offline flap.
	client, err := hm.usecase.getAgentClientUnpooled(node)
	if err != nil {
		return err
	}
	defer client.Close()

	stream, err := client.Heartbeat(ctx)
	if err != nil {
		return err
	}

	// Detect offline→online transition
	wasOffline := !node.IsOnline

	// Mark node online
	if updateErr := hm.usecase.nodeRepo.UpdateNodeStatus(ctx, node.ID, true, time.Now()); updateErr != nil {
		log.WithError(updateErr).Warn("[HeartbeatManager] Failed to update online status")
	}
	// Update local state
	node.IsOnline = true

	// Reset failure counter on successful connection
	s.consecutiveFailures = 0

	// If node was offline, trigger config re-push
	if wasOffline {
		log.Info("[HeartbeatManager] Node reconnected, scheduling config push")
		hm.triggerConfigPush(node)
	}

	// Start pong receiver in background
	pongChan := make(chan *pb.HeartbeatPong, 4)
	errChan := make(chan error, 1)
	go func() {
		for {
			pong, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}
			select {
			case pongChan <- pong:
			default:
				// Drop if channel full (receiver is slow)
			}
		}
	}()

	missedPongs := 0
	ticker := time.NewTicker(hm.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hm.stopChan:
			return nil
		case err := <-errChan:
			return err

		case <-ticker.C:
			s.mu.Lock()
			s.sequence++
			seq := s.sequence
			s.mu.Unlock()

			ping := &pb.HeartbeatPing{
				Timestamp: time.Now().UnixMilli(),
				Sequence:  seq,
			}
			if err := stream.Send(ping); err != nil {
				return err
			}

			// Drain stale pongs accumulated while we were busy
		drain:
			for {
				select {
				case <-pongChan:
				default:
					break drain
				}
			}

			// Wait for pong with timeout
			select {
			case pong := <-pongChan:
				missedPongs = 0
				hm.handlePong(ctx, node, s, pong)
			case err := <-errChan:
				return err
			case <-ctx.Done():
				return ctx.Err()
			case <-hm.stopChan:
				return nil
			case <-time.After(hm.pingInterval):
				missedPongs++
				if missedPongs >= hm.missedPongMax {
					log.Warnf("[HeartbeatManager] %d consecutive missed pongs, reconnecting", missedPongs)
					return fmt.Errorf("missed %d consecutive pongs", missedPongs)
				}
			}
		}
	}
}

// handlePong processes a heartbeat pong response.
func (hm *HeartbeatManager) handlePong(ctx context.Context, node *domain.Node, s *heartbeatSession, pong *pb.HeartbeatPong) {
	// UUID identity validation — check before updating lastPong so that
	// a mismatched agent doesn't keep this node session alive.
	if pong.NodeUuid != "" && node.UUID != "" && pong.NodeUuid != node.UUID {
		s.mu.Lock()
		if !s.uuidMismatch {
			logger.GetLogger().WithFields(map[string]interface{}{
				"node_id":       node.ID,
				"expected_uuid": node.UUID,
				"agent_uuid":    pong.NodeUuid,
			}).Warn("[HeartbeatManager] Node identity mismatch — agent UUID doesn't match. Redeploy the agent.")
		}
		s.uuidMismatch = true
		s.agentReportedUUID = pong.NodeUuid
		s.mu.Unlock()
		return // Don't process further — this agent isn't ours
	}

	s.mu.Lock()
	// Clear mismatch if UUID now matches (after redeploy)
	if s.uuidMismatch {
		s.uuidMismatch = false
		s.agentReportedUUID = ""
	}
	s.lastPong = time.Now()
	s.lastRTT = pong.RttMs
	s.mu.Unlock()

	if pong.Status == nil {
		return
	}

	// --- Xray status tracking (before config hash early returns) ---
	hm.handleXrayStatus(ctx, node, s, pong.Status.XrayRunning)

	// --- Pending recovery retry: if recovery was deferred due to cooldown, retry once cooldown expires ---
	if s.pendingRecovery && !s.lastRecoveryCommandTime.IsZero() {
		settings := node.GetCrashRecoverySettingsOrDefault()
		cooldown := time.Duration(settings.Cooldown) * time.Minute
		if time.Since(s.lastRecoveryCommandTime) >= cooldown {
			s.pendingRecovery = false
			if freshNode, err := hm.usecase.nodeRepo.GetNode(ctx, node.ID); err == nil && freshNode.XrayStopped {
				hm.attemptCrashRecovery(ctx, freshNode, s)
			}
		}
	}

	// Config drift detection from heartbeat status
	newHash := pong.Status.ConfigHash
	if newHash == "" {
		return
	}

	s.mu.Lock()
	oldHash := s.configHash
	s.configHash = newHash
	s.mu.Unlock()

	// Skip on first pong (we just learned the hash)
	if oldHash == "" {
		return
	}

	// Check against last pushed config hash
	hm.usecase.configHashMu.RLock()
	expectedHash := hm.usecase.lastPushedConfigHash[node.ID]
	hm.usecase.configHashMu.RUnlock()

	if expectedHash != "" && expectedHash != newHash {
		logger.GetLogger().WithFields(map[string]interface{}{
			"node_id":       node.ID,
			"agent_hash":    newHash,
			"expected_hash": expectedHash,
		}).Warn("[HeartbeatManager] Config drift detected via heartbeat")
		hm.triggerConfigPush(node)
	}
}

// triggerConfigPush schedules a rate-limited async config push to a node.
func (hm *HeartbeatManager) triggerConfigPush(node *domain.Node) {
	hm.usecase.tryScheduleConfigPush(node)
}

// getXrayMonitorSettings returns cached settings, refreshing from DB if stale (>1 min TTL).
func (hm *HeartbeatManager) getXrayMonitorSettings(ctx context.Context, s *heartbeatSession) *xrayMonitorSettings {
	if s.settingsCache != nil && time.Since(s.settingsCacheTime) < time.Minute {
		return s.settingsCache
	}

	// Defaults
	settings := &xrayMonitorSettings{
		crashLoopThreshold:  3,
		crashLoopCooldown:   5 * time.Minute,
		autoDisableEnabled:  false,
		autoDisableMaxFails: 10,
		stabilityPeriod:     5 * time.Minute,
	}

	if hm.usecase.settingUC == nil {
		s.settingsCache = settings
		s.settingsCacheTime = time.Now()
		return settings
	}

	if val, err := hm.usecase.settingUC.GetByKey(ctx, "xray_crash_loop_threshold"); err == nil {
		if v, e := strconv.Atoi(val); e == nil && v > 0 {
			settings.crashLoopThreshold = v
		}
	}
	if val, err := hm.usecase.settingUC.GetByKey(ctx, "xray_crash_loop_cooldown"); err == nil {
		if v, e := strconv.Atoi(val); e == nil && v > 0 {
			settings.crashLoopCooldown = time.Duration(v) * time.Minute
		}
	}
	if val, err := hm.usecase.settingUC.GetByKey(ctx, "xray_auto_disable_enabled"); err == nil {
		settings.autoDisableEnabled = val == "true"
	}
	if val, err := hm.usecase.settingUC.GetByKey(ctx, "xray_auto_disable_max_failures"); err == nil {
		if v, e := strconv.Atoi(val); e == nil && v > 0 {
			settings.autoDisableMaxFails = v
		}
	}
	if val, err := hm.usecase.settingUC.GetByKey(ctx, "xray_stability_period"); err == nil {
		if v, e := strconv.Atoi(val); e == nil && v > 0 {
			settings.stabilityPeriod = time.Duration(v) * time.Minute
		}
	}

	s.settingsCache = settings
	s.settingsCacheTime = time.Now()
	return settings
}

// handleXrayStatus tracks xray running state transitions and publishes events.
func (hm *HeartbeatManager) handleXrayStatus(ctx context.Context, node *domain.Node, s *heartbeatSession, xrayRunning bool) {
	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)

	// First pong: record state, no transition detection
	if s.lastXrayRunning == nil {
		s.lastXrayRunning = &xrayRunning
		if xrayRunning {
			s.lastStableTime = time.Now()
		}
		return
	}

	// Stability reset: if xray is running long enough, reset crash count
	if xrayRunning && s.crashCount > 0 && !s.lastStableTime.IsZero() {
		settings := hm.getXrayMonitorSettings(ctx, s)
		if time.Since(s.lastStableTime) >= settings.stabilityPeriod {
			log.WithField("crash_count", s.crashCount).Info("[HeartbeatManager] Xray stable, resetting crash counter")
			s.crashCount = 0
			s.recoveryCommandCount = 0
			s.recoveryExhausted = false
			s.pendingRecovery = false

			// Clear persisted recovery state — node is healthy again.
			// Uses the heartbeat loop's node pointer (not a fresh fetch) since we're
			// only nulling one JSONB field; acceptable trade-off to avoid an extra DB read.
			if node.LastCrashRecovery != nil {
				node.LastCrashRecovery = nil
				if updateErr := hm.usecase.nodeRepo.UpdateNode(ctx, node); updateErr != nil {
					log.WithError(updateErr).Error("[HeartbeatManager] Failed to clear last crash recovery")
				}
			}
		}
	}

	prev := *s.lastXrayRunning
	s.lastXrayRunning = &xrayRunning

	// No transition
	if prev == xrayRunning {
		return
	}

	if prev && !xrayRunning {
		// running → stopped: potential crash
		hm.handleXrayCrash(ctx, node, s)
	} else {
		// stopped → running: recovery
		hm.handleXrayRecovery(ctx, node, s)
	}
}

// handleXrayCrash handles the running→stopped transition.
func (hm *HeartbeatManager) handleXrayCrash(ctx context.Context, node *domain.Node, s *heartbeatSession) {
	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)

	// Re-read XrayStopped from DB to detect manual stops (the session's node pointer is stale)
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	freshNode, err := hm.usecase.nodeRepo.GetNode(dbCtx, node.ID)
	if err != nil {
		log.WithError(err).Warn("[HeartbeatManager] Failed to read node from DB during xray crash detection, suppressing notification")
		return
	}
	if freshNode.XrayStopped {
		log.Debug("[HeartbeatManager] Xray stopped manually, not a crash")
		return
	}

	// It's a crash
	s.crashCount++
	s.lastCrashTime = time.Now()
	settings := hm.getXrayMonitorSettings(ctx, s)

	log.WithField("crash_count", s.crashCount).Warn("[HeartbeatManager] Xray process crashed")

	// Auto-disable check — always bypasses crashLoopNotified suppression
	if settings.autoDisableEnabled && s.crashCount >= settings.autoDisableMaxFails {
		log.WithField("crash_count", s.crashCount).Warn("[HeartbeatManager] Auto-disabling xray after too many failures")
		freshNode.XrayStopped = true
		if updateErr := hm.usecase.nodeRepo.UpdateNode(ctx, freshNode); updateErr != nil {
			log.WithError(updateErr).Error("[HeartbeatManager] Failed to auto-disable xray")
		}
		// Attempt crash recovery command before giving up
		if hm.attemptCrashRecovery(ctx, freshNode, s) {
			return
		}

		hm.publishXrayEvent(events.EventXrayCrashLoop, node, s.crashCount, fmt.Sprintf("Xray auto-disabled after %d consecutive failures. Manual intervention required.", s.crashCount))
		return
	}

	// Adaptive notification
	if s.crashCount <= settings.crashLoopThreshold {
		// Individual crash notification
		hm.publishXrayEvent(events.EventXrayDown, node, s.crashCount, "")
	} else if !s.crashLoopNotified {
		// First crash after threshold: send crash loop summary
		s.crashLoopNotified = true
		s.lastCrashLoopNotifyTime = time.Now()
		hm.publishXrayEvent(events.EventXrayCrashLoop, node, s.crashCount, "")
	} else {
		// Check cooldown: re-alert if cooldown has expired since last crash loop notification
		if !s.lastCrashLoopNotifyTime.IsZero() && time.Since(s.lastCrashLoopNotifyTime) >= settings.crashLoopCooldown {
			// Cooldown expired — send a new crash loop notification now
			s.lastCrashLoopNotifyTime = time.Now()
			hm.publishXrayEvent(events.EventXrayCrashLoop, node, s.crashCount, "")
		}
		// Otherwise: suppress
	}

	// Fetch error logs in background (fire and forget, best-effort logging only)
	crashCount := s.crashCount // capture before goroutine
	go hm.fetchAndLogErrors(node, crashCount)
}

// handleXrayRecovery handles the stopped→running transition.
func (hm *HeartbeatManager) handleXrayRecovery(ctx context.Context, node *domain.Node, s *heartbeatSession) {
	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)
	log.WithField("crash_count", s.crashCount).Info("[HeartbeatManager] Xray process recovered")

	if s.crashCount > 0 {
		hm.publishXrayEvent(events.EventXrayUp, node, s.crashCount, "")
	}
	s.crashLoopNotified = false
	s.pendingRecovery = false // xray is back; no need to retry recovery
	s.lastStableTime = time.Now()
	// Note: crashCount is NOT reset here — only after sustained stability
}

// publishXrayEvent publishes an xray status event to the event bus.
func (hm *HeartbeatManager) publishXrayEvent(eventType events.EventType, node *domain.Node, crashCount int, message string) {
	if hm.usecase.eventBus == nil {
		return
	}
	hm.usecase.eventBus.Publish(events.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload: events.XrayStatusPayload{
			NodeID:     node.ID,
			NodeName:   node.Name,
			IP:         node.IP,
			CrashCount: crashCount,
			Message:    message,
		},
	})
}

// recoveryResult holds the outcome of a recovery command execution.
type recoveryResult struct {
	exitCode int
	stdout   string
	stderr   string
	success  bool
	err      error // non-nil if the RPC itself failed
}

// publishRecoveryEvent publishes a crash recovery event to the event bus.
func (hm *HeartbeatManager) publishRecoveryEvent(eventType events.EventType, node *domain.Node, s *heartbeatSession, settings *domain.CrashRecoverySettings, result *recoveryResult) {
	if hm.usecase.eventBus == nil {
		return
	}
	payload := events.XrayRecoveryPayload{
		NodeID:      node.ID,
		NodeName:    node.Name,
		IP:          node.IP,
		CrashCount:  s.crashCount,
		AttemptNum:  s.recoveryCommandCount,
		MaxAttempts: settings.MaxAttempts,
		Command:     settings.Command,
	}
	if result != nil {
		payload.ExitCode = result.exitCode
		payload.Stdout = result.stdout
		payload.Stderr = result.stderr
		payload.Success = result.success
		if result.err != nil {
			payload.ErrorMessage = result.err.Error()
		}
	}
	hm.usecase.eventBus.Publish(events.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}

// attemptCrashRecovery checks if a crash recovery command is configured and eligible,
// executes it, and starts xray. Returns true if recovery was attempted (or skipped due
// to cooldown/exhaustion), meaning the caller should not send the default notification.
func (hm *HeartbeatManager) attemptCrashRecovery(ctx context.Context, node *domain.Node, s *heartbeatSession) bool {
	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)

	settings := node.GetCrashRecoverySettingsOrDefault()
	if !settings.Enabled || settings.Command == "" {
		return false
	}

	// Check if exhausted from a previous cycle
	if s.recoveryExhausted {
		return false
	}

	// Check max attempts
	if settings.MaxAttempts > 0 && s.recoveryCommandCount >= settings.MaxAttempts {
		s.recoveryExhausted = true
		hm.publishRecoveryEvent(events.EventXrayRecoveryExhausted, node, s, settings, nil)

		// Persist exhaustion for panel display
		node.LastCrashRecovery = &domain.LastCrashRecovery{
			Timestamp:   time.Now(),
			AttemptNum:  s.recoveryCommandCount,
			MaxAttempts: settings.MaxAttempts,
			Exhausted:   true,
		}
		if updateErr := hm.usecase.nodeRepo.UpdateNode(ctx, node); updateErr != nil {
			logger.GetLogger().WithError(updateErr).Error("[HeartbeatManager] Failed to persist recovery exhaustion")
		}
		return true
	}

	// Check cooldown
	if !s.lastRecoveryCommandTime.IsZero() {
		cooldown := time.Duration(settings.Cooldown) * time.Minute
		if time.Since(s.lastRecoveryCommandTime) < cooldown {
			log.Info("[HeartbeatManager] Recovery command on cooldown, will retry after cooldown expires")
			s.pendingRecovery = true
			hm.publishXrayEvent(events.EventXrayCrashLoop, node, s.crashCount,
				fmt.Sprintf("Xray auto-disabled after %d failures. Recovery command on cooldown.", s.crashCount))
			return true
		}
	}

	// Execute recovery synchronously — blocks heartbeat for this node only.
	// This is acceptable because xray is already stopped (auto-disabled).
	hm.executeRecoveryCommand(ctx, node.ID, s, settings)
	return true
}

// executeRecoveryCommand executes the configured recovery command on the node,
// publishes a notification with the result, then starts xray one more time.
func (hm *HeartbeatManager) executeRecoveryCommand(ctx context.Context, nodeID uint, s *heartbeatSession, settings *domain.CrashRecoverySettings) {
	log := logger.GetLogger().WithField("node_id", nodeID)

	// Fetch a fresh node copy — never share the heartbeat loop's node pointer
	node, err := hm.usecase.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		log.WithError(err).Error("[HeartbeatManager] Failed to fetch node for recovery command")
		return
	}
	log = log.WithField("node_name", node.Name)

	// Connect to agent
	client, err := hm.usecase.getAgentClient(node)
	if err != nil {
		log.WithError(err).Error("[HeartbeatManager] Failed to connect for recovery command")
		// Set cooldown timer even on connection failure to prevent per-pong retry spam
		// when the agent is persistently unreachable. Don't increment recoveryCommandCount
		// since no command actually ran.
		s.lastRecoveryCommandTime = time.Now()
		hm.publishXrayEvent(events.EventXrayCrashLoop, node, s.crashCount,
			fmt.Sprintf("Xray auto-disabled. Recovery command failed: %v", err))
		return
	}
	defer client.Close()

	// Execute command
	log.WithField("command", settings.Command).Info("[HeartbeatManager] Executing crash recovery command")
	resp, err := client.ExecuteCommand(ctx, settings.Command, settings.CommandTimeout)

	if err != nil {
		log.WithError(err).Error("[HeartbeatManager] Recovery command RPC failed")
		hm.publishRecoveryEvent(events.EventXrayRecoveryCommand, node, s, settings, &recoveryResult{
			err: err,
		})
		// Persist RPC failure for panel display (attempt not counted, but error is visible)
		node.LastCrashRecovery = &domain.LastCrashRecovery{
			Timestamp:   time.Now(),
			AttemptNum:  s.recoveryCommandCount,
			MaxAttempts: settings.MaxAttempts,
			Error:       err.Error(),
		}
		if updateErr := hm.usecase.nodeRepo.UpdateNode(ctx, node); updateErr != nil {
			log.WithError(updateErr).Error("[HeartbeatManager] Failed to persist recovery RPC failure")
		}
		return
	}

	// Update tracking state — only after the command actually ran on the node.
	// RPC failures (network timeout, agent unreachable) must not burn an attempt.
	s.recoveryCommandCount++
	s.lastRecoveryCommandTime = time.Now()

	// Publish notification with command output
	hm.publishRecoveryEvent(events.EventXrayRecoveryCommand, node, s, settings, &recoveryResult{
		exitCode: int(resp.ExitCode),
		stdout:   resp.Stdout,
		stderr:   resp.Stderr,
		success:  resp.Success,
	})

	// Persist recovery result for panel display
	node.LastCrashRecovery = &domain.LastCrashRecovery{
		Timestamp:   time.Now(),
		ExitCode:    int(resp.ExitCode),
		Stdout:      resp.Stdout,
		Stderr:      resp.Stderr,
		Success:     resp.Success,
		AttemptNum:  s.recoveryCommandCount,
		MaxAttempts: settings.MaxAttempts,
		Exhausted:   false,
	}

	// Start xray one more time
	log.Info("[HeartbeatManager] Recovery command done, starting xray")
	node.XrayStopped = false
	if updateErr := hm.usecase.nodeRepo.UpdateNode(ctx, node); updateErr != nil {
		log.WithError(updateErr).Error("[HeartbeatManager] Failed to clear XrayStopped after recovery")
		return
	}

	if startErr := hm.usecase.StartXray(ctx, node.ID); startErr != nil {
		log.WithError(startErr).Error("[HeartbeatManager] Failed to start xray after recovery command")
	}

	// Reset crash count so the next cycle starts fresh
	s.crashCount = 0
	s.crashLoopNotified = false
}

// fetchAndLogErrors fetches recent error logs from the agent and logs them server-side.
// This runs in a background goroutine — does NOT publish follow-up events to avoid duplicate notifications.
func (hm *HeartbeatManager) fetchAndLogErrors(node *domain.Node, crashCount int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log := logger.GetLogger().WithField("node_id", node.ID).WithField("node_name", node.Name)

	client, err := hm.usecase.getAgentClient(node)
	if err != nil {
		return
	}
	defer client.Close()

	stream, err := client.StreamLogs(ctx, 10, false)
	if err != nil {
		return
	}

	var lastErrors []string
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		if msg.Level == "error" || msg.Level == "warning" {
			lastErrors = append(lastErrors, msg.Message)
		}
		if len(lastErrors) >= 5 {
			break
		}
	}

	if len(lastErrors) > 0 {
		log.WithFields(map[string]interface{}{
			"crash_count": crashCount,
			"error_lines": strings.Join(lastErrors, " | "),
		}).Warn("[HeartbeatManager] Xray crash error context")
	}
}

// StartHeartbeats initializes and starts the heartbeat manager for all agent nodes.
func (u *nodeUsecase) StartHeartbeats(ctx context.Context) {
	u.heartbeatMgr = NewHeartbeatManager(u)
	u.heartbeatMgr.Start(ctx)
}

// StopHeartbeats gracefully shuts down the heartbeat manager.
func (u *nodeUsecase) StopHeartbeats() {
	if u.heartbeatMgr != nil {
		u.heartbeatMgr.Stop()
	}
}

// GetHeartbeatManager returns the heartbeat manager instance.
// Must be called after StartHeartbeats; returns nil if heartbeats have not been started.
func (u *nodeUsecase) GetHeartbeatManager() *HeartbeatManager {
	return u.heartbeatMgr
}

// TriggerSync signals the heartbeat manager to immediately re-sync sessions.
// Used after connect mode switches to avoid waiting for the 30s ticker.
func (hm *HeartbeatManager) TriggerSync() {
	select {
	case hm.syncNow <- struct{}{}:
	default:
	}
}
