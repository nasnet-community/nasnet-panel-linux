package usecase

import (
	"context"
	"fmt"
	"strings"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"golang.org/x/crypto/bcrypt"
)

// InitialConfig holds values from environment/config that seed the server settings category.
type InitialConfig struct {
	AppPort int
	BaseURL string

	SubPanelURL     string
	BotToken        string
	ACMEEnabled     bool
	ACMEEmail       string
	ACMEStaging     bool
	ACMEAutoRenew   bool
	TLSCertFile     string
	TLSKeyFile      string
	MetricsUsername string
	MetricsPassword string
	LogLevel        string
	PanelBasePath   string
	ProxyEnabled    bool
	ProxyType       string
	ProxyHost       string
	ProxyPort       int
	ProxyUsername   string
	ProxyPassword   string
}

type settingUsecase struct {
	repo                  domain.SettingRepository
	initialCfg            *InitialConfig
	onXrayVersionChange   func(version string)
	onMaintenanceChange   func()
	onOutboundProxyChange func(proxyURL string, enabled map[string]bool)
	onRouterHealthChange  func()
	auditUC               auditDomain.AuditLogUsecase
}

func NewSettingUsecase(repo domain.SettingRepository, cfg *InitialConfig) domain.SettingUsecase {
	return &settingUsecase{repo: repo, initialCfg: cfg}
}

func (u *settingUsecase) SetOnXrayVersionChange(fn func(string)) {
	u.onXrayVersionChange = fn
}

func (u *settingUsecase) SetOnMaintenanceChange(fn func()) {
	u.onMaintenanceChange = fn
}

func (u *settingUsecase) SetOnOutboundProxyChange(fn func(proxyURL string, enabled map[string]bool)) {
	u.onOutboundProxyChange = fn
}

func (u *settingUsecase) SetOnRouterHealthChange(fn func()) {
	u.onRouterHealthChange = fn
}

// outboundProxyFeatureKeys lists the suffix names for the proxy_use_* setting
// family. Keep in sync with httpclient.AllFeatures().
var outboundProxyFeatureKeys = []string{
	"telegram", "geofiles", "xray_binary", "github_api",
	"webhooks", "geoip", "acme", "wizard",
}

func (u *settingUsecase) SetAuditUsecase(auc auditDomain.AuditLogUsecase) {
	u.auditUC = auc
}

func (u *settingUsecase) GetAll(ctx context.Context) (map[string][]*domain.Setting, error) {
	settings, err := u.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string][]*domain.Setting)
	for _, s := range settings {
		grouped[s.Category] = append(grouped[s.Category], s)
	}

	// Filter out sensitive admin settings from API response
	delete(grouped, "admin")

	// Mask values of sensitive settings
	for _, categorySettings := range grouped {
		for _, s := range categorySettings {
			if s.Sensitive && s.Value != "" {
				s.Value = maskValue(s.Value)
			}
		}
	}

	return grouped, nil
}

// maskValue replaces all but the last 4 characters with asterisks.
func maskValue(v string) string {
	if len(v) <= 4 {
		return strings.Repeat("*", len(v))
	}
	return strings.Repeat("*", len(v)-4) + v[len(v)-4:]
}

func (u *settingUsecase) UpdateMany(ctx context.Context, settings []*domain.Setting) error {
	// Filter out unchanged sensitive fields (value contains masking asterisks)
	filtered := make([]*domain.Setting, 0, len(settings))
	for _, s := range settings {
		if isMaskedValue(s.Value) {
			continue
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return nil
	}

	// Hash panel password before storing
	for _, s := range filtered {
		if s.Key == "sub_panel_password" && s.Value != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(s.Value), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash panel password: %w", err)
			}
			s.Value = string(hash)
		}
	}

	// Capture old values + canonical Sensitive flags for audit. Trusting
	// the request's Sensitive flag would let a malicious or buggy client
	// suppress redaction by omitting it; read from storage instead.
	oldValues := make(map[string]string, len(filtered))
	canonicalSensitive := make(map[string]bool, len(filtered))
	if u.auditUC != nil {
		for _, s := range filtered {
			if old, err := u.repo.GetByKey(ctx, s.Key); err == nil {
				oldValues[s.Key] = old.Value
				canonicalSensitive[s.Key] = old.Sensitive
			}
		}
	}

	if err := u.repo.UpdateMany(ctx, filtered); err != nil {
		return err
	}

	// Audit log the changes
	if u.auditUC != nil {
		for _, s := range filtered {
			oldVal := oldValues[s.Key]
			newVal := s.Value
			if s.Sensitive || canonicalSensitive[s.Key] {
				oldVal = "[redacted]"
				newVal = "[redacted]"
			}
			u.auditUC.Log(ctx, &auditDomain.AuditLog{
				Action:     string(auditDomain.AuditSettingsUpdate),
				EntityType: "setting",
				OldValues:  fmt.Sprintf(`{"%s":"%s"}`, s.Key, oldVal),
				NewValues:  fmt.Sprintf(`{"%s":"%s"}`, s.Key, newVal),
				Source:     "api",
			})
		}
	}

	// Post-update hooks
	maintenanceTouched := false
	for _, s := range filtered {
		if s.Key == "log_level" {
			if err := logger.SetLevel(s.Value); err != nil {
				logger.GetLogger().WithError(err).Warn("Failed to apply log_level setting")
			} else {
				logger.GetLogger().WithField("level", s.Value).Info("Log level changed via settings")
			}
		}
		if s.Key == "xray_default_version" && u.onXrayVersionChange != nil {
			autoDownload := "true"
			if v, err := u.GetByKey(ctx, "xray_auto_download"); err == nil {
				autoDownload = v
			}
			if autoDownload == "true" {
				go u.onXrayVersionChange(s.Value)
			}
		}
		switch s.Key {
		case "maintenance_mode_enabled", "maintenance_mode_message", "maintenance_mode_since":
			maintenanceTouched = true
		}
	}
	if maintenanceTouched && u.onMaintenanceChange != nil {
		u.onMaintenanceChange()
	}

	// Fire outbound-proxy callback on outbound_proxy_url or proxy_use_*
	// change. Callback reads canonical values from storage.
	proxyTouched := false
	for _, s := range filtered {
		if s.Key == "outbound_proxy_url" || strings.HasPrefix(s.Key, "proxy_use_") {
			proxyTouched = true
			break
		}
	}
	if proxyTouched && u.onOutboundProxyChange != nil {
		url, _ := u.GetByKey(ctx, "outbound_proxy_url")
		enabled := make(map[string]bool, len(outboundProxyFeatureKeys))
		for _, feat := range outboundProxyFeatureKeys {
			v, _ := u.GetByKey(ctx, "proxy_use_"+feat)
			enabled[feat] = v == "true"
		}
		u.onOutboundProxyChange(url, enabled)
	}

	// The health loop re-reads its config when any router_ key changes.
	if u.onRouterHealthChange != nil {
		for _, s := range filtered {
			if strings.HasPrefix(s.Key, "router_") {
				u.onRouterHealthChange()
				break
			}
		}
	}

	return nil
}

// isMaskedValue returns true if the string looks like a masked value (starts with asterisks).
func isMaskedValue(s string) bool {
	return len(s) > 0 && strings.HasPrefix(s, "***")
}

func (u *settingUsecase) GetByKey(ctx context.Context, key string) (string, error) {
	s, err := u.repo.GetByKey(ctx, key)
	if err != nil {
		return "", err
	}
	return s.Value, nil
}

func (u *settingUsecase) SeedDefaults(ctx context.Context) error {
	defaults := []*domain.Setting{
		// General
		{Key: "site_name", Value: "NasNet Panel", Type: "string", Category: "general", Description: "The name of your site", Label: "Site Name"},
		{Key: "registration_enabled", Value: "true", Type: "bool", Category: "general", Description: "Allow new users to register via Telegram", Label: "Registration Enabled"},

		// Support links (merged into general)
		{Key: "support_url", Value: "https://t.me/your_support_user", Type: "string", Category: "general", Description: "Link to support contact", Label: "Support URL"},
		{Key: "support_contact", Value: "", Type: "string", Category: "general", Description: "Telegram handle/link shown in the bot Support message", Label: "Support Contact"},
		{Key: "faq_url", Value: "", Type: "string", Category: "general", Description: "Link to FAQ page (optional)", Label: "FAQ URL"},
		{Key: "chat_enabled", Value: "false", Type: "bool", Category: "general", Description: "Allow users to chat with admin from the subscription panel", Label: "Chat"},
		// Maintenance mode (feature-gated, affects non-admin Telegram writes + public HTTP writes)
		{Key: "maintenance_mode_enabled", Value: "false", Type: "bool", Category: "maintenance", Description: "When enabled, all non-admin users see a maintenance notice; purchase/renew/mutation actions are blocked on both bot and web.", Label: "Global Maintenance Mode"},
		{Key: "maintenance_mode_message", Value: "", Type: "string", Category: "maintenance", Description: "Optional message shown to users during global maintenance. Leave empty to use the default translated notice.", Label: "Maintenance Message"},
		{Key: "maintenance_mode_since", Value: "", Type: "string", Category: "maintenance", Description: "RFC3339 timestamp automatically set when global maintenance is enabled. Not user-editable.", Label: "Maintenance Since"},

		// Telegram
		{Key: "telegram_bot_enabled", Value: "true", Type: "bool", Category: "telegram", Description: "Enable or disable the Telegram bot (disabling silently drops all updates)", Label: "Bot Enabled"},
		{Key: "welcome_message", Value: "Welcome to our bot! Use /start to begin.", Type: "string", Category: "telegram", Description: "Message sent to new users", Label: "Welcome Message"},

		// Security (JWT)
		{Key: "jwt_access_expiry", Value: "60", Type: "int", Category: "security", Description: "Access token expiry in minutes", Label: "Access Token Expiry (min)"},
		{Key: "jwt_refresh_expiry", Value: "168", Type: "int", Category: "security", Description: "Refresh token expiry in hours", Label: "Refresh Token Expiry (hours)"},

		// Agent Connection (uTLS)
		{Key: "utls_enabled", Value: "true", Type: "bool", Category: "agent", Description: "Enable uTLS fingerprint mimicry for agent connections to bypass DPI", Label: "Enable uTLS"},
		{Key: "utls_fingerprint", Value: "chrome", Type: "string", Category: "agent", Description: "TLS fingerprint to mimic (chrome, firefox, safari, edge, ios, android, random)", Label: "TLS Fingerprint"},
		{Key: "utls_sni_override", Value: "meet.google.com", Type: "string", Category: "agent", Description: "Camouflage SNI domain for agent connections (e.g. www.google.com). Leave empty to use actual server address.", Label: "SNI Camouflage Domain"},

		// Agent Xray Version
		{Key: "xray_default_version", Value: "26.2.6", Type: "string", Category: "agent", Description: "Default xray-core version installed on new nodes (without v prefix)", Label: "Default Xray Version"},
		{Key: "xray_auto_download", Value: "true", Type: "bool", Category: "agent", Description: "Auto-download xray binaries from GitHub when version changes", Label: "Auto Download Xray"},

		// Subscription
		{Key: "max_active_subscriptions", Value: "3", Type: "int", Category: "subscription", Description: "Maximum active subscriptions per user", Label: "Max Active Subscriptions"},
		{Key: "sub_panel_auth_enabled", Value: "false", Type: "bool", Category: "subscription", Description: "Require a password to access subscription panel pages", Label: "Panel Authentication"},
		{Key: "sub_panel_password", Value: "", Type: "string", Category: "subscription", Description: "Default password for all subscription panels (when panel auth is enabled)", Label: "Default Panel Password", Sensitive: true},
		{Key: "data_warning_threshold", Value: "80", Type: "int", Category: "subscription", Description: "Percentage of data usage to trigger a warning notification", Label: "Data Warning Threshold (%)"},
		{Key: "expiry_warning_days", Value: "3", Type: "int", Category: "subscription", Description: "Days before expiry to send a warning notification", Label: "Expiry Warning Days"},

		// Notification (merged into subscription)
		{Key: "notify_expiry", Value: "true", Type: "bool", Category: "subscription", Description: "Send notification when a subscription is about to expire", Label: "Notify on Expiry"},
		{Key: "notify_data_warning", Value: "true", Type: "bool", Category: "subscription", Description: "Send notification when data usage reaches the warning threshold", Label: "Notify on Data Warning"},
		{Key: "notify_data_exhausted", Value: "true", Type: "bool", Category: "subscription", Description: "Send notification when subscription data is fully consumed", Label: "Notify on Data Exhausted"},

		// Appearance
		{Key: "sub_profile_title", Value: "", Type: "string", Category: "appearance", Description: "Custom title shown in client subscription profiles", Label: "Subscription Profile Title"},
		{Key: "sub_update_interval", Value: "24", Type: "int", Category: "appearance", Description: "How often clients should check for subscription updates (in hours)", Label: "Subscription Update Interval (hours)"},
		{Key: "sub_support_url", Value: "", Type: "string", Category: "appearance", Description: "Support URL shown in client subscription profiles", Label: "Subscription Support URL"},

		// Data Retention — Time-series (node metrics + aggregations)
		{Key: "retention_node_stats_days", Value: "30", Type: "int", Category: "data", Description: "How many days to keep per-node CPU/memory/disk stat snapshots (0 = keep forever). Drives the node performance sparkline and history view.", Label: "Node Stats Retention (days)"},
		{Key: "retention_node_daily_traffic_days", Value: "365", Type: "int", Category: "data", Description: "How many days to keep per-node daily uplink/downlink totals (0 = keep forever). One row per node per UTC day — low volume; 365 keeps a full year of traffic history.", Label: "Node Daily Traffic Retention (days)"},
		{Key: "retention_node_uptime_events_days", Value: "90", Type: "int", Category: "data", Description: "How many days to keep node online/offline transition events (0 = keep forever). Can grow fast during network flapping; shorter retention is usually fine.", Label: "Node Uptime Events Retention (days)"},
		{Key: "retention_starlink_stats_days", Value: "30", Type: "int", Category: "data", Description: "How many days to keep Starlink dish telemetry samples (0 = keep forever). High sample rate, so retention here is typically shortest.", Label: "Starlink Stats Retention (days)"},
		{Key: "retention_online_users_history_days", Value: "7", Type: "int", Category: "data", Description: "How many days to keep global online-user history snapshots (0 = keep forever). Used by the dashboard online-users chart.", Label: "Online Users History Retention (days)"},
		{Key: "retention_access_log_days", Value: "30", Type: "int", Category: "data", Description: "How many days to keep hourly access-log summaries (0 = keep forever). Drives the per-user request/domain history.", Label: "Access Log Retention (days)"},
		{Key: "access_log_grace_minutes", Value: "90", Type: "int", Category: "data", Description: "Minutes the agent waits after an hour ends before shipping that hour's access log summary. Higher = fewer dropped late entries, more staleness on the Access History page. Lower = fresher data, but xray write-buffer lag and log rotation gaps may cause edge log lines to be dropped. 0 (or empty) keeps the agent's built-in default (90 minutes). Range 1–1440 (24h).", Label: "Access Log Grace Window (minutes)"},
		{Key: "access_log_max_domains_per_hour", Value: "100", Type: "int", Category: "data", Description: "Top-N cap on accepted domains stored per (subscription email, node, hour). Higher captures more long-tail domains; lower shrinks JSON blob size and search scan cost. Lowering does NOT shrink already-persisted rows. Range 1–500.", Label: "Accepted Domains per Hour"},
		{Key: "access_log_max_rejected_domains_per_hour", Value: "20", Type: "int", Category: "data", Description: "Top-N cap on rejected domains stored per (subscription email, node, hour). Smaller default than accepted because reject volume is typically narrower (block lists). Lowering does NOT shrink already-persisted rows. Range 1–500.", Label: "Rejected Domains per Hour"},
		{Key: "access_log_max_source_ips_per_hour", Value: "100", Type: "int", Category: "data", Description: "Top-N cap on source IPs stored per (subscription email, node, hour). Drives the source-IP rollup on the Access History page. Lowering does NOT shrink already-persisted rows. Range 1–500.", Label: "Source IPs per Hour"},

		// Data Retention — Subscription activity
		{Key: "retention_ip_days", Value: "30", Type: "int", Category: "data", Description: "How many days to keep subscription connected-IP records (0 = keep forever). Drives the Connected IPs list on the subscription sheet.", Label: "Subscription IPs Retention (days)"},
		{Key: "retention_subscription_daily_usage_days", Value: "365", Type: "int", Category: "data", Description: "How many days to keep per-subscription daily usage deltas (0 = keep forever). Drives the subscription usage history sparkline.", Label: "Subscription Daily Usage Retention (days)"},
		{Key: "retention_user_daily_usage_days", Value: "365", Type: "int", Category: "data", Description: "How many days to keep per-user aggregated daily usage snapshots (0 = keep forever).", Label: "User Daily Usage Retention (days)"},

		// Data Retention — Operational
		{Key: "retention_audit_logs_days", Value: "90", Type: "int", Category: "data", Description: "How many days to keep admin-action audit log entries (0 = keep forever).", Label: "Audit Log Retention (days)"},
		{Key: "retention_provisioning_tasks_days", Value: "14", Type: "int", Category: "data", Description: "How many days to keep completed provisioning tasks (0 = keep forever). Failed/pending tasks are retained regardless.", Label: "Provisioning Task Retention (days)"},
		{Key: "retention_notification_logs_days", Value: "30", Type: "int", Category: "data", Description: "How many days to keep sent-notification records (0 = keep forever). Used to dedupe notifications; shorter retention means duplicates may re-send after this window.", Label: "Notification Log Retention (days)"},
		{Key: "retention_alert_events_days", Value: "180", Type: "int", Category: "data", Description: "How many days to keep alerting event history (0 = keep forever). Alert rules continue firing regardless — only old fire events are pruned.", Label: "Alert Events Retention (days)"},
		{Key: "retention_chat_messages_days", Value: "0", Type: "int", Category: "data", Description: "How many days to keep admin↔user chat messages (0 = keep forever).", Label: "Chat Message Retention (days)"},

		// Notification — Channel Configuration
		{Key: "notification_discord_webhook_url", Value: "", Type: "string", Category: "notification", Description: "Discord webhook URL for notifications", Label: "Discord Webhook URL", Sensitive: true},
		{Key: "notification_webhook_url", Value: "", Type: "string", Category: "notification", Description: "Generic webhook URL for notifications", Label: "Webhook URL", Sensitive: true},
		{Key: "notification_webhook_secret", Value: "", Type: "string", Category: "notification", Description: "HMAC-SHA256 secret for webhook signature verification", Label: "Webhook Secret", Sensitive: true},

		// Notification — Telegram Toggles
		{Key: "notification_telegram_node_online", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when a server comes online", Label: "Server Online"},
		{Key: "notification_telegram_node_offline", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when a server goes offline", Label: "Server Offline"},
		{Key: "notification_telegram_node_created", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a server is created", Label: "Server Created"},
		{Key: "notification_telegram_node_deleted", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a server is deleted", Label: "Server Deleted"},
		{Key: "notification_telegram_subscription_created", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when a new subscription is created", Label: "Subscription Created"},
		{Key: "notification_telegram_subscription_renewed", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a subscription is renewed", Label: "Subscription Renewed"},
		{Key: "notification_telegram_subscription_cancelled", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a subscription is cancelled", Label: "Subscription Cancelled"},
		{Key: "notification_telegram_subscription_expired", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a subscription expires", Label: "Subscription Expired"},
		{Key: "notification_telegram_user_registered", Value: "false", Type: "bool", Category: "notification", Description: "Send Telegram notification when a new user registers", Label: "User Registered"},
		{Key: "notification_telegram_system_alert", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification for system alerts", Label: "System Alert"},
		{Key: "notification_telegram_xray_down", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when xray process crashes on a server", Label: "Xray Down"},
		{Key: "notification_telegram_xray_up", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when xray process recovers after a crash", Label: "Xray Recovered"},
		{Key: "notification_telegram_xray_crash_loop", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when xray enters a crash loop", Label: "Xray Crash Loop"},
		{Key: "notification_telegram_xray_recovery_command", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when crash recovery command executes on a server", Label: "Xray Recovery Command"},
		{Key: "notification_telegram_xray_recovery_exhausted", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when crash recovery attempts are exhausted", Label: "Xray Recovery Exhausted"},

		// Notification — Discord Toggles
		{Key: "notification_discord_node_online", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a server comes online", Label: "Server Online"},
		{Key: "notification_discord_node_offline", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a server goes offline", Label: "Server Offline"},
		{Key: "notification_discord_node_created", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a server is created", Label: "Server Created"},
		{Key: "notification_discord_node_deleted", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a server is deleted", Label: "Server Deleted"},
		{Key: "notification_discord_subscription_created", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a new subscription is created", Label: "Subscription Created"},
		{Key: "notification_discord_subscription_renewed", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a subscription is renewed", Label: "Subscription Renewed"},
		{Key: "notification_discord_subscription_cancelled", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a subscription is cancelled", Label: "Subscription Cancelled"},
		{Key: "notification_discord_subscription_expired", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a subscription expires", Label: "Subscription Expired"},
		{Key: "notification_discord_user_registered", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a new user registers", Label: "User Registered"},
		{Key: "notification_discord_system_alert", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification for system alerts", Label: "System Alert"},
		{Key: "notification_discord_xray_down", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when xray process crashes on a server", Label: "Xray Down"},
		{Key: "notification_discord_xray_up", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when xray process recovers after a crash", Label: "Xray Recovered"},
		{Key: "notification_discord_xray_crash_loop", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when xray enters a crash loop", Label: "Xray Crash Loop"},
		{Key: "notification_discord_xray_recovery_command", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when crash recovery command executes on a server", Label: "Xray Recovery Command"},
		{Key: "notification_discord_xray_recovery_exhausted", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when crash recovery attempts are exhausted", Label: "Xray Recovery Exhausted"},

		// Notification — Webhook Toggles
		{Key: "notification_webhook_node_online", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a server comes online", Label: "Server Online"},
		{Key: "notification_webhook_node_offline", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a server goes offline", Label: "Server Offline"},
		{Key: "notification_webhook_node_created", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a server is created", Label: "Server Created"},
		{Key: "notification_webhook_node_deleted", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a server is deleted", Label: "Server Deleted"},
		{Key: "notification_webhook_subscription_created", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a new subscription is created", Label: "Subscription Created"},
		{Key: "notification_webhook_subscription_renewed", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a subscription is renewed", Label: "Subscription Renewed"},
		{Key: "notification_webhook_subscription_cancelled", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a subscription is cancelled", Label: "Subscription Cancelled"},
		{Key: "notification_webhook_subscription_expired", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a subscription expires", Label: "Subscription Expired"},
		{Key: "notification_webhook_user_registered", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a new user registers", Label: "User Registered"},
		{Key: "notification_webhook_system_alert", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification for system alerts", Label: "System Alert"},
		{Key: "notification_webhook_xray_down", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when xray process crashes on a server", Label: "Xray Down"},
		{Key: "notification_webhook_xray_up", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when xray process recovers after a crash", Label: "Xray Recovered"},
		{Key: "notification_webhook_xray_crash_loop", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when xray enters a crash loop", Label: "Xray Crash Loop"},
		{Key: "notification_webhook_xray_recovery_command", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when crash recovery command executes on a server", Label: "Xray Recovery Command"},
		{Key: "notification_webhook_xray_recovery_exhausted", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when crash recovery attempts are exhausted", Label: "Xray Recovery Exhausted"},

		// Notification — Chat toggles
		{Key: "notification_telegram_chat_new_message", Value: "true", Type: "bool", Category: "notification", Description: "Send Telegram notification when a new chat message is received", Label: "New Chat Message"},
		{Key: "notification_discord_chat_new_message", Value: "false", Type: "bool", Category: "notification", Description: "Send Discord notification when a new chat message is received", Label: "New Chat Message"},
		{Key: "notification_webhook_chat_new_message", Value: "false", Type: "bool", Category: "notification", Description: "Send webhook notification when a new chat message is received", Label: "New Chat Message"},

		// Xray Monitoring
		{Key: "xray_crash_loop_threshold", Value: "3", Type: "int", Category: "xray_monitoring", Description: "Number of crashes before switching to summary notification mode", Label: "Crash Loop Threshold"},
		{Key: "xray_crash_loop_cooldown", Value: "5", Type: "int", Category: "xray_monitoring", Description: "Minutes after crash loop summary before re-alerting", Label: "Crash Loop Cooldown (min)"},
		{Key: "xray_auto_disable_enabled", Value: "false", Type: "bool", Category: "xray_monitoring", Description: "Automatically stop xray on a node after too many consecutive failures", Label: "Auto-Disable Enabled"},
		{Key: "xray_auto_disable_max_failures", Value: "10", Type: "int", Category: "xray_monitoring", Description: "Consecutive failures before auto-disabling xray on the node", Label: "Auto-Disable Max Failures"},
		{Key: "xray_stability_period", Value: "5", Type: "int", Category: "xray_monitoring", Description: "Minutes of continuous xray running to reset crash counter", Label: "Stability Period (min)"},

		// Outbound proxy (SOCKS5 — hub-side outgoing traffic, per-feature opt-in)
		{Key: "outbound_proxy_url", Value: "", Type: "string", Category: "server",
			Label:           "Outbound proxy URL",
			Description:     "SOCKS5 proxy URL for selected hub outbound features. Format: socks5h://user:pass@host:1080. Leave empty to disable. socks5h tunnels DNS; socks5 resolves locally.",
			RequiresRestart: false},
		{Key: "proxy_use_telegram", Value: "false", Type: "bool", Category: "server",
			Label:           "Route Telegram via proxy",
			Description:     "When enabled, Telegram bot API calls use outbound_proxy_url. Falls back to telegram_proxy_* if those are enabled and outbound URL is empty. Bot reconnects on hub restart.",
			RequiresRestart: false},
		{Key: "proxy_use_geofiles", Value: "false", Type: "bool", Category: "server",
			Label:           "Route geofile downloads via proxy",
			Description:     "When enabled, geoip.dat / geosite.dat fetches use outbound_proxy_url.",
			RequiresRestart: false},
		{Key: "proxy_use_xray_binary", Value: "false", Type: "bool", Category: "server",
			Label:           "Route xray binary fetch via proxy",
			Description:     "When enabled, xray-core release downloads from GitHub use outbound_proxy_url.",
			RequiresRestart: false},
		{Key: "proxy_use_github_api", Value: "false", Type: "bool", Category: "server",
			Label:           "Route GitHub API via proxy",
			Description:     "When enabled, GitHub API calls (release lists) use outbound_proxy_url.",
			RequiresRestart: false},
		{Key: "proxy_use_webhooks", Value: "false", Type: "bool", Category: "server",
			Label:           "Route notification webhooks via proxy",
			Description:     "When enabled, Discord and generic webhook deliveries use outbound_proxy_url.",
			RequiresRestart: false},
		{Key: "proxy_use_geoip", Value: "false", Type: "bool", Category: "server",
			Label:           "Route GeoIP lookups via proxy",
			Description:     "When enabled, ip-api.com geo-lookups use outbound_proxy_url.",
			RequiresRestart: false},
		{Key: "proxy_use_acme", Value: "false", Type: "bool", Category: "server",
			Label:           "Route ACME (Let's Encrypt) via proxy",
			Description:     "When enabled, LE HTTPS API calls use outbound_proxy_url. WARNING: DNS-01 TXT/A lookups use the system resolver and are NOT proxied (SOCKS5 limitation).",
			RequiresRestart: false},
		{Key: "proxy_use_wizard", Value: "false", Type: "bool", Category: "server",
			Label:           "Route wizard/updater traffic via proxy",
			Description:     "When enabled, set OUTBOUND_PROXY_URL env var before running nasnet-tool. The wizard binary runs as a separate process and cannot read DB settings live; env var is the integration point. This toggle is documentation only.",
			RequiresRestart: false},

		// Router health probes (only used in router mode)
		{Key: "router_probe_targets_domestic", Value: `[{"address":"217.218.155.155:53","proto":"dns"},{"address":"178.22.122.100:53","proto":"dns"}]`,
			Type: "json", Category: "router",
			Label:       "Domestic probe targets",
			Description: "Addresses the domestic WAN must reach to count as online. Foreign IPs are filtered on this uplink, so keep these Iranian. Proto: tcp or dns."},
		{Key: "router_probe_targets_foreign", Value: `[{"address":"1.1.1.1:443","proto":"tcp"},{"address":"8.8.8.8:443","proto":"tcp"}]`,
			Type: "json", Category: "router",
			Label:       "Foreign probe targets",
			Description: "Addresses the secondary uplink and the VPN tunnel must reach. The kill switch only lets probes through to these exact IPs."},
		{Key: "router_degraded_loss_pct", Value: "25", Type: "int", Category: "router",
			Label:       "Degraded loss threshold (%)",
			Description: "Probe loss over the last 100 seconds that marks an uplink degraded. Display and event only, never reroutes."},
		{Key: "router_degraded_rtt_ms_domestic", Value: "300", Type: "int", Category: "router",
			Label:       "Degraded RTT, domestic (ms)",
			Description: "Median probe RTT that marks the domestic WAN degraded."},
		{Key: "router_degraded_rtt_ms_foreign", Value: "800", Type: "int", Category: "router",
			Label:       "Degraded RTT, foreign (ms)",
			Description: "Median probe RTT that marks the secondary uplink degraded. Higher floor: satellite latency is normal."},
		{Key: "router_failover_domestic_to_vpn", Value: "true", Type: "bool", Category: "router",
			Label:       "Failover domestic traffic to the VPN",
			Description: "When the domestic ISP loses internet, send Iranian-destined traffic through the tunnel until it recovers. Turn off if your domestic services reject foreign IPs."},
	}

	// Server/Infrastructure settings seeded from environment config
	if u.initialCfg != nil {
		defaults = append(defaults, u.buildEnvSettings()...)
	}

	for _, s := range defaults {
		existing, err := u.repo.GetByKey(ctx, s.Key)
		if err != nil {
			// Setting doesn't exist, create it
			if err := u.repo.Update(ctx, s); err != nil {
				return err
			}
		} else {
			needsUpdate := false
			// Sync the sensitive flag if it changed
			if existing.Sensitive != s.Sensitive {
				existing.Sensitive = s.Sensitive
				needsUpdate = true
			}
			// Restore value if it was corrupted by masking
			if isMaskedValue(existing.Value) && s.Value != "" {
				existing.Value = s.Value
				needsUpdate = true
			}
			// Migrate category if it changed (consolidation: support→general, notification→subscription, telegram_bot_token server→telegram)
			if existing.Category != s.Category {
				existing.Category = s.Category
				needsUpdate = true
			}
			if needsUpdate {
				if err := u.repo.Update(ctx, existing); err != nil {
					return err
				}
			}
		}
	}

	// Migrate legacy category for maintenance-mode settings (originally seeded under "general").
	for _, key := range []string{"maintenance_mode_enabled", "maintenance_mode_message", "maintenance_mode_since"} {
		if existing, err := u.repo.GetByKey(ctx, key); err == nil && existing != nil && existing.Category != "maintenance" {
			existing.Category = "maintenance"
			_ = u.repo.Update(ctx, existing)
		}
	}

	return nil
}

// buildEnvSettings returns the setting definitions whose values come from
// environment variables (InitialConfig). These are deployment-specific
// settings that should match the current server's .env configuration.
func (u *settingUsecase) buildEnvSettings() []*domain.Setting {
	if u.initialCfg == nil {
		return nil
	}
	boolStr := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	return []*domain.Setting{
		{Key: "app_port", Value: fmt.Sprintf("%d", u.initialCfg.AppPort), Type: "int", Category: "server", Description: "HTTP server port", Label: "API Port", RequiresRestart: true},
		{Key: "app_base_url", Value: u.initialCfg.BaseURL, Type: "string", Category: "server", Description: "Public base URL for subscription links", Label: "Base URL"},
		{Key: "sub_panel_url", Value: u.initialCfg.SubPanelURL, Type: "string", Category: "server", Description: "Subscription panel URL for browser redirects", Label: "Sub Panel URL"},
		{Key: "telegram_bot_token", Value: u.initialCfg.BotToken, Type: "string", Category: "telegram", Description: "Telegram Bot API token", Label: "Telegram Bot Token", RequiresRestart: true, Sensitive: true},
		{Key: "acme_enabled", Value: boolStr(u.initialCfg.ACMEEnabled), Type: "bool", Category: "server", Description: "Enable HTTPS via Let's Encrypt automatic certificates", Label: "ACME Enabled", RequiresRestart: true},
		{Key: "acme_email", Value: u.initialCfg.ACMEEmail, Type: "string", Category: "server", Description: "Email for Let's Encrypt ACME certificates", Label: "ACME Email", RequiresRestart: true},
		{Key: "acme_staging", Value: boolStr(u.initialCfg.ACMEStaging), Type: "bool", Category: "server", Description: "Use Let's Encrypt staging environment", Label: "ACME Staging", RequiresRestart: true},
		{Key: "acme_auto_renew", Value: boolStr(u.initialCfg.ACMEAutoRenew), Type: "bool", Category: "server", Description: "Automatically renew ACME certificates", Label: "ACME Auto Renew"},
		{Key: "tls_cert_file", Value: u.initialCfg.TLSCertFile, Type: "string", Category: "server", Description: "Path to TLS certificate PEM file (for manual HTTPS without ACME)", Label: "TLS Certificate File", RequiresRestart: true},
		{Key: "tls_key_file", Value: u.initialCfg.TLSKeyFile, Type: "string", Category: "server", Description: "Path to TLS private key PEM file (for manual HTTPS without ACME)", Label: "TLS Key File", RequiresRestart: true},
		{Key: "metrics_enabled", Value: "false", Type: "bool", Category: "server", Description: "Enable Prometheus metrics collection and /metrics endpoint", Label: "Metrics Enabled"},
		{Key: "metrics_username", Value: u.initialCfg.MetricsUsername, Type: "string", Category: "server", Description: "Username for /metrics Basic Auth (empty = public)", Label: "Metrics Username", RequiresRestart: true},
		{Key: "metrics_password", Value: u.initialCfg.MetricsPassword, Type: "string", Category: "server", Description: "Password for /metrics Basic Auth (empty = public)", Label: "Metrics Password", RequiresRestart: true, Sensitive: true},
		{Key: "log_level", Value: u.initialCfg.LogLevel, Type: "string", Category: "server", Description: "Application log level (debug, info, warn, error)", Label: "Log Level"},
		{Key: "panel_base_path", Value: u.initialCfg.PanelBasePath, Type: "string", Category: "server", Description: "URL path prefix for the admin panel (e.g., /x7k2m9). Requires restart.", Label: "Panel Base Path", RequiresRestart: true},
		{Key: "telegram_proxy_enabled", Value: boolStr(u.initialCfg.ProxyEnabled), Type: "bool", Category: "telegram", Description: "Route Telegram API traffic through a SOCKS5 proxy", Label: "Proxy Enabled", RequiresRestart: true},
		{Key: "telegram_proxy_type", Value: u.initialCfg.ProxyType, Type: "string", Category: "telegram", Description: "Proxy protocol type", Label: "Proxy Type", RequiresRestart: true},
		{Key: "telegram_proxy_host", Value: u.initialCfg.ProxyHost, Type: "string", Category: "telegram", Description: "Proxy server hostname or IP address", Label: "Proxy Host", RequiresRestart: true},
		{Key: "telegram_proxy_port", Value: fmt.Sprintf("%d", u.initialCfg.ProxyPort), Type: "int", Category: "telegram", Description: "Proxy server port", Label: "Proxy Port", RequiresRestart: true},
		{Key: "telegram_proxy_username", Value: u.initialCfg.ProxyUsername, Type: "string", Category: "telegram", Description: "Proxy authentication username (optional)", Label: "Proxy Username", RequiresRestart: true, Sensitive: true},
		{Key: "telegram_proxy_password", Value: u.initialCfg.ProxyPassword, Type: "string", Category: "telegram", Description: "Proxy authentication password (optional)", Label: "Proxy Password", RequiresRestart: true, Sensitive: true},
	}
}

// ReseedEnvSettings force-overwrites deployment-specific settings in the
// database with values from the current environment. This ensures that after
// a backup restore, the panel's URLs, ports, and tokens match the current
// installation rather than the source of the backup.
func (u *settingUsecase) ReseedEnvSettings(ctx context.Context) error {
	envSettings := u.buildEnvSettings()
	if len(envSettings) == 0 {
		return nil
	}

	log := logger.GetLogger()
	for _, s := range envSettings {
		existing, err := u.repo.GetByKey(ctx, s.Key)
		if err != nil {
			// Setting doesn't exist — create it
			if err := u.repo.Update(ctx, s); err != nil {
				return err
			}
			continue
		}
		// Overwrite value, category, and metadata from the env definition
		existing.Value = s.Value
		existing.Category = s.Category
		existing.Sensitive = s.Sensitive
		if err := u.repo.Update(ctx, existing); err != nil {
			return err
		}
		log.WithField("key", s.Key).Debug("Reseeded env setting after restore")
	}
	return nil
}

// MigrateGlobalPanelPassword checks whether the stored global panel password
// is plaintext (does not start with "$2", a bcrypt prefix) and hashes it if
// so. This is a one-time migration that runs on startup to transition existing
// installations from plaintext to bcrypt storage.
func (u *settingUsecase) MigrateGlobalPanelPassword(ctx context.Context) {
	pw, err := u.GetByKey(ctx, "sub_panel_password")
	if err != nil || pw == "" {
		return
	}
	if !strings.HasPrefix(pw, "$2") {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			logger.GetLogger().WithError(err).Warn("Failed to hash global panel password during migration")
			return
		}
		if err := u.repo.UpdateMany(ctx, []*domain.Setting{
			{Key: "sub_panel_password", Value: string(hash)},
		}); err != nil {
			logger.GetLogger().WithError(err).Warn("Failed to persist hashed global panel password")
		}
	}
}
