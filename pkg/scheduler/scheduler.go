package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/metrics"
	"gopkg.in/telebot.v3"
)

// AccountExhaustionChecker checks for per-inbound data limit exhaustion
type AccountExhaustionChecker interface {
	CheckAndDisableExhaustedAccounts(ctx context.Context) (int, error)
}

// Scheduler runs periodic tasks for subscription and server management
type Scheduler struct {
	subUsecase        SubscriptionUsecase
	nodeUsecase       NodeUsecase
	sniUsecase        SNIUsecase
	notifRepo         NotificationRepository
	userRepo          UserRepository
	bot               *telebot.Bot
	statsInterval     time.Duration
	expireInterval    time.Duration
	reconcileInterval time.Duration
	stopChan          chan struct{}
	expireDoneChan    chan struct{}
	reconcileDoneChan chan struct{}
	adminTelegramIDs  []int64
	lastDailyDigest   time.Time
	lastTokenCleanup  time.Time

	// Retention cleanup dependencies
	settingUC          SettingUsecase
	nodeRepository     NodeRepository
	provisioningRepo   ProvisioningRepository
	auditUC            AuditLogUsecase
	lastRetentionClean time.Time

	// Daily usage snapshot
	usageRepo         UserDailyUsageRepository
	lastUsageSnapshot time.Time

	// Prometheus metrics collector
	metricsCollector *metrics.Collector

	// Per-inbound account exhaustion checker
	accountExhaustionChecker AccountExhaustionChecker

	// Chat message cleanup
	chatCleanupFn func(ctx context.Context) error

	// JWT blacklist cleanup
	jwtBlacklistCleanupFn func(ctx context.Context) (int64, error)

	// Subscription IP cleanup
	subIPRepo     SubscriptionIPRepository
	lastIPCleanup time.Time

	// Online-users snapshot
	onlineUsersRecorder OnlineUsersSnapshotRecorder
	onlineUsersCleaner  OnlineUsersSnapshotCleaner

	// Additional retention cleaners (optional)
	alertEventCleaner    AlertEventCleaner
	subDailyUsageCleaner SubscriptionDailyUsageCleaner

	// Context for active operations
	ctx    context.Context
	cancel context.CancelFunc

	// Separate done channel for the stats goroutine
	statsDoneChan chan struct{}

	// Guards against overlapping executions, one per loop. Stats loop is
	// already bounded by SyncNodeStats' own per-cycle constraints.
	expireRunning    atomic.Bool
	reconcileRunning atomic.Bool

	// Protects lastDailyDigest, lastTokenCleanup, lastRetentionClean,
	// lastUsageSnapshot, and lastIPCleanup from concurrent reads/writes.
	mu sync.Mutex

	// Scheduler configuration
	cfg Config
}

// Config holds scheduler configuration
type Config struct {
	// StatsInterval drives SyncNodeStats + SSE freshness (default 5s).
	// Short: a slow value makes the panel look stale.
	StatsInterval time.Duration
	// ExpireInterval drives expiration + data-limit + notification
	// checks (default 30s). These flip UI state (active → expired)
	// and send messages; seconds of lag is acceptable.
	ExpireInterval time.Duration
	// ReconcileInterval drives the heavy user-reconcile sweep plus
	// cert renewals and cleanup tasks (default 60s). Heartbeat
	// already detects drift in ~seconds, so reconcile is a catch-up
	// safety net and does not need a hot cadence.
	ReconcileInterval time.Duration

	AdminTelegramIDs []int64 // Admin Telegram IDs for digest notifications

	TokenCleanupInterval      time.Duration // How often to cleanup expired tokens (default: 6h)
	RetentionCleanupInterval  time.Duration // How often to run retention cleanup (default: 6h)
	UsageSnapshotInterval     time.Duration // How often to take usage snapshots (default: 23h)
	IPCleanupInterval         time.Duration // How often to cleanup old IPs (default: 24h)
	IPRetentionDays           int           // How many days of IPs to keep (default: 30)
	NotificationRetentionDays int           // How many days of notifications to keep (default: 30)

	// Retention cleanup dependencies
	SettingUsecase   SettingUsecase
	NodeRepository   NodeRepository
	ProvisioningRepo ProvisioningRepository
	AuditUsecase     AuditLogUsecase

	// OnlineUsersSnapshotRecorder writes a global online-user count
	// snapshot at the end of each collectNodeStats tick. If nil, the
	// tick path skips the write — enabling tests without DB wiring.
	OnlineUsersSnapshotRecorder OnlineUsersSnapshotRecorder

	// OnlineUsersSnapshotCleaner prunes old snapshots during the
	// retention cleanup loop. May be nil; cleanup is skipped if so.
	OnlineUsersSnapshotCleaner OnlineUsersSnapshotCleaner

	// Daily usage snapshots
	UserDailyUsageRepo UserDailyUsageRepository

	// Prometheus metrics collector (optional)
	MetricsCollector *metrics.Collector

	// Per-inbound account exhaustion checker (optional)
	AccountExhaustionChecker AccountExhaustionChecker

	// Subscription IP cleanup (optional)
	SubscriptionIPRepo SubscriptionIPRepository

	// Chat message cleanup (optional)
	ChatCleanupFn func(ctx context.Context) error

	// JWT blacklist cleanup (optional) — removes expired revoked tokens
	JWTBlacklistCleanupFn func(ctx context.Context) (int64, error)

	// Additional retention cleaners (all optional — nil disables the task).
	AlertEventCleaner             AlertEventCleaner
	SubscriptionDailyUsageCleaner SubscriptionDailyUsageCleaner
}

// DefaultConfig returns default scheduler configuration
func DefaultConfig() Config {
	return Config{
		StatsInterval:             5 * time.Second,
		ExpireInterval:            30 * time.Second,
		ReconcileInterval:         60 * time.Second,
		TokenCleanupInterval:      6 * time.Hour,
		RetentionCleanupInterval:  6 * time.Hour,
		UsageSnapshotInterval:     23 * time.Hour,
		IPCleanupInterval:         24 * time.Hour,
		IPRetentionDays:           30,
		NotificationRetentionDays: 30,
	}
}

// New creates a new scheduler
func New(
	subUsecase SubscriptionUsecase,
	nodeUsecase NodeUsecase,
	sniUsecase SNIUsecase,
	notifRepo NotificationRepository,
	userRepo UserRepository,
	bot *telebot.Bot,
	cfg Config,
) *Scheduler {
	if cfg.StatsInterval == 0 {
		cfg.StatsInterval = 5 * time.Second
	}
	if cfg.ExpireInterval == 0 {
		cfg.ExpireInterval = 30 * time.Second
	}
	if cfg.ReconcileInterval == 0 {
		cfg.ReconcileInterval = 60 * time.Second
	}
	if cfg.TokenCleanupInterval == 0 {
		cfg.TokenCleanupInterval = 6 * time.Hour
	}
	if cfg.RetentionCleanupInterval == 0 {
		cfg.RetentionCleanupInterval = 6 * time.Hour
	}
	if cfg.UsageSnapshotInterval == 0 {
		cfg.UsageSnapshotInterval = 23 * time.Hour
	}
	if cfg.IPCleanupInterval == 0 {
		cfg.IPCleanupInterval = 24 * time.Hour
	}
	if cfg.IPRetentionDays == 0 {
		cfg.IPRetentionDays = 30
	}
	if cfg.NotificationRetentionDays == 0 {
		cfg.NotificationRetentionDays = 30
	}

	return &Scheduler{
		subUsecase:               subUsecase,
		nodeUsecase:              nodeUsecase,
		sniUsecase:               sniUsecase,
		notifRepo:                notifRepo,
		userRepo:                 userRepo,
		bot:                      bot,
		statsInterval:            cfg.StatsInterval,
		expireInterval:           cfg.ExpireInterval,
		reconcileInterval:        cfg.ReconcileInterval,
		stopChan:                 make(chan struct{}),
		expireDoneChan:           make(chan struct{}),
		reconcileDoneChan:        make(chan struct{}),
		adminTelegramIDs:         cfg.AdminTelegramIDs,
		settingUC:                cfg.SettingUsecase,
		nodeRepository:           cfg.NodeRepository,
		provisioningRepo:         cfg.ProvisioningRepo,
		auditUC:                  cfg.AuditUsecase,
		usageRepo:                cfg.UserDailyUsageRepo,
		metricsCollector:         cfg.MetricsCollector,
		accountExhaustionChecker: cfg.AccountExhaustionChecker,
		subIPRepo:                cfg.SubscriptionIPRepo,
		chatCleanupFn:            cfg.ChatCleanupFn,
		jwtBlacklistCleanupFn:    cfg.JWTBlacklistCleanupFn,
		onlineUsersRecorder:      cfg.OnlineUsersSnapshotRecorder,
		onlineUsersCleaner:       cfg.OnlineUsersSnapshotCleaner,
		alertEventCleaner:        cfg.AlertEventCleaner,
		subDailyUsageCleaner:     cfg.SubscriptionDailyUsageCleaner,
		cfg:                      cfg,
	}
}

// Start runs the three ambient loops (stats 5s, expiration 30s, reconcile 60s)
// on separate goroutines, each guarded by an atomic flag so a slow pass
// doesn't starve the others.
func (s *Scheduler) Start() {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"stats_interval":     s.statsInterval,
		"expire_interval":    s.expireInterval,
		"reconcile_interval": s.reconcileInterval,
	}).Info("Scheduler starting...")

	s.ctx, s.cancel = context.WithCancel(context.Background())

	// One-shot agent-node sync on startup (triggers xray restart).
	go s.syncAgentNodes(s.ctx)

	s.nodeUsecase.StartHeartbeats(s.ctx)

	// First reconcile sweep immediately so we don't wait ReconcileInterval.
	go s.reconcileNodes(s.ctx)

	s.statsDoneChan = make(chan struct{})
	go s.runStatsLoop(s.ctx)
	go s.runExpirationLoop(s.ctx)
	go s.runReconcileLoop(s.ctx)
}

// Stop cancels the context (aborts in-flight RPCs) and waits for each
// loop's done channel. StopHeartbeats also triggers client pool shutdown.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}

	// Signal loops to stop. close once — extra sends would panic.
	close(s.stopChan)

	// Stop heartbeat streams (sessions see cancelled context and exit fast).
	s.nodeUsecase.StopHeartbeats()

	if s.statsDoneChan != nil {
		<-s.statsDoneChan
	}
	if s.expireDoneChan != nil {
		<-s.expireDoneChan
	}
	if s.reconcileDoneChan != nil {
		<-s.reconcileDoneChan
	}
}

// runStatsLoop collects node stats and publishes SSE events on a
// dedicated ticker, independent of the heavier maintenance tasks.
func (s *Scheduler) runStatsLoop(ctx context.Context) {
	defer close(s.statsDoneChan)
	log := logger.GetLogger()

	ticker := time.NewTicker(s.statsInterval)
	defer ticker.Stop()

	// Run once immediately on startup.
	s.collectNodeStats(ctx)

	for {
		select {
		case <-ticker.C:
			s.collectNodeStats(ctx)
		case <-ctx.Done():
			log.Debug("Stats loop stopping (context cancelled)")
			return
		case <-s.stopChan:
			return
		}
	}
}

// runExpirationLoop: sub expiration + data-limit exhaustion +
// notifications at ExpireInterval. Sub-second cadence not needed.
func (s *Scheduler) runExpirationLoop(ctx context.Context) {
	defer close(s.expireDoneChan)
	log := logger.GetLogger()

	ticker := time.NewTicker(s.expireInterval)
	defer ticker.Stop()

	// First pass slightly delayed so heartbeat startup finishes.
	firstTick := time.After(5 * time.Second)

	for {
		select {
		case <-firstTick:
			firstTick = nil // only fire once
			s.runExpirationChecks(ctx)
		case <-ticker.C:
			if !s.expireRunning.CompareAndSwap(false, true) {
				log.Debug("Scheduler: skipping expire tick, previous still running")
				continue
			}
			func() {
				defer s.expireRunning.Store(false)
				s.runExpirationChecks(ctx)
			}()
		case <-ctx.Done():
			log.Debug("Expiration loop stopping (context cancelled)")
			return
		case <-s.stopChan:
			return
		}
	}
}

// runReconcileLoop: heavy reconcile sweep + cert renewals + daily
// cleanups at ReconcileInterval. Heartbeat handles primary drift
// detection; this is the safety net (agent restart dropping xray's
// user map without master noticing).
func (s *Scheduler) runReconcileLoop(ctx context.Context) {
	defer close(s.reconcileDoneChan)
	log := logger.GetLogger()

	ticker := time.NewTicker(s.reconcileInterval)
	defer ticker.Stop()

	// Stagger first pass 10s so heartbeat + expiration loops settle first.
	firstTick := time.After(10 * time.Second)

	for {
		select {
		case <-firstTick:
			firstTick = nil
			s.runReconcileChecks(ctx)
		case <-ticker.C:
			if !s.reconcileRunning.CompareAndSwap(false, true) {
				log.Debug("Scheduler: skipping reconcile tick, previous still running")
				continue
			}
			func() {
				defer s.reconcileRunning.Store(false)
				s.runReconcileChecks(ctx)
			}()
		case <-ctx.Done():
			log.Debug("Reconcile loop stopping (context cancelled)")
			return
		case <-s.stopChan:
			return
		}
	}
}

// collectNodeStats wraps SyncNodeStats with metrics observation.
func (s *Scheduler) collectNodeStats(ctx context.Context) {
	s.observeTask("sync_node_stats", func() error {
		if err := s.nodeUsecase.SyncNodeStats(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("Scheduler: Failed to collect node stats")
			return err
		}
		return nil
	})

	if s.onlineUsersRecorder != nil {
		if err := s.onlineUsersRecorder.RecordOnlineUsersSnapshot(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("Scheduler: Failed to record online-users snapshot")
		}
	}
}

// observeTask records the duration of a scheduler task in Prometheus.
// If the task returns an error, the error counter is also incremented.
func (s *Scheduler) observeTask(name string, fn func() error) {
	start := time.Now()
	err := fn()
	if metrics.Registry != nil && metrics.Enabled.Load() {
		metrics.SchedulerTaskDuration.WithLabelValues(name).Observe(time.Since(start).Seconds())
		if err != nil {
			metrics.SchedulerTaskErrors.WithLabelValues(name).Inc()
		}
	}
}

// runExpirationChecks: medium-cadence group (notifications + expiration
// transitions). Keep tight so the loop meets its tick under load.
func (s *Scheduler) runExpirationChecks(ctx context.Context) {
	log := logger.GetLogger()

	// Notifications first — they read the pre-transition state
	// (e.g. "expires in 3 days"). Flipping expiry status before
	// sending would misclassify the subscription.
	s.sendExpirationNotifications(ctx)
	if ctx.Err() != nil {
		return
	}
	s.sendDataUsageNotifications(ctx)
	if ctx.Err() != nil {
		return
	}
	s.sendTrafficExhaustedNotifications(ctx)
	if ctx.Err() != nil {
		return
	}

	// Now apply expiration transitions.
	s.observeTask("expire_subscriptions", func() error {
		if err := s.subUsecase.CheckAndExpireSubscriptions(ctx); err != nil {
			log.WithError(err).Error("Failed to check date-expired subscriptions")
			return err
		}
		if err := s.subUsecase.CheckAndExpireByDataLimit(ctx); err != nil {
			log.WithError(err).Error("Failed to check data-exhausted subscriptions")
			return err
		}
		if s.accountExhaustionChecker != nil {
			if _, err := s.accountExhaustionChecker.CheckAndDisableExhaustedAccounts(ctx); err != nil {
				log.WithError(err).Error("Failed to check per-inbound exhausted accounts")
			}
		}
		return nil
	})

	// Prometheus metric collection is cheap and needs to be
	// refreshed often enough that it doesn't belong in the slow
	// reconcile bucket.
	if s.metricsCollector != nil {
		s.metricsCollector.Collect(ctx)
	}
}

// runReconcileChecks: slow-cadence group (user reconcile, cert renewal,
// daily maintenance). Safety net behind heartbeat-driven drift detection.
func (s *Scheduler) runReconcileChecks(ctx context.Context) {
	log := logger.GetLogger()

	// Inbound sync (server-restart recovery).
	s.observeTask("reconcile_nodes", func() error { s.reconcileNodes(ctx); return nil })
	if ctx.Err() != nil {
		return
	}

	// The expensive one. Walks every active node × every inbound,
	// diffs users in memory vs DB, patches via AlterInbound.
	s.observeTask("reconcile_users", func() error {
		stats, err := s.subUsecase.ReconcileUsers(ctx)
		if err != nil {
			log.WithError(err).Error("Failed to reconcile users")
			return err
		}
		if stats.MissingAdded > 0 || stats.GhostsRemoved > 0 || stats.Errors > 0 {
			log.WithFields(map[string]interface{}{
				"active_db":      stats.TotalDBUsers,
				"active_xray":    stats.TotalXrayUsers,
				"missing_added":  stats.MissingAdded,
				"ghosts_removed": stats.GhostsRemoved,
				"errors":         stats.Errors,
			}).Info("User Reconciliation Report")
		}
		return nil
	})
	if ctx.Err() != nil {
		return
	}

	// Cert renewals.
	s.observeTask("cert_renewals", func() error { s.checkCertRenewals(ctx); return nil })

	// Daily admin digest (inner gating — ~9 AM UTC).
	s.checkAndSendDailyDigest(ctx)

	// Daily cleanups — each gated by its own "last run" timestamp so
	// re-running this loop every 60s is cheap when the interval has
	// not elapsed.
	if s.notifRepo != nil {
		s.notifRepo.CleanupOldNotifications(ctx, s.cfg.NotificationRetentionDays)
	}

	// Periodic JWT blacklist cleanup
	if s.jwtBlacklistCleanupFn != nil {
		s.mu.Lock()
		shouldClean := time.Since(s.lastTokenCleanup) > s.cfg.TokenCleanupInterval
		s.mu.Unlock()
		if shouldClean {
			if deleted, err := s.jwtBlacklistCleanupFn(ctx); err != nil {
				log.WithError(err).Warn("Failed to cleanup expired revoked JWT tokens")
			} else if deleted > 0 {
				log.WithField("deleted", deleted).Info("Cleaned up expired revoked JWT tokens")
			}
			s.mu.Lock()
			s.lastTokenCleanup = time.Now()
			s.mu.Unlock()
		}
	}

	if s.settingUC != nil {
		s.mu.Lock()
		shouldCleanRetention := time.Since(s.lastRetentionClean) > s.cfg.RetentionCleanupInterval
		s.mu.Unlock()
		if shouldCleanRetention {
			s.runRetentionCleanup(ctx)
			s.mu.Lock()
			s.lastRetentionClean = time.Now()
			s.mu.Unlock()
		}
	}

	if s.usageRepo != nil {
		s.mu.Lock()
		shouldSnapshot := time.Since(s.lastUsageSnapshot) > s.cfg.UsageSnapshotInterval
		s.mu.Unlock()
		if shouldSnapshot {
			s.recordDailyUsageSnapshots(ctx)
			s.mu.Lock()
			s.lastUsageSnapshot = time.Now()
			s.mu.Unlock()
		}
	}

	if s.subIPRepo != nil {
		s.mu.Lock()
		shouldCleanIPs := time.Since(s.lastIPCleanup) > s.cfg.IPCleanupInterval
		s.mu.Unlock()
		if shouldCleanIPs {
			olderThan := time.Now().AddDate(0, 0, -s.cfg.IPRetentionDays)
			deleted, err := s.subIPRepo.DeleteOldSubscriptionIPs(ctx, olderThan)
			if err != nil {
				log.WithError(err).Warn("Failed to cleanup old subscription IPs")
			} else if deleted > 0 {
				log.WithField("deleted", deleted).Info("Cleaned up old subscription IPs")
			}
			s.mu.Lock()
			s.lastIPCleanup = time.Now()
			s.mu.Unlock()
		}
	}
}
