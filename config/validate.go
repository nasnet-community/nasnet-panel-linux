package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidationError represents a single validation error or warning
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult holds all validation errors and warnings
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError
}

// HasErrors returns true if there are any validation errors
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// String returns a human-readable representation of all errors and warnings
func (r *ValidationResult) String() string {
	var sb strings.Builder
	if len(r.Errors) > 0 {
		sb.WriteString("Configuration Errors:\n")
		for _, e := range r.Errors {
			fmt.Fprintf(&sb, "  - %s\n", e)
		}
	}
	if len(r.Warnings) > 0 {
		sb.WriteString("Configuration Warnings:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&sb, "  - %s\n", w)
		}
	}
	return sb.String()
}

func (r *ValidationResult) addError(field, msg string) {
	r.Errors = append(r.Errors, ValidationError{Field: field, Message: msg})
}

func (r *ValidationResult) addWarning(field, msg string) {
	r.Warnings = append(r.Warnings, ValidationError{Field: field, Message: msg})
}

// Validate checks the configuration for errors and warnings
func (c *Config) Validate() *ValidationResult {
	r := &ValidationResult{}

	// Database driver validation
	validDrivers := map[string]bool{"postgres": true, "sqlite": true}
	if !validDrivers[strings.ToLower(c.Database.Driver)] {
		r.addError("Database.Driver", fmt.Sprintf("must be one of: postgres, sqlite; got %q", c.Database.Driver))
	}

	// Required fields (conditional on driver)
	if strings.ToLower(c.Database.Driver) != "sqlite" {
		if c.Database.Host == "" {
			r.addError("Database.Host", "database host is required (DB_HOST)")
		}
		if c.Database.User == "" {
			r.addError("Database.User", "database user is required (DB_USER)")
		}
		if c.Database.Password == "" {
			r.addError("Database.Password", "database password is required (DB_PASSWORD)")
		}
		if c.Database.Name == "" {
			r.addError("Database.Name", "database name is required (DB_NAME)")
		}
	} else {
		if c.Database.Path == "" {
			r.addError("Database.Path", "database path is required for SQLite (DB_PATH)")
		}
	}
	if c.JWT.SecretKey == "" {
		r.addError("JWT.SecretKey", "JWT secret key is required (JWT_SECRET_KEY)")
	}
	if c.Telegram.Enabled && c.Telegram.BotToken == "" {
		r.addError("Telegram.BotToken", "Telegram bot token is required when Telegram is enabled (TELEGRAM_BOT_TOKEN)")
	}

	// Port validation
	if c.App.Port < 1 || c.App.Port > 65535 {
		r.addError("App.Port", fmt.Sprintf("port must be between 1 and 65535, got %d", c.App.Port))
	}
	if strings.ToLower(c.Database.Driver) != "sqlite" && (c.Database.Port < 1 || c.Database.Port > 65535) {
		r.addError("Database.Port", fmt.Sprintf("port must be between 1 and 65535, got %d", c.Database.Port))
	}

	// Log level validation
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Log.Level != "" && !validLogLevels[strings.ToLower(c.Log.Level)] {
		r.addError("Log.Level", fmt.Sprintf("must be one of: debug, info, warn, error; got %q", c.Log.Level))
	}

	// Log format validation
	validLogFormats := map[string]bool{"text": true, "json": true}
	if c.Log.Format != "" && !validLogFormats[strings.ToLower(c.Log.Format)] {
		r.addError("Log.Format", fmt.Sprintf("must be one of: text, json; got %q", c.Log.Format))
	}

	// Bot mode validation (only relevant when Telegram is enabled)
	if c.Telegram.Enabled {
		validBotModes := map[string]bool{"polling": true, "webhook": true}
		if c.Telegram.BotMode != "" && !validBotModes[strings.ToLower(c.Telegram.BotMode)] {
			r.addError("Telegram.BotMode", fmt.Sprintf("must be one of: polling, webhook; got %q", c.Telegram.BotMode))
		}

		// Conditional: webhook mode requires webhook URL
		if strings.ToLower(c.Telegram.BotMode) == "webhook" && c.Telegram.WebhookURL == "" {
			r.addError("Telegram.WebhookURL", "webhook URL is required when bot mode is 'webhook'")
		}
		if c.Telegram.WebhookURL != "" {
			if _, err := url.ParseRequestURI(c.Telegram.WebhookURL); err != nil {
				r.addError("Telegram.WebhookURL", fmt.Sprintf("invalid URL: %v", err))
			}
		}
	}

	// Conditional: ACME email format
	if c.ACME.Email != "" && !strings.Contains(c.ACME.Email, "@") {
		r.addError("ACME.Email", fmt.Sprintf("invalid email format: %q", c.ACME.Email))
	}

	// Conditional: proxy enabled requires host + port (only when Telegram is enabled)
	if c.Telegram.Enabled && c.Telegram.Proxy.Enabled {
		if c.Telegram.Proxy.Host == "" {
			r.addError("Telegram.Proxy.Host", "proxy host is required when proxy is enabled")
		}
		if c.Telegram.Proxy.Port < 1 || c.Telegram.Proxy.Port > 65535 {
			r.addError("Telegram.Proxy.Port", fmt.Sprintf("proxy port must be between 1 and 65535, got %d", c.Telegram.Proxy.Port))
		}
	}

	// JWT secret strength validation
	if c.JWT.SecretKey != "" && len(c.JWT.SecretKey) < 32 {
		r.addWarning("JWT.SecretKey", fmt.Sprintf("JWT secret key is only %d characters — use at least 32 characters for security", len(c.JWT.SecretKey)))
	}
	if c.App.Env == "production" && !c.JWT.CookieSecure {
		r.addWarning("JWT.CookieSecure", "CookieSecure is false in production — cookies will not be secure")
	}

	// Metrics auth warnings
	hasMetricsUser := c.Metrics.Username != ""
	hasMetricsPass := c.Metrics.Password != ""
	if hasMetricsUser != hasMetricsPass {
		r.addWarning("Metrics.Auth", "only one of METRICS_USERNAME / METRICS_PASSWORD is set — both are required for auth, endpoint will remain public")
	}
	if c.Metrics.Enabled && c.App.Env == "production" && !hasMetricsUser && !hasMetricsPass {
		r.addWarning("Metrics.Auth", "metrics endpoint is public in production — set METRICS_USERNAME and METRICS_PASSWORD to protect it")
	}

	// Panel base path validation
	if c.App.PanelBasePath != "" {
		if !strings.HasPrefix(c.App.PanelBasePath, "/") {
			r.addError("App.PanelBasePath", "panel base path must start with /")
		}
		if strings.HasSuffix(c.App.PanelBasePath, "/") {
			r.addError("App.PanelBasePath", "panel base path must not end with /")
		}
		for _, ch := range c.App.PanelBasePath[1:] {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '/') {
				r.addError("App.PanelBasePath", fmt.Sprintf("panel base path contains invalid character: %q", string(ch)))
				break
			}
		}
	}

	return r
}
