// Access Log Types
export interface AccessLogEntry {
    timestamp: number
    source_ip: string
    status: string
    network: string
    domain: string
    port: number
    inbound_tag: string
    outbound_tag: string
    email: string
}

export interface AggregatedAccessLogEntry extends AccessLogEntry {
    node_id: number
    node_name: string
    node_country: string
}

export interface AccessLogSummary {
    id: number
    node_id: number
    email: string
    hour_time: string
    accepted_count: number
    rejected_count: number
    tcp_count: number
    udp_count: number
    top_domains: string
    rejected_domains: string
    source_ips: string
    created_at: string
    updated_at: string
}

export interface DomainCount {
    domain: string
    count: number
}
