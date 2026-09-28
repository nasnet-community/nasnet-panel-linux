package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/reverse"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	// Version information (set at build time via ldflags)
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

var (
	configPath string
	debug      bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "nasnet-agent",
		Short: "Node agent for managing Xray-core on remote servers",
		Long: `NasNet Agent is a lightweight daemon that runs on each remote server.
It manages the local Xray-core process and exposes a gRPC API for the panel
to control configuration, users, and monitor system resources.`,
		Version: version,
		RunE:    run,
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("nasnet-agent %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built: %s\n", buildTime)
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Configure logging
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.InfoLevel)
	}
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Apply logging config
	if cfg.Logging.Level != "" {
		level, err := logrus.ParseLevel(cfg.Logging.Level)
		if err == nil {
			logrus.SetLevel(level)
		}
	}

	if cfg.Logging.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	// Set version info for server
	server.Version = version
	server.Commit = commit
	server.BuildTime = buildTime

	logrus.WithFields(logrus.Fields{
		"version":     version,
		"listen_addr": cfg.ListenAddr,
		"xray_binary": cfg.Xray.BinaryPath,
		"xray_config": cfg.Xray.ConfigPath,
	}).Info("Starting nasnet-agent")

	// Create and start server
	srv, err := server.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if cfg.ConnectMode == "reverse" {
		return runReverse(cfg, srv)
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logrus.WithField("signal", sig).Info("Received shutdown signal")

		// Create context with timeout for graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		srv.Stop(ctx)
	}()

	// Start server (blocks until stopped)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	logrus.Info("Agent shutdown complete")
	return nil
}

func runReverse(cfg *config.Config, srv *server.Server) error {
	logrus.WithField("hub_url", cfg.HubURL).Info("Starting in reverse mode")

	srv.StartBackgroundServices(context.Background())

	handler := reverse.NewCommandHandler(srv)
	connector := reverse.NewConnector(cfg, handler)
	connector.Version = server.Version
	connector.Commit = server.Commit

	// Give the InstallCertificate reply a second to reach the hub, then
	// reconnect so the hub sees the new certificate.
	srv.SetOnCertInstalled(func() { time.AfterFunc(time.Second, connector.Recycle) })

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logrus.Info("Received shutdown signal")
		cancel()
		connector.Stop()
	}()

	err := connector.Run(ctx)

	// Clean up background services (xray, traffic collector, access log, TC bandwidth)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	srv.Stop(stopCtx)

	logrus.Info("Agent shutdown complete")
	return err
}
