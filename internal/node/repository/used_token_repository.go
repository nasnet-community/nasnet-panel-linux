package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/gorm"
)

// UsedTokenRepository tracks consumed deployment tokens to prevent replay.
type UsedTokenRepository interface {
	MarkUsed(ctx context.Context, jti string, nodeID uint, expiresAt time.Time) error
	IsUsed(ctx context.Context, jti string) (bool, error)
	CleanupExpired(ctx context.Context) error
}

type usedTokenRepository struct {
	db *gorm.DB
}

// NewUsedTokenRepository creates a new used token repository.
func NewUsedTokenRepository(db *gorm.DB) UsedTokenRepository {
	return &usedTokenRepository{db: db}
}

func (r *usedTokenRepository) MarkUsed(ctx context.Context, jti string, nodeID uint, expiresAt time.Time) error {
	token := &domain.UsedDeploymentToken{
		JTI:       jti,
		NodeID:    nodeID,
		UsedAt:    time.Now(),
		ExpiresAt: expiresAt,
	}
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *usedTokenRepository) IsUsed(ctx context.Context, jti string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.UsedDeploymentToken{}).
		Where("jti = ?", jti).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *usedTokenRepository) CleanupExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&domain.UsedDeploymentToken{}).Error
}
