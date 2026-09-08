package tool

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

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
	existingOrDefault := func(key, fallback string) string {
		if value := ReadEnvValue(key, cfg.EnvFile); value != "" {
			return value
		}
		return fallback
	}
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
		sb.WriteString(fmt.Sprintf("DB_PATH=%s\n", existingOrDefault("DB_PATH", dbPath)))
	} else {
		sb.WriteString(fmt.Sprintf("DB_HOST=%s\n", existingOrDefault("DB_HOST", "localhost")))
		sb.WriteString(fmt.Sprintf("DB_PORT=%s\n", existingOrDefault("DB_PORT", "5432")))
		sb.WriteString(fmt.Sprintf("DB_USER=%s\n", existingOrDefault("DB_USER", "postgres")))
		sb.WriteString(fmt.Sprintf("DB_PASSWORD=%s\n", wv.DBPassword))
		sb.WriteString(fmt.Sprintf("DB_NAME=%s\n", existingOrDefault("DB_NAME", "nasnet_panel")))
		sb.WriteString(fmt.Sprintf("DB_SSL_MODE=%s\n", existingOrDefault("DB_SSL_MODE", "disable")))
		if service := ReadEnvValue("PGSQL_SERVICE_NAME", cfg.EnvFile); service != "" {
			sb.WriteString(fmt.Sprintf("PGSQL_SERVICE_NAME=%s\n", service))
		}
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
	tlsCert, tlsKey := "", ""
	if strings.HasPrefix(wv.AppBaseURL, "https://") && wv.AcmeEnabled != "true" {
		tlsCert = ReadEnvValue("TLS_CERT_FILE", cfg.EnvFile)
		tlsKey = ReadEnvValue("TLS_KEY_FILE", cfg.EnvFile)
	}
	sb.WriteString(fmt.Sprintf("TLS_CERT_FILE=%s\n", tlsCert))
	sb.WriteString(fmt.Sprintf("TLS_KEY_FILE=%s\n", tlsKey))
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
