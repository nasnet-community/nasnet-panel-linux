package repository

import (
	"context"
	"errors"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeviceLabelRepository persists operator-assigned device names. The only
// stored device state — the rest of the list is derived per request.
type DeviceLabelRepository interface {
	// ByMAC returns every label keyed by canonical MAC.
	ByMAC(ctx context.Context) (map[string]string, error)
	// Set writes a label, or removes it when label is empty.
	Set(ctx context.Context, mac, label string) error
}

type deviceLabelRepository struct {
	db *gorm.DB
}

func NewDeviceLabelRepository(db *gorm.DB) DeviceLabelRepository {
	return &deviceLabelRepository{db: db}
}

func (r *deviceLabelRepository) ByMAC(ctx context.Context) (map[string]string, error) {
	var rows []domain.LANDeviceLabel
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.MAC] = row.Label
	}
	return out, nil
}

// Set upserts on the MAC. Callers pass an already-normalized MAC; the unique
// index is on that canonical form, so an unnormalized one would slip past it.
func (r *deviceLabelRepository) Set(ctx context.Context, mac, label string) error {
	if mac == "" {
		return errors.New("no MAC given")
	}
	if label == "" {
		return r.db.WithContext(ctx).
			Where("mac = ?", mac).Delete(&domain.LANDeviceLabel{}).Error
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mac"}},
		DoUpdates: clause.AssignmentColumns([]string{"label", "updated_at"}),
	}).Create(&domain.LANDeviceLabel{NodeID: 1, MAC: mac, Label: label}).Error
}
