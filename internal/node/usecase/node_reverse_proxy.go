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
	if rp.Tag != existing.Tag {
		kind := "outbound"
		if existing.Type == "bridge" {
			kind = "inbound"
		}
		if err := u.rejectReferencedTagRename(ctx, rp.NodeID, existing.Tag, kind, existing); err != nil {
			return err
		}
	}

	if err := u.validateReverseProxy(ctx, rp); err != nil {
		return err
	}

	// Read old rule priorities before deleting so regenerated rules keep the same position
	var oldPriority int
	hasOldPriorities := false
	if existing.Rule2ID != nil {
		if r, err := u.nodeRepo.GetRoutingRule(ctx, *existing.Rule2ID); err == nil {
			oldPriority = r.Priority
			hasOldPriorities = true
		}
	}

	// Wrap in transaction: delete old rules + update + generate new rules
	if err := u.nodeRepo.Transaction(ctx, func(txRepo repository.NodeRepository) error {
		if err := u.deleteReverseProxyRulesWithRepo(ctx, txRepo, existing); err != nil {
			return err
		}
		if err := txRepo.UpdateReverseProxy(ctx, rp); err != nil {
			return fmt.Errorf("failed to update reverse proxy: %w", err)
		}
		if hasOldPriorities {
			return u.generateReverseProxyRulesWithRepo(ctx, txRepo, rp, oldPriority)
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
		if err := u.deleteReverseProxyRulesWithRepo(ctx, txRepo, rp); err != nil {
			return err
		}
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

	if domain.IsGeneratedXrayTag(rp.Tag) {
		return fmt.Errorf("tag %q is reserved for generated Xray handlers", rp.Tag)
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
	for _, out := range outbounds {
		if out.Tag == rp.Tag {
			return fmt.Errorf("tag '%s' conflicts with an existing outbound tag", rp.Tag)
		}
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

	// Validate the protocol and ownership constraints against all entries.
	candidates := make([]*domain.ReverseProxy, 0, len(existingRPs)+1)
	for _, other := range existingRPs {
		if other.ID != rp.ID {
			candidates = append(candidates, other)
		}
	}
	candidates = append(candidates, rp)
	return domain.ValidateVLESSReverse(inbounds, outbounds, candidates)
}

// generateReverseProxyRulesWithRepo stores the managed VLESS Reverse traffic route.
func (u *nodeUsecase) generateReverseProxyRulesWithRepo(ctx context.Context, repo repository.NodeRepository, rp *domain.ReverseProxy, oldPriorities ...int) error {
	priority := 9000
	if len(oldPriorities) > 0 {
		priority = oldPriorities[0]
	}
	rule := &domain.RoutingRule{NodeID: rp.NodeID, RuleTag: "reverse-" + rp.Tag + "-traffic", Remark: "[reverse] " + rp.Tag, Priority: priority, Enabled: true}
	if rp.Type == "bridge" {
		rule.InboundTags = []string{rp.Tag}
		rule.OutboundTag = rp.OutboundTag
	} else {
		rule.InboundTags = rp.InboundTags
		rule.OutboundTag = rp.Tag
	}
	if err := repo.CreateRoutingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to create reverse traffic rule: %w", err)
	}
	rp.Rule2ID = &rule.ID
	if err := repo.UpdateReverseProxy(ctx, rp); err != nil {
		return err
	}
	rules, err := repo.ListRoutingRulesByNode(ctx, rp.NodeID)
	if err != nil {
		return fmt.Errorf("failed to load routing order: %w", err)
	}
	ordered := domain.OrderReverseRoutingRules(rules, []*domain.ReverseProxy{rp})
	ids := make([]uint, len(ordered))
	for i, rule := range ordered {
		ids[i] = rule.ID
	}
	return repo.ReorderRoutingRules(ctx, rp.NodeID, ids)
}

// deleteReverseProxyRulesWithRepo removes the auto-generated routing rules using the provided repo.
func (u *nodeUsecase) deleteReverseProxyRulesWithRepo(ctx context.Context, repo repository.NodeRepository, rp *domain.ReverseProxy) error {
	if rp.Rule2ID != nil {
		if err := repo.DeleteRoutingRule(ctx, *rp.Rule2ID); err != nil {
			return fmt.Errorf("failed to delete reverse proxy rule %d: %w", *rp.Rule2ID, err)
		}
	}
	return nil
}
