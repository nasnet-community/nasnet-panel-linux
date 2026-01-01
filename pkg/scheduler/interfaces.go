package scheduler

import (
	"context"
	"time"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	notifDomain "github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
	sniDomain "github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
)

// Domain type aliases re-exported so task_*.go can skip internal/ imports.

// NotificationType mirrors the notification domain type.
type NotificationType = notifDomain.NotificationType

// NotificationLog mirrors the notification domain struct.
type NotificationLog = notifDomain.NotificationLog

// UserDailyUsage mirrors the user domain struct.
type UserDailyUsage = userDomain.UserDailyUsage

// ReconcileStats is a type alias for the subscription usecase reconcile result.
type ReconcileStats = subUC.ReconcileStats

// SyncResult is a type alias for the node usecase sync result.
type SyncResult = nodeUC.SyncResult

// Notification type constants re-exported for task files.
const (
	NotificationTypeExpired       = notifDomain.NotificationTypeExpired
	NotificationTypeExpiry1Day    = notifDomain.NotificationTypeExpiry1Day
	NotificationTypeExpiry3Days   = notifDomain.NotificationTypeExpiry3Days
	NotificationTypeExpiry7Days   = notifDomain.NotificationTypeExpiry7Days
	NotificationTypeDataExhausted = notifDomain.NotificationTypeDataExhausted
)

// Subscription status string constants for use in scheduler task files.
const (
	SubStatusActive           = string(subDomain.SubscriptionStatusActive)
	SubStatusTrafficExhausted = string(subDomain.SubscriptionStatusTrafficExhausted)
)

// ---------------------------------------------------------------------------
// Dependency interfaces — only the methods the scheduler actually calls.
// ---------------------------------------------------------------------------

// SubscriptionUsecase defines what the scheduler needs from the subscription
// business logic layer.
type SubscriptionUsecase interface {
	CheckAndExpireSubscriptions(ctx context.Context) error
	CheckAndExpireByDataLimit(ctx context.Context) error
	ReconcileUsers(ctx context.Context) (*ReconcileStats, error)
	ListAllSubscriptions(ctx context.Context, status string, offset, limit int) ([]*subDomain.Subscription, error)
	CheckAndSendDataWarnings(ctx context.Context) ([]*subDomain.Subscription, error)
}

// NodeUsecase defines what the scheduler needs from the node business logic layer.
type NodeUsecase interface {
	ListNodes(ctx context.Context) ([]*nodeDomain.Node, error)
	SyncInbounds(ctx context.Context, nodeID uint) (*SyncResult, error)
	SyncNodeStats(ctx context.Context) error
	StartHeartbeats(ctx context.Context)
	StopHeartbeats()
}

// SNIUsecase defines what the scheduler needs from the SNI (certificate) layer.
type SNIUsecase interface {
	GetExpiringCertificates(ctx context.Context, days int) ([]*sniDomain.SNI, error)
	RenewCertificate(ctx context.Context, id uint) error
	MarkExpiryNotified(ctx context.Context, id uint, level int) error
}

// NotificationRepository defines what the scheduler needs from notification
// persistence. Legacy CleanupOldNotifications is kept for the reconcile-loop
// hot path; the retention sweep uses the same method through a settings-driven
// adapter so the behaviour stays the same but becomes UI-configurable.
type NotificationRepository interface {
	HasSentNotification(ctx context.Context, subscriptionID uint, notifType notifDomain.NotificationType) (bool, error)
	Create(ctx context.Context, log *notifDomain.NotificationLog) error
	CleanupOldNotifications(ctx context.Context, olderThanDays int) error
}

// UserRepository defines what the scheduler needs from user persistence.
type UserRepository interface {
	FindByID(ctx context.Context, id uint) (*userDomain.User, error)
}

// SettingUsecase defines what the scheduler needs from the settings layer.
type SettingUsecase interface {
	GetByKey(ctx context.Context, key string) (string, error)
}

// NodeRepository defines what the scheduler needs from node persistence
// (retention cleanup).
type NodeRepository interface {
	CleanupOldNodeStats(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldAccessLogSummaries(ctx context.Context, before time.Time) (int64, error)
	CleanupOldNodeDailyTraffic(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldUptimeEvents(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldStarlinkStats(ctx context.Context, olderThanDays int) (int64, error)
}

// ProvisioningRepository defines what the scheduler needs from provisioning
// persistence (retention cleanup).
type ProvisioningRepository interface {
	CleanupCompletedTasks(ctx context.Context, olderThanDays int) (int64, error)
}

// AuditLogUsecase defines what the scheduler needs from the audit log layer
// (retention cleanup).
type AuditLogUsecase interface {
	Cleanup(ctx context.Context, days int) (int64, error)
}

// UserDailyUsageRepository defines what the scheduler needs from daily usage
// persistence. DeleteOlderThan takes a cutoff timestamp rather than a day
// count so the existing repo method signature is usable unchanged.
type UserDailyUsageRepository interface {
	Upsert(ctx context.Context, entry *userDomain.UserDailyUsage) error
	DeleteOlderThan(ctx context.Context, before time.Time) error
}

// SubscriptionIPRepository defines what the scheduler needs from subscription
// IP persistence (cleanup).
type SubscriptionIPRepository interface {
	DeleteOldSubscriptionIPs(ctx context.Context, olderThan time.Time) (int64, error)
}

// OnlineUsersSnapshotRecorder writes one global online-user snapshot.
// Implemented by internal/admin/usecase.AdminUsecase.
type OnlineUsersSnapshotRecorder interface {
	RecordOnlineUsersSnapshot(ctx context.Context) error
}

// OnlineUsersSnapshotCleaner prunes old global online-user snapshots.
// Implemented by internal/admin/repository.OnlineUsersSnapshotRepository.
type OnlineUsersSnapshotCleaner interface {
	CleanupOlderThan(ctx context.Context, olderThanDays int) (int64, error)
}

// AlertEventCleaner prunes alerting Event rows older than the retention
// window. Implemented by internal/alerting/repository.AlertRepository.
type AlertEventCleaner interface {
	CleanupOldEvents(ctx context.Context, olderThanDays int) (int64, error)
}

// SubscriptionDailyUsageCleaner prunes SubscriptionDailyUsage rows older
// than the retention window. Implemented by
// internal/subscription/repository.SubscriptionRepository.
type SubscriptionDailyUsageCleaner interface {
	CleanupOldDailyUsage(ctx context.Context, olderThanDays int) (int64, error)
}
