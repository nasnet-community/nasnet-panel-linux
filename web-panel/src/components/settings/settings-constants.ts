import type { IconType } from "react-icons"
import {
    HiOutlineCog,
    HiOutlineCreditCard,
    HiOutlineColorSwatch,
    HiOutlineShieldCheck,
    HiOutlineTicket,
    HiOutlineChip,
    HiOutlineServer,
    HiOutlineDatabase,
    HiOutlineBell,
    HiOutlineGlobeAlt,
    HiOutlineExclamationCircle,
} from "react-icons/hi"
import { LuRouter } from "react-icons/lu"
import { BsTelegram, BsDiscord } from "react-icons/bs"

export interface CategoryMeta {
    icon: IconType
    description: string
    order: number
    label?: string // Custom display label (defaults to capitalized key)
}

export interface FieldOverride {
    type: "textarea" | "select" | "url" | "port" | "duration" | "percentage" | "crypto_address"
    options?: string[]
    unit?: string // suffix for duration fields (e.g. "minutes", "hours", "days")
    placeholder?: string // overrides the type-default placeholder (e.g. "https://" for url)
}

export const categoryMeta: Record<string, CategoryMeta> = {
    server: {
        icon: HiOutlineServer,
        description: "Server infrastructure, URLs, and deployment settings",
        order: -1,
    },
    general: {
        icon: HiOutlineCog,
        description: "Core system settings like site name, currency, and support links",
        order: 0,
    },
    subscription: {
        icon: HiOutlineTicket,
        description: "Subscription limits, trial access, and notification preferences",
        order: 1,
    },
    payment: {
        icon: HiOutlineCreditCard,
        description: "Payment methods, wallet addresses, and card details",
        order: 2,
    },
    telegram: {
        icon: BsTelegram,
        description: "Telegram bot configuration and messages",
        order: 3,
    },
    notification: {
        icon: HiOutlineBell,
        description: "Configure admin notification channels and event toggles",
        order: 3.5,
        label: "Notifications",
    },
    appearance: {
        icon: HiOutlineColorSwatch,
        description: "Customize client-facing subscription profiles",
        order: 4,
        label: "Client Profile",
    },
    security: {
        icon: HiOutlineShieldCheck,
        description: "JWT token expiry and authentication settings",
        order: 5,
    },
    agent: {
        icon: HiOutlineChip,
        description: "Agent connection settings and TLS fingerprinting",
        order: 6,
    },
    data: {
        icon: HiOutlineDatabase,
        description: "Data retention and cleanup policies",
        order: 7,
        label: "Data Retention",
    },
    xray_monitoring: {
        icon: HiOutlineExclamationCircle,
        description: "Configure xray crash detection, notification throttling, and auto-disable behavior",
        order: 6.5,
        label: "Xray Monitoring",
    },
    router: {
        icon: LuRouter,
        description: "Uplink health probes, degraded thresholds, and failover behavior",
        order: 6.7,
        label: "Router",
    },
    maintenance: {
        icon: HiOutlineExclamationCircle,
        description: "Toggle service maintenance mode and notify active users",
        order: 0.5,
        label: "Maintenance",
    },
}

export const fieldOverrides: Record<string, FieldOverride> = {
    manual_payment_instruction: { type: "textarea" },
    welcome_message: { type: "textarea" },
    currency: { type: "select", options: ["USD", "EUR", "GBP", "IRR", "TRY", "RUB", "CNY", "AED"] },
    utls_fingerprint: { type: "select", options: ["chrome", "firefox", "safari", "edge", "ios", "android", "random"] },
    log_level: { type: "select", options: ["debug", "info", "warn", "error"] },
    // Notification URL fields
    notification_discord_webhook_url: { type: "url" },
    notification_webhook_url: { type: "url" },
    // URL fields
    app_base_url: { type: "url" },
    sub_panel_url: { type: "url" },
    support_url: { type: "url" },
    faq_url: { type: "url" },
    sub_support_url: { type: "url" },
    // Port field
    app_port: { type: "port" },
    // Telegram proxy fields
    telegram_proxy_type: { type: "select", options: ["socks5"] },
    telegram_proxy_port: { type: "port" },
    // Outbound proxy
    outbound_proxy_url: { type: "url" },
    // Duration fields
    jwt_access_expiry: { type: "duration", unit: "minutes" },
    jwt_refresh_expiry: { type: "duration", unit: "hours" },
    sub_update_interval: { type: "duration", unit: "hours" },
    expiry_warning_days: { type: "duration", unit: "days" },
    // Data-retention fields — all day-typed int settings where `0` means
    // "keep forever" (SettingField renders a pill in that case).
    retention_node_stats_days: { type: "duration", unit: "days" },
    retention_node_daily_traffic_days: { type: "duration", unit: "days" },
    retention_node_uptime_events_days: { type: "duration", unit: "days" },
    retention_starlink_stats_days: { type: "duration", unit: "days" },
    retention_online_users_history_days: { type: "duration", unit: "days" },
    retention_access_log_days: { type: "duration", unit: "days" },
    access_log_grace_minutes: { type: "duration", unit: "minutes" },
    retention_ip_days: { type: "duration", unit: "days" },
    retention_subscription_daily_usage_days: { type: "duration", unit: "days" },
    retention_user_daily_usage_days: { type: "duration", unit: "days" },
    retention_audit_logs_days: { type: "duration", unit: "days" },
    retention_provisioning_tasks_days: { type: "duration", unit: "days" },
    retention_notification_logs_days: { type: "duration", unit: "days" },
    retention_alert_events_days: { type: "duration", unit: "days" },
    retention_chat_messages_days: { type: "duration", unit: "days" },
    // Xray monitoring duration fields
    xray_crash_loop_cooldown: { type: "duration", unit: "minutes" },
    xray_stability_period: { type: "duration", unit: "minutes" },
    // Percentage fields
    data_warning_threshold: { type: "percentage" },
    // Crypto address fields
    crypto_usdt_address: { type: "crypto_address" },
    crypto_btc_address: { type: "crypto_address" },
    crypto_xmr_address: { type: "crypto_address" },
}

export const URL_KEYS = ["support_url", "faq_url", "app_base_url", "sub_panel_url", "sub_support_url"]

// Sub-group definitions for visual grouping within categories
export interface SubGroup {
    label: string
    description?: string
    keys: string[]
    component?: string
}

export const categorySubGroups: Record<string, SubGroup[]> = {
    server: [
        {
            label: "URLs & Endpoints",
            keys: ["app_base_url", "sub_panel_url", "panel_base_path"],
        },
        {
            label: "HTTPS",
            description: "Secure your server with TLS — choose automatic (Let's Encrypt) or manual certificates",
            keys: ["acme_enabled", "acme_email", "acme_staging", "acme_auto_renew", "tls_cert_file", "tls_key_file"],
            component: "https-mode",
        },
        {
            label: "Monitoring",
            description: "Prometheus metrics collection and endpoint",
            keys: ["metrics_enabled", "metrics_username", "metrics_password"],
        },
        {
            label: "Server Runtime",
            keys: ["app_port", "log_level"],
        },
        {
            label: "Outbound Proxy",
            description: "SOCKS5 proxy for selected hub-side outbound traffic. Each feature opts in individually. Falls back to direct connection (with warning log) if URL is empty or invalid.",
            keys: [
                "outbound_proxy_url",
                "proxy_use_telegram",
                "proxy_use_geofiles",
                "proxy_use_xray_binary",
                "proxy_use_github_api",
                "proxy_use_webhooks",
                "proxy_use_geoip",
                "proxy_use_acme",
                "proxy_use_crypto_rpc",
                "proxy_use_wizard",
            ],
        },
    ],
    general: [
        {
            label: "Site Identity",
            keys: ["site_name", "currency", "registration_enabled"],
        },
        {
            label: "Support Links",
            keys: ["support_url", "faq_url"],
        },
    ],
    subscription: [
        {
            label: "Limits",
            keys: ["max_active_subscriptions"],
        },
        {
            label: "Trial Access",
            keys: ["trial_enabled"],
        },
        {
            label: "Warning Thresholds",
            keys: ["data_warning_threshold", "expiry_warning_days"],
        },
        {
            label: "Notifications",
            keys: ["notify_expiry", "notify_data_warning", "notify_data_exhausted"],
        },
    ],
    payment: [
        {
            label: "Payment System",
            keys: ["payment_enabled", "manual_payment_instruction"],
        },
        {
            label: "Crypto Wallets",
            keys: ["crypto_usdt_address", "crypto_btc_address", "crypto_xmr_address"],
        },
        {
            label: "USDT Auto-Verify",
            keys: ["auto_verify_usdt_enabled", "bsc_rpc_url", "usdt_bep20_contract", "usdt_min_confirmations", "usdt_amount_tolerance_pct", "usdt_verify_timeout_min"],
        },
        {
            label: "Card Payment",
            keys: ["card_number", "card_holder", "usd_to_toman_rate"],
        },
    ],
    telegram: [
        {
            label: "Bot Configuration",
            keys: ["telegram_bot_token", "telegram_bot_enabled"],
        },
        {
            label: "Messages",
            keys: ["welcome_message"],
        },
        {
            label: "Proxy",
            description: "Route Telegram API traffic through a SOCKS5 proxy",
            keys: [
                "telegram_proxy_enabled",
                "telegram_proxy_type",
                "telegram_proxy_host",
                "telegram_proxy_port",
                "telegram_proxy_username",
                "telegram_proxy_password",
            ],
        },
    ],
    notification: [
        {
            label: "Channels",
            description: "Configure external notification delivery channels",
            keys: ["notification_discord_webhook_url", "notification_webhook_url", "notification_webhook_secret"],
        },
        {
            label: "Event Alerts",
            description: "Configure which events trigger notifications on each channel",
            keys: [],
            component: "notification-matrix",
        },
    ],
    appearance: [
        {
            label: "Profile Customization",
            keys: ["sub_profile_title", "sub_update_interval", "sub_support_url"],
        },
    ],
    agent: [
        {
            label: "uTLS Fingerprinting",
            description: "Mimic browser TLS fingerprints to bypass deep packet inspection",
            keys: ["utls_enabled", "utls_fingerprint", "utls_sni_override"],
        },
        {
            label: "Xray Core",
            keys: ["xray_default_version"],
        },
    ],
    data: [
        {
            label: "Time-series · node metrics",
            description:
                "How long to keep per-node telemetry (CPU, traffic, uptime, dish) and dashboard history charts. Set any field to 0 to keep forever.",
            keys: [
                "retention_node_stats_days",
                "retention_node_daily_traffic_days",
                "retention_node_uptime_events_days",
                "retention_starlink_stats_days",
                "retention_online_users_history_days",
                "retention_access_log_days",
            ],
        },
        {
            label: "Pipeline freshness",
            description:
                "How long the agent buffers a completed hour before shipping it. Higher tolerates xray write-buffer lag and log rotation gaps; lower puts data on the Access History page sooner. 0 = ship immediately (drops late entries).",
            keys: [
                "access_log_grace_minutes",
            ],
        },
        {
            label: "Per-hour top-N caps",
            description:
                "How many distinct domains and IPs each (subscription email, node, hour) row stores. Higher captures more long-tail values at the cost of larger JSON blobs and slower search scans. Lowering does NOT shrink rows already on disk — only new hours are affected. Range 1–500.",
            keys: [
                "access_log_max_domains_per_hour",
                "access_log_max_rejected_domains_per_hour",
                "access_log_max_source_ips_per_hour",
            ],
        },
        {
            label: "Subscription activity",
            description:
                "How long to keep per-subscription IP records and per-day usage deltas that drive the subscription sheet.",
            keys: [
                "retention_ip_days",
                "retention_subscription_daily_usage_days",
                "retention_user_daily_usage_days",
            ],
        },
        {
            label: "Operational",
            description:
                "Audit trail, provisioning task history, notification dedupe logs, alert event history, and chat messages.",
            keys: [
                "retention_audit_logs_days",
                "retention_provisioning_tasks_days",
                "retention_notification_logs_days",
                "retention_alert_events_days",
                "retention_chat_messages_days",
            ],
        },
    ],
    xray_monitoring: [
        {
            label: "Crash Detection",
            description: "Control when crashes are detected and when the counter resets",
            keys: ["xray_crash_loop_threshold", "xray_stability_period"],
        },
        {
            label: "Notification Throttling",
            description: "Prevent notification spam during crash loops",
            keys: ["xray_crash_loop_cooldown"],
        },
        {
            label: "Auto-Disable Safety",
            description: "Automatically stop xray after too many failures",
            keys: ["xray_auto_disable_enabled", "xray_auto_disable_max_failures"],
        },
    ],
    router: [
        {
            label: "Probe Targets",
            description: "What each uplink must reach to count as online",
            keys: ["router_probe_targets_domestic", "router_probe_targets_foreign"],
        },
        {
            label: "Degraded Thresholds",
            description: "When a working uplink gets flagged as lossy or slow",
            keys: [
                "router_degraded_loss_pct",
                "router_degraded_rtt_ms_domestic",
                "router_degraded_rtt_ms_foreign",
            ],
        },
        {
            label: "Failover",
            keys: ["router_failover_domestic_to_vpn"],
        },
    ],
}

// Helper to get display label for a category
export function getCategoryLabel(key: string): string {
    return categoryMeta[key]?.label || key.charAt(0).toUpperCase() + key.slice(1)
}

// Notification matrix configuration
export const notificationChannels = [
    { key: "telegram", label: "Telegram", icon: BsTelegram },
    { key: "discord", label: "Discord", icon: BsDiscord },
    { key: "webhook", label: "Webhook", icon: HiOutlineGlobeAlt },
] as const

export interface NotificationMatrixEvent {
    label: string
    key: string
}

export interface NotificationMatrixSection {
    label: string
    events: NotificationMatrixEvent[]
}

export const notificationMatrixSections: NotificationMatrixSection[] = [
    {
        label: "Servers",
        events: [
            { label: "Server Online", key: "node_online" },
            { label: "Server Offline", key: "node_offline" },
            { label: "Server Created", key: "node_created" },
            { label: "Server Deleted", key: "node_deleted" },
        ],
    },
    {
        label: "Xray Process",
        events: [
            { label: "Xray Down", key: "xray_down" },
            { label: "Xray Recovered", key: "xray_up" },
            { label: "Xray Crash Loop", key: "xray_crash_loop" },
            { label: "Xray Recovery Command", key: "xray_recovery_command" },
            { label: "Xray Recovery Exhausted", key: "xray_recovery_exhausted" },
        ],
    },
    {
        label: "Payments",
        events: [
            { label: "Payment Created", key: "payment_created" },
            { label: "Payment Completed", key: "payment_completed" },
            { label: "Payment Failed", key: "payment_failed" },
            { label: "Payment Refunded", key: "payment_refunded" },
        ],
    },
    {
        label: "Subscriptions",
        events: [
            { label: "Subscription Created", key: "subscription_created" },
            { label: "Subscription Renewed", key: "subscription_renewed" },
            { label: "Subscription Cancelled", key: "subscription_cancelled" },
            { label: "Subscription Expired", key: "subscription_expired" },
            { label: "Trial Activated", key: "subscription_trial_activated" },
        ],
    },
    {
        label: "Other",
        events: [
            { label: "User Registered", key: "user_registered" },
            { label: "System Alert", key: "system_alert" },
        ],
    },
    {
        label: "Chat",
        events: [
            { label: "New Chat Message", key: "chat_new_message" },
        ],
    },
]
