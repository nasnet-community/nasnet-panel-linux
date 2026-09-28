package usecase

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
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

// SwitchConnectMode live-switches a node between "direct" and "reverse" connect modes.
// It validates preconditions, writes a new agent config via the current connection,
// updates the DB record, and triggers a heartbeat sync.
func (u *nodeUsecase) SwitchConnectMode(ctx context.Context, nodeID uint, newMode string) error {
	log := logger.GetLogger().WithField("node_id", nodeID).WithField("new_mode", newMode)

	// --- Validate mode value ---
	if newMode == "local" {
		return ErrLocalNode // there is one built-in node, made at boot
	}
	if newMode != "direct" && newMode != "reverse" {
		return fmt.Errorf("invalid connect mode %q: must be \"direct\" or \"reverse\"", newMode)
	}

	// --- Fetch node ---
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("node %d not found: %w", nodeID, err)
	}

	// --- Validate preconditions ---
	if node.IsLocal() {
		return ErrLocalNode
	}
	if node.IsStealth {
		return fmt.Errorf("cannot switch connect mode on stealth node %d", nodeID)
	}
	if node.ConnectMode == newMode {
		return fmt.Errorf("node %d is already in %q mode", nodeID, newMode)
	}
	if !node.IsOnline {
		return fmt.Errorf("node %d is offline; cannot switch connect mode", nodeID)
	}
	if newMode == "direct" && node.IP == "" {
		return fmt.Errorf("node %d has no IP address set; required for direct mode", nodeID)
	}

	log.WithField("current_mode", node.ConnectMode).Info("[SwitchConnectMode] Starting mode switch")

	// --- Get agent client via current connection mode ---
	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to get agent client for node %d: %w", nodeID, err)
	}
	defer client.Close()

	// --- Backup existing config ---
	log.Info("[SwitchConnectMode] Backing up agent config")
	backupCtx, backupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer backupCancel()
	if _, err := client.ExecuteCommand(backupCtx, agentBackupCmd, 10); err != nil {
		return fmt.Errorf("failed to backup agent config: %w", err)
	}

	// --- Build new config YAML ---
	var configBytes []byte
	switch newMode {
	case "reverse":
		hub := u.hubURL(ctx)
		configBytes = []byte(fmt.Sprintf(reverseConfigTemplate, hub, node.UUID))
	case "direct":
		configBytes = []byte(fmt.Sprintf(directConfigTemplate, node.AgentPort))
	}

	// --- Write new config to agent ---
	log.Info("[SwitchConnectMode] Writing new agent config")
	writeCtx, writeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer writeCancel()
	if _, err := client.WriteFile(writeCtx, agentConfigPath, configBytes, 0600); err != nil {
		return fmt.Errorf("failed to write new agent config: %w", err)
	}

	// --- Open firewall port when switching to direct mode ---
	if newMode == "direct" {
		log.WithField("agent_port", node.AgentPort).Info("[SwitchConnectMode] Opening firewall port for direct mode")
		fwCmd := fmt.Sprintf("iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || ufw allow %d/tcp 2>/dev/null; true", node.AgentPort, node.AgentPort)
		fwCtx, fwCancel := context.WithTimeout(ctx, 15*time.Second)
		defer fwCancel()
		if _, err := client.ExecuteCommand(fwCtx, fwCmd, 10); err != nil {
			log.WithError(err).Warn("[SwitchConnectMode] Firewall port open returned error (non-fatal)")
		}
	}

	// --- Update DB ---
	node.ConnectMode = newMode
	if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
		return fmt.Errorf("failed to update node %d connect mode in DB: %w", nodeID, err)
	}
	log.Info("[SwitchConnectMode] DB updated")

	// --- Trigger agent restart (error expected due to connection drop, ignore) ---
	log.Info("[SwitchConnectMode] Triggering agent restart")
	restartCtx, restartCancel := context.WithTimeout(ctx, 8*time.Second)
	defer restartCancel()
	_, _ = client.ExecuteCommand(restartCtx, agentRestartCmd, 5)

	// --- Trigger heartbeat sync to pick up new mode ---
	if u.heartbeatMgr != nil {
		u.heartbeatMgr.TriggerSync()
		log.Info("[SwitchConnectMode] Heartbeat sync triggered")
	}

	log.Info("[SwitchConnectMode] Mode switch complete")
	return nil
}
