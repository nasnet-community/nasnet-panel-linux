package http

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/config"
	accessHistoryHttp "github.com/nasnet-community/nasnet-panel-linux/internal/access_history/delivery/http"
	accessHistoryUC "github.com/nasnet-community/nasnet-panel-linux/internal/access_history/usecase"
	accountHttp "github.com/nasnet-community/nasnet-panel-linux/internal/account/delivery/http"
	accountUC "github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
	adminHttp "github.com/nasnet-community/nasnet-panel-linux/internal/admin/delivery/http"
	adminUC "github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	alertHttp "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/delivery/http"
	alertUC "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/usecase"
	auditHttp "github.com/nasnet-community/nasnet-panel-linux/internal/audit/delivery/http"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	authHttp "github.com/nasnet-community/nasnet-panel-linux/internal/auth/delivery/http"
	authMiddleware "github.com/nasnet-community/nasnet-panel-linux/internal/auth/middleware"
	chatHttp "github.com/nasnet-community/nasnet-panel-linux/internal/chat/delivery/http"
	chatDomain "github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	eventsHttp "github.com/nasnet-community/nasnet-panel-linux/internal/events/delivery/http"
	mntHTTP "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/delivery/http"
	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	networkHttp "github.com/nasnet-community/nasnet-panel-linux/internal/network/delivery/http"
	networkUC "github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	nodeHttp "github.com/nasnet-community/nasnet-panel-linux/internal/node/delivery/http"
	nodeHandler "github.com/nasnet-community/nasnet-panel-linux/internal/node/handler"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUCPkg "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	settingHttp "github.com/nasnet-community/nasnet-panel-linux/internal/setting/delivery/http"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	sniHttp "github.com/nasnet-community/nasnet-panel-linux/internal/sni/delivery/http"
	sniUC "github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
	subHttp "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/delivery/http"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userHttp "github.com/nasnet-community/nasnet-panel-linux/internal/user/delivery/http"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	userUCPkg "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	xrayHandler "github.com/nasnet-community/nasnet-panel-linux/internal/xray/handler"
	xrayUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jwt"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/maintenance"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/metrics"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

type Server struct {
	engine          *gin.Engine
	httpServer      *http.Server
	chatRateLimiter *chatHttp.ChatRateLimiter
}

// CoreDeps holds the core business usecases (required)
type CoreDeps struct {
	UserUsecase userUCPkg.UserUsecase
	SubUsecase  subUC.SubscriptionUsecase
	ChatUsecase chatDomain.ChatUsecase
	WGDevice    wireguardUC.DeviceUsecase
}

// AdminDeps holds admin-panel and extended usecases (optional)
type AdminDeps struct {
	AdminUsecase       adminUC.AdminUsecase
	AccountUsecase     accountUC.AccountUsecase
	NodeUsecase        nodeUCPkg.NodeUsecase
	SNIUsecase         sniUC.SNIUsecase
	SettingUsecase     settingDomain.SettingUsecase
	CertificateUsecase nodeUCPkg.CertificateUsecase
	AuditUsecase       auditDomain.AuditLogUsecase
	BackupService      *adminUC.BackupService
	AlertUsecase       alertUC.AlertUsecase
	MaintenanceUsecase mntUC.Usecase
	NetworkUsecase     networkUC.NetworkUsecase
	RouterMode         bool
}

// InfraDeps holds infrastructure-level dependencies
type InfraDeps struct {
	DB                *gorm.DB
	JWTManager        *jwt.Manager
	TokenManager      *auth.TokenManager
	EventBus          *events.EventBus
	ACMEManager       *acme.CertManager
	ShutdownCtx       context.Context
	HTTPClientFactory *httpclient.Factory
}

// ConfigDeps holds configuration values
type ConfigDeps struct {
	AdminConfig      config.AdminConfig
	AppConfig        config.AppConfig
	DatabaseConfig   config.DatabaseConfig
	MetricsConfig    config.MetricsConfig
	TelegramBotToken string
	WebFS            embed.FS // embedded SPA filesystem from embed.go
}

// RepoDeps holds repository dependencies
type RepoDeps struct {
	NodeRepository nodeRepo.NodeRepository
	UserRepository userRepo.UserRepository
	SubRepository  subRepo.SubscriptionRepository
}

// ServerDeps holds all dependencies for the HTTP server
type ServerDeps struct {
	Core   CoreDeps
	Admin  AdminDeps
	Infra  InfraDeps
	Config ConfigDeps
	Repos  RepoDeps
}

// NewServer creates a new HTTP server with all handlers registered
func NewServer(deps ServerDeps) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	basePath := deps.Config.AppConfig.PanelBasePath

	// Middlewares
	engine.Use(gin.Recovery())
	engine.Use(httpMiddleware.RequestIDMiddleware())
	engine.Use(LoggerMiddleware())
	engine.Use(CORSMiddleware(deps.Config.AppConfig.CORSOrigins))
	engine.Use(maintenanceMiddleware(basePath))

	// Prometheus metrics middleware and endpoint
	if deps.Config.MetricsConfig.Enabled && metrics.Registry != nil {
		engine.Use(metrics.GinMiddleware())
		metricsPath := deps.Config.MetricsConfig.Path
		if metricsPath == "" {
			metricsPath = "/metrics"
		}
		metricsHandler := gin.WrapH(promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))
		if deps.Config.MetricsConfig.Username != "" && deps.Config.MetricsConfig.Password != "" {
			metricsGroup := engine.Group(metricsPath, gin.BasicAuth(gin.Accounts{
				deps.Config.MetricsConfig.Username: deps.Config.MetricsConfig.Password,
			}))
			metricsGroup.GET("", metricsHandler)
		} else {
			engine.GET(metricsPath, metricsHandler)
		}
	}

	// Health check (liveness)
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Readiness check (checks DB connectivity)
	engine.GET("/health/ready", func(c *gin.Context) {
		if deps.Infra.DB != nil {
			sqlDB, err := deps.Infra.DB.DB()
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": "db connection unavailable"})
				return
			}
			if err := sqlDB.Ping(); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": "db ping failed"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	// ACME Challenge Handler (Let's Encrypt)
	if deps.Infra.ACMEManager != nil {
		engine.GET("/.well-known/acme-challenge/:token", func(c *gin.Context) {
			token := c.Param("token")
			if key, ok := deps.Infra.ACMEManager.GetHTTPChallengeKey(token); ok {
				c.String(http.StatusOK, key)
			} else {
				c.String(http.StatusNotFound, "Challenge not found")
			}
		})
	}

	// Public Routes (No Auth)
	// Used for Subscription Links (e.g., /sub/uuid)
	shutdownCtx := deps.Infra.ShutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	var subAuthSecret string
	if deps.Infra.JWTManager != nil {
		subAuthSecret = deps.Infra.JWTManager.GetSecretKey()
	}
	subHandler := subHttp.NewHandler(deps.Core.SubUsecase, deps.Admin.AccountUsecase, deps.Config.AppConfig.BaseURL, deps.Config.AppConfig.SubPanelURL, shutdownCtx, deps.Admin.SettingUsecase, subAuthSecret)
	// Enable WireGuard device management on the public panel (same UUID+password auth).
	subHandler.SetDeviceUsecase(deps.Core.WGDevice)

	// Build the access-history usecase once for the admin route. The sub-panel
	// no longer exposes access history.
	var accessHistoryUsecase accessHistoryUC.Usecase
	if deps.Repos.NodeRepository != nil && deps.Admin.AccountUsecase != nil && deps.Admin.SettingUsecase != nil {
		accessHistoryUsecase = accessHistoryUC.New(
			deps.Repos.NodeRepository,
			deps.Admin.AccountUsecase,
			deps.Admin.SettingUsecase,
			nil,
		)
	}

	panel := engine.Group(basePath)

	// Subscription public routes live at the root (not behind basePath)
	// so that /sub/:uuid links remain stable regardless of panel base path.
	root := engine.Group("")

	// Maintenance mode: attach the write-guard BEFORE registering public
	// write routes so gin actually applies it. The guard is a pass-through
	// for GET/HEAD/OPTIONS, so it's safe to mount on the entire root group.
	// Admin routes live under api.Group("/api/v1/admin"), a separate subtree,
	// so they are NOT affected by this guard.
	if deps.Admin.MaintenanceUsecase != nil {
		guard := httpMiddleware.NewMaintenanceWriteGuard(deps.Admin.MaintenanceUsecase, deps.Core.SubUsecase)
		root.Use(guard)
	}

	subHandler.RegisterPublicRoutes(root)

	// Maintenance mode: public status endpoint (GET only).
	var mntHandler *mntHTTP.Handler
	if deps.Admin.MaintenanceUsecase != nil {
		mntHandler = mntHTTP.NewHandler(deps.Admin.MaintenanceUsecase, deps.Core.SubUsecase)
		mntHandler.RegisterPublicRoutes(root.Group("/api/v1/public"))
	}

	// Chat handlers (public REST + WebSocket)
	chatHandler := chatHttp.NewHandler(deps.Core.ChatUsecase, deps.Core.SubUsecase)
	chatRateLimiter := chatHttp.NewChatRateLimiter()
	subChatGroup := root.Group("/api/v1/public/sub/:uuid")
	if deps.Admin.MaintenanceUsecase != nil {
		guard := httpMiddleware.NewMaintenanceWriteGuard(deps.Admin.MaintenanceUsecase, deps.Core.SubUsecase)
		subChatGroup.Use(guard)
	}
	chatHandler.RegisterPublicRoutes(subChatGroup, chatRateLimiter)

	// Chat WebSocket — user (widget)
	chatWSHandler := chatHttp.NewWSHandler(
		deps.Core.ChatUsecase,
		deps.Infra.EventBus,
		func(ctx context.Context, configID string) (uint, error) {
			sub, err := deps.Core.SubUsecase.GetByConfigID(ctx, configID)
			if err != nil {
				return 0, err
			}
			return sub.ID, nil
		},
		chatRateLimiter,
	)
	chatWSHandler.RegisterRoutes(subChatGroup)

	// Chat WebSocket — admin (per conversation)
	chatWSAdminHandler := chatHttp.NewWSAdminHandler(deps.Core.ChatUsecase, deps.Infra.EventBus, chatRateLimiter)

	// API routes base
	api := panel.Group("/api/v1")

	// Create JWT middleware if manager is provided
	var jwtMW *authMiddleware.JWTMiddleware
	if deps.Infra.JWTManager != nil {
		jwtMW = authMiddleware.NewJWTMiddleware(deps.Infra.JWTManager)

		// Register auth routes (public - no auth required)
		authHandler := authHttp.NewAuthHandler(deps.Core.UserUsecase, deps.Infra.JWTManager, deps.Admin.SettingUsecase, deps.Admin.AuditUsecase, deps.Config.TelegramBotToken, deps.Config.AdminConfig)
		authHandler.RegisterRoutes(api)
	}

	// ============================================================
	// PUBLIC API ROUTES (No authentication required)
	// ============================================================
	publicAPI := api.Group("")
	_ = publicAPI

	// ============================================================
	// PROTECTED API ROUTES (Authentication required)
	// ============================================================
	protectedAPI := api.Group("")
	if jwtMW != nil {
		protectedAPI.Use(jwtMW.RequireAuth())
	}
	{
		// User endpoints
		userHandler := userHttp.NewHandler(deps.Core.UserUsecase)
		userHandler.RegisterRoutes(protectedAPI)

		// Subscription endpoints
		subHandler.RegisterRoutes(protectedAPI)
	}

	// ============================================================
	// ADMIN API ROUTES (Admin authentication required)
	// ============================================================
	adminAPI := api.Group("")
	if jwtMW != nil {
		adminAPI.Use(jwtMW.RequireAuth())
		adminAPI.Use(jwtMW.RequireAdmin())
	}
	{
		// Admin dashboard and management
		if deps.Admin.AdminUsecase != nil {
			adminHandler := adminHttp.NewHandler(deps.Admin.AdminUsecase, deps.Config.AppConfig.BaseURL, deps.Admin.AuditUsecase, deps.Config.TelegramBotToken, deps.Admin.SettingUsecase)
			adminHandler.SetHTTPClientFactory(deps.Infra.HTTPClientFactory)
			adminHandler.RegisterRoutes(adminAPI)
		}

		// Audit log viewer
		if deps.Admin.AuditUsecase != nil {
			auditHandler := auditHttp.NewHandler(deps.Admin.AuditUsecase)
			auditHandler.RegisterRoutes(adminAPI)
		}

		// Data export
		if deps.Repos.UserRepository != nil && deps.Repos.SubRepository != nil {
			exportHandler := adminHttp.NewExportHandler(deps.Repos.UserRepository, deps.Repos.SubRepository)
			exportHandler.RegisterRoutes(adminAPI)
		}

		// Database backup
		if deps.Admin.BackupService != nil {
			backupHandler := adminHttp.NewBackupHandler(deps.Admin.BackupService, deps.Admin.AuditUsecase, deps.Admin.SettingUsecase)
			backupHandler.RegisterRoutes(adminAPI)
		}

		// Account management (Xray accounts)
		if deps.Admin.AccountUsecase != nil {
			accountHandler := accountHttp.NewHandler(deps.Admin.AccountUsecase)
			accountHandler.RegisterRoutes(adminAPI)
		}

		// Per-subscription access-log history. Shares usecase with the
		// sub-panel path so retention + concurrency agree. Audited.
		if accessHistoryUsecase != nil {
			ahHandler := accessHistoryHttp.NewHandler(accessHistoryUsecase)
			if deps.Admin.AuditUsecase != nil {
				ahHandler.SetAuditUC(deps.Admin.AuditUsecase)
			}
			ahHandler.RegisterAdminRoutes(adminAPI)
		}

		// Node management
		if deps.Admin.NodeUsecase != nil {
			nodeHandler := nodeHttp.NewHandler(deps.Admin.NodeUsecase)
			nodeHandler.SetHTTPClientFactory(deps.Infra.HTTPClientFactory)
			nodeHandler.RegisterRoutes(adminAPI)
		}

		// SNI/Certificate management
		if deps.Admin.SNIUsecase != nil {
			sniHandler := sniHttp.NewHandler(deps.Admin.SNIUsecase)
			sniHandler.RegisterRoutes(adminAPI)
		}

		// Router mode
		if deps.Admin.NetworkUsecase != nil {
			networkHttp.NewHandler(deps.Admin.NetworkUsecase, deps.Admin.RouterMode).
				RegisterRoutes(adminAPI)
		}

		// System Settings
		if deps.Admin.SettingUsecase != nil {
			settingHandler := settingHttp.NewSettingHandler(deps.Admin.SettingUsecase)
			settingHandler.RegisterRoutes(adminAPI)
		}

		// Alert rules + events
		if deps.Admin.AlertUsecase != nil {
			alertHandler := alertHttp.NewHandler(deps.Admin.AlertUsecase)
			alertHandler.RegisterRoutes(adminAPI)
		}

		// Maintenance mode admin routes
		if mntHandler != nil {
			mntGroup := adminAPI.Group("/admin/maintenance")
			mntHandler.RegisterAdminRoutes(mntGroup)
		}

		// Admin auth routes (change password, etc.)
		if deps.Infra.JWTManager != nil {
			authHandler := authHttp.NewAuthHandler(deps.Core.UserUsecase, deps.Infra.JWTManager, deps.Admin.SettingUsecase, deps.Admin.AuditUsecase, deps.Config.TelegramBotToken, deps.Config.AdminConfig)
			authHandler.RegisterAdminRoutes(adminAPI)
		}

		// Certificate management (public & ACME certs for inbound TLS)
		if deps.Admin.CertificateUsecase != nil {
			certHandler := nodeHandler.NewCertificateHandler(deps.Admin.CertificateUsecase)
			certHandler.RegisterAdminRoutes(adminAPI)
		}

		// Xray Binary Distribution
		xrayBM := xrayUsecase.NewBinaryManager("bin/xray", deps.Infra.HTTPClientFactory)
		xrayH := xrayHandler.NewXrayHandler(xrayBM, deps.Admin.SettingUsecase, deps.Infra.TokenManager, deps.Admin.AuditUsecase)
		xrayH.RegisterPublicRoutes(publicAPI.Group("/deploy/xray"))
		xrayH.RegisterAdminRoutes(adminAPI)

		// Wire xray deps into node usecase for hub-based xray updates
		if deps.Admin.NodeUsecase != nil {
			deps.Admin.NodeUsecase.SetXrayDeps(xrayBM, deps.Infra.TokenManager, deps.Config.AppConfig.BaseURL)
			deps.Admin.NodeUsecase.SetHTTPClientFactory(deps.Infra.HTTPClientFactory)
		}

		// Wire up xray version-change hook for auto-download
		if deps.Admin.SettingUsecase != nil {
			deps.Admin.SettingUsecase.SetOnXrayVersionChange(xrayBM.PrefetchVersion)
		}

		// Startup check: ensure default xray version is cached
		go func() {
			defaultVersion := "26.2.6"
			if deps.Admin.SettingUsecase != nil {
				if v, err := deps.Admin.SettingUsecase.GetByKey(context.Background(), "xray_default_version"); err == nil && v != "" {
					defaultVersion = v
				}
				autoDownload := "true"
				if v, err := deps.Admin.SettingUsecase.GetByKey(context.Background(), "xray_auto_download"); err == nil {
					autoDownload = v
				}
				if autoDownload == "true" && !xrayBM.IsCached(defaultVersion, "amd64") {
					xrayBM.PrefetchVersion(defaultVersion)
				}
			}
		}()

		// Real-time Events (SSE streaming)
		if deps.Infra.EventBus != nil {
			eventsHandler := eventsHttp.NewHandler(deps.Infra.EventBus)
			eventsHandler.RegisterRoutes(adminAPI)
		}

		// Chat management (admin)
		chatHandler.RegisterAdminRoutes(adminAPI)
		chatWSAdminHandler.RegisterRoutes(adminAPI)

	}

	// Serve the embedded SPA (admin panel)
	runtimeConfig := fmt.Sprintf(`{"basePath":"%s","appName":"%s"}`,
		basePath, "NasNet Panel")
	spaConfig := ServeSPA(panel, engine, deps.Config.WebFS, basePath, runtimeConfig)

	// Wire SPA serving into the subscription handler so browser requests
	// to /sub/:uuid get the SPA instead of raw subscription config.
	// Use an empty basePath in the runtime config so React Router matches
	// /sub/:uuid (which lives outside the panel base path).
	subRuntimeConfig := fmt.Sprintf(`{"basePath":"","appName":"%s"}`, "NasNet Panel")
	subHandler.SetSPAServing(&subHttp.SPAConfig{
		DistFS:        spaConfig.DistFS,
		RuntimeConfig: subRuntimeConfig,
		BasePath:      spaConfig.BasePath,
	})

	return &Server{engine: engine, chatRateLimiter: chatRateLimiter}
}

func (s *Server) Run(addr string) error {
	s.httpServer = &http.Server{
		Addr:           addr,
		Handler:        s.engine,
		ReadTimeout:    5 * time.Minute,
		WriteTimeout:   5 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) RunTLS(addr string, tlsConfig *tls.Config) error {
	// Listen on a raw TCP socket so we can auto-detect TLS vs plain HTTP.
	// Plain HTTP connections get a 301 redirect to HTTPS.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	autoLn := newTLSAutoListener(ln, tlsConfig, "")

	s.httpServer = &http.Server{
		Addr:           addr,
		Handler:        s.engine,
		ReadTimeout:    5 * time.Minute,
		WriteTimeout:   5 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}
	return s.httpServer.Serve(autoLn)
}

func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpServer.Shutdown(ctx)
	// After the HTTP server has stopped accepting requests, drain
	// auxiliary background goroutines we own. The chat rate limiter's
	// cleanup goroutine is the only one wired through this path today.
	if s.chatRateLimiter != nil {
		s.chatRateLimiter.Shutdown()
	}
	return err
}

func LoggerMiddleware() gin.HandlerFunc {
	log := logger.GetLogger()
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		// Don't log health checks to reduce noise
		if path != "/health" && path != "/health/ready" {
			fields := map[string]interface{}{
				"status":   c.Writer.Status(),
				"method":   c.Request.Method,
				"path":     path,
				"latency":  time.Since(start),
				"clientIP": c.ClientIP(),
			}
			if reqID, exists := c.Get(httpMiddleware.RequestIDKey); exists {
				fields["request_id"] = reqID
			}
			log.WithFields(fields).Info("HTTP Request")
		}
	}
}

func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if origin != "" && len(originSet) > 0 {
			if originSet[origin] {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			// If not in allowlist, no ACAO header → browser blocks
		} else if origin != "" {
			// No allowlist configured: allow origin but WITHOUT credentials
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Base-URL, Cookie")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Set-Cookie, Content-Length, Content-Type")
		c.Writer.Header().Set("Vary", "Origin")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// maintenanceMiddleware returns 503 for all requests during database maintenance,
// except for backup/restore endpoints, health checks, and ACME challenges.
func maintenanceMiddleware(basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !maintenance.IsActive() {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		checkPath := strings.TrimPrefix(path, basePath)
		if strings.HasPrefix(checkPath, "/api/v1/admin/backups") ||
			strings.HasPrefix(path, "/health") ||
			strings.HasPrefix(path, "/.well-known/") {
			c.Next()
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   maintenance.Message(),
		})
		c.Abort()
	}
}
