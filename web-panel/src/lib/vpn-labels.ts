import type { PoolStrategy, TunnelStatus, VPNProfile } from "@/lib/types/network"
import type { TunnelHealth } from "@/lib/types/health"

/** handshakeLabel turns the only liveness signal WireGuard offers into words.
 *  There is no link state to read: the interface is up whether or not anyone
 *  is on the other end. */
export function handshakeLabel(status: { handshake_age_seconds: number | null }): string {
    if (status.handshake_age_seconds === null) return "No handshake yet"
    const s = status.handshake_age_seconds
    if (s < 60) return "Last handshake just now"
    if (s < 3600) return `Last handshake ${Math.floor(s / 60)} min ago`
    return `Last handshake ${Math.floor(s / 3600)} h ago`
}

/** The table's compact form of the same signal. */
export function handshakeShort(age: number | null): string {
    if (age === null) return "never"
    if (age < 60) return "just now"
    if (age < 3600) return `${Math.floor(age / 60)}m ago`
    return `${Math.floor(age / 3600)}h ago`
}

export function formatBytes(n: number): string {
    if (n < 1024) return `${n} B`
    const units = ["KB", "MB", "GB", "TB"]
    let v = n / 1024
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

/** detectFormat tells the two accepted forms apart. They cannot be confused, so
 *  the operator never has to say which one they pasted. */
export function detectFormat(raw: string): "uri" | "conf" | "" {
    const t = raw.trim().toLowerCase()
    if (t.startsWith("wireguard://")) return "uri"
    if (t.includes("[interface]")) return "conf"
    return ""
}

/** The three ways to use a pool. Only the selected blurb is shown. */
export const POOL_STRATEGIES: { value: PoolStrategy; label: string; blurb: string }[] = [
    {
        value: "spread",
        label: "Share evenly",
        blurb: "Every VPN carries a share of the traffic. One connection stays on the VPN it started on.",
    },
    {
        value: "order",
        label: "In order",
        blurb: "The first VPN carries everything. If it stops answering the next one takes over, and hands it back when it returns.",
    },
    {
        value: "fastest",
        label: "Fastest first",
        blurb: "The quickest VPN carries everything. The box keeps measuring and moves traffic when another is clearly faster.",
    },
]

export function strategyLabel(s: PoolStrategy): string {
    return POOL_STRATEGIES.find((x) => x.value === s)?.label ?? s
}

/** What one VPN is doing, in one word. The old table said this twice. */
export type PoolRowState = "carrying" | "next-up" | "standby" | "not-answering" | "checking" | "off"

export interface PoolRowCondition {
    state: PoolRowState
    /** Slow or lossy but still carrying. A tone, not a state. */
    degraded: boolean
}

export function poolRowCondition(
    profile: VPNProfile,
    status: TunnelStatus | undefined,
    health: TunnelHealth | undefined,
    nextUp: boolean,
): PoolRowCondition {
    if (!profile.enabled) return { state: "off", degraded: false }
    const degraded = health?.degraded ?? false
    if (health?.verdict === "no-internet") return { state: "not-answering", degraded }
    // No verdict yet: the damper is warming up.
    if (!health || health.verdict === "") return { state: "checking", degraded }
    if (status?.in_pool) return { state: "carrying", degraded }
    if (nextUp) return { state: "next-up", degraded }
    return { state: "standby", degraded }
}

export const POOL_STATE_LABEL: Record<PoolRowState, string> = {
    "carrying": "carrying",
    "next-up": "next up",
    "standby": "standby",
    "not-answering": "not answering",
    "checking": "checking",
    "off": "off",
}
