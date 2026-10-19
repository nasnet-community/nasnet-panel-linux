package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

func (r *nodeRepository) CreateReverseTunnel(ctx context.Context, t *domain.ReverseTunnel) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *nodeRepository) GetReverseTunnel(ctx context.Context, id uint) (*domain.ReverseTunnel, error) {
	var t domain.ReverseTunnel
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateReverseTunnel writes the settings an operator can change. Status and
// traffic have their own writers, so a save never rolls them back.
func (r *nodeRepository) UpdateReverseTunnel(ctx context.Context, t *domain.ReverseTunnel) error {
	return r.db.WithContext(ctx).Model(t).
		Select("name", "portal_inbound_id", "portal_host_id", "portal_tag", "inbound_tags", "rule_id",
			"bridge_tag", "exit_outbound_tag", "exit_balancer_tag", "uuid", "portal_link").
		Updates(t).Error
}

func (r *nodeRepository) DeleteReverseTunnel(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.ReverseTunnel{}, id).Error
}

func (r *nodeRepository) ListReverseTunnels(ctx context.Context) ([]*domain.ReverseTunnel, error) {
	var tunnels []*domain.ReverseTunnel
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&tunnels).Error; err != nil {
		return nil, err
	}
	return tunnels, nil
}

func (r *nodeRepository) ListReverseTunnelsByNode(ctx context.Context, nodeID uint) ([]*domain.ReverseTunnel, error) {
	var tunnels []*domain.ReverseTunnel
	if err := r.db.WithContext(ctx).
		Where("portal_node_id = ? OR bridge_node_id = ?", nodeID, nodeID).
		Order("id ASC").
		Find(&tunnels).Error; err != nil {
		return nil, err
	}
	return tunnels, nil
}

func (r *nodeRepository) UpdateReverseTunnelState(ctx context.Context, t *domain.ReverseTunnel) error {
	return r.db.WithContext(ctx).Model(&domain.ReverseTunnel{}).Where("id = ?", t.ID).
		Updates(map[string]interface{}{
			"state":        t.State,
			"state_since":  t.StateSince,
			"bridge_ip":    t.BridgeIP,
			"last_seen_at": t.LastSeenAt,
		}).Error
}

func (r *nodeRepository) AddReverseTunnelTraffic(ctx context.Context, id uint, toBridge, fromBridge int64) error {
	return database.GetExecutor(r.db, ctx).Model(&domain.ReverseTunnel{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"to_bridge":   gorm.Expr("to_bridge + ?", toBridge),
			"from_bridge": gorm.Expr("from_bridge + ?", fromBridge),
		}).Error
}
