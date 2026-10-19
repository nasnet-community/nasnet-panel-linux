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
    interconnection_tag: string
    interconnection_tags: string[]
    outbound_tag: string
    inbound_tags: string[]
    rule2_id?: number
    // The one credential allowed to open a portal's tunnel.
    uuid?: string
    created_at: string
    updated_at: string
}

// Which ends of a reverse tunnel the panel runs: both nodes, only the portal
// (external bridge), or only the bridge (external portal).
export type ReverseTunnelKind = "nodes" | "external_bridge" | "external_portal"

export interface ReverseTunnel {
    id: number
    name: string
    kind: ReverseTunnelKind
    portal_node_id: number | null
    portal_node_name?: string
    portal_inbound_id: number | null
    portal_inbound_tag?: string
    portal_host_id: number | null
    portal_tag: string
    inbound_tags: string[] | null
    rule_id: number | null
    bridge_node_id: number | null
    bridge_node_name?: string
    bridge_tag: string
    exit_outbound_tag: string
    exit_balancer_tag: string
    uuid: string
    portal_link?: string
    state: "" | "connected" | "down"
    state_since?: string
    bridge_ip?: string
    last_seen_at?: string
    to_bridge: number
    from_bridge: number
    created_at: string
    updated_at: string
}

export interface ReverseTunnelInput {
    name: string
    portal_node_id: number | null
    portal_inbound_id: number | null
    portal_host_id: number | null
    inbound_tags: string[]
    bridge_node_id: number | null
    exit_outbound_tag: string
    exit_balancer_tag: string
    portal_link: string
}

// A reverse tunnel's tag on its portal, offered next to outbounds.
export interface ReverseTunnelTarget {
    id: number
    tag: string
    name: string
}

// What to set up on the end the panel doesn't run.
export interface ReverseTunnelPeerConfig {
    kind: ReverseTunnelKind
    link?: string
    config?: string
    client?: string
}
