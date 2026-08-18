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
import type { StarlinkDataPoint } from "@/lib/types"
import { type TimeRange, TIME_RANGE_CONFIG, formatMbps } from "./starlink-helpers"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"

interface StarlinkChartsProps {
    data: StarlinkDataPoint[]
    isLoading: boolean
    compact?: boolean
    timeRange: TimeRange
    onTimeRangeChange: (range: TimeRange) => void
}

type ChartTab = "throughput" | "latency" | "loss" | "obstruction"

const chartTabs: { key: ChartTab; label: string }[] = [
    { key: "throughput", label: "Throughput" },
    { key: "latency", label: "Latency" },
    { key: "loss", label: "Packet Loss" },
    { key: "obstruction", label: "Obstruction" },
]

const timeRanges: TimeRange[] = ["1h", "6h", "24h", "7d"]

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

export function StarlinkCharts({ data, isLoading, compact = false, timeRange, onTimeRangeChange }: StarlinkChartsProps) {
    const c = useChartPalette()
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

    if (isLoading && chartData.length === 0) {
        return (
            <Card className="relative overflow-hidden rounded-2xl p-4 md:p-6 bg-card/50 backdrop-blur-sm border-white/5 transition-shadow duration-300 hover:shadow-lg hover:shadow-blue-500/10">
                <div className="flex flex-wrap items-center gap-1.5 mb-4">
                    {chartTabs.map((t) => (
                        <Skeleton key={t.key} className="h-7 w-20 rounded-md" />
                    ))}
                </div>
                <Skeleton className={`${chartHeight} w-full rounded-lg`} />
            </Card>
        )
    }

    if (chartData.length === 0) {
        return (
            <Card className="relative overflow-hidden rounded-2xl p-4 md:p-6 bg-card/50 backdrop-blur-sm border-white/5 transition-shadow duration-300 hover:shadow-lg hover:shadow-blue-500/10">
                <div className={`${chartHeight} flex flex-col items-center justify-center text-muted-foreground border-2 border-dashed border-white/5 rounded-xl`}>
                    <p className="text-sm">No history data yet</p>
                    <p className="text-xs mt-1 text-muted-foreground/60">Updates every {Math.round(TIME_RANGE_CONFIG[timeRange].refetchInterval / 1000)}s</p>
                </div>
            </Card>
        )
    }

    return (
        <Card className="relative overflow-hidden rounded-2xl p-4 md:p-6 bg-card/50 backdrop-blur-sm border-white/5 transition-shadow duration-300 hover:shadow-lg hover:shadow-blue-500/10">
            <div className="flex flex-col gap-2 mb-4 md:mb-6 md:flex-row md:items-center md:justify-between">
                <div className="overflow-x-auto no-scrollbar max-w-full">
                    <div className="flex items-center gap-1.5 bg-muted/30 rounded-lg p-0.5 w-max">
                        {chartTabs.map((t) => (
                            <button
                                key={t.key}
                                type="button"
                                aria-pressed={tab === t.key}
                                onClick={() => setTab(t.key)}
                                className={`shrink-0 whitespace-nowrap px-3 py-1.5 rounded-md text-[11px] font-bold transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                                    tab === t.key
                                        ? "bg-foreground text-background shadow-sm"
                                        : "text-muted-foreground hover:text-foreground"
                                }`}
                            >
                                {t.label}
                            </button>
                        ))}
                    </div>
                </div>

                <div className="flex items-center gap-1 bg-muted/30 rounded-lg p-0.5 shrink-0 self-start md:self-auto">
                    {timeRanges.map((r) => (
                        <button
                            key={r}
                            type="button"
                            aria-pressed={timeRange === r}
                            aria-label={`Time range ${r}`}
                            onClick={() => onTimeRangeChange(r)}
                            className={`shrink-0 px-2 py-1 rounded-md text-[11px] font-bold transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                                timeRange === r
                                    ? "bg-foreground text-background shadow-sm"
                                    : "text-muted-foreground hover:text-foreground"
                            }`}
                        >
                            {r}
                        </button>
                    ))}
                </div>
            </div>

            {tab === "throughput" && (
                <div className="flex items-center gap-4 mb-3">
                    <div className="flex items-center gap-2">
                        <div className="w-2 h-2 rounded-full bg-emerald-500" />
                        <span className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-wider">Download</span>
                    </div>
                    <div className="flex items-center gap-2">
                        <div className="w-2 h-2 rounded-full bg-blue-500" />
                        <span className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-wider">Upload</span>
                    </div>
                </div>
            )}

            <div className={`${chartHeight} w-full`}>
                {tab === "throughput" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                            <defs>
                                <linearGradient id="sl-gradient-dl" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.success} stopOpacity={0.4} />
                                    <stop offset="50%" stopColor={c.success} stopOpacity={0.15} />
                                    <stop offset="100%" stopColor={c.success} stopOpacity={0} />
                                </linearGradient>
                                <linearGradient id="sl-gradient-ul" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.info} stopOpacity={0.4} />
                                    <stop offset="50%" stopColor={c.info} stopOpacity={0.15} />
                                    <stop offset="100%" stopColor={c.info} stopOpacity={0} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            <XAxis dataKey="time" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: c.axis }} interval={Math.max(Math.floor(chartData.length / 6), 1)} />
                            <YAxis tickFormatter={(v) => `${v.toFixed(0)} Mbps`} width={80} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" />
                            <Tooltip contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }} labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                                formatter={(value: any, name: any) => [`${formatMbps((value ?? 0) * 1_000_000)} Mbps`, name === "dl_mbps" ? "Download" : "Upload"]}
                            />
                            <Area type="monotone" dataKey="dl_mbps" stroke={c.success} strokeWidth={2} fill="url(#sl-gradient-dl)" dot={false} animationDuration={500} />
                            <Area type="monotone" dataKey="ul_mbps" stroke={c.info} strokeWidth={2} fill="url(#sl-gradient-ul)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "latency" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                            <defs>
                                <linearGradient id="sl-gradient-lat" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.warning} stopOpacity={0.4} />
                                    <stop offset="50%" stopColor={c.warning} stopOpacity={0.15} />
                                    <stop offset="100%" stopColor={c.warning} stopOpacity={0} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            <XAxis dataKey="time" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: c.axis }} interval={Math.max(Math.floor(chartData.length / 6), 1)} />
                            <YAxis tickFormatter={(v) => `${v.toFixed(0)} ms`} width={60} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" />
                            <ReferenceLine y={50} stroke={c.danger} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: "50ms", position: "left", fill: c.danger, fontSize: 10 }} />
                            <Tooltip contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }} labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                                formatter={(value: any) => [`${(value ?? 0).toFixed(1)} ms`, "Latency"]}
                            />
                            <Area type="monotone" dataKey="pop_ping_latency_ms" stroke={c.warning} strokeWidth={2} fill="url(#sl-gradient-lat)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "loss" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                            <defs>
                                <linearGradient id="sl-gradient-loss" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.danger} stopOpacity={0.4} />
                                    <stop offset="50%" stopColor={c.danger} stopOpacity={0.15} />
                                    <stop offset="100%" stopColor={c.danger} stopOpacity={0} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            <XAxis dataKey="time" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: c.axis }} interval={Math.max(Math.floor(chartData.length / 6), 1)} />
                            <YAxis tickFormatter={(v) => `${v.toFixed(1)}%`} width={50} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" domain={[0, "auto"]} />
                            <Tooltip contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }} labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                                formatter={(value: any) => [`${(value ?? 0).toFixed(2)}%`, "Packet Loss"]}
                            />
                            <Area type="monotone" dataKey="drop_pct" stroke={c.danger} strokeWidth={2} fill="url(#sl-gradient-loss)" dot={false} animationDuration={500} />
                        </AreaChart>
                    </ResponsiveContainer>
                )}

                {tab === "obstruction" && (
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                            <defs>
                                <linearGradient id="sl-gradient-obst" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.chart5} stopOpacity={0.4} />
                                    <stop offset="50%" stopColor={c.chart5} stopOpacity={0.15} />
                                    <stop offset="100%" stopColor={c.chart5} stopOpacity={0} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                            <XAxis dataKey="time" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: c.axis }} interval={Math.max(Math.floor(chartData.length / 6), 1)} />
                            <YAxis tickFormatter={(v) => `${v.toFixed(1)}%`} width={50} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" domain={[0, "auto"]} />
                            <ReferenceLine y={5} stroke={c.warning} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: "5%", position: "left", fill: c.warning, fontSize: 10 }} />
                            <Tooltip contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }} labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                                // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
