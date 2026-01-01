package adapters

import (
	"context"
	"time"

	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"gorm.io/gorm"
)

// SettingAdapter wraps settingDomain.SettingUsecase to match mntUC.SettingIO.
type SettingAdapter struct {
	UC settingDomain.SettingUsecase
}

func (a *SettingAdapter) GetByKey(ctx context.Context, key string) (string, error) {
	return a.UC.GetByKey(ctx, key)
}

func (a *SettingAdapter) UpdateMany(ctx context.Context, pairs []*mntUC.SettingPair) error {
	settings := make([]*settingDomain.Setting, 0, len(pairs))
	for _, p := range pairs {
		settings = append(settings, &settingDomain.Setting{Key: p.Key, Value: p.Value})
	}
	return a.UC.UpdateMany(ctx, settings)
}

// NodeAdapter reads/writes maintenance columns on the nodes table directly via GORM.
type NodeAdapter struct {
	DB *gorm.DB
}

func (a *NodeAdapter) GetNodeMaintenance(ctx context.Context, id uint) (bool, string, *time.Time, error) {
	var n nodeDomain.Node
	if err := a.DB.WithContext(ctx).
		Select("maintenance_mode", "maintenance_message", "maintenance_since").
		Where("id = ?", id).
		First(&n).Error; err != nil {
		return false, "", nil, err
	}
	return n.MaintenanceMode, n.MaintenanceMessage, n.MaintenanceSince, nil
}

func (a *NodeAdapter) SetNodeMaintenance(ctx context.Context, id uint, active bool, message string, since *time.Time) error {
	return a.DB.WithContext(ctx).
		Model(&nodeDomain.Node{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"maintenance_mode":    active,
			"maintenance_message": message,
			"maintenance_since":   since,
		}).Error
}

// SubAdapter reads/writes maintenance columns on subscriptions, plus looks up
// linked node IDs via the subscription's created accounts
type SubAdapter struct {
	DB *gorm.DB
}

func (a *SubAdapter) GetSubMaintenance(ctx context.Context, id uint) (bool, string, *time.Time, error) {
	var s subDomain.Subscription
	if err := a.DB.WithContext(ctx).
		Select("maintenance_mode", "maintenance_message", "maintenance_since").
		Where("id = ?", id).
		First(&s).Error; err != nil {
		return false, "", nil, err
	}
	return s.MaintenanceMode, s.MaintenanceMessage, s.MaintenanceSince, nil
}

func (a *SubAdapter) SetSubMaintenance(ctx context.Context, id uint, active bool, message string, since *time.Time) error {
	return a.DB.WithContext(ctx).
		Model(&subDomain.Subscription{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"maintenance_mode":    active,
			"maintenance_message": message,
			"maintenance_since":   since,
		}).Error
}

// GetSubLinkedNodeIDs returns the distinct set of node IDs linked to a
// subscription via the inbounds of its provisioned accounts.
func (a *SubAdapter) GetSubLinkedNodeIDs(ctx context.Context, id uint) ([]uint, error) {
	var rows []struct {
		NodeID uint
	}
	err := a.DB.WithContext(ctx).Raw(`
		SELECT DISTINCT i.node_id AS node_id
		FROM accounts a
		JOIN inbounds i ON i.id = a.inbound_id
		WHERE a.subscription_id = ? AND a.deleted_at IS NULL
	`, id).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]uint, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.NodeID)
	}
	return out, nil
}
