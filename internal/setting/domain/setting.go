package domain

import (
	"context"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
)

type Setting struct {
	Key             string `gorm:"primaryKey" json:"key"`
	Value           string `json:"value"`
	Type            string `json:"type"`     // string, int, bool, json
	Category        string `json:"category"` // general, xray, telegram
	Description     string `json:"description"`
	Label           string `json:"label"`
	Sensitive       bool   `gorm:"default:false" json:"sensitive,omitempty"`
	RequiresRestart bool   `gorm:"default:false" json:"requires_restart,omitempty"`
}

type SettingRepository interface {
	GetAll(ctx context.Context) ([]*Setting, error)
	GetByKey(ctx context.Context, key string) (*Setting, error)
	Update(ctx context.Context, setting *Setting) error
	UpdateMany(ctx context.Context, settings []*Setting) error
}

type SettingUsecase interface {
	GetAll(ctx context.Context) (map[string][]*Setting, error) // Grouped by category
	UpdateMany(ctx context.Context, settings []*Setting) error
	GetByKey(ctx context.Context, key string) (string, error) // Helper to get value directly
	SeedDefaults(ctx context.Context) error                   // Seed default settings
	SetOnXrayVersionChange(fn func(string))
	SetOnMaintenanceChange(fn func())
	SetOnOutboundProxyChange(fn func(proxyURL string, enabled map[string]bool))
	SetOnRouterHealthChange(fn func())
	SetAuditUsecase(auc auditDomain.AuditLogUsecase)
	ReseedEnvSettings(ctx context.Context) error
	MigrateGlobalPanelPassword(ctx context.Context)
}
