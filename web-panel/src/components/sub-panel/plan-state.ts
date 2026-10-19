import { Minus, TrendingDown, TrendingUp } from "lucide-react"
import type {
    SubPanelData,
    SubPanelExhaustionPrediction,
    SubPanelHourlyUsagePoint,
    SubPanelServer,
} from "@/lib/types/sub-panel"

export type Tone = "emerald" | "amber" | "red" | "zinc"

export const DAY_MS = 86_400_000
const HOUR_MS = 3_600_000

export function hasEndDate(data: SubPanelData): boolean {
    return data.days_remaining >= 0 && !!data.end_date
}

/** Milliseconds until the end date; 0 once it has passed, Infinity without one. */
export function msLeft(data: SubPanelData): number {
    if (!hasEndDate(data) || !data.end_date) return Infinity
    return Math.max(0, new Date(data.end_date).getTime() - Date.now())
}

export function isPastEnd(data: SubPanelData): boolean {
    return hasEndDate(data) && msLeft(data) === 0
}

/** Cancelling only flips the status; the end date can still be ahead. */
export function isCancelled(data: SubPanelData): boolean {
    return data.status === "cancelled" && !isPastEnd(data)
}

/** The plan no longer grants access because of its time, not its data. */
export function hasEnded(data: SubPanelData): boolean {
    return data.status === "expired" || data.status === "cancelled" || isPastEnd(data)
}

export function wholeDaysLeft(data: SubPanelData): number {
    const ms = msLeft(data)
    return Number.isFinite(ms) ? Math.floor(ms / DAY_MS) : 0
}

/** "24d 3h"; under a day it falls to hours, then minutes. */
export function compactRemaining(data: SubPanelData): string {
    const ms = msLeft(data)
    if (!Number.isFinite(ms)) return "∞"
    const days = Math.floor(ms / DAY_MS)
    const hours = Math.floor((ms % DAY_MS) / HOUR_MS)
    if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`
    if (hours > 0) return `${hours}h`
    return `${Math.max(1, Math.floor(ms / 60_000))}m`
}

/**
 * Time left as a share of one plan period. A plan extended far past its length
 * reads full until its last period, instead of nearly empty for months.
 */
export function timeLeftPercent(data: SubPanelData): number {
    if (!hasEndDate(data) || !data.end_date) return 100
    const left = msLeft(data)
    if (left <= 0) return 0
    let period = data.plan_duration * DAY_MS
    if (period <= 0 && data.start_date) period = new Date(data.end_date).getTime() - new Date(data.start_date).getTime()
    if (period <= 0) return 100
    return Math.min(100, (left / period) * 100)
}

/** Colour from the time actually left, not from the share of a long span. */
export function timeTone(data: SubPanelData): Tone {
    if (!hasEndDate(data)) return "emerald"
    if (hasEnded(data)) return "red"
    const left = msLeft(data)
    if (left <= 3 * DAY_MS) return "red"
    if (left <= 7 * DAY_MS) return "amber"
    return "emerald"
}

/** Share of the data allowance still left, 0–100. */
export function dataLeftPercent(data: SubPanelData): number {
    return Math.max(0, Math.min(100, 100 - data.data_usage_percent))
}

export function dataTone(data: SubPanelData, prediction?: SubPanelExhaustionPrediction): Tone {
    if (data.is_unlimited) return "emerald"
    if (data.status === "traffic_exhausted" || dataLeftPercent(data) <= 0) return "red"
    // Paused or ended: what's left is on hold, not draining.
    if (data.status !== "active") return "zinc"
    if (dataLeftPercent(data) <= 10 || prediction?.will_exhaust_first) return "amber"
    return "emerald"
}

export const TREND_STYLE = {
    increasing: { icon: TrendingUp, label: "Going up", color: "text-amber-700 dark:text-amber-400" },
    decreasing: { icon: TrendingDown, label: "Going down", color: "text-emerald-700 dark:text-emerald-400" },
    stable: { icon: Minus, label: "Steady", color: "text-muted-foreground" },
}

/** "Nov 11", with the year only when it isn't this year. */
export function shortDate(d: Date): string {
    const sameYear = d.getFullYear() === new Date().getFullYear()
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric", ...(sameYear ? {} : { year: "numeric" }) })
}

export function hourLabel(h: number): string {
    if (h === 0) return "12 am"
    if (h < 12) return `${h} am`
    if (h === 12) return "12 pm"
    return `${h - 12} pm`
}

/**
 * The usage pattern comes bucketed by UTC hour. Re-bucket it into the
 * visitor's hours; on a half-hour offset each bucket splits across two.
 */
export function localHourBuckets(points?: SubPanelHourlyUsagePoint[]): number[] {
    const utc = new Array<number>(24).fill(0)
    for (const p of points ?? []) {
        if (p.hour >= 0 && p.hour < 24) utc[p.hour] = p.count
    }
    const offset = -new Date().getTimezoneOffset() / 60
    const whole = Math.floor(offset)
    const frac = offset - whole
    const local = new Array<number>(24).fill(0)
    for (let h = 0; h < 24; h++) {
        const first = (((h + whole) % 24) + 24) % 24
        local[first] += utc[h] * (1 - frac)
        if (frac > 0) local[(first + 1) % 24] += utc[h] * frac
    }
    return local
}

/**
 * Remark templates decorate the node label for VPN clients — "🇩🇪 Berlin | 📊 12 GB
 * left | ⏳ 9d". The panel already shows the flag, usage and expiry in their own
 * columns, so a row titled with the raw remark repeats them and doubles the flag.
 * Prefer the plain node name; fall back to stripping the decoration.
 */
export function cleanServerName(server: SubPanelServer): string {
    if (server.node_name) return server.node_name
    const head = (server.name || "").split("|")[0]
    return (
        head
            .replace(/^(?:[\s\u{1F1E6}-\u{1F1FF}\p{Extended_Pictographic}]|\u{200D}|\u{FE0F})+/u, "")
            .replace(/[\s|\-–—]+$/u, "")
            .trim() || server.address
    )
}
