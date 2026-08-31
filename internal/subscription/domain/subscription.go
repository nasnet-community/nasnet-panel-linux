package domain

import (
	"fmt"
	"strings"
	"time"

	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"gorm.io/gorm"
)

type SubscriptionStatus string

const (
	SubscriptionStatusPending          SubscriptionStatus = "pending"
	SubscriptionStatusActive           SubscriptionStatus = "active"
	SubscriptionStatusPaused           SubscriptionStatus = "paused"
	SubscriptionStatusExpired          SubscriptionStatus = "expired"
	SubscriptionStatusCancelled        SubscriptionStatus = "cancelled"
	SubscriptionStatusTrafficExhausted SubscriptionStatus = "traffic_exhausted"
)

type Subscription struct {
	ID     uint             `gorm:"primaryKey" json:"id"`
	UserID *uint            `gorm:"index" json:"user_id"`
	User   *userDomain.User `gorm:"foreignKey:UserID" json:"user,omitempty"`

	ProductType product.ProductType `gorm:"size:50;default:'xray'" json:"product_type"`
	Status      SubscriptionStatus  `gorm:"size:50;default:'pending';index:idx_sub_status_deleted,priority:1" json:"status"`
	IsManual    bool                `gorm:"default:false" json:"is_manual"`
	MaxDevices  int                 `gorm:"default:0" json:"max_devices"`

	// Custom Label for User
	Label string `gorm:"size:100" json:"label"`

	// Telegram Chat ID for notifications
	TelegramChatID int64 `gorm:"default:0" json:"telegram_chat_id"`

	StartDate            *time.Time `json:"start_date"`
	EndDate              *time.Time `json:"end_date"`
	DataUsed             int64      `gorm:"default:0" json:"data_used"`
	LifetimeDataUsed     int64      `gorm:"default:0" json:"lifetime_data_used"` // Never resets
	DataUpload           int64      `gorm:"default:0" json:"data_upload"`
	DataDownload         int64      `gorm:"default:0" json:"data_download"`
	LifetimeDataUpload   int64      `gorm:"default:0" json:"lifetime_data_upload"`
	LifetimeDataDownload int64      `gorm:"default:0" json:"lifetime_data_download"`
	DataLimit            int64      `json:"data_limit"`

	LastActiveAt *time.Time `json:"last_active_at"`

	// Admin Override Fields
	CustomDataLimit      *int64     `json:"custom_data_limit"`                         // Admin override, nil = use plan default
	CustomEndDate        *time.Time `json:"custom_end_date"`                           // Admin override, nil = use calculated end date
	CustomBandwidthLimit *int       `json:"custom_bandwidth_limit"`                    // Admin override (Mbps), nil = use plan default
	IsDataLimitCustom    bool       `gorm:"default:false" json:"is_data_limit_custom"` // Flag for UI
	IsEndDateCustom      bool       `gorm:"default:false" json:"is_end_date_custom"`   // Flag for UI
	IsBandwidthCustom    bool       `gorm:"default:false" json:"is_bandwidth_custom"`  // Flag for UI

	// Data Warning Tracking
	LastDataWarning  *time.Time `json:"last_data_warning"`                   // For notification throttling
	DataWarningLevel int        `gorm:"default:0" json:"data_warning_level"` // 0=none, 1=50%, 2=75%, 3=90%

	// Panel Password Protection
	PanelPasswordHash string `gorm:"size:255" json:"-"`                                    // bcrypt hash, empty = use global default
	PanelPasswordMode string `gorm:"size:20;default:'default'" json:"panel_password_mode"` // "default", "custom", "disabled"

	// Maintenance mode fields (per-subscription layer of maintenance feature)
	MaintenanceMode    bool       `gorm:"default:false" json:"maintenance_mode"`
	MaintenanceMessage string     `gorm:"type:text;default:''" json:"maintenance_message"`
	MaintenanceSince   *time.Time `gorm:"default:null" json:"maintenance_since,omitempty"`

	// Generic config fields
	ConfigID        string `gorm:"size:100;uniqueIndex" json:"config_id"` // UUID or identifier (Xray user UUID)
	LinkKey         string `gorm:"size:100;uniqueIndex" json:"link_key"`  // Subscription link key (for URLs, separate from ConfigID)
	ConfigEmail     string `gorm:"size:255;index" json:"config_email"`    // Email/username
	ConfigData      string `gorm:"type:text" json:"config_data"`          // Full config content
	SubLink         string `gorm:"type:text" json:"sub_link"`             // Import link
	SubscriptionURL string `gorm:"-" json:"subscription_url"`             // Full subscription URL (computed)
	FileExt         string `gorm:"size:20" json:"file_ext"`               // .json, .ovpn, .conf

	// Transient field for partial provisioning notification (not persisted)
	PartialProvisioningNote string `gorm:"-" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_sub_status_deleted,priority:2" json:"-"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}

// GrantsAccess reports whether this subscription should currently be carrying
// traffic. It is the authority for what a node is told about a user, so that a
// missed lifecycle hook cannot leave access live: pause, cancel, expiry and
// quota exhaustion all revoke here regardless of what any per-account or
// per-peer status column says. Pending is allowed so a freshly purchased
// subscription works in the window before its status flips to active.
func (s *Subscription) GrantsAccess() bool {
	switch s.Status {
	case SubscriptionStatusPending, SubscriptionStatusActive:
	default:
		return false
	}
	return !s.IsExpired() && !s.IsDataExhausted()
}

func (s *Subscription) IsExpired() bool {
	endDate := s.GetEffectiveEndDate()
	if endDate == nil {
		return false
	}
	return time.Now().After(*endDate)
}

func (s *Subscription) IsDataExhausted() bool {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 { // unlimited
		return false
	}
	return s.DataUsed >= limit
}

func (s *Subscription) RemainingData() int64 {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 {
		return -1 // unlimited
	}
	remaining := limit - s.DataUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *Subscription) DaysRemaining() int {
	endDate := s.GetEffectiveEndDate()
	if endDate == nil {
		return -1
	}
	duration := time.Until(*endDate)
	if duration <= 0 {
		return 0
	}
	// Ceiling division: any remaining partial day counts as 1 day
	hours := duration.Hours()
	days := int(hours) / 24
	if int(hours)%24 > 0 || hours > float64(days*24) {
		days++
	}
	return days
}

func (s *Subscription) TimeRemainingFormatted() string {
	endDate := s.GetEffectiveEndDate()
	if endDate == nil {
		return "Unlimited"
	}
	duration := time.Until(*endDate)
	if duration < 0 {
		return "Expired"
	}

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d days %d hours", days, hours)
		}
		return fmt.Sprintf("%d days", days)
	}

	if hours > 0 {
		return fmt.Sprintf("%d hours", hours)
	}

	minutes := int(duration.Minutes()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%d min", minutes)
	}

	return "Less than 1 min"
}

func (s *Subscription) GetDisplayName() string {
	if s.Label != "" {
		return s.Label
	}
	return "Subscription"
}

// DisplayLabel returns a human-friendly identifier for the subscription,
// preferring an operator-set custom Label, then the owning user's @username
// (or their name), then the plan name. Returns "" when nothing is known so
// callers can fall back to "Sub #<id>". Requires User/Plan to be preloaded
// for the respective fallbacks to fire.
func (s *Subscription) DisplayLabel() string {
	if s.Label != "" {
		return s.Label
	}
	if s.User != nil {
		if s.User.Username != "" {
			return "@" + s.User.Username
		}
		if name := strings.TrimSpace(s.User.FirstName + " " + s.User.LastName); name != "" {
			return name
		}
	}
	return ""
}

// GetLinkKey returns the subscription link key for URLs.
// Falls back to ConfigID for backward compatibility with older subscriptions.
func (s *Subscription) GetLinkKey() string {
	if s.LinkKey != "" {
		return s.LinkKey
	}
	return s.ConfigID
}

// ToSubscriptionInfo converts to product.SubscriptionInfo
func (s *Subscription) ToSubscriptionInfo() *product.SubscriptionInfo {
	var userID uint
	if s.UserID != nil {
		userID = *s.UserID
	}
	return &product.SubscriptionInfo{
		ID:             s.ID,
		UserID:         userID,
		ConfigID:       s.ConfigID,
		Email:          s.ConfigEmail,
		DataLimit:      s.GetEffectiveDataLimit(),
		BandwidthLimit: s.GetEffectiveBandwidthLimit(),
	}
}

// GetUserID safely returns UserID or 0 if nil
func (s *Subscription) GetUserID() uint {
	if s.UserID != nil {
		return *s.UserID
	}
	return 0
}

// GetEffectiveDataLimit returns custom limit if set, otherwise the base data limit.
// When CustomDataLimit is set to 0, it means unlimited (overrides plan limit).
func (s *Subscription) GetEffectiveDataLimit() int64 {
	if s.CustomDataLimit != nil {
		return *s.CustomDataLimit
	}
	return s.DataLimit
}

// GetEffectiveEndDate returns custom end date if set, otherwise the base end date.
// When IsEndDateCustom is true and CustomEndDate is nil, the subscription has no expiry (unlimited).
func (s *Subscription) GetEffectiveEndDate() *time.Time {
	if s.IsEndDateCustom {
		return s.CustomEndDate // nil means unlimited
	}
	return s.EndDate
}

// GetEffectiveBandwidthLimit returns custom bandwidth if set, otherwise the plan's bandwidth.
// Returns 0 for unlimited.
func (s *Subscription) GetEffectiveBandwidthLimit() int {
	if s.CustomBandwidthLimit != nil {
		return *s.CustomBandwidthLimit
	}
	return 0
}

// GetDataUsagePercentage returns usage as percentage (0-100)
func (s *Subscription) GetDataUsagePercentage() float64 {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 {
		return 0 // Unlimited
	}
	percentage := float64(s.DataUsed) / float64(limit) * 100
	if percentage > 100 {
		return 100
	}
	return percentage
}

// IsApproachingDataLimit checks if usage is above given threshold percentage
func (s *Subscription) IsApproachingDataLimit(thresholdPercent float64) bool {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 {
		return false // Unlimited
	}
	return s.GetDataUsagePercentage() >= thresholdPercent
}

// GetDataWarningLevelString returns human-readable warning level
func (s *Subscription) GetDataWarningLevelString() string {
	switch {
	case s.GetDataUsagePercentage() >= 100:
		return "exhausted"
	case s.GetDataUsagePercentage() >= 90:
		return "critical"
	case s.GetDataUsagePercentage() >= 75:
		return "warning"
	case s.GetDataUsagePercentage() >= 50:
		return "notice"
	default:
		return "none"
	}
}

// Helper constants for data formatting
const (
	GB = 1024 * 1024 * 1024
	MB = 1024 * 1024
	KB = 1024
)

// FormatBytes converts bytes to human-readable string
func FormatBytes(bytes int64) string {
	if bytes < 0 {
		return "Unlimited"
	}
	gb := float64(bytes) / float64(GB)
	if gb >= 1 {
		return formatFloat(gb) + " GB"
	}
	mb := float64(bytes) / float64(MB)
	if mb >= 1 {
		return formatFloat(mb) + " MB"
	}
	kb := float64(bytes) / float64(KB)
	return formatFloat(kb) + " KB"
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%.2f", f)
}

// GetFormattedDataUsage returns human-readable data usage string
func (s *Subscription) GetFormattedDataUsage() string {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 {
		return FormatBytes(s.DataUsed) + " / Unlimited"
	}
	return FormatBytes(s.DataUsed) + " / " + FormatBytes(limit)
}

// GetRemainingDataFormatted returns formatted remaining data
func (s *Subscription) GetRemainingDataFormatted() string {
	limit := s.GetEffectiveDataLimit()
	if limit == 0 {
		return "Unlimited"
	}
	remaining := limit - s.DataUsed
	if remaining < 0 {
		return "0 B"
	}
	return FormatBytes(remaining)
}
