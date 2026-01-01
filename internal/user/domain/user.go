package domain

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	TelegramID int64          `gorm:"uniqueIndex;not null" json:"telegram_id"`
	Username   string         `gorm:"size:255" json:"username"`
	FirstName  string         `gorm:"size:255" json:"first_name"`
	LastName   string         `gorm:"size:255" json:"last_name"`
	Language   string         `gorm:"size:10;default:'en'" json:"language"`
	IsAdmin    bool           `gorm:"default:false;index:idx_user_banned_admin,priority:2" json:"is_admin"`
	IsBanned   bool           `gorm:"default:false;index:idx_user_banned_admin,priority:1" json:"is_banned"`
	AdminNotes string         `gorm:"type:text" json:"admin_notes"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string {
	return "users"
}
