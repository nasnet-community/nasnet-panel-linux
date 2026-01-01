package tool

import (
	"fmt"
	"os"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// WizardChangeURLs lets the operator update APP_BASE_URL and SUB_PANEL_URL in
// the .env file, then optionally restarts services.
func WizardChangeURLs(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("Change URLs")

	if _, err := os.Stat(cfg.EnvFile); err != nil {
		ui.StepFail("No .env file found — run Fresh Install first")
		ui.PressAnyKey()
		return
	}

	currentBaseURL := ReadEnvValue("APP_BASE_URL", cfg.EnvFile)
	currentPanelURL := ReadEnvValue("SUB_PANEL_URL", cfg.EnvFile)

	// ── Show current values ───────────────────────────────────────────────
	ui.DrawHeader("Current URLs")
	ui.Table(
		[]string{"Variable", "Value"},
		[][]string{
			{"APP_BASE_URL", currentBaseURL},
			{"SUB_PANEL_URL", currentPanelURL},
		},
	)
	fmt.Println()

	// ── Collect new values ────────────────────────────────────────────────
	newBaseURL, err := ui.InputStringDefault("APP_BASE_URL", currentBaseURL)
	if err != nil {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	newPanelURL, err := ui.InputStringDefault("SUB_PANEL_URL", currentPanelURL)
	if err != nil {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	if newBaseURL == currentBaseURL && newPanelURL == currentPanelURL {
		ui.StepInfo("No changes — URLs remain the same")
		ui.PressAnyKey()
		return
	}

	// ── Review ────────────────────────────────────────────────────────────
	ui.DrawHeader("Review Changes")
	reviewRows := [][]string{}
	if newBaseURL != currentBaseURL {
		reviewRows = append(reviewRows, []string{"APP_BASE_URL", currentBaseURL + " → " + newBaseURL})
	}
	if newPanelURL != currentPanelURL {
		reviewRows = append(reviewRows, []string{"SUB_PANEL_URL", currentPanelURL + " → " + newPanelURL})
	}
	ui.Table([]string{"Variable", "New Value"}, reviewRows)
	fmt.Println()

	ok, confirmErr := ui.Confirm("Apply URL changes?")
	if confirmErr != nil || !ok {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// ── Update .env ───────────────────────────────────────────────────────
	ui.DrawHeader("Updating Configuration")

	if newBaseURL != currentBaseURL {
		if err := updateEnvValue(cfg.EnvFile, "APP_BASE_URL", newBaseURL); err != nil {
			ui.StepFail("Failed to update APP_BASE_URL: " + err.Error())
			ui.PressAnyKey()
			return
		}
	}
	if newPanelURL != currentPanelURL {
		if err := updateEnvValue(cfg.EnvFile, "SUB_PANEL_URL", newPanelURL); err != nil {
			ui.StepFail("Failed to update SUB_PANEL_URL: " + err.Error())
			ui.PressAnyKey()
			return
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
			if err := ui.Spinner("Restarting backend",
				cfg.DockerCompose("restart", "app")); err != nil {
				ui.StepFail("Failed to restart backend")
			} else {
				ui.StepOk("Backend restarted")
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
	ui.DrawHeader("URL Change Complete")
	ui.Table(
		[]string{"Variable", "Value"},
		[][]string{
			{"APP_BASE_URL", newBaseURL},
			{"SUB_PANEL_URL", newPanelURL},
		},
	)
	ui.PressAnyKey()
}
