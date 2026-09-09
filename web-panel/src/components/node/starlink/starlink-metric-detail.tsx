import { useMemo } from "react"
import { Area, AreaChart, ResponsiveContainer, YAxis, XAxis, Tooltip, CartesianGrid, ReferenceLine } from "recharts"
import { useChartPalette, type ChartPalette } from "@/lib/design/palette"
import type { StarlinkDataPoint } from "@/lib/types"
import { formatMbps, type TimeRange, TIME_RANGE_CONFIG } from "./starlink-helpers"
import { SegmentedControl, T } from "./starlink-ui"

interface StarlinkMetricDetailProps {
    metricType: "latency" | "dropRate" | "download" | "upload"
    data: StarlinkDataPoint[]
    timeRange: TimeRange
    onTimeRangeChange: (range: TimeRange) => void
}

const metricConfig = (c: ChartPalette) => ({
    latency: { key: "pop_ping_latency_ms" as const, label: "Latency", unit: "ms", color: c.warning, refLine: { y: 50, label: "50ms" } },
    dropRate: { key: "pop_ping_drop_rate" as const, label: "Packet loss", unit: "%", color: c.danger, refLine: null },
    download: { key: "downlink_throughput_bps" as const, label: "Download", unit: "Mbps", color: c.success, refLine: null },
    upload: { key: "uplink_throughput_bps" as const, label: "Upload", unit: "Mbps", color: c.info, refLine: null },
})

const timeRangeOptions = (Object.keys(TIME_RANGE_CONFIG) as TimeRange[]).map(r => ({
    value: r,
    label: r,
    ariaLabel: `Time range ${r}`,
}))

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
    const config = metricConfig(c)[metricType]

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
        if (metricType === "download" || metricType === "upload") return formatMbps(v * 1_000_000)
        if (metricType === "dropRate") return v.toFixed(2)
        return v.toFixed(1)
    }

    return (
        <div className="space-y-4">
            <SegmentedControl
                value={timeRange}
                options={timeRangeOptions}
                onChange={onTimeRangeChange}
                size="lg"
            />

            {/* Stats row */}
            <div className="flex justify-between gap-3 rounded-xl bg-muted/20 px-4 py-3">
                {(["min", "avg", "max"] as const).map(stat => (
                    <div key={stat} className="flex flex-col items-center gap-0.5">
                        <span className={T.eyebrow}>{stat}</span>
                        <span className="text-sm font-bold tabular-nums">
                            {formatValue(stats[stat])} <span className="font-medium text-muted-foreground">{config.unit}</span>
                        </span>
                    </div>
                ))}
            </div>

            <div className="flex justify-end">
                <span className={T.meta}>{config.unit}</span>
            </div>

            {/* Chart */}
            <div className="h-[280px] w-full">
                <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={chartData} margin={{ top: 10, right: 4, left: 0, bottom: 5 }}>
                        <defs>
                            <linearGradient id="sl-detail-grad" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={config.color} stopOpacity={0.4} />
                                <stop offset="50%" stopColor={config.color} stopOpacity={0.15} />
                                <stop offset="100%" stopColor={config.color} stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                        <XAxis
                            dataKey="time"
                            axisLine={false}
                            tickLine={false}
                            tick={{ fontSize: 11, fill: c.axis }}
                            interval="preserveStartEnd"
                            minTickGap={56}
                        />
                        <YAxis tickFormatter={(v) => `${v.toFixed(0)}`} width={40} tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" />
                        {config.refLine && (
                            <ReferenceLine y={config.refLine.y} stroke={c.danger} strokeDasharray="6 4" strokeOpacity={0.5} label={{ value: config.refLine.label, position: "left", fill: c.danger, fontSize: 11 }} />
                        )}
                        <Tooltip
                            contentStyle={{
                                backgroundColor: "var(--popover)",
                                color: "var(--popover-foreground)",
                                border: "1px solid var(--border)",
                                borderRadius: "12px",
                                padding: "8px 12px",
                                fontSize: 12,
                            }}
                            labelStyle={{ color: c.label, fontSize: 11 }}
                            formatter={(value: any) => [`${formatValue(value ?? 0)} ${config.unit}`, config.label]}
                        />
                        <Area type="monotone" dataKey="value" stroke={config.color} strokeWidth={2} fill="url(#sl-detail-grad)" dot={false} activeDot={{ r: 4, fill: config.color, stroke: config.color, strokeWidth: 2 }} isAnimationActive={false} />
                    </AreaChart>
                </ResponsiveContainer>
            </div>
        </div>
    )
}
