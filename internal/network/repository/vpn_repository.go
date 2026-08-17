package repository

import (
	"context"
	"errors"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// VPNRepository stores tunnel profiles. At most one is active at a time, and
// that is a database constraint rather than a convention — see
// EnsureVPNProfileIndex.
type VPNRepository interface {
	List(ctx context.Context) ([]domain.VPNProfile, error)
	Get(ctx context.Context, id uint) (*domain.VPNProfile, error)
	Create(ctx context.Context, p *domain.VPNProfile) error
	// Update writes the name and the config. It never changes which profile is
	// active: that goes through the apply pipeline.
	Update(ctx context.Context, p *domain.VPNProfile) error
	Delete(ctx context.Context, id uint) error
	// Active returns nil, nil when no profile is active.
	Active(ctx context.Context) (*domain.VPNProfile, error)
	SetActive(ctx context.Context, id uint) error
	ClearActive(ctx context.Context) error
}

type vpnRepository struct {
	db *gorm.DB
}

func NewVPNRepository(db *gorm.DB) VPNRepository {
	return &vpnRepository{db: db}
}

// EnsureVPNProfileIndex enforces "one active profile" in the database. Indexing
// active rather than a natural key is deliberate: a deleted profile then holds
// nothing, so it can never block a later one.
func EnsureVPNProfileIndex(db *gorm.DB) error {
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_vpn_profile_active
		  ON vpn_profiles (active)
		  WHERE active AND deleted_at IS NULL`).Error
}

func (r *vpnRepository) List(ctx context.Context) ([]domain.VPNProfile, error) {
	var rows []domain.VPNProfile
	err := r.db.WithContext(ctx).Order("id").Find(&rows).Error
	return rows, err
}

func (r *vpnRepository) Get(ctx context.Context, id uint) (*domain.VPNProfile, error) {
	var p domain.VPNProfile
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrProfileNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *vpnRepository) Create(ctx context.Context, p *domain.VPNProfile) error {
	if p.NodeID == 0 {
		p.NodeID = 1
	}
	if p.Type == "" {
		p.Type = domain.VPNTypeWireGuard
	}
	// Activation is a routing change, so it never rides on a create.
	p.Active = false
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *vpnRepository) Update(ctx context.Context, p *domain.VPNProfile) error {
	if p.ID == 0 {
		return errors.New("no profile ID given")
	}
	// Select writes zero values too, so an empty config would erase the keys.
	if p.Config == "" {
		return errors.New("refusing to store a profile with no config")
	}
	return r.db.WithContext(ctx).Model(&domain.VPNProfile{}).Where("id = ?", p.ID).
		Select("Name", "Type", "Config").Updates(p).Error
}

func (r *vpnRepository) Delete(ctx context.Context, id uint) error {
	p, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	// Deleting the row under a live tunnel leaves nothing to turn it off with.
	if p.Active {
		return domain.ErrProfileActive
	}
	return r.db.WithContext(ctx).Delete(&domain.VPNProfile{}, id).Error
}

func (r *vpnRepository) Active(ctx context.Context) (*domain.VPNProfile, error) {
	var p domain.VPNProfile
	err := r.db.WithContext(ctx).Where("active").First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetActive clears and sets in one transaction: the unique index rejects the
// instant two rows are active, so the two statements cannot be separated.
func (r *vpnRepository) SetActive(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p domain.VPNProfile
		if err := tx.First(&p, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&domain.VPNProfile{}).
			Where("active").Update("active", false).Error; err != nil {
			return err
		}
		return tx.Model(&domain.VPNProfile{}).
			Where("id = ?", id).Update("active", true).Error
	})
}

func (r *vpnRepository) ClearActive(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&domain.VPNProfile{}).
		Where("active").Update("active", false).Error
}
