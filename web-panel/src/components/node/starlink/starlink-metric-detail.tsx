import { useMemo } from "react"
import { Area, AreaChart, ResponsiveContainer, YAxis, XAxis, Tooltip, CartesianGrid, ReferenceLine } from "recharts"
import type { StarlinkDataPoint } from "@/lib/types"
import { formatMbps, type TimeRange, TIME_RANGE_CONFIG } from "./starlink-helpers"
import { useChartPalette, tooltipContentStyle, type ChartPalette } from "@/lib/design/palette"

interface StarlinkMetricDetailProps {
    metricType: "latency" | "dropRate" | "download" | "upload"
    data: StarlinkDataPoint[]
    timeRange: TimeRange
    onTimeRangeChange: (range: TimeRange) => void
}

const METRIC_CONFIG = {
    latency: { key: "pop_ping_latency_ms" as const, label: "Latency", unit: "ms", colorKey: "warning" as keyof ChartPalette, refLine: { y: 50, label: "50ms" } },
    dropRate: { key: "pop_ping_drop_rate" as const, label: "Packet Loss", unit: "%", colorKey: "danger" as keyof ChartPalette, refLine: null },
    download: { key: "downlink_throughput_bps" as const, label: "Download", unit: "Mbps", colorKey: "success" as keyof ChartPalette, refLine: null },
    upload: { key: "uplink_throughput_bps" as const, label: "Upload", unit: "Mbps", colorKey: "info" as keyof ChartPalette, refLine: null },
}

function formatTime(dateStr: string, range: TimeRange): string {
    const d = new Date(dateStr)
    if (range === "24h" || range === "7d") {
        const date = d.toLocaleDateString([], { month: "short", day: "numeric" })
        const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
        return `${date} ${time}`
    }
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

export function StarlinkMetricDetail({ metricType, data, timeRange, onTimeRangeChange }: StarlinkMetricDetailProps) {
    const c = useChartPalette()
    const config = METRIC_CONFIG[metricType]
    const color = c[config.colorKey]

    const chartData = useMemo(() => {
        // Backend returns oldest→newest; render in that order.
        return data.map(d => {
            let value = d[config.key] as number
            if (metricType === "dropRate") value = value * 100
            if (metricType === "download" || metricType === "upload") value = value / 1_000_000
            return { value, time: formatTime(d.created_at, timeRange) }
        })
    }, [data, config.key, metricType, timeRange])

    const stats = useMemo(() => {
        if (chartData.length === 0) return { min: 0, avg: 0, max: 0 }
        const vals = chartData.map(d => d.value)
        return {
            min: Math.min(...vals),
            avg: vals.reduce((a, b) => a + b, 0) / vals.length,
            max: Math.max(...vals),
        }
    }, [chartData])

    const formatValue = (v: number) => {
        if (metricType === "download" || metricType === "upload") return `${formatMbps(v * 1_000_000)} ${config.unit}`
        if (metricType === "dropRate") return `${v.toFixed(2)}${config.unit}`
        return `${v.toFixed(1)} ${config.unit}`
    }

    return (
        <div className="space-y-4">
            {/* Time range selector */}
            <div className="flex items-center gap-1 bg-muted/30 rounded-lg p-0.5 w-fit">
                {(Object.keys(TIME_RANGE_CONFIG) as TimeRange[]).map(r => (
                    <button
                        key={r}
                        type="button"
                        aria-pressed={timeRange === r}
                        aria-label={`Time range ${r}`}
                        onClick={() => onTimeRangeChange(r)}
                        className={`px-3 py-1.5 rounded-md text-xs font-bold transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                            timeRange === r ? "bg-foreground text-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                        }`}
                    >
                        {r}
                    </button>
                ))}
            </div>

            {/* Stats row */}
            <div className="flex justify-between px-2">
                {(["min", "avg", "max"] as const).map(stat => (
                    <div key={stat} className="flex flex-col items-center">
                        <span className="text-xs uppercase font-bold text-muted-foreground/60 tracking-wider">{stat}</span>
                        <span className="text-sm font-bold font-mono">{formatValue(stats[stat])}</span>
                    </div>
                ))}
            </div>

            {/* Chart */}
            <div className="h-[280px] w-full">
                <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                        <defs>
                            <linearGradient id="sl-detail-grad" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={color} stopOpacity={0.4} />
                                <stop offset="50%" stopColor={color} stopOpacity={0.15} />
                                <stop offset="100%" stopColor={color} stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                        <XAxis dataKey="time" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: c.axis }} interval={Math.max(Math.floor(chartData.length / 6), 1)} />
                        <YAxis tickFormatter={(v) => `${v.toFixed(0)}`} width={50} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" />
                        {config.refLine && (
                            <ReferenceLine y={config.refLine.y} stroke={c.danger} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: config.refLine.label, position: "left", fill: c.danger, fontSize: 10 }} />
                        )}
                        <Tooltip
                            contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }}
                            labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                            // eslint-disable-next-line @typescript-eslint/no-explicit-any
                            formatter={(value: any) => [formatValue(value ?? 0), config.label]}
                        />
                        <Area type="monotone" dataKey="value" stroke={color} strokeWidth={2} fill="url(#sl-detail-grad)" dot={false} activeDot={{ r: 4, fill: color, stroke: "#fff", strokeWidth: 2 }} isAnimationActive={false} />
                    </AreaChart>
                </ResponsiveContainer>
            </div>
        </div>
    )
}
