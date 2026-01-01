package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// requireSystemd checks that systemctl is available on the system.
func requireSystemd() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		ui.StepFail("systemctl not found — systemd is not available on this system")
		return fmt.Errorf("systemctl not found")
	}
	return nil
}

// detectSystemdServices finds which systemd services are managed by nasnet-panel.
// Returns the service names and deploy type ("wizard" or "compose").
func detectSystemdServices(cfg *Config) (services []string, deployType string, err error) {
	// Wizard-created service (bare-metal deploy)
	beUnit := fmt.Sprintf("/etc/systemd/system/%s.service", DefaultBackendService)
	if _, statErr := os.Stat(beUnit); statErr == nil {
		return []string{DefaultBackendService}, "wizard", nil
	}

	// Docker Compose wrapper service
	composeUnit := "/etc/systemd/system/nasnet-panel.service"
	if _, statErr := os.Stat(composeUnit); statErr == nil {
		return []string{"nasnet-panel"}, "compose", nil
	}

	return nil, "", fmt.Errorf("no nasnet-panel systemd services found")
}

// systemdStatusInline renders a table showing active/enabled state for each service.
func systemdStatusInline(cfg *Config) {
	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepWarn("No nasnet-panel systemd services found")
		return
	}

	headers := []string{"Service", "Active", "Boot"}
	rows := make([][]string, 0, len(services))

	for _, svc := range services {
		activeOut, _ := exec.Command("systemctl", "is-active", svc).Output()
		activeState := strings.TrimSpace(string(activeOut))
		if activeState == "" {
			activeState = "unknown"
		}

		enabledOut, _ := exec.Command("systemctl", "is-enabled", svc).Output()
		enabledState := strings.TrimSpace(string(enabledOut))
		if enabledState == "" {
			enabledState = "unknown"
		}

		rows = append(rows, []string{svc, colorActive(activeState), colorEnabled(enabledState)})
	}

	ui.Table(headers, rows)
}

func colorActive(state string) string {
	switch state {
	case "active":
		return ui.StyleSuccess.Render("● active")
	case "inactive":
		return ui.StyleDim.Render("● inactive")
	case "failed":
		return ui.StyleError.Render("● failed")
	default:
		return ui.StyleWarning.Render("● " + state)
	}
}

func colorEnabled(state string) string {
	switch state {
	case "enabled":
		return ui.StyleSuccess.Render("enabled")
	case "disabled":
		return ui.StyleDim.Render("disabled")
	default:
		return ui.StyleWarning.Render(state)
	}
}

// ActionSystemdStatus shows the current status of all managed systemd services.
func ActionSystemdStatus(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Systemd Service Status")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepWarn("No nasnet-panel systemd services found")
		ui.StepInfo("Use the setup wizard to install with systemd, or 'Install Service' for Docker Compose wrapper")
		ui.PressAnyKey()
		return
	}

	systemdStatusInline(cfg)
	fmt.Println()

	ui.DrawSeparator()
	fmt.Println()
	ui.StepInfo("Full status output:")
	fmt.Println()

	for _, svc := range services {
		fmt.Printf("  %s\n", ui.StyleTitle.Render("── "+svc+" ──"))
		statusCmd := exec.Command("systemctl", "status", svc, "--no-pager")
		statusCmd.Stdout = os.Stdout
		statusCmd.Stderr = os.Stdout
		statusCmd.Run() //nolint:errcheck
		fmt.Println()
	}

	ui.PressAnyKey()
}

// ActionSystemdInstall creates and installs a Docker Compose wrapper systemd unit file.
func ActionSystemdInstall(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Install Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	// If wizard services already exist, inform the user
	services, deployType, err := detectSystemdServices(cfg)
	if err == nil && deployType == "wizard" {
		ui.StepOk("Wizard-managed services are already installed:")
		for _, svc := range services {
			ui.StepInfo(fmt.Sprintf("  /etc/systemd/system/%s.service", svc))
		}
		fmt.Println()
		ui.StepInfo("Use 'View Status' to check them, or 'Delete Service' to remove")
		ui.PressAnyKey()
		return
	}

	// Check if compose wrapper already exists
	composeUnitFile := "/etc/systemd/system/nasnet-panel.service"
	if _, statErr := os.Stat(composeUnitFile); statErr == nil {
		ui.StepWarn(fmt.Sprintf("Unit file already exists at %s", composeUnitFile))
		overwrite, confirmErr := ui.Confirm("Overwrite existing unit file?")
		if confirmErr != nil || !overwrite {
			ui.StepInfo("Cancelled")
			ui.PressAnyKey()
			return
		}
	}

	// Get docker binary path
	composeBin, lookErr := exec.LookPath("docker")
	if lookErr != nil {
		ui.StepFail("docker not found — cannot create service")
		ui.PressAnyKey()
		return
	}

	// Build compose args
	composeArgs := fmt.Sprintf("compose -f %s", cfg.ComposeFile)
	if cfg.IsSQLite() {
		composeArgs = fmt.Sprintf("%s -f %s", composeArgs, cfg.SQLiteComposeFile)
	}
	composeArgs = fmt.Sprintf("%s --project-directory %s", composeArgs, cfg.ProjectDir)

	ui.StepInfo(fmt.Sprintf("Creating unit file: %s", composeUnitFile))

	unitContent := fmt.Sprintf(`[Unit]
Description=nasnet-panel (Docker Compose)
Documentation=https://github.com/nasnet-community/nasnet-panel-linux
After=network-online.target docker.service
Requires=docker.service
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=%s
ExecStart=%s %s up -d
ExecStop=%s %s down
ExecReload=%s %s restart
TimeoutStartSec=120
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
`, cfg.ProjectDir,
		composeBin, composeArgs,
		composeBin, composeArgs,
		composeBin, composeArgs)

	teeCmd := exec.Command("sudo", "tee", composeUnitFile)
	teeCmd.Stdin = strings.NewReader(unitContent)
	teeCmd.Stdout = os.Stdout
	if teeErr := teeCmd.Run(); teeErr != nil {
		ui.StepFail("Failed to write unit file (need sudo?)")
		ui.PressAnyKey()
		return
	}
	ui.StepOk("Unit file written")

	ui.StepInfo("Reloading systemd daemon...")
	reloadCmd := exec.Command("sudo", "systemctl", "daemon-reload")
	if reloadErr := reloadCmd.Run(); reloadErr != nil {
		ui.StepFail("daemon-reload failed")
		ui.PressAnyKey()
		return
	}
	ui.StepOk("Daemon reloaded")

	fmt.Println()
	systemdStatusInline(cfg)
	fmt.Println()
	ui.StepInfo("Use 'Enable Service' to start on boot, then 'Start Service' to run now")
	ui.PressAnyKey()
}

// ActionSystemdStart starts all managed systemd services.
func ActionSystemdStart(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Start Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepFail("No services installed — run 'Install Service' or the setup wizard first")
		ui.PressAnyKey()
		return
	}

	for _, svc := range services {
		ui.StepInfo(fmt.Sprintf("Starting %s...", svc))
		if startErr := exec.Command("sudo", "systemctl", "start", svc).Run(); startErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to start %s", svc))
		} else {
			ui.StepOk(fmt.Sprintf("%s started", svc))
		}
	}

	fmt.Println()
	systemdStatusInline(cfg)
	ui.PressAnyKey()
}

// ActionSystemdStop stops all managed systemd services.
func ActionSystemdStop(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Stop Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepFail("No services installed")
		ui.PressAnyKey()
		return
	}

	svcNames := strings.Join(services, ", ")
	confirmed, confirmErr := ui.Confirm(fmt.Sprintf("Stop %s?", svcNames))
	if confirmErr != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	for _, svc := range services {
		ui.StepInfo(fmt.Sprintf("Stopping %s...", svc))
		if stopErr := exec.Command("sudo", "systemctl", "stop", svc).Run(); stopErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to stop %s", svc))
		} else {
			ui.StepOk(fmt.Sprintf("%s stopped", svc))
		}
	}

	fmt.Println()
	systemdStatusInline(cfg)
	ui.PressAnyKey()
}

// ActionSystemdEnable enables all managed systemd services to start on boot.
func ActionSystemdEnable(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Enable Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepFail("No services installed — run 'Install Service' or the setup wizard first")
		ui.PressAnyKey()
		return
	}

	for _, svc := range services {
		ui.StepInfo(fmt.Sprintf("Enabling %s to start on boot...", svc))
		if enableErr := exec.Command("sudo", "systemctl", "enable", svc).Run(); enableErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to enable %s", svc))
		} else {
			ui.StepOk(fmt.Sprintf("%s enabled", svc))
		}
	}

	fmt.Println()
	systemdStatusInline(cfg)
	ui.PressAnyKey()
}

// ActionSystemdDisable disables all managed systemd services from starting on boot.
func ActionSystemdDisable(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Disable Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, _, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepFail("No services installed")
		ui.PressAnyKey()
		return
	}

	svcNames := strings.Join(services, ", ")
	confirmed, confirmErr := ui.Confirm(fmt.Sprintf("Disable %s from starting on boot?", svcNames))
	if confirmErr != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	for _, svc := range services {
		ui.StepInfo(fmt.Sprintf("Disabling %s...", svc))
		if disableErr := exec.Command("sudo", "systemctl", "disable", svc).Run(); disableErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to disable %s", svc))
		} else {
			ui.StepOk(fmt.Sprintf("%s disabled", svc))
		}
	}

	fmt.Println()
	systemdStatusInline(cfg)
	ui.PressAnyKey()
}

// ActionSystemdDelete stops, disables, and removes all managed systemd unit files.
func ActionSystemdDelete(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Delete Systemd Service")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, deployType, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepInfo("No services installed — nothing to delete")
		ui.PressAnyKey()
		return
	}

	fmt.Println()
	systemdStatusInline(cfg)
	fmt.Println()

	svcNames := strings.Join(services, ", ")
	confirmed, confirmErr := ui.ConfirmDangerous(
		fmt.Sprintf("This will stop, disable, and remove: %s", svcNames),
		"DELETE",
	)
	if confirmErr != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	for _, svc := range services {
		unitFile := fmt.Sprintf("/etc/systemd/system/%s.service", svc)

		// Stop if running
		activeOut, _ := exec.Command("systemctl", "is-active", svc).Output()
		activeState := strings.TrimSpace(string(activeOut))
		if activeState == "active" {
			ui.StepInfo(fmt.Sprintf("Stopping %s...", svc))
			exec.Command("sudo", "systemctl", "stop", svc).Run() //nolint:errcheck
			ui.StepOk(fmt.Sprintf("%s stopped", svc))
		}

		// Disable if enabled
		enabledOut, _ := exec.Command("systemctl", "is-enabled", svc).Output()
		enabledState := strings.TrimSpace(string(enabledOut))
		if enabledState == "enabled" {
			ui.StepInfo(fmt.Sprintf("Disabling %s...", svc))
			exec.Command("sudo", "systemctl", "disable", svc).Run() //nolint:errcheck
			ui.StepOk(fmt.Sprintf("%s disabled", svc))
		}

		// Remove unit file
		if _, statErr := os.Stat(unitFile); statErr == nil {
			ui.StepInfo(fmt.Sprintf("Removing %s...", unitFile))
			if rmErr := exec.Command("sudo", "rm", "-f", unitFile).Run(); rmErr != nil {
				ui.StepFail(fmt.Sprintf("Failed to remove %s", unitFile))
			} else {
				ui.StepOk("Unit file removed")
			}
		}
	}

	ui.StepInfo("Reloading systemd daemon...")
	exec.Command("sudo", "systemctl", "daemon-reload").Run() //nolint:errcheck
	ui.StepOk("Daemon reloaded")

	fmt.Println()
	ui.StepOk("Systemd service(s) completely removed")

	if deployType == "compose" {
		ui.StepInfo("Containers are still present — use 'Docker Services > Stop' to remove them")
	}

	// Offer to clean up install directory for wizard deployments
	if deployType == "wizard" {
		if _, statErr := os.Stat(cfg.InstallDir); statErr == nil {
			fmt.Println()
			remove, confirmErr2 := ui.Confirm(fmt.Sprintf("Also remove installed files at %s?", cfg.InstallDir))
			if confirmErr2 == nil && remove {
				exec.Command("sudo", "rm", "-rf", cfg.InstallDir).Run() //nolint:errcheck
				ui.StepOk(fmt.Sprintf("Removed %s", cfg.InstallDir))
			} else {
				ui.StepInfo(fmt.Sprintf("Keeping %s (you can remove it manually)", cfg.InstallDir))
			}
		}
	}

	ui.PressAnyKey()
}

// ActionSystemdRebuild rebuilds project artifacts and redeploys them for wizard (bare-metal) deployments.
func ActionSystemdRebuild(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("Rebuild & Redeploy")

	if err := requireSystemd(); err != nil {
		ui.PressAnyKey()
		return
	}

	services, deployType, err := detectSystemdServices(cfg)
	if err != nil {
		ui.StepInfo("No systemd services installed — nothing to rebuild")
		ui.StepInfo("Use the setup wizard or 'Install Service' first")
		ui.PressAnyKey()
		return
	}

	if deployType != "wizard" {
		ui.StepWarn("Rebuild is only available for bare-metal (wizard) deployments")
		ui.StepInfo("For Docker Compose wrapper services, use the Update option instead")
		ui.PressAnyKey()
		return
	}

	// Show current state
	ui.DrawHeader("Current Status")
	systemdStatusInline(cfg)

	backendBinary := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
	if info, statErr := os.Stat(backendBinary); statErr == nil {
		ui.StepInfo(fmt.Sprintf("Binary last built: %s", info.ModTime().Format("2006-01-02 15:04:05")))
	} else {
		ui.StepWarn(fmt.Sprintf("No existing binary found at %s", backendBinary))
	}

	// Let user choose what to rebuild
	fmt.Println()
	choice, menuErr := ui.Menu("What to rebuild?", []string{
		"Everything (backend + agents + frontend)",
		"Backend only",
		"Agents only",
		"Frontend (rebuild binary with embedded SPA)",
		"Backend + Agents",
	})
	if menuErr != nil || choice == -1 {
		return
	}

	doBackend := false
	doAgents := false
	doWebPanel := false

	switch choice {
	case 0:
		doBackend, doAgents, doWebPanel = true, true, true
	case 1:
		doBackend = true
	case 2:
		doAgents = true
	case 3:
		doWebPanel = true
	case 4:
		doBackend, doAgents = true, true
	}

	fmt.Println()
	confirmed, confirmErr := ui.Confirm("Rebuild and redeploy? Services will be restarted.")
	if confirmErr != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// Build phase
	ui.DrawHeader("Building")
	buildFailed := false

	// Frontend must be built before backend (Go binary embeds web-panel/dist via go:embed)
	if (doBackend || doWebPanel) && !buildFailed {
		frontendCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s/web-panel' && pnpm install && pnpm build", cfg.ProjectDir))
		if runErr := ui.RunLogged("Building frontend", frontendCmd); runErr != nil {
			ui.StepFail("Frontend build failed")
			buildFailed = true
		} else {
			ui.StepOk("Frontend built")
		}
	}

	if doBackend && !buildFailed {
		cgoEnabled := "0"
		if cfg.IsSQLite() {
			cgoEnabled = "1"
		}

		modCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && go mod download", cfg.ProjectDir))
		if runErr := ui.RunLogged("Downloading Go modules", modCmd); runErr != nil {
			ui.StepFail("Failed to download Go modules")
			buildFailed = true
		}

		if !buildFailed {
			buildCmd := exec.Command("bash", "-c",
				fmt.Sprintf("cd '%s' && CGO_ENABLED=%s go build -ldflags='-w -s' -o nasnet-panel .",
					cfg.ProjectDir, cgoEnabled))
			if runErr := ui.RunLogged("Building backend binary", buildCmd); runErr != nil {
				ui.StepFail("Backend build failed")
				buildFailed = true
			} else {
				ui.StepOk("Backend binary built")
			}
		}
	}

	if doAgents && !buildFailed {
		agentCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make build-agent", cfg.ProjectDir))
		if runErr := ui.RunLogged("Building agent binaries", agentCmd); runErr != nil {
			ui.StepFail("Agent binary build failed")
			buildFailed = true
		} else {
			ui.StepOk("Agent binaries built")
		}
	}

	// If only webpanel was selected (not backend), still rebuild Go binary to embed new frontend
	if doWebPanel && !doBackend && !buildFailed {
		cgoEnabled := "0"
		if cfg.IsSQLite() {
			cgoEnabled = "1"
		}
		rebuildCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && CGO_ENABLED=%s go build -ldflags='-w -s' -o nasnet-panel .",
				cfg.ProjectDir, cgoEnabled))
		if runErr := ui.RunLogged("Rebuilding backend binary with new frontend", rebuildCmd); runErr != nil {
			ui.StepFail("Build failed")
			buildFailed = true
		} else {
			ui.StepOk("Binary rebuilt with embedded frontend")
		}
	}

	if buildFailed {
		fmt.Println()
		ui.StepFail("Build failed — aborting deploy")
		ui.PressAnyKey()
		return
	}

	// Stop services
	fmt.Println()
	ui.DrawHeader("Deploying")
	ui.StepInfo("Stopping services...")
	for _, svc := range services {
		if stopErr := exec.Command("sudo", "systemctl", "stop", svc).Run(); stopErr == nil {
			ui.StepOk(fmt.Sprintf("%s stopped", svc))
		}
	}

	// Deploy artifacts
	deployArtifacts(cfg)

	// Start services
	fmt.Println()
	ui.StepInfo("Starting services...")
	for _, svc := range services {
		if startErr := exec.Command("sudo", "systemctl", "start", svc).Run(); startErr != nil {
			ui.StepFail(fmt.Sprintf("%s start failed", svc))
		} else {
			ui.StepOk(fmt.Sprintf("%s started", svc))
		}
	}

	// Health check
	fmt.Println()
	appPort := cfg.AppPort
	if appPort == "" {
		appPort = "9761"
	}
	ui.StepInfo("Waiting for backend to become healthy...")

	maxRetries := 15
	healthy := false
	for i := 0; i < maxRetries; i++ {
		checkCmd := exec.Command("curl", "-sf", "--max-time", "3",
			fmt.Sprintf("http://localhost:%s/health/ready", appPort))
		if checkCmd.Run() == nil {
			healthy = true
			break
		}
		time.Sleep(2 * time.Second)
		fmt.Printf("\r  %s Waiting... (%ds/%ds)",
			ui.StyleCyan.Render("⠋"),
			(i+1)*2, maxRetries*2)
	}
	fmt.Printf("\r%60s\r", "")

	// Final status
	fmt.Println()
	ui.DrawHeader("Rebuild Complete")
	systemdStatusInline(cfg)

	if healthy {
		ui.StepOk("Backend health check passed")
	} else {
		ui.StepWarn(fmt.Sprintf("Backend health check failed — check logs: journalctl -u %s -n 30",
			DefaultBackendService))
	}

	ui.PressAnyKey()
}

// deployArtifacts copies built binaries into the install directory.
func deployArtifacts(cfg *Config) {
	ui.StepInfo(fmt.Sprintf("Deploying to %s...", cfg.InstallDir))

	// Create directory structure
	for _, dir := range []string{
		filepath.Join(cfg.InstallDir, "bin"),
		filepath.Join(cfg.InstallDir, "bin", "agent"),
		filepath.Join(cfg.InstallDir, "data", "backups"),
	} {
		exec.Command("sudo", "mkdir", "-p", dir).Run() //nolint:errcheck
	}

	// Copy backend binary
	srcBin := filepath.Join(cfg.ProjectDir, "nasnet-panel")
	dstBin := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
	exec.Command("sudo", "cp", srcBin, dstBin).Run()  //nolint:errcheck
	exec.Command("sudo", "chmod", "+x", dstBin).Run() //nolint:errcheck

	// Copy agent binaries if they exist
	agentSrcDir := filepath.Join(cfg.ProjectDir, "bin", "agent")
	if entries, readErr := os.ReadDir(agentSrcDir); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			src := filepath.Join(agentSrcDir, entry.Name())
			dst := filepath.Join(cfg.InstallDir, "bin", "agent", entry.Name())
			exec.Command("sudo", "cp", src, dst).Run()     //nolint:errcheck
			exec.Command("sudo", "chmod", "+x", dst).Run() //nolint:errcheck
		}
		ui.StepOk("Agent binaries deployed")
	}

	ui.StepOk(fmt.Sprintf("Deployed to %s", cfg.InstallDir))
}
