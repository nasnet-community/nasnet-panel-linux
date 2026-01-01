export interface DNSServer {
    address: string
    port?: number
    domains?: string[]
    expected_ips?: string[]
    unexpected_ips?: string[]
    skip_fallback?: boolean
    query_strategy?: string
    tag?: string
    client_ip?: string
    timeout_ms?: number
    disable_cache?: boolean | null
    serve_stale?: boolean | null
    serve_expired_ttl?: number | null
    final_query?: boolean
}

export interface DNSSettings {
    servers?: DNSServer[]
    hosts?: Record<string, string | string[]>
    client_ip?: string
    query_strategy?: string
    disable_cache?: boolean
    disable_fallback?: boolean
    disable_fallback_if_match?: boolean
    tag?: string
    serve_stale?: boolean
    serve_expired_ttl?: number | null
    enable_parallel_query?: boolean
    use_system_hosts?: boolean
}

export interface FakeDNSPool {
    ip_pool?: string
    lru_size?: number
}
