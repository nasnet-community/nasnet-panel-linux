import { useEffect, useId, useMemo, useState } from "react"
import { Area, CartesianGrid, ComposedChart, Line, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { Loader2 } from "lucide-react"
import { useNodeStatsSeries } from "@/lib/queries"
import type { Node, NodeStatsSeries, StatsRange } from "@/lib/types"
import { cn, formatSpeed } from "@/lib/utils"
import { axis, chart, network } from "@/lib/design/palette"
import { SegmentedControl, T } from "@/components/node/node-ui"
import { TrafficSummaryChart } from "@/components/node/traffic-summary-chart"
import { OverviewCard } from "@/components/node/overview/overview-card"
import { CHART_RANGES, formatCount, formatTick, formatTooltipTime, type ChartRange } from "@/components/node/overview/overview-helpers"
import type { MtprotoLivePoint } from "./use-mtproto-live"

type ChartTab = "throughput" | "streams" | "traffic"

const TABS: readonly { value: ChartTab; label: string }[] = [
    { value: "throughput", label: "Throughput" },
    { value: "streams", label: "Streams" },
    { value: "traffic", label: "Traffic" },
]

const COLORS = { streams: chart.blue, users: chart.emerald }

interface Row {
    t: number
    down: number | null
    up: number | null
    streams: number | null
    users: number | null
}

function axisSpeed(v: number): string {
    if (v >= 1024 * 1024) return `${+(v / 1024 / 1024).toFixed(v >= 10 * 1024 * 1024 ? 0 : 1)}M`
    if (v >= 1024) return `${+(v / 1024).toFixed(v >= 10 * 1024 ? 0 : 1)}K`
    return `${Math.round(v)}`
}

function Legend({ color, label, value }: { color: string; label: string; value?: string }) {
    return (
        <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
            <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: color }} aria-hidden />
            <span className="text-muted-foreground">{label}</span>
            {value !== undefined && <span className="font-semibold tabular-nums">{value}</span>}
        </span>
    )
}

// a gap wherever a few polls went missing, so the line doesn't bridge an outage
function liveRows(points: MtprotoLivePoint[]): Row[] {
    const rows: Row[] = []
    let prev: number | null = null
    for (const p of points) {
        if (prev !== null && p.t - prev > 35_000) rows.push({ t: prev + 10_000, down: null, up: null, streams: null, users: null })
        rows.push({ t: p.t, down: p.down, up: p.up, streams: p.streams, users: p.users })
        prev = p.t
    }
    return rows
}

// a bucket's users are its peak: people are whole, and an average of 0.5 reads as nonsense
function seriesRows(s: NodeStatsSeries): Row[] {
    const byT = new Map(s.points.map((p) => [p.t, p]))
    const rows: Row[] = []
    for (let t = Math.floor(s.from / s.step_sec) * s.step_sec; t <= s.to; t += s.step_sec) {
        const p = byT.get(t)
        rows.push(p
            ? { t: t * 1000, down: p.down_avg, up: p.up_avg, streams: Math.round(p.tcp_avg), users: Math.round(p.users_max) }
            : { t: t * 1000, down: null, up: null, streams: null, users: null })
    }
    return rows
}

interface MtprotoChartsProps {
    node: Node
    live: MtprotoLivePoint[]
    /** current readings for the legend */
    now?: { down: number; up: number; streams: number; users?: number }
    compact?: boolean
    className?: string
}

/** Throughput and streams over time, plus daily traffic, in the full node's chart card. */
export function MtprotoCharts({ node, live, now, compact, className }: MtprotoChartsProps) {
    const [tab, setTab] = useState<ChartTab>("throughput")
    const [range, setRange] = useState<ChartRange>("1h")
    const gradientId = useId().replace(/:/g, "")

    useEffect(() => {
        if (!node.is_online && range === "live") setRange("24h")
    }, [node.is_online, range])

    const showSeries = tab !== "traffic" && range !== "live"
    const series = useNodeStatsSeries(node.id, (range === "live" ? "1h" : range) as StatsRange, showSeries)

    const rows: Row[] = useMemo(() => {
        if (range === "live") return liveRows(live)
        if (!series.data) return []
        return seriesRows(series.data)
    }, [range, live, series.data])

    const domain: [number, number] | ["dataMin", "dataMax"] = range !== "live" && series.data
        ? [series.data.from * 1000, series.data.to * 1000]
        : ["dataMin", "dataMax"]
    const hasData = rows.some((r) => r.down !== null || r.streams !== null)
    const loading = showSeries && series.isLoading
    const height = compact ? 190 : 270
    const rangeOptions = CHART_RANGES.map((r) => (r.value === "live" && !node.is_online ? { ...r, disabled: true, title: "Needs the node online" } : r))
    const ctrl = compact ? "w-full [&>button]:flex-1 [&>button]:justify-center" : ""

    return (
        <OverviewCard glow="blue" className={className}>
            <div className={cn("flex flex-wrap items-center justify-between gap-2", compact && "flex-col items-stretch")}>
                <SegmentedControl value={tab} options={TABS} onChange={setTab} size={compact ? "lg" : "sm"} className={ctrl} />
                {tab !== "traffic" && <SegmentedControl value={range} options={rangeOptions} onChange={setRange} size={compact ? "lg" : "sm"} className={ctrl} />}
            </div>

            {tab === "traffic" ? (
                <div className={compact ? "h-[260px]" : "h-[330px]"}>
                    <TrafficSummaryChart nodeId={node.id} />
                </div>
            ) : (
                <>
                    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-1", compact ? "text-[14px]" : "text-xs")}>
                        {tab === "throughput" ? (
                            <>
                                <Legend color={network.rx} label="From Telegram" value={now && node.is_online ? formatSpeed(now.down) : undefined} />
                                <Legend color={network.tx} label="To Telegram" value={now && node.is_online ? formatSpeed(now.up) : undefined} />
                            </>
                        ) : (
                            <>
                                <Legend color={COLORS.streams} label="Open streams" value={now && node.is_online ? now.streams.toLocaleString() : undefined} />
                                <Legend color={COLORS.users} label="Users online" value={now?.users !== undefined && node.is_online ? String(now.users) : undefined} />
                            </>
                        )}
                        {loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" aria-label="Loading" />}
                    </div>

                    <div style={{ minHeight: height }} className="relative w-full flex-1">
                        {!hasData && !loading ? (
                            <div className={cn(T.meta, "absolute inset-0 flex items-center justify-center text-center")}>
                                {range === "live" ? "Readings appear here every 10 seconds" : "Nothing recorded in this range"}
                            </div>
                        ) : (
                            <ResponsiveContainer width="100%" height="100%" className="absolute inset-0">
                                <ComposedChart data={rows} margin={{ top: 8, right: 4, left: 0, bottom: 0 }}>
                                    <defs>
                                        <linearGradient id={`${gradientId}-down`} x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="0%" stopColor={network.rx} stopOpacity={0.3} />
                                            <stop offset="100%" stopColor={network.rx} stopOpacity={0} />
                                        </linearGradient>
                                    </defs>
                                    <CartesianGrid vertical={false} stroke={axis.grid} strokeDasharray="3 3" />
                                    <XAxis
                                        dataKey="t" type="number" scale="time" domain={domain}
                                        tickFormatter={(t) => formatTick(Number(t), range)}
                                        tick={{ fontSize: 12, fill: axis.tick }} axisLine={false} tickLine={false}
                                        minTickGap={compact ? 40 : 56}
                                    />
                                    {tab === "throughput" ? (
                                        <>
                                            <YAxis orientation="right" width={44} tickFormatter={axisSpeed} tick={{ fontSize: 12, fill: axis.tick }} axisLine={false} tickLine={false} />
                                            <Area type="monotone" dataKey="down" stroke={network.rx} strokeWidth={1.75} fill={`url(#${gradientId}-down)`} isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="up" stroke={network.tx} strokeWidth={1.5} isAnimationActive={false} connectNulls={false} dot={false} />
                                        </>
                                    ) : (
                                        <>
                                            <YAxis orientation="right" width={compact ? 48 : 56} tickFormatter={formatCount} tick={{ fontSize: 12, fill: axis.tick }} axisLine={false} tickLine={false} allowDecimals={false} />
                                            <YAxis yAxisId="users" hide domain={[0, (max: number) => Math.max(1, Math.ceil(max * 1.2))]} />
                                            <Line type="monotone" dataKey="streams" stroke={COLORS.streams} strokeWidth={1.75} isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="users" yAxisId="users" stroke={COLORS.users} strokeWidth={1.5} isAnimationActive={false} connectNulls={false} dot={false} />
                                        </>
                                    )}
                                    <Tooltip
                                        cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
                                        content={({ active, payload, label }) => {
                                            if (!active || !payload?.length) return null
                                            const row = payload[0].payload as Row
                                            if (row.down === null && row.streams === null) return null
                                            return (
                                                <div className="rounded-lg border border-border bg-popover px-3 py-2 text-xs shadow-lg">
                                                    <div className={cn(T.meta, "mb-1")}>{formatTooltipTime(Number(label), range)}{range !== "live" && tab === "throughput" && " (average)"}</div>
                                                    {tab === "throughput" ? (
                                                        <div className="space-y-0.5 tabular-nums">
                                                            <div><span style={{ color: network.rx }}>↓</span> {formatSpeed(row.down ?? 0)} from Telegram</div>
                                                            <div><span style={{ color: network.tx }}>↑</span> {formatSpeed(row.up ?? 0)} to Telegram</div>
                                                        </div>
                                                    ) : (
                                                        <div className="space-y-0.5 tabular-nums">
                                                            <div><span style={{ color: COLORS.streams }}>Streams</span> {Math.round(row.streams ?? 0).toLocaleString()}</div>
                                                            {row.users !== null && <div><span style={{ color: COLORS.users }}>Users online</span> {Math.round(row.users ?? 0)}{range !== "live" && <span className="text-muted-foreground"> at most</span>}</div>}
                                                        </div>
                                                    )}
                                                </div>
                                            )
                                        }}
                                    />
                                </ComposedChart>
                            </ResponsiveContainer>
                        )}
                    </div>
                </>
            )}
        </OverviewCard>
    )
}
