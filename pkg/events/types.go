package events

import (
	"strings"
	"time"
)

// EventType represents the type of event being published
type EventType string

const (
	// Node events
	EventNodeOnline       EventType = "node.online"
	EventNodeOffline      EventType = "node.offline"
	EventNodeStatsUpdated EventType = "node.stats_updated"

	// Subscription events
	EventSubscriptionCreated        EventType = "subscription.created"
	EventSubscriptionExpiring       EventType = "subscription.expiring"
	EventSubscriptionExpired        EventType = "subscription.expired"
	EventSubscriptionRenewed        EventType = "subscription.renewed"
	EventSubscriptionCancelled      EventType = "subscription.cancelled"
	EventSubscriptionTrialActivated EventType = "subscription.trial_activated"

	// Node lifecycle events
	EventNodeCreated EventType = "node.created"
	EventNodeUpdated EventType = "node.updated"
	EventNodeDeleted EventType = "node.deleted"

	// User events
	EventUserRegistered EventType = "user.registered"

	// Account events
	EventAccountDataExhausted EventType = "account.data_exhausted"

	// System events
	EventSystemAlert EventType = "system.alert"

	// Xray process events
	EventXrayDown      EventType = "xray.down"
	EventXrayUp        EventType = "xray.up"
	EventXrayCrashLoop EventType = "xray.crash_loop"

	// Crash recovery command events
	EventXrayRecoveryCommand   EventType = "xray.recovery_command"   // Recovery command was executed
	EventXrayRecoveryExhausted EventType = "xray.recovery_exhausted" // Recovery command max attempts reached

	// Chat events
	EventChatUserMessage       EventType = "chat.user_message"
	EventChatAdminMessage      EventType = "chat.admin_message"
	EventChatMessagesRead      EventType = "chat.messages_read"
	EventChatAdminMessagesRead EventType = "chat.admin_messages_read"
	EventChatNewMessage        EventType = "chat.new_message" // for notification dispatcher
	EventChatTyping            EventType = "chat.typing"
	EventChatOnlineStatus      EventType = "chat.online_status"
	EventChatMessageEdited     EventType = "chat.message_edited"
	EventChatMessageDeleted    EventType = "chat.message_deleted"
	EventChatReactionAdded     EventType = "chat.reaction_added"
	EventChatReactionRemoved   EventType = "chat.reaction_removed"

	// Network / router mode
	EventInterfaceAdded       EventType = "interface.added"
	EventInterfaceRemoved     EventType = "interface.removed"
	EventInterfaceLinkChanged EventType = "interface.link_changed"
	EventWANUp                EventType = "wan.up"
	EventWANDown              EventType = "wan.down"
	EventWANFailover          EventType = "wan.failover"
	EventWANApplyRolledBack   EventType = "wan.apply_rolled_back"
	EventWANLeaseWarning      EventType = "wan.lease_warning"
	// EventWANApplied fires when a plan's ops have run and the dead-man is armed.
	EventWANApplied EventType = "wan.applied"
	// Separate from uplink health: that loop withdraws routes, and a dead tunnel
	// is the wrong reason to.
	EventVPNUp   EventType = "vpn.up"
	EventVPNDown EventType = "vpn.down"
)

// IsNetworkEvent is the recorder filter for the flow page's timeline.
func IsNetworkEvent(e Event) bool {
	t := string(e.Type)
	return strings.HasPrefix(t, "wan.") || strings.HasPrefix(t, "vpn.") ||
		strings.HasPrefix(t, "interface.")
}

// Event represents a real-time event that can be published and subscribed to
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// NodeStatusPayload contains data for node online/offline events
type NodeStatusPayload struct {
	NodeID   uint   `json:"node_id"`
	NodeName string `json:"node_name"`
	IP       string `json:"ip"`
	IsOnline bool   `json:"is_online"`
	Message  string `json:"message,omitempty"`
}

// NodeStatsPayload contains updated stats for a node
type NodeStatsPayload struct {
	NodeID         uint    `json:"node_id"`
	NodeName       string  `json:"node_name"`
	OnlineUsers    int     `json:"online_users"`
	TotalUplink    int64   `json:"total_uplink"`
	TotalDownlink  int64   `json:"total_downlink"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryPercent  float64 `json:"memory_percent"`
	DiskPercent    float64 `json:"disk_percent"`
	MemoryUsedMB   uint64  `json:"memory_used_mb"`
	MemoryTotalMB  uint64  `json:"memory_total_mb"`
	DiskUsedGB     uint64  `json:"disk_used_gb"`
	DiskTotalGB    uint64  `json:"disk_total_gb"`
	UpSpeed        uint64  `json:"up_speed"`
	DownSpeed      uint64  `json:"down_speed"`
	TcpCount       uint64  `json:"tcp_count"`
	UdpCount       uint64  `json:"udp_count"`
	FdCount        uint64  `json:"fd_count"`
	Uptime         uint64  `json:"uptime"`
	SystemUptime   int64   `json:"system_uptime"`
	XrayStatus     string  `json:"xray_status"`
	XrayPID        int64   `json:"xray_pid"`
	AgentVersion   string  `json:"agent_version"`
	XrayVersion    string  `json:"xray_version"`
	LoadAvg1       float64 `json:"load_avg_1"`
	LoadAvg5       float64 `json:"load_avg_5"`
	LoadAvg15      float64 `json:"load_avg_15"`
	TotalAccounts  int64   `json:"total_accounts"`
	ActiveAccounts int64   `json:"active_accounts"`
}

// SubscriptionEventPayload contains data for subscription-related events
type SubscriptionEventPayload struct {
	SubscriptionID uint   `json:"subscription_id"`
	UserID         uint   `json:"user_id"`
	Username       string `json:"username,omitempty"`
	PlanName       string `json:"plan_name"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

// SystemAlertPayload contains data for system alerts
type SystemAlertPayload struct {
	Level   string `json:"level"` // "info", "warning", "error"
	Title   string `json:"title"`
	Message string `json:"message"`
}

// NodeLifecyclePayload contains data for node CRUD events
type NodeLifecyclePayload struct {
	NodeID   uint   `json:"node_id"`
	NodeName string `json:"node_name"`
	IP       string `json:"ip"`
	Action   string `json:"action"` // "created", "updated", "deleted"
}

// UserRegisteredPayload contains data for user registration events
type UserRegisteredPayload struct {
	UserID     uint   `json:"user_id"`
	TelegramID int64  `json:"telegram_id"`
	Username   string `json:"username"`
}

// XrayStatusPayload contains data for xray process status events
type XrayStatusPayload struct {
	NodeID     uint   `json:"node_id"`
	NodeName   string `json:"node_name"`
	IP         string `json:"ip"`
	CrashCount int    `json:"crash_count"`
	ErrorLog   string `json:"error_log,omitempty"`
	Message    string `json:"message,omitempty"`
}

// XrayRecoveryPayload contains data for crash recovery command events.
type XrayRecoveryPayload struct {
	NodeID       uint   `json:"node_id"`
	NodeName     string `json:"node_name"`
	IP           string `json:"ip"`
	CrashCount   int    `json:"crash_count"`
	AttemptNum   int    `json:"attempt_num"`  // Which recovery attempt (1-based)
	MaxAttempts  int    `json:"max_attempts"` // Configured max (0=unlimited)
	Command      string `json:"command"`      // The command that was run
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	Success      bool   `json:"success"`                 // exit code == 0
	ErrorMessage string `json:"error_message,omitempty"` // RPC error if command failed to execute
}

// ChatMessagePayload contains data for chat events
type ChatMessagePayload struct {
	SubscriptionID    uint   `json:"subscription_id"`
	MessageID         uint   `json:"message_id"`
	ContentPreview    string `json:"content_preview,omitempty"`
	SubscriptionLabel string `json:"subscription_label,omitempty"`
	SenderType        string `json:"sender_type,omitempty"`
}

// ChatTypingPayload contains data for typing indicator events
type ChatTypingPayload struct {
	SubscriptionID uint   `json:"subscription_id"`
	SenderType     string `json:"sender_type"`
}

// ChatOnlineStatusPayload contains data for online status events
type ChatOnlineStatusPayload struct {
	SubscriptionID uint   `json:"subscription_id"`
	IsOnline       bool   `json:"is_online"`
	SenderType     string `json:"sender_type"` // "user" or "admin"
}

// ChatMessageMutationPayload contains data for chat message edit/delete events.
type ChatMessageMutationPayload struct {
	SubscriptionID uint   `json:"subscription_id"`
	MessageID      uint   `json:"message_id"`
	Content        string `json:"content,omitempty"`
	EditedAt       string `json:"edited_at,omitempty"`
	By             string `json:"by"` // "user" or "admin"
}

// ChatReactionPayload contains data for reaction add/remove events.
type ChatReactionPayload struct {
	SubscriptionID uint   `json:"subscription_id"`
	MessageID      uint   `json:"message_id"`
	Reactor        string `json:"reactor"`
	Emoji          string `json:"emoji"`
}
