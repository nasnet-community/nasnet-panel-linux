package domain

import "time"

// UsedDeploymentToken tracks deployment tokens that have already been consumed.
// Tokens are one-time use: once the install script is downloaded, the JTI is recorded
// so the same token cannot be replayed.
type UsedDeploymentToken struct {
	ID        uint      `gorm:"primaryKey"`
	JTI       string    `gorm:"size:100;uniqueIndex;not null"`
	NodeID    uint      `gorm:"not null"`
	UsedAt    time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
}
