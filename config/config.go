package config

import (
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Telegram TelegramConfig
	Log      LogConfig
	Xray     XrayConfig
	Admin    AdminConfig
	ACME     ACMEConfig
	JWT      JWTConfig
	Metrics  MetricsConfig
}

type MetricsConfig struct {
	Enabled  bool   // METRICS_ENABLED (default: true)
	Path     string // METRICS_PATH (default: "/metrics")
	Username string // METRICS_USERNAME (empty = no auth)
	Password string // METRICS_PASSWORD (empty = no auth)
}

type ACMEConfig struct {
	Enabled   bool
	Email     string
	CacheDir  string
	Staging   bool
	AutoRenew bool
}

type AppConfig struct {
	Env           string
	Port          int
	BaseURL       string   // Base URL for subscription links (e.g. https://sub.example.com)
	BackupDir     string   // Directory for database backups
	SubPanelURL   string   // URL of the subscription panel (e.g. https://panel.example.com). When set, browser requests to /sub/:uuid are redirected here.
	TLSCertFile   string   // Path to TLS certificate PEM file
	TLSKeyFile    string   // Path to TLS private key PEM file
	PanelBasePath string   // URL path prefix for admin panel (e.g., /x7k2m9)
	CORSOrigins   []string // CORS_ORIGINS (comma-separated)
}

type DatabaseConfig struct {
	Driver          string // "postgres" or "sqlite"
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	Path            string // SQLite database file path
	MaxOpenConns    int    // DB_MAX_OPEN_CONNS (default: 25)
	MaxIdleConns    int    // DB_MAX_IDLE_CONNS (default: 10)
	ConnMaxLifetime int    // DB_CONN_MAX_LIFETIME_MINUTES (default: 5)
	ConnMaxIdleTime int    // DB_CONN_MAX_IDLE_MINUTES (default: 3)
}

type TelegramConfig struct {
	Enabled    bool // TELEGRAM_ENABLED (default: true for backward compatibility)
	BotToken   string
	BotMode    string // "polling" or "webhook"
	WebhookURL string // specific URL for webhook (optional, can be auto-generated)
	Proxy      ProxyConfig
}

type ProxyConfig struct {
	Enabled  bool
	Type     string // "socks5" (future: "http")
	Host     string
	Port     int
	Username string
	Password string
}

type LogConfig struct {
	Level  string
	Format string
}

type XrayConfig struct {
	APITimeout int
	InboundTag string
}

type AdminConfig struct {
	InitialAdminIDs []int64
	// Admin Panel Login Credentials
	Username     string // Admin panel login username
	PasswordHash string // bcrypt hashed password
}

type JWTConfig struct {
	SecretKey          string
	AccessTokenExpiry  int // in minutes
	RefreshTokenExpiry int // in hours
	CookieDomain       string
	CookieSecure       bool
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		logrus.Warn("No .env file found, using environment variables")
	}

	return &Config{
		App: AppConfig{
			Env:     getEnv("APP_ENV", "development"),
			Port:    getEnvAsInt("APP_PORT", 9761),
			BaseURL: getEnv("APP_BASE_URL", ""),

			BackupDir:     getEnv("BACKUP_DIR", "./data/backups"),
			SubPanelURL:   getEnv("SUB_PANEL_URL", ""),
			TLSCertFile:   getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:    getEnv("TLS_KEY_FILE", ""),
			PanelBasePath: CleanBasePath(getEnv("APP_PANEL_BASE_PATH", "")),
			CORSOrigins:   getEnvAsStringSlice("CORS_ORIGINS"),
		},
		Database: DatabaseConfig{
			Driver:          getEnv("DB_DRIVER", "postgres"),
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvAsInt("DB_PORT", 5432),
			User:            getEnv("DB_USER", "postgres"),
			Password:        getEnv("DB_PASSWORD", "postgres"),
			Name:            getEnv("DB_NAME", "nasnet_panel"),
			SSLMode:         getEnv("DB_SSL_MODE", "disable"),
			Path:            getEnv("DB_PATH", "./data/nasnet_panel.db"),
			MaxOpenConns:    getEnvAsInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvAsInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvAsInt("DB_CONN_MAX_LIFETIME_MINUTES", 5),
			ConnMaxIdleTime: getEnvAsInt("DB_CONN_MAX_IDLE_MINUTES", 3),
		},
		Telegram: TelegramConfig{
			Enabled:    getEnvAsBool("TELEGRAM_ENABLED", false),
			BotToken:   getEnv("TELEGRAM_BOT_TOKEN", ""),
			BotMode:    getEnv("BOT_MODE", "polling"),
			WebhookURL: getEnv("WEBHOOK_URL", ""),
			Proxy: ProxyConfig{
				Enabled:  getEnvAsBool("TELEGRAM_PROXY_ENABLED", false),
				Type:     getEnv("TELEGRAM_PROXY_TYPE", "socks5"),
				Host:     getEnv("TELEGRAM_PROXY_HOST", ""),
				Port:     getEnvAsInt("TELEGRAM_PROXY_PORT", 1080),
				Username: getEnv("TELEGRAM_PROXY_USERNAME", ""),
				Password: getEnv("TELEGRAM_PROXY_PASSWORD", ""),
			},
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "debug"),
			Format: getEnv("LOG_FORMAT", "text"),
		},
		Xray: XrayConfig{
			APITimeout: getEnvAsInt("XRAY_API_TIMEOUT", 5),
			InboundTag: getEnv("XRAY_INBOUND_TAG", "vmess-in"),
		},
		Admin: AdminConfig{
			InitialAdminIDs: getEnvAsInt64Slice("ADMIN_IDS"),
			Username:        getEnv("ADMIN_USERNAME", "admin"),
			PasswordHash:    getEnv("ADMIN_PASSWORD_HASH", ""),
		},
		ACME: ACMEConfig{
			Enabled:   getEnvAsBool("ACME_ENABLED", false),
			Email:     getEnv("ACME_EMAIL", ""),
			CacheDir:  getEnv("ACME_CACHE_DIR", "./data/acme"),
			Staging:   getEnvAsBool("ACME_STAGING", false),
			AutoRenew: getEnvAsBool("ACME_AUTO_RENEW", true),
		},
		JWT: JWTConfig{
			SecretKey:          getEnv("JWT_SECRET_KEY", ""),
			AccessTokenExpiry:  getEnvAsInt("JWT_ACCESS_EXPIRY", 60),   // 60 minutes
			RefreshTokenExpiry: getEnvAsInt("JWT_REFRESH_EXPIRY", 168), // 7 days (168 hours)
			CookieDomain:       getEnv("JWT_COOKIE_DOMAIN", ""),
			CookieSecure:       getEnvAsBool("JWT_COOKIE_SECURE", false), // Set true in production
		},
		Metrics: MetricsConfig{
			Enabled:  getEnvAsBool("METRICS_ENABLED", false),
			Path:     getEnv("METRICS_PATH", "/metrics"),
			Username: getEnv("METRICS_USERNAME", ""),
			Password: getEnv("METRICS_PASSWORD", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsInt64Slice(key string) []int64 {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			result = append(result, id)
		}
	}
	return result
}

func getEnvAsStringSlice(key string) []string {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// CleanBasePath normalises a URL path prefix: ensures a leading slash,
// removes trailing slashes, and returns "" for empty/root paths.
func CleanBasePath(p string) string {
	if p == "" {
		return ""
	}
	p = path.Clean("/" + p)
	if p == "/" {
		return ""
	}
	return p
}
