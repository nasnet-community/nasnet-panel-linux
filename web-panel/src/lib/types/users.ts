// User Types
export interface User {
    id: number
    telegram_id: number
    username: string
    first_name: string
    last_name: string
    is_admin: boolean
    is_banned: boolean
    created_at: string
    updated_at: string
}

export type UserGroup = "all" | "active" | "ending" | "lapsed" | "never" | "banned"
export type UserGroupCounts = Record<UserGroup, number>

/** One subscription as the users list shows it. Bytes are raw; dates are ISO. */
export interface UserListSubscription {
    id: number
    status: string
    pause_reason?: string
    label: string
    end_date: string | null
    data_used: number
    /** 0 means unlimited. */
    data_limit: number
    subscription_url?: string
}

export interface UserListMatch {
    field: "label" | "email" | "link"
    value: string
    subscription_id: number
}

export interface UserListItem extends User {
    active_subscriptions: number
    total_subscriptions: number
    last_active_at: string | null
    data_used: number
    online: boolean
    /** The user's most specific group; "" for stand-ins with no subscription. */
    group: Exclude<UserGroup, "all"> | ""
    subscription: UserListSubscription | null
    active_links: UserListSubscription[] | null
    match: UserListMatch | null
}

export interface UserDetails extends User {
    language: string
    admin_notes: string
    total_subscriptions: number
    active_subscriptions: number
    total_data_used: number
    total_data_upload: number
    total_data_download: number
    last_active_at: string | null
}

/** Who did something and when (latest pause or ban audit entry). */
export interface ActorAt {
    at: string
    actor?: string
}

/** Mirrors pkg/analytics.ExhaustionPrediction. Dates are YYYY-MM-DD (UTC). */
export interface ExhaustionForecast {
    data_limit: number
    data_used: number
    data_remaining: number
    daily_avg_bytes: number
    days_remaining: number
    end_date: string | null
    days_until_expiry: number
    exhaustion_date: string | null
    will_exhaust_first: boolean
    usage_trend: string
    confidence: number
    unlimited: boolean
}

export interface NodeAccount {
    account_id: number
    status: string
    protocol?: string
    inbound_tag?: string
    node_id?: number
    node_name?: string
    node_country?: string
    last_activity_at?: string
}

export interface SubscriptionOverview {
    subscription_id: number
    online_ips: string[]
    ips_30d: number
    traffic_30d: number
    forecast?: ExhaustionForecast
    exhausted_at?: string
    paused?: ActorAt
    accounts: NodeAccount[]
}

/** GET /admin/users/:id/overview */
export interface UserOverview {
    traffic_30d: number
    online_ips: number
    ips_30d: number
    last_seen_at?: string
    last_seen_node?: string
    ban?: ActorAt
    subscriptions: SubscriptionOverview[]
}

export type TimelineKind = "service" | "notice" | "admin"

export interface TimelineEvent {
    id: string
    kind: TimelineKind
    /** "subscription.created", "notice.expiry_7d", an audit action, ... */
    type: string
    at: string
    subscription_id?: number
    status?: string
    description?: string
    actor?: string
    old_values?: string
    new_values?: string
}

/** GET /admin/users/:id/timeline */
export interface UserTimeline {
    events: TimelineEvent[]
    counts: Record<"all" | "views" | TimelineKind, number>
    total: number
    /** Pass as `before` to get the next, older page. */
    next_cursor?: string
}

/** Subscriptions a ban paused or an unban resumed. */
export interface SubscriptionChanges {
    changed: number[]
    failed?: number[]
}

export interface UserAccountInfo {
    account_id: number
    email: string
    status: string
    inbound_tag: string
    protocol: string
    node_id: number
    node_name: string
    node_country: string
    subscription_id?: number
    data_used: number
    last_activity_at?: string
}

export interface UsersResponse {
    users: UserListItem[]
    total: number
}
