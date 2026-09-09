package repository

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/gorm"
)

// UpdateNodeLogSettings does not save the whole node: health checks and other
// operators may update unrelated fields while the agent applies a config.
func (r *nodeRepository) UpdateNodeLogSettings(ctx context.Context, id uint, settings domain.XrayLogSettings) error {
	fields := map[string]interface{}{}
	if settings.Level != nil {
		fields["log_level"] = *settings.Level
	}
	if settings.Access != nil {
		fields["log_access"] = *settings.Access
	}
	if settings.Error != nil {
		fields["log_error"] = *settings.Error
	}
	if settings.DNS != nil {
		fields["log_dns"] = *settings.DNS
	}
	if len(fields) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
