// Mirrors HealthView and friends in internal/network/usecase/health_view.go.

import type { UplinkSlot } from "@/lib/types/network"

export interface TargetStatus {
    address: string
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

export interface UplinkHealth {
    slot: UplinkSlot
    if_name: string
    carrier: string
    gateway: string
    internet: string
    verdict: UplinkVerdict
    force_state: "" | "up" | "down"
    degraded: boolean
    loss_pct: number
    median_rtt_ms: number
    targets: TargetStatus[]
    history: HealthSample[]
}

export type TunnelVerdict = "" | "up" | "no-internet" | "degraded"

export interface TunnelHealth {
    profile_id: number
    name: string
    if_name: string
    priority: number
    weight: number
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
    active_tier: number
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
