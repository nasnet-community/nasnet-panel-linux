import { useState, useMemo } from "react"
import { Bar, BarChart, ResponsiveContainer, YAxis, Tooltip, XAxis, CartesianGrid } from "recharts"
import { formatBytes } from "@/lib/utils"
import { Loader2 } from "lucide-react"
import { useNodeDailyTraffic } from "@/lib/queries/use-nodes"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"

interface TrafficSummaryChartProps {
    nodeId: number
    enabled?: boolean
}

export function TrafficSummaryChart({ nodeId, enabled = true }: TrafficSummaryChartProps) {
    const c = useChartPalette()
    const [period, setPeriod] = useState<7 | 30>(7)
    const { data: rawData, isLoading } = useNodeDailyTraffic(nodeId, period, enabled)

    const chartData = useMemo(() => {
        if (!rawData) return []
        return rawData.map((d) => ({
            date: new Date(d.date).toLocaleDateString("en-US", { month: "short", day: "numeric" }),
            uplink: d.uplink,
            downlink: d.downlink,
            total: d.uplink + d.downlink,
        }))
    }, [rawData])

    const totals = useMemo(() => {
        if (!rawData || rawData.length === 0) return { uplink: 0, downlink: 0, total: 0 }
        const up = rawData.reduce((s, d) => s + d.uplink, 0)
        const down = rawData.reduce((s, d) => s + d.downlink, 0)
        return { uplink: up, downlink: down, total: up + down }
    }, [rawData])

    const formatTrafficAxis = (value: number) => {
        if (value === 0) return "0"
        if (value >= 1_099_511_627_776) return `${(value / 1_099_511_627_776).toFixed(1)}TB`
        if (value >= 1_073_741_824) return `${(value / 1_073_741_824).toFixed(1)}GB`
        if (value >= 1_048_576) return `${(value / 1_048_576).toFixed(0)}MB`
        return `${(value / 1024).toFixed(0)}KB`
    }

    return (
        <div className="flex flex-col h-full">
            {/* Header with period toggle and summary */}
            <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-3">
                    <div className="flex items-center gap-1 bg-muted/30 rounded-lg p-0.5">
                        {([7, 30] as const).map((p) => (
                            <button
                                key={p}
                                onClick={() => setPeriod(p)}
                                className={`px-2.5 py-1 rounded-md text-[11px] font-bold transition-all ${
                                    period === p
                                        ? "bg-foreground text-background shadow-sm"
                                        : "text-muted-foreground hover:text-foreground"
                                }`}
                            >
                                {p}D
                            </button>
                        ))}
                    </div>
                    {isLoading && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground/50" />}
                </div>
                <div className="flex items-center gap-4 text-[11px]">
                    <div className="flex items-center gap-1.5">
                        <div className="w-2 h-2 rounded-full bg-blue-500" />
                        <span className="text-muted-foreground font-bold uppercase tracking-wider">Up</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                        <div className="w-2 h-2 rounded-full bg-emerald-500" />
                        <span className="text-muted-foreground font-bold uppercase tracking-wider">Down</span>
                    </div>
                </div>
            </div>

            {/* Summary stats */}
            <div className="grid grid-cols-3 gap-3 mb-4">
                <div className="flex flex-col">
                    <span className="text-[10px] uppercase font-bold text-muted-foreground/60 tracking-wider">Upload</span>
                    <span className="text-sm font-bold font-mono text-blue-400">{formatBytes(totals.uplink)}</span>
                </div>
                <div className="flex flex-col">
                    <span className="text-[10px] uppercase font-bold text-muted-foreground/60 tracking-wider">Download</span>
                    <span className="text-sm font-bold font-mono text-emerald-400">{formatBytes(totals.downlink)}</span>
                </div>
                <div className="flex flex-col">
                    <span className="text-[10px] uppercase font-bold text-muted-foreground/60 tracking-wider">Total</span>
                    <span className="text-sm font-bold font-mono">{formatBytes(totals.total)}</span>
                </div>
            </div>

            {/* Chart */}
            <div className="flex-1 min-h-0">
                {chartData.length === 0 && !isLoading ? (
                    <div className="flex items-center justify-center h-full text-muted-foreground/50 text-sm">
                        No traffic data yet
                    </div>
                ) : (
                    <ResponsiveContainer width="100%" height="100%">
                        <BarChart data={chartData} margin={{ top: 5, right: 5, left: 0, bottom: 5 }}>
                            <defs>
                                <linearGradient id="bar-gradient-up" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.info} stopOpacity={0.9} />
                                    <stop offset="100%" stopColor={c.info} stopOpacity={0.5} />
                                </linearGradient>
                                <linearGradient id="bar-gradient-down" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor={c.success} stopOpacity={0.9} />
                                    <stop offset="100%" stopColor={c.success} stopOpacity={0.5} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid
                                vertical={false}
                                stroke={c.grid}
                                strokeDasharray="3 3"
                            />
                            <XAxis
                                dataKey="date"
                                axisLine={false}
                                tickLine={false}
                                tick={{ fontSize: 10, fill: c.axis }}
                                interval={period === 30 ? 4 : 0}
                            />
                            <YAxis
                                tickFormatter={formatTrafficAxis}
                                width={50}
                                tick={{ fontSize: 10, fill: c.axis }}
                                axisLine={false}
                                tickLine={false}
                            />
                            <Tooltip
                                contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }}
                                itemStyle={{ fontSize: "12px", fontWeight: 600, padding: "2px 0" }}
                                labelStyle={{ color: c.tooltipLabel, fontSize: "10px", marginBottom: "8px" }}
                                formatter={(value, name) => [formatBytes(Number(value)), String(name ?? "")]}
                                cursor={{ fill: "rgba(255,255,255,0.03)" }}
                            />
                            <Bar
                                dataKey="uplink"
                                stackId="traffic"
                                fill="url(#bar-gradient-up)"
                                radius={[0, 0, 0, 0]}
                                name="Upload"
                            />
                            <Bar
                                dataKey="downlink"
                                stackId="traffic"
                                fill="url(#bar-gradient-down)"
                                radius={[3, 3, 0, 0]}
                                name="Download"
                            />
                        </BarChart>
                    </ResponsiveContainer>
                )}
            </div>
        </div>
    )
}
