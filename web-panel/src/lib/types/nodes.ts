import type { Inbound } from "./inbounds"
import type { RoutingSettings } from "./routing"
import type { DNSSettings } from "./dns"
import type { OutboundTestSettings } from "./outbounds"

// Node Types
export interface Node {
    id: number
    uuid: string
    name: string
    ip: string
    country_code: string
    /** chosen on purpose; GeoIP leaves the country blank */
    no_flag?: boolean
    datacenter: string
    api_port: number
    agent_port: number
    connect_mode: "local" | "direct" | "reverse" | "link"
    /** A lite node is an MTProto node: no gRPC agent, driven over the panel's link. */
    kind?: "full" | "lite"
    is_stealth: boolean
    is_persistent_stealth?: boolean
    is_active: boolean
    is_online: boolean
    last_check: string
    /** Last recorded outage start; absent means no outage on record. */
    last_disconnected_at?: string | null
    last_connected_at?: string | null
    last_offline_reason?: NodeTransitionReason | ""
    log_access?: string
    log_error?: string
    log_dns?: boolean
    log_level?: string
    inbounds?: Inbound[]
    routing_settings?: RoutingSettings
    dns_settings?: DNSSettings
    bandwidth_settings?: BandwidthSettings
    starlink_settings?: StarlinkSettings
    outbound_test_settings?: OutboundTestSettings
    enable_access_log?: boolean
    last_access_log_synced_at?: string
    // System stats (populated from agent)
    cpu_usage?: number
    memory_used?: number
    memory_total?: number
    memory_percent?: number
    disk_used?: number
    disk_total?: number
    disk_percent?: number
    xray_uptime?: number
    xray_version?: string
    agent_version?: string
    maintenance_mode?: boolean
    maintenance_message?: string
    maintenance_since?: string | null
    created_at: string
    updated_at: string
}

// Node stats from backend API
export interface NodeStats {
    stats_unavailable?: boolean
    total_uplink: number
    total_downlink: number
    online_users: number
    uptime: number
    cpu_percent: number
    memory_percent: number
    memory_used_mb: number
    memory_total_mb: number
    disk_percent: number
    disk_used_gb: number
    disk_total_gb: number
    // Network rates (from agent)
    up_speed?: number
    down_speed?: number
    // Network details
    tcp_count?: number
    udp_count?: number
    fd_count?: number
    // Xray Process Stats
    xray_status?: string
    xray_pid?: number
    process_uptime?: number
    system_uptime?: number
    // Agent info
    agent_version?: string
    xray_version?: string
    // Account counts
    total_accounts?: number
    active_accounts?: number
    // Load averages
    load_avg_1?: number
    load_avg_5?: number
    load_avg_15?: number
    /** When the agent collected the system readings (ISO, agent clock). */
    sampled_at?: string
    /** How long ago the hub received those readings (hub clock). */
    reading_age_ms?: number
    /** Client-side: when this browser learned of the readings (its own clock). */
    received_at?: number
}

/** One fixed-width bucket of recorded stats; `t` is its start in unix seconds. */
export interface NodeStatBucket {
    t: number
    cpu_avg: number
    cpu_max: number
    mem_avg: number
    mem_max: number
    disk_avg: number
    up_avg: number
    up_max: number
    down_avg: number
    down_max: number
    tcp_avg: number
    udp_avg: number
    fd_avg: number
    load1_avg: number
    users_avg: number
    users_max: number
    samples: number
}

export type StatsRange = "1h" | "6h" | "24h" | "7d" | "30d"

/** Recorded stats for one chart range. Missing buckets are gaps. */
export interface NodeStatsSeries {
    range: StatsRange
    step_sec: number
    from: number
    to: number
    points: NodeStatBucket[]
}

export interface NodeOverview {
    connection: {
        /** "local" is the built-in node: in-process, no RTT. */
        mode: "local" | "direct" | "reverse" | "stealth"
        /** Latest heartbeat round trip; null when none is recent. */
        rtt_ms: number | null
    }
    agent: {
        version: string
        /** The agent this hub ships; empty when unknown. */
        bundled_version: string
        /** The hub ships a build this node doesn't run (by binary, not by version). */
        update_available: boolean
        /** SHA-256 of the running and the shipped binary; dev builds share a version. */
        build?: string
        bundled_build?: string
    }
    config: {
        state: "in_sync" | "drifted" | "unknown"
    }
    today: {
        uplink: number
        downlink: number
        /** Highest up+down rate over the last 24 hours, bytes/s. */
        peak_bps: number
    }
}

export interface NodeHostInfo {
    hostname: string
    os: string
    platform: string
    platform_family: string
    platform_version: string
    kernel_version: string
    arch: string
    virtualization_system: string
    virtualization_role: string
    cpu_model_name: string
    cpu_cores: number
    total_memory: number // bytes
    total_swap: number   // bytes
    boot_time: number
    /** When the agent reported this (ISO). */
    fetched_at?: string
    /** The agent couldn't be asked; this is the last stored copy. */
    stale?: boolean
}

export interface SSHStatus {
    enabled: boolean
    port: number
    is_active: boolean
}

export interface XrayUser {
    email: string
    uuid: string
    level: number
    alter_id?: number
    traffic: number
    uplink: number
    downlink: number
}

export interface InboundUsers {
    inbound_tag: string
    protocol: string
    port: number
    users: XrayUser[]
}

export interface NodeDataPoint {
    id: number
    node_id: number
    cpu: number
    memory: number
    disk: number
    up_speed: number
    down_speed: number
    tcp_count?: number
    udp_count?: number
    fd_count?: number
    load_avg_1?: number
    created_at: string
}

export interface NodeDailyTraffic {
    id: number
    node_id: number
    date: string
    uplink: number
    downlink: number
    created_at: string
}

export interface NodeUptimeEvent {
    id: number
    node_id: number
    status: "online" | "offline"
    /** Why the transition happened; absent on rows written before reasons existed. */
    reason?: NodeTransitionReason
    /** Underlying error or actor, for display only. */
    detail?: string
    /** When the state actually changed (outage start), not when it was confirmed. */
    timestamp: string
}

/** Closed set, mirroring the server's transition reasons. */
export type NodeTransitionReason =
    | "heartbeat_timeout"
    | "agent_unreachable"
    | "health_check_failed"
    | "hub_shutdown"
    | "admin_disabled"
    | "maintenance"
    | "heartbeat_ok"
    | "agent_reachable"
    | "admin_enabled"
    | "hub_start"
    /** Built-in node only: the panel (and with it xray) wasn't running. */
    | "panel_down"

/** One offline stretch, paired server-side from the transition events. */
export interface NodeOutage {
    started_at: string
    ended_at?: string
    duration_sec: number
    reason?: NodeTransitionReason
    detail?: string
    /** Operator-caused (disable, maintenance) — shown, but not counted as downtime. */
    planned: boolean
    /** Still down; duration_sec grows between requests. */
    ongoing: boolean
}

export interface NodeDailyUptime {
    id: number
    node_id: number
    date: string
    observed_sec: number
    downtime_sec: number
    planned_sec: number
    outage_count: number
}

export interface NodeUptimeSummary {
    days: NodeDailyUptime[]
    observed_sec: number
    downtime_sec: number
    planned_sec: number
    outage_count: number
    /** Measured against observed time only — panel-down windows are excluded. */
    uptime_pct: number
    last_outage_at?: string
}

/** A window in which the panel wasn't running, so nothing observed the nodes. */
export interface HubGap {
    from: string
    to: string
}

export interface BandwidthSettings {
    enabled: boolean
    interface?: string   // e.g. "eth0"
    total_bw?: number    // total link bandwidth in Mbps
}

export interface StarlinkSettings {
    enabled: boolean
    dish_address?: string
}

