package domain

import (
	"time"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"gorm.io/gorm"
)

// AccountSource indicates how the account was created
type AccountSource string

const (
	AccountSourceSubscription  AccountSource = "subscription"
	AccountSourceManual        AccountSource = "manual"
	AccountSourceImport        AccountSource = "import"
	AccountSourceAdminBulk     AccountSource = "admin_bulk"
	AccountSourceAdminExcluded AccountSource = "admin_excluded"
)

// AccountStatus indicates the current state of the account
type AccountStatus string

const (
	AccountStatusActive           AccountStatus = "active"
	AccountStatusDisabled         AccountStatus = "disabled"
	AccountStatusExpired          AccountStatus = "expired"
	AccountStatusPendingProvision AccountStatus = "pending_provision"
)

// Account represents an Xray user credential on a specific inbound
type Account struct {
	ID        uint `gorm:"primaryKey" json:"id"`
	InboundID uint `gorm:"index;uniqueIndex:idx_email_inbound;not null" json:"inbound_id"`

	// Belongs To Relation
	Inbound *nodeDomain.Inbound `gorm:"foreignKey:InboundID" json:"inbound,omitempty"`

	// User credentials
	Email      string `gorm:"size:255;uniqueIndex:idx_email_inbound;not null" json:"email"`
	UUID       string `gorm:"size:100;not null" json:"uuid"`
	Flow       string `gorm:"size:50" json:"flow"`                      // VLESS flow (e.g., xtls-rprx-vision)
	Encryption string `gorm:"size:50;default:'none'" json:"encryption"` // VLESS encryption

	// Source tracking
	Source         AccountSource           `gorm:"size:20;not null" json:"source"`
	SubscriptionID *uint                   `gorm:"index" json:"subscription_id"` // Nullable FK
	Subscription   *subDomain.Subscription `gorm:"foreignKey:SubscriptionID" json:"subscription,omitempty"`

	// Status and limits
	Status         AccountStatus `gorm:"size:20;default:'active';index:idx_acct_status_deleted,priority:1" json:"status"`
	DataLimit      int64         `gorm:"default:0" json:"data_limit"` // 0 = unlimited
	DataUsed       int64         `gorm:"default:0" json:"data_used"`
	ExpiresAt      *time.Time    `json:"expires_at"`
	LastActivityAt *time.Time    `json:"last_activity_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_acct_status_deleted,priority:2" json:"-"`
}

func (Account) TableName() string {
	return "accounts"
}

// IsExpired checks if the account has passed its expiration date
func (a *Account) IsExpired() bool {
	if a.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*a.ExpiresAt)
}

// IsDataExhausted checks if the account has exceeded its data limit
func (a *Account) IsDataExhausted() bool {
	if a.DataLimit == 0 {
		return false // unlimited
	}
	return a.DataUsed >= a.DataLimit
}

// RemainingData returns the remaining data allowance
func (a *Account) RemainingData() int64 {
	if a.DataLimit == 0 {
		return -1 // unlimited
	}
	remaining := a.DataLimit - a.DataUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}
