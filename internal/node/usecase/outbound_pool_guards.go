package usecase

import (
	"context"
	"fmt"
	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"strings"
)

func (u *nodeUsecase) guardPoolOutbound(ctx context.Context, out *d.Outbound, removing bool) error {
	if out.ManagedPoolID != nil {
		return fmt.Errorf("outbound is managed by pool %d; edit the pool instead", *out.ManagedPoolID)
	}
	if u.pools == nil || !removing {
		return nil
	}
	var count int64
	err := u.pools.db.WithContext(ctx).Table("outbound_pools").Joins("JOIN balancing_rules ON balancing_rules.id = outbound_pools.balancer_id").Where("outbound_pools.node_id = ? AND balancing_rules.fallback_tag = ?", out.NodeID, out.Tag).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("outbound is a pool fallback; choose another fallback first")
	}
	return nil
}
func (u *nodeUsecase) guardPoolBalancer(ctx context.Context, rule *d.BalancingRule, deleting bool) error {
	if u.pools == nil {
		return nil
	}
	var pools []d.OutboundPool
	if err := u.pools.db.WithContext(ctx).Where("node_id = ?", rule.NodeID).Find(&pools).Error; err != nil {
		return err
	}
	for _, p := range pools {
		if p.BalancerID == rule.ID {
			if deleting || !rule.Enabled {
				return fmt.Errorf("%w: remove the pool before deleting or disabling its balancer", ErrInvalidRoutingRule)
			}
			if err := poolFallback(u.pools.db.WithContext(ctx), rule.NodeID, rule.FallbackTag); err != nil {
				return err
			}
			var exists bool
			for _, selector := range rule.OutboundSelectors {
				if selector == poolPrefix(p.ID) {
					exists = true
				}
			}
			if p.AppliedRevision > 0 && !exists {
				return fmt.Errorf("%w: keep the managed pool selector", ErrInvalidRoutingRule)
			}
		}
		for _, selector := range rule.OutboundSelectors {
			if strings.HasPrefix(poolPrefix(p.ID), selector) && !(rule.ID == p.BalancerID && selector == poolPrefix(p.ID)) {
				return fmt.Errorf("%w: selector overlaps a managed pool", ErrInvalidRoutingRule)
			}
		}
	}
	return nil
}
