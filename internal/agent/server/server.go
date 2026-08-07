package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lilendian0x00/xray-knife/v10/pkg/core"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/accesslog"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/middleware"
	agentnetif "github.com/nasnet-community/nasnet-panel-linux/internal/agent/netif"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/process"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/ssh"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/stats"
	agenttc "github.com/nasnet-community/nasnet-panel-linux/internal/agent/tc"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	agentxray "github.com/nasnet-community/nasnet-panel-linux/internal/agent/xray"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// Version information (set at build time)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Server implements the NodeAgent gRPC service
type Server struct {
	pb.UnimplementedNodeAgentServer

	cfg          *config.Config
	xrayMgr      *process.XrayManager
	sshMgr       *ssh.Manager
	statsCollect *stats.Collector
	xrayClient   *agentxray.LocalClient
	grpcServer   *grpc.Server
	listener     net.Listener

	// Cache
	xrayVersion string
	mu          sync.RWMutex

	// Certificate denylist for revoked client certificates
	denylistMu    sync.RWMutex
	deniedSerials map[string]bool

	// Traffic buffering
	trafficStore     traffic.Store
	trafficCollector *traffic.Collector

	// Bandwidth shaping (TC)
	tcManager *agenttc.Manager

	// The single writer of `table inet nasnet`
	nftManager *nft.Manager

	// Access log capture (per-subscription DNS logs)
	accessLogCollector *accesslog.Collector

	// Guards overlapping Wipe/Nuke/Uninstall calls; once set, the agent is
	// on its way to self-destruct and no further teardown RPCs should run.
	nukeInFlight atomic.Bool

	// Serializes UpdateXrayBinary so two concurrent admin-triggered updates
	// can't race on stop/backup/write/restart.
	xrayUpdateMu sync.Mutex
}

// NewServer creates a new gRPC server for the node agent
func NewServer(cfg *config.Config) (*Server, error) {
	// Create xray manager
	xrayMgr := process.NewXrayManager(process.Config{
		BinaryPath:      cfg.Xray.BinaryPath,
		ConfigPath:      cfg.Xray.ConfigPath,
		APIAddr:         cfg.Xray.APIAddr,
		DataDir:         cfg.Xray.DataDir,
		WatchdogEnabled: cfg.Process.WatchdogEnabled,
		RestartDelay:    time.Duration(cfg.Xray.RestartDelay) * time.Millisecond,
		MaxRestarts:     cfg.Xray.MaxRestarts,
		RestartWindow:   time.Duration(cfg.Xray.RestartWindow) * time.Second,
		CaptureOutput:   cfg.Process.CaptureOutput,
		BufferSize:      cfg.Process.BufferSize,
		ServiceMode:     cfg.Process.ServiceMode,
		ServiceName:     cfg.Process.ServiceName,
	})

	// Create stats collector
	statsCollect := stats.NewCollector("/")

	// Create SSH manager
	sshMgr := ssh.NewManager(ssh.Config{})

	// Create local xray client
	xrayClient := agentxray.NewLocalClient(
		cfg.Xray.APIAddr,
		time.Duration(cfg.Xray.APITimeout)*time.Second,
	)

	s := &Server{
		cfg:           cfg,
		xrayMgr:       xrayMgr,
		sshMgr:        sshMgr,
		statsCollect:  statsCollect,
		xrayClient:    xrayClient,
		deniedSerials: make(map[string]bool),
		nftManager:    nft.NewManager(nft.NewCmdApplier("")),
	}

	// Initialize bandwidth shaping (TC)
	if cfg.Bandwidth.Enabled && cfg.Bandwidth.Interface != "" {
		tcMgr, err := agenttc.NewManager(cfg.Bandwidth.Interface, cfg.Bandwidth.TotalBW, s.nftManager)
		if err != nil {
			return nil, fmt.Errorf("bandwidth manager: %w", err)
		}
		s.tcManager = tcMgr
	}

	// Initialize access log collector (per-subscription DNS logs)
	if cfg.Xray.AccessLogPath != "" {
		store := accesslog.NewStore(cfg.AccessLog.MaxPerEmail, cfg.AccessLog.MaxEmails)
		aggregator := accesslog.NewAggregator("/var/lib/nasnet-agent/accesslog-summary.json")
		s.accessLogCollector = accesslog.NewCollector(cfg.Xray.AccessLogPath, store, aggregator)
	}

	// Initialize traffic buffering
	if cfg.Traffic.BufferEnabled {
		var store traffic.Store
		fileStore, err := traffic.NewFileStore(
			cfg.Traffic.StorePath,
			cfg.Traffic.BucketDuration,
			cfg.Traffic.Retention,
			30*time.Second,
		)
		if err != nil {
			logrus.WithError(err).Warn("Failed to create file-backed traffic store, falling back to memory")
			store = traffic.NewMemoryStore(cfg.Traffic.BucketDuration, cfg.Traffic.Retention)
		} else {
			store = fileStore
		}
		s.trafficStore = store
		s.trafficCollector = traffic.NewCollector(xrayClient, store, cfg.Traffic.CollectionInterval)
	}

	return s, nil
}

// NftManager exposes the single writer of `table inet nasnet`, so router mode
// mutates the same ruleset instead of clobbering it.
func (s *Server) NftManager() *nft.Manager { return s.nftManager }

// StartBackgroundServices starts the background services (xray auto-start, TC setup,
// traffic collector, access log collector). It is called by Start() and can also be
// called independently in reverse mode before the gRPC server is set up.
func (s *Server) StartBackgroundServices(ctx context.Context) {
	// Auto-start xray if a config file exists (handles agent restart, self-update, reboot)
	if _, err := os.Stat(s.xrayMgr.ConfigPath()); err == nil {
		logrus.Info("Auto-starting xray process")
		if err := s.xrayMgr.Start(); err != nil {
			logrus.WithError(err).Warn("Failed to auto-start xray (can be started via gRPC)")
		}
	}

	// Setup TC bandwidth shaping
	if s.tcManager != nil {
		if err := s.tcManager.Setup(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to setup TC bandwidth shaping (non-fatal)")
		}
	}

	// Start traffic collector after xray
	if s.trafficCollector != nil {
		s.trafficCollector.Start()
	}

	// Start access log collector after xray
	if s.accessLogCollector != nil {
		s.accessLogCollector.Start()
	}
}

// Start begins the gRPC server
func (s *Server) Start() error {
	// Create listener
	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.cfg.ListenAddr, err)
	}
	s.listener = listener

	// Configure gRPC server options
	var opts []grpc.ServerOption

	if s.cfg.TLS.Enabled {
		tlsCreds, err := s.loadTLSCredentials()
		if err != nil {
			return fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		opts = append(opts, grpc.Creds(tlsCreds))
	}

	// Add logging middleware
	loggingMw := middleware.NewLoggingMiddleware()
	opts = append(opts,
		grpc.UnaryInterceptor(loggingMw.UnaryServerInterceptor()),
		grpc.StreamInterceptor(loggingMw.StreamServerInterceptor()),
	)

	// Create gRPC server
	// Increase max receive message size to 100MB for binary updates
	opts = append(opts, grpc.MaxRecvMsgSize(100*1024*1024))
	s.grpcServer = grpc.NewServer(opts...)
	pb.RegisterNodeAgentServer(s.grpcServer, s)

	logrus.WithField("addr", s.cfg.ListenAddr).Info("Starting gRPC server")

	s.StartBackgroundServices(context.Background())

	// Start serving (blocks)
	return s.grpcServer.Serve(listener)
}

func (s *Server) StartLocal(ctx context.Context) error {
	logrus.Info("Starting node agent in process (single bin mode, no grpc listener)")
	s.StartBackgroundServices(ctx)
	return nil
}

// Stop stops the agent server
func (s *Server) Stop(ctx context.Context) {
	// Stop traffic collector and flush store
	if s.trafficCollector != nil {
		s.trafficCollector.Stop()
	}
	if s.trafficStore != nil {
		if err := s.trafficStore.Close(); err != nil {
			logrus.WithError(err).Warn("Failed to close traffic store")
		}
	}

	// Stop access log collector
	if s.accessLogCollector != nil {
		s.accessLogCollector.Stop()
	}

	// Teardown TC bandwidth shaping
	if s.tcManager != nil {
		if err := s.tcManager.Teardown(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to teardown TC bandwidth shaping")
		}
	}

	// Stop Xray
	if s.xrayMgr != nil {
		// Use a short timeout for xray stop if context doesn't have one, or derive from it
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// extract timeout from context if possible
		deadline, ok := stopCtx.Deadline()
		timeout := 5 * time.Second
		if ok {
			timeout = time.Until(deadline)
		}

		if err := s.xrayMgr.Stop(timeout); err != nil {
			logrus.WithError(err).Error("Failed to stop xray manager")
		}
	}

	// Stop gRPC server with short timeout
	if s.grpcServer != nil {
		logrus.Info("Stopping gRPC server...")

		stopped := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(stopped)
		}()

		select {
		case <-ctx.Done():
			logrus.Warn("Context cancelled during graceful shutdown, forcing stop")
			s.grpcServer.Stop()
		case <-time.After(1 * time.Second):
			logrus.Warn("Graceful shutdown timed out, forcing stop")
			s.grpcServer.Stop()
		case <-stopped:
			logrus.Info("gRPC server stopped gracefully")
		}
	}
}

// loadTLSCredentials loads mTLS credentials for the server
func (s *Server) loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(s.cfg.TLS.ServerCert, s.cfg.TLS.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA certificate for client verification
	caCert, err := os.ReadFile(s.cfg.TLS.CACert)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			for _, chain := range verifiedChains {
				for _, cert := range chain {
					serial := cert.SerialNumber.String()
					s.denylistMu.RLock()
					denied := s.deniedSerials[serial]
					s.denylistMu.RUnlock()
					if denied {
						return fmt.Errorf("certificate with serial %s has been revoked", serial)
					}
				}
			}
			return nil
		},
	}

	return credentials.NewTLS(tlsConfig), nil
}

// ===== Lifecycle Methods =====

// GetStatus returns the current status of the node
func (s *Server) GetStatus(ctx context.Context, _ *pb.Empty) (*pb.NodeStatus, error) {
	configHash, _ := s.xrayMgr.ConfigHash()

	// Use cached version or fetch if empty
	s.mu.RLock()
	xrayVersion := s.xrayVersion
	s.mu.RUnlock()

	if xrayVersion == "" {
		xrayVersion = s.getXrayVersion()
		s.mu.Lock()
		s.xrayVersion = xrayVersion
		s.mu.Unlock()
	}

	return &pb.NodeStatus{
		XrayRunning:     s.xrayMgr.IsRunning(),
		XrayVersion:     xrayVersion,
		XrayPid:         int64(s.xrayMgr.PID()),
		UptimeSeconds:   int64(s.xrayMgr.Uptime().Seconds()),
		ConfigHash:      configHash,
		LastRestartTime: s.xrayMgr.StartTime().Unix(),
	}, nil
}

// StartXray starts the xray process
func (s *Server) StartXray(ctx context.Context, _ *pb.Empty) (*pb.CommandResponse, error) {
	if err := s.xrayMgr.Start(); err != nil {
		// Include tail logs for context on what went wrong
		tailLogs := s.xrayMgr.TailLogs(20)
		msg := fmt.Sprintf("Failed to start xray: %v", err)
		if len(tailLogs) > 0 {
			msg += "\n--- Recent logs ---\n" + strings.Join(tailLogs, "\n")
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   msg,
			ErrorCode: "START_FAILED",
		}, nil
	}

	// Wait briefly and verify process is still alive (catches immediate config crashes)
	time.Sleep(2 * time.Second)
	if !s.xrayMgr.IsRunning() {
		tailLogs := s.xrayMgr.TailLogs(30)
		msg := "Xray started but crashed immediately"
		if len(tailLogs) > 0 {
			msg += "\n--- Recent logs ---\n" + strings.Join(tailLogs, "\n")
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   msg,
			ErrorCode: "START_CRASHED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "Xray started successfully",
	}, nil
}

// StopXray stops the xray process
func (s *Server) StopXray(ctx context.Context, req *pb.StopRequest) (*pb.CommandResponse, error) {
	timeout := time.Duration(req.GracefulTimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = s.cfg.Process.GracefulTimeout
	}

	if err := s.xrayMgr.Stop(timeout); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to stop xray: %v", err),
			ErrorCode: "STOP_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "Xray stopped successfully",
	}, nil
}

// RestartXray restarts the xray process
func (s *Server) RestartXray(ctx context.Context, req *pb.RestartRequest) (*pb.CommandResponse, error) {
	timeout := time.Duration(req.GracefulTimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = s.cfg.Process.GracefulTimeout
	}

	if err := s.xrayMgr.Restart(ctx, req.ValidateConfig, timeout); err != nil {
		tailLogs := s.xrayMgr.TailLogs(20)
		msg := fmt.Sprintf("Failed to restart xray: %v", err)
		if len(tailLogs) > 0 {
			msg += "\n--- Recent logs ---\n" + strings.Join(tailLogs, "\n")
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   msg,
			ErrorCode: "RESTART_FAILED",
		}, nil
	}

	// Wait briefly and verify process is still alive (catches immediate config crashes)
	time.Sleep(2 * time.Second)
	if !s.xrayMgr.IsRunning() {
		tailLogs := s.xrayMgr.TailLogs(30)
		msg := "Xray restarted but crashed immediately"
		if len(tailLogs) > 0 {
			msg += "\n--- Recent logs ---\n" + strings.Join(tailLogs, "\n")
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   msg,
			ErrorCode: "RESTART_CRASHED",
		}, nil
	}

	// Refresh version cache after restart in case binary changed
	go func() {
		v := s.fetchXrayVersion() // Use internal fetcher
		s.mu.Lock()
		s.xrayVersion = v
		s.mu.Unlock()
	}()

	return &pb.CommandResponse{
		Success: true,
		Message: "Xray restarted successfully",
	}, nil
}

// ===== File Management Methods =====

// WriteFile writes content to a file on the node
func (s *Server) WriteFile(ctx context.Context, req *pb.FilePayload) (*pb.CommandResponse, error) {
	if req.Path == "" {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Path cannot be empty",
			ErrorCode: "INVALID_ARGUMENT",
		}, nil
	}

	// Restrict writes to the xray config dir + subdirs.
	configDir := filepath.Dir(s.xrayMgr.ConfigPath())

	// If path is relative, join with config directory
	if !filepath.IsAbs(req.Path) {
		req.Path = filepath.Join(configDir, req.Path)
	}

	cleanPath := filepath.Clean(req.Path)

	// Check if path is within config directory
	// Note: We resolve symlinks to be safe
	realConfigDir, err := filepath.EvalSymlinks(configDir)
	if err != nil {
		// If config dir doesn't exist yet (first run?), fall back to cleaned path check
		realConfigDir = configDir
	}

	if cleanPath != realConfigDir && !strings.HasPrefix(cleanPath, realConfigDir+string(os.PathSeparator)) {
		// Fallback: check against the original (non-symlink-resolved) config dir
		if cleanPath != configDir && !strings.HasPrefix(cleanPath, configDir+string(os.PathSeparator)) {
			logrus.WithField("path", req.Path).Warn("WriteFile request to path outside config directory")
			return &pb.CommandResponse{
				Success:   false,
				Message:   "Path forbidden: must be within Xray config directory",
				ErrorCode: "PERMISSION_DENIED",
			}, nil
		}
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to create directory: %v", err),
			ErrorCode: "MKDIR_FAILED",
		}, nil
	}

	// Determine permissions (default 0644)
	perm := os.FileMode(0644)
	if req.Perm != 0 {
		perm = os.FileMode(req.Perm)
	}

	// Write file
	if err := os.WriteFile(req.Path, req.Content, perm); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to write file: %v", err),
			ErrorCode: "WRITE_FAILED",
		}, nil
	}

	logrus.WithFields(logrus.Fields{
		"path": req.Path,
		"size": len(req.Content),
	}).Info("File written successfully")

	return &pb.CommandResponse{
		Success: true,
		Message: req.Path, // Return the absolute path so client knows where it was written
	}, nil
}

// ===== Configuration Methods =====

// PushConfig receives and writes a new configuration
func (s *Server) PushConfig(ctx context.Context, req *pb.ConfigPayload) (*pb.CommandResponse, error) {
	if err := s.xrayMgr.UpdateConfig(req.JsonContent); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to update config: %v", err),
			ErrorCode: "CONFIG_UPDATE_FAILED",
		}, nil
	}

	// Restart xray to apply the new config (xray-core does not hot-reload)
	if err := s.xrayMgr.Restart(ctx, false, s.cfg.Process.GracefulTimeout); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Config written but restart failed: %v", err),
			ErrorCode: "RESTART_AFTER_CONFIG_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "Configuration updated and xray restarted",
	}, nil
}

// GetCurrentConfig returns the current configuration
func (s *Server) GetCurrentConfig(ctx context.Context, _ *pb.Empty) (*pb.ConfigPayload, error) {
	content, err := s.xrayMgr.GetConfigContent()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	hash, _ := s.xrayMgr.ConfigHash()

	return &pb.ConfigPayload{
		JsonContent: string(content),
		Checksum:    hash,
	}, nil
}

// UpdateXrayAPIConfig updates the Xray API address in the agent config
func (s *Server) UpdateXrayAPIConfig(ctx context.Context, req *pb.XrayAPIConfigPayload) (*pb.CommandResponse, error) {
	if req.ApiAddr == "" {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "API address cannot be empty",
			ErrorCode: "INVALID_ARGUMENT",
		}, nil
	}

	logrus.Infof("Updating Xray API address to %s", req.ApiAddr)

	// Update config in memory
	s.cfg.Xray.APIAddr = req.ApiAddr

	// Persist to disk
	if err := s.cfg.Save(); err != nil {
		logrus.Errorf("Failed to save config: %v", err)
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to save config: %v", err),
			ErrorCode: "SAVE_FAILED",
		}, nil
	}

	// Update Xray client
	s.xrayClient = agentxray.NewLocalClient(
		s.cfg.Xray.APIAddr,
		time.Duration(s.cfg.Xray.APITimeout)*time.Second,
	)

	return &pb.CommandResponse{
		Success: true,
		Message: "Xray API address updated successfully",
	}, nil
}

// ValidateConfig validates a configuration without applying it
func (s *Server) ValidateConfig(ctx context.Context, req *pb.ConfigPayload) (*pb.ValidationResult, error) {
	// Write to temp file in the same directory as the config to ensure write permissions
	// (Some systems have read-only /tmp or restricted access)
	configDir := filepath.Dir(s.xrayMgr.ConfigPath())
	tmpFile, err := os.CreateTemp(configDir, "xray-validate-*.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(req.JsonContent); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Run xray with -test flag
	cmd := exec.CommandContext(ctx, s.xrayMgr.BinaryPath(), "-test", "-c", tmpFile.Name())
	// Set working directory to config directory so relative paths in config work
	cmd.Dir = configDir
	// Set asset location so Xray finds geoip.dat/geosite.dat
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+s.xrayMgr.AssetDir())
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &pb.ValidationResult{
			Valid:  false,
			Errors: []string{string(output)},
		}, nil
	}

	return &pb.ValidationResult{
		Valid: true,
	}, nil
}

// ===== User Management Methods =====

// AddUser adds a user to an inbound
func (s *Server) AddUser(ctx context.Context, req *pb.UserPayload) (*pb.CommandResponse, error) {
	err := s.xrayClient.AddUser(ctx, req.InboundTag, req.Email, req.Uuid, req.Protocol, req.Flow, req.Encryption, req.Level)
	if err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to add user: %v", err),
			ErrorCode: "ADD_USER_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("User %s added successfully", req.Email),
	}, nil
}

// RemoveUser removes a user from an inbound
func (s *Server) RemoveUser(ctx context.Context, req *pb.UserPayload) (*pb.CommandResponse, error) {
	err := s.xrayClient.RemoveUser(ctx, req.InboundTag, req.Email)
	if err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to remove user: %v", err),
			ErrorCode: "REMOVE_USER_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("User %s removed successfully", req.Email),
	}, nil
}

// ListUsers returns users on an inbound
func (s *Server) ListUsers(ctx context.Context, req *pb.InboundSelector) (*pb.UserList, error) {
	users, err := s.xrayClient.GetInboundUsers(ctx, req.InboundTag)
	if err != nil {
		// Log error but return empty list to avoid crashing client?
		// Better to return error so panel knows something went wrong.
		return nil, fmt.Errorf("failed to list users: %v", err)
	}

	pbUsers := make([]*pb.UserInfo, 0, len(users))
	for _, u := range users {
		pbUsers = append(pbUsers, &pb.UserInfo{
			Email: u.Email,
			Level: int32(u.Level),
		})
	}

	return &pb.UserList{
		Users: pbUsers,
	}, nil
}

// ===== Statistics Methods =====

// GetSystemStats returns system resource statistics
func (s *Server) GetSystemStats(ctx context.Context, _ *pb.Empty) (*pb.SystemStats, error) {
	sysStats, err := s.statsCollect.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect system stats: %w", err)
	}

	return &pb.SystemStats{
		CpuUsagePercent:      sysStats.CPUUsagePercent,
		CpuPerCore:           sysStats.CPUPerCore,
		MemoryTotalBytes:     sysStats.MemoryTotalBytes,
		MemoryUsedBytes:      sysStats.MemoryUsedBytes,
		MemoryAvailableBytes: sysStats.MemoryAvailableBytes,
		MemoryUsagePercent:   sysStats.MemoryUsagePercent,
		SwapTotalBytes:       sysStats.SwapTotalBytes,
		SwapUsedBytes:        sysStats.SwapUsedBytes,
		DiskTotalBytes:       sysStats.DiskTotalBytes,
		DiskUsedBytes:        sysStats.DiskUsedBytes,
		DiskFreeBytes:        sysStats.DiskFreeBytes,
		DiskUsagePercent:     sysStats.DiskUsagePercent,
		NetworkRecvBytes:     sysStats.NetworkRecvBytes,
		NetworkSentBytes:     sysStats.NetworkSentBytes,
		NetworkRecvRate:      sysStats.NetworkRecvRate,
		NetworkSentRate:      sysStats.NetworkSentRate,
		LoadAvg_1:            sysStats.LoadAvg1,
		LoadAvg_5:            sysStats.LoadAvg5,
		LoadAvg_15:           sysStats.LoadAvg15,
		SystemUptimeSeconds:  sysStats.SystemUptimeSeconds,
		TcpConns:             sysStats.TcpCount,
		UdpConns:             sysStats.UdpCount,
		FdCount:              sysStats.FdCount,
	}, nil
}

// GetHostInfo returns static host information
func (s *Server) GetHostInfo(ctx context.Context, _ *pb.Empty) (*pb.HostInfo, error) {
	info, err := stats.GetHostInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}

	return &pb.HostInfo{
		Hostname:             info.Hostname,
		Os:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		Arch:                 info.Arch,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		CpuModelName:         info.CPUModelName,
		CpuCores:             info.CPUCores,
		TotalMemory:          info.TotalMemory,
		TotalSwap:            info.TotalSwap,
		BootTime:             int64(info.BootTime),
	}, nil
}

// ListNetInterfaces enumerates the box's NICs plus their live addresses.
func (s *Server) ListNetInterfaces(_ context.Context) ([]agentnetif.Interface, map[string][]string, error) {
	ifs, err := agentnetif.List(agentnetif.Opts{PermMAC: permanentMAC})
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate interfaces: %w", err)
	}

	addrs := make(map[string][]string, len(ifs))
	links, err := net.Interfaces()
	if err == nil {
		for _, l := range links {
			as, aerr := l.Addrs()
			if aerr != nil {
				continue
			}
			list := make([]string, 0, len(as))
			for _, a := range as {
				list = append(list, a.String())
			}
			addrs[l.Name] = list
		}
	}
	return ifs, addrs, nil
}

// GetXrayStats returns xray traffic statistics from the buffered store.
// The collector now owns Xray counter resets — this method serves aggregated data from the store.
func (s *Server) GetXrayStats(ctx context.Context, req *pb.StatsRequest) (*pb.XrayStats, error) {
	if s.trafficStore == nil {
		// Fallback: traffic buffering not enabled, query Xray directly
		xrayStats, err := s.xrayClient.QueryStats(ctx, req.Pattern, req.Reset_)
		if err != nil {
			return nil, fmt.Errorf("failed to query xray stats: %w", err)
		}
		return &pb.XrayStats{
			UserUplink:       xrayStats.UserUplink,
			UserDownlink:     xrayStats.UserDownlink,
			InboundUplink:    xrayStats.InboundUplink,
			InboundDownlink:  xrayStats.InboundDownlink,
			TotalUplink:      xrayStats.TotalUplink,
			TotalDownlink:    xrayStats.TotalDownlink,
			OutboundUplink:   xrayStats.OutboundUplink,
			OutboundDownlink: xrayStats.OutboundDownlink,
		}, nil
	}

	// Serve from the aggregated store
	type aggregator interface {
		Aggregate() *traffic.XrayStatsSnapshot
		DrainAll() *traffic.XrayStatsSnapshot
	}
	agg, ok := s.trafficStore.(aggregator)
	if !ok {
		return &pb.XrayStats{}, nil
	}

	var snapshot *traffic.XrayStatsSnapshot
	if req.Reset_ {
		snapshot = agg.DrainAll()
	} else {
		snapshot = agg.Aggregate()
	}

	return &pb.XrayStats{
		UserUplink:       snapshot.UserUplink,
		UserDownlink:     snapshot.UserDownlink,
		InboundUplink:    snapshot.InboundUplink,
		InboundDownlink:  snapshot.InboundDownlink,
		OutboundUplink:   snapshot.OutboundUplink,
		OutboundDownlink: snapshot.OutboundDownlink,
		TotalUplink:      snapshot.TotalUplink,
		TotalDownlink:    snapshot.TotalDownlink,
	}, nil
}

// GetBufferedTraffic returns time-bucketed traffic records from the agent's local buffer.
func (s *Server) GetBufferedTraffic(ctx context.Context, _ *pb.Empty) (*pb.BufferedTrafficStats, error) {
	if s.trafficStore == nil {
		return &pb.BufferedTrafficStats{}, nil
	}

	buckets := s.trafficStore.GetAll()
	records := make([]*pb.TrafficRecord, 0, len(buckets))
	var startTime, endTime int64
	for _, b := range buckets {
		records = append(records, &pb.TrafficRecord{
			Timestamp:        b.Timestamp,
			UserUplink:       b.UserUplink,
			UserDownlink:     b.UserDownlink,
			InboundUplink:    b.InboundUplink,
			InboundDownlink:  b.InboundDownlink,
			OutboundUplink:   b.OutboundUplink,
			OutboundDownlink: b.OutboundDownlink,
			TotalUplink:      b.TotalUplink,
			TotalDownlink:    b.TotalDownlink,
		})
		if startTime == 0 || b.Timestamp < startTime {
			startTime = b.Timestamp
		}
		if b.Timestamp > endTime {
			endTime = b.Timestamp
		}
	}

	return &pb.BufferedTrafficStats{
		Records:         records,
		BufferStartTime: startTime,
		BufferEndTime:   endTime,
	}, nil
}

// AckBufferedTraffic acknowledges receipt of traffic records up to a given timestamp.
func (s *Server) AckBufferedTraffic(ctx context.Context, req *pb.AckTrafficRequest) (*pb.CommandResponse, error) {
	if s.trafficStore == nil {
		return &pb.CommandResponse{Success: true, Message: "No traffic buffer configured"}, nil
	}

	s.trafficStore.Drain(req.AckedThroughTimestamp)

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("Acknowledged traffic through %d", req.AckedThroughTimestamp),
	}, nil
}

// StreamLogs streams xray logs (placeholder for future implementation)
func (s *Server) StreamLogs(req *pb.LogRequest, stream pb.NodeAgent_StreamLogsServer) error {
	// Get historical lines first
	lines := s.xrayMgr.TailLogs(int(req.TailLines))
	for _, line := range lines {
		if err := stream.Send(&pb.LogEntry{
			Timestamp: time.Now().UnixMilli(),
			LogType:   pb.LogType_LOG_TYPE_ALL,
			Message:   line,
		}); err != nil {
			return err
		}
	}

	if !req.Follow {
		return nil
	}

	// Stream new logs
	logChan := s.xrayMgr.LogChannel()
	for {
		select {
		case line := <-logChan:
			if err := stream.Send(&pb.LogEntry{
				Timestamp: time.Now().UnixMilli(),
				LogType:   pb.LogType_LOG_TYPE_ALL,
				Message:   line,
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// ===== Bandwidth Shaping Methods =====

// SetupBandwidth initializes TC bandwidth shaping on the node
func (s *Server) SetupBandwidth(ctx context.Context, req *pb.BandwidthRequest) (*pb.CommandResponse, error) {
	iface := req.InterfaceName
	if iface == "" {
		iface = s.cfg.Bandwidth.Interface
	}
	if iface == "" {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "No network interface specified (set interface_name in request or bandwidth.interface in agent config)",
			ErrorCode: "MISSING_INTERFACE",
		}, nil
	}
	totalBW := int(req.TotalBwMbps)
	if totalBW <= 0 {
		totalBW = s.cfg.Bandwidth.TotalBW
	}

	tcMgr, err := agenttc.NewManager(iface, totalBW, s.nftManager)
	if err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Failed to create bandwidth manager",
			ErrorCode: err.Error(),
		}, nil
	}
	s.tcManager = tcMgr
	if err := s.tcManager.Setup(ctx); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Failed to setup bandwidth shaping",
			ErrorCode: err.Error(),
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("Bandwidth shaping configured on %s (%d Mbps link)", iface, totalBW),
	}, nil
}

// TeardownBandwidth removes TC bandwidth shaping from the node
func (s *Server) TeardownBandwidth(ctx context.Context, _ *pb.Empty) (*pb.CommandResponse, error) {
	if s.tcManager == nil {
		return &pb.CommandResponse{Success: true, Message: "No bandwidth shaping configured"}, nil
	}

	if err := s.tcManager.Teardown(ctx); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Failed to teardown bandwidth shaping",
			ErrorCode: err.Error(),
		}, nil
	}
	s.tcManager = nil

	return &pb.CommandResponse{
		Success: true,
		Message: "Bandwidth shaping removed",
	}, nil
}

// ===== Access Log Methods =====

// GetAccessLogs returns recent parsed access log entries, optionally filtered by email.
func (s *Server) GetAccessLogs(ctx context.Context, req *pb.AccessLogRequest) (*pb.AccessLogResponse, error) {
	if s.accessLogCollector == nil {
		logrus.Warn("[GetAccessLogs] accessLogCollector is nil — collector not initialized")
		return &pb.AccessLogResponse{}, nil
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	store := s.accessLogCollector.Store()
	var entries []accesslog.Entry
	if req.Email != "" {
		entries = store.GetByEmail(req.Email, limit)
	} else {
		entries = store.GetAll(limit)
	}

	logrus.WithFields(logrus.Fields{
		"email":       req.Email,
		"limit":       limit,
		"store_total": store.Len(),
		"returned":    len(entries),
	}).Debug("[GetAccessLogs] query result")

	pbEntries := make([]*pb.AccessLogEntry, 0, len(entries))
	for _, e := range entries {
		pbEntries = append(pbEntries, &pb.AccessLogEntry{
			Timestamp:   e.Timestamp.Unix(),
			SourceIp:    e.SourceIP,
			Status:      e.Status,
			Network:     e.Network,
			Domain:      e.Domain,
			Port:        int32(e.Port),
			InboundTag:  e.InboundTag,
			OutboundTag: e.OutboundTag,
			Email:       e.Email,
		})
	}

	return &pb.AccessLogResponse{Entries: pbEntries}, nil
}

// GetBufferedAccessLogSummary returns hourly aggregated access log summaries.
func (s *Server) GetBufferedAccessLogSummary(ctx context.Context, _ *pb.Empty) (*pb.BufferedAccessLogSummary, error) {
	if s.accessLogCollector == nil {
		return &pb.BufferedAccessLogSummary{}, nil
	}

	agg := s.accessLogCollector.Aggregator()
	if agg == nil {
		return &pb.BufferedAccessLogSummary{}, nil
	}

	summaries := agg.GetBuffered()
	entries := make([]*pb.AccessLogHourlySummary, 0, len(summaries))
	for _, s := range summaries {
		entries = append(entries, &pb.AccessLogHourlySummary{
			Email:           s.Email,
			HourTimestamp:   s.HourTimestamp,
			AcceptedCount:   s.AcceptedCount,
			RejectedCount:   s.RejectedCount,
			TcpCount:        s.TcpCount,
			UdpCount:        s.UdpCount,
			Domains:         s.Domains,
			SourceIps:       s.SourceIPs,
			RejectedDomains: s.RejectedDomains,
		})
	}

	return &pb.BufferedAccessLogSummary{Entries: entries}, nil
}

// AckBufferedAccessLogSummary acknowledges processing of summaries up to a timestamp.
// Also applies the hub-pushed grace window so future GetBuffered calls
// honor the operator's setting.
func (s *Server) AckBufferedAccessLogSummary(ctx context.Context, req *pb.AckAccessLogSummaryRequest) (*pb.CommandResponse, error) {
	if s.accessLogCollector == nil {
		return &pb.CommandResponse{Success: true}, nil
	}

	agg := s.accessLogCollector.Aggregator()
	if agg == nil {
		return &pb.CommandResponse{Success: true}, nil
	}

	if g := req.GetGracePeriodSeconds(); g > 0 {
		agg.SetGracePeriod(time.Duration(g) * time.Second)
	}
	// Hub may push 0 for any cap → aggregator keeps that cap's default.
	agg.SetTopNCaps(
		req.GetMaxDomainsPerHour(),
		req.GetMaxRejectedDomainsPerHour(),
		req.GetMaxSourceIpsPerHour(),
	)
	agg.Ack(req.UpToTimestamp)
	return &pb.CommandResponse{Success: true, Message: "Access log summaries acknowledged"}, nil
}

// ===== Health Methods =====

// HealthCheck performs a health check
func (s *Server) HealthCheck(ctx context.Context, _ *pb.Empty) (*pb.HealthResponse, error) {
	components := make(map[string]*pb.ComponentHealth)

	// Check xray process
	xrayStatus := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	xrayMessage := "Running"
	if !s.xrayMgr.IsRunning() {
		xrayStatus = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
		xrayMessage = "Not running"
	}
	components["xray"] = &pb.ComponentHealth{
		Status:        xrayStatus,
		Message:       xrayMessage,
		LastCheckTime: time.Now().Unix(),
	}

	// gRPC API down → degraded, not unhealthy (xray process may still be fine)
	apiStatus := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	apiMessage := "Reachable"
	if err := s.xrayClient.Ping(ctx); err != nil {
		apiStatus = pb.HealthStatus_HEALTH_STATUS_DEGRADED
		apiMessage = fmt.Sprintf("API unreachable (Xray may still be functional): %v", err)
	}
	components["xray_api"] = &pb.ComponentHealth{
		Status:        apiStatus,
		Message:       apiMessage,
		LastCheckTime: time.Now().Unix(),
	}

	// Overall status
	overallStatus := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	overallMessage := "All components healthy"
	for _, c := range components {
		if c.Status == pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			overallStatus = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
			overallMessage = "Some components unhealthy"
			break
		} else if c.Status == pb.HealthStatus_HEALTH_STATUS_DEGRADED {
			overallStatus = pb.HealthStatus_HEALTH_STATUS_DEGRADED
			overallMessage = "Some components degraded"
		}
	}

	return &pb.HealthResponse{
		Status:     overallStatus,
		Message:    overallMessage,
		Components: components,
	}, nil
}

// GetVersion returns version information
func (s *Server) GetVersion(ctx context.Context, _ *pb.Empty) (*pb.VersionInfo, error) {
	// Use cached version or fetch
	s.mu.RLock()
	xrayVersion := s.xrayVersion
	s.mu.RUnlock()

	if xrayVersion == "" {
		xrayVersion = s.getXrayVersion()
		s.mu.Lock()
		s.xrayVersion = xrayVersion
		s.mu.Unlock()
	}

	return &pb.VersionInfo{
		AgentVersion:   Version,
		AgentCommit:    Commit,
		AgentBuildTime: BuildTime,
		XrayVersion:    xrayVersion,
		GoVersion:      runtime.Version(),
		Os:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}, nil
}

// getXrayVersion wraps fetchXrayVersion to be used by public methods
func (s *Server) getXrayVersion() string {
	return s.fetchXrayVersion()
}

// fetchXrayVersion actually runs the command
func (s *Server) fetchXrayVersion() string {
	cmd := exec.Command(s.xrayMgr.BinaryPath(), "version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	// Parse first line
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

// ===== Heartbeat Methods =====

// Heartbeat handles bidirectional heartbeat stream
func (s *Server) Heartbeat(stream pb.NodeAgent_HeartbeatServer) error {
	for {
		ping, err := stream.Recv()
		if err != nil {
			return err
		}

		// Calculate RTT
		rtt := time.Now().UnixMilli() - ping.Timestamp

		// Get quick status with a per-ping deadline so a hanging xray API
		// doesn't starve the whole heartbeat stream.
		statusCtx, statusCancel := context.WithTimeout(stream.Context(), 3*time.Second)
		status, _ := s.GetStatus(statusCtx, &pb.Empty{})
		statusCancel()

		pong := &pb.HeartbeatPong{
			Timestamp: time.Now().UnixMilli(),
			Sequence:  ping.Sequence,
			RttMs:     rtt,
			Status:    status,
			NodeUuid:  s.cfg.NodeUUID,
		}

		if err := stream.Send(pong); err != nil {
			return err
		}
	}
}

// ===== SSH Methods =====

// GetSSHStatus returns the current status of the SSH service
func (s *Server) GetSSHStatus(ctx context.Context, _ *pb.Empty) (*pb.SSHStatus, error) {
	enabled, port, active, err := s.sshMgr.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get ssh status: %w", err)
	}

	return &pb.SSHStatus{
		Enabled:  enabled,
		Port:     int32(port),
		IsActive: active,
	}, nil
}

// UpdateSSHConfig updates the SSH configuration (port, enabled)
func (s *Server) UpdateSSHConfig(ctx context.Context, req *pb.SSHConfigPayload) (*pb.CommandResponse, error) {
	if err := s.sshMgr.UpdateConfig(req.Enabled, int(req.Port)); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to update ssh config: %v", err),
			ErrorCode: "SSH_UPDATE_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "SSH configuration updated successfully",
	}, nil
}

// ClearSSHLogs clears/truncates the SSH auth logs
func (s *Server) ClearSSHLogs(ctx context.Context, _ *pb.Empty) (*pb.CommandResponse, error) {
	if err := s.sshMgr.ClearLogs(); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to clear ssh logs: %v", err),
			ErrorCode: "SSH_LOGS_CLEAR_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "SSH logs cleared successfully",
	}, nil
}

// RestartSSH restarts the SSH service
func (s *Server) RestartSSH(ctx context.Context, _ *pb.Empty) (*pb.CommandResponse, error) {
	if err := s.sshMgr.Restart(); err != nil {
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to restart ssh service: %v", err),
			ErrorCode: "SSH_RESTART_FAILED",
		}, nil
	}

	return &pb.CommandResponse{
		Success: true,
		Message: "SSH service restarted successfully",
	}, nil
}

// ===== Certificate Denylist Methods =====

// UpdateCertDenylist receives a list of revoked certificate serial numbers
func (s *Server) UpdateCertDenylist(ctx context.Context, req *pb.CertDenylist) (*pb.CommandResponse, error) {
	newDenylist := make(map[string]bool, len(req.RevokedSerialNumbers))
	for _, serial := range req.RevokedSerialNumbers {
		newDenylist[serial] = true
	}

	s.denylistMu.Lock()
	s.deniedSerials = newDenylist
	s.denylistMu.Unlock()

	logrus.WithFields(logrus.Fields{
		"count": len(req.RevokedSerialNumbers),
		"hash":  req.DenylistHash,
	}).Info("[UpdateCertDenylist] Certificate denylist updated")

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("Denylist updated with %d revoked serials", len(req.RevokedSerialNumbers)),
	}, nil
}

// ===== Xray Version Management Methods =====

// UpdateXrayBinary downloads and installs a specific xray-core version.
// Serialized per-agent via xrayUpdateMu so concurrent admin requests cannot
// race on the backup/write/restart path.
func (s *Server) UpdateXrayBinary(ctx context.Context, req *pb.UpdateXrayRequest) (*pb.CommandResponse, error) {
	s.xrayUpdateMu.Lock()
	defer s.xrayUpdateMu.Unlock()

	version := req.Version
	if version == "" {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Version cannot be empty",
			ErrorCode: "INVALID_ARGUMENT",
		}, nil
	}

	// Validate version string (digits, letters, dots, hyphens) — matches the
	// hub's IsValidVersion so custom builds like "26.5.9-wg1" are accepted.
	// No '/' allowed, so it stays path-safe.
	for _, c := range version {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '.' || c == '-') {
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Invalid version format: %s", version),
				ErrorCode: "INVALID_ARGUMENT",
			}, nil
		}
	}

	logrus.WithField("version", version).Info("[UpdateXrayBinary] Starting xray-core update")

	// If hub provides a download URL, use it instead of GitHub.
	// Checksum is mandatory in this path: without it the binary cannot be
	// integrity-verified, defeating the purpose of going through the hub.
	if req.DownloadUrl != "" {
		if req.Checksum == "" {
			return &pb.CommandResponse{
				Success:   false,
				Message:   "Hub-supplied DownloadUrl requires a Checksum; aborting to avoid installing an unverified binary",
				ErrorCode: "MISSING_CHECKSUM",
			}, nil
		}
		return s.updateXrayFromHub(ctx, req, version)
	}

	// Fall through to GitHub-based install for backward compatibility / disaster recovery.
	return s.updateXrayFromGitHub(ctx, req, version)
}

// updateXrayFromHub performs a hub-mediated atomic update with full rollback.
// Caller must hold xrayUpdateMu.
func (s *Server) updateXrayFromHub(ctx context.Context, req *pb.UpdateXrayRequest, version string) (*pb.CommandResponse, error) {
	xrayBin := s.xrayMgr.BinaryPath()
	backupPath := xrayBin + ".bak"

	wasRunning := s.xrayMgr.IsRunning()
	if wasRunning {
		logrus.Info("[UpdateXrayBinary] Stopping xray before hub update")
		if err := s.xrayMgr.Stop(10 * time.Second); err != nil {
			// Refuse to overwrite a running binary — Linux gives ETXTBSY on
			// truncate of in-use exec, and partial writes corrupt the
			// process's executable mmap. Bail out cleanly.
			logrus.WithError(err).Error("[UpdateXrayBinary] Failed to stop xray; aborting update to avoid corruption")
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Refusing to update: failed to stop running xray: %v", err),
				ErrorCode: "STOP_FAILED",
			}, nil
		}
	}

	// Back up the current binary. The write MUST succeed — otherwise we
	// have no rollback path on a later failure.
	origData, origErr := os.ReadFile(xrayBin)
	hasBackup := false
	if origErr == nil {
		if err := writeFileAtomic(backupPath, origData, 0755); err != nil {
			if wasRunning {
				_ = s.xrayMgr.Start()
			}
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Failed to back up current binary: %v", err),
				ErrorCode: "BACKUP_FAILED",
			}, nil
		}
		hasBackup = true
	} else if !os.IsNotExist(origErr) {
		// File exists but unreadable — treat as fatal; we'd have no rollback.
		if wasRunning {
			_ = s.xrayMgr.Start()
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to read current binary for backup: %v", origErr),
			ErrorCode: "BACKUP_FAILED",
		}, nil
	}

	// Helper to roll back to the backup binary and (optionally) restart.
	rollback := func(reason string) {
		if hasBackup {
			if backupData, berr := os.ReadFile(backupPath); berr == nil {
				if err := writeFileAtomic(xrayBin, backupData, 0755); err != nil {
					logrus.WithError(err).WithField("reason", reason).Error("[UpdateXrayBinary] Rollback write failed")
				}
			}
		}
		if wasRunning {
			if err := s.xrayMgr.Start(); err != nil {
				logrus.WithError(err).Error("[UpdateXrayBinary] Failed to restart xray with old binary after rollback")
			}
		}
	}

	// Download from hub.
	dlClient := &http.Client{Timeout: 5 * time.Minute}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.DownloadUrl, nil)
	if err != nil {
		rollback("create request failed")
		return &pb.CommandResponse{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}, nil
	}
	if req.DownloadToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.DownloadToken)
	}

	resp, err := dlClient.Do(httpReq)
	if err != nil {
		rollback("download error")
		return &pb.CommandResponse{Success: false, Message: fmt.Sprintf("Failed to download xray: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rollback("download non-200")
		return &pb.CommandResponse{Success: false, Message: fmt.Sprintf("Download failed: HTTP %d", resp.StatusCode)}, nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		rollback("read body error")
		return &pb.CommandResponse{Success: false, Message: fmt.Sprintf("Failed to read response: %v", err)}, nil
	}

	// Verify SHA256 — mandatory in the hub path (enforced at entry).
	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])
	if actual != req.Checksum {
		rollback("checksum mismatch")
		return &pb.CommandResponse{
			Success: false,
			Message: fmt.Sprintf("Checksum mismatch: expected %.16s..., got %.16s...", req.Checksum, actual),
		}, nil
	}
	logrus.Info("[UpdateXrayBinary] Checksum verified")

	// Validate ELF + arch matches this host before writing.
	if err := validateXrayBinary(data); err != nil {
		rollback("binary invalid: " + err.Error())
		return &pb.CommandResponse{
			Success: false,
			Message: fmt.Sprintf("Refusing to install: %v", err),
		}, nil
	}

	// Write new binary atomically. Tmp file + rename so a crash mid-write
	// cannot leave a truncated /usr/local/bin/xray behind.
	if err := writeFileAtomic(xrayBin, data, 0755); err != nil {
		rollback("write failed")
		return &pb.CommandResponse{Success: false, Message: fmt.Sprintf("Failed to write binary: %v", err)}, nil
	}

	// Clear cached version so next GetVersion fetches fresh.
	s.mu.Lock()
	s.xrayVersion = ""
	s.mu.Unlock()

	// Restart if requested or it was running before. Failure rolls back.
	shouldRun := req.RestartAfter || wasRunning
	if shouldRun {
		logrus.Info("[UpdateXrayBinary] Restarting xray with new binary")
		if err := s.xrayMgr.Start(); err != nil {
			logrus.WithError(err).Error("[UpdateXrayBinary] Restart with new binary failed; rolling back")
			rollback("restart failed")
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Xray restart failed after update: %v (rolled back)", err),
				ErrorCode: "RESTART_FAILED",
			}, nil
		}
	}

	// Verify the installed binary actually reports a version. Empty/garbage
	// output means the binary is broken; roll back rather than report a
	// misleading success.
	newVersion := s.fetchXrayVersion()
	if newVersion == "" || newVersion == "unknown" {
		logrus.Warn("[UpdateXrayBinary] Post-install version probe returned empty/unknown; rolling back")
		if shouldRun {
			_ = s.xrayMgr.Stop(10 * time.Second)
		}
		rollback("version probe failed")
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Installed binary failed to report a version; rolled back",
			ErrorCode: "POST_INSTALL_VERIFY_FAILED",
		}, nil
	}

	// Only now is it safe to remove the backup.
	if hasBackup {
		_ = os.Remove(backupPath)
	}

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("Xray updated to %s from hub", newVersion),
	}, nil
}

// updateXrayFromGitHub: official XTLS install script. Caller holds
// xrayUpdateMu. No backup (script manages binary); post-install version
// probe verifies success.
func (s *Server) updateXrayFromGitHub(ctx context.Context, req *pb.UpdateXrayRequest, version string) (*pb.CommandResponse, error) {
	wasRunning := s.xrayMgr.IsRunning()
	if wasRunning {
		logrus.Info("[UpdateXrayBinary] Stopping xray before GitHub install")
		if err := s.xrayMgr.Stop(10 * time.Second); err != nil {
			logrus.WithError(err).Error("[UpdateXrayBinary] Failed to stop xray; aborting GitHub install")
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Refusing to install: failed to stop running xray: %v", err),
				ErrorCode: "STOP_FAILED",
			}, nil
		}
	}

	installCmd := fmt.Sprintf(
		`bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install --version v%s`,
		version,
	)
	cmd := exec.CommandContext(ctx, "bash", "-c", installCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.WithError(err).WithField("output", string(output)).Error("[UpdateXrayBinary] Install script failed")
		if wasRunning {
			_ = s.xrayMgr.Start()
		}
		return &pb.CommandResponse{
			Success:   false,
			Message:   fmt.Sprintf("Install failed: %s", string(output)),
			ErrorCode: "INSTALL_FAILED",
		}, nil
	}

	logrus.WithField("output", string(output)).Info("[UpdateXrayBinary] Install script completed")

	// Clear cached xray version so verification reads fresh.
	s.mu.Lock()
	s.xrayVersion = ""
	s.mu.Unlock()

	// Restart if requested.
	if req.RestartAfter || wasRunning {
		logrus.Info("[UpdateXrayBinary] Restarting xray with new binary")
		if err := s.xrayMgr.Start(); err != nil {
			return &pb.CommandResponse{
				Success:   false,
				Message:   fmt.Sprintf("Xray updated but failed to restart: %v", err),
				ErrorCode: "RESTART_FAILED",
			}, nil
		}
	}

	// Verify post-install version: script can exit 0 yet leave the old
	// binary in place (e.g. no-op re-install).
	newVersion := s.fetchXrayVersion()
	if newVersion == "" || newVersion == "unknown" {
		return &pb.CommandResponse{
			Success:   false,
			Message:   "Install script returned success but xray --version produced no output",
			ErrorCode: "POST_INSTALL_VERIFY_FAILED",
		}, nil
	}
	if !strings.Contains(newVersion, version) {
		logrus.WithFields(logrus.Fields{
			"requested": version,
			"installed": newVersion,
		}).Warn("[UpdateXrayBinary] Post-install version does not contain requested version")
	}

	return &pb.CommandResponse{
		Success: true,
		Message: fmt.Sprintf("Xray updated to %s", newVersion),
	}, nil
}

// writeFileAtomic writes data to a tmp file in the same directory then renames
// over the target. Rename within one filesystem is atomic on POSIX, so readers
// see either the old file or the new file — never a partial write.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// validateXrayBinary checks that data is an ELF executable for the agent's
// host architecture. Refuses to install wrong-arch binaries that would
// either fail to exec or, worse, leave the agent unable to roll back.
func validateXrayBinary(data []byte) error {
	if len(data) < 20 {
		return fmt.Errorf("binary too small to be an ELF")
	}
	if !(data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F') {
		return fmt.Errorf("not an ELF file")
	}
	// e_machine at byte offset 0x12 (little-endian u16). Linux xray builds
	// are 64-bit ELF, so the byte at 0x12 is enough.
	wantArch := elfArchForHost()
	if wantArch == "" {
		// Unknown host arch — accept rather than block; fetchXrayVersion
		// will catch a broken install.
		return nil
	}
	gotArch := ""
	switch data[0x12] {
	case 0x3E:
		gotArch = "amd64"
	case 0xB7:
		gotArch = "arm64"
	}
	if gotArch == "" {
		return fmt.Errorf("unsupported ELF e_machine 0x%02x", data[0x12])
	}
	if gotArch != wantArch {
		return fmt.Errorf("binary arch %s does not match host arch %s", gotArch, wantArch)
	}
	return nil
}

// elfArchForHost returns the canonical arch string for the agent's CPU, or
// "" for unrecognized hosts.
func elfArchForHost() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	}
	return ""
}

// ===== Outbound Testing Methods =====

// TestOutbound tests outbound connectivity using xray-knife.
// It parses a config link, creates a temporary xray instance, and makes an HTTP
// request through the proxy to verify end-to-end connectivity.
func (s *Server) TestOutbound(ctx context.Context, req *pb.OutboundTestRequest) (*pb.OutboundTestResponse, error) {
	configLink := req.ConfigLink
	if configLink == "" {
		return &pb.OutboundTestResponse{Success: false, Error: "config_link is required"}, nil
	}

	testURL := req.TestUrl
	if testURL == "" {
		testURL = "https://www.google.com/generate_204"
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// 1. Create xray-knife core (insecureTLS=true for self-signed certs)
	xrayCore := core.CoreFactory(core.XrayCoreType, true, false)

	// 2. Parse protocol from config link
	proto, err := xrayCore.CreateProtocol(configLink)
	if err != nil {
		return &pb.OutboundTestResponse{
			Success: false,
			Error:   "failed to parse config: " + err.Error(),
		}, nil
	}

	// CreateProtocol only identifies the protocol type; Parse() actually
	// parses the URL into struct fields (address, transport, TLS, etc.)
	if err := proto.Parse(); err != nil {
		return &pb.OutboundTestResponse{
			Success: false,
			Error:   "failed to parse protocol link: " + err.Error(),
		}, nil
	}

	// 3. Create HTTP client tunneled through the outbound
	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient, instance, err := xrayCore.MakeHttpClient(testCtx, proto, timeout)
	if err != nil {
		return &pb.OutboundTestResponse{
			Success: false,
			Error:   "failed to create proxy client: " + err.Error(),
		}, nil
	}
	defer instance.Close()

	// 4. Measure latency with HTTP request through proxy
	start := time.Now()
	resp, err := httpClient.Get(testURL)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return &pb.OutboundTestResponse{
			Success:   false,
			LatencyMs: latency,
			Error:     err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	result := &pb.OutboundTestResponse{
		Success:    true,
		LatencyMs:  latency,
		StatusCode: int32(resp.StatusCode),
		Message:    fmt.Sprintf("Connected (%dms) - HTTP %d", latency, resp.StatusCode),
	}

	return result, nil
}

// ===== Online User Detection Methods =====

// GetUserOnlineIPs returns the IPs currently connected for a specific user
func (s *Server) GetUserOnlineIPs(ctx context.Context, req *pb.UserEmailRequest) (*pb.OnlineIPsResponse, error) {
	ips, err := s.xrayClient.GetUserOnlineIPs(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to get online IPs: %w", err)
	}
	if ips == nil {
		ips = make(map[string]int64)
	}
	return &pb.OnlineIPsResponse{Ips: ips}, nil
}

// GetAllUsersOnlineIPs returns IP→timestamp map for every online user in
// one RPC (collapses the hub↔agent N+1; agent still hits xray stats per
// user on localhost). Per-user xray errors are swallowed — bad user
// just absent from the map.
func (s *Server) GetAllUsersOnlineIPs(ctx context.Context, _ *pb.Empty) (*pb.AllOnlineIPsResponse, error) {
	emails, err := s.xrayClient.GetAllOnlineUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list online users: %w", err)
	}
	users := make(map[string]*pb.OnlineIPMap, len(emails))
	for _, email := range emails {
		if ctx.Err() != nil {
			break
		}
		ips, err := s.xrayClient.GetUserOnlineIPs(ctx, email)
		if err != nil {
			logrus.WithError(err).WithField("email", email).Debug("GetAllUsersOnlineIPs: per-user fetch failed")
			continue
		}
		if ips == nil {
			ips = make(map[string]int64)
		}
		users[email] = &pb.OnlineIPMap{Ips: ips}
	}
	return &pb.AllOnlineIPsResponse{Users: users}, nil
}

// ===== Tools =====

// GenerateVLESSKeys runs `xray vlessenc` to generate VLESS encryption keys (X25519 + ML-KEM-768).
func (s *Server) GenerateVLESSKeys(ctx context.Context, _ *pb.Empty) (*pb.VLESSKeysResponse, error) {
	cmd := exec.CommandContext(ctx, s.xrayMgr.BinaryPath(), "vlessenc")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run xray vlessenc: %w", err)
	}
	keys := parseVLESSEncOutput(string(output))
	return &pb.VLESSKeysResponse{Keys: keys}, nil
}

// parseVLESSEncOutput parses the output of `xray vlessenc` into key pairs.
func parseVLESSEncOutput(output string) []*pb.VLESSKeyPair {
	lines := strings.Split(output, "\n")
	var keys []*pb.VLESSKeyPair
	var current *pb.VLESSKeyPair

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Authentication:") {
			if current != nil {
				keys = append(keys, current)
			}
			current = &pb.VLESSKeyPair{
				Label: strings.TrimSpace(strings.TrimPrefix(line, "Authentication:")),
			}
		} else if current != nil {
			if strings.HasPrefix(line, "\"decryption\"") {
				if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
					current.Decryption = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			} else if strings.HasPrefix(line, "\"encryption\"") {
				if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
					current.Encryption = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			}
		}
	}

	if current != nil {
		keys = append(keys, current)
	}

	return keys
}

// ===== Cleanup Methods =====

// removePathLogged removes a file or directory and logs the result.
func removePathLogged(path string, isDir bool) {
	var err error
	if isDir {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil && !os.IsNotExist(err) {
		logrus.WithError(err).WithField("path", path).Warn("Failed to remove path during uninstall")
	} else if err == nil {
		logrus.WithField("path", path).Info("Removed during uninstall")
	}
}

// removeGlobLogged removes all files matching a glob pattern.
func removeGlobLogged(pattern string) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		logrus.WithError(err).WithField("pattern", pattern).Warn("Failed to glob during uninstall")
		return
	}
	for _, m := range matches {
		removePathLogged(m, false)
	}
}

// Uninstall removes the agent and xray from the system
func (s *Server) Uninstall(ctx context.Context, _ *pb.Empty) (*pb.CommandResponse, error) {
	logrus.Info("Uninstall requested - initiating self-destruct sequence")

	if !s.nukeInFlight.CompareAndSwap(false, true) {
		return nil, status.Error(codes.AlreadyExists, "another wipe/nuke/uninstall is already in progress")
	}

	// 1. Stop Xray process
	if err := s.xrayMgr.Stop(s.cfg.Process.GracefulTimeout); err != nil {
		logrus.WithError(err).Warn("Failed to stop xray during uninstall")
	}

	// 2. Schedule cleanup and exit (after gRPC response is sent)
	s.scheduleSelfDestruct()

	return &pb.CommandResponse{
		Success: true,
		Message: "Uninstall initiated. Agent and Xray will be removed.",
	}, nil
}

// scheduleSelfDestruct runs the agent's teardown in a detached goroutine,
// matching the Uninstall pattern. Callers should send any final RPC response
// before invoking this.
func (s *Server) scheduleSelfDestruct() {
	go func() {
		time.Sleep(2 * time.Second)
		s.runSelfDestructInline()
	}()
}

// runSelfDestructInline performs the actual cleanup and exits. Separated so
// tests can run it synchronously in a subprocess if needed.
func (s *Server) runSelfDestructInline() {
	xrayServiceName := s.cfg.Process.ServiceName
	if xrayServiceName != "" {
		logrus.WithField("service", xrayServiceName).Info("Disabling xray systemd service")
		exec.Command("systemctl", "disable", "--now", xrayServiceName).Run()
		removePathLogged(filepath.Join("/etc/systemd/system", xrayServiceName+".service"), false)
		removePathLogged(filepath.Join("/etc/systemd/system", xrayServiceName+".service.d"), true)
	}
	configDir := filepath.Dir(s.xrayMgr.ConfigPath())
	removePathLogged(configDir, true)
	removePathLogged(s.xrayMgr.BinaryPath(), false)
	xrayDataDir := s.cfg.Xray.DataDir
	if xrayDataDir == "" {
		xrayDataDir = "/usr/local/share/xray"
	}
	removePathLogged(xrayDataDir, true)
	removePathLogged("/var/log/xray", true)
	removePathLogged("/etc/nasnet-agent", true)
	removePathLogged("/var/lib/nasnet-agent", true)
	removePathLogged("/var/log/nasnet-agent", true)
	removeGlobLogged("/tmp/nasnet-*")
	removeGlobLogged("/tmp/xray-*")
	removeGlobLogged("/tmp/agent-*")
	if executable, err := os.Executable(); err == nil {
		removePathLogged(executable+".old", false)
		removePathLogged(executable+".new", false)
		logrus.WithField("path", executable).Info("Removing agent binary")
		os.Remove(executable)
	}
	logrus.Info("Disabling agent systemd service")
	exec.Command("systemctl", "disable", "--now", "nasnet-agent").Run()
	removePathLogged("/etc/systemd/system/nasnet-agent.service", false)
	removePathLogged("/etc/systemd/system/nasnet-agent.service.d", true)
	exec.Command("systemctl", "daemon-reload").Run()
	logrus.Info("Uninstall complete. Exiting...")
	os.Exit(0)
}

// Wipe runs the nuke phase list (in WIPE or NUKE mode) to completion and
// returns the final report. The agent self-destructs after responding.
func (s *Server) Wipe(ctx context.Context, req *pb.NukeRequest) (*pb.NukeReport, error) {
	if !s.nukeInFlight.CompareAndSwap(false, true) {
		return nil, status.Error(codes.AlreadyExists, "another wipe/nuke/uninstall is already in progress")
	}
	if err := preFlight(NewRootAt("/")); err != nil {
		s.nukeInFlight.Store(false)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	root := NewRootAt("/").WithDryRun(req.DryRun)
	runner := &nukeRunner{
		phases: defaultPhases(func() error {
			return s.xrayMgr.Stop(s.cfg.Process.GracefulTimeout)
		}),
		root: root,
	}
	report, _ := runner.Run(ctx, req, nil)

	if !req.DryRun {
		s.scheduleSelfDestruct()
	} else {
		s.nukeInFlight.Store(false)
	}
	return report, nil
}

// Nuke is the streaming variant. Emits NukePhaseResult per phase, then a
// terminal NukeReport, then closes as the agent self-destructs.
func (s *Server) Nuke(req *pb.NukeRequest, stream pb.NodeAgent_NukeServer) error {
	if !s.nukeInFlight.CompareAndSwap(false, true) {
		return status.Error(codes.AlreadyExists, "another wipe/nuke/uninstall is already in progress")
	}
	if err := preFlight(NewRootAt("/")); err != nil {
		s.nukeInFlight.Store(false)
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	root := NewRootAt("/").WithDryRun(req.DryRun)
	runner := &nukeRunner{
		phases: defaultPhases(func() error {
			return s.xrayMgr.Stop(s.cfg.Process.GracefulTimeout)
		}),
		root: root,
	}

	emit := func(r *pb.NukePhaseResult) {
		_ = stream.Send(&pb.NukeProgress{Event: &pb.NukeProgress_Phase{Phase: r}})
	}

	report, _ := runner.Run(stream.Context(), req, emit)
	_ = stream.Send(&pb.NukeProgress{Event: &pb.NukeProgress_Done{Done: report}})

	if !req.DryRun {
		s.scheduleSelfDestruct()
	} else {
		s.nukeInFlight.Store(false)
	}
	return nil
}
