package repository

import (
	"context"
	"errors"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// VPNRepository stores tunnel profiles. Slot uniqueness among enabled rows is
// a database constraint rather than a convention — see EnsureVPNPoolMigration.
type VPNRepository interface {
	List(ctx context.Context) ([]domain.VPNProfile, error)
	Get(ctx context.Context, id uint) (*domain.VPNProfile, error)
	Create(ctx context.Context, p *domain.VPNProfile) error
	// Update writes the name and the config. It never changes pool membership:
	// that goes through the apply pipeline.
	Update(ctx context.Context, p *domain.VPNProfile) error
	Delete(ctx context.Context, id uint) error
	// Enabled returns the pool, priority then id order.
	Enabled(ctx context.Context) ([]domain.VPNProfile, error)
	// SetEnabled allocates or frees the interface slot with the flag.
	SetEnabled(ctx context.Context, id uint, on bool) error
	SetRole(ctx context.Context, id uint, priority, weight int) error
	// SetPool is the rollback path: the enabled set becomes exactly want.
	SetPool(ctx context.Context, want []domain.VPNProfile) error

	// Retired by the pool rework; deleted once the last caller goes.
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

// EnsureVPNPoolMigration retires the single-active model. Idempotent: the
// active flag is cleared as it converts, so a later disable sticks.
func EnsureVPNPoolMigration(db *gorm.DB) error {
	if err := db.Exec(`DROP INDEX IF EXISTS ux_vpn_profile_active`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_vpn_wg_slot
		  ON vpn_profiles (wg_slot)
		  WHERE wg_slot IS NOT NULL AND deleted_at IS NULL`).Error; err != nil {
		return err
	}
	return db.Exec(`
		UPDATE vpn_profiles SET enabled = true, wg_slot = 0, weight = 1,
		  priority = 0, active = false
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
	if p.Enabled {
		return domain.ErrProfileActive
	}
	return r.db.WithContext(ctx).Delete(&domain.VPNProfile{}, id).Error
}

func (r *vpnRepository) Enabled(ctx context.Context) ([]domain.VPNProfile, error) {
	var rows []domain.VPNProfile
	err := r.db.WithContext(ctx).Where("enabled").Order("priority, id").Find(&rows).Error
	return rows, err
}

// SetEnabled allocates the lowest free interface slot inside the same tx, so
// two concurrent enables cannot pick the same one.
func (r *vpnRepository) SetEnabled(ctx context.Context, id uint, on bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p domain.VPNProfile
		if err := tx.First(&p, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrProfileNotFound
			}
			return err
		}
		if !on {
			return tx.Model(&domain.VPNProfile{}).Where("id = ?", id).
				Updates(map[string]any{"enabled": false, "wg_slot": nil}).Error
		}
		if p.Enabled {
			return nil
		}
		var used []int
		if err := tx.Model(&domain.VPNProfile{}).Where("enabled AND wg_slot IS NOT NULL").
			Pluck("wg_slot", &used).Error; err != nil {
			return err
		}
		taken := map[int]bool{}
		for _, s := range used {
			taken[s] = true
		}
		slot := -1
		for s := 0; s < domain.MaxEnabledProfiles; s++ {
			if !taken[s] {
				slot = s
				break
			}
		}
		if slot == -1 {
			return domain.ErrPoolFull
		}
		return tx.Model(&domain.VPNProfile{}).Where("id = ?", id).
			Updates(map[string]any{"enabled": true, "wg_slot": slot}).Error
	})
}

func (r *vpnRepository) SetRole(ctx context.Context, id uint, priority, weight int) error {
	if err := domain.ValidatePoolRole(priority, weight); err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Model(&domain.VPNProfile{}).Where("id = ?", id).
		Updates(map[string]any{"priority": priority, "weight": weight})
	if res.Error == nil && res.RowsAffected == 0 {
		return domain.ErrProfileNotFound
	}
	return res.Error
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

func (r *vpnRepository) SetPool(ctx context.Context, want []domain.VPNProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.VPNProfile{}).Where("enabled").
			Updates(map[string]any{"enabled": false, "wg_slot": nil}).Error; err != nil {
			return err
		}
		for i := range want {
			p := want[i]
			if err := tx.Model(&domain.VPNProfile{}).Where("id = ?", p.ID).Updates(map[string]any{
				"enabled": true, "wg_slot": p.WGSlot,
				"priority": p.Priority, "weight": p.Weight, "config": p.Config,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
