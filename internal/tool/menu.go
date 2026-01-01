package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// Run is the main menu loop for nasnet-tool.
func Run(cfg *Config) {
	// First-run detection: if no .env exists, offer to run wizard
	if _, err := os.Stat(cfg.EnvFile); os.IsNotExist(err) {
		ui.ClearScreen()
		ui.DrawBox("nasnet-panel — First Run")
		ui.StepWarn("No .env file found.")
		fmt.Println(ui.StyleDim.Render("  It looks like this is a fresh installation."))
		fmt.Println()
		if ok, _ := ui.Confirm("Run the installation wizard now?"); ok {
			WizardInstall(cfg)
		}
	}

	for {
		ui.ClearScreen()
		ui.DrawHeader("nasnet-panel Admin Tool")

		version := detectVersion(cfg)
		behind := detectUpdateBehind(cfg)

		ui.StepInfo(fmt.Sprintf("Version: %s", version))
		if behind > 0 {
			ui.StepWarn(fmt.Sprintf("%d update(s) available", behind))
		}
		ui.DrawSeparator()

		options := []string{
			"Setup Wizard",
			"Update",
			"Auto-Update (Release)",
		}

		if cfg.IsDocker() {
			options = append(options, "Docker Services")
		} else {
			options = append(options, "Systemd Services")
		}

		options = append(options,
			"Database",
			"Monitoring",
			"Security",
			"Maintenance",
			"Uninstall",
		)

		choice, err := ui.Menu("Main Menu", options)
		if err != nil || choice < 0 {
			ui.ClearScreen()
			fmt.Println("Goodbye.")
			return
		}

		// Map choices back — options are dynamic so we track by label
		label := options[choice]
		switch label {
		case "Setup Wizard":
			menuSetupWizard(cfg)
		case "Update":
			WizardUpdate(cfg)
		case "Auto-Update (Release)":
			ActionAutoUpdate(cfg)
		case "Docker Services":
			menuServices(cfg)
		case "Systemd Services":
			menuSystemd(cfg)
		case "Database":
			menuDatabase(cfg)
		case "Monitoring":
			menuMonitoring(cfg)
		case "Security":
			menuSecurity(cfg)
		case "Maintenance":
			menuMaintenance(cfg)
		case "Uninstall":
			ActionUninstall(cfg)
		}
	}
}

func menuSetupWizard(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Setup Wizard")

		choice, err := ui.Menu("Setup Wizard", []string{
			"Fresh Install",
			"Reconfigure",
			"View Config",
			"Change Ports",
			"Change URLs",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			WizardInstall(cfg)
		case 1:
			WizardReconfigure(cfg)
		case 2:
			WizardViewConfig(cfg)
		case 3:
			WizardChangePorts(cfg)
		case 4:
			WizardChangeURLs(cfg)
		}
	}
}

func menuServices(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Docker Services")

		choice, err := ui.Menu("Docker Services", []string{
			"Start All Services",
			"Stop All Services",
			"Restart a Service",
			"View Status",
			"View Logs",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			ActionStartServices(cfg)
		case 1:
			ActionStopServices(cfg)
		case 2:
			ActionRestartService(cfg)
		case 3:
			ActionViewStatus(cfg)
		case 4:
			ActionViewLogs(cfg)
		}
	}
}

func menuSystemd(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Systemd Services")

		choice, err := ui.Menu("Systemd Services", []string{
			"Status",
			"Install Service",
			"Rebuild Service",
			"Start",
			"Stop",
			"Enable (auto-start)",
			"Disable (auto-start)",
			"Delete Service",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			ActionSystemdStatus(cfg)
		case 1:
			ActionSystemdInstall(cfg)
		case 2:
			ActionSystemdRebuild(cfg)
		case 3:
			ActionSystemdStart(cfg)
		case 4:
			ActionSystemdStop(cfg)
		case 5:
			ActionSystemdEnable(cfg)
		case 6:
			ActionSystemdDisable(cfg)
		case 7:
			ActionSystemdDelete(cfg)
		}
	}
}

func menuDatabase(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Database")

		choice, err := ui.Menu("Database", []string{
			"Backup Database",
			"Restore Database",
			"Wipe Database",
			"Open DB Shell",
			"List Backups",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			ActionDBBackup(cfg)
		case 1:
			ActionDBRestore(cfg)
		case 2:
			ActionDBWipe(cfg)
		case 3:
			ActionDBShell(cfg)
		case 4:
			ActionDBListBackups(cfg)
		}
	}
}

func menuMonitoring(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Monitoring")

		choice, err := ui.Menu("Monitoring", []string{
			"Health Check",
			"System Info",
			"Disk Usage",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			ActionHealthCheck(cfg)
		case 1:
			ActionSystemInfo(cfg)
		case 2:
			ActionDiskUsage(cfg)
		}
	}
}

func menuSecurity(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Security")

		choice, err := ui.Menu("Security", []string{
			"Reset Admin Password",
			"Create Admin User",
		})
		if err != nil || choice < 0 {
			return
		}

		switch choice {
		case 0:
			ActionResetAdminPassword(cfg)
		case 1:
			ActionCreateAdminUser(cfg)
		}
	}
}

func menuMaintenance(cfg *Config) {
	for {
		ui.ClearScreen()
		ui.DrawBox("Maintenance")

		var options []string
		if cfg.IsDocker() {
			options = []string{
				"Clean Docker Logs",
				"Clean Old Backups",
				"Docker System Prune",
			}
		} else {
			options = []string{
				"Clean Old Backups",
				"Clean Journal Logs",
			}
		}

		choice, err := ui.Menu("Maintenance", options)
		if err != nil || choice < 0 {
			return
		}

		label := options[choice]
		switch label {
		case "Clean Docker Logs":
			ActionCleanDockerLogs(cfg)
		case "Clean Old Backups":
			ActionCleanOldBackups(cfg)
		case "Docker System Prune":
			ActionDockerPrune(cfg)
		case "Clean Journal Logs":
			ActionCleanJournalLogs(cfg)
		}
	}
}

// detectVersion returns the current installed version string.
func detectVersion(cfg *Config) string {
	// Check .version file first (written by auto-update)
	if data, err := os.ReadFile(filepath.Join(cfg.InstallDir, ".version")); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}
	// Fall back to git describe
	if out, err := exec.Command("git", "-C", cfg.ProjectDir, "describe", "--tags", "--always").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	return ""
}

// detectUpdateBehind returns how many commits the local branch is behind remote.
// Returns 0 on error or when offline.
func detectUpdateBehind(cfg *Config) int {
	if cfg.OfflineMode {
		return 0
	}
	branchOut, err := exec.Command("git", "-C", cfg.ProjectDir, "branch", "--show-current").Output()
	if err != nil {
		return 0
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		return 0
	}
	// Fetch with a short timeout — don't block the menu
	fetchCmd := exec.Command("git", "-C", cfg.ProjectDir, "fetch", "origin", branch)
	fetchCmd.Run() // ignore errors

	countOut, err := exec.Command("git", "-C", cfg.ProjectDir, "rev-list", "HEAD..origin/"+branch, "--count").Output()
	if err != nil {
		return 0
	}
	var count int
	fmt.Sscanf(strings.TrimSpace(string(countOut)), "%d", &count)
	return count
}
