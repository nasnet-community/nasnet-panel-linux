import { useState, useMemo } from "react"
import { Bar, BarChart, ResponsiveContainer, XAxis, YAxis, Tooltip } from "recharts"
import { useUserUsageHistory } from "@/lib/queries"
import { formatBytes, formatDate, cn } from "@/lib/utils"
import { ChartTooltip } from "@/components/ui/chart-tooltip"
import { Skeleton } from "@/components/ui/skeleton"
import { useChartPalette } from "@/lib/design/palette"

interface UsageChartProps {
    userId: number
}

const RANGES = [
    { label: "7d", days: 7 },
    { label: "30d", days: 30 },
    { label: "90d", days: 90 },
    { label: "All", days: 365 },
] as const

// Safe formatter that handles fractional/negative values Recharts may generate for ticks
function safeFormatBytes(value: number): string {
    if (!value || value < 1) return "0 B"
    return formatBytes(Math.round(value))
}

export function UsageChart({ userId }: UsageChartProps) {
    const c = useChartPalette()
    const [selectedRange, setSelectedRange] = useState(30)
    const [mode, setMode] = useState<"combined" | "upload" | "download">("combined")
    const { data: usageHistory, isLoading } = useUserUsageHistory(userId, selectedRange)

    const chartData = useMemo(() => {
        if (!usageHistory?.length) return []
        return usageHistory.map(d => ({
            date: d.date,
            data_used: d.data_used,
            data_upload: d.data_upload,
            data_download: d.data_download,
        }))
    }, [usageHistory])

    const hasUploadDownload = chartData.length > 0 && chartData[0].data_upload !== undefined

    const totalUsage = useMemo(() => {
        if (!chartData.length) return 0
        return chartData.reduce((sum, d) => sum + d.data_used, 0)
    }, [chartData])

    const maxValue = useMemo(() => {
        if (!chartData.length) return 1024 * 1024
        const key = mode === "upload" ? "data_upload" : mode === "download" ? "data_download" : "data_used"
        return Math.max(...chartData.map(d => (d as any)[key] ?? d.data_used), 1)
    }, [chartData, mode])

    // Consider data trivial if total usage across all days < 1 KB
    const isTrivialData = totalUsage < 1024

    return (
        <div className="space-y-3">
            <div className="flex justify-end">
                <div className="flex rounded-md border overflow-hidden">
                    {RANGES.map(r => (
                        <button
                            key={r.days}
                            onClick={() => setSelectedRange(r.days)}
                            className={cn(
                                "px-2.5 py-1 text-[11px] font-medium transition-colors",
                                selectedRange === r.days
                                    ? "bg-primary text-primary-foreground"
                                    : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
                            )}
                        >
                            {r.label}
                        </button>
                    ))}
                </div>
            </div>

            {isLoading ? (
                <Skeleton className="h-[200px] w-full" />
            ) : chartData.length === 0 || isTrivialData ? (
                <div className="flex items-center justify-center h-[200px] text-xs text-muted-foreground/60 border rounded-md">
                    No usage data yet
                </div>
            ) : (
                <>
                <div className="h-[200px] -ml-2 [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground">
                    <ResponsiveContainer width="100%" height="100%">
                        <BarChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
                            <XAxis
                                dataKey="date"
                                axisLine={false}
                                tickLine={false}
                                tick={{ fontSize: 10 }}
                                tickFormatter={(value: string) => formatDate(value)}
                                interval={Math.max(Math.floor(chartData.length / 7) - 1, 0)}
                            />
                            <YAxis
                                axisLine={false}
                                tickLine={false}
                                tick={{ fontSize: 10 }}
                                tickFormatter={safeFormatBytes}
                                width={52}
                                domain={[0, Math.ceil(maxValue * 1.15)]}
                            />
                            <Tooltip
                                content={
                                    <ChartTooltip
                                        labelFormatter={formatDate}
                                        valueFormatter={safeFormatBytes}
                                    />
                                }
                                cursor={{ fill: "var(--muted)", opacity: 0.5 }}
                            />
                            <Bar
                                dataKey={mode === "upload" ? "data_upload" : mode === "download" ? "data_download" : "data_used"}
                                fill={mode === "upload" ? c.info : mode === "download" ? c.success : "var(--primary)"}
                                radius={[3, 3, 0, 0]}
                                maxBarSize={32}
                            />
                        </BarChart>
                    </ResponsiveContainer>
                </div>
                <div className="flex items-center justify-center gap-4 mt-3">
                    {[
                        { key: "combined" as const, label: "Combined", color: "bg-primary" },
                        { key: "upload" as const, label: "Upload", color: "bg-blue-500" },
                        { key: "download" as const, label: "Download", color: "bg-emerald-500" },
                    ].map((s) => (
                        <button
                            key={s.key}
                            onClick={() => hasUploadDownload && setMode(s.key)}
                            disabled={!hasUploadDownload && s.key !== "combined"}
                            className={cn(
                                "flex items-center gap-1.5 text-[10px] transition-colors",
                                mode === s.key ? "text-foreground font-semibold" : "text-muted-foreground",
                                !hasUploadDownload && s.key !== "combined" && "opacity-40 cursor-not-allowed"
                            )}
                            title={!hasUploadDownload && s.key !== "combined" ? "Upload/download split requires a backend update" : undefined}
                        >
                            <span className={cn("w-2 h-2 rounded-sm", s.color)} />
                            {s.label}
                        </button>
                    ))}
                </div>
                </>
            )}
        </div>
    )
}
