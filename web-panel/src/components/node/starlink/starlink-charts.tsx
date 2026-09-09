import { useMemo, useState } from "react"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Area,
    AreaChart,
    ResponsiveContainer,
    YAxis,
    XAxis,
    Tooltip,
    CartesianGrid,
    ReferenceLine,
} from "recharts"
import { useChartPalette } from "@/lib/design/palette"
import type { StarlinkDataPoint } from "@/lib/types"
import { type TimeRange, TIME_RANGE_CONFIG, formatMbps } from "./starlink-helpers"
import { SegmentedControl, T, cardShell } from "./starlink-ui"

interface StarlinkChartsProps {
    data: StarlinkDataPoint[]
    isLoading: boolean
    compact?: boolean
    timeRange: TimeRange
    onTimeRangeChange: (range: TimeRange) => void
}

type ChartTab = "throughput" | "latency" | "loss" | "obstruction"

const chartTabs = [
    { value: "throughput" as const, label: "Throughput" },
    { value: "latency" as const, label: "Latency" },
    { value: "loss" as const, label: "Packet loss" },
    { value: "obstruction" as const, label: "Obstruction" },
]

const timeRangeOptions = (Object.keys(TIME_RANGE_CONFIG) as TimeRange[]).map(r => ({
    value: r,
    label: r,
    ariaLabel: `Time range ${r}`,
}))

// The unit belongs on the axis once, as a caption. Every tick used to carry
// it ("80 Mbps", "60 Mbps", …), which cost 80px of plot width to repeat one
// word five times.
const UNIT: Record<ChartTab, string> = {
    throughput: "Mbps",
    latency: "ms",
    loss: "%",
    obstruction: "%",
}

// formatTime renders a chart X-axis tick. For windows that span multiple
// days (24h, 7d) HH:MM alone collapses every day onto the same labels, so we
// prefix the date for those ranges.
function formatTime(dateStr: string, range: TimeRange): string {
    const d = new Date(dateStr)
    if (range === "24h" || range === "7d") {
        const date = d.toLocaleDateString([], { month: "short", day: "numeric" })
        const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
        return `${date} ${time}`
    }
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

// var() resolves against the artifact's own theme, so the tooltip follows a
// light/dark switch instead of staying a hardcoded slate panel.
const tooltipStyle = {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "12px",
    padding: "8px 12px",
    fontSize: 12,
    boxShadow: "0 8px 32px rgb(0 0 0 / 0.25)",
}

export function StarlinkCharts({ data, isLoading, compact = false, timeRange, onTimeRangeChange }: StarlinkChartsProps) {
    const c = useChartPalette()
    const tooltipLabelStyle = { color: c.tooltipLabel, fontSize: 11 }
    const tickStyle = { fontSize: 11, fill: c.axis }
    const [tab, setTab] = useState<ChartTab>("throughput")

    const chartData = useMemo(() => data.map((d) => ({
        ...d,
        dl_mbps: d.downlink_throughput_bps / 1_000_000,
        ul_mbps: d.uplink_throughput_bps / 1_000_000,
        drop_pct: d.pop_ping_drop_rate * 100,
        obstruction_pct: d.obstruction_fraction * 100,
        time: formatTime(d.created_at, timeRange),
    })), [data, timeRange])

    const chartHeight = compact ? "h-[200px]" : "h-[280px]"
    const controlSize = compact ? "lg" : "sm"

    if (isLoading && chartData.length === 0) {
        return (
            <Card className={cardShell}>
                <div className="mb-4 flex flex-wrap items-center gap-1.5">
                    {chartTabs.map((t) => <Skeleton key={t.value} className="h-7 w-20 rounded-lg" />)}
                </div>
                <Skeleton className={`${chartHeight} w-full rounded-xl`} />
            </Card>
        )
    }

    if (chartData.length === 0) {
        return (
            <Card className={cardShell}>
                <div className={`${chartHeight} flex flex-col items-center justify-center rounded-xl border-2 border-dashed border-border text-muted-foreground`}>
                    <p className="text-sm">No history data yet</p>
                    <p className={`${T.meta} mt-1`}>Updates every {Math.round(TIME_RANGE_CONFIG[timeRange].refetchInterval / 1000)}s</p>
                </div>
            </Card>
        )
    }

    // preserveStartEnd + minTickGap lets Recharts drop labels that would
    // collide. The old fixed `interval` was computed from the row count
    // alone, so a phone rendered six labels into 326px as "6 PM05:37 PM…".
    const xAxis = (
        <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            tick={tickStyle}
            interval="preserveStartEnd"
            minTickGap={compact ? 56 : 44}
        />
    )

    return (
        <Card className={cardShell}>
            <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                <div className="no-scrollbar max-w-full overflow-x-auto">
                    <SegmentedControl value={tab} options={chartTabs} onChange={setTab} size={controlSize} />
                </div>
                <SegmentedControl
                    value={timeRange}
                    options={timeRangeOptions}
                    onChange={onTimeRangeChange}
                    size={controlSize}
                    className="self-start md:self-auto"
                />
            </div>

            <div className="flex items-center justify-between gap-3">
                {tab === "throughput" ? (
                    <div className="flex items-center gap-4">
                        <LegendKey color={c.success} label="Download" />
                        <LegendKey color={c.info} label="Upload" />
                    </div>
                ) : <span />}
                <span className={T.meta}>{UNIT[tab]}</span>
            </div>

            <div className={`${chartHeight} w-full`}>
                {tab === "throughput" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                            <defs>
                                <Fade id="sl-gradient-dl" color={c.success} />
                                <Fade id="sl-gradient-ul" color={c.info} />
                            </defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            {xAxis}
                            <YAxis tickFormatter={(v) => v.toFixed(0)} width={44} tick={tickStyle} axisLine={false} tickLine={false} orientation="right" />
                            <Tooltip contentStyle={tooltipStyle} labelStyle={tooltipLabelStyle}
                                formatter={(value: any, name: any) => [`${formatMbps((value ?? 0) * 1_000_000)} Mbps`, name === "dl_mbps" ? "Download" : "Upload"]}
                            />
                            <Area type="monotone" dataKey="dl_mbps" stroke={c.success} strokeWidth={2} fill="url(#sl-gradient-dl)" dot={false} animationDuration={500} />
                            <Area type="monotone" dataKey="ul_mbps" stroke={c.info} strokeWidth={2} fill="url(#sl-gradient-ul)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "latency" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                            <defs><Fade id="sl-gradient-lat" color={c.warning} /></defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            {xAxis}
                            <YAxis tickFormatter={(v) => v.toFixed(0)} width={40} tick={tickStyle} axisLine={false} tickLine={false} orientation="right" />
                            <ReferenceLine y={50} stroke={c.danger} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: "50ms", position: "left", fill: c.danger, fontSize: 11 }} />
                            <Tooltip contentStyle={tooltipStyle} labelStyle={tooltipLabelStyle}
                                formatter={(value: any) => [`${(value ?? 0).toFixed(1)} ms`, "Latency"]}
                            />
                            <Area type="monotone" dataKey="pop_ping_latency_ms" stroke={c.warning} strokeWidth={2} fill="url(#sl-gradient-lat)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "loss" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                            <defs><Fade id="sl-gradient-loss" color={c.danger} /></defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            {xAxis}
                            <YAxis tickFormatter={(v) => v.toFixed(1)} width={40} tick={tickStyle} axisLine={false} tickLine={false} orientation="right" domain={[0, "auto"]} />
                            <Tooltip contentStyle={tooltipStyle} labelStyle={tooltipLabelStyle}
                                formatter={(value: any) => [`${(value ?? 0).toFixed(2)}%`, "Packet loss"]}
                            />
                            <Area type="monotone" dataKey="drop_pct" stroke={c.danger} strokeWidth={2} fill="url(#sl-gradient-loss)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "obstruction" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                            <defs><Fade id="sl-gradient-obst" color={c.chart5} /></defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            {xAxis}
                            <YAxis tickFormatter={(v) => v.toFixed(1)} width={40} tick={tickStyle} axisLine={false} tickLine={false} orientation="right" domain={[0, "auto"]} />
                            <ReferenceLine y={5} stroke={c.warning} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: "5%", position: "left", fill: c.warning, fontSize: 11 }} />
                            <Tooltip contentStyle={tooltipStyle} labelStyle={tooltipLabelStyle}
                                formatter={(value: any) => [`${(value ?? 0).toFixed(2)}%`, "Obstruction"]}
                            />
                            <Area type="monotone" dataKey="obstruction_pct" stroke={c.chart5} strokeWidth={2} fill="url(#sl-gradient-obst)" dot={false} animationDuration={500} />
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

function LegendKey({ color, label }: { color: string; label: string }) {
    return (
        <span className={`flex items-center gap-2 ${T.meta}`}>
            <span className="h-2 w-2 rounded-full" style={{ background: color }} />
            {label}
        </span>
    )
}
