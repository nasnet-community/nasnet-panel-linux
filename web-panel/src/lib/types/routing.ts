export interface DomainMatcher {
    type: 'plain' | 'regex' | 'domain' | 'full'
    value: string
}

export interface RoutingSettings {
    domain_strategy: string
    block_bittorrent: boolean
    block_ips: string[]
    block_domains: string[]
    direct_ips: string[]
    direct_domains: string[]
    ipv4_routing: string[]
    warp_enabled: boolean
    warp_domains: string[]
    warp_ips: string[]
    outbound_test_url: string
}

export interface RoutingRule {
    id: number
    node_id: number
    rule_tag: string
    remark: string
    priority: number
    enabled: boolean
    outbound_tag: string
    balancing_tag: string
    domain_rules: DomainMatcher[]
    geoip_rules: string[]
    ipcidr_rules: string[]
    port_rules: string[]
    network_rules: string[]
    protocol_rules: string[]
    inbound_tags: string[]
    user_emails: string[]
    source_ips: string[]
    source_ports: string[]
    attributes: Record<string, string>
    process_names: string[]
    local_ips: string[]
    local_ports: string[]
    vless_routes: string[]
    webhook_url: string
    webhook_deduplication: number
    webhook_headers: Record<string, string>
    created_at: string
    updated_at: string
}

export interface BalancingRule {
    id: number
    node_id: number
    tag: string
    outbound_selectors: string[]
    strategy: "random" | "leastping" | "roundrobin" | "leastload"
    fallback_tag: string
    enabled: boolean
    created_at: string
    updated_at: string
}

export interface ReverseProxy {
    id: number
    node_id: number
    type: "bridge" | "portal"
    tag: string
    domain: string
    interconnection_tag: string
    interconnection_tags: string[]
    outbound_tag: string
    inbound_tags: string[]
    rule1_id?: number
    rule2_id?: number
    created_at: string
    updated_at: string
}
