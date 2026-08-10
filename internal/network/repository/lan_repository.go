package repository

import (
	"context"
	"errors"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"gorm.io/gorm"
)

// LANRepository holds the single LAN row per node.
type LANRepository interface {
	// Get never returns nil: with no row it returns the defaults, so the UI has
	// something to render and Reconcile sees a disabled LAN.
	Get(ctx context.Context) (*domain.LANConfig, error)
	Save(ctx context.Context, cfg *domain.LANConfig) error
	// DisarmInputFirewall turns the firewall off after a revert: the dead-man
	// restores the kernel but not the intent, so it would come back armed.
	DisarmInputFirewall(ctx context.Context) error
}

type lanRepository struct {
	db *gorm.DB
}

func NewLANRepository(db *gorm.DB) LANRepository {
	return &lanRepository{db: db}
}

// DefaultLANConfig is what an unconfigured box shows, disabled until asked.
func DefaultLANConfig() domain.LANConfig {
	return domain.LANConfig{
		NodeID:     1,
		BridgeName: system.LANBridgeName,
		CIDR:       system.DefaultLANCIDR,
		// Low addresses stay free for hosts a port forward can target.
		DHCPRangeLow:  "10.77.0.100",
		DHCPRangeHigh: "10.77.0.200",
		LeaseHours:    12,
	}
}

func (r *lanRepository) Get(ctx context.Context) (*domain.LANConfig, error) {
	var cfg domain.LANConfig
	err := r.db.WithContext(ctx).Where("node_id = ?", 1).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		d := DefaultLANConfig()
		return &d, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *lanRepository) Save(ctx context.Context, cfg *domain.LANConfig) error {
	if cfg.NodeID == 0 {
		cfg.NodeID = 1
	}
	if cfg.ID == 0 {
		// First save: an existing row wins, so a stale zero ID cannot duplicate it.
		var existing domain.LANConfig
		err := r.db.WithContext(ctx).Where("node_id = ?", cfg.NodeID).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return r.db.WithContext(ctx).Create(cfg).Error
		case err != nil:
			return err
		}
		cfg.ID = existing.ID
	}
	// Go field names, not columns: GORM renames CIDR to c_id_r. Select is what
	// makes false and 0 actually get written.
	return r.db.WithContext(ctx).Model(&domain.LANConfig{}).Where("id = ?", cfg.ID).
		Select("BridgeName", "CIDR", "DHCPRangeLow", "DHCPRangeHigh",
			"LeaseHours", "Enabled", "InputFirewall").
		Updates(cfg).Error
}

func (r *lanRepository) DisarmInputFirewall(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&domain.LANConfig{}).
		Where("node_id = ?", 1).Update("input_firewall", false).Error
}
