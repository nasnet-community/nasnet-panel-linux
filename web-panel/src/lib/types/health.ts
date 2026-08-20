// Mirrors HealthView and friends in internal/network/usecase/health_view.go.

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
    slot: "domestic" | "secondary"
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

export interface VPNHealth {
    present: boolean
    loss_pct: number
    median_rtt_ms: number
    targets: TargetStatus[]
    history: HealthSample[]
}

export interface RouterHealth {
    generated_unix: number
    failover_active: boolean
    uplinks: UplinkHealth[]
    vpn: VPNHealth | null
}
