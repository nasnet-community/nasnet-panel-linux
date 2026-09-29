import type { StarlinkDataPoint, StarlinkEvent, StarlinkLive } from "@/lib/types"
import { isOutageReason } from "./starlink-helpers"

/** One chart sample. t is epoch ms; null values are gaps. */
export interface ChartPoint {
    t: number
    dl_mbps: number | null
    ul_mbps: number | null
    latency: number | null
    drop_pct: number | null
    obstruction_pct: number | null
    power_w: number | null
}

export interface OutageBand {
    start: number
    end: number
    label: string
}

type SeriesKey = keyof Omit<ChartPoint, "t">

const mbps = (v: number | null) => (v == null ? null : v / 1_000_000)
const pct = (v: number | null) => (v == null ? null : v * 100)

export function pointsFromLive(live: StarlinkLive): ChartPoint[] {
    const n = live.pop_ping_latency_ms.length
    const end = new Date(live.end_at).getTime()
    const stepMs = live.step_s * 1000
    return live.pop_ping_latency_ms.map((lat, i) => ({
        t: end - (n - 1 - i) * stepMs,
        dl_mbps: mbps(live.downlink_throughput_bps[i] ?? null),
        ul_mbps: mbps(live.uplink_throughput_bps[i] ?? null),
        latency: lat,
        drop_pct: pct(live.pop_ping_drop_rate[i] ?? null),
        obstruction_pct: null,
        power_w: live.power_w[i] ?? null,
    }))
}

export function pointsFromHistory(data: StarlinkDataPoint[]): ChartPoint[] {
    return data.map((d) => ({
        t: new Date(d.created_at).getTime(),
        dl_mbps: d.downlink_throughput_bps / 1_000_000,
        ul_mbps: d.uplink_throughput_bps / 1_000_000,
        latency: d.pop_ping_latency_ms,
        drop_pct: d.pop_ping_drop_rate * 100,
        obstruction_pct: d.obstruction_fraction * 100,
        power_w: null,
    }))
}

/** Outage spans from dish events (the live log or the DB). */
export function outageBands(events: StarlinkEvent[]): OutageBand[] {
    return events
        .filter((e) => isOutageReason(e.reason))
        .map((e) => {
            const start = new Date(e.start_at).getTime()
            return { start, end: start + Math.max(e.duration_ms, 0), label: e.reason }
        })
}

/** Sparkline input for one series, gaps dropped. */
export function sparkSeries(points: ChartPoint[], key: SeriesKey): { value: number }[] {
    const out: { value: number }[] = []
    for (const p of points) {
        const v = p[key]
        if (v != null) out.push({ value: v })
    }
    return out
}

export function lastValue(points: ChartPoint[], key: SeriesKey): number | null {
    for (let i = points.length - 1; i >= 0; i--) {
        const v = points[i][key]
        if (v != null) return v
    }
    return null
}
