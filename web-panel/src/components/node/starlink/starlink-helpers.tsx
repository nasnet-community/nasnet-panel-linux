import type { StarlinkStatus, StarlinkDataPoint } from "@/lib/types"

// ─── Types ──────────────────────────────────────────────────────────

export type DrawerType = "signal" | "obstruction" | "latency" | "dropRate" | "download" | "upload" | "alerts" | null
export type TimeRange = "1h" | "6h" | "24h" | "7d"

export interface AlertInfo {
    label: string
    severity: "critical" | "warning" | "info"
}

// ─── Time Range Helpers ─────────────────────────────────────────────

export const TIME_RANGE_CONFIG: Record<TimeRange, { label: string; limit: number; refetchInterval: number }> = {
    "1h": { label: "1h", limit: 120, refetchInterval: 30_000 },
    "6h": { label: "6h", limit: 360, refetchInterval: 30_000 },
    "24h": { label: "24h", limit: 500, refetchInterval: 60_000 },
    "7d": { label: "7d", limit: 500, refetchInterval: 60_000 },
}

// ─── Formatters ─────────────────────────────────────────────────────

export function formatMbps(bps: number): string {
    const mbps = bps / 1_000_000
    if (mbps >= 100) return mbps.toFixed(0)
    if (mbps >= 10) return mbps.toFixed(1)
    return mbps.toFixed(2)
}

export function formatUptime(seconds: number): string {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const mins = Math.floor((seconds % 3600) / 60)
    if (days > 0) return `${days}d ${hours}h ${mins}m`
    if (hours > 0) return `${hours}h ${mins}m`
    return `${mins}m`
}

// ─── Color Helpers ──────────────────────────────────────────────────

export function latencyColor(ms: number): string {
    if (ms < 40) return "text-emerald-400"
    if (ms < 80) return "text-amber-400"
    return "text-red-400"
}

export function dropRateColor(rate: number): string {
    if (rate < 0.01) return "text-emerald-400"
    if (rate < 0.05) return "text-amber-400"
    return "text-red-400"
}

export function clearanceGaugeColor(pct: number): "emerald" | "amber" | "red" {
    if (pct >= 98) return "emerald"
    if (pct >= 90) return "amber"
    return "red"
}

export function healthDotColor(status: StarlinkStatus): "emerald" | "amber" | "red" {
    if (!status.available) return "red"
    const hasCritical = status.alert_thermal_shutdown || status.alert_motors_stuck || status.alert_no_ethernet_link
    if (hasCritical) return "red"
    const hasWarning = status.alert_thermal_throttle || status.alert_is_heating ||
        status.alert_slow_ethernet || status.alert_mast_not_near_vertical || status.currently_obstructed
    if (hasWarning) return "amber"
    return "emerald"
}

// ─── Obstruction Map Cell Classification ────────────────────────────
//
// `dish_get_obstruction_map` reports one float per sky cell: 0.0-1.0 for
// measured directions and -1.0 for "never measured". Current firmware only
// ever emits the endpoints — 1.0 = clear line of sight, 0.0 = obstructed —
// so a continuous SNR gradient paints every clear cell mid-scale (the map
// came out uniformly orange) and bucketing by ">3 = clear" never matched a
// single cell. Treat it as the binary signal it is, while still tolerating
// an intermediate value should firmware ever emit one.

export type ObstructionCell = "clear" | "obstructed" | "nodata"

export const OBSTRUCTION_COLORS = {
    clear: "rgba(236,242,255,0.82)",
    obstructed: "#ef4444",
    nodata: "rgba(255,255,255,0.045)",
} as const

export function classifyObstructionCell(v: number | null | undefined): ObstructionCell {
    if (v === null || v === undefined || Number.isNaN(v) || v < 0) return "nodata"
    // Anything at/below the floor is blocked sky; the rest has a usable path.
    return v <= 0 ? "obstructed" : "clear"
}

export function obstructionCellColor(v: number | null | undefined): string {
    return OBSTRUCTION_COLORS[classifyObstructionCell(v)]
}

// ─── Alignment Helpers ──────────────────────────────────────────────

// The attitude filter must be converged before the reported boresight
// azimuth is trustworthy — that's what gates rotating a dish-relative
// (FRAME_UT) obstruction map into compass coordinates.
export function isAttitudeConverged(state: string | undefined): boolean {
    return state === "FILTER_CONVERGED"
}

// An agent older than the extended-alignment fields sends zeroes and an empty
// attitude state. A current agent always sends an enum name (the proto zero
// value stringifies to "FILTER_RESET"), so a non-empty state is the signal
// that uncertainty / desired-heading values are real rather than absent.
export function hasAlignmentTelemetry(status: { attitude_estimation_state?: string }): boolean {
    return !!status.attitude_estimation_state
}

export function attitudeStateLabel(state: string | undefined): string {
    switch (state) {
        case "FILTER_CONVERGED": return "Converged"
        case "FILTER_UNCONVERGED": return "Converging"
        case "FILTER_RESET": return "Reset"
        case "FILTER_FAULTED": return "Faulted"
        case "FILTER_INVALID": return "Invalid"
        default: return "Unknown"
    }
}

export function attitudeStateTone(state: string | undefined): "emerald" | "amber" | "red" {
    switch (state) {
        case "FILTER_CONVERGED": return "emerald"
        case "FILTER_UNCONVERGED":
        case "FILTER_RESET": return "amber"
        default: return "red"
    }
}

// ACTUATOR_STATE_IDLE → "Idle"
export function actuatorStateLabel(state: string | undefined): string {
    if (!state) return "Unknown"
    const bare = state.replace(/^ACTUATOR_STATE_/, "").replace(/_/g, " ").toLowerCase()
    return bare.charAt(0).toUpperCase() + bare.slice(1)
}

// Compass bearing → nearest 16-point cardinal name.
const COMPASS_POINTS = [
    "N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
    "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW",
]

export function bearingToCardinal(deg: number): string {
    const norm = ((deg % 360) + 360) % 360
    return COMPASS_POINTS[Math.round(norm / 22.5) % 16]
}

// ─── Alert Helpers ──────────────────────────────────────────────────

export function getActiveAlerts(status: StarlinkStatus): AlertInfo[] {
    const alerts: AlertInfo[] = []
    if (status.alert_thermal_shutdown) alerts.push({ label: "Thermal Shutdown", severity: "critical" })
    if (status.alert_motors_stuck) alerts.push({ label: "Motors Stuck", severity: "critical" })
    if (status.alert_no_ethernet_link) alerts.push({ label: "No Ethernet Link", severity: "critical" })
    if (status.alert_thermal_throttle) alerts.push({ label: "Thermal Throttle", severity: "warning" })
    if (status.alert_is_heating) alerts.push({ label: "Heating", severity: "warning" })
    if (status.alert_slow_ethernet) alerts.push({ label: "Slow Ethernet", severity: "warning" })
    if (status.alert_power_save_idle) alerts.push({ label: "Power Save", severity: "warning" })
    if (status.alert_mast_not_near_vertical) alerts.push({ label: "Mast Misaligned", severity: "warning" })
    if (status.alert_roaming) alerts.push({ label: "Roaming", severity: "info" })
    if (status.alert_unexpected_location) alerts.push({ label: "Unexpected Location", severity: "info" })
    if (status.alert_install_pending) alerts.push({ label: "Install Pending", severity: "info" })
    return alerts
}

export function alertBadgeVariant(severity: "critical" | "warning" | "info") {
    switch (severity) {
        case "critical": return "danger" as const
        case "warning": return "outline" as const
        case "info": return "secondary" as const
    }
}

// Alert bitmask names in order (bit 0 = index 0)
const ALERT_NAMES = [
    "Thermal Shutdown", "Thermal Throttle", "Motors Stuck", "No Ethernet Link",
    "Heating", "Slow Ethernet", "Power Save", "Mast Misaligned",
    "Roaming", "Unexpected Location", "Install Pending",
]

export interface AlertTimelineEntry {
    name: string
    startTime: string
    endTime: string | null // null = still active
}

export function buildAlertTimeline(history: StarlinkDataPoint[]): AlertTimelineEntry[] {
    if (history.length < 2) return []
    const sorted = [...history].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
    const entries: AlertTimelineEntry[] = []
    const active = new Map<number, string>() // bit index -> start time

    for (const point of sorted) {
        for (let bit = 0; bit < ALERT_NAMES.length; bit++) {
            const isSet = (point.alert_flags & (1 << bit)) !== 0
            if (isSet && !active.has(bit)) {
                active.set(bit, point.created_at)
            } else if (!isSet && active.has(bit)) {
                entries.push({ name: ALERT_NAMES[bit], startTime: active.get(bit)!, endTime: point.created_at })
                active.delete(bit)
            }
        }
    }
    // Still-active alerts
    for (const [bit, startTime] of active) {
        entries.push({ name: ALERT_NAMES[bit], startTime, endTime: null })
    }
    return entries.sort((a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime())
}

