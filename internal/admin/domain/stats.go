package domain

// DashboardStats represents overall system statistics
type DashboardStats struct {
	TotalUsers           int64 `json:"total_users"`
	ActiveUsers          int64 `json:"active_users"`
	OnlineUsers          int   `json:"online_users"`
	BannedUsers          int64 `json:"banned_users"`
	AdminUsers           int64 `json:"admin_users"`
	TotalSubscriptions   int64 `json:"total_subscriptions"`
	ActiveSubscriptions  int64 `json:"active_subscriptions"`
	ExpiredSubscriptions int64 `json:"expired_subscriptions"`
}

// XraySystemStats represents xray-core system statistics
type XraySystemStats struct {
	NumGoroutine uint32 `json:"num_goroutine"`
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	Uptime       uint32 `json:"uptime"`
	OnlineUsers  int    `json:"online_users"`
}

// UserDetails represents detailed user information for admin
type UserDetails struct {
	ID                  uint    `json:"id"`
	TelegramID          int64   `json:"telegram_id"`
	Username            string  `json:"username"`
	FirstName           string  `json:"first_name"`
	LastName            string  `json:"last_name"`
	IsAdmin             bool    `json:"is_admin"`
	IsBanned            bool    `json:"is_banned"`
	Language            string  `json:"language"`
	AdminNotes          string  `json:"admin_notes"`
	TotalSubscriptions  int     `json:"total_subscriptions"`
	ActiveSubscriptions int     `json:"active_subscriptions"`
	TotalDataUsed       int64   `json:"total_data_used"`
	TotalDataUpload     int64   `json:"total_data_upload"`
	TotalDataDownload   int64   `json:"total_data_download"`
	LastActiveAt        *string `json:"last_active_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// UserListItem represents an enriched user row for the admin list view
type UserListItem struct {
	ID                  uint    `json:"id"`
	TelegramID          int64   `json:"telegram_id"`
	Username            string  `json:"username"`
	FirstName           string  `json:"first_name"`
	LastName            string  `json:"last_name"`
	IsAdmin             bool    `json:"is_admin"`
	IsBanned            bool    `json:"is_banned"`
	ActiveSubscriptions int     `json:"active_subscriptions"`
	TotalSubscriptions  int     `json:"total_subscriptions"`
	LastActiveAt        *string `json:"last_active_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// BroadcastResult represents the result of a broadcast operation
type BroadcastResult struct {
	TotalUsers int `json:"total_users"`
	Sent       int `json:"sent"`
	Failed     int `json:"failed"`
}

// UserDailyUsagePoint represents a single day's data usage for chart rendering
type UserDailyUsagePoint struct {
	Date     string `json:"date"`
	DataUsed int64  `json:"data_used"`
}

// UserActivityEvent represents a single audit log event for the user activity feed
type UserActivityEvent struct {
	ID         uint   `json:"id"`
	Action     string `json:"action"`
	ActorName  string `json:"actor_name"`
	EntityType string `json:"entity_type"`
	EntityID   uint   `json:"entity_id"`
	OldValues  string `json:"old_values,omitempty"`
	NewValues  string `json:"new_values,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// ==================== Analytics Types ====================

// HourlyUsagePoint represents connection count for a single hour of the day.
type HourlyUsagePoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// PeakHourPoint represents aggregated connection stats for a single hour of the day.
type PeakHourPoint struct {
	Hour        int   `json:"hour"`
	Connections int64 `json:"connections"`
	Rejected    int64 `json:"rejected"`
	UniqueUsers int64 `json:"unique_users"`
	TcpCount    int64 `json:"tcp_count"`
	UdpCount    int64 `json:"udp_count"`
}

// BlockedDomainStat represents a single blocked domain's statistics.
type BlockedDomainStat struct {
	Domain        string `json:"domain"`
	RejectedCount int64  `json:"rejected_count"`
	NodeCount     int    `json:"node_count"`
	LastSeen      string `json:"last_seen"`
}

// BlockedDomainSummary holds aggregated blocked domain statistics.
type BlockedDomainSummary struct {
	Domains       []BlockedDomainStat `json:"domains"`
	TotalRejected int64               `json:"total_rejected"`
	TotalAccepted int64               `json:"total_accepted"`
	RejectionRate float64             `json:"rejection_rate"`
	PeriodFrom    string              `json:"period_from"`
	PeriodTo      string              `json:"period_to"`
}

// ExhaustionPrediction represents a data exhaustion forecast for a subscription.
type ExhaustionPrediction struct {
	SubscriptionID   uint    `json:"subscription_id"`
	Label            string  `json:"label"`
	DataLimit        int64   `json:"data_limit"`
	DataUsed         int64   `json:"data_used"`
	DataRemaining    int64   `json:"data_remaining"`
	DailyAvgBytes    int64   `json:"daily_avg_bytes"`
	DaysRemaining    int     `json:"days_remaining"`
	EndDate          *string `json:"end_date"`
	DaysUntilExpiry  int     `json:"days_until_expiry"`
	ExhaustionDate   *string `json:"exhaustion_date"`
	WillExhaustFirst bool    `json:"will_exhaust_first"`
	UsageTrend       string  `json:"usage_trend"`
	Confidence       float64 `json:"confidence"`
	Unlimited        bool    `json:"unlimited"`
}

// UserAccountInfo represents an account belonging to a user, grouped by node
type UserAccountInfo struct {
	AccountID      uint   `json:"account_id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	InboundTag     string `json:"inbound_tag"`
	Protocol       string `json:"protocol"`
	NodeID         uint   `json:"node_id"`
	NodeName       string `json:"node_name"`
	NodeCountry    string `json:"node_country"`
	SubscriptionID *uint  `json:"subscription_id"`
	DataUsed       int64  `json:"data_used"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
}
