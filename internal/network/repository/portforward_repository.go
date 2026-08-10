package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// PortForwardRepository persists the DNAT rows the nat_pre chain is rendered from.
type PortForwardRepository interface {
	List(ctx context.Context) ([]domain.PortForward, error)
	Get(ctx context.Context, id uint) (*domain.PortForward, error)
	Create(ctx context.Context, pf *domain.PortForward) error
	Update(ctx context.Context, pf *domain.PortForward) error
	Delete(ctx context.Context, id uint) error
}

type portForwardRepository struct {
	db *gorm.DB
}

func NewPortForwardRepository(db *gorm.DB) PortForwardRepository {
	return &portForwardRepository{db: db}
}

// Ordered so the rendered chain is stable across restarts. GORM names DPort
// d_port, hence the column name.
func (r *portForwardRepository) List(ctx context.Context) ([]domain.PortForward, error) {
	var out []domain.PortForward
	err := r.db.WithContext(ctx).Order("proto ASC, d_port ASC, id ASC").Find(&out).Error
	return out, err
}

func (r *portForwardRepository) Get(ctx context.Context, id uint) (*domain.PortForward, error) {
	var pf domain.PortForward
	if err := r.db.WithContext(ctx).First(&pf, id).Error; err != nil {
		return nil, err
	}
	return &pf, nil
}

func (r *portForwardRepository) Create(ctx context.Context, pf *domain.PortForward) error {
	if pf.NodeID == 0 {
		pf.NodeID = 1
	}
	return r.db.WithContext(ctx).Create(pf).Error
}

func (r *portForwardRepository) Update(ctx context.Context, pf *domain.PortForward) error {
	// Go field names so GORM maps the columns, and so `false` gets written.
	return r.db.WithContext(ctx).Model(&domain.PortForward{}).Where("id = ?", pf.ID).
		Select("UplinkKey", "Proto", "DPort", "ToAddr", "ToPort", "Comment", "Enabled").
		Updates(pf).Error
}

func (r *portForwardRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.PortForward{}, id).Error
}
