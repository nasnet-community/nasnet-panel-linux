package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// LogEntryDTO is the structured log entry sent to the frontend via SSE.
type LogEntryDTO struct {
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`
	LogType   string `json:"log_type"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

var logLevelPattern = regexp.MustCompile(`\[(Warning|Warn|Error|Debug|Info)\]`)

// parseLogLevel extracts a log level from the message text when the agent
// doesn't populate the level field. Returns "info" as default.
func parseLogLevel(message string) string {
	m := logLevelPattern.FindStringSubmatch(message)
	if len(m) > 1 {
		switch strings.ToLower(m[1]) {
		case "warning", "warn":
			return "warning"
		case "error":
			return "error"
		case "debug":
			return "debug"
		case "info":
			return "info"
		}
	}
	if strings.Contains(message, "accepted") {
		return "info"
	}
	return "info"
}

// === Agent Process Control ===

func (u *nodeUsecase) StartXray(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.StartXray(ctx); err != nil {
		return fmt.Errorf("failed to start xray: %w", err)
	}

	// Fresh stats must reflect running process — drop cached entry so
	// the next GetNodeStats goes live instead of serving pre-start data.
	u.statsCache.Invalidate(nodeID)

	// Clear user-stopped flag so scheduled tasks resume normal operation
	if node.XrayStopped {
		node.XrayStopped = false
		if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Warn("[StartXray] Failed to clear XrayStopped flag")
		}
	}

	// Update cached status if possible (fire and forget health check)
	go func() {
		// Wait a bit for process to start
		time.Sleep(1 * time.Second)
		if _, err := u.CheckAgentHealth(context.Background(), nodeID); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Debug("[StartXray] Post-start health check failed")
		}
	}()

	return nil
}

func (u *nodeUsecase) StopXray(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	// Default 5s graceful timeout
	if err := client.StopXray(ctx, 5*time.Second); err != nil {
		return fmt.Errorf("failed to stop xray: %w", err)
	}

	u.statsCache.Invalidate(nodeID)

	// Mark as user-stopped so scheduled config pushes don't auto-restart xray
	node.XrayStopped = true
	if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
		logger.GetLogger().WithError(err).WithField("node_id", nodeID).Warn("[StopXray] Failed to persist XrayStopped flag")
	}

	// Update cached status
	go func() {
		time.Sleep(1 * time.Second)
		if _, err := u.CheckAgentHealth(context.Background(), nodeID); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Debug("[StopXray] Post-stop health check failed")
		}
	}()

	return nil
}

func (u *nodeUsecase) RestartXray(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	// Validate config before restart by default
	if err := client.RestartXray(ctx, true); err != nil {
		return fmt.Errorf("failed to restart xray: %w", err)
	}

	u.statsCache.Invalidate(nodeID)

	// Clear user-stopped flag so scheduled tasks resume normal operation
	if node.XrayStopped {
		node.XrayStopped = false
		if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Warn("[RestartXray] Failed to clear XrayStopped flag")
		}
	}

	// Update cached status
	go func() {
		time.Sleep(2 * time.Second) // Wait a bit longer for restart
		if _, err := u.CheckAgentHealth(context.Background(), nodeID); err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Debug("[RestartXray] Post-restart health check failed")
		}
	}()

	return nil
}

// === Xray Config Management ===

func (u *nodeUsecase) GetNodeXrayConfig(ctx context.Context, nodeID uint) (string, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return "", err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return "", err
	}
	defer client.Close()

	configJSON, _, err := client.GetCurrentConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get config from agent: %w", err)
	}

	return configJSON, nil
}

func (u *nodeUsecase) UpdateNodeXrayConfig(ctx context.Context, nodeID uint, content string) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	// Parse content to extract log settings
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(content), &configMap); err == nil {
		if logConfig, ok := configMap["log"].(map[string]interface{}); ok {
			updated := false

			// LogLevel
			if level, ok := logConfig["loglevel"].(string); ok && level != "" {
				if node.LogLevel != level {
					node.LogLevel = level
					updated = true
				}
			}

			// LogAccess
			if access, ok := logConfig["access"].(string); ok {
				if node.LogAccess != access {
					node.LogAccess = access
					updated = true
				}
			}

			// LogError
			if errLog, ok := logConfig["error"].(string); ok {
				if node.LogError != errLog {
					node.LogError = errLog
					updated = true
				}
			}

			// dnsLog is a custom field the FE attaches to the log object.
			if dnsLog, ok := logConfig["dnsLog"].(bool); ok {
				if node.LogDNS != dnsLog {
					node.LogDNS = dnsLog
					updated = true
				}
			}

			if updated {
				if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
					logger.GetLogger().Warnf("Failed to update node log settings: %v", err)
				}
			}
		}
	}

	if err := client.PushConfig(ctx, content); err != nil {
		return fmt.Errorf("failed to push config to agent: %w", err)
	}

	// Update lastPushedConfigHash so drift detection doesn't treat
	// the manual edit as a drift and overwrite it with a regenerated config.
	go func() {
		hashCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		newClient, err := u.getAgentClient(node)
		if err != nil {
			return
		}
		defer newClient.Close()
		status, err := newClient.GetStatus(hashCtx)
		if err == nil && status.ConfigHash != "" {
			u.configHashMu.Lock()
			u.lastPushedConfigHash[nodeID] = status.ConfigHash
			u.configHashMu.Unlock()
		}
	}()

	return nil
}

func (u *nodeUsecase) ValidateNodeXrayConfig(ctx context.Context, nodeID uint, content string) (bool, []string, []string, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return false, nil, nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return false, nil, nil, err
	}
	defer client.Close()

	valid, errors, warnings, err := client.ValidateConfig(ctx, content)
	if err != nil {
		return false, nil, nil, fmt.Errorf("failed to validate config: %w", err)
	}

	return valid, errors, warnings, nil
}

func (u *nodeUsecase) StreamNodeLogs(ctx context.Context, nodeID uint, tail int, follow bool) (<-chan LogEntryDTO, <-chan error, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, nil, err
	}

	stream, err := client.StreamLogs(ctx, tail, follow)
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("failed to start log stream: %w", err)
	}

	logs := make(chan LogEntryDTO)
	errs := make(chan error)

	// Subscribe to EventBus for xray status events on this node
	var eventCh events.Subscriber
	var eventSubID string
	if u.eventBus != nil && follow {
		eventSubID = fmt.Sprintf("log-stream-%d-%d", nodeID, time.Now().UnixNano())
		eventCh = u.eventBus.Subscribe(eventSubID)
	}

	// Receive gRPC log messages in a sub-goroutine since Recv() blocks
	type grpcMsg struct {
		entry *pb.LogEntry
		err   error
	}
	grpcCh := make(chan grpcMsg, 16)
	go func() {
		defer close(grpcCh)
		for {
			msg, err := stream.Recv()
			if err != nil {
				grpcCh <- grpcMsg{err: err}
				return
			}
			grpcCh <- grpcMsg{entry: msg}
		}
	}()

	go func() {
		defer close(logs)
		defer close(errs)
		defer client.Close()
		if eventCh != nil {
			defer u.eventBus.Unsubscribe(eventSubID)
		}

		for {
			select {
			case gm, ok := <-grpcCh:
				if !ok {
					return
				}
				if gm.err != nil {
					errs <- gm.err
					return
				}
				msg := gm.entry

				level := msg.Level
				if level == "" {
					level = parseLogLevel(msg.Message)
				}

				logType := "all"
				switch msg.LogType {
				case pb.LogType_LOG_TYPE_ACCESS:
					logType = "access"
				case pb.LogType_LOG_TYPE_ERROR:
					logType = "error"
				}

				ts := msg.Timestamp
				if ts == 0 {
					ts = time.Now().UnixMilli()
				}

				logs <- LogEntryDTO{
					Timestamp: ts,
					Level:     level,
					LogType:   logType,
					Message:   msg.Message,
					Source:    msg.Source,
				}

			case evt, ok := <-eventCh:
				if !ok {
					return
				}
				entry, match := xrayEventToLogEntry(nodeID, evt)
				if match {
					logs <- entry
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return logs, errs, nil
}

// xrayEventToLogEntry converts an xray EventBus event into a LogEntryDTO
// if it matches the given nodeID. Returns false if the event is not relevant.
func xrayEventToLogEntry(nodeID uint, evt events.Event) (LogEntryDTO, bool) {
	payload, ok := evt.Payload.(events.XrayStatusPayload)
	if !ok || payload.NodeID != nodeID {
		return LogEntryDTO{}, false
	}

	var level, message string
	switch evt.Type {
	case events.EventXrayDown:
		level = "error"
		message = fmt.Sprintf("Xray process crashed (crash #%d)", payload.CrashCount)
	case events.EventXrayUp:
		level = "info"
		message = fmt.Sprintf("Xray process recovered (after %d crash(es))", payload.CrashCount)
	case events.EventXrayCrashLoop:
		level = "error"
		if payload.Message != "" {
			message = fmt.Sprintf("Xray crash loop detected (%d crashes): %s", payload.CrashCount, payload.Message)
		} else {
			message = fmt.Sprintf("Xray crash loop detected (%d crashes)", payload.CrashCount)
		}
	default:
		return LogEntryDTO{}, false
	}

	return LogEntryDTO{
		Timestamp: evt.Timestamp.UnixMilli(),
		Level:     level,
		LogType:   "error",
		Message:   message,
		Source:    "system",
	}, true
}

// GetNodeRecentLogs fetches the last N log lines from the agent (non-streaming).
func (u *nodeUsecase) GetNodeRecentLogs(ctx context.Context, nodeID uint, lines int) ([]LogEntryDTO, error) {
	if lines <= 0 {
		lines = 50
	}

	logsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	logsCh, errsCh, err := u.StreamNodeLogs(logsCtx, nodeID, lines, false)
	if err != nil {
		return nil, err
	}

	var result []LogEntryDTO
	for {
		select {
		case entry, ok := <-logsCh:
			if !ok {
				return result, nil
			}
			result = append(result, entry)
		case err, ok := <-errsCh:
			if !ok {
				return result, nil
			}
			// Stream ended with error (EOF is expected for follow=false)
			if strings.Contains(err.Error(), "EOF") {
				return result, nil
			}
			return result, err
		case <-logsCtx.Done():
			return result, nil
		}
	}
}

// AutoUpdateAgent automatically updates the agent on a node using local binaries
func (u *nodeUsecase) AutoUpdateAgent(ctx context.Context, nodeID uint, progress chan<- UpdateProgress) error {
	log := logger.GetLogger().WithField("node_id", nodeID)

	// Helper to send progress
	sendProgress := func(step, message, status string, err error) {
		if progress == nil {
			return
		}

		p := UpdateProgress{
			Step:    step,
			Message: message,
			Status:  status,
		}
		if err != nil {
			p.Error = err.Error()
			p.Status = "error"
		}

		// Non-blocking send
		select {
		case progress <- p:
		default:
		}
	}

	sendProgress("init", "Initializing update process...", "running", nil)

	// 1. Get Node
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		sendProgress("init", "Failed to get node info", "error", err)
		return err
	}

	sendProgress("init", "Node info retrieved", "success", nil)
	sendProgress("connect", "Connecting to agent...", "running", nil)

	// 2. Connect to agent to get architecture
	client, err := u.getAgentClient(node)
	if err != nil {
		sendProgress("connect", "Failed to connect to agent", "error", err)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}

	// Get Host Info
	hostInfo, err := client.GetHostInfo(ctx)
	if err != nil {
		closeAgentClient(client)
		sendProgress("check_arch", "Failed to get host info", "error", err)
		return fmt.Errorf("failed to get host info: %w", err)
	}
	closeAgentClient(client) // Close connection, we will reconnect in UpdateAgentBinary

	sendProgress("connect", "Connected to agent", "success", nil)
	sendProgress("check_arch", fmt.Sprintf("Detected system: %s/%s", hostInfo.OS, hostInfo.Arch), "running", nil)

	// 3. Determine binary path
	// Expected format: bin/agent/nasnet-agent-{os}-{arch}
	// e.g. bin/agent/nasnet-agent-linux-amd64
	osName := strings.ToLower(hostInfo.OS)
	arch := strings.ToLower(hostInfo.Arch)

	// Normalize common discrepancies if any (usually Go's runtime.GOOS/GOARCH match build targets)
	if arch == "x86_64" {
		arch = "amd64"
	} else if arch == "aarch64" {
		arch = "arm64"
	}

	sendProgress("check_arch", fmt.Sprintf("Target architecture: %s-%s", osName, arch), "success", nil)
	sendProgress("prep_binary", "Locating binary...", "running", nil)

	binaryName := fmt.Sprintf("nasnet-agent-%s-%s", osName, arch)
	binaryPath := fmt.Sprintf("bin/agent/%s", binaryName)

	log.WithFields(map[string]interface{}{
		"node_id": nodeID,
		"os":      osName,
		"arch":    arch,
		"path":    binaryPath,
	}).Info("[AutoUpdateAgent] Located update binary")

	// 4. Read binary file
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		sendProgress("prep_binary", "Binary file not found on server", "error", err)
		return fmt.Errorf("failed to read binary file %s: %w. Make sure to run 'make build-agent'", binaryPath, err)
	}

	// 5. Calculate checksum
	sendProgress("prep_binary", "Verifying binary integrity...", "running", nil)
	hash := sha256.Sum256(content)
	checksum := hex.EncodeToString(hash[:])

	// Log checksum for debugging
	log.WithField("checksum", checksum).Info("[AutoUpdateAgent] Calculated new binary checksum")

	// Load binary signature if available
	sigPath := binaryPath + ".sig"
	signature, sigErr := os.ReadFile(sigPath)
	if sigErr != nil {
		log.WithField("sig_path", sigPath).Warn("[AutoUpdateAgent] No signature file found; update may be rejected by agents with signature verification enabled")
		signature = nil
	} else {
		log.WithField("sig_path", sigPath).Info("[AutoUpdateAgent] Loaded binary signature")
	}

	sendProgress("prep_binary", "Binary Verified", "success", nil)
	sendProgress("verify", "Checking current version...", "running", nil)

	// 6. Check if agent already has this binary (via GetSelfChecksum)
	// Reconnect to agent
	client, err = u.getAgentClient(node)
	if err == nil {
		remoteChecksum, err := client.GetSelfChecksum(ctx)
		closeAgentClient(client)

		// If we got a checksum and it matches, we can skip update
		if err == nil && remoteChecksum != nil {
			log.WithFields(map[string]interface{}{
				"remote_checksum": remoteChecksum.Checksum,
				"local_checksum":  checksum,
			}).Info("[AutoUpdateAgent] Compared checksums")

			if remoteChecksum.Checksum == checksum {
				log.Info("[AutoUpdateAgent] Agent already running latest binary, skipping update")
				sendProgress("verify", "Agent is already up-to-date", "success", nil)
				sendProgress("complete", "Update completed", "success", nil)
				return nil
			}
		} else {
			// Expected for older agents that don't implementation GetSelfChecksum
			log.Warnf("[AutoUpdateAgent] Could not get remote checksum (likely old agent version): %v", err)
		}
	} else {
		log.Warnf("[AutoUpdateAgent] Failed to reconnect for checksum check: %v", err)
	}

	sendProgress("verify", "New version available", "success", nil)

	// 7. Get current version of the binary (hacky? or we pass a version flag?)
	// For now, we use a timestamp or a generic version since we rely on the binary being "latest"
	version := fmt.Sprintf("auto-%d", time.Now().Unix())

	// 8. Push update
	sendProgress("upload", "Uploading binary...", "running", nil)
	sendProgress("install", "Waiting for installation...", "pending", nil)

	if err := u.UpdateAgentBinary(ctx, nodeID, content, checksum, version, signature); err != nil {
		sendProgress("upload", "Upload failed", "error", err)
		return err
	}

	sendProgress("upload", "Upload complete", "success", nil)
	sendProgress("install", "Installing and restarting agent...", "running", nil)

	// Wait for the agent to restart (UpdateAgentBinary returns before the restart actually fires).
	time.Sleep(2 * time.Second)

	sendProgress("install", "Installation command sent", "success", nil)
	sendProgress("complete", "Update sequence finished", "success", nil)

	return nil
}

// OpenTerminal opens an interactive PTY terminal session to a node via the agent
func (u *nodeUsecase) OpenTerminal(ctx context.Context, nodeID uint) (pb.NodeAgent_OpenTerminalClient, func(), error) {
	log := logger.GetLogger()
	log.WithField("node_id", nodeID).Info("[OpenTerminal] Opening terminal session")

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, nil, ErrNodeNotFound
	}

	if !node.IsOnline {
		return nil, nil, fmt.Errorf("node is offline")
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to agent: %w", err)
	}

	stream, err := client.OpenTerminal(ctx)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to open terminal stream: %w", err)
	}

	// Return cleanup function that closes the client
	cleanup := func() {
		client.Close()
		log.WithField("node_id", nodeID).Info("[OpenTerminal] Terminal session closed")
	}

	return stream, cleanup, nil
}
