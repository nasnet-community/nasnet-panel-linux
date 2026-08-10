export type InterfaceRole = "unassigned" | "wan" | "lan" | "lan_member" | "mgmt"
export type UplinkSlot = "" | "domestic" | "secondary"
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
    force_state: "" | "up" | "down"
}

export interface NetworkState {
    router_mode: boolean
    takeover_done: boolean
    warnings: string[]
    uplinks: UplinkView[]
    pending_plan_id: number
    confirm_deadline_unix: number
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
