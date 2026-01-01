package tool

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// wizardVars holds all state gathered during the installation wizard.
type wizardVars struct {
	DeployMode      string // "docker" or "systemd"
	DBDriver        string // "postgres" or "sqlite"
	Mode            string // "domain" or "ip"
	Domain          string
	BasePath        string
	AppBaseURL      string
	SubPanelURL     string
	CookieDomain    string
	CookieSecure    string
	AcmeStaging     string
	AcmeEnabled     string
	AcmeEmail       string
	AppPort         string
	TelegramEnabled string
	BotToken        string
	AdminIDs        string
	AdminPass       string
	JWTSecret       string
	DBPassword      string
	AdminHash       string
}

// genRandomPath generates a random 6-char lowercase alphanumeric path prefix.
func genRandomPath() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, 6)
	for i := range result {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[idx.Int64()]
	}
	return "/" + string(result)
}

// WizardInstall runs the full interactive installation wizard.
func WizardInstall(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("nasnet-panel Installation Wizard")

	// ── Check existing .env ─────────────────────────────────────────────
	if _, err := os.Stat(cfg.EnvFile); err == nil {
		ui.StepWarn("An .env file already exists at " + cfg.EnvFile)
		overwrite, confirmErr := ui.Confirm("Overwrite it? (existing secrets will be lost)")
		if confirmErr != nil || !overwrite {
			ui.StepInfo("Cancelled — use Reconfigure to modify existing config")
			ui.PressAnyKey()
			return
		}
	}

	wv := &wizardVars{}

	// ── Step 1: Deployment Mode ─────────────────────────────────────────
	if cfg.OfflineMode {
		wv.DeployMode = "systemd"
		ui.StepInfo("Offline mode — using systemd deployment")
	} else {
		ui.DrawHeader("Step 1: Deployment Mode")
		fmt.Println("  How do you want to run nasnet-panel?")
		fmt.Println()

		choice, err := ui.Menu("Select deployment", []string{
			"Docker (recommended)",
			"Systemd (bare-metal)",
		})
		if err != nil || choice < 0 {
			return
		}
		if choice == 0 {
			wv.DeployMode = "docker"
		} else {
			wv.DeployMode = "systemd"
		}
	}

	// ── Step 2: Database Engine ─────────────────────────────────────────
	ui.DrawHeader("Step 2: Database Engine")
	fmt.Println("  Which database engine do you want to use?")
	fmt.Println()

	dbChoice, err := ui.Menu("Select database engine", []string{
		"PostgreSQL (recommended)",
		"SQLite (lightweight)",
	})
	if err != nil || dbChoice < 0 {
		return
	}
	if dbChoice == 0 {
		wv.DBDriver = "postgres"
	} else {
		wv.DBDriver = "sqlite"
	}

	// ── Step 3: Prerequisites ───────────────────────────────────────────
	ui.DrawHeader("Step 3: Prerequisites")

	if wv.DeployMode == "docker" {
		if !PrereqsDocker(cfg) {
			ui.PressAnyKey()
			return
		}
	} else {
		if !PrereqsSystemd(cfg, wv.DBDriver) {
			ui.PressAnyKey()
			return
		}
	}
	fmt.Println()

	// ── Step 4: Access Mode ─────────────────────────────────────────────
	ui.DrawHeader("Step 4: Access Mode")

	wv.AppPort = RandomPort()
	ui.StepInfo(fmt.Sprintf("Generated random port — APP_PORT: %s", ui.StyleTitle.Render(wv.AppPort)))

	if !wizardPromptAccessMode(cfg, wv) {
		ui.PressAnyKey()
		return
	}

	// ── Step 5: Telegram & Admin ────────────────────────────────────────
	ui.DrawHeader("Step 5: Telegram & Admin")

	wv.TelegramEnabled = "true"
	enableTelegram, err := ui.Confirm("Enable Telegram bot?")
	if err != nil {
		return
	}

	if enableTelegram {
		token, err := ui.InputString("Telegram bot token (from @BotFather)", "")
		if err != nil {
			return
		}
		if token == "" {
			ui.StepFail("Bot token is required when Telegram is enabled")
			ui.PressAnyKey()
			return
		}
		wv.BotToken = token

		adminIDs, err := ui.InputString("Admin Telegram ID (your numeric ID)", "")
		if err != nil {
			return
		}
		validIDs := regexp.MustCompile(`^[0-9, ]+$`)
		if adminIDs == "" || !validIDs.MatchString(adminIDs) {
			ui.StepFail("A valid numeric Telegram ID is required")
			ui.PressAnyKey()
			return
		}
		wv.AdminIDs = adminIDs
	} else {
		wv.TelegramEnabled = "false"
		ui.StepOk("Telegram bot disabled — running in web-panel-only mode")
	}

	fmt.Println()
	pass, err := ui.InputPassword("Admin panel password (for web dashboard login)")
	if err != nil {
		return
	}
	if len(pass) < 6 {
		ui.StepFail("Password must be at least 6 characters")
		ui.PressAnyKey()
		return
	}

	pass2, err := ui.InputPassword("Confirm password")
	if err != nil {
		return
	}
	if pass != pass2 {
		ui.StepFail("Passwords do not match")
		ui.PressAnyKey()
		return
	}
	wv.AdminPass = pass

	// ── Step 6: Generate Secrets ────────────────────────────────────────
	ui.DrawHeader("Step 6: Generating Secrets")

	wv.JWTSecret = GenSecret(32)
	ui.StepOk(fmt.Sprintf("JWT secret key generated (%d chars)", len(wv.JWTSecret)))

	if wv.DBDriver != "sqlite" {
		wv.DBPassword = GenPassword(24)
		ui.StepOk("Database password generated")
	} else {
		ui.StepOk("SQLite — no database password needed")
	}

	ui.StepInfo("Generating bcrypt hash for admin password...")
	hash, err := GenBcrypt(wv.AdminPass)
	if err != nil {
		ui.StepFail("Failed to generate bcrypt hash: " + err.Error())
		ui.PressAnyKey()
		return
	}
	wv.AdminHash = hash
	ui.StepOk("Admin password hash generated")

	// ── Systemd: set up PostgreSQL database ─────────────────────────────
	if wv.DeployMode == "systemd" && wv.DBDriver != "sqlite" {
		fmt.Println()
		if cfg.OfflineMode {
			if !wizardSetupPostgresOffline(cfg, "postgres", wv.DBPassword, "nasnet_panel") {
				ui.PressAnyKey()
				return
			}
		} else {
			if !wizardSetupPostgres("postgres", wv.DBPassword, "nasnet_panel") {
				ui.PressAnyKey()
				return
			}
		}
	}

	// ── Step 7: Review Configuration ────────────────────────────────────
	ui.DrawHeader("Step 7: Review Configuration")

	maskedToken := "(disabled)"
	if wv.BotToken != "" {
		maskedToken = MaskSecret(wv.BotToken, 8)
	}
	maskedJWT := MaskSecret(wv.JWTSecret, 8)

	basePath := wv.BasePath
	if basePath == "" {
		basePath = "(none)"
	}
	cookieDomain := wv.CookieDomain
	if cookieDomain == "" {
		cookieDomain = "(empty)"
	}
	acmeEmail := wv.AcmeEmail
	if acmeEmail == "" {
		acmeEmail = "(none)"
	}
	adminIDs := wv.AdminIDs
	if adminIDs == "" {
		adminIDs = "(none)"
	}

	rows := [][]string{
		{"Deploy", wv.DeployMode},
		{"DB Engine", wv.DBDriver},
		{"Mode", wv.Mode},
		{"APP_BASE_URL", wv.AppBaseURL},
		{"SUB_PANEL_URL", wv.SubPanelURL},
		{"Panel Path", basePath},
		{"JWT_COOKIE_DOMAIN", cookieDomain},
		{"JWT_COOKIE_SECURE", wv.CookieSecure},
		{"ACME_ENABLED", wv.AcmeEnabled},
		{"ACME_STAGING", wv.AcmeStaging},
		{"ACME_EMAIL", acmeEmail},
		{"TELEGRAM_ENABLED", wv.TelegramEnabled},
		{"BOT_TOKEN", maskedToken},
		{"ADMIN_IDS", adminIDs},
		{"ADMIN_USERNAME", "admin"},
		{"JWT_SECRET", maskedJWT},
	}
	if wv.DBDriver != "sqlite" {
		maskedDBPass := MaskSecret(wv.DBPassword, 4)
		rows = append(rows, []string{"DB_PASSWORD", maskedDBPass})
	}

	ui.Table([]string{"Setting", "Value"}, rows)
	fmt.Println()

	confirmed, err := ui.Confirm("Write this configuration to .env and start services?")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// ── Step 8: Write .env ──────────────────────────────────────────────
	ui.DrawHeader("Step 8: Writing Configuration")

	comment := fmt.Sprintf("Generated by nasnet-tool wizard on %s", time.Now().Format("2006-01-02 15:04:05"))
	wizardWriteEnv(cfg, wv, wv.DeployMode, comment)

	// ── Step 9: Build & Start ───────────────────────────────────────────
	if wv.DeployMode == "docker" {
		if !wizardBuildStartDocker(cfg, wv) {
			ui.PressAnyKey()
			return
		}
	} else {
		if !wizardBuildStartSystemd(cfg, wv) {
			ui.PressAnyKey()
			return
		}
	}

	// ── Post-install Summary ────────────────────────────────────────────
	fmt.Println()
	ui.DrawHeader("Installation Complete!")

	fmt.Printf("  %s\n", ui.StyleSuccess.Bold(true).Render("nasnet-panel is now running!"))
	fmt.Println()
	fmt.Printf("  %s %s\n", ui.StyleTitle.Render("Deploy Mode:"), ui.StyleCyan.Render(wv.DeployMode))
	fmt.Printf("  %s %s\n", ui.StyleTitle.Render("Web Panel:"), ui.StyleCyan.Render(wv.SubPanelURL))
	fmt.Printf("  %s %s\n", ui.StyleTitle.Render("Backend API:"), ui.StyleCyan.Render(wv.AppBaseURL))
	fmt.Printf("  %s admin / (your password)\n", ui.StyleTitle.Render("Login:"))
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    1. Open the web panel URL above in your browser")
	fmt.Println("    2. Log in with admin and the password you set")
	if wv.TelegramEnabled == "true" {
		fmt.Println("    3. Send /start to your Telegram bot")
	}
	fmt.Println()

	if wv.Mode == "domain" {
		fmt.Printf("  %s\n", ui.StyleDim.Render("TLS certificates will be auto-provisioned on first request."))
	}
	if wv.DeployMode == "systemd" {
		fmt.Printf("  %s\n", ui.StyleDim.Render(fmt.Sprintf("Installed to:  %s", cfg.InstallDir)))
		fmt.Printf("  %s\n", ui.StyleDim.Render(fmt.Sprintf("Service logs:  journalctl -u %s -f", DefaultBackendService)))
		fmt.Printf("  %s\n", ui.StyleDim.Render(fmt.Sprintf("Config: %s/.env", cfg.InstallDir)))
	} else {
		fmt.Printf("  %s\n", ui.StyleDim.Render(fmt.Sprintf("Config: %s", cfg.EnvFile)))
	}
	fmt.Printf("  %s\n", ui.StyleDim.Render("Manage: ./nasnet-tool"))

	ui.PressAnyKey()
}

// wizardPromptAccessMode prompts the user for domain/IP access configuration.
// It fills in wv.Mode, wv.Domain, wv.BasePath, wv.AppBaseURL, wv.SubPanelURL,
// wv.CookieDomain, wv.CookieSecure, wv.AcmeEnabled, wv.AcmeEmail, wv.AcmeStaging.
// Returns true on success, false on cancel.
func wizardPromptAccessMode(cfg *Config, wv *wizardVars) bool {
	fmt.Println("  How will users access this server?")
	fmt.Println()

	modeChoice, err := ui.Menu("Select mode", []string{
		"Domain mode (recommended)",
		"IP-only mode",
	})
	if err != nil || modeChoice < 0 {
		return false
	}

	if modeChoice == 0 {
		// ── Domain Mode ──
		wv.Mode = "domain"

		// ── Protocol ──
		fmt.Println()
		fmt.Println("  Protocol:")
		fmt.Printf("  %s — Secure, requires TLS certificate (auto or manual)\n", ui.StyleCyan.Render("HTTPS"))
		fmt.Printf("  %s  — No encryption (use if TLS is handled by a reverse proxy)\n", ui.StyleCyan.Render("HTTP"))
		fmt.Println()

		protoChoice, err := ui.Menu("Select protocol", []string{
			"HTTPS (recommended)",
			"HTTP",
		})
		if err != nil || protoChoice < 0 {
			return false
		}

		proto := "https"
		wv.CookieSecure = "true"
		if protoChoice == 1 {
			proto = "http"
			wv.CookieSecure = "false"
		}

		// ── API domain ──
		fmt.Println()
		apiDomain, err := ui.InputString("API domain (e.g. api.example.com)", "")
		if err != nil {
			return false
		}
		if apiDomain == "" {
			ui.StepFail("API domain is required")
			return false
		}

		// ── API port ──
		apiPort, err := ui.InputStringDefault("API port", wv.AppPort)
		if err != nil {
			return false
		}
		if apiPort != "" {
			wv.AppPort = apiPort
		}

		// ── Panel domain ──
		fmt.Println()
		panelDomain, err := ui.InputStringDefault("Panel domain", apiDomain)
		if err != nil {
			return false
		}
		if panelDomain == "" {
			panelDomain = apiDomain
		}

		// ── Panel base path (auto-generated for security) ──
		wv.BasePath = genRandomPath()
		fmt.Println()
		ui.StepInfo("A random panel path has been generated for security.")
		ui.StepInfo(fmt.Sprintf("Your admin panel will be at: %s",
			ui.StyleSuccess.Render(fmt.Sprintf("%s://%s:%s%s", proto, panelDomain, wv.AppPort, wv.BasePath))))

		bpInput, err := ui.InputStringDefault("Panel base path", wv.BasePath)
		if err != nil {
			return false
		}
		if bpInput == "none" {
			wv.BasePath = ""
		} else if bpInput != "" {
			wv.BasePath = strings.TrimRight(bpInput, "/")
			if !strings.HasPrefix(wv.BasePath, "/") {
				wv.BasePath = "/" + wv.BasePath
			}
		}

		wv.Domain = apiDomain
		wv.AppBaseURL = fmt.Sprintf("%s://%s:%s", proto, apiDomain, wv.AppPort)
		wv.SubPanelURL = fmt.Sprintf("%s://%s:%s%s", proto, panelDomain, wv.AppPort, wv.BasePath)
		wv.CookieDomain = ""
		wv.AcmeStaging = "false"

		fmt.Println()
		ui.StepInfo("Derived URLs:")
		fmt.Printf("    APP_BASE_URL  = %s\n", ui.StyleSuccess.Render(wv.AppBaseURL))
		fmt.Printf("    SUB_PANEL_URL = %s\n", ui.StyleSuccess.Render(wv.SubPanelURL))
		fmt.Println()

		overrideURLs, err := ui.Confirm("Override any derived URLs?")
		if err != nil {
			return false
		}
		if overrideURLs {
			fmt.Println()
			override, err := ui.InputStringDefault("APP_BASE_URL", wv.AppBaseURL)
			if err != nil {
				return false
			}
			if override != "" {
				wv.AppBaseURL = override
			}

			override, err = ui.InputStringDefault("SUB_PANEL_URL", wv.SubPanelURL)
			if err != nil {
				return false
			}
			if override != "" {
				wv.SubPanelURL = override
			}
		}

		// ── ACME / Let's Encrypt ──
		wv.AcmeEmail = ""
		wv.AcmeEnabled = "false"
		if proto == "https" {
			fmt.Println()
			fmt.Printf("  %s\n", ui.StyleDim.Render("If you use a reverse proxy (nginx/Caddy) for TLS, choose No."))
			issueACME, err := ui.Confirm("Issue TLS certificate via Let's Encrypt (ACME)?")
			if err != nil {
				return false
			}
			if issueACME {
				email, err := ui.InputString("Email for Let's Encrypt", "")
				if err != nil {
					return false
				}
				if email == "" || !strings.Contains(email, "@") {
					ui.StepFail("A valid email is required for Let's Encrypt")
					return false
				}
				wv.AcmeEmail = email
				wv.AcmeEnabled = "true"
			}
		}

	} else {
		// ── IP Mode ──
		wv.Mode = "ip"

		ui.StepInfo("Detecting server IP...")
		detectedIP := DetectIP(cfg.OfflineMode)

		if detectedIP != "" {
			ui.StepOk("Detected: " + detectedIP)
		} else {
			ui.StepWarn("Could not auto-detect IP")
		}

		ipOverride, err := ui.InputStringDefault("Server IP", detectedIP)
		if err != nil {
			return false
		}
		if ipOverride != "" {
			detectedIP = ipOverride
		}
		if detectedIP == "" {
			ui.StepFail("Server IP is required")
			return false
		}

		// ── Panel base path (auto-generated for security) ──
		wv.BasePath = genRandomPath()
		fmt.Println()
		ui.StepInfo("A random panel path has been generated for security.")
		ui.StepInfo(fmt.Sprintf("Your admin panel will be at: %s",
			ui.StyleSuccess.Render(fmt.Sprintf("http://%s:%s%s", detectedIP, wv.AppPort, wv.BasePath))))

		bpInput, err := ui.InputStringDefault("Panel base path", wv.BasePath)
		if err != nil {
			return false
		}
		if bpInput == "none" {
			wv.BasePath = ""
		} else if bpInput != "" {
			wv.BasePath = strings.TrimRight(bpInput, "/")
			if !strings.HasPrefix(wv.BasePath, "/") {
				wv.BasePath = "/" + wv.BasePath
			}
		}

		wv.AppBaseURL = fmt.Sprintf("http://%s:%s", detectedIP, wv.AppPort)
		wv.SubPanelURL = fmt.Sprintf("http://%s:%s%s", detectedIP, wv.AppPort, wv.BasePath)
		wv.CookieDomain = ""
		wv.CookieSecure = "false"
		wv.AcmeStaging = "true"
		wv.AcmeEnabled = "false"
		wv.AcmeEmail = ""

		fmt.Println()
		ui.StepInfo("Derived URLs:")
		fmt.Printf("    APP_BASE_URL  = %s\n", ui.StyleSuccess.Render(wv.AppBaseURL))
		fmt.Printf("    SUB_PANEL_URL = %s\n", ui.StyleSuccess.Render(wv.SubPanelURL))
		fmt.Println()
	}

	return true
}

// wizardWriteEnv writes the .env file with all configuration variables.
func wizardWriteEnv(cfg *Config, wv *wizardVars, deployMode, comment string) {
	dbDriver := wv.DBDriver
	if dbDriver == "" {
		dbDriver = "postgres"
	}

	// Determine deployment-specific defaults
	acmeCacheDir := "/app/data/acme"
	promTarget := fmt.Sprintf("app:%s", wv.AppPort)
	if deployMode == "systemd" {
		acmeCacheDir = filepath.Join(cfg.InstallDir, "data", "acme")
		promTarget = fmt.Sprintf("localhost:%s", wv.AppPort)
	}

	var sb strings.Builder

	sb.WriteString("# ── nasnet-panel Configuration ──────────────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("# %s\n", comment))
	sb.WriteString(fmt.Sprintf("# Mode: %s  |  Deploy: %s  |  DB: %s\n", wv.Mode, deployMode, dbDriver))
	sb.WriteString("\n")
	sb.WriteString("# Deployment\n")
	sb.WriteString(fmt.Sprintf("DEPLOY_MODE=%s\n", deployMode))
	sb.WriteString("\n")
	sb.WriteString("# Application\n")
	sb.WriteString("APP_ENV=production\n")
	sb.WriteString(fmt.Sprintf("APP_PORT=%s\n", wv.AppPort))
	sb.WriteString(fmt.Sprintf("APP_BASE_URL=%s\n", wv.AppBaseURL))
	sb.WriteString(fmt.Sprintf("SUB_PANEL_URL=%s\n", wv.SubPanelURL))
	sb.WriteString(fmt.Sprintf("APP_PANEL_BASE_PATH=%s\n", wv.BasePath))
	sb.WriteString("\n")
	sb.WriteString("# Database\n")
	sb.WriteString(fmt.Sprintf("DB_DRIVER=%s\n", dbDriver))

	if dbDriver == "sqlite" {
		dbPath := "/app/data/nasnet_panel.db"
		if deployMode == "systemd" {
			dbPath = filepath.Join(cfg.InstallDir, "data", "nasnet_panel.db")
		}
		sb.WriteString(fmt.Sprintf("DB_PATH=%s\n", dbPath))
	} else {
		sb.WriteString("DB_HOST=localhost\n")
		sb.WriteString("DB_PORT=5432\n")
		sb.WriteString("DB_USER=postgres\n")
		sb.WriteString(fmt.Sprintf("DB_PASSWORD=%s\n", wv.DBPassword))
		sb.WriteString("DB_NAME=nasnet_panel\n")
		sb.WriteString("DB_SSL_MODE=disable\n")
	}

	sb.WriteString("\n")
	sb.WriteString("# Telegram Bot\n")
	sb.WriteString(fmt.Sprintf("TELEGRAM_ENABLED=%s\n", wv.TelegramEnabled))
	sb.WriteString(fmt.Sprintf("TELEGRAM_BOT_TOKEN=%s\n", wv.BotToken))
	sb.WriteString("BOT_MODE=polling\n")
	sb.WriteString("WEBHOOK_URL=\n")
	sb.WriteString("\n")
	sb.WriteString("# Telegram Proxy (optional)\n")
	sb.WriteString("TELEGRAM_PROXY_ENABLED=false\n")
	sb.WriteString("TELEGRAM_PROXY_TYPE=socks5\n")
	sb.WriteString("TELEGRAM_PROXY_HOST=\n")
	sb.WriteString("TELEGRAM_PROXY_PORT=1080\n")
	sb.WriteString("TELEGRAM_PROXY_USERNAME=\n")
	sb.WriteString("TELEGRAM_PROXY_PASSWORD=\n")
	sb.WriteString("\n")
	sb.WriteString("# Logging\n")
	sb.WriteString("LOG_LEVEL=info\n")
	sb.WriteString("LOG_FORMAT=text\n")
	sb.WriteString("\n")
	sb.WriteString("# Admin\n")
	sb.WriteString(fmt.Sprintf("ADMIN_IDS=%s\n", wv.AdminIDs))
	sb.WriteString("ADMIN_USERNAME=admin\n")
	sb.WriteString(fmt.Sprintf("ADMIN_PASSWORD_HASH='%s'\n", wv.AdminHash))
	sb.WriteString("\n")
	sb.WriteString("# TLS (optional — leave empty for auto ACME, or set paths for custom certs)\n")
	sb.WriteString("TLS_CERT_FILE=\n")
	sb.WriteString("TLS_KEY_FILE=\n")
	sb.WriteString("\n")
	sb.WriteString("# ACME / Let's Encrypt\n")
	acmeEnabled := wv.AcmeEnabled
	if acmeEnabled == "" {
		acmeEnabled = "false"
	}
	sb.WriteString(fmt.Sprintf("ACME_ENABLED=%s\n", acmeEnabled))
	sb.WriteString(fmt.Sprintf("ACME_EMAIL=%s\n", wv.AcmeEmail))
	sb.WriteString(fmt.Sprintf("ACME_CACHE_DIR=%s\n", acmeCacheDir))
	sb.WriteString(fmt.Sprintf("ACME_STAGING=%s\n", wv.AcmeStaging))
	sb.WriteString("ACME_AUTO_RENEW=true\n")
	sb.WriteString("\n")
	sb.WriteString("# JWT Authentication\n")
	sb.WriteString(fmt.Sprintf("JWT_SECRET_KEY=%s\n", wv.JWTSecret))
	sb.WriteString("JWT_ACCESS_EXPIRY=60\n")
	sb.WriteString("JWT_REFRESH_EXPIRY=168\n")
	sb.WriteString(fmt.Sprintf("JWT_COOKIE_DOMAIN=%s\n", wv.CookieDomain))
	sb.WriteString(fmt.Sprintf("JWT_COOKIE_SECURE=%s\n", wv.CookieSecure))
	sb.WriteString("\n")
	sb.WriteString("# Metrics\n")
	sb.WriteString("METRICS_ENABLED=true\n")
	sb.WriteString("METRICS_PATH=/metrics\n")
	sb.WriteString("METRICS_USERNAME=\n")
	sb.WriteString("METRICS_PASSWORD=\n")
	sb.WriteString("\n")
	sb.WriteString("# Prometheus\n")
	sb.WriteString("PROMETHEUS_PORT=9090\n")
	sb.WriteString(fmt.Sprintf("PROMETHEUS_TARGET=%s\n", promTarget))
	sb.WriteString("PROMETHEUS_SCRAPE_INTERVAL=5s\n")
	sb.WriteString("PROMETHEUS_RETENTION=15d\n")

	if err := os.WriteFile(cfg.EnvFile, []byte(sb.String()), 0600); err != nil {
		ui.StepFail("Failed to write .env: " + err.Error())
		return
	}
	ui.StepOk(".env written (permissions: 600)")
}

// wizardBuildStartDocker builds and starts services using Docker Compose.
func wizardBuildStartDocker(cfg *Config, wv *wizardVars) bool {
	ui.DrawHeader("Building & Starting Services (Docker)")

	// Ensure host bind-mount dirs exist and are writable by container UID 1000.
	if err := cfg.PrepareDockerBindMounts(); err != nil {
		ui.StepFail("Failed to prepare data directories: " + err.Error())
		return false
	}

	// Build backend image
	if err := ui.RunLogged("Building backend image", WithBuildEnv(cfg.DockerCompose("build", "app"))); err != nil {
		ui.StepFail("Backend build failed")
		ui.StepInfo("Run manually: docker compose build app")
		return false
	}

	// Start containers
	if err := ui.RunLogged("Starting containers", cfg.DockerCompose("up", "-d")); err != nil {
		ui.StepFail("Failed to start services")
		ui.StepInfo("Check with: docker compose ps")
		return false
	}

	// Wait for health checks
	fmt.Println()
	ui.StepInfo("Waiting for services to become healthy...")
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		out, err := exec.Command("docker", "ps",
			"--filter", "name=nasnet_panel",
			"--filter", "health=healthy",
			"--format", "{{.Names}}").Output()
		if err == nil {
			names := strings.Fields(strings.TrimSpace(string(out)))
			if len(names) >= 2 {
				break
			}
		}
		time.Sleep(2 * time.Second)
		fmt.Printf("\r  %s Waiting for health checks... (%d/%ds)",
			ui.StyleCyan.Render("⠋"), (i+1)*2, maxRetries*2)
	}
	fmt.Printf("\r%60s\r", "")

	fmt.Println()
	viewStatusInline(cfg)
	return true
}

// wizardBuildStartSystemd builds and deploys nasnet-panel as a systemd service.
func wizardBuildStartSystemd(cfg *Config, wv *wizardVars) bool {
	ui.DrawHeader("Building & Starting Services (Systemd)")

	if cfg.OfflineMode {
		// Offline: skip all builds, artifacts already deployed by install.sh
		ui.StepOk("Offline mode — using pre-built artifacts from bundle")
		fmt.Println()
	} else {
		// ── Build frontend ──
		frontendCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s/web-panel' && pnpm install && pnpm build", cfg.ProjectDir))
		if err := ui.RunLogged("Building frontend", frontendCmd); err != nil {
			ui.StepFail("Frontend build failed")
			return false
		}
		ui.StepOk("Frontend built")

		// ── Download Go modules ──
		modCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && go mod download", cfg.ProjectDir))
		if err := ui.RunLogged("Downloading Go modules", modCmd); err != nil {
			ui.StepFail("Failed to download Go modules")
			return false
		}

		// ── Download Iran geofiles ──
		geoCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make geofiles", cfg.ProjectDir))
		if err := ui.RunLogged("Downloading Iran geofiles", geoCmd); err != nil {
			ui.StepFail("Failed to download Iran geofiles")
			return false
		}
		ui.StepOk("Iran geofiles ready")

		// ── Build Go backend ──
		cgoEnabled := "0"
		if wv.DBDriver == "sqlite" {
			cgoEnabled = "1"
		}
		buildCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && CGO_ENABLED=%s go build -ldflags='-w -s' -o nasnet-panel .",
				cfg.ProjectDir, cgoEnabled))
		if err := ui.RunLogged("Building nasnet-panel binary", buildCmd); err != nil {
			ui.StepFail("Backend build failed")
			return false
		}
		ui.StepOk("Binary built: " + cfg.ProjectDir + "/nasnet-panel")

		// ── Build agent binaries ──
		agentCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make build-agent", cfg.ProjectDir))
		if err := ui.RunLogged("Building agent binaries", agentCmd); err != nil {
			ui.StepFail("Agent binary build failed")
			return false
		}
		ui.StepOk("Agent binaries built")

		// ── Deploy artifacts ──
		fmt.Println()
		ui.DrawHeader("Deploying to " + cfg.InstallDir)
		wizardDeployArtifacts(cfg)
	}

	fmt.Println()

	// ── Create systemd service ──
	ui.DrawHeader("Installing Systemd Services")

	// Determine the user to run services as
	runUser := os.Getenv("SUDO_USER")
	if runUser == "" {
		runUser = os.Getenv("USER")
		if runUser == "" {
			runUser = "root"
		}
	}

	runGroup := runUser
	if out, err := exec.Command("id", "-gn", runUser).Output(); err == nil {
		runGroup = strings.TrimSpace(string(out))
	}

	ui.StepInfo(fmt.Sprintf("Creating %s.service...", DefaultBackendService))

	svcAfter := "network-online.target"
	svcRequires := ""
	svcEnvDB := ""
	if wv.DBDriver != "sqlite" {
		pgSvcName := "postgresql.service"
		if cfg.OfflineMode {
			pgSvcName = "nasnet-postgresql.service"
		}
		svcAfter = "network-online.target " + pgSvcName
		svcRequires = "Requires=" + pgSvcName
		svcEnvDB = "Environment=DB_HOST=localhost"
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=nasnet-panel Backend API
Documentation=https://github.com/nasnet-community/nasnet-panel-linux
After=%s
%s
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
EnvironmentFile=%s/.env
%s
ExecStart=%s/bin/nasnet-panel serve
Restart=always
RestartSec=5
LimitNOFILE=65536

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`, svcAfter, svcRequires, runUser, runGroup, cfg.InstallDir,
		cfg.InstallDir, svcEnvDB, cfg.InstallDir, DefaultBackendService)

	unitFile := fmt.Sprintf("/etc/systemd/system/%s.service", DefaultBackendService)
	teeCmd := exec.Command("sudo", "tee", unitFile)
	teeCmd.Stdin = strings.NewReader(unitContent)
	if err := teeCmd.Run(); err != nil {
		ui.StepFail("Failed to write unit file (need sudo?)")
		return false
	}
	ui.StepOk(fmt.Sprintf("%s.service created", DefaultBackendService))

	// Reload systemd
	if err := exec.Command("sudo", "systemctl", "daemon-reload").Run(); err != nil {
		ui.StepFail("daemon-reload failed")
		return false
	}
	ui.StepOk("Systemd daemon reloaded")

	// ── Enable and start ──
	fmt.Println()
	ui.StepInfo("Starting services...")

	exec.Command("sudo", "systemctl", "enable", DefaultBackendService, "--now").Run() //nolint:errcheck
	time.Sleep(2 * time.Second)

	activeOut, _ := exec.Command("systemctl", "is-active", DefaultBackendService).Output()
	if strings.TrimSpace(string(activeOut)) == "active" {
		ui.StepOk(DefaultBackendService + " is running")
	} else {
		ui.StepFail(DefaultBackendService + " failed to start")
		ui.StepInfo(fmt.Sprintf("Check logs: journalctl -u %s -n 20", DefaultBackendService))
	}

	// ── Health check ──
	fmt.Println()
	ui.StepInfo("Waiting for services to become healthy...")
	maxRetries := 15
	healthy := false
	for i := 0; i < maxRetries; i++ {
		checkCmd := exec.Command("curl", "-sf", "--max-time", "3",
			fmt.Sprintf("http://localhost:%s/health/ready", wv.AppPort))
		if checkCmd.Run() == nil {
			healthy = true
			break
		}
		time.Sleep(2 * time.Second)
		fmt.Printf("\r  %s Waiting for backend... (%d/%ds)",
			ui.StyleCyan.Render("⠋"), (i+1)*2, maxRetries*2)
	}
	fmt.Printf("\r%60s\r", "")

	// Show status
	fmt.Println()
	var statusRows [][]string

	if healthy {
		statusRows = append(statusRows, []string{
			"nasnet-panel",
			fmt.Sprintf("http://localhost:%s", wv.AppPort),
			ui.StyleSuccess.Render("● Healthy"),
		})
	} else {
		statusRows = append(statusRows, []string{
			"nasnet-panel",
			fmt.Sprintf("http://localhost:%s", wv.AppPort),
			ui.StyleError.Render("● Unreachable"),
		})
	}

	if wv.DBDriver == "sqlite" {
		statusRows = append(statusRows, []string{
			"SQLite",
			"embedded",
			ui.StyleSuccess.Render("● Ready"),
		})
	} else {
		pgActive, _ := exec.Command("systemctl", "is-active", "postgresql").Output()
		pgState := strings.TrimSpace(string(pgActive))
		pgStatusStr := ui.StyleError.Render("● Stopped")
		if pgState == "active" {
			pgStatusStr = ui.StyleSuccess.Render("● Running")
		}
		statusRows = append(statusRows, []string{
			"PostgreSQL",
			"systemd",
			pgStatusStr,
		})
	}

	ui.Table([]string{"Service", "Endpoint", "Status"}, statusRows)

	return true
}

// wizardDeployArtifacts copies built binaries and config to the install directory.
func wizardDeployArtifacts(cfg *Config) {
	runUser := os.Getenv("SUDO_USER")
	if runUser == "" {
		runUser = os.Getenv("USER")
		if runUser == "" {
			runUser = "root"
		}
	}

	runGroup := runUser
	if out, err := exec.Command("id", "-gn", runUser).Output(); err == nil {
		runGroup = strings.TrimSpace(string(out))
	}

	if cfg.OfflineMode {
		// Offline mode: just sync .env
		ui.StepInfo("Offline mode — syncing configuration...")
		if _, err := os.Stat(cfg.EnvFile); err == nil {
			exec.Command("sudo", "cp", cfg.EnvFile, filepath.Join(cfg.InstallDir, ".env")).Run() //nolint:errcheck
			exec.Command("sudo", "chmod", "600", filepath.Join(cfg.InstallDir, ".env")).Run()    //nolint:errcheck
		}
		exec.Command("sudo", "chown", "-R", runUser+":"+runGroup, cfg.InstallDir).Run() //nolint:errcheck
		ui.StepOk("Configuration synced to " + cfg.InstallDir)
		return
	}

	ui.StepInfo("Deploying to " + cfg.InstallDir + "...")

	// Create directory structure
	for _, dir := range []string{
		filepath.Join(cfg.InstallDir, "bin"),
		filepath.Join(cfg.InstallDir, "bin", "agent"),
		filepath.Join(cfg.InstallDir, "bin", "xray"),
		filepath.Join(cfg.InstallDir, "data", "backups"),
		filepath.Join(cfg.InstallDir, "data", "acme"),
	} {
		exec.Command("sudo", "mkdir", "-p", dir).Run() //nolint:errcheck
	}

	// Copy backend binary
	srcBin := filepath.Join(cfg.ProjectDir, "nasnet-panel")
	dstBin := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
	exec.Command("sudo", "cp", srcBin, dstBin).Run()  //nolint:errcheck
	exec.Command("sudo", "chmod", "+x", dstBin).Run() //nolint:errcheck

	// Copy agent binaries
	agentSrcDir := filepath.Join(cfg.ProjectDir, "bin", "agent")
	if entries, err := os.ReadDir(agentSrcDir); err == nil {
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

	// Copy .env
	exec.Command("sudo", "cp", cfg.EnvFile, filepath.Join(cfg.InstallDir, ".env")).Run() //nolint:errcheck
	exec.Command("sudo", "chmod", "600", filepath.Join(cfg.InstallDir, ".env")).Run()    //nolint:errcheck

	// Write version marker
	versionOut, err := exec.Command("bash", "-c",
		fmt.Sprintf("cd '%s' && git describe --tags --always 2>/dev/null || echo unknown", cfg.ProjectDir)).Output()
	version := strings.TrimSpace(string(versionOut))
	if err != nil || version == "" {
		version = "unknown"
	}
	versionCmd := exec.Command("sudo", "tee", filepath.Join(cfg.InstallDir, ".version"))
	versionCmd.Stdin = strings.NewReader(version)
	versionCmd.Run() //nolint:errcheck

	// Set ownership
	exec.Command("sudo", "chown", "-R", runUser+":"+runGroup, cfg.InstallDir).Run() //nolint:errcheck

	ui.StepOk("Deployed to " + cfg.InstallDir)
}

// wizardSetupPostgres configures PostgreSQL for systemd deployments (online).
func wizardSetupPostgres(dbUser, dbPass, dbName string) bool {
	ui.StepInfo("Configuring PostgreSQL database...")

	// Check if user exists
	out, _ := exec.Command("sudo", "-u", "postgres", "psql", "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname='%s'", dbUser)).Output()
	userExists := strings.TrimSpace(string(out)) == "1"

	if userExists {
		ui.StepOk(fmt.Sprintf("Database user '%s' exists", dbUser))
		exec.Command("sudo", "-u", "postgres", "psql", "-c",
			fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s';", dbUser, dbPass)).Run() //nolint:errcheck
		ui.StepOk(fmt.Sprintf("Password updated for '%s'", dbUser))
	} else {
		if err := exec.Command("sudo", "-u", "postgres", "psql", "-c",
			fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", dbUser, dbPass)).Run(); err != nil {
			ui.StepFail("Failed to create database user")
			return false
		}
		ui.StepOk(fmt.Sprintf("Database user '%s' created", dbUser))
	}

	// Check if database exists
	out, _ = exec.Command("sudo", "-u", "postgres", "psql", "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s'", dbName)).Output()
	dbExists := strings.TrimSpace(string(out)) == "1"

	if dbExists {
		ui.StepOk(fmt.Sprintf("Database '%s' exists", dbName))
	} else {
		if err := exec.Command("sudo", "-u", "postgres", "psql", "-c",
			fmt.Sprintf("CREATE DATABASE %s OWNER %s;", dbName, dbUser)).Run(); err != nil {
			ui.StepFail("Failed to create database")
			return false
		}
		ui.StepOk(fmt.Sprintf("Database '%s' created", dbName))
	}

	// Grant privileges
	exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s;", dbName, dbUser)).Run() //nolint:errcheck
	ui.StepOk("Privileges granted")

	return true
}

// wizardSetupPostgresOffline sets up bundled PostgreSQL for offline mode.
func wizardSetupPostgresOffline(cfg *Config, dbUser, dbPass, dbName string) bool {
	ui.StepInfo("Setting up bundled PostgreSQL...")

	pgsqlInstallDir := "/usr/local/nasnet-postgresql"
	pgBin := filepath.Join(pgsqlInstallDir, "bin")
	pgData := filepath.Join(pgsqlInstallDir, "data")
	pgsqlService := "nasnet-postgresql"

	// Check bundled PostgreSQL exists
	if _, err := os.Stat(filepath.Join(pgBin, "pg_ctl")); err != nil {
		ui.StepFail("Bundled PostgreSQL not found at " + pgsqlInstallDir)
		ui.StepInfo("Run install.sh first to extract PostgreSQL")
		return false
	}

	// Check if port 5432 is already in use
	portInUse := false
	if out, err := exec.Command("ss", "-tlnp").Output(); err == nil {
		if strings.Contains(string(out), ":5432 ") {
			portInUse = true
		}
	}

	if portInUse {
		ui.StepWarn("Port 5432 is already in use")
		fmt.Println()
		fmt.Println("  An existing PostgreSQL may be running.")
		fmt.Println("  You can use the existing instance instead of the bundled one.")
		fmt.Println()

		pgChoice, err := ui.Menu("PostgreSQL setup", []string{
			"Use existing PostgreSQL on port 5432",
			"Stop existing and use bundled PostgreSQL",
		})
		if err != nil || pgChoice < 0 {
			return false
		}

		if pgChoice == 0 {
			ui.StepInfo("Using existing PostgreSQL...")
			return wizardSetupPostgres(dbUser, dbPass, dbName)
		}
		// pgChoice == 1: stop existing and continue
		ui.StepInfo("Stopping existing PostgreSQL...")
		exec.Command("systemctl", "stop", "postgresql").Run() //nolint:errcheck
	}

	// Initialize data directory if not already done
	if _, err := os.Stat(filepath.Join(pgData, "PG_VERSION")); err != nil {
		ui.StepInfo("Initializing PostgreSQL data directory...")

		// Write password to temp file
		pwFile, err := os.CreateTemp("", "pgpass")
		if err != nil {
			ui.StepFail("Failed to create temp file for password")
			return false
		}
		pwFile.WriteString(dbPass)
		pwFile.Close()
		os.Chmod(pwFile.Name(), 0644)

		initErr := exec.Command("sudo", "-u", "postgres",
			filepath.Join(pgBin, "initdb"),
			"-D", pgData, "--auth=md5",
			"--pwfile="+pwFile.Name(),
			"--username=postgres").Run()
		os.Remove(pwFile.Name())

		if initErr != nil {
			// Retry without --pwfile
			if err := exec.Command("sudo", "-u", "postgres",
				filepath.Join(pgBin, "initdb"),
				"-D", pgData).Run(); err != nil {
				ui.StepFail("Failed to initialize PostgreSQL data directory")
				return false
			}
		}
		ui.StepOk("Data directory initialized")

		// Configure pg_hba.conf
		hbaContent := `# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             postgres                                peer
local   all             all                                     md5
host    all             all             127.0.0.1/32            md5
host    all             all             ::1/128                 md5
`
		hbaCmd := exec.Command("sudo", "tee", filepath.Join(pgData, "pg_hba.conf"))
		hbaCmd.Stdin = strings.NewReader(hbaContent)
		hbaCmd.Run()                                                                                   //nolint:errcheck
		exec.Command("sudo", "chown", "postgres:postgres", filepath.Join(pgData, "pg_hba.conf")).Run() //nolint:errcheck
		ui.StepOk("pg_hba.conf configured")

		// Configure postgresql.conf
		confAppend := `
# nasnet-panel offline bundle settings
listen_addresses = 'localhost'
port = 5432
max_connections = 100
shared_buffers = 128MB
log_destination = 'stderr'
logging_collector = off
`
		appendCmd := exec.Command("sudo", "tee", "-a", filepath.Join(pgData, "postgresql.conf"))
		appendCmd.Stdin = strings.NewReader(confAppend)
		appendCmd.Run()                                                                                    //nolint:errcheck
		exec.Command("sudo", "chown", "postgres:postgres", filepath.Join(pgData, "postgresql.conf")).Run() //nolint:errcheck
		ui.StepOk("postgresql.conf configured")
	} else {
		ui.StepOk("Data directory already initialized")
	}

	// Create systemd service
	ui.StepInfo(fmt.Sprintf("Creating %s.service...", pgsqlService))
	svcContent := fmt.Sprintf(`[Unit]
Description=nasnet-panel PostgreSQL
After=network.target

[Service]
Type=forking
User=postgres
Group=postgres
Environment=LD_LIBRARY_PATH=%s/lib
ExecStart=%s/pg_ctl start -D %s -l /var/log/nasnet-postgresql.log
ExecStop=%s/pg_ctl stop -D %s -m fast
ExecReload=%s/pg_ctl reload -D %s
TimeoutSec=60

[Install]
WantedBy=multi-user.target
`, pgsqlInstallDir, pgBin, pgData, pgBin, pgData, pgBin, pgData)

	svcCmd := exec.Command("sudo", "tee", fmt.Sprintf("/etc/systemd/system/%s.service", pgsqlService))
	svcCmd.Stdin = strings.NewReader(svcContent)
	svcCmd.Run()                                             //nolint:errcheck
	exec.Command("sudo", "systemctl", "daemon-reload").Run() //nolint:errcheck
	ui.StepOk(pgsqlService + ".service created")

	// Start PostgreSQL
	if err := exec.Command("sudo", "systemctl", "enable", pgsqlService, "--now").Run(); err != nil {
		ui.StepFail("Failed to start PostgreSQL")
		ui.StepInfo(fmt.Sprintf("Check logs: journalctl -u %s -n 20", pgsqlService))
		ui.StepInfo("Also check: cat /var/log/nasnet-postgresql.log")
		return false
	}
	ui.StepOk("PostgreSQL started")

	// Wait for PostgreSQL to be ready
	ready := false
	for i := 0; i < 10; i++ {
		if exec.Command("sudo", "-u", "postgres",
			filepath.Join(pgBin, "pg_isready"), "-q").Run() == nil {
			ready = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !ready {
		ui.StepFail("PostgreSQL did not become ready within 10 seconds")
		return false
	}

	// Create user and database using bundled psql
	psqlBin := filepath.Join(pgBin, "psql")

	out, _ := exec.Command("sudo", "-u", "postgres", psqlBin, "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname='%s'", dbUser)).Output()
	userExists := strings.TrimSpace(string(out)) == "1"

	if userExists {
		ui.StepOk(fmt.Sprintf("Database user '%s' exists", dbUser))
		exec.Command("sudo", "-u", "postgres", psqlBin, "-c",
			fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s';", dbUser, dbPass)).Run() //nolint:errcheck
		ui.StepOk(fmt.Sprintf("Password updated for '%s'", dbUser))
	} else {
		if err := exec.Command("sudo", "-u", "postgres", psqlBin, "-c",
			fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", dbUser, dbPass)).Run(); err != nil {
			ui.StepFail("Failed to create database user")
			return false
		}
		ui.StepOk(fmt.Sprintf("Database user '%s' created", dbUser))
	}

	out, _ = exec.Command("sudo", "-u", "postgres", psqlBin, "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s'", dbName)).Output()
	dbExists := strings.TrimSpace(string(out)) == "1"

	if dbExists {
		ui.StepOk(fmt.Sprintf("Database '%s' exists", dbName))
	} else {
		if err := exec.Command("sudo", "-u", "postgres", psqlBin, "-c",
			fmt.Sprintf("CREATE DATABASE %s OWNER %s;", dbName, dbUser)).Run(); err != nil {
			ui.StepFail("Failed to create database")
			return false
		}
		ui.StepOk(fmt.Sprintf("Database '%s' created", dbName))
	}

	exec.Command("sudo", "-u", "postgres", psqlBin, "-c",
		fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s;", dbName, dbUser)).Run() //nolint:errcheck
	ui.StepOk("Privileges granted")

	return true
}
