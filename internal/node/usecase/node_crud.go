package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

func (u *nodeUsecase) CreateNode(ctx context.Context, name, ip, country, datacenter string, apiPort, agentPort int, connectMode string, isStealth, isPersistentStealth bool) (*domain.Node, error) {
	node := &domain.Node{
		Name:                name,
		IP:                  ip,
		CountryCode:         country,
		Datacenter:          datacenter,
		APIPort:             apiPort,
		AgentPort:           agentPort,
		ConnectMode:         connectMode,
		IsStealth:           isStealth,
		IsPersistentStealth: isPersistentStealth,
	}

	// Warn if another active node uses the same IP:Port
	if existingNodes, err := u.nodeRepo.ListNodes(ctx); err == nil {
		for _, existing := range existingNodes {
			if existing.IP == ip && existing.AgentPort == agentPort && existing.IsActive {
				logger.GetLogger().Warnf("[CreateNode] Warning: another active node (ID %d, %q) already uses %s:%d",
					existing.ID, existing.Name, ip, agentPort)
				break
			}
		}
	}

	if err := u.nodeRepo.CreateNode(ctx, node); err != nil {
		return nil, err
	}

	// Add default outbounds: direct (freedom) and blocked (blackhole)
	defaultOutbounds := []*domain.Outbound{
		{
			NodeID:   node.ID,
			Tag:      "direct",
			Protocol: "freedom",
			Remark:   "Direct",
		},
		{
			NodeID:            node.ID,
			Tag:               "blocked",
			Protocol:          "blackhole",
			Remark:            "Blocked",
			BlackholeSettings: &domain.BlackholeSettings{ResponseType: "none"},
		},
	}
	for _, ob := range defaultOutbounds {
		if err := u.nodeRepo.CreateOutbound(ctx, ob); err != nil {
			logger.GetLogger().Warnf("[CreateNode] Failed to create default outbound %q: %v", ob.Tag, err)
		}
	}

	// Trigger background tasks (GeoIP, etc)
	go u.triggerBackgroundNodeTasks(node.ID, node.IP, true)

	// Publish node created event
	if u.eventBus != nil {
		u.eventBus.Publish(events.Event{
			Type:      events.EventNodeCreated,
			Timestamp: time.Now(),
			Payload: events.NodeLifecyclePayload{
				NodeID:   node.ID,
				NodeName: node.Name,
				IP:       node.IP,
				Action:   "created",
			},
		})
	}

	return node, nil
}

func (u *nodeUsecase) ListNodes(ctx context.Context) ([]*domain.Node, error) {
	return u.nodeRepo.ListNodes(ctx)
}

func (u *nodeUsecase) GetNode(ctx context.Context, id uint) (*domain.Node, error) {
	return u.nodeRepo.GetNode(ctx, id)
}
func (u *nodeUsecase) DeleteNode(ctx context.Context, id uint, force bool) error {
	log := logger.GetLogger()
	log.WithField("node_id", id).WithField("force", force).Info("[DeleteNode] Request to delete node")

	// Get Node Details (needed for Agent connection)
	node, err := u.nodeRepo.GetNode(ctx, id)
	if err != nil {
		if !force {
			return err
		}
		// force=true: DB cleanup only (agent cleanup impossible without node).
		log.WithError(err).Warn("[DeleteNode] Node not found or fetch error; proceeding with DB cleanup if possible")
	}

	// 1. Check for active children (Inbounds -> Accounts)
	inbounds, err := u.nodeRepo.ListInboundsByNode(ctx, id)
	if err != nil {
		return err
	}

	hasActiveAccounts := false
	for _, inbound := range inbounds {
		accounts, err := u.accountRepo.ListByInboundID(ctx, inbound.ID)
		if err != nil {
			continue
		}
		if len(accounts) > 0 {
			hasActiveAccounts = true
			break
		}
	}

	if (len(inbounds) > 0 || hasActiveAccounts) && !force {
		log.WithField("node_id", id).Warn("[DeleteNode] Node has active children, blocking deletion")
		return ErrNodeHasChildren
	}

	// 2. Agent Cleanup (best effort, runs for all deletions)
	if node != nil {
		log.Info("[DeleteNode] Attempting to uninstall agent on remote node")
		client, err := u.getAgentClient(node)
		if err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to connect to agent for uninstall")
		} else {
			if err := client.Uninstall(ctx); err != nil {
				log.WithError(err).Warn("[DeleteNode] Agent uninstall request failed (remote node might be down)")
			} else {
				log.Info("[DeleteNode] Agent uninstall command sent successfully")
			}
			client.Close()
		}
	}

	// 3. Cascade Delete if Force is true
	if force {
		log.WithField("node_id", id).Info("[DeleteNode] Force deleting node and its children")

		// Cleanup Provisioning Tasks
		if u.provService != nil {
			log.Info("[DeleteNode] Cancelling pending provisioning tasks")
			if err := u.provService.CancelTasksForNode(ctx, id); err != nil {
				log.WithError(err).Warn("[DeleteNode] Failed to clean up provisioning tasks")
			}
		}

		log.WithField("node_id", id).Info("[DeleteNode] Deleting node from database")

		// Delete Accounts first
		for _, inbound := range inbounds {
			accounts, _ := u.accountRepo.ListByInboundID(ctx, inbound.ID)
			for _, account := range accounts {
				if err := u.accountRepo.Delete(ctx, account.ID); err != nil {
					log.WithError(err).Warnf("[DeleteNode] Failed to delete account %d", account.ID)
				}
			}
			// Delete Inbound
			if err := u.nodeRepo.DeleteInbound(ctx, inbound.ID); err != nil {
				log.WithError(err).Warnf("[DeleteNode] Failed to delete inbound %d", inbound.ID)
			}
		}

		// Delete Reverse Proxies (before routing rules since they reference them)
		if err := u.nodeRepo.DeleteReverseProxiesByNode(ctx, id); err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to delete reverse proxies")
		}

		// Delete Outbounds
		if err := u.nodeRepo.DeleteOutboundsByNode(ctx, id); err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to delete outbounds")
		}

		// Delete Routing Rules
		if err := u.nodeRepo.DeleteRoutingRulesByNode(ctx, id); err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to delete routing rules")
		}

		// Delete Balancing Rules
		if err := u.nodeRepo.DeleteBalancingRulesByNode(ctx, id); err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to delete balancing rules")
		}

		// Delete Node Stats
		if err := u.nodeRepo.DeleteNodeStatsByNode(ctx, id); err != nil {
			log.WithError(err).Warn("[DeleteNode] Failed to delete node stats")
		}

	}

	// 3. Delete Node (Soft Delete)
	err = u.nodeRepo.DeleteNode(ctx, id)
	if err != nil {
		log.WithError(err).WithField("node_id", id).Error("[DeleteNode] Failed to delete node")
		return err
	}

	// 4. Evict in-memory caches for this node
	u.cleanupNodeCaches(id)

	// Publish node deleted event
	if u.eventBus != nil && node != nil {
		u.eventBus.Publish(events.Event{
			Type:      events.EventNodeDeleted,
			Timestamp: time.Now(),
			Payload: events.NodeLifecyclePayload{
				NodeID:   id,
				NodeName: node.Name,
				IP:       node.IP,
				Action:   "deleted",
			},
		})
	}

	log.WithField("node_id", id).Info("[DeleteNode] Node deleted successfully")
	return nil
}

// cleanupNodeCaches evicts all in-memory state associated with a deleted node.
func (u *nodeUsecase) cleanupNodeCaches(nodeID uint) {
	u.configHashMu.Lock()
	delete(u.lastPushedConfigHash, nodeID)
	u.configHashMu.Unlock()

	u.pushStateMu.Lock()
	delete(u.pushState, nodeID)
	u.pushStateMu.Unlock()

	if u.statsCache != nil {
		u.statsCache.Invalidate(nodeID)
	}

	if u.heartbeatMgr != nil {
		u.heartbeatMgr.StopNode(nodeID)
	}
}

func (u *nodeUsecase) MigrateNodeAccounts(ctx context.Context, sourceNodeID, targetNodeID, targetInboundID uint) error {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"source_node":    sourceNodeID,
		"target_node":    targetNodeID,
		"target_inbound": targetInboundID,
	}).Info("[MigrateNodeAccounts] Migration requested")

	if sourceNodeID == targetNodeID {
		return errors.New("source and target nodes must be different")
	}

	// Validate Source Node
	sourceNode, err := u.nodeRepo.GetNode(ctx, sourceNodeID)
	if err != nil {
		return err
	}

	// Validate Target Node & Inbound
	targetNode, err := u.nodeRepo.GetNode(ctx, targetNodeID)
	if err != nil {
		return err
	}
	targetInbound, err := u.nodeRepo.GetInbound(ctx, targetInboundID)
	if err != nil {
		return ErrInboundNotFound
	}
	if targetInbound.NodeID != targetNodeID {
		return ErrInvalidTargetNode
	}

	// Get all inbounds of source node
	sourceInbounds, err := u.nodeRepo.ListInboundsByNode(ctx, sourceNodeID)
	if err != nil {
		return err
	}

	migratedCount := 0
	skippedCount := 0
	errorCount := 0
	for _, inbound := range sourceInbounds {
		// Protocol compatibility check
		sourceProtocol := strings.ToLower(inbound.Protocol)
		targetProtocol := strings.ToLower(targetInbound.Protocol)
		if sourceProtocol != targetProtocol {
			log.Warnf("[MigrateNodeAccounts] Skipping inbound %s: protocol mismatch (%s -> %s)",
				inbound.Tag, sourceProtocol, targetProtocol)
			skippedCount++
			continue
		}

		accounts, err := u.accountRepo.ListByInboundID(ctx, inbound.ID)
		if err != nil {
			log.WithError(err).Warnf("[MigrateNodeAccounts] Failed to list accounts for inbound %d", inbound.ID)
			errorCount++
			continue
		}

		for _, account := range accounts {
			if err := u.accountRepo.UpdateInbound(ctx, account.ID, targetInboundID); err != nil {
				log.WithError(err).Errorf("[MigrateNodeAccounts] Failed to migrate account %d", account.ID)
				errorCount++
				continue
			}
			migratedCount++
		}
	}

	log.WithFields(map[string]interface{}{
		"migrated": migratedCount,
		"skipped":  skippedCount,
		"errors":   errorCount,
	}).Infof("[MigrateNodeAccounts] Migration completed to node %d", targetNodeID)

	// Sync Target Node (Add Users)
	if err := u.pushConfigToAgent(ctx, targetNode); err != nil {
		log.Warnf("[MigrateNodeAccounts] Failed to push config to target agent: %v", err)
	}

	// Sync Source Node (Remove Users) - Best effort as it might be dead
	if err := u.pushConfigToAgent(ctx, sourceNode); err != nil {
		log.Warnf("[MigrateNodeAccounts] Failed to push config to source agent: %v", err)
	}

	return nil
}

// deriveCredentialsForInbound returns flow and encryption appropriate for the target inbound's protocol.
func deriveCredentialsForInbound(target *domain.Inbound) (flow, encryption string) {
	protocol := strings.ToLower(target.Protocol)
	switch protocol {
	case "vless":
		settings := target.GetVLESSSettingsOrDefault()
		return settings.Flow, settings.Encryption
	case "vmess":
		return "", "auto"
	case "trojan":
		return "", ""
	default:
		return "", ""
	}
}

func (u *nodeUsecase) MigrateInbound(ctx context.Context, sourceInboundID, targetInboundID uint) (*InboundMigrationResult, error) {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"source_inbound": sourceInboundID,
		"target_inbound": targetInboundID,
	}).Info("[MigrateInbound] Migration requested")

	if sourceInboundID == targetInboundID {
		return nil, errors.New("source and target inbounds must be different")
	}

	// Load source and target inbounds with their nodes
	sourceInbound, err := u.nodeRepo.GetInbound(ctx, sourceInboundID)
	if err != nil {
		return nil, fmt.Errorf("source inbound not found: %w", err)
	}
	targetInbound, err := u.nodeRepo.GetInbound(ctx, targetInboundID)
	if err != nil {
		return nil, fmt.Errorf("target inbound not found: %w", err)
	}

	result := &InboundMigrationResult{}

	// Check protocol compatibility
	sourceProtocol := strings.ToLower(sourceInbound.Protocol)
	targetProtocol := strings.ToLower(targetInbound.Protocol)
	if sourceProtocol != targetProtocol {
		result.ProtocolWarning = fmt.Sprintf(
			"Protocol mismatch: source is %s, target is %s. Flow/encryption settings were adjusted.",
			sourceProtocol, targetProtocol,
		)
		log.Warnf("[MigrateInbound] %s", result.ProtocolWarning)
	}

	// Derive target credentials
	targetFlow, targetEncryption := deriveCredentialsForInbound(targetInbound)

	// Get all accounts on the source inbound
	accounts, err := u.accountRepo.ListByInboundID(ctx, sourceInboundID)
	if err != nil {
		return nil, fmt.Errorf("failed to list source accounts: %w", err)
	}

	// Run DB mutations in a transaction (steps 2-4)
	if err := u.tm.Do(ctx, func(txCtx context.Context) error {
		for _, account := range accounts {
			// Check for duplicate on target
			existing, _ := u.accountRepo.FindByEmailAndInbound(txCtx, account.Email, targetInboundID)
			if existing != nil {
				// Duplicate — delete source account
				if err := u.accountRepo.Delete(txCtx, account.ID); err != nil {
					log.WithError(err).Warnf("[MigrateInbound] Failed to delete duplicate account %d", account.ID)
					result.FailedAccounts++
					continue
				}
				result.SkippedAccounts++
				continue
			}

			// Update account in-place
			if sourceProtocol != targetProtocol {
				if err := u.accountRepo.UpdateInboundAndCredentials(txCtx, account.ID, targetInboundID, targetFlow, targetEncryption); err != nil {
					log.WithError(err).Errorf("[MigrateInbound] Failed to migrate account %d", account.ID)
					result.FailedAccounts++
					continue
				}
			} else {
				if err := u.accountRepo.UpdateInbound(txCtx, account.ID, targetInboundID); err != nil {
					log.WithError(err).Errorf("[MigrateInbound] Failed to migrate account %d", account.ID)
					result.FailedAccounts++
					continue
				}
			}
			result.MigratedAccounts++
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// Step 5: Conditionally deactivate source inbound (outside transaction)
	// nodeRepo.UpdateInbound uses r.db.WithContext (not GetExecutor), so it
	// cannot participate in the transaction. This is safe: if deactivation
	// fails after a successful migration, the admin can manually disable it.
	if result.FailedAccounts == 0 {
		sourceInbound.IsDisabled = true
		if err := u.nodeRepo.UpdateInbound(ctx, sourceInbound); err != nil {
			log.WithError(err).Warn("[MigrateInbound] Failed to deactivate source inbound")
		} else {
			result.SourceDeactivated = true
		}
	}

	// Step 6: Push config to target node (outside transaction)
	targetNode, err := u.nodeRepo.GetNode(ctx, targetInbound.NodeID)
	if err == nil {
		if pushErr := u.pushConfigToAgent(ctx, targetNode); pushErr != nil {
			log.Warnf("[MigrateInbound] Failed to push config to target node %d: %v", targetNode.ID, pushErr)
		}
	}

	log.WithFields(map[string]interface{}{
		"migrated":    result.MigratedAccounts,
		"skipped":     result.SkippedAccounts,
		"failed":      result.FailedAccounts,
		"deactivated": result.SourceDeactivated,
	}).Info("[MigrateInbound] Migration completed")

	return result, nil
}

func (u *nodeUsecase) UpdateNode(ctx context.Context, node *domain.Node) error {
	// Get existing node to check for IP change
	existing, err := u.nodeRepo.GetNode(ctx, node.ID)
	if err != nil {
		return err
	}

	// Detect if we need to lookup
	shouldLookupLocation := node.IP != "" && (node.IP != existing.IP || node.CountryCode == "" || node.Datacenter == "")
	ipChanged := node.IP != existing.IP
	portChanged := existing.AgentPort != node.AgentPort
	accessLogChanged := existing.EnableAccessLog != node.EnableAccessLog

	// Update DB first
	if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
		return err
	}

	// rebuild the heartbeat session so it doesn't keep using stale node details.
	if (ipChanged || portChanged) && u.heartbeatMgr != nil {
		u.heartbeatMgr.StopNode(node.ID)
		u.heartbeatMgr.TriggerSync()
	}
	if u.statsCache != nil {
		u.statsCache.Invalidate(node.ID)
	}

	// If API Port changed and using agent, update agent config
	if existing.APIPort != node.APIPort {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			client, err := u.getAgentClient(node)
			if err != nil {
				logger.GetLogger().Warnf("[UpdateNode] Failed to connect to agent to update API port: %v", err)
				return
			}
			defer client.Close()

			// Assuming Xray listens on localhost relative to the agent
			newAPIAddr := fmt.Sprintf("127.0.0.1:%d", node.APIPort)
			if err := client.UpdateXrayAPIConfig(ctx, newAPIAddr); err != nil {
				logger.GetLogger().Warnf("[UpdateNode] Failed to update Xray API config on agent: %v", err)
			} else {
				logger.GetLogger().Infof("[UpdateNode] Updated Xray API config on agent for node %d to %s", node.ID, newAPIAddr)
			}

			// Push full config to Xray (this will update Xray's listening port and restart it)
			// We close the existing client first since pushConfigToAgent creates its own
			client.Close()

			if err := u.pushConfigToAgent(ctx, node); err != nil {
				logger.GetLogger().Warnf("[UpdateNode] Failed to push config to agent after port update: %v", err)
			}
		}()
	}

	// If access log capture changed, push updated xray config to agent
	if accessLogChanged && node.IsOnline {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := u.pushConfigToAgent(ctx, node); err != nil {
				logger.GetLogger().Warnf("[UpdateNode] Failed to push config after access log change: %v", err)
			} else {
				logger.GetLogger().Infof("[UpdateNode] Pushed config to node %d after access log change (enabled=%v)", node.ID, node.EnableAccessLog)
			}
		}()
	}

	// Trigger background tasks
	if shouldLookupLocation || ipChanged {
		go u.triggerBackgroundNodeTasks(node.ID, node.IP, shouldLookupLocation)
	}

	return nil
}

func (u *nodeUsecase) UpdateNodeDNSSettings(ctx context.Context, nodeID uint, settings *domain.DNSSettings) error {
	if err := ValidateDNSSettings(settings); err != nil {
		return err
	}

	if err := u.nodeRepo.UpdateNodeDNSSettings(ctx, nodeID, settings); err != nil {
		return err
	}

	// Fetch fresh node for config push
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	u.tryScheduleConfigPush(node)
	return nil
}

func (u *nodeUsecase) ClearNodeDNSSettings(ctx context.Context, nodeID uint) error {
	return u.UpdateNodeDNSSettings(ctx, nodeID, nil)
}

func (u *nodeUsecase) UpdateNodeFakeDNSSettings(ctx context.Context, nodeID uint, pools []domain.FakeDNSPool) error {
	if err := ValidateFakeDNSPools(pools); err != nil {
		return err
	}
	if err := u.nodeRepo.UpdateNodeFakeDNSSettings(ctx, nodeID, pools); err != nil {
		return err
	}
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	u.tryScheduleConfigPush(node)
	return nil
}

// triggerBackgroundNodeTasks performs GeoIP lookup and initial health check
func (u *nodeUsecase) triggerBackgroundNodeTasks(nodeID uint, ip string, lookupLocation bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. GeoIP Lookup if needed
	if lookupLocation && ip != "" {
		loc, err := geoip.Lookup(ip)
		if err == nil {
			// Fetch fresh node to avoid overwriting other changes
			node, err := u.nodeRepo.GetNode(ctx, nodeID)
			if err == nil {
				updated := false
				if node.CountryCode == "" && loc.CountryCode != "" {
					node.CountryCode = loc.CountryCode
					updated = true
				}
				if node.Datacenter == "" && loc.ISP != "" {
					node.Datacenter = loc.ISP
					if loc.Org != "" && loc.Org != loc.ISP {
						node.Datacenter = fmt.Sprintf("%s (%s)", loc.ISP, loc.Org)
					}
					updated = true
				}
				if updated {
					if updateErr := u.nodeRepo.UpdateNode(ctx, node); updateErr != nil {
						logger.GetLogger().WithError(updateErr).WithField("node_id", nodeID).Warn("[BackgroundTask] Failed to update node with GeoIP data")
					} else {
						logger.GetLogger().Infof("[BackgroundTask] Updated GeoIP for node %d", nodeID)
					}
				}
			}
		} else {
			logger.GetLogger().WithError(err).WithField("node_id", nodeID).Debug("[BackgroundTask] GeoIP lookup failed")
		}
	}

	// kick off a health check so the node turns green right away
	if _, healthErr := u.CheckNodeHealth(ctx, nodeID); healthErr != nil {
		logger.GetLogger().WithError(healthErr).WithField("node_id", nodeID).Debug("[BackgroundTask] Initial health check failed")
	}
}

// BackfillNodeUUIDs generates UUIDs for any existing nodes that have an empty
// UUID. Rows created before the column existed carry "", and the heartbeat's
// identity check skips a node whose UUID is empty — so an un-backfilled node
// never notices an agent that isn't its own. Called once at startup after
// AutoMigrate.
func (u *nodeUsecase) BackfillNodeUUIDs(ctx context.Context) error {
	log := logger.GetLogger()
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return err
	}
	backfilled := 0
	for _, node := range nodes {
		if node.UUID == "" {
			node.UUID = uuid.New().String()
			if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
				log.Warnf("[BackfillNodeUUIDs] Failed to set UUID for node %d: %v", node.ID, err)
			} else {
				backfilled++
			}
		}
	}
	if backfilled > 0 {
		log.Infof("[BackfillNodeUUIDs] Generated UUIDs for %d existing nodes", backfilled)
	}
	return nil
}

// CheckNodeHealth checks if a node is reachable (Agent only)
func (u *nodeUsecase) CheckNodeHealth(ctx context.Context, nodeID uint) (*domain.NodeHealth, error) {
	return u.CheckAgentHealth(ctx, nodeID)
}
