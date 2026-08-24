package usecase

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// WGRenderPeer is a managed WireGuard peer rendered into a WG inbound's config.
type WGRenderPeer struct {
	PublicKey      string
	PresharedKey   string
	AllowedIP      string // bare IP, rendered as /32
	SubscriptionID uint   // for traffic attribution
	PeerID         uint   // wg_peers.id, for per-device usage writes
	InboundID      uint   // for crediting the per-server account
}

// WGPeerSource supplies active managed peers for a WG inbound at build time.
// Set via SetWGPeerSource; nil when WG selling is off.
type WGPeerSource interface {
	ActivePeersByInbound(ctx context.Context, inboundID uint) ([]WGRenderPeer, error)
	AddPeerUsage(ctx context.Context, peerID uint, up, down int64) error
	TouchPeerLastSeen(ctx context.Context, peerID uint, t time.Time) error
}

// SetWGPeerSource injects the managed WireGuard peer source.
func (u *nodeUsecase) SetWGPeerSource(s WGPeerSource) { u.wgPeerSource = s }

// SetRouterMode mirrors cfg.Router.Enabled into generated xray configs
func (u *nodeUsecase) SetRouterMode(enabled bool) { u.routerMode = enabled }

func (u *nodeUsecase) SetRouterWANSource(fn func(context.Context) []xray.RouterWAN) {
	u.routerWANs = fn
}

// currentRouterWANs is nil-safe: unwired means no per-WAN outbounds, never a panic.
func (u *nodeUsecase) currentRouterWANs(ctx context.Context) []xray.RouterWAN {
	if u.routerWANs == nil || !u.routerMode {
		return nil
	}
	return u.routerWANs(ctx)
}

// ingressUplinkIfName is the uplink terminating client connections
func (u *nodeUsecase) ingressUplinkIfName() string {
	if u.ingressUplinkFn == nil {
		return ""
	}
	return u.ingressUplinkFn()
}

// SetIngressUplinkSource wires the router-mode uplink lookup.
func (u *nodeUsecase) SetIngressUplinkSource(fn func() string) { u.ingressUplinkFn = fn }

// mergeWGRenderPeers unions admin static peers with managed device peers, deduped by pubkey.
func mergeWGRenderPeers(static []domain.WireGuardPeer, managed []WGRenderPeer) []domain.WireGuardPeer {
	seen := make(map[string]bool, len(static)+len(managed))
	out := make([]domain.WireGuardPeer, 0, len(static)+len(managed))
	for _, p := range static {
		if p.PublicKey == "" || seen[p.PublicKey] {
			continue
		}
		seen[p.PublicKey] = true
		out = append(out, p)
	}
	for _, m := range managed {
		if m.PublicKey == "" || seen[m.PublicKey] {
			continue
		}
		seen[m.PublicKey] = true
		out = append(out, domain.WireGuardPeer{
			PublicKey:    m.PublicKey,
			PreSharedKey: m.PresharedKey,
			AllowedIPs:   []string{m.AllowedIP + "/32"},
		})
	}
	return out
}

// getAgentClient returns an in process client for the local agent
func (u *nodeUsecase) getAgentClient(node *domain.Node) (agent.NodeClient, error) {
	if node == nil {
		return nil, nil
	}
	if u.embeddedSrv == nil {
		return nil, fmt.Errorf("embedded node server not initialized")
	}
	return agent.NewEmbeddedClient(u.embeddedSrv), nil
}

// getAgentClientUnpooled single bin mode bypasses the grpc pool
func (u *nodeUsecase) getAgentClientUnpooled(node *domain.Node) (agent.NodeClient, error) {
	return u.getAgentClient(node)
}

// GetNodeClient looks up the node and returns its in process agent client
func (u *nodeUsecase) GetNodeClient(ctx context.Context, nodeID uint) (agent.NodeClient, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %d not found: %w", nodeID, err)
	}
	return u.getAgentClient(node)
}

// closeAgentClient safely closes an agent client
func closeAgentClient(client agent.NodeClient) {
	if client != nil {
		_ = client.Close()
	}
}

// GetNodeSystemStats retrieves system stats from a node's agent
// Returns empty stats if the node doesn't use an agent
func (u *nodeUsecase) GetNodeSystemStats(ctx context.Context, nodeID uint) (*agent.SystemStats, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	return client.GetSystemStats(ctx)
}

func (u *nodeUsecase) CheckAgentHealth(ctx context.Context, id uint) (*domain.NodeHealth, error) {
	node, err := u.nodeRepo.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		logger.GetLogger().Warnf("[CheckAgentHealth] Connection failed for node %d: %v", id, err)
		if updateErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, false, time.Now()); updateErr != nil {
			logger.GetLogger().Errorf("[CheckAgentHealth] Failed to update status DB: %v", updateErr)
		}
		return &domain.NodeHealth{
			Healthy: false,
			Message: fmt.Sprintf("Agent connection failed: %v", err),
			Latency: 0,
		}, nil
	}
	defer closeAgentClient(client)

	// Ping the agent
	latency, err := client.Ping(ctx)
	if err != nil {
		logger.GetLogger().Warnf("[CheckAgentHealth] Ping failed for node %d: %v", id, err)
		if statusErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, false, time.Now()); statusErr != nil {
			logger.GetLogger().WithError(statusErr).WithField("node_id", node.ID).Warn("[CheckAgentHealth] Failed to update node status after ping failure")
		}
		return &domain.NodeHealth{
			Healthy: false,
			Message: fmt.Sprintf("Agent ping failed: %v", err),
			Latency: 0,
		}, nil
	}

	// Get agent health check
	health, err := client.HealthCheck(ctx)
	if err != nil {
		logger.GetLogger().Warnf("[CheckAgentHealth] HealthCheck failed for node %d: %v", id, err)
		if statusErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, false, time.Now()); statusErr != nil {
			logger.GetLogger().WithError(statusErr).WithField("node_id", node.ID).Warn("[CheckAgentHealth] Failed to update node status after health check failure")
		}
		return &domain.NodeHealth{
			Healthy: false,
			Message: fmt.Sprintf("Health check failed: %v", err),
			Latency: latency,
		}, nil
	}

	// Agent reachable → node Online regardless of xray status.
	isHealthy := true

	message := health.Message
	if health.Status != agent.HealthHealthy && health.Status != agent.HealthDegraded {
		message = fmt.Sprintf("Agent Online (%s)", health.Message)
	}

	logger.GetLogger().Infof("[CheckAgentHealth] Node %d health: true", id)
	if updateErr := u.nodeRepo.UpdateNodeStatus(ctx, node.ID, isHealthy, time.Now()); updateErr != nil {
		logger.GetLogger().Errorf("[CheckAgentHealth] Failed to update status DB: %v", updateErr)
	}

	return &domain.NodeHealth{
		Healthy: isHealthy, // Always true if reachable
		Message: message,
		Latency: latency,
	}, nil
}

// AddUserViaAgent adds a user to a node using the agent
func (u *nodeUsecase) AddUserViaAgent(ctx context.Context, node *domain.Node, inboundTag, email, uuid, protocol, flow, encryption string) error {
	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("agent client is nil")
	}
	defer closeAgentClient(client)

	return client.AddUser(ctx, inboundTag, email, uuid, protocol, flow, encryption, 0)
}

// RemoveUserViaAgent removes a user from a node using the agent
func (u *nodeUsecase) RemoveUserViaAgent(ctx context.Context, node *domain.Node, inboundTag, email string) error {
	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("agent client is nil")
	}
	defer closeAgentClient(client)

	return client.RemoveUser(ctx, inboundTag, email)
}

// PushConfigViaAgent generates and pushes a full config to the agent.
// Delegates to pushConfigToAgent so all callers go through one path
// that filters disabled items, injects managed certs, and populates users.
func (u *nodeUsecase) PushConfigViaAgent(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	return u.pushConfigToAgent(ctx, node)
}

// PushFullConfig: re-fetches the node (fresh RoutingSettings etc.),
// builds the full xray config + certs, pushes to agent.
func (u *nodeUsecase) PushFullConfig(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	err = u.pushConfigToAgent(ctx, node)
	// Stats (especially XrayStatus / uptime / config hash) change on
	// every successful push; dropping the cached entry avoids serving
	// "still running old config" readings for the next TTL window.
	if err == nil && u.statsCache != nil {
		u.statsCache.Invalidate(nodeID)
	}
	return err
}

// GetNodeWithSystemStats retrieves a node and populates its system stats from the agent
func (u *nodeUsecase) GetNodeWithSystemStats(ctx context.Context, id uint) (*domain.Node, error) {
	node, err := u.nodeRepo.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		// Return node without stats if agent is unreachable
		return node, nil
	}
	defer closeAgentClient(client)

	// Get system stats
	stats, err := client.GetSystemStats(ctx)
	if err == nil {
		node.CPUUsage = stats.CPUUsagePercent
		node.MemoryUsed = stats.MemoryUsedBytes
		node.MemoryTotal = stats.MemoryTotalBytes
		node.MemoryPercent = stats.MemoryUsagePercent
		node.DiskUsed = stats.DiskUsedBytes
		node.DiskTotal = stats.DiskTotalBytes
		node.DiskPercent = stats.DiskUsagePercent
	}

	// Get status for uptime
	status, err := client.GetStatus(ctx)
	if err == nil {
		node.XrayUptime = status.UptimeSeconds
	}

	// Get version info
	version, err := client.GetVersion(ctx)
	if err == nil {
		node.XrayVersion = version.XrayVersion
		node.AgentVersion = version.AgentVersion
	}

	return node, nil
}

// ListNodesWithSystemStats retrieves all nodes and populates system stats for agent-enabled nodes
func (u *nodeUsecase) ListNodesWithSystemStats(ctx context.Context) ([]*domain.Node, error) {
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Populate system stats for all nodes
	for _, node := range nodes {
		client, err := u.getAgentClient(node)
		if err != nil {
			continue // Skip if agent is unreachable
		}

		// Get quick stats (without 1s CPU sample delay)
		stats, err := client.GetSystemStats(ctx)
		if err == nil {
			node.CPUUsage = stats.CPUUsagePercent
			node.MemoryUsed = stats.MemoryUsedBytes
			node.MemoryTotal = stats.MemoryTotalBytes
			node.MemoryPercent = stats.MemoryUsagePercent
		}

		closeAgentClient(client)
	}

	return nodes, nil
}

// sniCertBasename returns a fingerprinted basename so a renewed cert produces a
// new filename. That changes the built config hash, so the push-skip guard
// re-uploads the file instead of treating the renewal as a no-op.
func sniCertBasename(sniID uint, pem []byte) string {
	sum := sha256.Sum256(pem)
	return fmt.Sprintf("sni-%d-%s", sniID, hex.EncodeToString(sum[:])[:12])
}

// resolveSNICertContent fetches the certificate and key content for an SNI domain by ID.
// If the SNI uses path mode, the files are read from the master filesystem.
func (u *nodeUsecase) resolveSNICertContent(ctx context.Context, sniID uint) (certContent []byte, keyContent []byte, err error) {
	sni, err := u.sniUsecase.GetByID(ctx, sniID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch SNI ID=%d: %w", sniID, err)
	}
	if sni == nil {
		return nil, nil, fmt.Errorf("SNI ID=%d not found", sniID)
	}

	if sni.UsePathMode {
		certContent, err = os.ReadFile(sni.CertPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read SNI cert file %s: %w", sni.CertPath, err)
		}
		keyContent, err = os.ReadFile(sni.KeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read SNI key file %s: %w", sni.KeyPath, err)
		}
	} else {
		certContent = []byte(sni.Certificate)
		keyContent = []byte(sni.PrivateKey)
	}

	return certContent, keyContent, nil
}

// RepushForSNI re-applies config to every node serving the given SNI cert.
// Called (detached, off the request path) after a renewal or edit so the new
// material actually lands instead of waiting for an unrelated change.
func (u *nodeUsecase) RepushForSNI(ctx context.Context, sniID uint) {
	log := logger.GetLogger()
	nodeIDs, err := u.sniUsecase.ListNodeIDs(ctx, sniID)
	if err != nil {
		log.WithError(err).Warn("[RepushForSNI] could not list nodes for SNI")
		return
	}
	for _, id := range nodeIDs {
		if err := u.PushConfigViaAgent(ctx, id); err != nil {
			log.WithError(err).Warnf("[RepushForSNI] re-push failed for node %d", id)
		}
	}
}

// pushConfigToAgent builds and pushes Xray config to the agent (no direct Xray API call).
func (u *nodeUsecase) pushConfigToAgent(ctx context.Context, node *domain.Node) error {
	log := logger.GetLogger()

	// Mark push in progress so drift detection (tryScheduleConfigPush) doesn't
	// fire a concurrent push while we're already pushing.
	state := u.getOrCreatePushState(node.ID)
	state.mu.Lock()
	state.inProgress = true
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.inProgress = false
		state.mu.Unlock()
	}()

	// Connect to agent early to allow file uploads.
	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	// clientOwned tracks whether the background goroutine took ownership of closing
	// the client. If we return early (error), the deferred close handles it.
	clientOwned := false
	defer func() {
		if !clientOwned {
			closeAgentClient(client)
		}
	}()

	// Fetch node config data in parallel: inbounds, outbounds, routing rules, subscriptions
	var (
		inbounds       []*domain.Inbound
		outbounds      []*domain.Outbound
		routingRules   []*domain.RoutingRule
		reverseProxies []*domain.ReverseProxy
		balancingRules []*domain.BalancingRule
		dbErrors       [5]error
	)
	var dbWg sync.WaitGroup
	dbWg.Add(5)
	go func() {
		defer dbWg.Done()
		inbounds, dbErrors[0] = u.nodeRepo.ListInboundsByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		outbounds, dbErrors[1] = u.nodeRepo.ListOutboundsByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		routingRules, dbErrors[2] = u.nodeRepo.ListRoutingRulesByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		reverseProxies, dbErrors[3] = u.nodeRepo.ListReverseProxiesByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		balancingRules, dbErrors[4] = u.nodeRepo.ListBalancingRulesByNode(ctx, node.ID)
	}()
	dbWg.Wait()
	if dbErrors[0] != nil {
		return fmt.Errorf("failed to list inbounds: %w", dbErrors[0])
	}
	if dbErrors[1] != nil {
		return fmt.Errorf("failed to list outbounds: %w", dbErrors[1])
	}
	if dbErrors[2] != nil {
		return fmt.Errorf("failed to list routing rules: %w", dbErrors[2])
	}
	if dbErrors[3] != nil {
		return fmt.Errorf("failed to list reverse proxies: %w", dbErrors[3])
	}
	if dbErrors[4] != nil {
		return fmt.Errorf("failed to list balancing rules: %w", dbErrors[4])
	}

	// Filter disabled inbounds — excluded from Xray config, certificate injection, and user mapping
	enabledInbounds := make([]*domain.Inbound, 0, len(inbounds))
	for _, in := range inbounds {
		if !in.IsDisabled {
			enabledInbounds = append(enabledInbounds, in)
		}
	}
	inbounds = enabledInbounds

	// Merge managed WireGuard device peers into each WG inbound's settings.
	// wg_peers is the source of truth; this is in-memory only, never persisted.
	if u.wgPeerSource != nil {
		for _, in := range inbounds {
			if !strings.EqualFold(in.Protocol, "wireguard") {
				continue
			}
			managed, err := u.wgPeerSource.ActivePeersByInbound(ctx, in.ID)
			if err != nil {
				log.WithError(err).Warnf("[pushConfigToAgent] WG peer fetch failed for inbound %s; rendering static peers only", in.Tag)
				continue
			}
			wg := in.GetWireGuardSettingsOrDefault()
			wg.Peers = mergeWGRenderPeers(wg.Peers, managed)
			in.WireGuardSettings = wg
		}
	}

	// Filter disabled outbounds — excluded from Xray config
	enabledOutbounds := make([]*domain.Outbound, 0, len(outbounds))
	for _, out := range outbounds {
		if !out.IsDisabled {
			enabledOutbounds = append(enabledOutbounds, out)
		}
	}
	outbounds = enabledOutbounds

	// Populate users from the accounts table — the single source of truth.
	// Active accounts determine which users exist on each inbound.
	// Admin-excluded accounts (status=disabled) are naturally filtered out.
	usersMap := make(map[string][]*xray.User)
	addedUUIDs := make(map[string]map[string]bool) // tag -> uuid -> true (prevent duplicates)

	if u.accountRepo != nil {
		allAccounts, err := u.accountRepo.ListByNodeID(ctx, node.ID)
		if err != nil {
			return fmt.Errorf("failed to list accounts for node %d: %w", node.ID, err)
		}
		for _, acc := range allAccounts {
			if acc.Status != "active" || acc.Inbound == nil {
				continue
			}
			if acc.Inbound.IsDisabled {
				continue
			}
			tag := acc.Inbound.Tag

			// Deduplicate by UUID within the same inbound
			if addedUUIDs[tag] == nil {
				addedUUIDs[tag] = make(map[string]bool)
			}
			if addedUUIDs[tag][acc.UUID] {
				log.Warnf("[pushConfigToAgent] Skipping duplicate UUID %s (email=%s) on inbound %s", acc.UUID, acc.Email, tag)
				continue
			}

			var accLevel uint32
			if acc.Subscription != nil {
				accLevel = bandwidth.GetTier(acc.Subscription.GetEffectiveBandwidthLimit()).Level
			}

			usersMap[tag] = append(usersMap[tag], &xray.User{
				Email:      acc.Email,
				UUID:       acc.UUID,
				Level:      accLevel,
				Protocol:   xray.Protocol(acc.Inbound.Protocol),
				Flow:       acc.Flow,
				Encryption: acc.Encryption,
				AlterId:    0,
			})

			addedUUIDs[tag][acc.UUID] = true
		}
	}

	// Inject managed certificates for inbounds
	for i := range inbounds {
		in := inbounds[i]
		if in.Security == "tls" {
			tlsSettings := in.GetTLSSettingsOrDefault()
			for j := range tlsSettings.Certificates {
				cert := &tlsSettings.Certificates[j]
				if cert.ID > 0 {
					fetchedCert, err := u.certUC.GetCertificate(ctx, cert.ID)
					if err == nil && fetchedCert != nil {
						log.Infof("[pushConfigToAgent] Inbound %s: Injected managed cert ID=%d", in.Tag, cert.ID)

						// Write the cert+key to disk and point xray at the files.
						certPath := filepath.Join("certs", fmt.Sprintf("%d.crt", cert.ID))
						keyPath := filepath.Join("certs", fmt.Sprintf("%d.key", cert.ID))

						certAbsPath, err := client.WriteFile(ctx, certPath, fetchedCert.Certificate, 0644)
						if err != nil {
							log.Warnf("[pushConfigToAgent] Failed to write cert file %s: %v. Falling back to inline.", certPath, err)
							cert.CertificateFile = string(fetchedCert.Certificate)
							cert.KeyFile = string(fetchedCert.PrivateKey)
						} else {
							keyAbsPath, err := client.WriteFile(ctx, keyPath, fetchedCert.PrivateKey, 0600)
							if err != nil {
								log.Warnf("[pushConfigToAgent] Failed to write key file %s: %v. Falling back to inline.", keyPath, err)
								cert.CertificateFile = string(fetchedCert.Certificate)
								cert.KeyFile = string(fetchedCert.PrivateKey)
							} else {
								cert.CertificateFile = certAbsPath
								cert.KeyFile = keyAbsPath
							}
						}
					} else {
						// A TLS inbound with no usable cert would break Xray for the
						// whole node; fail the push and keep the last-good config.
						return fmt.Errorf("inbound %s: managed cert %d unavailable: %v", in.Tag, cert.ID, err)
					}
				} else if cert.SNIId > 0 {
					certPEM, keyPEM, sniErr := u.resolveSNICertContent(ctx, cert.SNIId)
					if sniErr == nil {
						log.Infof("[pushConfigToAgent] Inbound %s: Injected SNI cert ID=%d", in.Tag, cert.SNIId)

						base := sniCertBasename(cert.SNIId, certPEM)
						sniCertPath := filepath.Join("certs", base+".crt")
						sniKeyPath := filepath.Join("certs", base+".key")

						certAbsPath, uploadErr := client.WriteFile(ctx, sniCertPath, certPEM, 0644)
						if uploadErr != nil {
							log.Warnf("[pushConfigToAgent] Failed to write SNI cert file %s: %v. Falling back to inline.", sniCertPath, uploadErr)
							cert.CertificateFile = string(certPEM)
							cert.KeyFile = string(keyPEM)
						} else {
							keyAbsPath, uploadErr := client.WriteFile(ctx, sniKeyPath, keyPEM, 0600)
							if uploadErr != nil {
								log.Warnf("[pushConfigToAgent] Failed to write SNI key file %s: %v. Falling back to inline.", sniKeyPath, uploadErr)
								cert.CertificateFile = string(certPEM)
								cert.KeyFile = string(keyPEM)
							} else {
								cert.CertificateFile = certAbsPath
								cert.KeyFile = keyAbsPath
							}
						}
					} else {
						return fmt.Errorf("inbound %s: resolve SNI cert %d: %w", in.Tag, cert.SNIId, sniErr)
					}
				}
			}
			// Write back to the inbound struct so the builder sees the change.
		}
	}

	// Determine API port
	apiPort := node.APIPort
	if apiPort == 0 {
		apiPort = 10085
	}

	// Build full Xray config
	configBuilder := xray.NewFullConfigBuilder(node).
		WithRouterMode(u.routerMode).
		WithRouterWANs(u.currentRouterWANs(ctx)).
		WithInbounds(inbounds).
		WithOutbounds(outbounds).
		WithRoutingRules(routingRules).
		WithBalancingRules(balancingRules).
		WithReverseProxies(reverseProxies).
		WithUsers(usersMap).
		WithAPI(true, apiPort)

	configJSON, err := configBuilder.Build()
	if err != nil {
		return fmt.Errorf("failed to build xray config: %w", err)
	}

	// Skip push if config hasn't changed (avoids unnecessary Xray restart).
	// Also skip when the user explicitly stopped Xray and the config is
	// unchanged — prevents scheduled tasks from auto-restarting a stopped node.
	newHash := md5.Sum([]byte(configJSON))
	newHashStr := hex.EncodeToString(newHash[:])
	if status, err := client.GetStatus(ctx); err == nil && status.ConfigHash != "" {
		if status.ConfigHash == newHashStr {
			// Re-fetch node to get the freshest XrayStopped value
			freshNode, _ := u.nodeRepo.GetNode(ctx, node.ID)
			xrayStopped := freshNode != nil && freshNode.XrayStopped

			if status.XrayRunning || xrayStopped {
				log.WithFields(map[string]interface{}{
					"node_id":      node.ID,
					"node_name":    node.Name,
					"hash":         newHashStr,
					"xray_running": status.XrayRunning,
					"xray_stopped": xrayStopped,
				}).Info("[pushConfigToAgent] Config unchanged, skipping push")
				// Cache the hash so drift detection knows the current state
				u.configHashMu.Lock()
				u.lastPushedConfigHash[node.ID] = newHashStr
				u.configHashMu.Unlock()
				u.resetPushState(node.ID)
				return nil
			}
		}
	}

	log.WithFields(map[string]interface{}{
		"node_id":      node.ID,
		"node_name":    node.Name,
		"inbounds":     len(inbounds),
		"outbounds":    len(outbounds),
		"routing":      len(routingRules),
		"config_bytes": len(configJSON),
	}).Info("[pushConfigToAgent] Pushing config to agent")

	// DEBUG: Log the actual config for troubleshooting
	log.WithField("config", configJSON).Debug("[pushConfigToAgent] Config JSON")

	// Update Agent's Xray API Port to match what we are pushing
	apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)
	if err := client.UpdateXrayAPIConfig(ctx, apiAddr); err != nil {
		log.WithError(err).Warn("[pushConfigToAgent] Failed to update agent xray api config (continuing)")
	}

	// Push config and restart
	if err := client.PushConfigAndRestart(ctx, configJSON, true); err != nil {
		return fmt.Errorf("failed to push config to agent: %w", err)
	}

	log.WithField("node_id", node.ID).Info("[pushConfigToAgent] Config pushed, verifying xray health...")

	// Adaptive readiness check: poll with increasing intervals instead of a
	// fixed 1s sleep. Xray typically starts or crashes within 200-500ms, so
	// most successful pushes return on the first or second poll.
	var lastHealth *agent.HealthResult
	delays := []time.Duration{200 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond}
	for _, d := range delays {
		time.Sleep(d)
		h, err := client.HealthCheck(ctx)
		if err == nil && h.Status != agent.HealthUnhealthy {
			log.WithField("node_id", node.ID).Info("[pushConfigToAgent] Config applied and xray healthy")
			lastHealth = h
			break
		}
		lastHealth = h
	}
	if lastHealth == nil || lastHealth.Status == agent.HealthUnhealthy {
		return fmt.Errorf("xray failed to start after config push (port may be in use)")
	}

	// Setup or teardown TC bandwidth shaping based on node settings
	bwSettings := node.GetBandwidthSettingsOrDefault()
	if bwSettings.Enabled {
		iface, warning := node.ResolveShapingInterface(u.ingressUplinkIfName())
		if warning != "" {
			log.WithField("node", node.ID).Warn(warning)
		}
		if iface == "" {
			iface = "eth0"
		}
		totalBW := bwSettings.TotalBW
		if totalBW <= 0 {
			totalBW = 1000
		}
		if err := client.SetupBandwidth(ctx, iface, totalBW); err != nil {
			log.WithError(err).Warn("[pushConfigToAgent] Failed to setup TC bandwidth shaping (non-fatal)")
		} else {
			log.WithFields(map[string]interface{}{
				"interface": iface,
				"total_bw":  totalBW,
			}).Info("[pushConfigToAgent] TC bandwidth shaping configured")
		}
	} else {
		// Teardown any existing bandwidth shaping if disabled
		if err := client.TeardownBandwidth(ctx); err != nil {
			log.WithError(err).Debug("[pushConfigToAgent] Teardown bandwidth (non-fatal, may not have been set up)")
		}
	}

	// cache the hash in a goroutine so we don't block the caller
	clientOwned = true
	nodeID := node.ID
	go func() {
		defer closeAgentClient(client)
		hashCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, err := client.GetStatus(hashCtx)
		if err == nil && status.ConfigHash != "" {
			u.configHashMu.Lock()
			u.lastPushedConfigHash[nodeID] = status.ConfigHash
			u.configHashMu.Unlock()
		}
	}()

	// Reset push backoff state so drift detection resumes normally
	u.resetPushState(node.ID)

	// Config changed and Xray was restarted — clear the user-stopped flag
	// so subsequent pushes don't think the user wants Xray stopped.
	if node.XrayStopped {
		node.XrayStopped = false
		if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
			log.WithError(err).WithField("node_id", node.ID).Warn("[pushConfigToAgent] Failed to clear XrayStopped flag")
		}
	}

	return nil
}

// UpdateAgentBinary pushes a new agent binary to a node
func (u *nodeUsecase) UpdateAgentBinary(ctx context.Context, nodeID uint, binaryContent []byte, checksum, version string, signature []byte) error {
	log := logger.GetLogger()

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer closeAgentClient(client)

	log.WithFields(map[string]interface{}{
		"node_id":      nodeID,
		"version":      version,
		"binary_bytes": len(binaryContent),
	}).Info("[UpdateAgentBinary] Pushing binary update")

	result, err := client.SelfUpdate(ctx, binaryContent, checksum, version, true, signature, false)
	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("agent update failed: %s", result.Message)
	}

	log.WithFields(map[string]interface{}{
		"node_id":     nodeID,
		"old_version": result.OldVersion,
		"new_version": result.NewVersion,
	}).Info("[UpdateAgentBinary] Update successful")

	return nil
}

// SetXrayDeps injects optional xray binary distribution dependencies.
func (u *nodeUsecase) SetXrayDeps(bm interface {
	GetChecksum(version, arch string) (string, error)
	EnsureCached(version, arch string) error
}, tm interface {
	GenerateDeploymentToken(nodeID uint, duration time.Duration) (string, error)
}, baseURL string) {
	u.xrayBM = bm
	u.tokenManager = tm
	u.baseURL = baseURL
}

// SetHTTPClientFactory injects the outbound-proxy-aware HTTP factory used for
// hub-side fetches (geofiles, etc). nil leaves consumers using direct
// http.DefaultClient.
func (u *nodeUsecase) SetHTTPClientFactory(f *httpclient.Factory) {
	u.httpFactory = f
}

// normalizeArch maps host-reported architecture strings to the canonical
// values used in xray binary cache paths ("amd64" or "arm64").
// Unknown / empty input falls back to "amd64" to preserve historical behavior.
func normalizeArch(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "amd64", "x86_64", "x86-64", "x64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return "amd64"
	}
}

// UpdateXrayVersion updates the xray-core binary on a node to a specific version
func (u *nodeUsecase) UpdateXrayVersion(ctx context.Context, nodeID uint, version string) error {
	log := logger.GetLogger()

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer closeAgentClient(client)

	// Detect arch for the binary URL. Falls back to amd64 + WARN if
	// HostInfo unavailable; agent then fails SHA256 / exec rather than
	// silently shipping wrong-arch.
	arch := "amd64"
	if hostInfo, hiErr := client.GetHostInfo(ctx); hiErr != nil {
		log.WithError(hiErr).WithField("node_id", nodeID).
			Warn("[UpdateXrayVersion] GetHostInfo failed, assuming amd64")
	} else {
		arch = normalizeArch(hostInfo.Arch)
	}

	// Construct hub download URL and token
	downloadURL := ""
	downloadToken := ""
	checksum := ""

	if u.xrayBM != nil && u.tokenManager != nil && u.baseURL != "" {
		// Honor panel_base_path so deployments mounted under a path prefix
		// (e.g. /your-panel-path) still resolve to the correct public endpoint.
		panelBasePath := ""
		if u.settingUC != nil {
			if v, err := u.settingUC.GetByKey(ctx, "panel_base_path"); err == nil {
				panelBasePath = v
			}
		}
		downloadURL = fmt.Sprintf("%s%s/api/v1/deploy/xray/binary?version=%s&arch=%s", u.baseURL, panelBasePath, version, arch)

		token, err := u.tokenManager.GenerateDeploymentToken(nodeID, 15*time.Minute)
		if err != nil {
			log.WithError(err).Warn("[UpdateXrayVersion] Failed to generate download token, falling back to GitHub")
			downloadURL = ""
		} else {
			downloadToken = token
		}

		// Pre-warm hub cache (most agents have no path to GitHub; hub does).
		// EnsureCached is a no-op when already stored; failure here falls
		// back to the agent-side GitHub install.
		if downloadURL != "" {
			if err := u.xrayBM.EnsureCached(version, arch); err != nil {
				log.WithError(err).WithFields(map[string]interface{}{
					"version": version,
					"arch":    arch,
				}).Warn("[UpdateXrayVersion] Hub failed to fetch from GitHub — falling back to agent-side install")
				downloadURL = ""
				downloadToken = ""
			}
		}

		// Require a known checksum before instructing the agent to install
		// the binary. Without it the agent cannot verify integrity and an
		// attacker with hub-fs write could replace the binary undetected.
		if downloadURL != "" {
			cs, csErr := u.xrayBM.GetChecksum(version, arch)
			if csErr != nil || cs == "" {
				log.WithError(csErr).WithFields(map[string]interface{}{
					"version": version,
					"arch":    arch,
				}).Warn("[UpdateXrayVersion] No cached checksum after EnsureCached — falling back to agent-side install")
				downloadURL = ""
				downloadToken = ""
			} else {
				checksum = cs
			}
		}
	}

	log.WithFields(map[string]interface{}{
		"node_id":  nodeID,
		"version":  version,
		"arch":     arch,
		"from_hub": downloadURL != "",
	}).Info("[UpdateXrayVersion] Updating xray-core version")

	if err := client.UpdateXrayBinary(ctx, version, true, downloadURL, downloadToken, checksum); err != nil {
		return fmt.Errorf("failed to update xray version: %w", err)
	}

	log.WithFields(map[string]interface{}{
		"node_id": nodeID,
		"version": version,
	}).Info("[UpdateXrayVersion] Xray version updated successfully")

	return nil
}

// PushCertDenylistToNode pushes the current certificate denylist to a specific node
func (u *nodeUsecase) PushCertDenylistToNode(ctx context.Context, nodeID uint) error {
	log := logger.GetLogger()

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Get revoked serial numbers
	serials, err := u.certUC.ListRevokedSerialNumbers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list revoked serials: %w", err)
	}

	// Compute a deterministic hash for sync verification
	denylistHash := computeDenylistHash(serials)

	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer closeAgentClient(client)

	if err := client.UpdateCertDenylist(ctx, serials, denylistHash); err != nil {
		return fmt.Errorf("failed to push denylist to node %d: %w", nodeID, err)
	}

	log.WithFields(map[string]interface{}{
		"node_id":       nodeID,
		"revoked_count": len(serials),
		"hash":          denylistHash,
	}).Info("[PushCertDenylist] Denylist pushed successfully")

	return nil
}

// PushCertDenylistToAllNodes pushes the certificate denylist to all agent-enabled nodes
func (u *nodeUsecase) PushCertDenylistToAllNodes(ctx context.Context) error {
	log := logger.GetLogger()

	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	var errs []string
	for _, node := range nodes {
		if err := u.PushCertDenylistToNode(ctx, node.ID); err != nil {
			log.WithError(err).Warnf("[PushCertDenylist] Failed for node %d", node.ID)
			errs = append(errs, fmt.Sprintf("node %d: %v", node.ID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("denylist push failed for some nodes: %s", strings.Join(errs, "; "))
	}

	return nil
}

// GetAccessLogs retrieves parsed access log entries from a node, optionally filtered by email.
func (u *nodeUsecase) GetAccessLogs(ctx context.Context, nodeID uint, email string, limit int32) ([]*pb.AccessLogEntry, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	return client.GetAccessLogs(ctx, email, limit)
}

// GetAggregatedAccessLogs fans out to multiple agents in parallel and merges results.
func (u *nodeUsecase) GetAggregatedAccessLogs(ctx context.Context, nodeIDs []uint, email string, limit int32) ([]AggregatedAccessLogEntry, error) {
	// Get candidate nodes
	allNodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	// Filter to online nodes with access log enabled
	wantIDs := make(map[uint]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		wantIDs[id] = true
	}

	var targets []*domain.Node
	for _, n := range allNodes {
		if !n.IsOnline || !n.EnableAccessLog {
			continue
		}
		if len(wantIDs) > 0 && !wantIDs[n.ID] {
			continue
		}
		targets = append(targets, n)
	}

	if len(targets) == 0 {
		return nil, nil
	}

	// Fan out gRPC calls in parallel with a per-request timeout
	type result struct {
		node    *domain.Node
		entries []*pb.AccessLogEntry
	}
	ch := make(chan result, len(targets))
	var wg sync.WaitGroup

	rpcCtx, rpcCancel := context.WithTimeout(ctx, 8*time.Second)
	defer rpcCancel()

	for _, n := range targets {
		wg.Add(1)
		go func(node *domain.Node) {
			defer wg.Done()
			client, err := u.getAgentClient(node)
			if err != nil {
				return
			}
			defer closeAgentClient(client)

			entries, err := client.GetAccessLogs(rpcCtx, email, limit)
			if err != nil {
				return
			}
			ch <- result{node: node, entries: entries}
		}(n)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	// Collect all results
	var all []AggregatedAccessLogEntry
	for r := range ch {
		for _, e := range r.entries {
			all = append(all, AggregatedAccessLogEntry{
				NodeID:      r.node.ID,
				NodeName:    r.node.Name,
				NodeCountry: r.node.CountryCode,
				Timestamp:   e.Timestamp,
				SourceIP:    e.SourceIp,
				Status:      e.Status,
				Network:     e.Network,
				Domain:      e.Domain,
				Port:        e.Port,
				InboundTag:  e.InboundTag,
				OutboundTag: e.OutboundTag,
				Email:       e.Email,
			})
		}
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp > all[j].Timestamp
	})

	// Apply global limit
	if int32(len(all)) > limit {
		all = all[:limit]
	}

	return all, nil
}

// DomainCount represents a domain and its total request count.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

// GetAccessLogAnalytics returns persisted hourly access log summaries.
func (u *nodeUsecase) GetAccessLogAnalytics(ctx context.Context, filter repository.AccessLogSummaryFilter) ([]*domain.AccessLogSummary, int64, error) {
	return u.nodeRepo.GetAccessLogSummaries(ctx, filter)
}

// GetAccessLogTopDomains aggregates top domains across matching summaries.
func (u *nodeUsecase) GetAccessLogTopDomains(ctx context.Context, filter repository.AccessLogSummaryFilter, topN int) ([]DomainCount, error) {
	summaries, err := u.nodeRepo.GetAccessLogTopDomains(ctx, filter)
	if err != nil {
		return nil, err
	}

	merged := make(map[string]int64)
	for _, s := range summaries {
		if s.TopDomains == "" {
			continue
		}
		var domains map[string]int64
		if err := json.Unmarshal([]byte(s.TopDomains), &domains); err != nil {
			continue
		}
		for d, c := range domains {
			merged[d] += c
		}
	}

	result := make([]DomainCount, 0, len(merged))
	for d, c := range merged {
		result = append(result, DomainCount{Domain: d, Count: c})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	if topN > 0 && len(result) > topN {
		result = result[:topN]
	}
	return result, nil
}

// computeDenylistHash produces a SHA256 hash of sorted serial numbers for sync verification
func computeDenylistHash(serials []string) string {
	sorted := make([]string, len(serials))
	copy(sorted, serials)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(h[:])
}
