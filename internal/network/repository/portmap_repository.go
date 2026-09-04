package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// PortMapRepository persists the operator's upstream mapping rules.
type PortMapRepository interface {
	List(ctx context.Context) ([]domain.PortMapRule, error)
	Create(ctx context.Context, r *domain.PortMapRule) error
	Update(ctx context.Context, r *domain.PortMapRule) error
	Delete(ctx context.Context, id uint) error
}

type portMapRepository struct {
	db *gorm.DB
}

func NewPortMapRepository(db *gorm.DB) PortMapRepository {
	return &portMapRepository{db: db}
}

// Ordered so the reconciler walks the rules the same way across restarts.
func (r *portMapRepository) List(ctx context.Context) ([]domain.PortMapRule, error) {
	var out []domain.PortMapRule
	err := r.db.WithContext(ctx).Order("proto ASC, port ASC, id ASC").Find(&out).Error
	return out, err
}

func (r *portMapRepository) Create(ctx context.Context, row *domain.PortMapRule) error {
	if row.NodeID == 0 {
		row.NodeID = 1
	}
	// Named fields, or GORM swaps a false Enabled for the column default.
	return r.db.WithContext(ctx).Select("NodeID", "UplinkKey", "Proto", "Port", "ExternalHint", "Comment", "Enabled").Create(row).Error
}

func (r *portMapRepository) Update(ctx context.Context, row *domain.PortMapRule) error {
	// Go field names so GORM maps the columns, and so `false` gets written.
	return r.db.WithContext(ctx).Model(&domain.PortMapRule{}).Where("id = ?", row.ID).
		Select("UplinkKey", "Proto", "Port", "ExternalHint", "Comment", "Enabled").
		Updates(row).Error
}

func (r *portMapRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.PortMapRule{}, id).Error
}
