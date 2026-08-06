package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// ApplyRepository persists the two-phase apply audit trail.
type ApplyRepository interface {
	Create(ctx context.Context, rec *domain.ApplyRecord) error
	Latest(ctx context.Context) (*domain.ApplyRecord, error)
	LatestConfirmed(ctx context.Context) (*domain.ApplyRecord, error)
	SetPhase(ctx context.Context, id uint, phase domain.ApplyPhase, errMsg string) error
}

type applyRepository struct {
	db *gorm.DB
}

func NewApplyRepository(db *gorm.DB) ApplyRepository {
	return &applyRepository{db: db}
}

func (r *applyRepository) Create(ctx context.Context, rec *domain.ApplyRecord) error {
	if rec.NodeID == 0 {
		rec.NodeID = 1
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *applyRepository) Latest(ctx context.Context) (*domain.ApplyRecord, error) {
	var rec domain.ApplyRecord
	if err := r.db.WithContext(ctx).Order("id DESC").First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// LatestConfirmed is what preflight reads to decide whether an unmasked
// NetworkManager is fatal or a finish-setup banner.
func (r *applyRepository) LatestConfirmed(ctx context.Context) (*domain.ApplyRecord, error) {
	var rec domain.ApplyRecord
	err := r.db.WithContext(ctx).Where("phase = ?", domain.PhaseConfirmed).
		Order("id DESC").First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *applyRepository) SetPhase(ctx context.Context, id uint, phase domain.ApplyPhase, errMsg string) error {
	return r.db.WithContext(ctx).Model(&domain.ApplyRecord{}).Where("id = ?", id).
		Updates(map[string]any{"phase": phase, "error": errMsg}).Error
}
