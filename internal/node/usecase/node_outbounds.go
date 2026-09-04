package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/link"
)

// === Outbound Management ===

// validateProxyChaining validates proxy_settings on an outbound:
// - prevents self-reference (tag == proxy_settings.tag)
// - verifies the target tag exists on the same node
// - detects circular chains (A→B→...→A)
func (u *nodeUsecase) validateProxyChaining(ctx context.Context, outbound *domain.Outbound) error {
	ps := outbound.ProxySettings
	if ps == nil || ps.Tag == "" {
		return nil
	}

	// Self-reference check
	if ps.Tag == outbound.Tag {
		return fmt.Errorf("proxy chaining: outbound '%s' cannot chain to itself", outbound.Tag)
	}

	// Load all outbounds on this node for target existence + cycle detection
	allOutbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, outbound.NodeID)
	if err != nil {
		return fmt.Errorf("proxy chaining: failed to list outbounds: %w", err)
	}

	// Build a tag→proxy_settings.tag map (using the about-to-be-saved outbound's value)
	chainMap := make(map[string]string, len(allOutbounds))
	targetExists := false
	for _, o := range allOutbounds {
		if o.Tag == outbound.Tag {
			// Use the new value being saved, not the DB value
			continue
		}
		if o.Tag == ps.Tag {
			targetExists = true
		}
		if o.ProxySettings != nil && o.ProxySettings.Tag != "" {
			chainMap[o.Tag] = o.ProxySettings.Tag
		}
	}

	if !targetExists {
		return fmt.Errorf("proxy chaining: target outbound '%s' does not exist on this node", ps.Tag)
	}

	// Insert the current outbound's chain into the map
	chainMap[outbound.Tag] = ps.Tag

	// Walk the chain from this outbound to detect cycles (max depth = number of outbounds)
	visited := make(map[string]bool, len(chainMap))
	current := outbound.Tag
	for i := 0; i < len(allOutbounds)+1; i++ {
		next, ok := chainMap[current]
		if !ok {
			break // end of chain
		}
		if visited[next] {
			return fmt.Errorf("proxy chaining: circular chain detected (cycle includes '%s')", next)
		}
		visited[current] = true
		current = next
	}

	return nil
}

// ensureUniqueOutboundTag rejects a tag already used by another outbound on the
// node; duplicate tags stop xray from starting.
func (u *nodeUsecase) ensureUniqueOutboundTag(ctx context.Context, outbound *domain.Outbound) error {
	existing, err := u.nodeRepo.ListOutboundsByNode(ctx, outbound.NodeID)
	if err != nil {
		return nil // don't block on a transient list error; push would surface it
	}
	for _, e := range existing {
		if e.ID != outbound.ID && e.Tag == outbound.Tag {
			return fmt.Errorf("an outbound with tag %q already exists on this node", outbound.Tag)
		}
	}
	return nil
}

// findOutboundsChainingTo returns outbound tags that have proxy_settings.tag == targetTag
func (u *nodeUsecase) findOutboundsChainingTo(ctx context.Context, nodeID uint, targetTag string) ([]string, error) {
	allOutbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, o := range allOutbounds {
		if o.ProxySettings != nil && o.ProxySettings.Tag == targetTag {
			refs = append(refs, o.Tag)
		}
	}
	return refs, nil
}

// AddOutbound creates an outbound in DB and pushes it to Xray.
// Mirrors AddInbound: rolls back the DB record if the push fails so a
// broken outbound doesn't poison drift detection.
func (u *nodeUsecase) AddOutbound(ctx context.Context, outbound *domain.Outbound) error {
	log := logger.GetLogger()

	if err := validateOutboundProtocol(outbound.Protocol); err != nil {
		return err
	}
	if err := ValidateOutbound(outbound); err != nil {
		return err
	}

	node, err := u.nodeRepo.GetNode(ctx, outbound.NodeID)
	if err != nil {
		return ErrNodeNotFound
	}

	if err := u.ensureUniqueOutboundTag(ctx, outbound); err != nil {
		return err
	}

	if err := u.validateProxyChaining(ctx, outbound); err != nil {
		return err
	}

	if err := u.nodeRepo.CreateOutbound(ctx, outbound); err != nil {
		return err
	}

	if err := u.pushConfigToAgent(ctx, node); err != nil {
		log.WithError(err).Warn("[AddOutbound] Config push failed, rolling back DB record")
		if delErr := u.nodeRepo.DeleteOutbound(ctx, outbound.ID); delErr != nil {
			log.WithError(delErr).Error("[AddOutbound] Failed to roll back outbound from DB")
		}
		return fmt.Errorf("failed to apply outbound config on node: %w", err)
	}

	return nil
}

// ListOutbounds returns the stored outbounds, preceded in router mode by the
// generated ones — otherwise the two that pick the uplink are invisible.
func (u *nodeUsecase) ListOutbounds(ctx context.Context, nodeID uint) ([]*domain.Outbound, error) {
	stored, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !u.routerMode {
		return stored, nil
	}
	return append(managedOutbounds(nodeID, u.currentRouterWANs(ctx)), stored...), nil
}

// From the builder's own definition, so the list can't drift from the config.
func managedOutbounds(nodeID uint, vias []xray.RouterWAN) []*domain.Outbound {
	gen := xray.RouterOutbounds(vias)
	out := make([]*domain.Outbound, 0, len(gen))
	for _, g := range gen {
		out = append(out, &domain.Outbound{
			NodeID:          nodeID,
			Managed:         true,
			Tag:             g.Tag,
			Protocol:        g.Protocol,
			Remark:          g.Remark,
			FreedomSettings: &domain.FreedomSettings{},
			SockoptSettings: &domain.SockoptSettings{Mark: g.Mark},
		})
	}
	return out
}

func (u *nodeUsecase) GetOutbound(ctx context.Context, id uint) (*domain.Outbound, error) {
	return u.nodeRepo.GetOutbound(ctx, id)
}

func (u *nodeUsecase) DeleteOutbound(ctx context.Context, id uint) error {
	outbound, err := u.nodeRepo.GetOutboundWithNode(ctx, id)
	if err != nil {
		return err
	}

	// Check if any reverse proxy references this outbound's tag
	if outbound.Node != nil {
		refs, refErr := u.nodeRepo.ListReverseProxiesByReferencedTag(ctx, outbound.NodeID, outbound.Tag)
		if refErr == nil && len(refs) > 0 {
			return fmt.Errorf("cannot delete: outbound '%s' is referenced by reverse proxy '%s'", outbound.Tag, refs[0].Tag)
		}
	}

	// Check if any other outbound chains to this one via proxy_settings
	chainRefs, err := u.findOutboundsChainingTo(ctx, outbound.NodeID, outbound.Tag)
	if err == nil && len(chainRefs) > 0 {
		return fmt.Errorf("cannot delete: outbound '%s' is a proxy chain target for outbound '%s'", outbound.Tag, chainRefs[0])
	}

	// 1. Delete from DB
	if err := u.nodeRepo.DeleteOutbound(ctx, id); err != nil {
		return err
	}

	// 2. Delete from Xray - via agent
	if outbound.Node != nil {
		if pushErr := u.pushConfigToAgent(ctx, outbound.Node); pushErr != nil {
			logger.GetLogger().WithError(pushErr).WithField("node_id", outbound.Node.ID).Warn("[DeleteOutbound] Failed to push config to agent after outbound deletion")
		}
	}

	return nil
}

func (u *nodeUsecase) ToggleOutboundDisabled(ctx context.Context, id uint) (*domain.Outbound, error) {
	log := logger.GetLogger()

	// Fetch outbound with node to get tag and node_id
	outbound, err := u.nodeRepo.GetOutboundWithNode(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validation: only when disabling (currently enabled → about to disable)
	if !outbound.IsDisabled {
		// Check routing rules
		rules, err := u.nodeRepo.ListRoutingRulesByOutboundTag(ctx, outbound.NodeID, outbound.Tag)
		if err != nil {
			return nil, fmt.Errorf("failed to check routing rules: %w", err)
		}
		if len(rules) > 0 {
			return nil, fmt.Errorf("cannot disable: outbound '%s' is referenced by routing rule '%s'", outbound.Tag, rules[0].RuleTag)
		}

		// Check reverse proxies
		refs, err := u.nodeRepo.ListReverseProxiesByReferencedTag(ctx, outbound.NodeID, outbound.Tag)
		if err != nil {
			return nil, fmt.Errorf("failed to check reverse proxies: %w", err)
		}
		if len(refs) > 0 {
			return nil, fmt.Errorf("cannot disable: outbound '%s' is referenced by reverse proxy '%s'", outbound.Tag, refs[0].Tag)
		}

		// Check if any other outbound chains to this one via proxy_settings
		chainRefs, err := u.findOutboundsChainingTo(ctx, outbound.NodeID, outbound.Tag)
		if err != nil {
			return nil, fmt.Errorf("failed to check proxy chains: %w", err)
		}
		if len(chainRefs) > 0 {
			return nil, fmt.Errorf("cannot disable: outbound '%s' is a proxy chain target for outbound '%s'", outbound.Tag, chainRefs[0])
		}
	}

	if err := u.nodeRepo.ToggleOutboundDisabled(ctx, id); err != nil {
		log.WithError(err).WithField("outbound_id", id).Error("[ToggleOutboundDisabled] Failed to toggle")
		return nil, err
	}

	outbound, err = u.nodeRepo.GetOutboundWithNode(ctx, id)
	if err != nil {
		return nil, err
	}

	log.WithFields(map[string]interface{}{
		"outbound_id": id,
		"is_disabled": outbound.IsDisabled,
	}).Info("[ToggleOutboundDisabled] Toggled outbound")

	// Push updated config to agent
	if outbound.Node != nil {
		if pushErr := u.pushConfigToAgent(ctx, outbound.Node); pushErr != nil {
			log.WithError(pushErr).Warn("[ToggleOutboundDisabled] Config push failed (will be retried by drift detection)")
		}
	}

	return outbound, nil
}

func (u *nodeUsecase) UpdateOutbound(ctx context.Context, outbound *domain.Outbound) error {
	log := logger.GetLogger()
	if err := validateOutboundProtocol(outbound.Protocol); err != nil {
		return err
	}
	if err := ValidateOutbound(outbound); err != nil {
		return err
	}
	if err := u.ensureUniqueOutboundTag(ctx, outbound); err != nil {
		return err
	}
	if err := u.validateProxyChaining(ctx, outbound); err != nil {
		return err
	}

	// snapshot the row so we can roll back if the push fails — one bad edit would
	// otherwise wedge every later config push for the node
	prev, prevErr := u.nodeRepo.GetOutbound(ctx, outbound.ID)

	if err := u.nodeRepo.UpdateOutbound(ctx, outbound); err != nil {
		return err
	}

	node, err := u.nodeRepo.GetNode(ctx, outbound.NodeID)
	if err != nil {
		return nil
	}
	if pushErr := u.pushConfigToAgent(ctx, node); pushErr != nil {
		// Roll back to the previous good state and restore the live config.
		if prevErr == nil && prev != nil {
			if rbErr := u.nodeRepo.UpdateOutbound(ctx, prev); rbErr != nil {
				log.WithError(rbErr).Error("[UpdateOutbound] Failed to roll back outbound after push failure")
			} else if rbPushErr := u.pushConfigToAgent(ctx, node); rbPushErr != nil {
				log.WithError(rbPushErr).Error("[UpdateOutbound] Failed to re-push config after rollback")
			}
		}
		return fmt.Errorf("outbound reverted: failed to apply config on node: %w", pushErr)
	}
	return nil
}

// validateOutboundProtocol guards the AddOutbound/UpdateOutbound paths so the
// usecase rejects unknown protocols before they hit the config builder
// (which would silently emit empty settings, producing a config that xray
// either rejects or runs in a broken state).
func validateOutboundProtocol(p string) error {
	switch p {
	case "freedom", "blackhole", "vless", "vmess", "trojan", "shadowsocks",
		"wireguard", "http", "socks", "dns", "loopback", "hysteria2":
		return nil
	}
	return fmt.Errorf("unsupported outbound protocol: %q", p)
}

// === Outbound Synchronization ===

func (u *nodeUsecase) SyncOutbounds(ctx context.Context, nodeID uint) (*SyncResult, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, ErrNodeNotFound
	}

	dbOutbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	if err := u.pushConfigToAgent(ctx, node); err != nil {
		return nil, fmt.Errorf("failed to push config to agent: %w", err)
	}
	return &SyncResult{
		Restored: len(dbOutbounds),
		Kept:     0,
		Imported: 0,
		Errors:   0,
	}, nil
}

// OutboundTestOptions are per-request overrides for a single outbound test.
// Everything not set here comes from the node's OutboundTestSettings.
type OutboundTestOptions struct {
	TestURL   string // overrides the node's configured test URL
	Speedtest bool   // also measure throughput (opt-in: it burns upstream traffic)
}

// OutboundTestOutcome is a test result paired with the moment it was taken.
// Both halves are persisted on the outbound, so the panel renders the same
// values after a reload as it did right after the test.
type OutboundTestOutcome struct {
	Result   *domain.OutboundTestResult `json:"result"`
	TestedAt time.Time                  `json:"tested_at"`
}

// TestOutbound tests outbound connectivity via the agent using xray-knife and
// records the outcome on the outbound.
func (u *nodeUsecase) TestOutbound(ctx context.Context, outboundID uint, opts OutboundTestOptions) (*OutboundTestOutcome, error) {
	outbound, err := u.nodeRepo.GetOutboundWithNode(ctx, outboundID)
	if err != nil {
		return nil, ErrOutboundNotFound
	}
	// Protocols with nothing to probe are answered without needing a node at all.
	if !outbound.IsTestable() {
		return u.recordTestResult(ctx, outbound.ID, &domain.OutboundTestResult{
			Success: true,
			Status:  domain.OutboundTestNotApplicable,
			Message: fmt.Sprintf("N/A - %s outbound", outbound.Protocol),
		}), nil
	}

	if outbound.Node == nil {
		return nil, fmt.Errorf("outbound testing requires a node")
	}

	settings := outbound.Node.GetOutboundTestSettingsOrDefault()
	testURL := opts.TestURL
	if testURL == "" {
		testURL = settings.TestURL
	}

	// Freedom has no share-link to build — the agent probes its own egress, and
	// a raw probe has no throughput mode. Dropping the flag here (rather than
	// letting the agent refuse) keeps a caller that asked for one from
	// overwriting a good stored result with a refusal.
	speedtest := opts.Speedtest && outbound.Protocol != "freedom"

	spec := &agent.OutboundTestSpec{
		TestURL:     testURL,
		MaxDelayMs:  int32(settings.MaxDelayMs),
		Retries:     int32(settings.Retries),
		InsecureTLS: *settings.InsecureTLS,
		Speedtest:   speedtest,
		SpeedtestKb: int32(settings.SpeedtestKb),
	}

	if outbound.Protocol == "freedom" {
		spec.DirectProbe = true
	} else {
		configLink, err := link.Generate(outbound)
		if err != nil {
			return nil, fmt.Errorf("cannot generate config link: %w", err)
		}
		spec.ConfigLink = configLink
	}

	client, err := u.getAgentClient(outbound.Node)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer closeAgentClient(client)

	rpcCtx, cancel := context.WithTimeout(ctx, outboundTestBudget(settings, speedtest))
	defer cancel()

	agentResult, err := client.TestOutbound(rpcCtx, spec)
	if err != nil {
		return u.recordTestResult(ctx, outbound.ID, &domain.OutboundTestResult{
			Success: false,
			Status:  domain.OutboundTestFailed,
			Error:   "agent: " + err.Error(),
		}), nil
	}

	result := &domain.OutboundTestResult{
		Success:      agentResult.Success,
		Status:       agentResult.Status,
		LatencyMs:    agentResult.LatencyMs,
		TTFBMs:       agentResult.TTFBMs,
		ConnectMs:    agentResult.ConnectMs,
		StatusCode:   agentResult.StatusCode,
		IP:           agentResult.IP,
		Country:      agentResult.Country,
		DownloadMbps: agentResult.DownloadMbps,
		UploadMbps:   agentResult.UploadMbps,
		Speedtest:    speedtest,
		Error:        agentResult.Error,
		Message:      agentResult.Message,
	}
	// Agents predating the v2 response fields report success as a bare bool.
	if result.Status == "" {
		if result.Success {
			result.Status = domain.OutboundTestPassed
		} else {
			result.Status = domain.OutboundTestFailed
		}
	}

	return u.recordTestResult(ctx, outbound.ID, result), nil
}

// Hard ceilings on how long one test may occupy the RPC, whatever the node's
// settings say. The panel's HTTP client has to be able to outwait these or the
// browser gives up on a test the agent then completes for nobody.
const (
	maxOutboundTestBudget          = 90 * time.Second
	maxOutboundSpeedtestBudget     = 240 * time.Second
	outboundTestSetupSlack         = 15 * time.Second
	outboundSpeedtestDirectionCost = 60 * time.Second // knife: 30s per direction, doubled for slack
)

// outboundTestBudget bounds the agent RPC. The tester makes 1+Retries attempts,
// each able to burn the full max delay, plus setup for the temporary core
// instance, plus the two speedtest directions when those are requested.
func outboundTestBudget(settings *domain.OutboundTestSettings, speedtest bool) time.Duration {
	attempts := 1 + settings.Retries
	budget := time.Duration(settings.MaxDelayMs)*time.Millisecond*time.Duration(attempts) + outboundTestSetupSlack

	cap := maxOutboundTestBudget
	if speedtest {
		budget += 2 * outboundSpeedtestDirectionCost
		cap = maxOutboundSpeedtestBudget
	}
	if budget > cap {
		budget = cap
	}
	return budget
}

// recordTestResult persists a result and returns it paired with its timestamp.
// A persistence failure is logged, never surfaced: the caller asked for a test,
// and the test itself succeeded or failed on its own merits.
func (u *nodeUsecase) recordTestResult(ctx context.Context, outboundID uint, result *domain.OutboundTestResult) *OutboundTestOutcome {
	testedAt := time.Now()
	// Detached from the request: a test that outlives the browser's patience
	// (or a Test All the operator navigated away from) has still produced a
	// real verdict, and dropping it would leave a stale result on screen.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := u.nodeRepo.UpdateOutboundTestResult(writeCtx, outboundID, result, testedAt); err != nil {
		logger.GetLogger().WithError(err).WithField("outbound_id", outboundID).
			Warn("[TestOutbound] Failed to persist test result")
	}
	return &OutboundTestOutcome{Result: result, TestedAt: testedAt}
}

func (u *nodeUsecase) DiscoverOutbounds(ctx context.Context, nodeID uint) ([]*domain.Outbound, error) {
	if _, err := u.nodeRepo.GetNode(ctx, nodeID); err != nil {
		return nil, ErrNodeNotFound
	}

	// Discovery doesn't make sense (agent manages Xray config from DB)
	return []*domain.Outbound{}, nil
}
