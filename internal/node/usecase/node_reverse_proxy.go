package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

func (u *nodeUsecase) ListReverseProxies(ctx context.Context, nodeID uint) ([]*domain.ReverseProxy, error) {
	return u.nodeRepo.ListReverseProxiesByNode(ctx, nodeID)
}

func (u *nodeUsecase) GetReverseProxy(ctx context.Context, id uint) (*domain.ReverseProxy, error) {
	return u.nodeRepo.GetReverseProxyWithNode(ctx, id)
}

func (u *nodeUsecase) AddReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error {
	log := logger.GetLogger()

	if err := u.validateReverseProxy(ctx, rp); err != nil {
		return err
	}

	// Wrap in transaction: create reverse proxy + generate routing rules
	if err := u.nodeRepo.Transaction(ctx, func(txRepo repository.NodeRepository) error {
		if err := txRepo.CreateReverseProxy(ctx, rp); err != nil {
			return fmt.Errorf("failed to create reverse proxy: %w", err)
		}
		return u.generateReverseProxyRulesWithRepo(ctx, txRepo, rp)
	}); err != nil {
		return err
	}

	// Push config to agent in background (don't block HTTP response)
	nodeID := rp.NodeID
	go func() {
		pushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		node, err := u.nodeRepo.GetNode(pushCtx, nodeID)
		if err != nil {
			log.Warnf("Failed to get node %d for reverse proxy config push: %v", nodeID, err)
			return
		}
		if err := u.pushConfigToAgent(pushCtx, node); err != nil {
			log.Warnf("Failed to push config to agent after adding reverse proxy: %v", err)
		}
	}()

	return nil
}

func (u *nodeUsecase) UpdateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error {
	log := logger.GetLogger()

	existing, err := u.nodeRepo.GetReverseProxy(ctx, rp.ID)
	if err != nil {
		return fmt.Errorf("reverse proxy not found: %w", err)
	}
	rp.NodeID = existing.NodeID // Ensure node ID cannot change

	if err := u.validateReverseProxy(ctx, rp); err != nil {
		return err
	}

	// Read old rule priorities before deleting so regenerated rules keep the same position
	var oldPriority1, oldPriority2 int
	hasOldPriorities := false
	if existing.Rule1ID != nil {
		if r, err := u.nodeRepo.GetRoutingRule(ctx, *existing.Rule1ID); err == nil {
			oldPriority1 = r.Priority
			hasOldPriorities = true
		}
	}
	if existing.Rule2ID != nil {
		if r, err := u.nodeRepo.GetRoutingRule(ctx, *existing.Rule2ID); err == nil {
			oldPriority2 = r.Priority
			hasOldPriorities = true
		}
	}

	// Wrap in transaction: delete old rules + update + generate new rules
	if err := u.nodeRepo.Transaction(ctx, func(txRepo repository.NodeRepository) error {
		u.deleteReverseProxyRulesWithRepo(ctx, txRepo, existing)
		if err := txRepo.UpdateReverseProxy(ctx, rp); err != nil {
			return fmt.Errorf("failed to update reverse proxy: %w", err)
		}
		if hasOldPriorities {
			return u.generateReverseProxyRulesWithRepo(ctx, txRepo, rp, oldPriority1, oldPriority2)
		}
		return u.generateReverseProxyRulesWithRepo(ctx, txRepo, rp)
	}); err != nil {
		return err
	}

	// Push config in background (don't block HTTP response)
	nodeID := rp.NodeID
	go func() {
		pushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		node, err := u.nodeRepo.GetNode(pushCtx, nodeID)
		if err != nil {
			log.Warnf("Failed to get node %d for reverse proxy config push: %v", nodeID, err)
			return
		}
		if err := u.pushConfigToAgent(pushCtx, node); err != nil {
			log.Warnf("Failed to push config to agent after updating reverse proxy: %v", err)
		}
	}()

	return nil
}

func (u *nodeUsecase) DeleteReverseProxy(ctx context.Context, id uint) error {
	rp, err := u.nodeRepo.GetReverseProxyWithNode(ctx, id)
	if err != nil {
		return fmt.Errorf("reverse proxy not found: %w", err)
	}

	// Wrap in transaction: delete rules + delete reverse proxy
	if err := u.nodeRepo.Transaction(ctx, func(txRepo repository.NodeRepository) error {
		u.deleteReverseProxyRulesWithRepo(ctx, txRepo, rp)
		return txRepo.DeleteReverseProxy(ctx, id)
	}); err != nil {
		return err
	}

	// Push config in background (don't block HTTP response)
	if rp.Node != nil {
		node := rp.Node
		go func() {
			pushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := u.pushConfigToAgent(pushCtx, node); err != nil {
				logger.GetLogger().Warnf("Failed to push config to agent after deleting reverse proxy: %v", err)
			}
		}()
	}

	return nil
}

// validateReverseProxy checks all constraints.
func (u *nodeUsecase) validateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error {
	if rp.Type != "bridge" && rp.Type != "portal" {
		return fmt.Errorf("type must be 'bridge' or 'portal'")
	}
	if strings.TrimSpace(rp.Tag) == "" {
		return fmt.Errorf("tag is required")
	}
	if strings.TrimSpace(rp.Domain) == "" {
		return fmt.Errorf("domain is required")
	}

	// Check tag uniqueness within node (across reverse proxies, inbounds, outbounds)
	inbounds, err := u.nodeRepo.ListInboundsByNode(ctx, rp.NodeID)
	if err != nil {
		return fmt.Errorf("failed to check inbound tags: %w", err)
	}
	for _, in := range inbounds {
		if in.Tag == rp.Tag {
			return fmt.Errorf("tag '%s' conflicts with an existing inbound tag", rp.Tag)
		}
	}

	outbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, rp.NodeID)
	if err != nil {
		return fmt.Errorf("failed to check outbound tags: %w", err)
	}
	outboundTagSet := make(map[string]bool, len(outbounds))
	for _, out := range outbounds {
		outboundTagSet[out.Tag] = true
		if out.Tag == rp.Tag {
			return fmt.Errorf("tag '%s' conflicts with an existing outbound tag", rp.Tag)
		}
	}

	inboundTagSet := make(map[string]bool, len(inbounds))
	for _, in := range inbounds {
		inboundTagSet[in.Tag] = true
	}

	// Check tag uniqueness among other reverse proxies
	existingRPs, err := u.nodeRepo.ListReverseProxiesByNode(ctx, rp.NodeID)
	if err != nil {
		return fmt.Errorf("failed to check reverse proxy tags: %w", err)
	}
	for _, existing := range existingRPs {
		if existing.ID != rp.ID && existing.Tag == rp.Tag {
			return fmt.Errorf("tag '%s' already used by another reverse proxy", rp.Tag)
		}
	}

	// Type-specific validation
	if rp.Type == "bridge" {
		if strings.TrimSpace(rp.InterconnectionTag) == "" {
			return fmt.Errorf("interconnection outbound tag is required for bridge")
		}
		if !outboundTagSet[rp.InterconnectionTag] {
			return fmt.Errorf("interconnection outbound '%s' does not exist", rp.InterconnectionTag)
		}
		if strings.TrimSpace(rp.OutboundTag) == "" {
			return fmt.Errorf("outbound tag is required for bridge")
		}
		if !outboundTagSet[rp.OutboundTag] {
			return fmt.Errorf("outbound '%s' does not exist", rp.OutboundTag)
		}
	} else {
		if len(rp.InterconnectionTags) == 0 {
			return fmt.Errorf("at least one interconnection inbound tag is required for portal")
		}
		for _, tag := range rp.InterconnectionTags {
			if !inboundTagSet[tag] {
				return fmt.Errorf("interconnection inbound '%s' does not exist", tag)
			}
		}
		if len(rp.InboundTags) == 0 {
			return fmt.Errorf("at least one inbound tag is required for portal")
		}
		for _, tag := range rp.InboundTags {
			if !inboundTagSet[tag] {
				return fmt.Errorf("inbound '%s' does not exist", tag)
			}
		}
	}

	return nil
}

// generateReverseProxyRulesWithRepo creates the 2 auto-generated routing rules using the provided repo (for transaction support).
// When oldPriorities are provided (exactly 2 values), they override the defaults (9000, 9001).
func (u *nodeUsecase) generateReverseProxyRulesWithRepo(ctx context.Context, repo repository.NodeRepository, rp *domain.ReverseProxy, oldPriorities ...int) error {
	remark := "[reverse] " + rp.Tag

	priority1, priority2 := 9000, 9001
	if len(oldPriorities) >= 2 {
		priority1 = oldPriorities[0]
		priority2 = oldPriorities[1]
	}

	var rule1, rule2 domain.RoutingRule

	if rp.Type == "bridge" {
		// Rule 1: domain-matching tunnel control -> interconnection outbound
		rule1 = domain.RoutingRule{
			NodeID:      rp.NodeID,
			RuleTag:     fmt.Sprintf("reverse-%s-tunnel", rp.Tag),
			Remark:      remark,
			Priority:    priority1,
			Enabled:     true,
			OutboundTag: rp.InterconnectionTag,
			InboundTags: []string{rp.Tag},
			DomainRules: domain.DomainMatcherSlice{
				{Type: domain.DomainTypeFull, Value: rp.Domain},
			},
		}
		// Rule 2: catch-all for bridge tag -> user traffic outbound
		rule2 = domain.RoutingRule{
			NodeID:      rp.NodeID,
			RuleTag:     fmt.Sprintf("reverse-%s-traffic", rp.Tag),
			Remark:      remark,
			Priority:    priority2,
			Enabled:     true,
			OutboundTag: rp.OutboundTag,
			InboundTags: []string{rp.Tag},
		}
	} else {
		// Portal rule 1: interconnection inbound traffic matching the internal domain -> portal tag
		rule1 = domain.RoutingRule{
			NodeID:      rp.NodeID,
			RuleTag:     fmt.Sprintf("reverse-%s-interconn", rp.Tag),
			Remark:      remark,
			Priority:    priority1,
			Enabled:     true,
			OutboundTag: rp.Tag,
			InboundTags: rp.InterconnectionTags,
			DomainRules: domain.DomainMatcherSlice{
				{Type: domain.DomainTypeFull, Value: rp.Domain},
			},
		}
		rule2 = domain.RoutingRule{
			NodeID:      rp.NodeID,
			RuleTag:     fmt.Sprintf("reverse-%s-external", rp.Tag),
			Remark:      remark,
			Priority:    priority2,
			Enabled:     true,
			OutboundTag: rp.Tag,
			InboundTags: rp.InboundTags,
		}
	}

	if err := repo.CreateRoutingRule(ctx, &rule1); err != nil {
		return fmt.Errorf("failed to create rule 1: %w", err)
	}
	if err := repo.CreateRoutingRule(ctx, &rule2); err != nil {
		return fmt.Errorf("failed to create rule 2: %w", err)
	}

	// Store rule IDs on the reverse proxy
	rp.Rule1ID = &rule1.ID
	rp.Rule2ID = &rule2.ID
	return repo.UpdateReverseProxy(ctx, rp)
}

// deleteReverseProxyRulesWithRepo removes the auto-generated routing rules using the provided repo.
func (u *nodeUsecase) deleteReverseProxyRulesWithRepo(ctx context.Context, repo repository.NodeRepository, rp *domain.ReverseProxy) {
	log := logger.GetLogger()
	if rp.Rule1ID != nil {
		if err := repo.DeleteRoutingRule(ctx, *rp.Rule1ID); err != nil {
			log.Warnf("Failed to delete reverse proxy rule 1 (ID %d): %v", *rp.Rule1ID, err)
		}
	}
	if rp.Rule2ID != nil {
		if err := repo.DeleteRoutingRule(ctx, *rp.Rule2ID); err != nil {
			log.Warnf("Failed to delete reverse proxy rule 2 (ID %d): %v", *rp.Rule2ID, err)
		}
	}
}
