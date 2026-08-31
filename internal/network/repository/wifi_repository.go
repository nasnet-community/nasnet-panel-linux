package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

type WifiRepository interface {
	List(ctx context.Context) ([]domain.WifiConfig, error)
	// GetByInterface never returns nil: with no row it returns a default-disabled
	// config, so the UI has something to render and Reconcile sees a disabled AP.
	GetByInterface(ctx context.Context, interfaceID uint) (*domain.WifiConfig, error)
	Save(ctx context.Context, cfg *domain.WifiConfig) error
	// ReplaceAll swaps the whole table for the given set. A rollback restores the
	// captured intent with it, and an empty capture tears everything down.
	ReplaceAll(ctx context.Context, cfgs []domain.WifiConfig) error
}

type wifiRepository struct {
	db *gorm.DB
}

func NewWifiRepository(db *gorm.DB) WifiRepository {
	return &wifiRepository{db: db}
}

// DefaultWifiConfig is what an unconfigured radio shows, disabled until asked
func DefaultWifiConfig(interfaceID uint) domain.WifiConfig {
	return domain.WifiConfig{InterfaceID: interfaceID, Mode: "ap", Band: "2g"}
}

func (r *wifiRepository) List(ctx context.Context) ([]domain.WifiConfig, error) {
	var rows []domain.WifiConfig
	if err := r.db.WithContext(ctx).Order("interface_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *wifiRepository) GetByInterface(ctx context.Context, interfaceID uint) (*domain.WifiConfig, error) {
	var cfg domain.WifiConfig
	err := r.db.WithContext(ctx).Where("interface_id = ?", interfaceID).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		d := DefaultWifiConfig(interfaceID)
		return &d, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *wifiRepository) Save(ctx context.Context, cfg *domain.WifiConfig) error {
	if cfg.ID == 0 {
		// First save: an existing row wins, so a stale zero ID cannot duplicate it
		var existing domain.WifiConfig
		err := r.db.WithContext(ctx).Where("interface_id = ?", cfg.InterfaceID).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return r.db.WithContext(ctx).Create(cfg).Error
		case err != nil:
			return err
		}
		cfg.ID = existing.ID
	}
	// Go field names, not columns: GORM renames SSID to s_s_i_d. Select is what
	// makes false and 0 actually get written.
	return r.db.WithContext(ctx).Model(&domain.WifiConfig{}).Where("id = ?", cfg.ID).
		Select("Mode", "SSID", "PSK", "CountryCode", "Band", "Channel", "Hidden", "Enabled").
		Updates(cfg).Error
}

func (r *wifiRepository) ReplaceAll(ctx context.Context, cfgs []domain.WifiConfig) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Hard delete: a soft-deleted row would shadow the restored one on the
		// interface_id lookup.
		if err := tx.Unscoped().Where("1 = 1").Delete(&domain.WifiConfig{}).Error; err != nil {
			return err
		}
		for i := range cfgs {
			row := cfgs[i]
			row.ID = 0
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
