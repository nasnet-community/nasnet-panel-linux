package usecase

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	agentConfigPath   = "/etc/nasnet-agent/config.yaml"
	agentConfigBackup = "/etc/nasnet-agent/config.yaml.bak"
	agentRestartCmd   = "nohup sh -c 'sleep 2 && systemctl restart nasnet-agent' >/dev/null 2>&1 &"
	agentBackupCmd    = "cp " + agentConfigPath + " " + agentConfigBackup
)

var reverseConfigTemplate = `connect_mode: reverse
hub_url: "%s"
node_uuid: "%s"
tls:
  enabled: true
  ca_cert: /etc/nasnet-agent/ca.crt
  server_cert: /etc/nasnet-agent/server.crt
  server_key: /etc/nasnet-agent/server.key
xray:
  binary_path: /usr/local/bin/xray
  config_path: /usr/local/etc/xray/config.json
  api_addr: 127.0.0.1:10085
  api_timeout: 10
  restart_delay: 1000
  max_restarts: 5
  restart_window: 60
traffic:
  buffer_enabled: true
  store_path: /var/lib/nasnet-agent/traffic.json
process:
  service_mode: true
  service_name: xray
  graceful_timeout: 10s
  watchdog_enabled: true
  capture_output: true
  buffer_size: 1000
logging:
  level: info
  format: text
  output: stdout
`

var directConfigTemplate = `listen_addr: ":%d"
tls:
  enabled: true
  ca_cert: /etc/nasnet-agent/ca.crt
  server_cert: /etc/nasnet-agent/server.crt
  server_key: /etc/nasnet-agent/server.key
xray:
  binary_path: /usr/local/bin/xray
  config_path: /usr/local/etc/xray/config.json
  api_addr: 127.0.0.1:10085
  api_timeout: 10
  restart_delay: 1000
  max_restarts: 5
  restart_window: 60
traffic:
  buffer_enabled: true
  store_path: /var/lib/nasnet-agent/traffic.json
process:
  service_mode: true
  service_name: xray
  graceful_timeout: 10s
  watchdog_enabled: true
  capture_output: true
  buffer_size: 1000
logging:
  level: info
  format: text
  output: stdout
`

// hubURL derives the gRPC hub address for reverse-mode agents.
// Reads hub_port from DB settings, falls back to HUB_LISTEN_ADDR env, then default 9091.
// Reads hostname from app_base_url setting, falls back to APP_BASE_URL env.
// Returns plain "host:port" (no scheme) — gRPC DialContext expects this format.
func (u *nodeUsecase) hubURL(ctx context.Context) string {
	// Resolve port: DB setting > env var > default
	port := 9091
	if u.settingUC != nil {
		if v, err := u.settingUC.GetByKey(ctx, "hub_port"); err == nil && v != "" {
			if p, _ := strconv.Atoi(v); p > 0 {
				port = p
			}
		}
	} else if listenAddr := os.Getenv("HUB_LISTEN_ADDR"); listenAddr != "" {
		trimmed := strings.TrimPrefix(listenAddr, ":")
		if p, err := strconv.Atoi(trimmed); err == nil && p > 0 {
			port = p
		}
	}

	// Resolve hostname: agent_hub_url (routers sit on a LAN base URL) >
	// app_base_url > env var > localhost
	baseURL := ""
	if u.settingUC != nil {
		if v, err := u.settingUC.GetByKey(ctx, "agent_hub_url"); err == nil && strings.TrimSpace(v) != "" {
			baseURL = strings.TrimSpace(v)
			if !strings.Contains(baseURL, "://") {
				baseURL = "https://" + baseURL // url.Parse needs a scheme to find the host
			}
		} else if v, err := u.settingUC.GetByKey(ctx, "app_base_url"); err == nil && v != "" {
			baseURL = v
		}
	}
	if baseURL == "" {
		baseURL = os.Getenv("APP_BASE_URL")
	}
	if baseURL == "" {
		return fmt.Sprintf("localhost:%d", port)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Sprintf("localhost:%d", port)
	}

	host := parsed.Hostname()
	return fmt.Sprintf("%s:%d", host, port)
}
