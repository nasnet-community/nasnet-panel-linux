package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the node agent
type Config struct {
	// Server settings
	ListenAddr string `yaml:"listen_addr"`

	// Node identity (set during deployment)
	NodeUUID string `yaml:"node_uuid"`

	// Connection mode: "direct" (default, agent listens for master) or "reverse" (agent dials master)
	ConnectMode string `yaml:"connect_mode"`

	// Hub URL for reverse mode (e.g. "master.example.com:9091")
	HubURL string `yaml:"hub_url"`

	// Xray settings
	Xray XrayConfig `yaml:"xray"`

	// TLS settings for mTLS
	TLS TLSConfig `yaml:"tls"`

	// Logging settings
	Logging LoggingConfig `yaml:"logging"`

	// Process management
	Process ProcessConfig `yaml:"process"`

	// Traffic buffering
	Traffic TrafficConfig `yaml:"traffic"`

	// Bandwidth shaping (TC)
	Bandwidth BandwidthConfig `yaml:"bandwidth"`

	// Access log capture (per-subscription DNS logs)
	AccessLog AccessLogConfig `yaml:"access_log"`

	// Internal
	FilePath string `yaml:"-"`
}

// BandwidthConfig holds settings for TC-based bandwidth shaping
type BandwidthConfig struct {
	Enabled   bool   `yaml:"enabled"`   // Enable TC bandwidth shaping
	Interface string `yaml:"interface"` // Egress network interface (e.g. "eth0")
	TotalBW   int    `yaml:"total_bw"`  // Total link bandwidth in Mbps
}

// TrafficConfig holds settings for agent-side traffic buffering
type TrafficConfig struct {
	BufferEnabled      bool          `yaml:"buffer_enabled"`
	CollectionInterval time.Duration `yaml:"collection_interval"`
	BucketDuration     time.Duration `yaml:"bucket_duration"`
	Retention          time.Duration `yaml:"retention"`
	StorePath          string        `yaml:"store_path"`
}

// Save writes the configuration to the specified path (or default path if empty)
func (c *Config) Save() error {
	if c.FilePath == "" {
		return fmt.Errorf("config file path is not set")
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to temporary file first to ensure atomic write
	tmpPath := c.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	// Rename temp file to actual file
	if err := os.Rename(tmpPath, c.FilePath); err != nil {
		return fmt.Errorf("failed to move config file: %w", err)
	}

	return nil
}

// XrayConfig holds xray-core specific settings
type XrayConfig struct {
	BinaryPath    string `yaml:"binary_path"`
	ConfigPath    string `yaml:"config_path"`
	APIAddr       string `yaml:"api_addr"`        // Local gRPC API address (e.g., 127.0.0.1:10085)
	APITimeout    int    `yaml:"api_timeout"`     // Timeout in seconds
	DataDir       string `yaml:"data_dir"`        // Directory for xray data (geoip, geosite)
	RestartDelay  int    `yaml:"restart_delay"`   // Delay between crash and restart (ms)
	MaxRestarts   int    `yaml:"max_restarts"`    // Max restarts within window before giving up
	RestartWindow int    `yaml:"restart_window"`  // Window in seconds for max_restarts
	AccessLogPath string `yaml:"access_log_path"` // Path to xray access log for DNS log capture (default: /var/log/xray/access.log)
}

// AccessLogConfig holds settings for access log (DNS) capture
type AccessLogConfig struct {
	MaxPerEmail int `yaml:"max_per_email"` // Max entries per email in ring buffer (default 200)
	MaxEmails   int `yaml:"max_emails"`    // Max distinct emails tracked (default 10000)
}

// TLSConfig holds mTLS certificate settings
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CACert     string `yaml:"ca_cert"`
	ServerCert string `yaml:"server_cert"`
	ServerKey  string `yaml:"server_key"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level      string `yaml:"level"`       // debug, info, warn, error
	Format     string `yaml:"format"`      // text, json
	Output     string `yaml:"output"`      // stdout, stderr, or file path
	MaxSize    int    `yaml:"max_size"`    // Max size in MB before rotation
	MaxBackups int    `yaml:"max_backups"` // Max number of old log files
	MaxAge     int    `yaml:"max_age"`     // Max days to retain old logs
}

// ProcessConfig holds process management settings
type ProcessConfig struct {
	ServiceMode     bool          `yaml:"service_mode"`     // Use systemd service instead of direct execution
	ServiceName     string        `yaml:"service_name"`     // Name of the systemd service (default: xray)
	GracefulTimeout time.Duration `yaml:"graceful_timeout"` // Timeout for graceful shutdown
	WatchdogEnabled bool          `yaml:"watchdog_enabled"` // Auto-restart xray on crash
	CaptureOutput   bool          `yaml:"capture_output"`   // Capture stdout/stderr for log streaming
	BufferSize      int           `yaml:"buffer_size"`      // Size of log ring buffer (lines)
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ListenAddr: ":9090",
		Xray: XrayConfig{
			BinaryPath:    "/usr/local/bin/xray",
			ConfigPath:    "/usr/local/etc/xray/config.json", // Ensure this matches systemd config path
			APIAddr:       "127.0.0.1:10085",
			APITimeout:    10,
			DataDir:       "", // empty = use config directory (where geofiles are written)
			RestartDelay:  1000,
			MaxRestarts:   5,
			RestartWindow: 60,
			AccessLogPath: "/var/log/xray/access.log",
		},
		TLS: TLSConfig{
			Enabled:    false,
			CACert:     "/etc/nasnet-agent/ca.crt",
			ServerCert: "/etc/nasnet-agent/server.crt",
			ServerKey:  "/etc/nasnet-agent/server.key",
		},
		Logging: LoggingConfig{
			Level:      "debug",
			Format:     "text",
			Output:     "stdout",
			MaxSize:    100,
			MaxBackups: 3,
			MaxAge:     7,
		},
		Process: ProcessConfig{
			ServiceMode:     true, // Default to false for backward compatibility
			ServiceName:     "xray",
			GracefulTimeout: 10 * time.Second,
			WatchdogEnabled: true,
			CaptureOutput:   true,
			BufferSize:      1000,
		},
		Bandwidth: BandwidthConfig{
			Enabled:   false,
			Interface: "eth0",
			TotalBW:   1000,
		},
		Traffic: TrafficConfig{
			BufferEnabled:      true,
			CollectionInterval: 5 * time.Second,
			BucketDuration:     1 * time.Hour,
			Retention:          7 * 24 * time.Hour,
			StorePath:          "/var/lib/nasnet-agent/traffic.json",
		},
		AccessLog: AccessLogConfig{
			MaxPerEmail: 200,
			MaxEmails:   10000,
		},
	}
}

// Load reads configuration from a file and merges with defaults
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Use defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Override with environment variables
	cfg.applyEnvOverrides()

	cfg.FilePath = path

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides
func (c *Config) applyEnvOverrides() {
	if val := os.Getenv("AGENT_LISTEN_ADDR"); val != "" {
		c.ListenAddr = val
	}
	if val := os.Getenv("XRAY_BINARY_PATH"); val != "" {
		c.Xray.BinaryPath = val
	}
	if val := os.Getenv("XRAY_CONFIG_PATH"); val != "" {
		c.Xray.ConfigPath = val
	}
	if val := os.Getenv("XRAY_API_ADDR"); val != "" {
		c.Xray.APIAddr = val
	}
	if val := os.Getenv("TLS_ENABLED"); val != "" {
		c.TLS.Enabled = val == "true" || val == "1"
	}
	if val := os.Getenv("TLS_CA_CERT"); val != "" {
		c.TLS.CACert = val
	}
	if val := os.Getenv("TLS_SERVER_CERT"); val != "" {
		c.TLS.ServerCert = val
	}
	if val := os.Getenv("TLS_SERVER_KEY"); val != "" {
		c.TLS.ServerKey = val
	}
	if val := os.Getenv("LOG_LEVEL"); val != "" {
		c.Logging.Level = val
	}
	if val := os.Getenv("TRAFFIC_BUFFER_ENABLED"); val != "" {
		c.Traffic.BufferEnabled = val == "true" || val == "1"
	}
	if val := os.Getenv("TRAFFIC_STORE_PATH"); val != "" {
		c.Traffic.StorePath = val
	}
	if val := os.Getenv("BANDWIDTH_ENABLED"); val != "" {
		c.Bandwidth.Enabled = val == "true" || val == "1"
	}
	if val := os.Getenv("BANDWIDTH_INTERFACE"); val != "" {
		c.Bandwidth.Interface = val
	}
	if val := os.Getenv("BANDWIDTH_TOTAL_BW"); val != "" {
		c.Bandwidth.TotalBW = getEnvAsInt("BANDWIDTH_TOTAL_BW", c.Bandwidth.TotalBW)
	}
	if val := os.Getenv("CONNECT_MODE"); val != "" {
		c.ConnectMode = val
	}
	if val := os.Getenv("HUB_URL"); val != "" {
		c.HubURL = val
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ConnectMode == "reverse" {
		if c.HubURL == "" {
			return fmt.Errorf("hub_url is required when connect_mode is 'reverse'")
		}
		if c.NodeUUID == "" {
			return fmt.Errorf("node_uuid is required when connect_mode is 'reverse'")
		}
	} else {
		if c.ListenAddr == "" {
			return fmt.Errorf("listen_addr is required")
		}
	}
	if c.Xray.BinaryPath == "" {
		return fmt.Errorf("xray.binary_path is required")
	}
	if c.Xray.ConfigPath == "" {
		return fmt.Errorf("xray.config_path is required")
	}
	if c.TLS.Enabled {
		if c.TLS.ServerCert == "" || c.TLS.ServerKey == "" {
			return fmt.Errorf("TLS is enabled but server_cert or server_key is missing")
		}
	}
	return nil
}

// getEnvAsInt is a helper to read int from environment
func getEnvAsInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
