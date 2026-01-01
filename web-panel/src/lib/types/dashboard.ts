// Dashboard Types
export interface DashboardStats {
    total_users: number
    active_users: number
    online_users: number
    banned_users: number
    admin_users: number
    total_subscriptions: number
    active_subscriptions: number
    expired_subscriptions: number
    // Optional forward-compatible fields read by sidebar panels
    new_users_today?: number
    total_certificates?: number
    certificates_expiring_30d?: number
    alerts_error?: number
    alerts_warn?: number
    alerts_info?: number
}

export interface XraySystemStats {
    num_goroutine: number
    alloc: number
    total_alloc: number
    sys: number
    uptime: number
    online_users: number
}

export interface OnlineUsersHistoryPoint {
    created_at: string
    count: number
}
