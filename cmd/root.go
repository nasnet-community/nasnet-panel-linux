package cmd

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	adminMiddleware "github.com/nasnet-community/nasnet-panel-linux/internal/admin/middleware"
	adminUC "github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"

	// Account packages
	accountTg "github.com/nasnet-community/nasnet-panel-linux/internal/account/delivery/telegram"
	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"

	// Node packages
	nodeTg "github.com/nasnet-community/nasnet-panel-linux/internal/node/delivery/telegram"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"

	agentconfig "github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"

	"github.com/nasnet-community/nasnet-panel-linux/internal/monitor"
	networkDomain "github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/preflight"
	networkRepo "github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	networkSystem "github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	networkUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	notifPkg "github.com/nasnet-community/nasnet-panel-linux/pkg/notification"

	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	wireguardDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wireguardNodebridge "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/nodebridge"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/metrics"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	xrayProvider "github.com/nasnet-community/nasnet-panel-linux/pkg/product/xray"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/scheduler"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	httpTransport "github.com/nasnet-community/nasnet-panel-linux/transport/http"
	telegramTransport "github.com/nasnet-community/nasnet-panel-linux/transport/telegram"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
	// Import domain packages for auto-migration
	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	alertDomain "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	chatDomain "github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	notifDomain "github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
	provisioningDomain "github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
	sniTg "github.com/nasnet-community/nasnet-panel-linux/internal/sni/delivery/telegram"
	sniDomain "github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"

	// Import telegram handlers
	adminTg "github.com/nasnet-community/nasnet-panel-linux/internal/admin/delivery/telegram"
	mntTg "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/delivery/telegram"
	subTg "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/delivery/telegram"
	userTg "github.com/nasnet-community/nasnet-panel-linux/internal/user/delivery/telegram"

	// Setting packages
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"

	// Audit packages
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
)

// Version info injected via -ldflags at build time.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var rootCmd = &cobra.Command{
	Use:     "nasnet-panel",
	Short:   "Xray panel",
	Long:    `Multi interface platform for managing and selling xray-core proxies`,
	Version: Version,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server and Telegram bot",
	Long:  `Start both the HTTP API server and the Telegram bot.`,
	Run:   runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(newNetCmd())
}

// WebFS holds the embedded SPA filesystem. Set by main before Execute().
var WebFS embed.FS

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// nodeSyncerAdapter adapts NodeUsecase to implement contract.NodeSyncer
type nodeSyncerAdapter struct {
	nodeUsecase nodeUC.NodeUsecase
}

func (a *nodeSyncerAdapter) SyncInbounds(ctx context.Context, nodeID uint) error {
	_, err := a.nodeUsecase.SyncInbounds(ctx, nodeID)
	return err
}

func runServe(cmd *cobra.Command, args []string) {
	// Load configuration
	cfg := config.Load()

	// Validate configuration
	if result := cfg.Validate(); result.HasErrors() {
		fmt.Fprintf(os.Stderr, "Configuration errors:\n%s", result)
		os.Exit(1)
	} else if len(result.Warnings) > 0 {
		// Warnings are logged after logger init below
		defer func() {
			for _, w := range result.Warnings {
				logger.GetLogger().WithField("field", w.Field).Warn("Config warning: " + w.Message)
			}
		}()
	}

	// Create a root context for background services
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	logger.Init(cfg.Log.Level, cfg.Log.Format)
	log := logger.GetLogger()

	log.Info("Starting...")

	// Database connection
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	log.Info("Connected to database")

	// Auto migrate models
	if err := database.AutoMigrate(db,
		&userDomain.User{},
		&subDomain.Subscription{},
		&nodeDomain.Node{},
		&nodeDomain.Inbound{},
		&nodeDomain.Outbound{},
		&nodeDomain.RoutingRule{},
		&nodeDomain.BalancingRule{},
		&nodeDomain.NodeStat{},
		&nodeDomain.Host{},
		&nodeDomain.HostTemplate{},
		&sniDomain.SNI{},
		&sniDomain.InboundSNI{},
		&accountDomain.Account{},
		&notifDomain.NotificationLog{},
		&provisioningDomain.ProvisioningTask{},
		&conversation.SessionEntity{},
		&settingDomain.Setting{},
		&auditDomain.AuditLog{},
		&userDomain.UserDailyUsage{},
		&subDomain.SubscriptionDailyUsage{},
		&subDomain.SubscriptionIP{},
		&nodeDomain.AccessLogSummary{},
		&nodeDomain.NodeDailyTraffic{},
		&nodeDomain.NodeUptimeEvent{},
		&nodeDomain.StarlinkStat{},
		&nodeDomain.ReverseProxy{},
		&chatDomain.ChatMessage{},
		&chatDomain.ChatReaction{},
		&alertDomain.Rule{},
		&alertDomain.State{},
		&alertDomain.Event{},
		&jwt.RevokedToken{},
		&adminDomain.OnlineUsersSnapshot{},
		&wireguardDomain.WGPeer{},
		&networkDomain.NetworkInterface{},
		&networkDomain.WANGroup{},
		&networkDomain.WANGroupMember{},
		&networkDomain.LANConfig{},
		&networkDomain.WifiConfig{},
		&networkDomain.PortForward{},
		&networkDomain.ApplyRecord{},
	); err != nil {
		log.WithError(err).Fatal("Failed to run migrations")
	}

	// Singleton roles need a DB constraint
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_netif_singleton_role
		  ON network_interfaces (node_id, role)
		  WHERE role IN ('lan','mgmt') AND deleted_at IS NULL`).Error; err != nil {
		log.WithError(err).Warn("Failed to create ux_netif_singleton_role")
	}

	// One uplink per slot, or two rows render the same unit file
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_netif_uplink_slot
		  ON network_interfaces (node_id, slot)
		  WHERE role = 'wan' AND slot != '' AND deleted_at IS NULL`).Error; err != nil {
		log.WithError(err).Warn("Failed to create ux_netif_uplink_slot")
	}

	if cfg.Router.Enabled {
		// net rollback boot check
		if err := runNetRollback(true); err != nil {
			log.WithError(err).Warn("Boot rollback check failed")
		}
	}

	// Create GIN indexes for PostgreSQL (unsupported in SQLite)
	if database.IsPostgres() {
		ginIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_routing_rules_geoip_rules_gin ON routing_rules USING gin (geo_ip_rules)",
			"CREATE INDEX IF NOT EXISTS idx_routing_rules_inbound_tags_gin ON routing_rules USING gin (inbound_tags)",
			"CREATE INDEX IF NOT EXISTS idx_routing_rules_user_emails_gin ON routing_rules USING gin (user_emails)",
		}
		for _, sql := range ginIndexes {
			if err := db.Exec(sql).Error; err != nil {
				log.WithError(err).Warn("Failed to create GIN index (non-fatal)")
			}
		}
	}

	// Infrastructure
	grpcClient := xray.NewGRPCClient(cfg.Xray.APITimeout)
	log.Info("Xray gRPC client initialized")

	providerFactory := product.NewProviderFactory()
	xrayProv := xrayProvider.NewProvider(grpcClient)
	providerFactory.Register(xrayProv)
	log.Info("XrayProvider registered")

	eventBus := events.NewEventBus()
	log.Info("Event Bus initialized")

	// Repositories
	repos := initRepositories(db)

	// Outbound proxy factory (early, ACME needs it). Empty until seeded
	// post-usecase-init. Snapshot consumers use LiveClient for live reloads.
	httpFactory := httpclient.NewFactory()
	geoip.SetClientFactory(func() *http.Client {
		return httpFactory.ClientFor(httpclient.FeatureGeoIP, httpclient.EgressForeign, 3*time.Second)
	})

	// ACME settings from database first, then environment config
	acmeEnabled := cfg.ACME.Enabled
	acmeEmail := cfg.ACME.Email
	acmeStaging := cfg.ACME.Staging
	if s, err := repos.Setting.GetByKey(context.Background(), "acme_enabled"); err == nil {
		acmeEnabled = s.Value == "true"
	}
	if s, err := repos.Setting.GetByKey(context.Background(), "acme_email"); err == nil && s.Value != "" {
		acmeEmail = s.Value
	}
	if s, err := repos.Setting.GetByKey(context.Background(), "acme_staging"); err == nil {
		acmeStaging = s.Value == "true"
	}

	var certManager *acme.CertManager
	if acmeEnabled {
		if acmeEmail != "" {
			// LiveClient routes LE API calls through the factory transport;
			// proxy_use_acme toggle is live. DNS-01 still uses system resolver.
			acmeClient := httpFactory.LiveClient(httpclient.FeatureACME, httpclient.EgressAdvertised, 30*time.Second)
			certManager, err = acme.NewCertManager(acmeEmail, cfg.ACME.CacheDir, acmeStaging, acmeClient)
			if err != nil {
				log.WithError(err).Warn("Failed to initialize ACME CertManager - certificate issuance disabled")
				certManager = nil
			}
		} else {
			log.Warn("ACME_ENABLED is true but ACME_EMAIL not set - certificate issuance disabled")
			certManager = nil
		}
	}

	// Usecases
	uc := initUsecases(db, cfg, repos, grpcClient, providerFactory, certManager, eventBus)

	// Wire NodeClient callback so the provider can reach both direct and reverse-mode nodes
	xrayProv.SetNodeClientFunc(uc.Node.GetNodeClient)

	// WireGuard: render managed peers into pushed configs + suspend/resume peers on sub lifecycle
	uc.Node.SetWGPeerSource(wireguardNodebridge.New(repos.WGPeer))
	uc.Node.SetRouterMode(cfg.Router.Enabled)

	httpFactory.SetRouterMode(cfg.Router.Enabled, httpclient.EgressDomestic)
	xrayProv.SetWGProvisioner(uc.WGDevice)

	// (Single binary mode) run the node agent in-process. Panel drives this server through an embedded client
	agentCfg := agentconfig.DefaultConfig()
	agentCfg.TLS.Enabled = false
	agentCfg.Process.ServiceMode = false
	embeddedSrv, err := agentserver.NewServer(agentCfg)
	if err != nil {
		log.WithError(err).Fatal("Failed to create in-process node agent server")
	}

	// Reconcile the network BEFORE the agent starts! (xray needs routing path)
	var networkUC networkUsecase.NetworkUsecase
	if cfg.Router.Enabled {
		var rmErr error
		networkUC, rmErr = startRouterMode(bgCtx, routerModeDeps{
			DB: db, NftMgr: embeddedSrv.NftManager(),
			Agent: agent.NewEmbeddedClient(embeddedSrv), Bus: eventBus, Log: log,
			PanelPort: cfg.App.Port, NodeRepo: repos.Node, HTTPFactory: httpFactory,
		})
		if rmErr != nil {
			log.WithError(rmErr).Fatal("Router mode failed to start")
		}
	}

	if err := embeddedSrv.StartLocal(bgCtx); err != nil {
		log.WithError(err).Fatal("Failed to start in-process node agent server")
	}
	uc.Node.SetEmbeddedServer(embeddedSrv)
	if networkUC != nil {
		// Shaping follows the uplink clients arrive on.
		uc.Node.SetIngressUplinkSource(networkUC.IngressUplinkIfName)
	}
	log.Info("In-process node agent started (single-binary mode)")

	// Create the single local node
	if nodes, listErr := uc.Node.ListNodes(context.Background()); listErr != nil {
		log.WithError(listErr).Warn("Failed to list nodes for local-node bootstrap")
	} else if len(nodes) == 0 {
		if _, createErr := uc.Node.CreateNode(context.Background(), "Local", "127.0.0.1", "", "", 10085, 0, "local", false, false); createErr != nil {
			log.WithError(createErr).Warn("Failed to bootstrap local node")
		} else {
			log.Info("Bootstrapped local node")
		}
	}

	// Apply log level from db settings (overrides config file)
	if val, err := uc.Setting.GetByKey(context.Background(), "log_level"); err == nil && val != "" {
		if err := logger.SetLevel(val); err == nil {
			log.WithField("level", val).Info("Log level applied from settings")
		}
	}

	// Outbound proxy settings reload
	applyProxySettings := func() {
		urlVal, _ := uc.Setting.GetByKey(context.Background(), "outbound_proxy_url")
		enabled := make(map[httpclient.Feature]bool, len(httpclient.AllFeatures()))
		for _, feat := range httpclient.AllFeatures() {
			v, _ := uc.Setting.GetByKey(context.Background(), "proxy_use_"+string(feat))
			enabled[feat] = v == "true"
		}
		httpFactory.Update(httpclient.Config{ProxyURL: urlVal, Enabled: enabled})
	}
	applyProxySettings()
	uc.Setting.SetOnOutboundProxyChange(func(proxyURL string, enabled map[string]bool) {
		feats := make(map[httpclient.Feature]bool, len(enabled))
		for k, v := range enabled {
			feats[httpclient.Feature(k)] = v
		}
		httpFactory.Update(httpclient.Config{ProxyURL: proxyURL, Enabled: feats})
		log.Info("[httpclient] outbound proxy settings reloaded")
	})

	go uc.ProvWorker.Start()
	log.Info("Provisioning Worker started")

	// Admin middleware & sync
	adminMW := adminMiddleware.NewAdminMiddleware(repos.User, cfg.Admin.InitialAdminIDs)
	log.WithField("admin_ids", cfg.Admin.InitialAdminIDs).Info("Admin middleware initialized")

	for _, adminTgID := range cfg.Admin.InitialAdminIDs {
		if u, err := repos.User.FindByTelegramID(context.Background(), adminTgID); err == nil {
			if !u.IsAdmin {
				if err := repos.User.UpdateAdminStatus(context.Background(), u.ID, true); err == nil {
					log.WithField("telegram_id", adminTgID).Info("Synced initial admin flag in database")
				}
			}
		}
	}

	// Telegram bot
	var bot *telegramTransport.Bot
	backupSvc := adminUC.NewBackupService(db, cfg.Database, cfg.App.BackupDir)

	if cfg.Telegram.Enabled {
		stateManager := conversation.NewStateManager(db)

		// Prefer proxy settings from database (panel-editable) over environment config.
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_enabled"); err == nil {
			cfg.Telegram.Proxy.Enabled = s.Value == "true"
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_type"); err == nil && s.Value != "" {
			cfg.Telegram.Proxy.Type = s.Value
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_host"); err == nil && s.Value != "" {
			cfg.Telegram.Proxy.Host = s.Value
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_port"); err == nil && s.Value != "" {
			if p, parseErr := strconv.Atoi(s.Value); parseErr == nil {
				cfg.Telegram.Proxy.Port = p
			}
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_username"); err == nil {
			cfg.Telegram.Proxy.Username = s.Value
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "telegram_proxy_password"); err == nil {
			cfg.Telegram.Proxy.Password = s.Value
		}

		userTelegramHandler := userTg.NewHandler(uc.User, uc.Subscription, uc.Setting, cfg.JWT.SecretKey)
		subTelegramHandler := subTg.NewHandler(uc.Subscription, uc.User, stateManager, cfg.App.BaseURL, uc.Setting, uc.WGDevice)
		sniTelegramHandler := sniTg.NewHandler(uc.SNI, uc.Node, stateManager)
		nodeTelegramHandler := nodeTg.NewHandler(uc.Node, uc.SNI, stateManager)
		maintenanceTelegramHandler := mntTg.NewHandler(uc.Maintenance)
		accountTelegramHandler := accountTg.NewHandler(uc.Account)
		adminTelegramHandler := adminTg.NewHandler(uc.Admin, uc.Account, uc.Subscription, userRepo.NewUserRepository(db), stateManager, nil, uc.Audit, uc.Setting, uc.Node, repos.Provisioning, backupSvc, db) // Bot set later

		var err error
		bot, err = telegramTransport.NewBot(
			cfg.Telegram,
			cfg.Admin.InitialAdminIDs,
			userTelegramHandler,
			subTelegramHandler,
			adminTelegramHandler,
			nodeTelegramHandler,
			sniTelegramHandler,
			maintenanceTelegramHandler,
			adminMW,
			stateManager,
			uc.User,
			uc.Setting,
			httpFactory,
		)
		if err != nil {
			// Telegram may be unreachable or token invalid (not fatal)
			log.WithError(err).Error("Failed to initialize Telegram bot; continuing without it")
			bot = nil
		} else {
			// Bind if worked
			accountTg.RegisterHandlers(bot.GetBot(), accountTelegramHandler, adminMW.RequireAdmin)
			adminTelegramHandler.SetBot(bot.GetBot())
			uc.Admin.SetBot(bot.GetBot())
			subTelegramHandler.SetMaintenanceUC(uc.Maintenance)
			setMaintenanceBroadcastBot(bot.GetBot())

			go bot.Start()
			log.Info("Telegram bot started")
		}
	} else {
		log.Info("Telegram bot disabled (TELEGRAM_ENABLED=false)")
	}

	// Admin Telegram IDs (config + DB)
	var telegramBot *telebot.Bot
	if bot != nil {
		telegramBot = bot.GetBot()
	}
	adminTelegramIDs := append([]int64{}, cfg.Admin.InitialAdminIDs...)
	if dbAdmins, err := repos.User.ListAdmins(context.Background()); err == nil {
		for _, admin := range dbAdmins {
			found := false
			for _, id := range adminTelegramIDs {
				if id == admin.TelegramID {
					found = true
					break
				}
			}
			if !found {
				adminTelegramIDs = append(adminTelegramIDs, admin.TelegramID)
			}
		}
	}

	// Metrics auth from DB settings (panel-editable, requires restart)
	if val, err := uc.Setting.GetByKey(context.Background(), "metrics_username"); err == nil && val != "" {
		cfg.Metrics.Username = val
	}
	if val, err := uc.Setting.GetByKey(context.Background(), "metrics_password"); err == nil && val != "" {
		cfg.Metrics.Password = val
	}

	// Panel Base Path from DB settings (panel editable, requires restart)
	if s, err := repos.Setting.GetByKey(context.Background(), "panel_base_path"); err == nil {
		cfg.App.PanelBasePath = config.CleanBasePath(s.Value)
	}

	// Prometheus metrics
	var metricsCollector *metrics.Collector
	if cfg.Metrics.Enabled {
		metrics.Init()

		sqlDB, _ := db.DB()
		statsProvider := metrics.NewDefaultStatsProvider(db)
		metricsCollector = metrics.NewCollector(sqlDB, statsProvider, uc.Setting)

		eventBus.OnPublish = func(eventType string) {
			if metrics.Enabled.Load() {
				metrics.EventsPublished.WithLabelValues(eventType).Inc()
			}
		}
		eventBus.OnDrop = func(eventType string, subscriberID string) {
			if metrics.Enabled.Load() {
				metrics.EventsDropped.WithLabelValues(eventType, subscriberID).Inc()
			}
		}
		eventBus.OnSubscriberChange = func(count int) {
			metrics.EventBusSubscribers.Set(float64(count))
		}

		metrics.StartEventListener(eventBus)
		log.Info("Prometheus metrics enabled")
	}

	// JWT Blacklist (created early so scheduler can use its cleanup)
	blacklist := jwt.NewBlacklist(db)

	// Scheduler
	schedulerCfg := scheduler.Config{
		StatsInterval:                 5 * time.Second,
		AdminTelegramIDs:              adminTelegramIDs,
		SettingUsecase:                uc.Setting,
		NodeRepository:                repos.Node,
		ProvisioningRepo:              repos.Provisioning,
		AuditUsecase:                  uc.Audit,
		UserDailyUsageRepo:            repos.UserDailyUsage,
		MetricsCollector:              metricsCollector,
		AccountExhaustionChecker:      uc.Account,
		SubscriptionIPRepo:            repos.SubscriptionIP,
		ChatCleanupFn:                 uc.Chat.CleanupOldMessages,
		JWTBlacklistCleanupFn:         blacklist.Cleanup,
		OnlineUsersSnapshotRecorder:   uc.Admin,
		OnlineUsersSnapshotCleaner:    repos.OnlineUsersSnapshot,
		AlertEventCleaner:             repos.Alert,
		SubscriptionDailyUsageCleaner: repos.Subscription,
	}
	expirationScheduler := scheduler.New(
		uc.Subscription,
		uc.Node,
		uc.SNI,
		repos.Notification,
		repos.User,
		telegramBot,
		schedulerCfg,
	)
	expirationScheduler.Start()
	log.Info("Expiration scheduler started")

	// Wire scheduler's on-demand retention method into admin usecase.
	// CleanupSummary → map[string]int64.
	uc.Admin.SetRetentionCleanupRunner(func(ctx context.Context) map[string]int64 {
		return expirationScheduler.RunRetentionNow(ctx)
	})

	// Monitor
	var botNotifier notifPkg.Notifier
	if bot != nil {
		botNotifier = bot
	}
	monitorService := monitor.NewMonitorService(repos.Node, uc.Node, botNotifier, 10*time.Second, eventBus)
	go monitorService.Start(bgCtx)
	log.Info("Server Monitor Service started")

	// Online Users Cache Cleanup
	cache.StartCleanup(bgCtx)

	// Heartbeat Manager
	uc.Node.StartHeartbeats(bgCtx)
	log.Info("Heartbeat Manager started")

	// Notification Dispatcher
	telegramCh := notifPkg.NewTelegramChannel(botNotifier)
	discordCh := notifPkg.NewDiscordChannel(uc.Setting, httpFactory)
	webhookCh := notifPkg.NewWebhookChannel(uc.Setting, httpFactory)
	dispatcher := notifPkg.NewDispatcher(eventBus, uc.Setting, telegramCh, discordCh, webhookCh)
	dispatcher.Start(bgCtx)

	// Alert engine: seed defaults (idempotent), then start. Engine after
	// dispatcher so first fires route in order (bus buffers events either way).
	if err := uc.Alert.SeedDefaults(bgCtx); err != nil {
		log.WithError(err).Warn("Failed to seed default alert rules (continuing)")
	}
	uc.AlertEngine.Start(bgCtx)

	// JWT
	jwtManager := jwt.NewManager(jwt.Config{
		SecretKey:          cfg.JWT.SecretKey,
		AccessTokenExpiry:  time.Duration(cfg.JWT.AccessTokenExpiry) * time.Minute,
		RefreshTokenExpiry: time.Duration(cfg.JWT.RefreshTokenExpiry) * time.Hour,
		CookieDomain:       cfg.JWT.CookieDomain,
		CookieSecure:       cfg.JWT.CookieSecure,
		Issuer:             "nasnet-panel",
	})
	jwtManager.SetBlacklist(blacklist)
	log.Info("JWT Manager initialized with token blacklist")

	tokenManager := auth.NewTokenManager(cfg.JWT.SecretKey)

	// HTTP server
	server := httpTransport.NewServer(httpTransport.ServerDeps{
		Core: httpTransport.CoreDeps{
			UserUsecase: uc.User,
			SubUsecase:  uc.Subscription,
			ChatUsecase: uc.Chat,
			WGDevice:    uc.WGDevice,
		},
		Admin: httpTransport.AdminDeps{
			AdminUsecase:       uc.Admin,
			AccountUsecase:     uc.Account,
			NodeUsecase:        uc.Node,
			SNIUsecase:         uc.SNI,
			SettingUsecase:     uc.Setting,
			CertificateUsecase: uc.Certificate,
			AuditUsecase:       uc.Audit,
			BackupService:      backupSvc,
			AlertUsecase:       uc.Alert,
			MaintenanceUsecase: uc.Maintenance,
			NetworkUsecase:     networkUC,
			RouterMode:         cfg.Router.Enabled,
		},
		Infra: httpTransport.InfraDeps{
			DB:                db,
			JWTManager:        jwtManager,
			TokenManager:      tokenManager,
			EventBus:          eventBus,
			ACMEManager:       certManager,
			ShutdownCtx:       bgCtx,
			HTTPClientFactory: httpFactory,
		},
		Config: httpTransport.ConfigDeps{
			AdminConfig:      cfg.Admin,
			AppConfig:        cfg.App,
			DatabaseConfig:   cfg.Database,
			MetricsConfig:    cfg.Metrics,
			TelegramBotToken: cfg.Telegram.BotToken,
			WebFS:            WebFS,
		},
		Repos: httpTransport.RepoDeps{
			NodeRepository: repos.Node,
			UserRepository: repos.User,
			SubRepository:  repos.Subscription,
		},
	})
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)

		// TLS only if manual cert files or ACME enabled; behind reverse
		// proxy, APP_BASE_URL can be HTTPS while app listens plain HTTP.
		// Prefer DB settings (panel-editable) for cert/key paths.
		tlsCertFile := cfg.App.TLSCertFile
		tlsKeyFile := cfg.App.TLSKeyFile
		if s, err := repos.Setting.GetByKey(context.Background(), "tls_cert_file"); err == nil && s.Value != "" {
			tlsCertFile = s.Value
		}
		if s, err := repos.Setting.GetByKey(context.Background(), "tls_key_file"); err == nil && s.Value != "" {
			tlsKeyFile = s.Value
		}
		hasManualCerts := tlsCertFile != "" && tlsKeyFile != ""

		if hasManualCerts {
			cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
			if err != nil {
				log.WithError(err).Warn("Failed to load TLS cert files, falling back to plain HTTP")
				goto plainHTTP
			}
			tlsCfg := &tls.Config{
				MinVersion:   tls.VersionTLS12,
				Certificates: []tls.Certificate{cert},
			}
			log.Info("TLS configured with manual certificate files")
			log.WithField("address", addr).Info("HTTPS server starting")
			if err := server.RunTLS(addr, tlsCfg); err != nil && err != http.ErrServerClosed {
				log.WithError(err).Fatal("HTTPS server failed")
			}
			return
		} else if certManager != nil {
			parsed, err := url.Parse(cfg.App.BaseURL)
			if err != nil {
				log.WithError(err).Warn("Failed to parse APP_BASE_URL, falling back to plain HTTP")
				goto plainHTTP
			}
			domain := parsed.Hostname()
			if err := certManager.EnsureServerCert(bgCtx, domain); err != nil {
				log.WithError(err).Warn("Failed to obtain ACME server cert, falling back to plain HTTP")
				goto plainHTTP
			}
			tlsCfg := certManager.ServerTLSConfig()
			certManager.StartServerCertRenewal(bgCtx, domain)
			log.WithField("domain", domain).Info("TLS configured with ACME auto-certificate")
			log.WithField("address", addr).Info("HTTPS server starting")
			if err := server.RunTLS(addr, tlsCfg); err != nil && err != http.ErrServerClosed {
				log.WithError(err).Fatal("HTTPS server failed")
			}
			return
		}

	plainHTTP:
		log.WithField("address", addr).Info("HTTP server starting")
		if err := server.Run(addr); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownStart := time.Now()
	log.Info("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop background event producers so SSE connections can drain
	bgCancel()                    // stops monitor service
	embeddedSrv.Stop(shutdownCtx) // stop in process xray
	uc.Node.StopHeartbeats()
	uc.AlertEngine.Stop()
	dispatcher.Stop()
	expirationScheduler.Stop()
	uc.ProvWorker.Stop()
	eventBus.Close() // closes all SSE subscriber channels, unblocking streaming handlers
	log.WithField("elapsed", time.Since(shutdownStart)).Info("Shutdown phase 1: background services stopped")

	// Stop accepting new HTTP requests and drain active connections
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("HTTP server shutdown error")
	}
	log.WithField("elapsed", time.Since(shutdownStart)).Info("Shutdown phase 2: HTTP server stopped")

	// Stop bot
	if bot != nil {
		bot.Stop()
	}
	log.WithField("elapsed", time.Since(shutdownStart)).Info("Shutdown phase 3: bot stopped")

	// Close connections
	grpcClient.CloseAll()
	uc.Audit.Stop()
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.Close()
	}
	log.WithField("elapsed", time.Since(shutdownStart)).Info("Shutdown phase 4: connections closed")

	log.WithField("total_elapsed", time.Since(shutdownStart)).Info("Server stopped")
}

type routerModeDeps struct {
	DB     *gorm.DB
	NftMgr *nft.Manager
	Agent  agent.NodeClient
	Bus    *events.EventBus
	Log    *logrus.Logger
	// PanelPort is what filter_in must keep open, or arming it locks the
	// operator out of the box they just firewalled.
	PanelPort int
	// NodeRepo supplies the inbound rows filter_in derives its accepts from.
	NodeRepo inboundLister
	// HTTPFactory routes the domestic address-list refresh like every other
	// outbound the panel makes.
	HTTPFactory *httpclient.Factory
}

// startRouterMode runs preflight, builds the network usecase and reconciles
func startRouterMode(ctx context.Context, deps routerModeDeps) (networkUsecase.NetworkUsecase, error) {
	paths := networkSystem.DefaultPaths()

	applyRepo := networkRepo.NewApplyRepository(deps.DB)
	_, confirmErr := applyRepo.LatestConfirmed(ctx)
	takeoverDone := confirmErr == nil && networkSystem.TakeoverDone(paths)

	env, err := preflight.Probe(takeoverDone)
	if err != nil {
		return nil, fmt.Errorf("preflight probe: %w", err)
	}
	result := preflight.Check(env)
	for _, w := range result.Warn {
		deps.Log.WithField("component", "router").Warn(w)
	}
	if !result.OK() {
		return nil, fmt.Errorf("router mode preflight failed: %s", strings.Join(result.Fatal, "; "))
	}

	backend, err := networkSystem.NewNetlinkBackend()
	if err != nil {
		return nil, fmt.Errorf("netlink backend: %w", err)
	}

	uc := networkUsecase.NewNetworkUsecase(networkUsecase.Deps{
		IfRepo:     networkRepo.NewInterfaceRepository(deps.DB),
		GroupRepo:  networkRepo.NewGroupRepository(deps.DB),
		ApplyRepo:  applyRepo,
		LANRepo:    networkRepo.NewLANRepository(deps.DB),
		PFRepo:     networkRepo.NewPortForwardRepository(deps.DB),
		PanelPort:  deps.PanelPort,
		Backend:    backend,
		Nft:        deps.NftMgr,
		Agent:      deps.Agent,
		Paths:      paths,
		RouterMode: true,
		EventBus:   deps.Bus,
		Inbounds:   inboundSource{repo: deps.NodeRepo, nodeID: 1},
		// The list is served from behind a redirect to a foreign host, so it
		// goes out the same way every other foreign fetch does.
		RangesClient: deps.HTTPFactory.ClientFor(
			httpclient.FeatureGeofiles, httpclient.EgressForeign, 2*time.Minute),
	})

	// Loud, never fatal: the panel is the only tool the operator has to fix
	// whatever broke the reconcile.
	if err := uc.Reconcile(ctx); err != nil {
		deps.Log.WithError(err).WithField("component", "router").
			Error("Network reconcile failed; router mode is degraded")
	}
	uc.StartHealthLoop(ctx, 5*time.Second)
	uc.StartRangesRefreshLoop(ctx, 0)
	deps.Log.Info("Router mode active")
	return uc, nil
}
