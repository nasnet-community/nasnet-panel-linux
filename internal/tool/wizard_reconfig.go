package tool

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// WizardReconfigure allows the user to modify the deployment configuration while
// preserving existing secrets (JWT key, DB password, admin hash).
func WizardReconfigure(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("nasnet-panel Reconfigure")

	if _, err := os.Stat(cfg.EnvFile); err != nil {
		ui.StepFail("No .env file found — run Fresh Install first")
		ui.PressAnyKey()
		return
	}

	// Load existing secret values from the .env file.
	existingJWT := ReadEnvValue("JWT_SECRET_KEY", cfg.EnvFile)
	existingDBPass := ReadEnvValue("DB_PASSWORD", cfg.EnvFile)
	existingAdminHash := ReadEnvValue("ADMIN_PASSWORD_HASH", cfg.EnvFile)
	existingBotToken := ReadEnvValue("TELEGRAM_BOT_TOKEN", cfg.EnvFile)
	existingAdminIDs := ReadEnvValue("ADMIN_IDS", cfg.EnvFile)
	telegramEnabled := ReadEnvValue("TELEGRAM_ENABLED", cfg.EnvFile)
	deployMode := ReadEnvValue("DEPLOY_MODE", cfg.EnvFile)
	dbDriver := ReadEnvValue("DB_DRIVER", cfg.EnvFile)

	if telegramEnabled == "" {
		telegramEnabled = "false"
	}
	if deployMode == "" {
		deployMode = "docker"
	}
	if dbDriver == "" {
		dbDriver = "postgres"
	}

	ui.DrawHeader("Reconfigure — Secrets Preserved")
	ui.StepOk("JWT secret, DB password, and admin hash will be kept")
	ui.StepInfo("Deployment mode: " + deployMode)
	ui.StepInfo("Database engine: " + dbDriver)
	fmt.Println()

	// ── Access Mode ─────────────────────────────────────────────────────────
	ui.DrawHeader("Access Mode")

	appPort := ReadEnvValue("APP_PORT", cfg.EnvFile)
	if appPort == "" {
		appPort = RandomPort()
	}

	// Use shared wizardVars + wizardPromptAccessMode from wizard_install.go
	wv := &wizardVars{
		DeployMode:      deployMode,
		DBDriver:        dbDriver,
		AppPort:         appPort,
		TelegramEnabled: telegramEnabled,
		BotToken:        existingBotToken,
		AdminIDs:        existingAdminIDs,
		JWTSecret:       existingJWT,
		DBPassword:      existingDBPass,
		AdminHash:       existingAdminHash,
	}

	if !wizardPromptAccessMode(cfg, wv) {
		ui.PressAnyKey()
		return
	}

	// ── Telegram settings ─────────────────────────────────────────────────
	fmt.Println()
	fmt.Printf("  Telegram bot: %s\n", telegramEnabled)

	if telegramEnabled == "true" {
		maskedToken := MaskSecret(existingBotToken, 8)
		fmt.Printf("  Current bot token: %s\n", ui.StyleDim.Render(maskedToken))
		changeToken, err := ui.Confirm("Change Telegram bot token?")
		if err == nil && changeToken {
			newToken, errT := ui.InputString("New bot token", "")
			if errT == nil && newToken != "" {
				wv.BotToken = newToken
			}
		}
	}

	toggleTelegram, err := ui.Confirm(fmt.Sprintf("Toggle Telegram bot (currently: %s)?", telegramEnabled))
	if err == nil && toggleTelegram {
		if telegramEnabled == "true" {
			telegramEnabled = "false"
			ui.StepOk("Telegram bot will be disabled")
		} else {
			telegramEnabled = "true"
			if wv.BotToken == "" {
				token, errT := ui.InputString("Telegram bot token (from @BotFather)", "")
				if errT != nil || token == "" {
					ui.StepFail("Bot token is required when enabling Telegram")
					ui.PressAnyKey()
					return
				}
				wv.BotToken = token
			}
			if wv.AdminIDs == "" {
				ids, errI := ui.InputString("Admin Telegram ID (your numeric ID)", "")
				if errI != nil || ids == "" {
					ui.StepFail("A valid numeric Telegram ID is required")
					ui.PressAnyKey()
					return
				}
				wv.AdminIDs = ids
			}
			ui.StepOk("Telegram bot will be enabled")
		}
	}
	wv.TelegramEnabled = telegramEnabled

	// ── Review ────────────────────────────────────────────────────────────
	ui.DrawHeader("Review Changes")

	reviewBotToken := "(disabled)"
	if telegramEnabled == "true" && wv.BotToken != "" {
		reviewBotToken = MaskSecret(wv.BotToken, 8)
	}

	adminIDsDisplay := wv.AdminIDs
	if adminIDsDisplay == "" {
		adminIDsDisplay = "(none)"
	}
	cookieDomainDisplay := wv.CookieDomain
	if cookieDomainDisplay == "" {
		cookieDomainDisplay = "(empty)"
	}

	ui.Table(
		[]string{"Setting", "Value"},
		[][]string{
			{"Deploy", deployMode},
			{"DB Engine", dbDriver},
			{"Mode", wv.Mode},
			{"APP_BASE_URL", wv.AppBaseURL},
			{"SUB_PANEL_URL", wv.SubPanelURL},
			{"JWT_COOKIE_DOMAIN", cookieDomainDisplay},
			{"JWT_COOKIE_SECURE", wv.CookieSecure},
			{"ACME_ENABLED", wv.AcmeEnabled},
			{"ACME_STAGING", wv.AcmeStaging},
			{"TELEGRAM_ENABLED", telegramEnabled},
			{"BOT_TOKEN", reviewBotToken},
			{"ADMIN_IDS", adminIDsDisplay},
			{"Secrets", ui.StyleDim.Render("(preserved from existing .env)")},
		},
	)
	fmt.Println()

	apply, err2 := ui.Confirm("Apply these changes?")
	if err2 != nil || !apply {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// ── Write .env ────────────────────────────────────────────────────────
	comment := fmt.Sprintf("Reconfigured by nasnet-tool wizard on %s", time.Now().Format("2006-01-02 15:04:05"))
	wizardWriteEnv(cfg, wv, deployMode, comment)

	// Sync .env to install dir for systemd
	_ = cfg.SyncEnvToInstallDir()

	// ── Rebuild & Restart ─────────────────────────────────────────────────
	ui.DrawHeader("Rebuilding & Restarting")

	if deployMode == "docker" {
		if err3 := ui.RunLogged("Recreating containers",
			cfg.DockerCompose("up", "-d", "--force-recreate")); err3 != nil {
			ui.StepFail("Failed to restart services")
		}
	} else {
		// Systemd: rebuild binary, deploy, restart services.
		buildCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make build", cfg.ProjectDir))
		if err3 := ui.RunLogged("Rebuilding nasnet-panel", buildCmd); err3 != nil {
			ui.StepFail("Build failed")
			ui.PressAnyKey()
			return
		}
		ui.StepOk("Binary rebuilt")

		ui.StepInfo("Stopping services...")
		_ = exec.Command("sudo", "systemctl", "stop", DefaultBackendService).Run()
		ui.StepOk("Backend stopped")

		deployArtifacts(cfg)

		ui.StepInfo("Starting services...")
		if startErr := exec.Command("sudo", "systemctl", "start", DefaultBackendService).Run(); startErr != nil {
			ui.StepFail("Backend start failed")
		} else {
			ui.StepOk("Backend started")
		}
	}

	fmt.Println()
	ui.StepOk("Reconfiguration complete")
	ui.PressAnyKey()
}
