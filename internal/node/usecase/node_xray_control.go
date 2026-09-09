package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
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
	state := u.getOrCreatePushState(nodeID)
	state.applyMu.Lock()
	defer state.applyMu.Unlock()
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := u.applyXrayConfigAndLogs(ctx, node, content, client); err != nil {
		return err
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

// Persist logging defaults only after the agent accepts the configuration.
func (u *nodeUsecase) applyXrayConfigAndLogs(ctx context.Context, node *domain.Node, content string, client agent.NodeClient) error {
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &configMap); err != nil || configMap == nil {
		return fmt.Errorf("invalid Xray configuration: expected a JSON object")
	}
	var settings domain.XrayLogSettings
	if raw, exists := configMap["log"]; exists {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("invalid Xray log settings: %w", err)
		}
	}
	if settings.Level != nil {
		switch *settings.Level {
		case "debug", "info", "warning", "error", "none":
		default:
			return fmt.Errorf("invalid Xray log level %q", *settings.Level)
		}
	}
	if err := client.PushConfig(ctx, content); err != nil {
		return fmt.Errorf("failed to push config to agent: %w", err)
	}
	if settings.Level != nil || settings.Access != nil || settings.Error != nil || settings.DNS != nil {
		if err := u.nodeRepo.UpdateNodeLogSettings(ctx, node.ID, settings); err != nil {
			return fmt.Errorf("configuration applied to agent, but log settings could not be persisted; retry saving: %w", err)
		}
	}
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

	// Embedded streams need their own cancellation; closing the shared local
	// client does not stop an individual shell.
	terminalCtx, cancel := context.WithCancel(ctx)
	stream, err := client.OpenTerminal(terminalCtx)
	if err != nil {
		cancel()
		client.Close()
		return nil, nil, fmt.Errorf("failed to open terminal stream: %w", err)
	}

	// Release this shell without affecting other embedded sessions.
	cleanup := func() {
		cancel()
		client.Close()
		log.WithField("node_id", nodeID).Info("[OpenTerminal] Terminal session closed")
	}

	return stream, cleanup, nil
}
