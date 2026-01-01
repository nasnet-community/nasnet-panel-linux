package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool"
	"github.com/spf13/cobra"
)

func init() {
	// Restore terminal cursor on exit (in case spinner hid it)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Print("\033[?25h") // show cursor
		os.Exit(1)
	}()
}

var offlineFlag bool

var rootCmd = &cobra.Command{
	Use:   "nasnet-tool",
	Short: "Interactive admin tool for nasnet-panel",
	Long:  "nasnet-tool is the interactive TUI admin tool for managing nasnet-panel deployments.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.Run(cfg)
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Run the installation wizard",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.WizardInstall(cfg)
		return nil
	},
}

var reconfigureCmd = &cobra.Command{
	Use:   "reconfigure",
	Short: "Run the reconfiguration wizard",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.WizardReconfigure(cfg)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update nasnet-panel to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.WizardUpdate(cfg)
		return nil
	},
}

var autoUpdateCmd = &cobra.Command{
	Use:   "auto-update",
	Short: "Enable or manage auto-update for nasnet-panel releases",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.ActionAutoUpdate(cfg)
		return nil
	},
}

var configCmd = &cobra.Command{
	Use:     "config",
	Aliases: []string{"view-config"},
	Short:   "View or manage the current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := tool.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if offlineFlag {
			cfg.OfflineMode = true
		}
		tool.WizardViewConfig(cfg)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&offlineFlag, "offline", false, "Run in offline mode (skip network checks)")
	rootCmd.AddCommand(installCmd, reconfigureCmd, updateCmd, autoUpdateCmd, configCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
