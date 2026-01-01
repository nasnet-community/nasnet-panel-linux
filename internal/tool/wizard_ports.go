package tool

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// updateEnvValue reads the env file at envFile, replaces the value for key
// (preserving quote style), and writes the file back. If the key is not found
// the line "key=newValue" is appended. Returns an error on I/O failure.
func updateEnvValue(envFile, key, newValue string) error {
	data, err := os.ReadFile(envFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			lines[i] = key + "=" + newValue
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+newValue)
	}

	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")), 0600)
}

// WizardChangePorts lets the operator change the APP_PORT value in the .env
// file and optionally restart services afterwards.
func WizardChangePorts(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("Change Ports")

	if _, err := os.Stat(cfg.EnvFile); err != nil {
		ui.StepFail("No .env file found — run Fresh Install first")
		ui.PressAnyKey()
		return
	}

	currentPort := ReadEnvValue("APP_PORT", cfg.EnvFile)
	if currentPort == "" {
		currentPort = cfg.AppPort
	}
	if currentPort == "" {
		currentPort = "9761"
	}

	ui.StepInfo("Current APP_PORT: " + currentPort)
	fmt.Println()

	newPort, err := ui.InputStringDefault("New APP_PORT", currentPort)
	if err != nil {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}
	newPort = strings.TrimSpace(newPort)
	if newPort == "" {
		newPort = currentPort
	}

	// Validate
	portNum, convErr := strconv.Atoi(newPort)
	if convErr != nil || portNum < 1 || portNum > 65535 {
		ui.StepFail("Invalid port number: " + newPort)
		ui.PressAnyKey()
		return
	}

	if newPort == currentPort {
		ui.StepInfo("No changes — port remains the same")
		ui.PressAnyKey()
		return
	}

	// Derive new URL values by substituting the old port in any URL that
	// contains it.
	oldBaseURL := ReadEnvValue("APP_BASE_URL", cfg.EnvFile)
	oldSubURL := ReadEnvValue("SUB_PANEL_URL", cfg.EnvFile)
	oldPromTarget := ReadEnvValue("PROMETHEUS_TARGET", cfg.EnvFile)

	newBaseURL := strings.ReplaceAll(oldBaseURL, ":"+currentPort, ":"+newPort)
	newSubURL := strings.ReplaceAll(oldSubURL, ":"+currentPort, ":"+newPort)
	newPromTarget := strings.ReplaceAll(oldPromTarget, ":"+currentPort, ":"+newPort)

	// ── Review ────────────────────────────────────────────────────────────
	ui.DrawHeader("Review Changes")
	reviewHeaders := []string{"Variable", "New Value"}
	reviewRows := [][]string{
		{"APP_PORT", currentPort + " → " + newPort},
	}
	if newBaseURL != oldBaseURL {
		reviewRows = append(reviewRows, []string{"APP_BASE_URL", newBaseURL})
	}
	if newSubURL != oldSubURL {
		reviewRows = append(reviewRows, []string{"SUB_PANEL_URL", newSubURL})
	}
	if newPromTarget != oldPromTarget {
		reviewRows = append(reviewRows, []string{"PROMETHEUS_TARGET", newPromTarget})
	}
	ui.Table(reviewHeaders, reviewRows)
	fmt.Println()

	ok, confirmErr := ui.Confirm("Apply port changes?")
	if confirmErr != nil || !ok {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// ── Update .env ───────────────────────────────────────────────────────
	ui.DrawHeader("Updating Configuration")

	if err := updateEnvValue(cfg.EnvFile, "APP_PORT", newPort); err != nil {
		ui.StepFail("Failed to update APP_PORT: " + err.Error())
		ui.PressAnyKey()
		return
	}
	if newBaseURL != oldBaseURL {
		if err := updateEnvValue(cfg.EnvFile, "APP_BASE_URL", newBaseURL); err != nil {
			ui.StepWarn("Failed to update APP_BASE_URL: " + err.Error())
		}
	}
	if newSubURL != oldSubURL {
		if err := updateEnvValue(cfg.EnvFile, "SUB_PANEL_URL", newSubURL); err != nil {
			ui.StepWarn("Failed to update SUB_PANEL_URL: " + err.Error())
		}
	}
	if newPromTarget != oldPromTarget {
		if err := updateEnvValue(cfg.EnvFile, "PROMETHEUS_TARGET", newPromTarget); err != nil {
			ui.StepWarn("Failed to update PROMETHEUS_TARGET: " + err.Error())
		}
	}
	ui.StepOk(".env updated")

	if err := cfg.SyncEnvToInstallDir(); err != nil {
		ui.StepWarn("Could not sync .env to install dir: " + err.Error())
	}

	// ── Offer restart ─────────────────────────────────────────────────────
	restart, _ := ui.Confirm("Restart services to apply changes?")
	if restart {
		ui.DrawHeader("Restarting")
		if cfg.IsDocker() {
			if err := ui.Spinner("Recreating containers",
				cfg.DockerCompose("up", "-d", "--force-recreate")); err != nil {
				ui.StepFail("Failed to restart services")
			} else {
				ui.StepOk("Services restarted")
			}
		} else {
			ui.StepInfo("Restarting backend service...")
			if err := cfg.Systemctl("restart", DefaultBackendService).Run(); err != nil {
				ui.StepFail("Backend restart failed")
			} else {
				ui.StepOk("Backend restarted")
			}
		}
	}

	fmt.Println()
	ui.DrawHeader("Port Change Complete")
	ui.Table([]string{"Service", "Port"}, [][]string{{"APP_PORT", newPort}})
	ui.PressAnyKey()
}
