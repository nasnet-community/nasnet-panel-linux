export type InterfaceRole = "unassigned" | "wan" | "lan" | "lan_member" | "mgmt"
export type UplinkSlot =
    | ""
    | "domestic"
    | "secondary"
    | "secondary2"
    | "secondary3"
    | "secondary4"
export type VerdictLevel = "reject" | "confirm" | "warn"

export interface Verdict {
    rule: string
    level: VerdictLevel
    message: string
}

export interface NetworkInterfaceView {
    id: number
    if_name: string
    perm_mac: string
    id_path: string
    key: string
    key_kind: "permaddr" | "idpath" | "ifname"
    source: string
    confidence: number
    driver: string
    carrier: boolean
    oper_state: string
    speed_mbit: number
    mtu: number
    usb_speed_mbit: number
    assignable: boolean
    addrs: string[]
    role: InterfaceRole
    slot: UplinkSlot
    label: string
    present: boolean
    healthy: boolean
}

export interface UplinkView {
    if_name: string
    slot: UplinkSlot
    label: string
    table: number
    addrs: string[]
    gateway: string
    healthy: boolean
    verdict: string
    force_state: "" | "up" | "down"
}

export interface NetworkState {
    router_mode: boolean
    takeover_done: boolean
    warnings: string[]
    uplinks: UplinkView[]
    pending_plan_id: number
    confirm_deadline_unix: number
    /** Never folded into uplink health — that's the link, this rides over it. */
    vpn: { active: boolean; connected: boolean }
}

export interface NetworkPlan {
    ops: string[]
    verdicts: Verdict[]
}

export interface NetworkApply {
    plan_id: number
    confirm_deadline_unix: number
    ops: string[]
}

export interface PortForward {
    id: number
    uplink_key: string
    proto: "tcp" | "udp"
    dport: number
    to_addr: string
    to_port: number
    comment: string
    enabled: boolean
}

export interface LANConfig {
    bridge_name: string
    cidr: string
    dhcp_range_low: string
    dhcp_range_high: string
    lease_hours: number
    enabled: boolean
    input_firewall: boolean
}

/** The stored LAN plus which classification layers this build can actually run. */
export interface LANView extends LANConfig {
    geoip_prefixes: number
    domain_layer: boolean
    resolver_ready: boolean
    resolver_running: boolean
    ranges_fetched_at?: string
}

export interface AssignRoleRequest {
    interface_id: number
    role: InterfaceRole
    slot: UplinkSlot
    evict_id?: number
    confirmed?: boolean
    master_id?: number
    method?: "dhcp4" | "static" | "rawip"
    static_address?: string
    static_gateway?: string
}

/** One client on the LAN bridge. Derived per request; only `label` is stored. */
export interface LANDevice {
    mac: string
    /** Every address seen for the MAC, the leased one first. */
    ips: string[]
    /** What the client asked to be called, after sanitizing. */
    hostname: string
    /** From the MAC's registered prefix. Empty for a randomized MAC. */
    vendor: string
    /** The operator's name for it. */
    label: string
    /** Locally-administered MAC: names a session, not a device. */
    randomized: boolean
    /** The bridge member it was learned on. */
    port?: string
    online: boolean
    last_seen_seconds?: number
    lease_expiry?: string
}

/** Which sources answered, so an empty list is never unexplained. */
export interface LANDeviceList {
    devices: LANDevice[]
    enabled: boolean
    leases_ok: boolean
    neighbours_ok: boolean
    /** The bridge ageing time: how long a departed device keeps reading online. */
    offline_after_seconds: number
}

/** One stored WireGuard config. The private key is served as-is; whoever can
 *  read it already holds an admin session. */
export interface WireGuardConfig {
    private_key: string
    /** This end's address inside the tunnel, as a CIDR. */
    address: string
    dns?: string
    /** 0 means the default is applied at connect time. */
    mtu?: number
    listen_port?: number
    peer: WGPeerConfig
    /** So the tunnel can come up before any resolver does. */
    pinned_endpoint_ip?: string
    /** What the parser dropped or filled in. */
    notices?: string[]
    /** From a URI fragment, offered as the profile name. */
    suggested_name?: string
}

export interface WGPeerConfig {
    public_key: string
    preshared_key?: string
    allowed_ips: string[]
    endpoint: string
    persistent_keepalive?: number
}

export interface VPNProfile {
    id: number
    name: string
    type: string
    /** In the pool. Priority 0 is the best tier; weight splits a tier's flows. */
    enabled: boolean
    priority: number
    weight: number
    /** Names the interface (nasnet-wg{slot}). Null while disabled. */
    wg_slot: number | null
    /** Interface key of the pinned WAN. Empty rides the pool's deal. */
    transport_uplink?: string
    config: WireGuardConfig
    /** Derived from the private key, for pasting into your own server. */
    public_key: string
    created_at: string
    updated_at: string
    /** Why the stored config would not decode. Such a row can only be deleted. */
    unreadable?: string
}

export interface TunnelStatus {
    profile_id: number
    name: string
    if_name: string
    /** Where this tunnel sits in the operator's order, first is 0. */
    position: number
    /** A handshake in the last few minutes. There is no link state to read. */
    connected: boolean
    handshake_age_seconds: number | null
    rx_bytes: number
    tx_bytes: number
    endpoint?: string
    /** Applied values, defaults included. */
    mtu: number
    keepalive_seconds: number
    last_error?: string
    /** In the nexthop set right now, i.e. actually carrying traffic. */
    in_pool: boolean
    /** The WAN this tunnel's transport rides. */
    via?: TunnelVia
}

export interface TunnelVia {
    if_name: string
    label: string
    key: string
    /** An operator chose this WAN; otherwise the pool dealt it. */
    pinned: boolean
}

/** One secondary uplink the pool can ride. */
export interface VPNUplink {
    slot: UplinkSlot
    if_name: string
    label: string
    key: string
    up: boolean
}

/** How traffic uses the pool. One choice for the whole pool. */
export type PoolStrategy = "spread" | "order" | "fastest"

export interface VPNPoolStatus {
    tunnels: TunnelStatus[]
    uplinks: VPNUplink[]
    /** Always true. Stated, never offered. */
    kill_switch: boolean
    strategy: PoolStrategy
    /** The tunnel carrying alone, when the strategy runs one at a time. */
    carrier?: string
}

export interface VPNProfileInput {
    name: string
    /** A wireguard:// URI or the contents of a .conf file. */
    raw?: string
    /** The manual-entry path, used when raw is empty. */
    config?: WireGuardConfig
}
