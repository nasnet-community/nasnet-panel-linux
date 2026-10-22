package cmd

import (
	"context"
	"crypto/x509"
	"embed"
	"errors"
	"fmt"
	agentpool "github.com/nasnet-community/nasnet-panel-linux/internal/agent/pool"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
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
	"github.com/nasnet-community/nasnet-panel-linux/internal/hub"
	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"

	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	coreRepository "github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	coreUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/monitor"
	"github.com/nasnet-community/nasnet-panel-linux/internal/mtprotonode"
	networkDomain "github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/preflight"
	networkRepo "github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	networkSystem "github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	networkUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/dohboot"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	notifPkg "github.com/nasnet-community/nasnet-panel-linux/pkg/notification"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/runtimestatus"

	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	userProfileUC "github.com/nasnet-community/nasnet-panel-linux/internal/userprofile/usecase"
	wireguardDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wireguardNodebridge "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/nodebridge"
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
	updateUC "github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"

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
	rootCmd.AddCommand(newUpdateCmd())
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
		&nodeDomain.OutboundPool{},
		&nodeDomain.OutboundPoolSource{},
		&nodeDomain.OutboundPoolCandidate{},
		&nodeDomain.OutboundPoolRun{},
		&nodeDomain.NodeDailyUptime{},
		&nodeDomain.HubLifecycleEvent{},
		&nodeDomain.StarlinkStat{},
		&nodeDomain.StarlinkEvent{},
		&nodeDomain.AgentCertificate{},
		&nodeDomain.NodeCertSighting{},
		&nodeDomain.UsedDeploymentToken{},
		&nodeDomain.ReverseProxy{},
		&nodeDomain.ReverseTunnel{},
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
		&networkDomain.PortMapRule{},
		&networkDomain.LANDeviceLabel{},
		&networkDomain.VPNProfile{},
		&networkDomain.ApplyRecord{},
		&coreDomain.CoreInbound{},
		&coreDomain.CoreUser{},
		&coreDomain.NodeCoreSetting{},
		&mtprotonode.LiteNode{},
		&mtprotonode.UsageBaseline{},
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

	// Each pool interface slot belongs to one profile.
	if err := networkRepo.EnsureVPNPoolIndex(db); err != nil {
		log.WithError(err).Warn("Failed to create the VPN pool slot index")
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

	// 200 events is days of wan/vpn churn. The flow page's timeline reads it,
	// so a page opened after an incident still shows what led up to it.
	netEvents := events.NewRecorder(eventBus, "network-recorder", 200, events.IsNetworkEvent)

	// Repositories
	repos := initRepositories(db)
	// Keep deployment values for seeding and backup-restore reseeding. Saved
	// settings resolve into the running config, not the environment baseline.
	envCfg := *cfg
	getStartupSetting := func(key string) (string, error) {
		s, err := repos.Setting.GetByKey(bgCtx, key)
		if err != nil {
			return "", err
		}
		return s.Value, nil
	}
	if err := resolveStartupConfig(cfg, getStartupSetting); err != nil {
		log.WithError(err).Fatal("Invalid startup configuration")
	}

	// Outbound proxy factory (early, ACME needs it). Empty until seeded
	// post-usecase-init. Snapshot consumers use LiveClient for live reloads.
	httpFactory := httpclient.NewFactory()
	geoip.SetClientFactory(func() *http.Client {
		return httpFactory.ClientFor(httpclient.FeatureGeoIP, httpclient.EgressForeign, 3*time.Second)
	})

	// LiveClient applies the saved outbound proxy settings once usecases load.
	acmeClient := httpFactory.LiveClient(httpclient.FeatureACME, httpclient.EgressAdvertised, 30*time.Second)
	certManager, err := initACMEManager(cfg, acmeClient)
	if err != nil {
		log.WithError(err).Fatal("Cannot initialize requested TLS; refusing to start plain HTTP")
	}

	// Usecases
	uc := initUsecases(db, &envCfg, repos, grpcClient, providerFactory, certManager, eventBus)

	// Wire NodeClient callback so the provider can reach both direct and reverse-mode nodes
	xrayProv.SetNodeClientFunc(uc.Node.GetNodeClient)
	xrayProv.SetShapingPushFunc(uc.Node.RequestShapingPush)

	// WireGuard: render managed peers into pushed configs + suspend/resume peers on sub lifecycle
	wgPeers := wireguardNodebridge.New(repos.WGPeer)
	wgPeers.SetAccessResolver(func(ctx context.Context, subIDs []uint) (map[uint]bool, error) {
		out := make(map[uint]bool, len(subIDs))
		for _, id := range subIDs {
			sub, err := repos.Subscription.FindByID(ctx, id)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					out[id] = false // peer outlived its subscription
				}
				continue // transient: no opinion, render the peer as stored
			}
			out[id] = sub.GrantsAccess()
		}
		return out, nil
	})
	uc.Node.SetWGPeerSource(wgPeers)
	uc.Subscription.SetWGAccess(uc.WGDevice) // Revoking a subscription must reach its WG peers too, not only via the Xray
	uc.Node.SetRouterMode(cfg.Router.Enabled)

	httpFactory.SetRouterMode(cfg.Router.Enabled, httpclient.EgressDomestic)
	xrayProv.SetWGProvisioner(uc.WGDevice)

	// (Single binary mode) run the node agent in-process. Panel drives this server through an embedded client
	agentCfg := agentconfig.DefaultConfig()
	agentCfg.TLS.Enabled = false
	agentCfg.Process.ServiceMode = false
	// Non-standard installs (and local test runs) can move xray's files.
	if v := os.Getenv("XRAY_BINARY_PATH"); v != "" {
		agentCfg.Xray.BinaryPath = v
	} else {
		agentCfg.Xray.BinaryPath = builtinXrayPath(context.Background(), uc.Setting, installedXray())
	}
	if v := os.Getenv("XRAY_CONFIG_PATH"); v != "" {
		agentCfg.Xray.ConfigPath = v
	}
	if v := os.Getenv("XRAY_ACCESS_LOG_PATH"); v != "" {
		agentCfg.Xray.AccessLogPath = v
	}
	if v := os.Getenv("CORES_DATA_DIR"); v != "" {
		agentCfg.Cores.DataDir = v
	}
	agentCfg.Cores.Embedded = true
	// Tunnel binaries follow the proxy settings and, on a router, the
	// foreign uplinks like every other GitHub download.
	agentCfg.Cores.Download = httpFactory.LiveClient(httpclient.FeatureCoreBinaries, httpclient.EgressForeign, 10*time.Minute)
	if cfg.Router.Enabled {
		// Tunnel egress follows the foreign pool like xray's direct-foreign, and
		// names resolve over DoH on the same mark: the uplink's own resolver may
		// poison exactly the names users tunnel to.
		mark := netmark.GroupMark(netmark.GroupForeign)
		agentCfg.Cores.EgressMark = mark
		agentCfg.Cores.Resolve = dohboot.New(mark).Resolve
	}
	// The built-in node reports the panel's version; releases only stamp cmd.
	if agentserver.Version == "dev" {
		agentserver.Version = Version
	}
	embeddedSrv, err := agentserver.NewServer(agentCfg)
	if err != nil {
		log.WithError(err).Fatal("Failed to create in-process node agent server")
	}

	// Reverse-mode hub: off unless HUB_ENABLED or the hub_enabled setting.
	hubEnabled, hubPort := resolveHub(cfg, uc.Setting)
	firewallHubPort := 0
	if hubEnabled {
		firewallHubPort = hubPort
	}

	coreRepo := coreRepository.NewCoreRepository(db)

	// Reconcile the network BEFORE the agent starts! (xray needs routing path)
	var networkUC networkUsecase.NetworkUsecase
	if cfg.Router.Enabled {
		var rmErr error
		networkUC, rmErr = startRouterMode(bgCtx, routerModeDeps{
			DB: db, NftMgr: embeddedSrv.NftManager(),
			Agent: agent.NewEmbeddedClient(embeddedSrv), Bus: eventBus,
			NetEvents: netEvents, Log: log,
			PanelPort: cfg.App.Port, HubPort: firewallHubPort, NodeRepo: repos.Node, Tunnels: coreRepo, LocalNodeID: uc.Node.LocalNodeID, HTTPFactory: httpFactory,
			PoolSettings: poolSettings{uc: uc.Setting},
		})
		if rmErr != nil {
			log.WithError(rmErr).Fatal("Router mode failed to start")
		}
	}

	// The built-in node scans pools inside this process (D7).
	agentpool.Configure(nodeDomain.LocalPoolScanThreads)
	if err := embeddedSrv.StartLocal(bgCtx); err != nil {
		log.WithError(err).Fatal("Failed to start in-process node agent server")
	}
	uc.Node.SetEmbeddedServer(embeddedSrv)
	uc.NodePools.Start()
	if networkUC != nil {
		// Shaping follows the uplink clients arrive on.
		uc.Node.SetIngressUplinkSource(networkUC.IngressUplinkIfName)
		// An inbound edit has to reach the firewall and the mapper at once, or
		// an armed filter_in silently drops the inbound that was just added.
		uc.Node.SetInboundsChangedHook(networkUC.OnInboundsChanged)
		// Read per config build, so the outbounds follow role changes.
		uc.Node.SetRouterWANSource(func(ctx context.Context) []xray.RouterWAN {
			views := networkUC.RouterWANs(ctx)
			wans := make([]xray.RouterWAN, 0, len(views))
			for _, v := range views {
				wans = append(wans, xray.RouterWAN{
					Slot: v.Slot, UplinkIndex: v.UplinkIndex, Label: v.Label,
				})
			}
			return wans
		})
	}
	log.Info("In-process node agent started (single-binary mode)")

	// SSH and MTProto tunnels: pushed to each node after its xray config.
	var tunnelFirewall func(context.Context) error
	if networkUC != nil {
		tunnelFirewall = networkUC.OnInboundsChanged
	}
	// declared first: the MTProto node manager and the core usecase need each other
	var coreUC *coreUsecase.CoreUsecase
	mtprotoMgr, mtprotoAllow, err := newMTProtoNodes(mtprotoDeps{
		DB: db, Nodes: uc.Node, Network: networkUC, Bus: eventBus, HTTP: httpFactory,
		Setting: func(ctx context.Context, key string) (string, error) {
			s, err := repos.Setting.GetByKey(ctx, key)
			if err != nil {
				return "", err
			}
			return s.Value, nil
		},
		Reconcile: func(ctx context.Context, nodeID uint) error {
			if coreUC == nil {
				return nil
			}
			return coreUC.ReconcileNodeByID(ctx, nodeID)
		},
		RouterMode: cfg.Router.Enabled, Log: log,
	})
	if err != nil {
		log.WithError(err).Fatal("Failed to build the MTProto node manager")
	}
	mtprotoAPI := newMTProtoAPI(mtprotoDeps{DB: db, Nodes: uc.Node, HTTP: httpFactory, Log: log}, repos.Node, mtprotoMgr, mtprotoAllow)
	tunnels := tunnelDeps{
		MTProto: mtprotoMgr,
		Repo:    coreRepo, Subs: repos.Subscription, Nodes: repos.Node, NodeUC: uc.Node, Embedded: embeddedSrv,
		BaseURL: cfg.App.BaseURL, PanelPort: cfg.App.Port, HubPort: hubPort,
		InboundsChanged: tunnelFirewall, RouterMode: cfg.Router.Enabled, Log: log,
	}
	coreUC = newTunnelUsecase(tunnels)
	tunnelInstaller := newTunnelInstaller(tunnels)
	uc.Node.SetCoreReconcileFn(func(ctx context.Context, client agent.NodeClient, nodeID uint) error {
		return coreUC.ReconcileNode(ctx, client, nodeID)
	})
	uc.Node.SetCorePortLister(coreUC.NodePorts)
	uc.Node.SetTunnelSocksLister(func(ctx context.Context, nodeID uint) ([]xray.TunnelSocks, error) {
		list, err := coreUC.XraySocks(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		out := make([]xray.TunnelSocks, len(list))
		for i, t := range list {
			out[i] = xray.TunnelSocks{InboundID: t.InboundID, Port: t.Port}
		}
		return out, nil
	})
	// Per-tunnel usage: the stats fold attributes tunnel bytes to each
	// subscription's tunnels.
	uc.Node.SetCoreUsageProvider(coreRepo)
	// Tunnels follow their subscription: assignment, pause, expiry, quota,
	// renewal, reset, regenerate and delete.
	uc.Subscription.SetCoreAssigner(coreUC)

	// Reverse-mode nodes arrive through the hub; its listener starts later,
	// only when the hub is on.
	reverseRegistry := hub.NewRegistry()
	hubServer := hub.NewHubServer(reverseRegistry, func(ctx context.Context, uuid string) (uint, string, bool) {
		node, err := repos.Node.GetNodeByUUID(ctx, uuid)
		if err != nil || node == nil {
			return 0, "", false
		}
		return node.ID, node.ConnectMode, true
	})
	if uc.CertWatch != nil {
		w := uc.CertWatch
		hubServer.SetCertGate(func(nodeID uint, leaf *x509.Certificate) error {
			return w.Gate(nodeID, "reverse", leaf)
		})
	}
	uc.Node.SetReverseRegistry(reverseRegistry)
	uc.Node.SetHubServer(hubServer)

	// Every setter-injected node dependency is in place by now. A missed setter
	// leaves a nil field whose consumer degrades quietly — audit logging for
	// node nuke/wipe is nil-guarded to a no-op — so fail here instead.
	if err := nodeUC.AssertWired(uc.Node); err != nil {
		log.WithError(err).Fatal("Node usecase wiring incomplete")
	}

	// Exactly one built-in node; extra "local" rows are demoted first, so the
	// unique index below can be created on older databases.
	localNode, err := uc.Node.EnsureLocalNode(context.Background())
	if err != nil {
		log.WithError(err).Fatal("Failed to set up the built-in node")
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ux_nodes_one_local
		  ON nodes (connect_mode) WHERE connect_mode = 'local' AND deleted_at IS NULL`).Error; err != nil {
		log.WithError(err).Warn("Failed to create ux_nodes_one_local")
	}
	if cfg.Router.Enabled && localNode.ID != 1 {
		log.WithField("node_id", localNode.ID).Fatal("Router mode needs the built-in node to be node 1 (router tables are pinned to it)")
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

	// What the settings page reports as in effect (GET /settings/status).
	runtimeStatus := runtimestatus.New(Version, cfg.App.Port)
	runtimeStatus.SetProxyConfigured(httpFactory.IsProxyConfigured)
	runtimeStatus.SetMetricsEndpoint(cfg.Metrics.Enabled)
	runtimeStatus.SetBot(func(b *runtimestatus.Bot) { b.TokenSet = cfg.Telegram.BotToken != "" })
	if all, err := repos.Setting.GetAll(context.Background()); err == nil {
		boot := make(map[string]string)
		for _, s := range all {
			// app_port and the bot token come from .env; nothing reads their rows
			if s.RequiresRestart && s.Key != "app_port" && s.Key != "telegram_bot_token" {
				boot[s.Key] = s.Value
			}
		}
		runtimeStatus.SetBootValues(boot)
	}

	// Settings drive the health prober; reload without a restart.
	if networkUC != nil {
		loadHealthCfg := func() {
			networkUC.SetHealthConfig(networkUsecase.ParseHealthConfig(func(k string) (string, error) {
				return uc.Setting.GetByKey(context.Background(), k)
			}))
		}
		loadHealthCfg()
		uc.Setting.SetOnRouterHealthChange(loadHealthCfg)
	}

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

	// Telegram bot. telegramBotRef publishes it to consumers built before it
	// exists: construction reaches api.telegram.org, so it can only succeed
	// once the outbound path is up, which may be minutes after boot.
	telegramBotRef := &botHolder{}
	// The transport wrapper is what the monitor notifier and shutdown need.
	var transportBotRef atomic.Pointer[telegramTransport.Bot]
	backupSvc := adminUC.NewBackupService(db, cfg.Database, cfg.App.BackupDir)
	// Scheduled backups run with the bot off too (dump + rotation); the bot
	// becomes the sender once it exists.
	backupRunner := adminUC.NewScheduledBackupRunner(backupSvc, repos.Setting, nil, cfg.Admin.InitialAdminIDs)
	installRoot, _ := os.Getwd()
	panelUpdates := updateUC.New(updateUC.Deps{
		Paths:    updateUC.Paths{Root: installRoot},
		Version:  Version,
		Settings: repos.Setting,
		DB:       db,
		HTTP:     httpFactory,
		Events:   eventBus,
	})

	if cfg.Telegram.Enabled {
		runtimeStatus.SetBot(func(b *runtimestatus.Bot) {
			b.State = runtimestatus.BotStarting
		})
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
		adminTelegramHandler := adminTg.NewHandler(uc.Admin, uc.Account, uc.Subscription, userRepo.NewUserRepository(db), stateManager, nil, uc.Audit, uc.Setting, uc.Node, repos.Provisioning, db) // Bot set later
		adminTelegramHandler.SetBackupRunner(backupRunner)

		// wireBot binds the finished bot into everything that needs it and
		// starts polling. It runs on the supervisor goroutine.
		wireBot := func(bot *telegramTransport.Bot) {
			accountTg.RegisterHandlers(bot.GetBot(), accountTelegramHandler, adminMW.RequireAdmin)
			adminTelegramHandler.SetBot(bot.GetBot())
			backupRunner.SetSender(bot)
			uc.Admin.SetBot(bot.GetBot())
			subTelegramHandler.SetMaintenanceUC(uc.Maintenance)
			setMaintenanceBroadcastBot(bot.GetBot())
			transportBotRef.Store(bot)
			telegramBotRef.Set(bot.GetBot())
			go bot.Start()
			log.Info("Telegram bot started")
		}

		// Supervise construction instead of giving up after a fixed window: a
		// proxy or outbound outage at boot used to leave Telegram dead for the
		// whole process. The panel keeps serving while this retries.
		go func() {
			delay := botInitRetryBase
			for attempt := 1; ; attempt++ {
				viaFactory := httpFactory.IsProxyConfigured()
				bot, err := telegramTransport.NewBot(
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
				if err == nil {
					if attempt > 1 {
						log.WithField("attempts", attempt).Info("Telegram bot initialized after retries")
					}
					runtimeStatus.SetBot(func(b *runtimestatus.Bot) {
						b.State, b.Since, b.Error, b.ViaFactory, b.Legacy = runtimestatus.BotRunning, time.Now(), "", viaFactory, ""
						if me := bot.GetBot().Me; me != nil {
							b.Username = me.Username
						}
						if !viaFactory && cfg.Telegram.Proxy.Enabled {
							b.Legacy = fmt.Sprintf("%s:%d", cfg.Telegram.Proxy.Host, cfg.Telegram.Proxy.Port)
						}
					})
					wireBot(bot)
					return
				}
				// telebot errors quote the request URL, and the token is part of it
				msg := err.Error()
				if t := cfg.Telegram.BotToken; t != "" {
					msg = strings.ReplaceAll(msg, t, "<token>")
				}
				runtimeStatus.SetBot(func(b *runtimestatus.Bot) { b.Error = msg })
				if errors.Is(err, telebot.ErrUnauthorized) {
					runtimeStatus.SetBot(func(b *runtimestatus.Bot) { b.State = runtimestatus.BotRejected })
					log.WithError(err).Error("Telegram bot token rejected (401 Unauthorized); giving up until the token is fixed and the panel restarted")
					return
				}
				log.WithError(err).WithFields(map[string]interface{}{
					"attempt":     attempt,
					"retry_after": delay.String(),
				}).Warn("Telegram bot init failed; retrying in the background")
				select {
				case <-bgCtx.Done():
					return
				case <-time.After(delay):
				}
				if delay < botInitRetryMax {
					delay = min(delay*2, botInitRetryMax)
				}
			}
		}()
	} else {
		log.Info("Telegram bot disabled (TELEGRAM_ENABLED=false)")
	}

	// Admin Telegram IDs (config + DB)
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

	// alerts and backups go to ADMIN_IDS only (Bot.NotifyAdmin)
	runtimeStatus.SetAdmin(cfg.Admin.Username, len(cfg.Admin.InitialAdminIDs))

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
		UsedTokenRepo:                 repos.UsedToken,
		BackupRunner:                  backupRunner,
		PanelUpdater:                  panelUpdates,
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
		nil, // the bot arrives through telegramBotRef, maybe after boot
		schedulerCfg,
	)
	telegramBotRef.OnReady(expirationScheduler.SetBot)
	expirationScheduler.Start()
	log.Info("Expiration scheduler started")

	// Wire scheduler's on-demand retention method into admin usecase.
	// CleanupSummary → map[string]int64.
	uc.Admin.SetRetentionCleanupRunner(func(ctx context.Context) map[string]int64 {
		return expirationScheduler.RunRetentionNow(ctx)
	})

	// --- Connectivity history ---
	// Seed the transition columns from any existing uptime events so nodes
	// that flapped before this shipped don't read as "never disconnected".
	if err := repos.Node.BackfillNodeTransitionColumns(bgCtx); err != nil {
		log.WithError(err).Warn("Failed to backfill node transition columns")
	}
	// Bracket this run. The complement of these rows is time in which nothing
	// observed the nodes — the built-in node counts it as downtime (its xray
	// stops with the panel), remote nodes as not observed.
	hubLifecycle := nodeUC.OpenHubLifecycle(bgCtx, repos.Node, Version)

	// Monitor
	// Resolved per send: the bot may still be coming up.
	botNotifier := notifPkg.Notifier(lazyBotNotifier{ref: &transportBotRef})
	monitorService := monitor.NewMonitorService(repos.Node, uc.Node, botNotifier, 10*time.Second, eventBus)
	go monitorService.Start(bgCtx)
	log.Info("Server Monitor Service started")

	// Online Users Cache Cleanup
	cache.StartCleanup(bgCtx)

	// Heartbeat Manager
	uc.Node.StartHeartbeats(bgCtx)
	log.Info("Heartbeat Manager started")

	// MTProto (lite) nodes: one link per node, driven for as long as the panel runs.
	if err := mtprotoMgr.Start(bgCtx); err != nil {
		log.WithError(err).Warn("MTProto node manager did not start")
	}
	go mtprotoAllow.Run(bgCtx)

	// --- Reverse-mode node hub ---
	heartbeatMgr := uc.Node.GetHeartbeatManager()
	reverseRegistry.SetCallbacks(
		func(nodeID uint) { heartbeatMgr.HandleReverseConnect(nodeID) },
		func(nodeID uint) { heartbeatMgr.HandleReverseDisconnect(nodeID) },
	)
	hubServer.SetHeartbeatCallback(func(nodeID uint, pong *hubpb.HeartbeatPong) {
		heartbeatMgr.HandleReversePong(nodeID, pong)
	})
	if hubEnabled {
		hubListenAddr := fmt.Sprintf(":%d", hubPort)
		hubCreds := &hubTLS{certUC: uc.Certificate, settings: uc.Setting, watch: uc.CertWatch, baseURL: cfg.App.BaseURL, log: log}
		runtimeStatus.SetReverseHub(true, hubPort, reverseRegistry.ConnectedCount)
		go func() {
			if err := hubServer.ListenAndServe(hubListenAddr, hubCreds.Config()); err != nil {
				runtimeStatus.SetReverseHub(false, hubPort, nil)
				log.WithError(err).Error("Hub server failed")
			}
		}()
		log.WithField("address", hubListenAddr).Info("Reverse-mode node hub started")
	} else {
		runtimeStatus.SetReverseHub(false, hubPort, nil)
		log.Info("Reverse-mode node hub is off")
	}

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

	// The panel's own certificate, served when self-signed HTTPS is chosen
	// and neither certificate files nor Let's Encrypt are set.
	selfSigned := newSelfSignedManager(repos.Setting, uc.Setting, uc.Audit)
	uc.Node.SetPanelHTTPS(runtimeStatus)

	// HTTP server
	server := httpTransport.NewServer(httpTransport.ServerDeps{
		Core: httpTransport.CoreDeps{
			UserUsecase:   uc.User,
			SubUsecase:    uc.Subscription,
			ChatUsecase:   uc.Chat,
			WGDevice:      uc.WGDevice,
			Tunnels:       coreUC,
			TunnelInstall: tunnelInstaller,
		},
		Admin: httpTransport.AdminDeps{
			MTProto:            mtprotoAPI,
			NodePools:          uc.NodePools,
			AdminUsecase:       uc.Admin,
			AccountUsecase:     uc.Account,
			NodeUsecase:        uc.Node,
			SNIUsecase:         uc.SNI,
			SettingUsecase:     uc.Setting,
			CertificateUsecase: uc.Certificate,
			AuditUsecase:       uc.Audit,
			BackupService:      backupSvc,
			PanelUpdates:       panelUpdates,
			AlertUsecase:       uc.Alert,
			MaintenanceUsecase: uc.Maintenance,
			NetworkUsecase:     networkUC,
			RouterMode:         cfg.Router.Enabled,
			UserProfileUsecase: userProfileUC.New(userProfileUC.Deps{
				Users:         repos.User,
				Subscriptions: repos.Subscription,
				SubIPs:        repos.SubscriptionIP,
				Accounts:      repos.Account,
				Notifications: repos.Notification,
				Audit:         repos.Audit,
				Hourly:        repos.Node,
			}),
		},
		Infra: httpTransport.InfraDeps{
			DB:                db,
			JWTManager:        jwtManager,
			TokenManager:      tokenManager,
			EventBus:          eventBus,
			ACMEManager:       certManager,
			ShutdownCtx:       bgCtx,
			HTTPClientFactory: httpFactory,
			RuntimeStatus:     runtimeStatus,
			SelfSigned:        selfSigned,
		},
		Config: httpTransport.ConfigDeps{
			AdminConfig:      cfg.Admin,
			AppConfig:        cfg.App,
			DatabaseConfig:   cfg.Database,
			MetricsConfig:    cfg.Metrics,
			TelegramBotToken: cfg.Telegram.BotToken,
			WebFS:            WebFS,
			Version:          Version,
		},
		Repos: httpTransport.RepoDeps{
			NodeRepository:      repos.Node,
			UsedTokenRepository: repos.UsedToken,
			UserRepository:      repos.User,
			SubRepository:       repos.Subscription,
		},
	})
	// A restored database may have just had its deployment settings reseeded.
	// Resolve manual certificate paths again against the environment baseline.
	cfg.App.TLSCertFile = envCfg.App.TLSCertFile
	cfg.App.TLSKeyFile = envCfg.App.TLSKeyFile
	cfg.App.TLSSelfSigned = envCfg.App.TLSSelfSigned
	if err := resolveStartupTLS(cfg, getStartupSetting); err != nil {
		log.WithError(err).Fatal("Cannot resolve panel TLS settings")
	}
	tlsCfg, err := prepareServerTLS(bgCtx, cfg, certManager)
	if err != nil {
		log.WithError(err).Fatal("Cannot initialize requested TLS; refusing to start plain HTTP")
	}
	httpsStatus := serverTLSStatus(cfg, tlsCfg)
	var plainOK func(*http.Request) bool
	if tlsCfg == nil && cfg.App.TLSSelfSigned {
		// Unlike the other modes, a self-signed failure falls back to plain
		// HTTP: it is the default on router installs, and must never lock
		// anyone out of their router.
		if err := selfSigned.Ensure(bgCtx); err != nil {
			log.WithError(err).Error("Self-signed HTTPS unavailable; serving plain HTTP")
			httpsStatus.Error = "The self-signed certificate couldn't be set up: " + err.Error()
		} else {
			tlsCfg, plainOK = selfSigned.TLSConfig(), server.SelfSignedPlainHTTP
			httpsStatus = runtimestatus.HTTPS{Mode: "self-signed", Cert: selfSigned.Leaf}
			selfSigned.StartRenewal(bgCtx)
		}
	}
	runtimeStatus.SetHTTPS(httpsStatus)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)
		if tlsCfg != nil {
			log.WithField("address", addr).Info("HTTPS server starting")
			if err := server.RunTLS(addr, tlsCfg, plainOK); err != nil && err != http.ErrServerClosed {
				log.WithError(err).Fatal("HTTPS server failed")
			}
			return
		}

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
	bgCancel() // stops monitor service
	// Close the lifecycle row before the DB goes away: a clean stop gives the
	// gap an exact end instead of one inferred at the next boot.
	hubLifecycle.Close(shutdownCtx)
	// Scans write to the DB; stop them before it closes.
	uc.NodePools.Stop()
	if networkUC != nil {
		// The mapper hands its leases back on the way out. Waiting is the
		// difference between a released port and one the upstream router keeps
		// forwarding here for the next two hours.
		relCtx, relCancel := context.WithTimeout(context.Background(), 5*time.Second)
		networkUC.StopPortMap(relCtx)
		relCancel()
	}
	hubServer.Stop()
	mtprotoMgr.Stop()
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
	if b := transportBotRef.Load(); b != nil {
		b.Stop()
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

// Telegram bot init retry policy: the supervisor in runServe keeps retrying
// with this backoff so the bot joins whenever the outbound path heals.
const (
	botInitRetryBase = 5 * time.Second
	botInitRetryMax  = 2 * time.Minute
)

// lazyBotNotifier forwards admin notifications to the Telegram bot once it
// exists; consumers built at boot can't hold the instance directly.
type lazyBotNotifier struct {
	ref *atomic.Pointer[telegramTransport.Bot]
}

func (n lazyBotNotifier) NotifyAdmin(message string) error {
	b := n.ref.Load()
	if b == nil {
		return nil
	}
	return b.NotifyAdmin(message)
}

type routerModeDeps struct {
	DB     *gorm.DB
	NftMgr *nft.Manager
	Agent  agent.InterfaceLister
	Bus    *events.EventBus
	// NetEvents is the flow page's history of what already happened.
	NetEvents *events.Recorder
	Log       *logrus.Logger
	// PanelPort is what filter_in must keep open, or arming it locks the
	// operator out of the box they just firewalled.
	PanelPort int
	// HubPort is the reverse-mode node hub, 0 when the hub is off.
	HubPort int
	// NodeRepo supplies the inbound rows filter_in derives its accepts from.
	NodeRepo inboundLister
	// Tunnels supplies the SSH/MTProto listeners filter_in opens as well.
	Tunnels tunnelLister
	// LocalNodeID names the built-in node whose inbounds filter_in opens.
	LocalNodeID func() uint
	// HTTPFactory routes the domestic address-list refresh like every other
	// outbound the panel makes.
	HTTPFactory *httpclient.Factory
	// PoolSettings persists how traffic uses the VPN pool.
	PoolSettings networkUsecase.PoolSettings
}

// The strategy lives in the settings table with the other router_ keys.
type poolSettings struct{ uc settingDomain.SettingUsecase }

func (p poolSettings) Set(ctx context.Context, key, value string) error {
	return p.uc.UpdateMany(ctx, []*settingDomain.Setting{{
		Key: key, Value: value, Type: "string", Category: "router",
	}})
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
		IfRepo:       networkRepo.NewInterfaceRepository(deps.DB),
		GroupRepo:    networkRepo.NewGroupRepository(deps.DB),
		ApplyRepo:    applyRepo,
		LANRepo:      networkRepo.NewLANRepository(deps.DB),
		PFRepo:       networkRepo.NewPortForwardRepository(deps.DB),
		WifiRepo:     networkRepo.NewWifiRepository(deps.DB),
		DeviceLabels: networkRepo.NewDeviceLabelRepository(deps.DB),
		VPNRepo:      networkRepo.NewVPNRepository(deps.DB),
		PoolSettings: deps.PoolSettings,
		PanelPort:    deps.PanelPort,
		HubPort:      deps.HubPort,
		Backend:      backend,
		Nft:          deps.NftMgr,
		Agent:        deps.Agent,
		Paths:        paths,
		RouterMode:   true,
		EventBus:     deps.Bus,
		Events:       deps.NetEvents,
		Inbounds:     inboundSource{repo: deps.NodeRepo, tunnels: deps.Tunnels, localNodeID: deps.LocalNodeID},
		PortMapRepo:  networkRepo.NewPortMapRepository(deps.DB),
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
	uc.StartPortMapLoop(ctx, 0)
	deps.Log.Info("Router mode active")
	return uc, nil
}

// resolveHub reads whether the reverse-mode hub runs and on which port:
// HUB_ENABLED or the hub_enabled setting turns it on, the hub_port setting
// overrides HUB_LISTEN_ADDR.
func resolveHub(cfg *config.Config, settings settingDomain.SettingUsecase) (bool, int) {
	enabled, port := cfg.App.HubEnabled, cfg.App.HubPort
	if v, err := settings.GetByKey(context.Background(), "hub_enabled"); err == nil && v == "true" {
		enabled = true
	}
	if v, err := settings.GetByKey(context.Background(), "hub_port"); err == nil && v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}
	return enabled, port
}
