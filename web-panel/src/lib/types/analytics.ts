// ==================== Analytics Types ====================

export interface UserLTV {
    user_id: number
    username: string
    first_name: string
    total_spent: number
    payment_count: number
    first_payment: string
    last_payment: string
    active_months: number
    monthly_avg: number
    total_data_used: number
    is_active: boolean
}

export interface LTVSummary {
    users: UserLTV[]
    total_users: number
    avg_ltv: number
    median_ltv: number
    top_spenders: number
}

export interface HourlyUsagePoint {
    hour: number
    count: number
}

export interface PeakHourPoint {
    hour: number
    connections: number
    rejected: number
    unique_users: number
    tcp_count: number
    udp_count: number
}

export interface BlockedDomainStat {
    domain: string
    rejected_count: number
    node_count: number
    last_seen: string
}

export interface BlockedDomainSummary {
    domains: BlockedDomainStat[]
    total_rejected: number
    total_accepted: number
    rejection_rate: number
    period_from: string
    period_to: string
}

export interface ExhaustionPrediction {
    subscription_id: number
    label: string
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
