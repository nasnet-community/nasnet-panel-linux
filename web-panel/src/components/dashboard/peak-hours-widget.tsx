import { useMemo, useState } from "react"
import {
    Bar,
    BarChart,
    ResponsiveContainer,
    XAxis,
    YAxis,
    Tooltip,
    CartesianGrid,
} from "recharts"
import { ChartTooltip } from "@/components/ui/chart-tooltip"
import { WidgetWrapper } from "./widget-wrapper"
import { Skeleton } from "@/components/ui/skeleton"
import { Clock } from "lucide-react"
import { usePeakHours } from "@/lib/queries/use-analytics"
import { useChartPalette } from "@/lib/design/palette"

type Period = 7 | 14 | 30

const HOUR_LABELS = [
    "12am", "1am", "2am", "3am", "4am", "5am", "6am", "7am",
    "8am", "9am", "10am", "11am", "12pm", "1pm", "2pm", "3pm",
    "4pm", "5pm", "6pm", "7pm", "8pm", "9pm", "10pm", "11pm",
]

function formatNumber(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + "M"
    if (n >= 1000) return (n / 1000).toFixed(1) + "K"
    return String(n)
}

interface PeakHoursWidgetProps {
    isEditMode?: boolean
}

export function PeakHoursWidget({ isEditMode }: PeakHoursWidgetProps) {
    const [period, setPeriod] = useState<Period>(7)
    const { data, isLoading } = usePeakHours(period)
    const c = useChartPalette()

    const chartData = useMemo(() => {
        if (!data?.length) return []
        return data.map((p) => ({
            hour: HOUR_LABELS[p.hour] || String(p.hour),
            accepted: p.connections,
            rejected: p.rejected,
            uniqueUsers: p.unique_users,
            tcp: p.tcp_count,
            udp: p.udp_count,
        }))
    }, [data])

    const peakHour = useMemo(() => {
        if (!data?.length) return null
        let max = data[0]
        for (const p of data) {
            if (p.connections > max.connections) max = p
        }
        return max
    }, [data])

    const totalConnections = useMemo(() => {
        if (!data?.length) return 0
        return data.reduce((sum, p) => sum + p.connections + p.rejected, 0)
    }, [data])

    return (
        <WidgetWrapper
            title="Peak Hours"
            icon={<Clock className="w-4 h-4 text-emerald-500" />}
            isEditMode={isEditMode}
            headerRight={
                <div className="flex items-center gap-1">
                    {([7, 14, 30] as Period[]).map((p) => (
                        <button
                            key={p}
                            onClick={() => setPeriod(p)}
                            className={`px-2 py-0.5 text-[10px] font-medium rounded transition-colors ${
                                period === p
                                    ? "bg-primary text-primary-foreground"
                                    : "text-muted-foreground hover:text-foreground hover:bg-muted"
                            }`}
                        >
                            {p}d
                        </button>
                    ))}
                </div>
            }
        >
            {isLoading ? (
                <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-3">
                        <Skeleton className="h-14" />
                        <Skeleton className="h-14" />
                    </div>
                    <Skeleton className="h-full min-h-[160px]" />
                </div>
            ) : (
                <div className="flex flex-col gap-3 h-full">
                    {/* Summary */}
                    <div className="grid grid-cols-2 gap-3">
                        <div className="rounded-lg border border-border/50 bg-muted/20 px-3 py-2">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Total Connections</p>
                            <p className="text-base font-bold mt-0.5">{formatNumber(totalConnections)}</p>
                        </div>
                        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/5 px-3 py-2">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Peak Hour</p>
                            <p className="text-base font-bold mt-0.5 text-emerald-500">
                                {peakHour ? HOUR_LABELS[peakHour.hour] : "\u2014"}
                            </p>
                        </div>
                    </div>

                    {/* Bar Chart */}
                    {chartData.length === 0 ? (
                        <div className="flex items-center justify-center flex-1 min-h-[120px] text-sm text-muted-foreground">
                            No connection data available
                        </div>
                    ) : (
                        <div className="flex-1 min-h-[140px] -ml-2 [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground">
                            <ResponsiveContainer width="100%" height="100%">
                                <BarChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
                                    <CartesianGrid
                                        vertical={false}
                                        stroke={c.grid}
                                        strokeOpacity={0.5}
                                        strokeDasharray="3 3"
                                    />
                                    <XAxis
                                        dataKey="hour"
                                        axisLine={false}
                                        tickLine={false}
                                        tick={{ fontSize: 9 }}
                                        interval={2}
                                        dy={8}
                                    />
                                    <YAxis
                                        axisLine={false}
                                        tickLine={false}
                                        tick={{ fontSize: 10 }}
                                        tickFormatter={formatNumber}
                                        width={42}
                                    />
                                    <Tooltip
                                        content={
                                            <ChartTooltip
                                                labelFormatter={(label) => `${label}`}
                                                valueFormatter={(v) => formatNumber(v)}
                                            />
                                        }
                                    />
                                    <Bar
                                        dataKey="accepted"
                                        name="Accepted"
                                        fill={c.success}
                                        radius={[2, 2, 0, 0]}
                                        stackId="connections"
                                    />
                                    <Bar
                                        dataKey="rejected"
                                        name="Rejected"
                                        fill={c.danger}
                                        radius={[2, 2, 0, 0]}
                                        stackId="connections"
                                    />
                                </BarChart>
                            </ResponsiveContainer>
                        </div>
                    )}

                    {/* Legend */}
                    <div className="flex items-center justify-center gap-4 text-[10px] text-muted-foreground">
                        <div className="flex items-center gap-1.5">
                            <div className="w-2 h-2 rounded-sm bg-emerald-500" />
                            <span>Accepted</span>
                        </div>
                        <div className="flex items-center gap-1.5">
                            <div className="w-2 h-2 rounded-sm bg-red-500" />
                            <span>Rejected</span>
                        </div>
                    </div>
                </div>
            )}
        </WidgetWrapper>
    )
}
