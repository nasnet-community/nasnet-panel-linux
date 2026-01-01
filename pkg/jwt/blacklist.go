package jwt

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// RevokedToken stores a revoked JWT token ID for the blacklist.
type RevokedToken struct {
	ID        uint      `gorm:"primaryKey"`
	TokenID   string    `gorm:"size:36;uniqueIndex;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	RevokedAt time.Time `gorm:"not null"`
}

func (RevokedToken) TableName() string {
	return "revoked_tokens"
}

// Blacklist manages revoked tokens via database.
type Blacklist struct {
	db *gorm.DB
}

func NewBlacklist(db *gorm.DB) *Blacklist {
	return &Blacklist{db: db}
}

func (bl *Blacklist) Revoke(ctx context.Context, tokenID string, expiresAt time.Time) error {
	return bl.db.WithContext(ctx).Create(&RevokedToken{
		TokenID:   tokenID,
		ExpiresAt: expiresAt,
		RevokedAt: time.Now(),
	}).Error
}

func (bl *Blacklist) IsRevoked(ctx context.Context, tokenID string) bool {
	var count int64
	bl.db.WithContext(ctx).Model(&RevokedToken{}).Where("token_id = ?", tokenID).Count(&count)
	return count > 0
}

func (bl *Blacklist) Cleanup(ctx context.Context) (int64, error) {
	result := bl.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&RevokedToken{})
	return result.RowsAffected, result.Error
}
