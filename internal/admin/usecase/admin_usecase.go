package usecase

import (
	"context"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	adminRepo "github.com/nasnet-community/nasnet-panel-linux/internal/admin/repository"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

// AdminUsecase (admin operations)
type AdminUsecase interface {
	// Dashboard
	GetDashboardStats(ctx context.Context) (*adminDomain.DashboardStats, error)
	GetXraySystemStats(ctx context.Context) (*adminDomain.XraySystemStats, error)
	GetOnlineUsers(ctx context.Context) ([]string, error)
	GetUserOnlineSessions(ctx context.Context, email string) (int64, error)

	// Subscription IP tracking
	GetSubscriptionIPs(ctx context.Context, subID uint) ([]subDomain.SubscriptionIP, error)
	GetSubscriptionActiveIPs(ctx context.Context, subID uint) ([]subDomain.SubscriptionIP, error)
	GetOnlineUsersWithIPs(ctx context.Context) (map[string][]string, error)

	// Data retention
	GetRetentionStats(ctx context.Context) ([]adminDomain.RetentionStat, error)
	// RunRetentionCleanup triggers the same sweep as the scheduler's timed
	// tick but synchronously. Returns per-task deleted counts so the UI can
	// show a "deleted N rows across M tables" toast. Safe to call concurrently
	// with the scheduler; runs in process, no separate worker.
	RunRetentionCleanup(ctx context.Context) map[string]int64
	// SetRetentionCleanupRunner wires the scheduler's synchronous cleanup
	// hook post-construction, breaking the bootstrap cycle (admin UC is
	// built before the scheduler).
	SetRetentionCleanupRunner(run func(ctx context.Context) map[string]int64)

	// RecordOnlineUsersSnapshot writes one row capturing the current
	// dedup'd global online-user count from pkg/cache. Intended to be
	// called from the scheduler at the end of each SyncNodeStats tick.
	RecordOnlineUsersSnapshot(ctx context.Context) error

	// GetOnlineUsersHistory returns snapshots newer than now - `minutes`
	// in ascending CreatedAt order. `minutes` is clamped to [5, 1440].
	GetOnlineUsersHistory(ctx context.Context, minutes int) ([]*adminDomain.OnlineUsersSnapshot, error)

	// User management
	ListUsers(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*adminDomain.UserListItem, int64, error)
	GetUserDetails(ctx context.Context, id uint) (*adminDomain.UserDetails, error)
	BanUser(ctx context.Context, id uint) error
	UnbanUser(ctx context.Context, id uint) error
	SetAdmin(ctx context.Context, id uint, isAdmin bool) error

	ListAllNodes(ctx context.Context) ([]*nodeDomain.Node, error)

	// Subscription management
	ListAllSubscriptions(ctx context.Context, status string, offset, limit int) ([]*subDomain.Subscription, error)
	GetSubscription(ctx context.Context, id uint) (*subDomain.Subscription, error)
	GetSubscriptionsByUser(ctx context.Context, userID uint) ([]*subDomain.Subscription, error)
	ExtendSubscription(ctx context.Context, id uint, days int) error
	RevokeSubscription(ctx context.Context, id uint) error
	PauseSubscription(ctx context.Context, id uint) error
	ResumeSubscription(ctx context.Context, id uint) error
	RegenerateSubscriptionKey(ctx context.Context, id uint, customKey string) (*subDomain.Subscription, error)
	RegenerateSubscriptionUUID(ctx context.Context, id uint) (*subDomain.Subscription, error)
	SetSubscriptionUUID(ctx context.Context, id uint, newUUID string) (int, error)
	GetSubscriptionLink(ctx context.Context, id uint) (string, error)
	ResetDataUsage(ctx context.Context, id uint) error
	SetDataUsage(ctx context.Context, id uint, bytesUsed int64) error
	DeleteSubscription(ctx context.Context, id uint) error
	BulkSubscriptionAction(ctx context.Context, action string, ids []uint) (*BulkActionResult, error)
	BulkSetBandwidthLimit(ctx context.Context, ids []uint, limitMbps *int) (*BulkActionResult, error)
	BulkManageInbounds(ctx context.Context, subscriptionIDs []uint, addInboundIDs, removeInboundIDs []uint) (*BulkInboundResult, error)
	GetBulkInboundSummary(ctx context.Context, subscriptionIDs []uint) (*BulkInboundSummary, error)
	CountAllSubscriptions(ctx context.Context) (int64, error)
	CountSubscriptionsByStatus(ctx context.Context, status string) (int64, error)
	ListAllFilteredSubscriptions(ctx context.Context, filter subRepo.SubscriptionFilter) ([]*subDomain.Subscription, int64, error)

	// User detail page endpoints
	UpdateUserNotes(ctx context.Context, userID uint, notes string) error
	GetUserUsageHistory(ctx context.Context, userID uint, days int) ([]adminDomain.UserDailyUsagePoint, error)
	GetUserAccounts(ctx context.Context, userID uint) ([]adminDomain.UserAccountInfo, error)

	// Broadcast
	BroadcastMessage(ctx context.Context, bot *telebot.Bot, message string, onlyActive bool) (*adminDomain.BroadcastResult, error)

	// Xray & Advanced Management
	GetInboundDetails(ctx context.Context) (string, error)

	// Node Inbound Discovery
	DiscoverNodeInbounds(ctx context.Context, nodeID uint) ([]*nodeDomain.Inbound, error)
	SyncNodeInbounds(ctx context.Context, nodeID uint) (*nodeUC.SyncResult, error)

	// Manual User Management
	AddUserToInbound(ctx context.Context, nodeID uint, inboundTag, email string) (*xray.User, string, error)
	GenerateCustomConfigLink(ctx context.Context, nodeID uint, inboundTag, email, uuid string) (string, error)

	// Subscription Data/Expiry/Bandwidth Management
	SetSubscriptionBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error
	SetSubscriptionDataLimit(ctx context.Context, id uint, limitGB *float64) error
	SetSubscriptionMaxDevices(ctx context.Context, id uint, maxDevices int) error
	SetSubscriptionEndDate(ctx context.Context, id uint, endDate *time.Time, unlimited bool) (*subDomain.Subscription, error)
	RenameSubscription(ctx context.Context, id uint, label string) error
	SetSubscriptionPanelPassword(ctx context.Context, id uint, mode, password string) error
	AddSubscriptionData(ctx context.Context, id uint, additionalGB float64) error
	ResetSubscriptionData(ctx context.Context, id uint) error
	GetSubscriptionUsageDetails(ctx context.Context, id uint) (*subUC.SubscriptionUsageDetails, error)
	GetSubscriptionUsageHistory(ctx context.Context, id uint, days int) ([]subUC.UsageHistoryPoint, error)

	// Manual Subscription
	CreateManualSubscription(ctx context.Context, req *subUC.ManualSubscriptionRequest) (*subDomain.Subscription, error)

	// User management (admin)
	UpdateUserTelegramID(ctx context.Context, userID uint, telegramID int64) error
	CreateUser(ctx context.Context, username, firstName, lastName string, telegramID int64) (*userDomain.User, error)

	// Subscription user assignment
	AssignSubscriptionUser(ctx context.Context, subID uint, userID *uint) (*subDomain.Subscription, error)

	// Analytics
	GetUserUsagePattern(ctx context.Context, userID uint, days int) ([]adminDomain.HourlyUsagePoint, error)
	GetPeakHours(ctx context.Context, days int, nodeIDs []uint) ([]adminDomain.PeakHourPoint, error)
	GetBlockedDomainStats(ctx context.Context, days int, nodeIDs []uint, topN int) (*adminDomain.BlockedDomainSummary, error)
	GetExhaustionPrediction(ctx context.Context, subID uint) (*adminDomain.ExhaustionPrediction, error)

	// Database cleanup
	CleanupDatabase(ctx context.Context) (*CleanupResult, error)

	SetBot(bot *telebot.Bot)
}

type adminUsecase struct {
	userRepo                userRepo.UserRepository
	subRepo                 subRepo.SubscriptionRepository
	subIPRepo               subRepo.SubscriptionIPRepository
	subUC                   subUC.SubscriptionUsecase
	nodeRepo                nodeRepo.NodeRepository
	nodeUC                  nodeUC.NodeUsecase
	providerFactory         *product.ProviderFactory
	grpcClient              *xray.GRPCClient
	provService             provisioning.ProvisioningService
	inboundTag              string
	accountUC               contract.AccountManager // Injected later to avoid circular deps
	bot                     *telebot.Bot
	settingUC               settingDomain.SettingUsecase
	accountRepo             accountRepo.AccountRepository
	usageRepo               userRepo.UserDailyUsageRepository
	db                      *gorm.DB
	onlineUsersSnapshotRepo adminRepo.OnlineUsersSnapshotRepository
	retentionStatsRepo      adminRepo.RetentionStatsRepository
	retentionCleanupRunner  func(ctx context.Context) map[string]int64
}

// BulkActionResult aggregates results from a bulk operation
type BulkActionResult struct {
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

// BulkInboundResult aggregates results from a bulk inbound management operation
type BulkInboundResult struct {
	SubscriptionsAffected int      `json:"subscriptions_affected"`
	AccountsAdded         int      `json:"accounts_added"`
	AccountsMarkedRemoval int      `json:"accounts_marked_for_removal"`
	Skipped               int      `json:"skipped"`
	Errors                []string `json:"errors,omitempty"`
}

// BulkInboundSummary shows inbound coverage across selected subscriptions
type BulkInboundSummary struct {
	InboundCounts      map[uint]int `json:"inbound_counts"`
	TotalSubscriptions int          `json:"total_subscriptions"`
}

// CleanupResult holds per-table deletion counts from a database cleanup operation
type CleanupResult struct {
	AccountsRemoved          int64 `json:"accounts_removed"`
	SubscriptionsDeleted     int64 `json:"subscriptions_deleted"`
	UsersDeleted             int64 `json:"users_deleted"`
	AuditLogsDeleted         int64 `json:"audit_logs_deleted"`
	NotificationLogsDeleted  int64 `json:"notification_logs_deleted"`
	ProvisioningTasksDeleted int64 `json:"provisioning_tasks_deleted"`
	UserDailyUsageDeleted    int64 `json:"user_daily_usage_deleted"`
	NodeStatsDeleted         int64 `json:"node_stats_deleted"`
	AdminsReset              int64 `json:"admins_reset"`
	ConversationsDeleted     int64 `json:"conversations_deleted"`
}

func NewAdminUsecase(
	userRepo userRepo.UserRepository,
	subRepo subRepo.SubscriptionRepository,
	subIPRepo subRepo.SubscriptionIPRepository,
	subUC subUC.SubscriptionUsecase,
	nodeRepo nodeRepo.NodeRepository,
	nodeUC nodeUC.NodeUsecase,
	providerFactory *product.ProviderFactory,
	grpcClient *xray.GRPCClient,
	provService provisioning.ProvisioningService,
	inboundTag string,
	settingUC settingDomain.SettingUsecase,
	accountManager contract.AccountManager,
	accountRepository accountRepo.AccountRepository,
	usageDailyRepo userRepo.UserDailyUsageRepository,
	database *gorm.DB,
	onlineUsersSnapshotRepo adminRepo.OnlineUsersSnapshotRepository,
	retentionStatsRepo adminRepo.RetentionStatsRepository,
) AdminUsecase {
	return &adminUsecase{
		userRepo:                userRepo,
		subRepo:                 subRepo,
		subIPRepo:               subIPRepo,
		subUC:                   subUC,
		nodeRepo:                nodeRepo,
		nodeUC:                  nodeUC,
		providerFactory:         providerFactory,
		grpcClient:              grpcClient,
		provService:             provService,
		inboundTag:              inboundTag,
		settingUC:               settingUC,
		accountUC:               accountManager,
		accountRepo:             accountRepository,
		usageRepo:               usageDailyRepo,
		db:                      database,
		onlineUsersSnapshotRepo: onlineUsersSnapshotRepo,
		retentionStatsRepo:      retentionStatsRepo,
	}
}

func (u *adminUsecase) SetBot(bot *telebot.Bot) {
	u.bot = bot
}

func (u *adminUsecase) ListAllNodes(ctx context.Context) ([]*nodeDomain.Node, error) {
	return u.nodeRepo.ListNodes(ctx)
}
