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

export interface UserListItem extends User {
    active_subscriptions: number
    total_subscriptions: number
    last_active_at: string | null
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

export interface UserDailyUsagePoint {
    date: string
    data_used: number
    data_upload?: number
    data_download?: number
}

export interface UserActivityEvent {
    id: number
    action: string
    actor_name: string
    entity_type: string
    entity_id: number
    old_values?: string
    new_values?: string
    created_at: string
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
