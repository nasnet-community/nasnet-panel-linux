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

// ─── SNR Gradient Color ─────────────────────────────────────────────

function lerpColor(a: [number, number, number], b: [number, number, number], t: number): string {
    const r = Math.round(a[0] + (b[0] - a[0]) * t)
    const g = Math.round(a[1] + (b[1] - a[1]) * t)
    const bl = Math.round(a[2] + (b[2] - a[2]) * t)
    return `rgb(${r},${g},${bl})`
}

export function snrToColor(snr: number | null): string {
    if (snr === null || snr === undefined || isNaN(snr) || snr < 0) return "#1a1a2e"
    if (snr === 0) return "#ef4444"
    if (snr <= 3) return lerpColor([239, 68, 68], [245, 158, 11], snr / 3)
    if (snr <= 6) return lerpColor([245, 158, 11], [20, 184, 166], (snr - 3) / 3)
    return "#14b8a6"
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

