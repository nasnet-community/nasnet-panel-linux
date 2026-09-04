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
    datacenter: string
    api_port: number
    agent_port: number
    connect_mode: "direct" | "reverse"
    is_stealth: boolean
    is_persistent_stealth?: boolean
    is_active: boolean
    is_online: boolean
    last_check: string
    log_level?: string
    inbounds?: Inbound[]
    routing_settings?: RoutingSettings
    dns_settings?: DNSSettings
    bandwidth_settings?: BandwidthSettings
    starlink_settings?: StarlinkSettings
    crash_recovery_settings?: CrashRecoverySettings
    last_crash_recovery?: LastCrashRecovery
    outbound_test_settings?: OutboundTestSettings
    enable_access_log?: boolean
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
    timestamp: string
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

export interface CrashRecoverySettings {
    enabled: boolean
    command?: string
    command_timeout?: number
    cooldown?: number
    max_attempts?: number
}

export interface LastCrashRecovery {
    timestamp: string
    exit_code: number
    stdout?: string
    stderr?: string
    success: boolean
    attempt_num: number
    max_attempts: number
    exhausted: boolean
    error?: string
}
