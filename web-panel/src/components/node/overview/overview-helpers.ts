import type { NodeStats, NodeStatsSeries, StatsRange } from "@/lib/types"
import { chart } from "@/lib/design/palette"

/** CPU, memory and disk keep one hue each across cards, gauges and charts. */
export const HOST_COLORS = { cpu: chart.blue, memory: chart.violet, disk: chart.teal } as const

/** A reading older than this is shown as "last reading", not live. */
export const LIVE_FOR_MS = 20_000

/** The node answered recently: the values on screen are current. */
export function isLiveReading(stats: NodeStats | undefined, now: number): boolean {
    if (!stats || stats.stats_unavailable || stats.received_at === undefined) return false
    return now - stats.received_at < LIVE_FOR_MS
}

/** 24-hour clock time, e.g. "14:29". */
export function clockTime(value: string | number): string {
    return new Date(value).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" })
}

/** "Last reading 14:29", or "yesterday 14:29" / "Sep 23, 14:29" when older. */
export function lastReadingLabel(stats: NodeStats | undefined): string {
    const when = stats?.received_at ?? (stats?.sampled_at ? Date.parse(stats.sampled_at) : undefined)
    if (when === undefined) return "No reading yet"
    const at = new Date(when)
    const today = new Date()
    if (at.toDateString() === today.toDateString()) return `Last reading ${clockTime(at.getTime())}`
    return `Last reading ${at.toLocaleDateString("en-US", { month: "short", day: "numeric" })}, ${clockTime(at.getTime())}`
}

export type Tone = "ok" | "warn" | "crit"

export function usageTone(pct: number | undefined, warn: number, crit: number): Tone {
    if (pct === undefined) return "ok"
    if (pct >= crit) return "crit"
    if (pct >= warn) return "warn"
    return "ok"
}

export const toneText: Record<Tone, string> = { ok: "", warn: "text-amber-500", crit: "text-red-500" }

// ─── Charts ─────────────────────────────────────────────────────────

export type ChartRange = "live" | StatsRange

export const CHART_RANGES: readonly { value: ChartRange; label: string }[] = [
    { value: "live", label: "Live" },
    { value: "1h", label: "1h" },
    { value: "6h", label: "6h" },
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
]

/** One chart point. Null values are gaps: nothing was recorded then. */
export interface ChartRow {
    t: number
    down: number | null
    up: number | null
    tcp: number | null
    udp: number | null
    fd: number | null
    users: number | null
}

const GAP: Omit<ChartRow, "t"> = { down: null, up: null, tcp: null, udp: null, fd: null, users: null }

/** Lays the series on its full bucket grid so empty buckets break the lines. */
export function rowsFromSeries(series: NodeStatsSeries): ChartRow[] {
    const byT = new Map(series.points.map(p => [p.t, p]))
    const rows: ChartRow[] = []
    const first = Math.floor(series.from / series.step_sec) * series.step_sec
    for (let t = first; t <= series.to; t += series.step_sec) {
        const p = byT.get(t)
        rows.push(p
            ? { t: t * 1000, down: p.down_avg, up: p.up_avg, tcp: p.tcp_avg, udp: p.udp_avg, fd: p.fd_avg, users: p.users_avg }
            : { t: t * 1000, ...GAP })
    }
    return rows
}

/** A live sample, 5s apart while the node answers. */
export interface LivePoint {
    t: number
    up: number
    down: number
    tcp: number
    udp: number
    fd: number
    users: number
}

/** Live samples, with a gap wherever the node missed a few sweeps. */
export function rowsFromLive(points: LivePoint[]): ChartRow[] {
    const rows: ChartRow[] = []
    let prev: number | null = null
    for (const p of points) {
        if (prev !== null && p.t - prev > 20_000) rows.push({ t: prev + 5_000, ...GAP })
        rows.push({ t: p.t, down: p.down, up: p.up, tcp: p.tcp, udp: p.udp, fd: p.fd, users: p.users })
        prev = p.t
    }
    return rows
}

export function formatTick(t: number, range: ChartRange): string {
    if (range === "7d" || range === "30d") {
        return new Date(t).toLocaleDateString("en-US", { month: "short", day: "numeric" })
    }
    return clockTime(t)
}

export function formatTooltipTime(t: number, range: ChartRange): string {
    const d = new Date(t)
    if (range === "live" || range === "1h") return d.toLocaleTimeString("en-GB")
    return `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })}, ${clockTime(t)}`
}

/** Compact count: 4215 → "4.2K". */
export function formatCount(v: number): string {
    if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`
    if (v >= 1_000) return `${(v / 1_000).toFixed(1)}K`
    return `${Math.round(v)}`
}
