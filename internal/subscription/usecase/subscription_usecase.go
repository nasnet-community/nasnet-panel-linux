package usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	notifDomain "github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

var (
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrPlanNotFound         = errors.New("plan not found")
	ErrUserNotFound         = errors.New("user not found")
	ErrInsufficientBalance  = errors.New("insufficient balance")
	ErrCannotTopUpUnlimited = errors.New("cannot top up an unlimited subscription")
	ErrSubscriptionExpired  = errors.New("subscription has expired")
	ErrDataLimitExceeded    = errors.New("data limit exceeded")
	ErrProviderNotFound     = errors.New("provider not found for product type")
	ErrNoNodesAvailable     = errors.New("no active nodes available for this plan")

	ErrTrialAlreadyUsed     = errors.New("trial already used")
	ErrNoTrialAvailable     = errors.New("no trial plan available")
	ErrTrialInvalidDuration = errors.New("trial plan has invalid duration (must be > 0 days)")
	ErrPlanNotRenewable     = errors.New("this plan cannot be renewed")
	ErrTopUpNotAllowed      = errors.New("top-up not allowed for this plan")
	ErrRenewNotAllowed      = errors.New("renew not allowed for this plan")
	// ErrPlanInactive blocks owners from renewing or adding data on a plan an
	// admin has disabled. Existing subs keep running until they lapse, but a
	// disabled plan can't be re-bought into via renew/top-up.
	ErrPlanInactive = errors.New("plan is inactive")
)

// SubscriptionConfigResult holds the generated config and metadata for subscription headers
type SubscriptionConfigResult struct {
	Config    string     // Newline-separated proxy links
	DataUsed  int64      // Bytes downloaded
	DataLimit int64      // Total data limit in bytes (0 = unlimited)
	ExpiresAt *time.Time // Subscription expiry (nil = unlimited)
	PlanName  string     // Plan display name
}

type ReconcileStats struct {
	TotalDBUsers   int
	TotalXrayUsers int
	MissingAdded   int
	GhostsRemoved  int
	Errors         int
}

// SubscriptionUsageDetails provides detailed usage information for a subscription
type SubscriptionUsageDetails struct {
	Subscription       *domain.Subscription `json:"subscription"`
	EffectiveDataLimit int64                `json:"effective_data_limit"`
	DataLimitGB        float64              `json:"data_limit_gb"`
	DataUsedGB         float64              `json:"data_used_gb"`
	DataRemainingGB    float64              `json:"data_remaining_gb"`
	UsagePercentage    float64              `json:"usage_percentage"`
	IsUnlimited        bool                 `json:"is_unlimited"`
	IsCustomLimit      bool                 `json:"is_custom_limit"`
	IsCustomExpiry     bool                 `json:"is_custom_expiry"`
	DaysRemaining      int                  `json:"days_remaining"`
	Status             string               `json:"status"`
	WarningLevel       string               `json:"warning_level"` // "none", "notice", "warning", "critical", "exhausted"
}

// UsageHistoryPoint represents a daily usage data point
type UsageHistoryPoint struct {
	Date     string `json:"date"`
	DataUsed int64  `json:"data_used"` // daily delta bytes
}

// HourlyUsagePoint represents aggregated connection count for an hour of day.
type HourlyUsagePoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

type SubscriptionUsecase interface {
	GetByID(ctx context.Context, id uint) (*domain.Subscription, error)
	GetByConfigID(ctx context.Context, configID string) (*domain.Subscription, error)
	ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]*domain.Subscription, error)
	GetActiveByUserID(ctx context.Context, userID uint) ([]*domain.Subscription, error)
	ListAllSubscriptions(ctx context.Context, status string, offset, limit int) ([]*domain.Subscription, error)
	Cancel(ctx context.Context, id uint) error
	UpdateDataUsage(ctx context.Context, id uint, bytesUsed int64) error
	CheckAndExpireSubscriptions(ctx context.Context) error
	CheckAndExpireByDataLimit(ctx context.Context) error
	GetSubscriptionLink(ctx context.Context, id uint) (string, error)
	SyncUsageFromXray(ctx context.Context, id uint) error
	RenameSubscription(ctx context.Context, id uint, label string) error
	UpdateTelegramChatIDByConfigID(ctx context.Context, configID string, chatID int64) error
	ReconcileUsers(ctx context.Context) (*ReconcileStats, error)
	RegenerateUUID(ctx context.Context, id uint) (*domain.Subscription, error)
	RegenerateSubscriptionKey(ctx context.Context, id uint, customKey string) (*domain.Subscription, error)

	GetSubscriptionConfig(ctx context.Context, configID string) (*SubscriptionConfigResult, error)
	GetSubscriptionServers(ctx context.Context, subID uint) ([]SubServer, error)

	// Migration support
	GetByConfigEmail(ctx context.Context, email string) (*domain.Subscription, error)
	CreateDirect(ctx context.Context, sub *domain.Subscription) error
	AssignToUser(ctx context.Context, subID, userID uint) error

	// Inbound assignment
	AssignToInbound(ctx context.Context, subscriptionID, inboundID uint) error

	// Custom data/expiry/bandwidth management (admin overrides)
	SetCustomDataLimit(ctx context.Context, id uint, limitGB *float64) error
	SetCustomEndDate(ctx context.Context, id uint, endDate *time.Time, isCustom bool) (*domain.Subscription, error)
	SetCustomBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error
	SetMaxDevices(ctx context.Context, id uint, maxDevices int) error
	AddData(ctx context.Context, id uint, additionalGB float64) error
	ResetDataUsed(ctx context.Context, id uint) error
	GetUsageDetails(ctx context.Context, id uint) (*SubscriptionUsageDetails, error)
	CheckAndSendDataWarnings(ctx context.Context) ([]*domain.Subscription, error)

	// Filtered admin list (used by web panel)
	ListAllFilteredSubscriptions(ctx context.Context, filter repository.SubscriptionFilter) ([]*domain.Subscription, int64, error)

	// Subscription daily usage
	GetSubscriptionUsageHistory(ctx context.Context, subID uint, days int) ([]UsageHistoryPoint, error)
	ListDailyUsageRecords(ctx context.Context, subID uint, days int) ([]*domain.SubscriptionDailyUsage, error)
	GetSubscriptionUsageTrend(ctx context.Context, subID uint, rangeDays int) (*domain.UsageTrend, error)

	// Analytics (public)
	GetSubscriptionUsagePattern(ctx context.Context, configID string, days int) ([]HourlyUsagePoint, error)

	// Panel password management
	SetPanelPassword(ctx context.Context, id uint, mode string, password string) error
	GetPanelPasswordHash(ctx context.Context, id uint) (hash string, mode string, err error)

	// Manual subscription (no user/plan)
	CreateManual(ctx context.Context, req *ManualSubscriptionRequest) (*domain.Subscription, error)
}

// ManualSubscriptionRequest contains parameters for creating a manual subscription
type ManualSubscriptionRequest struct {
	Label          string
	InboundIDs     []uint
	DataLimit      int64 // bytes, 0 = unlimited
	BandwidthLimit int   // Mbps, 0 = unlimited
	MaxDevices     int
	EndDate        *time.Time // nil = unlimited
	UserID         *uint      // optional user link
}

// userLockEntry holds a mutex and a reference count for cleanup.
type userLockEntry struct {
	mu       sync.Mutex
	refCount int32 // atomic
}

type subscriptionUsecase struct {
	subRepo         repository.SubscriptionRepository
	userRepo        userRepo.UserRepository
	nodeRepo        nodeRepo.NodeRepository
	nodeUC          nodeUC.NodeUsecase
	nodeSyncer      contract.NodeSyncer
	accountManager  contract.AccountManager
	accountReader   contract.AccountReader
	providerFactory *product.ProviderFactory
	grpcClient      xray.XrayClient
	tm              database.TransactionManager
	eventBus        *events.EventBus
	notifCleaner    NotificationCleaner // optional; re-arms one-shot notifications on renew/reset
	wgPeerReader    WGPeerReader        // optional; enables wireguard:// links in /sub
	userLocks       sync.Map            // keyed by uint userID → *userLockEntry
}

// NotificationCleaner re-arms one-shot subscription notifications by deleting
// their log rows. Implemented by the notification repository and injected via
// SetNotificationCleaner so the usecase needn't depend on the repo at
// construction time (and stays nil-safe where it's unwired).
type NotificationCleaner interface {
	DeleteBySubscriptionAndTypes(ctx context.Context, subscriptionID uint, notifTypes ...notifDomain.NotificationType) error
}

// WGPeerReader loads a sub's WG peers for wireguard:// links. Optional.
type WGPeerReader interface {
	ListBySubscription(ctx context.Context, subID uint) ([]*wgDomain.WGPeer, error)
}

// expiryNotificationTypes are the one-shot expiry reminders that must re-arm
// when a subscription's validity window is extended (renew / top-up-extend).
var expiryNotificationTypes = []notifDomain.NotificationType{
	notifDomain.NotificationTypeExpiry7Days,
	notifDomain.NotificationTypeExpiry3Days,
	notifDomain.NotificationTypeExpiry1Day,
	notifDomain.NotificationTypeExpired,
}

// SetNotificationCleaner wires the optional notification cleaner. Following the
// existing setter-injection pattern used for reconcilers in bootstrap.
func (u *subscriptionUsecase) SetNotificationCleaner(c NotificationCleaner) {
	u.notifCleaner = c
}

// SetWGPeerReader wires the optional WG peer reader (setter injection).
func (u *subscriptionUsecase) SetWGPeerReader(r WGPeerReader) {
	u.wgPeerReader = r
}

// resetDataWarnings clears all "data running low/out" alert state for a sub so a
// fresh cycle re-arms it: the monotonic warning-level counter (DB column) AND
// the one-shot data-exhausted notification log. Without clearing the log, a
// renewed sub never re-fires its exhaustion alert until retention cleanup
// purges the stale row. Mirrors how the level counter already re-arms.
func (u *subscriptionUsecase) resetDataWarnings(ctx context.Context, id uint) error {
	if u.notifCleaner != nil {
		if err := u.notifCleaner.DeleteBySubscriptionAndTypes(ctx, id, notifDomain.NotificationTypeDataExhausted); err != nil {
			logger.GetLogger().WithError(err).WithField("subscription_id", id).
				Warn("[resetDataWarnings] failed to clear data-exhausted notification")
		}
	}
	return u.subRepo.ResetDataWarningLevel(ctx, id)
}

// rearmExpiryNotifications clears the one-shot expiry reminder logs so a sub
// whose validity window was just extended re-fires them as it nears the new end
// date. Without this a renewed sub stays silent near expiry until retention
// cleanup purges the stale rows. Nil-safe when the cleaner isn't wired.
func (u *subscriptionUsecase) rearmExpiryNotifications(ctx context.Context, id uint) {
	if u.notifCleaner == nil {
		return
	}
	if err := u.notifCleaner.DeleteBySubscriptionAndTypes(ctx, id, expiryNotificationTypes...); err != nil {
		logger.GetLogger().WithError(err).WithField("subscription_id", id).
			Warn("[rearmExpiryNotifications] failed to clear expiry notifications")
	}
}

// acquireUserLock returns a locked mutex for the given user. Call the returned
// function to unlock and release the entry (cleaning it up if no other goroutine holds it).
func (u *subscriptionUsecase) acquireUserLock(userID uint) func() {
	val, _ := u.userLocks.LoadOrStore(userID, &userLockEntry{})
	entry := val.(*userLockEntry)
	atomic.AddInt32(&entry.refCount, 1)
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		if atomic.AddInt32(&entry.refCount, -1) == 0 {
			// No other goroutine waiting; safe to remove from the map.
			// If a new goroutine races in, LoadOrStore will create a fresh entry.
			u.userLocks.CompareAndDelete(userID, entry)
		}
	}
}

func NewSubscriptionUsecase(
	subRepo repository.SubscriptionRepository,
	userRepo userRepo.UserRepository,
	nodeRepo nodeRepo.NodeRepository,
	providerFactory *product.ProviderFactory,
	grpcClient xray.XrayClient,
	nodeUC nodeUC.NodeUsecase,
	nodeSyncer contract.NodeSyncer,
	accountManager contract.AccountManager,
	accountReader contract.AccountReader,
	tm database.TransactionManager,
	eventBus *events.EventBus,
) SubscriptionUsecase {
	return &subscriptionUsecase{
		subRepo:         subRepo,
		userRepo:        userRepo,
		nodeRepo:        nodeRepo,
		providerFactory: providerFactory,
		grpcClient:      grpcClient,
		nodeUC:          nodeUC,
		nodeSyncer:      nodeSyncer,
		accountManager:  accountManager,
		accountReader:   accountReader,
		tm:              tm,
		eventBus:        eventBus,
	}
}
