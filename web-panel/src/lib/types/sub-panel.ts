export interface SubPanelData {
    status: string
    label: string
    plan_name: string
    plan_duration: number
    product_type: string
    data_used: number
    data_limit: number
    data_used_display: string
    data_limit_display: string
    data_remaining_display: string
    data_usage_percent: number
    is_unlimited: boolean
    days_remaining: number
    time_remaining: string
    time_used_percent: number
    start_date: string | null
    end_date: string | null
    is_custom_expiry: boolean
    is_custom_data_limit: boolean
    telegram_chat_id: number
    telegram_connected: boolean
    subscription_url: string
    config_id_masked: string
    created_at: string
    is_online: boolean
    online_count: number
    online_ips?: string[]
    last_active_at: string | null
    servers: SubPanelServer[]
    chat_enabled: boolean
    telegram_bot_username?: string
}

export type UsageTrendRange = "7d" | "30d"

export type UsageTrendPoint = {
    date: string                    // YYYY-MM-DD (UTC calendar day)
    upload: number | null
    download: number | null
    total: number
}

export type UsageTrendResponse = {
    range: UsageTrendRange
    points: UsageTrendPoint[]
    unit_hint: "KB" | "MB" | "GB"
}

export interface SubPanelExhaustionPrediction {
    data_limit: number
    data_used: number
    data_remaining: number
    daily_avg_bytes: number
    days_remaining: number
    end_date: string | null
    days_until_expiry: number
    exhaustion_date: string | null
    will_exhaust_first: boolean
    usage_trend: "increasing" | "decreasing" | "stable"
    confidence: number
    unlimited: boolean
}

export interface SubPanelHourlyUsagePoint {
    hour: number
    count: number
}

export interface SubPanelServer {
    /** Client-facing remark — may carry emoji/usage decoration from the template. */
    name: string
    /** Plain node label. Absent on older backends. */
    node_name?: string
    country_code: string
    flag: string
    protocol: string
    network: string
    security: string
    address: string
    port: number
    link: string
    is_online: boolean
    last_activity_at: string | null
    account_email: string
    data_used: number
    data_used_display: string
}

// WireGuard device management (panel parity with the Telegram bot / mini-app).
export interface WgDevice {
    id: number
    label: string
    assigned_ip: string
    status: string
    created_at: string
    inbound_id: number
    /** Pinned presentation host; absent = the inbound's own address. */
    host_id?: number | null
    up_bytes: number
    down_bytes: number
    last_seen?: string | null
}

/** One pickable endpoint: an inbound, optionally seen through one of its hosts. */
export interface WgServerOption {
    inbound_id: number
    /** 0 = the inbound's own address:port. */
    host_id: number
    node_name: string
    country_code: string
    /** Host remark, template rendered; empty for a direct inbound endpoint. */
    label: string
    /** host:port the client will dial. */
    endpoint: string
}

export interface WgDevicesResponse {
    devices: WgDevice[]
    max_devices: number
    used: number
}
