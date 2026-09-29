import { useEffect, useId, useMemo, useState } from "react"
import { Area, CartesianGrid, ComposedChart, Line, ReferenceArea, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { Loader2 } from "lucide-react"
import { useNodeStatsSeries } from "@/lib/queries"
import type { Node, NodeStats, StatsRange } from "@/lib/types"
import { cn, formatSpeed } from "@/lib/utils"
import { axis, chart, network } from "@/lib/design/palette"
import { SegmentedControl, T } from "../node-ui"
import { TrafficSummaryChart } from "../traffic-summary-chart"
import { OverviewCard } from "./overview-card"
import { CHART_RANGES, formatCount, formatTick, formatTooltipTime, rowsFromLive, rowsFromSeries, type ChartRange, type ChartRow } from "./overview-helpers"
import { useLiveBuffer } from "./use-live-buffer"

type ChartTab = "network" | "connections" | "traffic"

const TABS: readonly { value: ChartTab; label: string }[] = [
    { value: "network", label: "Network" },
    { value: "connections", label: "Connections" },
    { value: "traffic", label: "Traffic" },
]

const CONN = { tcp: chart.blue, udp: chart.violet, fd: chart.amber, users: chart.emerald }

interface OverviewChartsProps {
    node: Node
    stats?: NodeStats
    live: boolean
    compact?: boolean
    className?: string
}

function LegendItem({ color, label, value, dashed }: { color: string; label: string; value?: string; dashed?: boolean }) {
    return (
        <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
            <span className={cn("h-2 w-2 shrink-0 rounded-full", dashed && "rounded-none h-0.5 w-3")} style={{ background: color }} aria-hidden />
            <span className="text-muted-foreground">{label}</span>
            {value !== undefined && <span className="font-semibold tabular-nums">{value}</span>}
        </span>
    )
}

/** Short rate for axis ticks: 16040000 → "15M", 7600 → "7.4K". */
function axisSpeed(v: number): string {
    if (v >= 1024 * 1024) return `${+(v / 1024 / 1024).toFixed(v >= 10 * 1024 * 1024 ? 0 : 1)}M`
    if (v >= 1024) return `${+(v / 1024).toFixed(v >= 10 * 1024 ? 0 : 1)}K`
    return `${Math.round(v)}`
}

export function OverviewCharts({ node, stats, live, compact, className }: OverviewChartsProps) {
    const [tab, setTab] = useState<ChartTab>("network")
    const [range, setRange] = useState<ChartRange>("1h")
    const hatchId = useId().replace(/:/g, "")

    // Live needs readings arriving; fall back to recorded history otherwise.
    useEffect(() => {
        if (!node.is_online && range === "live") setRange("24h")
    }, [node.is_online, range])

    const showSeries = tab !== "traffic" && range !== "live"
    const series = useNodeStatsSeries(node.id, (range === "live" ? "1h" : range) as StatsRange, showSeries)
    const buffer = useLiveBuffer(node.id, stats, tab !== "traffic" && range === "live" && node.is_online)

    const rows: ChartRow[] = useMemo(() => {
        if (range === "live") return rowsFromLive(buffer)
        return series.data ? rowsFromSeries(series.data) : []
    }, [range, buffer, series.data])

    const domain: [number, number] | ["dataMin", "dataMax"] = range !== "live" && series.data
        ? [series.data.from * 1000, series.data.to * 1000]
        : ["dataMin", "dataMax"]

    // While the node is offline, mark the stretch since its last recorded
    // bucket ends (not where it starts, or offline time is overstated).
    const lastSeen = useMemo(() => {
        if (node.is_online || range === "live") return null
        const stepMs = (series.data?.step_sec ?? 0) * 1000
        for (let i = rows.length - 1; i >= 0; i--) if (rows[i].down !== null || rows[i].tcp !== null) return rows[i].t + stepMs
        return null
    }, [rows, node.is_online, range, series.data?.step_sec])

    const hasData = rows.some(r => r.down !== null || r.tcp !== null)
    const loading = showSeries && series.isLoading
    const height = compact ? 190 : 270

    const rangeOptions = CHART_RANGES.map(r => r.value === "live" && !node.is_online
        ? { ...r, disabled: true, title: "Needs the node online" }
        : r)

    const tooltip = (
        <Tooltip
            cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
            content={({ active, payload, label }) => {
                if (!active || !payload?.length) return null
                const row = payload[0].payload as ChartRow
                if (row.down === null && row.tcp === null) return null
                return (
                    <div className="rounded-lg border border-border bg-popover px-3 py-2 text-xs shadow-lg">
                        <div className={cn(T.meta, "mb-1")}>{formatTooltipTime(Number(label), range)}{range !== "live" && " (average)"}</div>
                        {tab === "network" ? (
                            <div className="space-y-0.5 tabular-nums">
                                <div><span style={{ color: network.rx }}>↓</span> {formatSpeed(row.down ?? 0)}</div>
                                <div><span style={{ color: network.tx }}>↑</span> {formatSpeed(row.up ?? 0)}</div>
                            </div>
                        ) : (
                            <div className="space-y-0.5 tabular-nums">
                                <div><span style={{ color: CONN.tcp }}>TCP</span> {Math.round(row.tcp ?? 0).toLocaleString()}</div>
                                <div><span style={{ color: CONN.udp }}>UDP</span> {Math.round(row.udp ?? 0).toLocaleString()}</div>
                                <div><span style={{ color: CONN.fd }}>Open files</span> {Math.round(row.fd ?? 0).toLocaleString()}</div>
                                <div><span style={{ color: CONN.users }}>Online users</span> {(row.users ?? 0).toFixed(range === "live" ? 0 : 1)}</div>
                            </div>
                        )}
                    </div>
                )
            }}
        />
    )

    return (
        <OverviewCard glow="blue" className={className}>
            <div className={cn("flex flex-wrap items-center justify-between gap-2", compact && "flex-col items-stretch")}>
                <SegmentedControl value={tab} options={TABS} onChange={setTab} size={compact ? "lg" : "sm"} className={compact ? "w-full [&>button]:flex-1 [&>button]:justify-center" : ""} />
                {tab !== "traffic" && (
                    <SegmentedControl value={range} options={rangeOptions} onChange={setRange} size={compact ? "lg" : "sm"} className={compact ? "w-full [&>button]:flex-1 [&>button]:justify-center" : ""} />
                )}
            </div>

            {tab === "traffic" ? (
                <div className={compact ? "h-[260px]" : "h-[330px]"}>
                    <TrafficSummaryChart nodeId={node.id} />
                </div>
            ) : (
                <>
                    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-1", compact ? "text-[14px]" : "text-xs")}>
                        {tab === "network" ? (
                            <>
                                <LegendItem color={network.rx} label={compact ? "Down" : "Download"} value={live ? formatSpeed(stats?.down_speed ?? 0) : undefined} />
                                <LegendItem color={network.tx} label={compact ? "Up" : "Upload"} value={live ? formatSpeed(stats?.up_speed ?? 0) : undefined} />
                            </>
                        ) : (
                            <>
                                <LegendItem color={CONN.tcp} label="TCP" value={live ? (stats?.tcp_count ?? 0).toLocaleString() : undefined} />
                                <LegendItem color={CONN.udp} label="UDP" value={live ? (stats?.udp_count ?? 0).toLocaleString() : undefined} />
                                <LegendItem color={CONN.fd} label="Open files" dashed value={live ? (stats?.fd_count ?? 0).toLocaleString() : undefined} />
                                <LegendItem color={CONN.users} label="Online users" value={live ? String(stats?.online_users ?? 0) : undefined} />
                            </>
                        )}
                        {loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" aria-label="Loading" />}
                    </div>

                    {/* Grows to fill the card when the specs card beside it is taller. */}
                    <div style={{ minHeight: height }} className="relative w-full flex-1">
                        {!hasData && !loading ? (
                            <div className={cn(T.meta, "absolute inset-0 flex items-center justify-center")}>
                                {range === "live" ? "Waiting for the first readings" : "Nothing recorded in this range"}
                            </div>
                        ) : (
                            <ResponsiveContainer width="100%" height="100%" className="absolute inset-0">
                                <ComposedChart data={rows} margin={{ top: 8, right: 4, left: 0, bottom: 0 }}>
                                    <defs>
                                        <linearGradient id={`${hatchId}-down`} x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="0%" stopColor={network.rx} stopOpacity={0.3} />
                                            <stop offset="100%" stopColor={network.rx} stopOpacity={0} />
                                        </linearGradient>
                                        <pattern id={`${hatchId}-hatch`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
                                            <line x1="0" y1="0" x2="0" y2="6" stroke="rgba(248,113,113,.3)" strokeWidth="2" />
                                        </pattern>
                                    </defs>
                                    <CartesianGrid vertical={false} stroke={axis.grid} strokeDasharray="3 3" />
                                    <XAxis
                                        dataKey="t"
                                        type="number"
                                        scale="time"
                                        domain={domain}
                                        tickFormatter={(t) => formatTick(Number(t), range)}
                                        tick={{ fontSize: 12, fill: axis.tick }}
                                        axisLine={false}
                                        tickLine={false}
                                        minTickGap={compact ? 40 : 56}
                                    />
                                    {tab === "network" ? (
                                        <>
                                            <YAxis orientation="right" width={44} tickFormatter={axisSpeed} tick={{ fontSize: 12, fill: axis.tick }} axisLine={false} tickLine={false} />
                                            <Area type="monotone" dataKey="down" stroke={network.rx} strokeWidth={1.75} fill={`url(#${hatchId}-down)`} isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="up" stroke={network.tx} strokeWidth={1.5} isAnimationActive={false} connectNulls={false} dot={false} />
                                        </>
                                    ) : (
                                        <>
                                            <YAxis orientation="right" width={compact ? 48 : 56} tickFormatter={formatCount} tick={{ fontSize: 12, fill: axis.tick }} axisLine={false} tickLine={false} />
                                            <YAxis yAxisId="users" hide domain={[0, (max: number) => Math.max(1, Math.ceil(max * 1.2))]} />
                                            <Line type="monotone" dataKey="tcp" stroke={CONN.tcp} strokeWidth={1.75} isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="udp" stroke={CONN.udp} strokeWidth={1.5} isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="fd" stroke={CONN.fd} strokeWidth={1.25} strokeDasharray="4 2" isAnimationActive={false} connectNulls={false} dot={false} />
                                            <Line type="monotone" dataKey="users" yAxisId="users" stroke={CONN.users} strokeWidth={1.5} isAnimationActive={false} connectNulls={false} dot={false} />
                                        </>
                                    )}
                                    {lastSeen !== null && typeof domain[1] === "number" && lastSeen < domain[1] && (
                                        <ReferenceArea x1={lastSeen} x2={domain[1]} fill={`url(#${hatchId}-hatch)`} fillOpacity={1} ifOverflow="hidden" label={{ value: "Offline", position: "insideTopLeft", fill: "#f87171", fontSize: 12 }} />
                                    )}
                                    {tooltip}
                                </ComposedChart>
                            </ResponsiveContainer>
                        )}
                    </div>
                </>
            )}
        </OverviewCard>
    )
}
