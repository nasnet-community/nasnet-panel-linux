/*
 * The Settings page's content: its sections, the cards in each, and what a
 * card's rows show and say about saving. The page, search, the phone list and
 * the ⌘K palette all read from here.
 */
import {
    Activity, Archive, Bell, BellRing, Clock, Cpu, Database, FileClock, Fingerprint, Globe, Headphones,
    History, KeyRound, LifeBuoy, Link2, Lock, Network, Radio, Route, Router, Send, Server, Shield, Split, Users, Wrench,
    type LucideIcon,
} from "lucide-react"
import type { SettingsStatus } from "@/lib/api/settings"
import { formatBytes } from "@/lib/utils"
import { strategyLabel } from "@/lib/vpn-labels"
import type { PoolStrategy } from "@/lib/types/network"

export type SectionId = "service" | "telegram" | "panel" | "router" | "nodes" | "security" | "data"

export interface SectionDef {
    id: SectionId
    title: string
    icon: LucideIcon
    description: string
}

export const SECTIONS: SectionDef[] = [
    { id: "service", title: "Service", icon: Users, description: "Maintenance, the subscription page and how customers reach you" },
    { id: "telegram", title: "Telegram & alerts", icon: Send, description: "The bot, and where admin alerts go" },
    { id: "panel", title: "Panel", icon: Server, description: "Addresses, HTTPS, reverse connections and the outbound proxy" },
    { id: "router", title: "Router", icon: Router, description: "Failover, and where uplink checks and the VPN pool are set" },
    { id: "nodes", title: "Nodes", icon: Cpu, description: "How the panel talks to nodes and watches Xray" },
    { id: "security", title: "Security", icon: Shield, description: "The admin login and how long it lasts" },
    { id: "data", title: "Data", icon: Database, description: "Backups and how long history is kept" },
]

/** Saved values by key. Secrets arrive masked: asterisks, then the last 4 characters. */
export type Values = Record<string, string>

export interface Ctx {
    values: Values
    status?: SettingsStatus
}

export type FieldType = "bool" | "text" | "number" | "select" | "secret" | "textarea" | "multi" | "readonly"

export interface FieldDef {
    /** the setting it edits; readonly rows have none */
    key?: string
    label: string
    type: FieldType
    mono?: boolean
    copy?: boolean
    dir?: "auto"
    unit?: string
    min?: number
    max?: number
    placeholder?: string
    hint?: string | ((ctx: Ctx) => string | undefined)
    onText?: string
    offText?: string
    options?: readonly (readonly [string, string])[]
    /** multi: one bool setting per item */
    items?: readonly (readonly [string, string])[]
    /** secret stored as a hash: it can be replaced but never shown */
    hashed?: boolean
    /** secret that may be removed */
    clearable?: boolean
    /** shown only when this holds, against the draft while editing */
    show?: (v: Values) => boolean
    editOnly?: boolean
    /** readonly rows: the value to show */
    text?: (ctx: Ctx) => string
    /** read view of a keyed value */
    format?: (v: string) => string
    /** set in .env, under this name */
    env?: string
    link?: { label: string; to: string }
    /** a card it points to */
    goto?: string
}

export type Tone = "ok" | "warn" | "danger"

export interface StatusLine {
    tone: Tone | "off"
    text: string
    hint?: string
}

export interface Effect {
    text: string
    danger?: boolean
}

export type CardKind = "fields" | "maintenance" | "https" | "events" | "retention" | "password"

export interface CardDef {
    id: string
    section: SectionId
    title: string
    icon: LucideIcon
    description: string
    keywords?: string
    kind?: CardKind
    fields?: FieldDef[]
    /** while this switch is off the card reads as one line */
    master?: { key: string; label: string; off: string }
    status?: (ctx: Ctx) => StatusLine | null
    note?: (ctx: Ctx) => { text: string; goto?: string; warn?: boolean } | null
    effect?: (draft: Values, saved: Values, changed: string[], ctx: Ctx) => Effect | null
    validate?: (draft: Values, saved: Values) => Record<string, string>
    link?: { label: string; to: string }
    /** wording for the confirm a danger effect asks for */
    confirm?: { title: string; label: string }
}

/** Settings that take effect only when the panel restarts. */
export const RESTART_KEYS = new Set([
    "panel_base_path", "acme_enabled", "acme_email", "acme_staging", "tls_cert_file", "tls_key_file",
    "hub_enabled", "hub_port", "metrics_username", "metrics_password",
    "telegram_proxy_enabled", "telegram_proxy_type", "telegram_proxy_host", "telegram_proxy_port",
    "telegram_proxy_username", "telegram_proxy_password",
])

export const on = (v: string | undefined) => v === "true"
export const isSet = (v: string | undefined) => !!v
const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`

/** "13 Oct, 16:43" in the browser's time zone. */
export function when(iso?: string): string {
    if (!iso) return ""
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return ""
    return d.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12: false })
}

export function day(iso?: string): string {
    if (!iso) return ""
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
}

/** "8 days", "5 hours" since a moment. */
export function since(iso?: string): string {
    if (!iso) return ""
    const ms = Date.now() - new Date(iso).getTime()
    if (!(ms >= 0)) return ""
    const h = Math.floor(ms / 3_600_000)
    if (h < 1) return plural(Math.max(1, Math.floor(ms / 60_000)), "minute")
    if (h < 48) return plural(h, "hour")
    return plural(Math.floor(h / 24), "day")
}

const URL_RE = /^https?:\/\/[^\s/]+/i
const urlError = (v: string) => (v && !URL_RE.test(v) ? "Starts with http:// or https://" : "")

export const EVENTS: { group: string; items: readonly (readonly [string, string])[] }[] = [
    { group: "Servers", items: [["node_online", "Server online"], ["node_offline", "Server offline"], ["node_created", "Server created"], ["node_deleted", "Server deleted"]] },
    { group: "Xray", items: [["xray_down", "Xray down"], ["xray_up", "Xray recovered"], ["xray_crash_loop", "Crash loop"]] },
    { group: "Subscriptions", items: [["subscription_created", "Subscription created"], ["subscription_renewed", "Renewed"], ["subscription_cancelled", "Cancelled"], ["subscription_expired", "Expired"]] },
    { group: "Other", items: [["user_registered", "User registered"], ["system_alert", "System alert"], ["chat_new_message", "New chat message"]] },
]

export type Channel = "telegram" | "discord" | "webhook"
export const CHANNEL_LABEL: Record<Channel, string> = { telegram: "Telegram", discord: "Discord", webhook: "Webhook" }
/** Channels that can deliver: Telegram always, the others once they have a URL. */
export const liveChannels = (v: Values): Channel[] =>
    ["telegram" as const, ...(isSet(v.notification_discord_webhook_url) ? ["discord" as const] : []), ...(isSet(v.notification_webhook_url) ? ["webhook" as const] : [])]
export const eventKey = (ch: Channel, ev: string) => `notification_${ch}_${ev}`

/** Datasets on the Retention card, in the order shown. */
export const RETENTION: { group: string; rows: readonly (readonly [string, string])[] }[] = [
    { group: "Node metrics", rows: [
        ["retention_starlink_stats_days", "Starlink stats"],
        ["retention_node_stats_days", "Node stats"],
        ["retention_online_users_history_days", "Online users"],
        ["retention_access_log_days", "Access log"],
        ["retention_starlink_events_days", "Starlink events"],
        ["retention_node_uptime_events_days", "Node uptime events"],
        ["retention_node_daily_uptime_days", "Node daily uptime"],
        ["retention_node_daily_traffic_days", "Node daily traffic"],
    ] },
    { group: "Subscriptions", rows: [
        ["retention_ip_days", "Subscription IPs"],
        ["retention_user_daily_usage_days", "User daily usage"],
        ["retention_subscription_daily_usage_days", "Subscription daily usage"],
    ] },
    { group: "Operational", rows: [
        ["retention_provisioning_tasks_days", "Provisioning tasks"],
        ["retention_alert_events_days", "Alert events"],
        ["retention_audit_logs_days", "Audit log"],
        ["retention_chat_messages_days", "Chat messages"],
        ["retention_notification_logs_days", "Notification log"],
        ["retention_hub_lifecycle_events_days", "Panel runs"],
    ] },
]

function backupStatus(ctx: Ctx): StatusLine | null {
    const b = ctx.status?.backup
    const r = b?.result
    if (r) {
        const at = when(r.at)
        const size = r.size_bytes ? formatBytes(r.size_bytes) : ""
        if (r.ok && r.sent) return { tone: "ok", text: `Last run ${at} · sent${size ? ` (${size})` : ""}` }
        if (r.sent) return { tone: "warn", text: `Last run ${at} · sent to some admins, not all`, hint: r.reason }
        if (r.reason === "too_big") return { tone: "warn", text: `Last run ${at} · not sent: ${size} is over Telegram's 50 MB limit`, hint: "The plain dump stays on the server. Telegram bots can't send files over 50 MB." }
        if (r.reason === "bot_off") return { tone: "warn", text: `Last run ${at} · not sent: the bot isn't running`, hint: "The plain dump stays on the server." }
        return { tone: "danger", text: `Last run ${at} failed`, hint: r.reason }
    }
    if (b?.last_run_at) return { tone: "off", text: `Last run ${when(b.last_run_at)}` }
    return null
}

function httpsStatus(ctx: Ctx): StatusLine | null {
    const h = ctx.status?.https
    if (!h) return null
    if (h.mode === "plain") return { tone: h.error ? "danger" : "off", text: h.error ? "Serving plain HTTP: the certificate didn't load" : "Serving plain HTTP", hint: h.error ?? "Fine when a reverse proxy in front handles TLS." }
    const c = h.cert
    if (!c) return { tone: "off", text: h.mode === "acme" ? "Let's Encrypt" : "Certificate files" }
    const left = c.days_left
    const text = `Serving ${c.issuer || "a certificate"} · expires ${day(c.not_after)}, ${left < 0 ? "expired" : `in ${plural(left, "day")}`}`
    if (h.on_disk) return { tone: "warn", text, hint: `A renewed certificate (expires ${day(h.on_disk.not_after)}) is on disk. Restart the panel to serve it.` }
    return {
        tone: left < 14 ? "warn" : "ok",
        text,
        hint: h.mode === "files" ? "The panel reads the files at startup, so it needs a restart after each renewal." : "Renews on its own before it expires.",
    }
}

export const CARDS: CardDef[] = [
    // ---------- Service ----------
    {
        id: "maintenance", section: "service", title: "Maintenance", icon: Wrench, kind: "maintenance",
        description: "Pauses changes from the bot and subscription pages for everyone but admins.", keywords: "maintenance notice broadcast pause",
        master: { key: "maintenance_mode_enabled", label: "Maintenance", off: "Customers can make changes as usual." },
        fields: [
            { key: "maintenance_mode_enabled", label: "Maintenance", type: "bool" },
            { key: "maintenance_mode_message", label: "Notice", type: "textarea", dir: "auto", placeholder: "Empty uses the default translated notice", show: v => on(v.maintenance_mode_enabled) },
        ],
        status: ({ values, status }) => on(values.maintenance_mode_enabled)
            ? { tone: "warn", text: `On${status?.maintenance.since ? ` since ${when(status.maintenance.since)}` : ""}`, hint: "Customers can't make changes from the bot or their subscription page." }
            : null,
    },
    {
        id: "subscription-page", section: "service", title: "Subscription page", icon: Globe,
        description: "The web page behind each subscription link.", keywords: "panel password",
        fields: [
            { key: "sub_panel_auth_enabled", label: "Password", type: "bool", onText: "Required", offText: "Not required" },
            { key: "sub_panel_password", label: "Default password", type: "secret", hashed: true, show: v => on(v.sub_panel_auth_enabled) },
        ],
        validate: (d, s): Record<string, string> => on(d.sub_panel_auth_enabled) && !d.sub_panel_password && !s.sub_panel_password ? { sub_panel_password: "Set a password first" } : {},
        effect: (d, s) => on(d.sub_panel_auth_enabled) && !on(s.sub_panel_auth_enabled)
            ? { text: "Customers need the password to open their subscription page." }
            : { text: "Applies the next time a subscription page is opened." },
    },
    {
        id: "support-chat", section: "service", title: "Support & chat", icon: Headphones,
        description: "How customers reach you.", keywords: "contact support chat",
        fields: [
            { key: "support_contact", label: "Support contact", type: "text", hint: "Shown in the bot's Support message" },
            { key: "chat_enabled", label: "Chat", type: "bool", hint: "Customers can message you from their subscription page" },
        ],
        effect: () => ({ text: "Applies right away." }),
    },

    // ---------- Telegram & alerts ----------
    {
        id: "bot", section: "telegram", title: "Bot", icon: Send,
        description: "The customer and admin bot.", keywords: "telegram token proxy",
        confirm: { title: "Stop the bot answering?", label: "Stop answering" },
        status: ({ status }) => {
            const b = status?.bot
            if (!b) return null
            if (b.state === "running") return { tone: "ok", text: `Running as @${b.username}`, hint: b.since ? `Up since ${when(b.since)}` : undefined }
            if (b.state === "rejected") return { tone: "danger", text: "Telegram refused the token", hint: "Fix TELEGRAM_BOT_TOKEN in .env and restart the panel." }
            if (b.state === "starting") return { tone: "warn", text: "Connecting to Telegram…", hint: b.error }
            return { tone: "off", text: "Turned off in .env", hint: "TELEGRAM_ENABLED=false" }
        },
        fields: [
            { key: "telegram_bot_enabled", label: "Answering", type: "bool", offText: "Off · the bot ignores every message" },
            {
                label: "Traffic", type: "readonly", goto: "outbound-proxy",
                text: ({ status }) => status?.bot.route === "outbound_proxy" ? "Through the outbound proxy" : status?.bot.route === "legacy_proxy" ? `Through ${status.bot.legacy}` : "Direct",
            },
            { label: "Token", type: "readonly", env: "TELEGRAM_BOT_TOKEN", text: ({ status }) => (status?.bot.token_set === false ? "Not set" : "••••••••") },
            // the legacy proxy only applies while no outbound proxy is set
            { key: "telegram_proxy_enabled", label: "Legacy proxy", type: "bool", show: v => !v.outbound_proxy_url },
            { key: "telegram_proxy_host", label: "Proxy host", type: "text", mono: true, show: v => !v.outbound_proxy_url && on(v.telegram_proxy_enabled) },
            { key: "telegram_proxy_port", label: "Proxy port", type: "number", mono: true, min: 1, max: 65535, show: v => !v.outbound_proxy_url && on(v.telegram_proxy_enabled) },
            { key: "telegram_proxy_username", label: "Proxy user", type: "secret", clearable: true, show: v => !v.outbound_proxy_url && on(v.telegram_proxy_enabled) },
            { key: "telegram_proxy_password", label: "Proxy password", type: "secret", clearable: true, show: v => !v.outbound_proxy_url && on(v.telegram_proxy_enabled) },
        ],
        effect: (d, s, changed) => {
            if (!on(d.telegram_bot_enabled) && on(s.telegram_bot_enabled)) return { danger: true, text: "The bot stops answering everyone, admins included, within 30 seconds." }
            if (changed.some(k => RESTART_KEYS.has(k))) return { text: "The proxy changes after a panel restart." }
            return { text: "The bot picks it up within 30 seconds." }
        },
    },
    {
        id: "alert-channels", section: "telegram", title: "Alert channels", icon: Bell,
        description: "Where admin alerts go.", keywords: "notification discord webhook",
        fields: [
            { label: "Telegram", type: "readonly", env: "ADMIN_IDS", text: ({ status }) => status ? `The admins · ${plural(status.admin.chats, "chat")}` : "The admins" },
            { key: "notification_discord_webhook_url", label: "Discord", type: "secret", clearable: true, placeholder: "https://discord.com/api/webhooks/…" },
            { key: "notification_webhook_url", label: "Webhook", type: "secret", clearable: true, placeholder: "https://" },
            { key: "notification_webhook_secret", label: "Signing secret", type: "secret", clearable: true, placeholder: "Signs each delivery with HMAC-SHA256", show: v => isSet(v.notification_webhook_url) },
        ],
        effect: () => ({ text: "Applies to the next alert." }),
    },
    {
        id: "alert-events", section: "telegram", title: "Alert events", icon: BellRing, kind: "events",
        description: "Which events send an alert.", keywords: "notification events alerts " + EVENTS.flatMap(g => g.items.map(i => i[1])).join(" "),
        effect: () => ({ text: "Applies to the next event." }),
    },

    // ---------- Panel ----------
    {
        id: "urls", section: "panel", title: "URLs & endpoints", icon: Link2,
        description: "Where the panel is reached.", keywords: "base url domain address path port agent hub",
        fields: [
            { key: "app_base_url", label: "Base URL", type: "text", mono: true, hint: "In subscription links and deploy scripts" },
            { key: "agent_hub_url", label: "Address for nodes", type: "text", mono: true, placeholder: "Empty uses the base URL", hint: "Remote nodes reach the panel here. Set it when the base URL is a LAN address; one-line installs need an https:// address." },
            { key: "sub_panel_url", label: "Subscription page", type: "text", mono: true },
            { key: "panel_base_path", label: "Panel path", type: "text", mono: true },
            { label: "API port", type: "readonly", mono: true, env: "APP_PORT", text: ({ status }) => (status ? String(status.hub.port) : "") },
        ],
        validate: d => {
            const e: Record<string, string> = {}
            if (urlError(d.app_base_url)) e.app_base_url = urlError(d.app_base_url)
            if (urlError(d.sub_panel_url)) e.sub_panel_url = urlError(d.sub_panel_url)
            if (d.panel_base_path && !d.panel_base_path.startsWith("/")) e.panel_base_path = "Starts with /"
            return e
        },
        effect: (_d, _s, changed) => changed.includes("panel_base_path")
            ? { text: "Takes effect after a panel restart. The panel then moves to the new path." }
            : { text: "New links and deploy scripts use it right away. Links already sent keep the old address." },
    },
    {
        id: "https", section: "panel", title: "HTTPS", icon: Lock, kind: "https",
        description: "How the panel serves TLS.", keywords: "tls ssl certificate lets encrypt acme",
        status: httpsStatus,
        effect: () => ({ text: "Takes effect after a panel restart." }),
    },
    {
        id: "reverse-hub", section: "panel", title: "Reverse connections", icon: Network,
        description: "Lets nodes behind NAT dial in.", keywords: "grpc reverse nat port hub",
        status: ({ status, values }) => {
            const r = status?.reverse_hub
            if (!r) return null
            if (r.listening) return { tone: "ok", text: `Listening on :${r.port} · ${plural(r.connected, "reverse node")} connected` }
            if (on(values.hub_enabled)) return { tone: "warn", text: "Not listening", hint: "It starts on the next panel restart, or check the panel log." }
            return null
        },
        master: { key: "hub_enabled", label: "Reverse connections", off: "Nodes are reached directly only." },
        fields: [
            { key: "hub_enabled", label: "Reverse connections", type: "bool" },
            { key: "hub_port", label: "Port", type: "number", mono: true, min: 1, max: 65535, show: v => on(v.hub_enabled) },
        ],
        effect: () => ({ text: "Takes effect after a panel restart. Reverse nodes reconnect on their own." }),
    },
    {
        id: "outbound-proxy", section: "panel", title: "Outbound proxy", icon: Route,
        description: "A SOCKS5 proxy for the panel's own outgoing traffic.", keywords: "socks socks5 proxy",
        fields: [
            { key: "outbound_proxy_url", label: "Proxy URL", type: "secret", clearable: true, placeholder: "socks5h://user:pass@host:1080" },
            {
                label: "Used for", type: "multi", items: [
                    ["proxy_use_telegram", "Telegram"], ["proxy_use_geofiles", "Geo files"], ["proxy_use_xray_binary", "Xray downloads"],
                    ["proxy_use_core_binaries", "Tunnel downloads"], ["proxy_use_geoip", "GeoIP lookups"], ["proxy_use_github_api", "GitHub API"],
                    ["proxy_use_webhooks", "Webhooks"], ["proxy_use_acme", "Let's Encrypt"],
                ],
            },
        ],
        note: ({ values }) => !values.outbound_proxy_url && Object.keys(values).some(k => k.startsWith("proxy_use_") && k !== "proxy_use_wizard" && on(values[k]))
            ? { text: "No proxy URL is set, so everything ticked here connects directly.", warn: true }
            : null,
        effect: (d, s, changed, ctx) => {
            const newRoute = changed.includes("outbound_proxy_url") && !s.outbound_proxy_url && on(d.proxy_use_telegram) && ctx.status?.bot.route !== "outbound_proxy"
            return { text: newRoute ? "New connections use it right away. The bot moves to it after a panel restart." : "New connections use it right away." }
        },
    },
    {
        id: "logging", section: "panel", title: "Logging & metrics", icon: Activity,
        description: "What the panel writes down and exposes.", keywords: "log level prometheus metrics",
        fields: [
            { key: "log_level", label: "Log level", type: "select", options: [["debug", "Debug"], ["info", "Info"], ["warn", "Warn"], ["error", "Error"]] },
            { key: "metrics_enabled", label: "Prometheus", type: "bool", hint: ({ status }) => (status?.metrics.endpoint ? "Served at /metrics" : "The /metrics endpoint also needs METRICS_ENABLED in .env") },
            { key: "metrics_username", label: "Metrics user", type: "text", placeholder: "Empty = no sign-in", show: v => on(v.metrics_enabled) },
            { key: "metrics_password", label: "Metrics password", type: "secret", clearable: true, show: v => on(v.metrics_enabled) },
        ],
        effect: (_d, _s, changed) => changed.some(k => RESTART_KEYS.has(k))
            ? { text: "The metrics sign-in changes after a panel restart." }
            : { text: "Applies right away." },
    },

    // ---------- Router ----------
    {
        id: "failover", section: "router", title: "Failover", icon: Split,
        description: "What happens to domestic traffic when the domestic line is down.", keywords: "failover domestic vpn iran isp",
        fields: [
            { key: "router_failover_domestic_to_vpn", label: "Domestic over the VPN", type: "bool", onText: "While the domestic line is down", offText: "Never", hint: "Turn off if your domestic services refuse foreign addresses." },
        ],
        effect: (d, s) => !on(d.router_failover_domestic_to_vpn) && on(s.router_failover_domestic_to_vpn)
            ? { text: "Domestic traffic waits for the domestic line instead of using the VPN." }
            : { text: "Applies at the next health check, within seconds." },
    },
    {
        id: "uplinks", section: "router", title: "Uplinks", icon: Router,
        description: "Set on the Router page, next to each line.", keywords: "probe checks targets degraded rtt loss pool strategy port mapping upnp",
        fields: [
            { label: "Connection checks", type: "readonly", text: () => "Per line, under Checks", link: { label: "Open Router", to: "/router" } },
            { label: "VPN pool", type: "readonly", text: ({ values }) => strategyLabel((values.router_vpn_pool_strategy || "spread") as PoolStrategy), link: { label: "Open Router", to: "/router" } },
            { label: "Port mapping", type: "readonly", text: ({ values }) => (on(values.router_portmap_enabled) ? "On" : "Off"), link: { label: "Open Router", to: "/router" } },
        ],
    },

    // ---------- Nodes ----------
    {
        id: "node-connectivity", section: "nodes", title: "Node connectivity", icon: Radio,
        description: "When a node counts as offline.", keywords: "offline debounce outage",
        fields: [{ key: "node_offline_debounce_sec", label: "Offline after", type: "number", unit: "seconds of failures", min: 0, hint: "Absorbs flapping links. The outage is recorded from when it started." }],
        effect: () => ({ text: "Applies within 30 seconds." }),
    },
    {
        id: "agent-certificates", section: "nodes", title: "Agent certificates", icon: KeyRound,
        description: "The certificates nodes use to talk to the panel.", keywords: "rotate certificate mtls strict",
        master: { key: "agent_cert_auto_rotate", label: "Auto-rotate", off: "Certificates are rotated by hand on Certificates." },
        fields: [
            { key: "agent_cert_auto_rotate", label: "Auto-rotate", type: "bool" },
            { key: "agent_cert_rotate_days", label: "Rotate at", type: "number", unit: "days left", min: 7, max: 90, show: v => on(v.agent_cert_auto_rotate) },
            { label: "Strict mode", type: "readonly", text: () => "Set on Certificates", link: { label: "Open", to: "/certificates" } },
        ],
        effect: () => ({ text: "Applies at the next rotation check." }),
    },
    {
        id: "utls", section: "nodes", title: "uTLS", icon: Fingerprint,
        description: "Makes agent connections look like a browser.", keywords: "fingerprint sni tls camouflage",
        master: { key: "utls_enabled", label: "uTLS", off: "Agent connections use Go's own TLS fingerprint." },
        fields: [
            { key: "utls_enabled", label: "uTLS", type: "bool" },
            { key: "utls_fingerprint", label: "Fingerprint", type: "select", show: v => on(v.utls_enabled), options: [["chrome", "Chrome"], ["firefox", "Firefox"], ["safari", "Safari"], ["edge", "Edge"], ["ios", "iOS"], ["android", "Android"], ["random", "Random"]] },
            { key: "utls_sni_override", label: "SNI", type: "text", mono: true, show: v => on(v.utls_enabled) },
        ],
        effect: () => ({ text: "New agent connections use it within 5 minutes." }),
    },
    {
        id: "xray-monitoring", section: "nodes", title: "Xray monitoring", icon: LifeBuoy,
        description: "What happens when Xray keeps crashing.", keywords: "crash loop auto disable cooldown",
        fields: [
            { key: "xray_crash_loop_threshold", label: "Crash loop at", type: "number", unit: "crashes", min: 1 },
            { key: "xray_stability_period", label: "Counter resets", type: "number", unit: "minutes stable", min: 1 },
            { key: "xray_crash_loop_cooldown", label: "Alert cooldown", type: "number", unit: "minutes", min: 1 },
            { key: "xray_auto_disable_enabled", label: "Auto-disable", type: "bool" },
            { key: "xray_auto_disable_max_failures", label: "Disable after", type: "number", unit: "failures", min: 1, show: v => on(v.xray_auto_disable_enabled) },
        ],
        effect: () => ({ text: "Applies within a minute." }),
    },
    {
        id: "mtproto-nodes", section: "nodes", title: "MTProto nodes", icon: Globe,
        description: "Where MTProto nodes get Telegram's address list.", keywords: "mtproto telegram cidr relay allowlist",
        fields: [
            { key: "relay_cidr_url", label: "Address list", type: "text", mono: true, placeholder: "Telegram's own list", format: v => v === "off" ? "Built-in list only" : v || "Telegram's own list", hint: "Refreshed daily and only adds ranges. \"off\" keeps the built-in list." },
        ],
        validate: (d): Record<string, string> => (d.relay_cidr_url && d.relay_cidr_url !== "off" && urlError(d.relay_cidr_url) ? { relay_cidr_url: urlError(d.relay_cidr_url) } : {}),
        effect: () => ({ text: "Applies at the next daily refresh." }),
    },

    // ---------- Security ----------
    {
        id: "panel-password", section: "security", title: "Panel password", icon: Lock, kind: "password",
        description: "The admin login.", keywords: "password login admin username",
    },
    {
        id: "sessions", section: "security", title: "Sessions", icon: Clock,
        description: "How long a sign-in lasts.", keywords: "jwt token expiry session",
        fields: [
            { key: "jwt_access_expiry", label: "Access token", type: "number", unit: "minutes", min: 1 },
            { key: "jwt_refresh_expiry", label: "Stay signed in", type: "number", unit: "hours", min: 1, format: v => (Number(v) >= 48 && Number(v) % 24 === 0 ? `${Number(v) / 24} days (${v} h)` : `${v} hours`) },
        ],
        effect: () => ({ text: "Applies at the next sign-in." }),
    },

    // ---------- Data ----------
    {
        id: "backups", section: "data", title: "Backups", icon: Archive,
        description: "Encrypted database backups, sent on Telegram.", keywords: "backup passphrase dump",
        status: backupStatus,
        link: { label: "Open backups", to: "/backup" },
        master: { key: "backup_enabled", label: "Scheduled", off: "Backups run only when you start one." },
        fields: [
            { key: "backup_enabled", label: "Scheduled", type: "bool" },
            { key: "backup_interval_hours", label: "Every", type: "number", unit: "hours", min: 1, show: v => on(v.backup_enabled) },
            { key: "backup_retention_count", label: "Keep", type: "number", unit: "local copies", min: 0, hint: "0 keeps every copy", show: v => on(v.backup_enabled) },
            { key: "backup_chat_id", label: "Send to", type: "text", mono: true, placeholder: "Empty sends to the admins", format: v => v || "The admins (ADMIN_IDS)", show: v => on(v.backup_enabled) },
            { key: "backup_passphrase", label: "Passphrase", type: "secret", show: v => on(v.backup_enabled) },
        ],
        validate: (d, s): Record<string, string> => on(d.backup_enabled) && !d.backup_passphrase && !s.backup_passphrase ? { backup_passphrase: "Backups need a passphrase" } : {},
        effect: () => ({ text: "Applies from the next run." }),
    },
    {
        id: "retention", section: "data", title: "Retention", icon: History, kind: "retention",
        description: "How long each kind of history is kept.", keywords: "retention cleanup history delete " + RETENTION.flatMap(g => g.rows.map(r => r[1])).join(" "),
        effect: () => ({ text: "The next cleanup removes what falls outside the new windows. It runs every 6 hours." }),
    },
    {
        id: "access-history", section: "data", title: "Access history", icon: FileClock,
        description: "How agents summarise access logs.", keywords: "access log domains grace caps",
        fields: [
            { key: "access_log_grace_minutes", label: "Grace window", type: "number", unit: "minutes", min: 0, max: 1440, hint: "How long agents hold a finished hour before sending it" },
            { key: "access_log_max_domains_per_hour", label: "Domains", type: "number", unit: "per hour", min: 1, max: 500 },
            { key: "access_log_max_rejected_domains_per_hour", label: "Rejected domains", type: "number", unit: "per hour", min: 1, max: 500 },
            { key: "access_log_max_source_ips_per_hour", label: "Source IPs", type: "number", unit: "per hour", min: 1, max: 500 },
        ],
        effect: () => ({ text: "Agents pick it up with their next report." }),
    },
]

export const cardsIn = (section: SectionId) => CARDS.filter(c => c.section === section)
export const cardById = (id: string) => CARDS.find(c => c.id === id)

/** Every key a card saves. */
export function cardKeys(card: CardDef, values?: Values): string[] {
    if (card.kind === "events") return liveChannels(values ?? {}).flatMap(ch => EVENTS.flatMap(g => g.items.map(([ev]) => eventKey(ch, ev))))
    if (card.kind === "retention") return RETENTION.flatMap(g => g.rows.map(r => r[0]))
    if (card.kind === "https") return ["acme_enabled", "acme_email", "acme_staging", "tls_cert_file", "tls_key_file"]
    const keys: string[] = []
    for (const f of card.fields ?? []) {
        if (f.key) keys.push(f.key)
        if (f.items) keys.push(...f.items.map(i => i[0]))
    }
    return keys
}

export function cardForKey(key: string): CardDef | undefined {
    return CARDS.find(c => cardKeys(c).includes(key))
}

/** Cards holding a saved, restart-only change the panel hasn't picked up. */
export function pendingCards(status?: SettingsStatus): CardDef[] {
    const out: CardDef[] = []
    for (const key of status?.restart_pending ?? []) {
        const c = cardForKey(key)
        if (c && !out.includes(c)) out.push(c)
    }
    return out
}

/** What a section needs from you, in a few words, or nothing. */
export function sectionNote(id: SectionId, ctx: Ctx): string | undefined {
    const s = ctx.status
    if (id === "panel") {
        const pending = pendingCards(s).filter(c => c.section === "panel")
        if (pending.length) return `Restart pending · ${pending.map(c => c.title).join(", ")}`
        if (s?.https.on_disk) return "Renewed certificate waits for a restart"
        if (s?.https.cert && s.https.cert.days_left < 14) return `Certificate expires in ${plural(Math.max(0, s.https.cert.days_left), "day")}`
    }
    if (id === "data") {
        const r = s?.backup.result
        if (r && !r.ok) return r.sent ? "Last backup reached some admins" : r.reason === "too_big" || r.reason === "bot_off" ? "Last backup not sent" : "Last backup failed"
    }
    if (id === "telegram" && s && (s.bot.state === "rejected" || s.bot.state === "starting")) return "The bot isn't running"
    if (id === "service" && on(ctx.values.maintenance_mode_enabled)) return "Maintenance is on"
    const elsewhere = pendingCards(s).filter(c => c.section === id)
    if (elsewhere.length) return `Restart pending · ${elsewhere.map(c => c.title).join(", ")}`
    return undefined
}

/** The tail a masked secret keeps, or "" when unset. */
export function secretTail(v: string | undefined): string {
    if (!v) return ""
    return v.replace(/^\*+/, "").slice(-4)
}

/** Lowercase text search over a card: its name, rows and keywords. */
export function cardHaystack(card: CardDef): string {
    const parts = [card.title, card.description, card.keywords ?? ""]
    for (const f of card.fields ?? []) {
        parts.push(f.label, typeof f.hint === "string" ? f.hint : "")
        if (f.items) parts.push(...f.items.map(i => i[1]))
        if (f.options) parts.push(...f.options.map(o => o[1]))
    }
    return parts.join(" ").toLowerCase()
}

/** Which rows of a card match a search: all of them, some, or none. */
export function matchCard(card: CardDef, q: string, values: Values): "all" | Set<string> | null {
    const query = q.trim().toLowerCase()
    if (!query) return "all"
    const has = (s?: string) => !!s && s.toLowerCase().includes(query)
    if (has(card.title) || has(card.description) || has(card.keywords)) return "all"
    if (card.kind && card.kind !== "fields" && card.kind !== "maintenance") return cardHaystack(card).includes(query) ? "all" : null
    const rows = new Set<string>()
    for (const f of card.fields ?? []) {
        const value = f.key && f.type !== "secret" ? values[f.key] : ""
        if (has(f.label) || has(typeof f.hint === "string" ? f.hint : "") || has(value) || f.items?.some(i => has(i[1])) || f.options?.some(o => has(o[1]))) {
            rows.add(f.key ?? f.label)
        }
    }
    return rows.size ? rows : null
}
