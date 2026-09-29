import { useMemo } from "react"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Area, AreaChart, CartesianGrid, ReferenceArea, ReferenceLine,
    ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts"
import { axis as axisPalette, starlink as slPalette, status as statusPalette } from "@/lib/design/palette"
import { type TimeRange, TIME_RANGE_CONFIG, formatMbps } from "./starlink-helpers"
import type { ChartPoint, OutageBand } from "./starlink-series"
import { SegmentedControl, T, cardShell } from "./starlink-ui"

export type ChartTab = "throughput" | "latency" | "loss" | "obstruction" | "power"

interface StarlinkChartsProps {
    points: ChartPoint[]
    bands: OutageBand[]
    isLoading: boolean
    compact?: boolean
    timeRange: TimeRange
    onTimeRangeChange: (range: TimeRange) => void
    tab: ChartTab
    onTabChange: (tab: ChartTab) => void
    /** False for agents without passthrough: no 15m range, no power. */
    liveAvailable: boolean
}

interface SeriesSpec {
    key: keyof Omit<ChartPoint, "t">
    color: string
    label: string
}

const TABS: Record<ChartTab, {
    label: string
    unit: string
    series: SeriesSpec[]
    fmt: (v: number) => string
    ref?: { y: number; label: string; color: string }
}> = {
    throughput: {
        label: "Throughput", unit: "Mbps",
        series: [
            { key: "dl_mbps", color: slPalette.download, label: "Download" },
            { key: "ul_mbps", color: slPalette.upload, label: "Upload" },
        ],
        fmt: (v) => `${formatMbps(v * 1_000_000)} Mbps`,
    },
    latency: {
        label: "Latency", unit: "ms",
        series: [{ key: "latency", color: slPalette.latency, label: "Latency" }],
        fmt: (v) => `${v.toFixed(1)} ms`,
        ref: { y: 50, label: "50ms", color: statusPalette.danger },
    },
    loss: {
        label: "Packet loss", unit: "%",
        series: [{ key: "drop_pct", color: slPalette.dropRate, label: "Packet loss" }],
        fmt: (v) => `${v.toFixed(2)}%`,
    },
    obstruction: {
        label: "Obstruction", unit: "%",
        series: [{ key: "obstruction_pct", color: slPalette.good, label: "Obstruction" }],
        fmt: (v) => `${v.toFixed(2)}%`,
        ref: { y: 5, label: "5%", color: statusPalette.warning },
    },
    power: {
        label: "Power", unit: "W",
        series: [{ key: "power_w", color: slPalette.power, label: "Power" }],
        fmt: (v) => `${v.toFixed(1)} W`,
    },
}

/** Tabs that have data for a range: power exists only in the dish's 15m buffer, obstruction only in the DB. */
export function tabsFor(range: TimeRange): ChartTab[] {
    return range === "15m"
        ? ["throughput", "latency", "loss", "power"]
        : ["throughput", "latency", "loss", "obstruction"]
}

function formatTick(t: number, range: TimeRange): string {
    const d = new Date(t)
    if (range === "24h" || range === "7d") {
        return `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
    }
    if (range === "15m") return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

// var() resolves against the page's own theme, so the tooltip follows a
// light/dark switch instead of staying a hardcoded slate panel.
const tooltipStyle = {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "12px",
    padding: "8px 12px",
    fontSize: 13,
    boxShadow: "0 8px 32px rgb(0 0 0 / 0.25)",
}
const tickStyle = { fontSize: 12, fill: axisPalette.tick }

export function StarlinkCharts({
    points, bands, isLoading, compact = false, timeRange, onTimeRangeChange, tab, onTabChange, liveAvailable,
}: StarlinkChartsProps) {
    const spec = TABS[tab]
    // On desktop the card stretches to the Sky card beside it; the plot takes the slack.
    const chartHeight = compact ? "h-[220px]" : "min-h-[300px] flex-1"
    const controlSize = compact ? "lg" : "sm"

    const rangeOptions = (Object.keys(TIME_RANGE_CONFIG) as TimeRange[])
        .filter((r) => liveAvailable || r !== "15m")
        .map((r) => ({ value: r, label: r, ariaLabel: `Time range ${r}` }))
    const tabOptions = tabsFor(timeRange).map((t) => ({ value: t, label: TABS[t].label }))

    const domain = useMemo<[number, number] | null>(() => {
        if (points.length < 2) return null
        return [points[0].t, points[points.length - 1].t]
    }, [points])

    // A 1 s outage is invisible on an hour-wide axis; give every band a floor
    // of 0.4% of the window so it still shows.
    const visibleBands = useMemo(() => {
        if (!domain) return []
        const [from, to] = domain
        const minW = (to - from) * 0.004
        return bands
            .filter((b) => b.end >= from && b.start <= to)
            .map((b) => ({ ...b, start: Math.max(b.start, from), end: Math.min(Math.max(b.end, b.start + minW), to) }))
    }, [bands, domain])

    const header = (
        <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
            <div className="no-scrollbar max-w-full overflow-x-auto">
                <SegmentedControl value={tab} options={tabOptions} onChange={onTabChange} size={controlSize} />
            </div>
            <SegmentedControl value={timeRange} options={rangeOptions} onChange={onTimeRangeChange} size={controlSize} className="self-start md:self-auto" />
        </div>
    )

    if (isLoading && points.length === 0) {
        return (
            <Card className={cardShell}>
                {header}
                <Skeleton className={`${chartHeight} w-full rounded-xl`} />
            </Card>
        )
    }

    return (
        <Card className={cardShell}>
            {header}
            <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-4">
                    {spec.series.length > 1 && spec.series.map((s) => <LegendKey key={s.key} color={s.color} label={s.label} />)}
                    {visibleBands.length > 0 && <LegendKey color={statusPalette.danger} label="Outage" faded />}
                </div>
                <span className={T.meta}>{spec.unit}</span>
            </div>

            <div className={`${chartHeight} w-full`}>
                {points.length === 0 || !domain ? (
                    <div className="flex h-full flex-col items-center justify-center rounded-xl border-2 border-dashed border-border text-muted-foreground">
                        <p className="text-sm">No data for this range yet</p>
                        <p className={`${T.meta} mt-1`}>Updates every {Math.round(TIME_RANGE_CONFIG[timeRange].refetchInterval / 1000)}s</p>
                    </div>
                ) : (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={points} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                            <defs>
                                {spec.series.map((s) => <Fade key={s.key} id={`sl-grad-${s.key}`} color={s.color} />)}
                            </defs>
                            <CartesianGrid vertical={false} stroke={axisPalette.grid} strokeDasharray="3 3" />
                            <XAxis
                                dataKey="t" type="number" scale="time" domain={domain}
                                tickFormatter={(t) => formatTick(t, timeRange)}
                                axisLine={false} tickLine={false} tick={tickStyle}
                                minTickGap={compact ? 56 : 44}
                            />
                            <YAxis width={44} tick={tickStyle} axisLine={false} tickLine={false} orientation="right"
                                domain={[0, "auto"]} tickFormatter={(v) => (tab === "loss" || tab === "obstruction" ? v.toFixed(1) : v.toFixed(0))} />
                            {visibleBands.map((b, i) => (
                                <ReferenceArea key={i} x1={b.start} x2={b.end} fill={statusPalette.danger} fillOpacity={0.14} strokeOpacity={0} ifOverflow="hidden" />
                            ))}
                            {spec.ref && (
                                <ReferenceLine y={spec.ref.y} stroke={spec.ref.color} strokeDasharray="6 4" strokeOpacity={0.5}
                                    label={{ value: spec.ref.label, position: "left", fill: spec.ref.color, fontSize: 12 }} />
                            )}
                            <Tooltip
                                contentStyle={tooltipStyle}
                                labelStyle={{ color: axisPalette.label, fontSize: 12 }}
                                labelFormatter={(t: any) => formatTick(Number(t), timeRange)}
                                formatter={(value: any, name: any) => {
                                    const s = spec.series.find((x) => x.key === name)
                                    return [value == null ? "—" : spec.fmt(Number(value)), s?.label ?? name]
                                }}
                            />
                            {spec.series.map((s) => (
                                <Area key={s.key} type="monotone" dataKey={s.key} stroke={s.color} strokeWidth={2}
                                    fill={`url(#sl-grad-${s.key})`} dot={false} connectNulls={false} isAnimationActive={false} />
                            ))}
                        </AreaChart>
                    </ResponsiveContainer>
                )}
            </div>
        </Card>
    )
}

function Fade({ id, color }: { id: string; color: string }) {
    return (
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.4} />
            <stop offset="50%" stopColor={color} stopOpacity={0.15} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
        </linearGradient>
    )
}

function LegendKey({ color, label, faded }: { color: string; label: string; faded?: boolean }) {
    return (
        <span className={`flex items-center gap-2 ${T.meta}`}>
            <span className="h-2 w-2 rounded-full" style={{ background: color, opacity: faded ? 0.5 : 1 }} />
            {label}
        </span>
    )
}
