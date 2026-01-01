package domain

import (
	"time"

	"gorm.io/gorm"
)

type TaskType string
type TaskStatus string

const (
	TypeAddUser    TaskType = "ADD_USER"
	TypeRemoveUser TaskType = "REMOVE_USER"

	StatusPending    TaskStatus = "PENDING"
	StatusProcessing TaskStatus = "PROCESSING"
	StatusFailed     TaskStatus = "FAILED"
	StatusCompleted  TaskStatus = "COMPLETED"
	StatusDead       TaskStatus = "DEAD" // Max retries reached
)

// ProvisioningTask represents an async operation to be performed on an Xray node
type ProvisioningTask struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	AccountID uint       `gorm:"index" json:"account_id"` // Link to the Account table
	NodeID    uint       `gorm:"index" json:"node_id"`    // Target Node
	Type      TaskType   `gorm:"size:20;not null" json:"type"`
	Status    TaskStatus `gorm:"size:20;default:'PENDING';index" json:"status"`

	// Payload snapshot (Critical: ensure we have data even if Account/User is deleted from DB)
	TargetAddress  string `gorm:"size:100;not null" json:"target_address"` // IP:Port of Xray API
	InboundTag     string `gorm:"size:100;not null" json:"inbound_tag"`
	UserEmail      string `gorm:"size:255;not null" json:"user_email"`
	UserUUID       string `gorm:"size:100" json:"user_uuid"`
	UserFlow       string `gorm:"size:100" json:"user_flow"`
	UserEncryption string `gorm:"size:100" json:"user_encryption"`
	UserLevel      int    `gorm:"default:0" json:"user_level"`
	Protocol       string `gorm:"size:20" json:"protocol"` // vless, vmess, etc.

	// Retry Logic
	RetryCount  int       `gorm:"default:0" json:"retry_count"`
	NextRetryAt time.Time `gorm:"index;default:CURRENT_TIMESTAMP" json:"next_retry_at"`
	LastError   string    `gorm:"type:text" json:"last_error"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProvisioningTask) TableName() string {
	return "provisioning_tasks"
}
