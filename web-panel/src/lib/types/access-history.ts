// Wire types for the per-subscription access-history endpoint
// (GET /api/v1/subscriptions/:id/access-history). Mirrors the Go
// internal/access_history/usecase.Response shape.

export type AccessHistoryGranularity = "hour" | "day"

export interface AccessHistoryParams {
    from: string
    to: string
    granularity?: AccessHistoryGranularity
    node_ids?: number[]
    top_n?: number
    include_ips?: boolean
}

export interface AccessHistoryTimeBucket {
    bucket: string
    accepted_count: number
    rejected_count: number
    tcp_count: number
    udp_count: number
}

export interface AccessHistoryDomainCount {
    domain: string
    count: number
}

export interface AccessHistoryIPCount {
    ip: string
    count: number
}

export interface AccessHistoryTotals {
    accepted_count: number
    rejected_count: number
    tcp_count: number
    udp_count: number
    hour_buckets: number
    active_hours?: number
    first_seen?: string
    last_seen?: string
}

// Hostnames and raw-IP destinations of one column, split apart.
export interface AccessHistoryDestinationSplit {
    domains: AccessHistoryDomainCount[] | null
    ips: AccessHistoryDomainCount[] | null
    domain_count: number
    ip_count: number
}

export interface AccessHistoryResponse {
    from: string
    to: string
    granularity: AccessHistoryGranularity
    series: AccessHistoryTimeBucket[] | null
    top_domains: AccessHistoryDomainCount[] | null
    top_rejected: AccessHistoryDomainCount[] | null
    top_source_ips?: AccessHistoryIPCount[] | null
    source_ip_count?: number
    destinations?: AccessHistoryDestinationSplit
    rejected_destinations?: AccessHistoryDestinationSplit
    totals: AccessHistoryTotals
    nodes_queried: number[] | null
    emails_resolved: number
    retention_days: number
    last_synced_at: Record<string, string>
}

// ─── Search ─────────────────────────────────────────────────────────

export type AccessHistorySearchKind = "domain" | "rejected_domain" | "source_ip"

export interface AccessHistorySearchParams {
    from: string
    to: string
    q: string
    kinds?: AccessHistorySearchKind[]
    node_ids?: number[]
    limit?: number
    include_ips?: boolean
}

export interface AccessHistorySearchHit {
    bucket: string
    node_id: number
    email: string
    kind: AccessHistorySearchKind
    value: string
    count: number
}

export interface AccessHistorySearchAggregate {
    kind: AccessHistorySearchKind
    value: string
    count: number
    hours: number
}

export interface AccessHistorySearchResponse {
    from: string
    to: string
    query: string
    kinds: AccessHistorySearchKind[] | null
    hits: AccessHistorySearchHit[] | null
    aggregates: AccessHistorySearchAggregate[] | null
    truncated: boolean
    nodes_queried: number[] | null
    emails_resolved: number
    retention_days: number
    last_synced_at: Record<string, string>
}

// ─── Global search (cross-subscription) ─────────────────────────────

export type AccessHistoryMatch = "contains" | "domain"

export interface AccessHistoryGlobalSearchParams {
    from: string
    to: string
    q: string
    match?: AccessHistoryMatch
    kinds?: AccessHistorySearchKind[]
    node_ids?: number[]
    subscription_ids?: number[]
    emails?: string[]
    limit?: number
    include_ips?: boolean
}

export interface AccessHistoryGlobalHit {
    bucket: string
    node_id: number
    email: string
    subscription_id: number
    user_id: number
    subscription_label?: string
    kind: AccessHistorySearchKind
    value: string
    count: number
}

export interface AccessHistoryGlobalSubValue {
    kind: AccessHistorySearchKind
    value: string
    count: number
    hours: number
}

export interface AccessHistoryGlobalSubscription {
    subscription_id: number
    user_id: number
    subscription_label?: string
    hits: number
    hours: number
    values: number
    emails: number
    node_ids: number[] | null
    first_seen: string
    last_seen: string
    top_values: AccessHistoryGlobalSubValue[] | null
}

export interface AccessHistoryGlobalValueAggregate {
    kind: AccessHistorySearchKind
    value: string
    count: number
    subscriptions: number
    hours: number
}

export interface AccessHistoryGlobalTotals {
    hits: number
    values: number
    subscriptions: number
    records: number
    hours: number
    first_seen?: string
    last_seen?: string
}

export interface AccessHistoryGlobalSeriesBucket {
    bucket: string
    accepted: number
    rejected: number
    source_ip: number
    nodes: Record<string, number>
}

export interface AccessHistoryGlobalSearchResponse {
    from: string
    to: string
    query: string
    match: AccessHistoryMatch
    kinds: AccessHistorySearchKind[] | null
    totals: AccessHistoryGlobalTotals
    subscriptions: AccessHistoryGlobalSubscription[] | null
    by_value: AccessHistoryGlobalValueAggregate[] | null
    series: AccessHistoryGlobalSeriesBucket[] | null
    hits: AccessHistoryGlobalHit[] | null
    // hits holds only the newest `limit` records
    truncated: boolean
    // the scan cap was reached; rollups cover the newest matches only
    approximate: boolean
    retention_days: number
    nodes_queried: number[] | null
    last_synced_at: Record<string, string>
}
