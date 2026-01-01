import type { User } from "./users"

// Subscription Types
export type SubscriptionStatus = "active" | "expired" | "cancelled" | "paused" | "traffic_exhausted"

export interface Subscription {
    id: number
    user_id: number | null
    uuid: string
    label: string
    status: SubscriptionStatus
    is_manual?: boolean
    max_devices?: number
    data_limit: number
    data_used: number
    lifetime_data_used: number
    data_upload: number
    data_download: number
    lifetime_data_upload: number
    lifetime_data_download: number
    start_date: string
    end_date: string
    created_at: string
    updated_at: string
    user?: User
    custom_data_limit?: number
    is_data_limit_custom?: boolean
    custom_end_date?: string
    is_end_date_custom?: boolean
    custom_bandwidth_limit?: number
    is_bandwidth_custom?: boolean
    sub_link?: string
    subscription_url?: string
    config_id?: string
    link_key?: string
    config_email?: string
    panel_password_mode?: "default" | "custom" | "disabled"
    last_active_at?: string | null
    maintenance_mode?: boolean
    maintenance_message?: string
    maintenance_since?: string | null
}

export interface SubscriptionIP {
    id: number
    subscription_id: number
    ip: string
    node_id: number
    first_seen: string
    last_seen: string
    created_at: string
    updated_at: string
}

export const BANDWIDTH_OPTIONS = [
    { value: 0, label: "Unlimited" },
    { value: 10, label: "10 Mbps" },
    { value: 30, label: "30 Mbps" },
    { value: 50, label: "50 Mbps" },
    { value: 100, label: "100 Mbps" },
    { value: 200, label: "200 Mbps" },
    { value: 500, label: "500 Mbps" },
] as const
