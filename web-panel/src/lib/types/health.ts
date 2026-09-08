// Mirrors HealthView and friends in internal/network/usecase/health_view.go.

import type { PoolStrategy, UplinkSlot } from "@/lib/types/network"

export interface TargetStatus {
    address: string
    /** The operator's name for the server, when they gave it one. */
    label?: string
    proto: "tcp" | "dns"
    ok: boolean
    rtt_ms: number
    error?: string
}

export interface HealthSample {
    unix: number
    ok_ratio: number
    rtt_ms: number
}

export type UplinkVerdict =
    | "up"
    | "degraded"
    | "no-internet"
    | "no-gateway"
    | "no-carrier"
    | "forced-up"
    | "forced-down"
    | ""

/** What the internet damper still wants before the line carries its own
 *  traffic again. */
export interface Recovery {
    passes: number
    needed: number
    dwell_seconds_left: number
}

export interface UplinkHealth {
    slot: UplinkSlot
    if_name: string
    carrier: string
    gateway: string
    internet: string
    verdict: UplinkVerdict
    /** Who carries this line's traffic: "" itself, a sibling's if_name, or "pool". */
    via: string
    force_state: "" | "up" | "down"
    degraded: boolean
    loss_pct: number
    median_rtt_ms: number
    targets: TargetStatus[]
    history: HealthSample[]
    /** A state the ladder can see but not explain, e.g. a captive portal. */
    note?: string
    /** The address the gateway rung dials, static or learned from DHCP. */
    gateway_ip?: string
    /** When this line last crossed between working and not. */
    since_unix?: number
    /** Present only while the damper is holding the line down. */
    recovery?: Recovery | null
    rx_bytes: number
    tx_bytes: number
    /** This line's own routing table, one entry per route. */
    routes?: string[]
}

export type TunnelVerdict = "" | "up" | "no-internet" | "degraded"

export interface TunnelHealth {
    profile_id: number
    name: string
    if_name: string
    /** The operator's order, first is 0. */
    position: number
    /** In the nexthop set right now, i.e. actually carrying traffic. */
    in_pool: boolean
    verdict: TunnelVerdict
    degraded: boolean
    loss_pct: number
    median_rtt_ms: number
    targets: TargetStatus[]
    history: HealthSample[]
}

export interface VPNPoolHealth {
    present: boolean
    strategy: PoolStrategy
    /** The tunnel carrying alone, empty when they all carry. */
    carrier?: string
    loss_pct: number
    median_rtt_ms: number
    /** Members' samples averaged, for the pool sparkline. */
    pool_history: HealthSample[]
    tunnels: TunnelHealth[]
}

export interface RouterHealth {
    generated_unix: number
    failover_active: boolean
    uplinks: UplinkHealth[]
    vpn: VPNPoolHealth | null
}
