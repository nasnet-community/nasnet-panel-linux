package domain

import (
	"context"
	"time"
)

// AuditAction constants for all admin operations
type AuditAction string

const (
	// User actions
	AuditUserBan         AuditAction = "user.ban"
	AuditUserUnban       AuditAction = "user.unban"
	AuditUserToggleAdmin AuditAction = "user.toggle_admin"
	AuditUserUpdateNotes AuditAction = "user.update_notes"

	// Subscription actions
	AuditSubExtend     AuditAction = "subscription.extend"
	AuditSubRevoke     AuditAction = "subscription.revoke"
	AuditSubPause      AuditAction = "subscription.pause"
	AuditSubResume     AuditAction = "subscription.resume"
	AuditSubSetData    AuditAction = "subscription.set_data"
	AuditSubAddData    AuditAction = "subscription.add_data"
	AuditSubResetData  AuditAction = "subscription.reset_data"
	AuditSubSetExpiry  AuditAction = "subscription.set_expiry"
	AuditSubRegenKey   AuditAction = "subscription.regen_key"
	AuditSubRename     AuditAction = "subscription.rename"
	AuditSubDelete     AuditAction = "subscription.delete"
	AuditSubBulk       AuditAction = "subscription.bulk"
	AuditSubSetDataLim AuditAction = "subscription.set_data_limit"
	// AuditSubViewAccessHistory: admin requested per-subscription request log.
	// Logged because access-history reveals destinations the user contacted —
	// admins should be accountable for these queries.
	AuditSubViewAccessHistory      AuditAction = "subscription.view_access_history"
	AuditSubSearchAccessHistory    AuditAction = "subscription.search_access_history"
	AuditAccessHistoryGlobalSearch AuditAction = "access_history.global_search"

	// Node actions
	AuditNodeCreate AuditAction = "node.create"
	AuditNodeUpdate AuditAction = "node.update"
	AuditNodeDelete AuditAction = "node.delete"
	AuditNodeWipe   AuditAction = "node.wipe"
	AuditNodeNuke   AuditAction = "node.nuke"

	// Xray binary actions
	AuditXrayBinaryUpload AuditAction = "xray.binary_upload"
	AuditXrayBinaryDelete AuditAction = "xray.binary_delete"

	// Settings actions
	AuditSettingsUpdate AuditAction = "settings.update"
	AuditPasswordChange AuditAction = "auth.password_change"
	AuditAdminLogin     AuditAction = "auth.admin_login"

	// Data management
	AuditDataExport AuditAction = "data.export"
	AuditDataBackup AuditAction = "data.backup"

	// Backup & Restore actions
	AuditBackupDelete    AuditAction = "backup.delete"
	AuditBackupDownload  AuditAction = "backup.download"
	AuditRestoreStart    AuditAction = "restore.start"
	AuditRestoreComplete AuditAction = "restore.complete"
	AuditRestoreFailed   AuditAction = "restore.failed"

	// Database management
	AuditDatabaseCleanup AuditAction = "database.cleanup"

	// Server management
	AuditServerRestart AuditAction = "server.restart"
)

// AuditLog represents an audit log entry
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Action     string    `gorm:"size:100;index" json:"action"`
	ActorID    uint      `gorm:"index" json:"actor_id"`
	ActorName  string    `gorm:"size:255" json:"actor_name"`
	EntityType string    `gorm:"size:50;index" json:"entity_type"`
	EntityID   uint      `gorm:"index" json:"entity_id"`
	OldValues  string    `gorm:"type:text" json:"old_values,omitempty"`
	NewValues  string    `gorm:"type:text" json:"new_values,omitempty"`
	IPAddress  string    `gorm:"size:45" json:"ip_address"`
	RequestID  string    `gorm:"size:36;index" json:"request_id"`
	Source     string    `gorm:"size:20" json:"source"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (AuditLog) TableName() string {
	return "audit_logs"
}

// AuditListFilters holds filtering parameters for listing audit logs
type AuditListFilters struct {
	Action     string
	ActorID    uint
	EntityType string
	EntityID   uint
	DateFrom   *time.Time
	DateTo     *time.Time
	Offset     int
	Limit      int
}

// AuditLogRepository defines the repository interface
type AuditLogRepository interface {
	Create(ctx context.Context, entry *AuditLog) error
	List(ctx context.Context, filters AuditListFilters) ([]*AuditLog, int64, error)
	CleanupOlderThan(ctx context.Context, days int) (int64, error)
}

// AuditLogUsecase defines the usecase interface
type AuditLogUsecase interface {
	Log(ctx context.Context, entry *AuditLog)
	List(ctx context.Context, filters AuditListFilters) ([]*AuditLog, int64, error)
	Cleanup(ctx context.Context, days int) (int64, error)
	Stop()
}
