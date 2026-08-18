package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	accountUC "github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
	adminRepo "github.com/nasnet-community/nasnet-panel-linux/internal/admin/repository"
	adminUC "github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	alertEngine "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/engine"
	alertRepo "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/repository"
	alertUC "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/usecase"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	auditRepo "github.com/nasnet-community/nasnet-panel-linux/internal/audit/repository"
	auditUC "github.com/nasnet-community/nasnet-panel-linux/internal/audit/usecase"
	chatDomain "github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	chatRepo "github.com/nasnet-community/nasnet-panel-linux/internal/chat/repository"
	chatUC "github.com/nasnet-community/nasnet-panel-linux/internal/chat/usecase"
	mntAdapt "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/adapters"
	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	notifRepo "github.com/nasnet-community/nasnet-panel-linux/internal/notification/repository"
	provisioningService "github.com/nasnet-community/nasnet-panel-linux/internal/provisioning"
	provisioningRepo "github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/repository"
	provisioningWorker "github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/worker"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	settingUC "github.com/nasnet-community/nasnet-panel-linux/internal/setting/usecase"
	sniRepo "github.com/nasnet-community/nasnet-panel-linux/internal/sni/repository"
	sniUC "github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	userUC "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	wireguardRepo "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/repository"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

// maintenanceBroadcastBotRef is set once the telegram bot has been built so
// that the broadcast closure used by the maintenance usecase can reach the
// real *telebot.Bot. It is read from the closure installed in initUsecases.
var maintenanceBroadcastBotRef *telebot.Bot

func setMaintenanceBroadcastBot(b *telebot.Bot) {
	maintenanceBroadcastBotRef = b
}

// repositoryBundle holds all repository instances created during bootstrap.
type repositoryBundle struct {
	User                userRepo.UserRepository
	Subscription        subRepo.SubscriptionRepository
	Node                nodeRepo.NodeRepository
	SNI                 sniRepo.SNIRepository
	Account             accountRepo.AccountRepository
	Provisioning        provisioningRepo.ProvisioningRepository
	UserDailyUsage      userRepo.UserDailyUsageRepository
	Setting             settingDomain.SettingRepository
	Notification        notifRepo.NotificationRepository
	Audit               auditDomain.AuditLogRepository
	Alert               alertRepo.AlertRepository
	AgentCert           nodeRepo.AgentCertificateRepository
	SubscriptionIP      subRepo.SubscriptionIPRepository
	Chat                chatDomain.ChatRepository
	OnlineUsersSnapshot adminRepo.OnlineUsersSnapshotRepository
	WGPeer              wireguardRepo.WGPeerRepository
}

// usecaseBundle holds all usecase instances created during bootstrap.
type usecaseBundle struct {
	User         userUC.UserUsecase
	SNI          sniUC.SNIUsecase
	Setting      settingDomain.SettingUsecase
	Account      accountUC.AccountUsecase
	Certificate  nodeUC.CertificateUsecase
	Node         nodeUC.NodeUsecase
	Subscription subUC.SubscriptionUsecase
	Admin        adminUC.AdminUsecase
	Audit        auditDomain.AuditLogUsecase
	Chat         chatDomain.ChatUsecase
	Alert        alertUC.AlertUsecase
	AlertEngine  *alertEngine.Engine
	Maintenance  mntUC.Usecase
	WGDevice     wireguardUC.DeviceUsecase

	ProvService provisioningService.ProvisioningService
	ProvWorker  *provisioningWorker.Worker
}

// initRepositories creates all repository instances from the database handle.
func initRepositories(db *gorm.DB) repositoryBundle {
	return repositoryBundle{
		User:                userRepo.NewUserRepository(db),
		Subscription:        subRepo.NewSubscriptionRepository(db),
		Node:                nodeRepo.NewNodeRepository(db),
		SNI:                 sniRepo.NewSNIRepository(db),
		Account:             accountRepo.NewAccountRepository(db),
		Provisioning:        provisioningRepo.NewProvisioningRepository(db),
		UserDailyUsage:      userRepo.NewUserDailyUsageRepository(db),
		Setting:             settingRepo.NewSettingRepository(db),
		Notification:        notifRepo.NewNotificationRepository(db),
		Audit:               auditRepo.NewAuditRepository(db),
		Alert:               alertRepo.NewAlertRepository(db),
		AgentCert:           nodeRepo.NewAgentCertificateRepository(db),
		SubscriptionIP:      subRepo.NewSubscriptionIPRepository(db),
		Chat:                chatRepo.NewChatRepository(db),
		OnlineUsersSnapshot: adminRepo.NewOnlineUsersSnapshotRepository(db),
		WGPeer:              wireguardRepo.NewWGPeerRepository(db),
	}
}

// initUsecases creates all usecase instances, wiring dependencies in the correct order.
// The only remaining setter call after this function is planUsecase.SetSubscriptionReconciler
// (true cycle) and the late-binding SetBot calls.
func initUsecases(
	db *gorm.DB,
	cfg *config.Config,
	repos repositoryBundle,
	grpcClient *xray.GRPCClient,
	providerFactory *product.ProviderFactory,
	certManager *acme.CertManager,
	eventBus *events.EventBus,
) usecaseBundle {
	log := logger.GetLogger()

	provService := provisioningService.NewProvisioningService(repos.Provisioning)

	// --- Usecases with no cross-usecase deps ---
	userUsecase := userUC.NewUserUsecase(repos.User, eventBus)
	sniUsecase := sniUC.NewSNIUsecase(repos.SNI, certManager)

	settingUsecase := settingUC.NewSettingUsecase(repos.Setting, &settingUC.InitialConfig{
		AppPort: cfg.App.Port,
		BaseURL: cfg.App.BaseURL,

		SubPanelURL:     cfg.App.SubPanelURL,
		BotToken:        cfg.Telegram.BotToken,
		ACMEEnabled:     cfg.ACME.Enabled,
		ACMEEmail:       cfg.ACME.Email,
		ACMEStaging:     cfg.ACME.Staging,
		ACMEAutoRenew:   cfg.ACME.AutoRenew,
		TLSCertFile:     cfg.App.TLSCertFile,
		TLSKeyFile:      cfg.App.TLSKeyFile,
		MetricsUsername: cfg.Metrics.Username,
		MetricsPassword: cfg.Metrics.Password,
		LogLevel:        cfg.Log.Level,
		PanelBasePath:   cfg.App.PanelBasePath,
		ProxyEnabled:    cfg.Telegram.Proxy.Enabled,
		ProxyType:       cfg.Telegram.Proxy.Type,
		ProxyHost:       cfg.Telegram.Proxy.Host,
		ProxyPort:       cfg.Telegram.Proxy.Port,
		ProxyUsername:   cfg.Telegram.Proxy.Username,
		ProxyPassword:   cfg.Telegram.Proxy.Password,
	})

	// --- Usecases with cross-usecase deps ---
	certUsecase := nodeUC.NewCertificateUsecase(repos.AgentCert, repos.Node, certManager)

	transactionManager := database.NewTransactionManager(db)

	// nodeUsecase must be built before accountUsecase so we can wire the
	// node-sweep trigger (SyncAccountStats delegates there to reuse the
	// scheduler's buffered-traffic path instead of calling xray directly).
	nodeUsecase := nodeUC.NewNodeUsecase(
		repos.Node,
		repos.Subscription,
		repos.SubscriptionIP,
		repos.Account,
		sniUsecase,
		certUsecase,
		provService,
		eventBus,
		settingUsecase,
		transactionManager,
	)

	// A renewed/edited cert must be re-pushed to the nodes that serve it; the
	// node usecase implements that hook.
	sniUsecase.SetRepusher(nodeUsecase)

	wgDeviceUsecase := wireguardUC.NewDeviceUsecase(repos.WGPeer, repos.Node, repos.Subscription, repos.Account, nodeUsecase)

	accountUsecase := accountUC.NewAccountUsecase(repos.Account, repos.Node, nodeUsecase, provService)

	// Wire certificate revocation callback
	certUsecase.SetOnRevokeCallback(func(ctx context.Context) {
		if err := nodeUsecase.PushCertDenylistToAllNodes(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("[CertRevoke] Failed to push denylist to agents")
		}
	})

	// Provisioning worker (nodeUC passed in constructor; started by caller)
	provWorker := provisioningWorker.NewWorker(repos.Provisioning, nodeUsecase)

	// NodeSyncer adapter bridges (*SyncResult, error) vs error
	nodeSyncer := &nodeSyncerAdapter{nodeUsecase}

	// SubscriptionUsecase (all deps passed in constructor)
	subUsecase := subUC.NewSubscriptionUsecase(
		repos.Subscription,
		repos.User,
		repos.Node,
		providerFactory,
		grpcClient,
		nodeUsecase,
		nodeSyncer,
		accountUsecase,
		accountUsecase,
		transactionManager,
		eventBus,
	)

	// Re-arm one-shot subscription notifications (e.g. data-exhausted) on
	// renew/reset by letting the usecase clear their log rows.
	if setter, ok := subUsecase.(interface {
		SetNotificationCleaner(subUC.NotificationCleaner)
	}); ok {
		setter.SetNotificationCleaner(repos.Notification)
	}

	// sub module need wg peer interface for reading the wg key
	if setter, ok := subUsecase.(interface {
		SetWGPeerReader(subUC.WGPeerReader)
	}); ok {
		setter.SetWGPeerReader(repos.WGPeer)
	}

	// AdminUsecase (all deps passed in constructor)
	adminUsecase := adminUC.NewAdminUsecase(
		repos.User,
		repos.Subscription,
		repos.SubscriptionIP,
		subUsecase,
		repos.Node,
		nodeUsecase,
		providerFactory,
		grpcClient,
		provService,
		cfg.Xray.InboundTag,
		settingUsecase,
		accountUsecase,
		repos.Account,
		repos.UserDailyUsage,
		db,
		repos.OnlineUsersSnapshot,
		adminRepo.NewRetentionStatsRepository(db),
	)

	auditUsecase := auditUC.NewAuditUsecase(repos.Audit)
	log.Info("Audit logging initialized")

	// Wire audit usecase into setting usecase so settings changes are audited
	settingUsecase.SetAuditUsecase(auditUsecase)

	// Wire audit usecase into node usecase so Node Nuke/Wipe operations are audited.
	// nodeUsecase is built before auditUsecase (nodeUsecase predates audit), so
	// we plumb via setter to break the ordering dependency.
	nodeUsecase.SetAuditUsecase(auditUsecase)

	// Seed default settings
	if err := settingUsecase.SeedDefaults(context.Background()); err != nil {
		log.WithError(err).Warn("Failed to seed default settings")
	}

	// Nodes stored before the uuid column existed carry an empty one, and the
	// heartbeat's agent-identity check skips a node whose UUID is empty.
	if err := nodeUsecase.BackfillNodeUUIDs(context.Background()); err != nil {
		log.WithError(err).Warn("Failed to backfill node UUIDs")
	}

	// After a SQLite backup restore the process restarts, but the restored
	// database may contain deployment-specific settings (URLs, ports, tokens)
	// from a different server. Check for the marker file and re-apply env values.
	reseedMarker := filepath.Join(cfg.App.BackupDir, ".reseed_after_restore")
	if _, err := os.Stat(reseedMarker); err == nil {
		log.Info("Post-restore reseed marker found, re-applying env settings")
		if err := settingUsecase.ReseedEnvSettings(context.Background()); err != nil {
			log.WithError(err).Warn("Failed to reseed env settings after restore")
		}
		os.Remove(reseedMarker)
	}

	// Migrate global panel password from plaintext to bcrypt if needed.
	settingUsecase.MigrateGlobalPanelPassword(context.Background())

	chatUsecase := chatUC.NewChatUsecase(
		repos.Chat,
		settingUsecase,
		eventBus,
		func(ctx context.Context, subID uint) string {
			sub, err := repos.Subscription.FindByID(ctx, subID)
			if err != nil {
				return ""
			}
			return sub.Label
		},
	)

	// Alerting: engine subscribes to the EventBus and publishes SystemAlert
	// events back to the same bus, which the notification dispatcher then
	// routes to Telegram/webhook based on the existing per-channel settings.
	alertEng := alertEngine.NewEngine(eventBus, repos.Alert)
	alertUsecase := alertUC.NewAlertUsecase(repos.Alert, alertEng)

	// Maintenance mode: broadcast closure reads the bot ref set by root.go
	// after bot construction. Admin broadcast reuses admin_usecase's path.
	maintenanceBroadcastFn := func(ctx context.Context, msg string) error {
		if maintenanceBroadcastBotRef == nil {
			return nil
		}
		_, err := adminUsecase.BroadcastMessage(ctx, maintenanceBroadcastBotRef, msg, true)
		return err
	}
	maintenanceUC := mntUC.NewUsecase(
		&mntAdapt.SettingAdapter{UC: settingUsecase},
		&mntAdapt.NodeAdapter{DB: db},
		&mntAdapt.SubAdapter{DB: db},
		maintenanceBroadcastFn,
	)
	if err := maintenanceUC.HydrateFromSettings(context.Background()); err != nil {
		log.WithError(err).Warn("Failed to hydrate maintenance mode state")
	}
	// Re-hydrate atomic state whenever maintenance settings are written via the
	// generic settings PUT/import path, so the in-memory globalActive/Message
	// can't drift from the persisted DB values.
	settingUsecase.SetOnMaintenanceChange(func() {
		if err := maintenanceUC.HydrateFromSettings(context.Background()); err != nil {
			log.WithError(err).Warn("Failed to re-hydrate maintenance state after settings change")
		}
	})

	return usecaseBundle{
		User:         userUsecase,
		SNI:          sniUsecase,
		Setting:      settingUsecase,
		Account:      accountUsecase,
		Certificate:  certUsecase,
		Node:         nodeUsecase,
		Subscription: subUsecase,
		Admin:        adminUsecase,
		Audit:        auditUsecase,
		Chat:         chatUsecase,
		Alert:        alertUsecase,
		AlertEngine:  alertEng,
		Maintenance:  maintenanceUC,
		WGDevice:     wgDeviceUsecase,
		ProvService:  provService,
		ProvWorker:   provWorker,
	}
}
