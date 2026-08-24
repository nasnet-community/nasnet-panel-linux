package usecase

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// GenerateXrayConfig generates the full Xray configuration for a node from the database
func (u *nodeUsecase) GenerateXrayConfig(ctx context.Context, nodeID uint) (string, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return "", err
	}

	// Get all inbounds for this node
	inbounds, err := u.nodeRepo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to list inbounds: %w", err)
	}

	// Convert to domain pointers
	inboundPtrs := make([]*domain.Inbound, len(inbounds))
	copy(inboundPtrs, inbounds)

	// Get outbounds
	outbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to list outbounds: %w", err)
	}
	outboundPtrs := make([]*domain.Outbound, len(outbounds))
	copy(outboundPtrs, outbounds)

	// Get routing rules
	rules, err := u.nodeRepo.ListRoutingRulesByNode(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to list routing rules: %w", err)
	}
	rulePtrs := make([]*domain.RoutingRule, len(rules))
	copy(rulePtrs, rules)

	// Get reverse proxies
	reverseProxies, err := u.nodeRepo.ListReverseProxiesByNode(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to list reverse proxies: %w", err)
	}

	// Get balancing rules
	balancingRules, err := u.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to list balancing rules: %w", err)
	}

	// Populate user's map
	usersMap := make(map[string][]*xray.User)
	accounts, err := u.accountRepo.ListByNodeID(ctx, nodeID)
	if err != nil {
		// Log error but continue (empty users is better than failure for bootstrap)
		logger.GetLogger().Warnf("failed to list accounts for config gen: %v", err)
	} else {
		for _, acc := range accounts {
			// Only active subscriptions' accounts belong in the generated config.
			if acc.Subscription == nil || acc.Subscription.Status != "active" {
				continue
			}
			in := acc.Inbound
			if in == nil || in.NodeID != node.ID {
				continue
			}
			bwTier := bandwidth.GetTier(acc.Subscription.GetEffectiveBandwidthLimit())
			usersMap[in.Tag] = append(usersMap[in.Tag], &xray.User{
				Email:      acc.Email,
				UUID:       acc.UUID,
				Level:      bwTier.Level,
				Protocol:   xray.Protocol(in.Protocol),
				Flow:       acc.Flow,
				Encryption: acc.Encryption,
				AlterId:    0,
			})
		}
	}

	// Build full Xray config
	configBuilder := xray.NewFullConfigBuilder(node).
		WithRouterMode(u.routerMode).
		WithRouterWANs(u.currentRouterWANs(ctx)).
		WithInbounds(inboundPtrs).
		WithOutbounds(outboundPtrs).
		WithRoutingRules(rulePtrs).
		WithBalancingRules(balancingRules).
		WithReverseProxies(reverseProxies).
		WithUsers(usersMap).
		WithAPI(true, node.APIPort)

	return configBuilder.Build()
}
