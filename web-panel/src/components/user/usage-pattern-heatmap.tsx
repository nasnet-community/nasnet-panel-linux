import { useMemo } from "react"
import { useUserUsagePattern } from "@/lib/queries/use-analytics"
import { Skeleton } from "@/components/ui/skeleton"

const HOUR_LABELS = [
    "12a", "1a", "2a", "3a", "4a", "5a", "6a", "7a",
    "8a", "9a", "10a", "11a", "12p", "1p", "2p", "3p",
    "4p", "5p", "6p", "7p", "8p", "9p", "10p", "11p",
]

function formatNumber(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + "M"
    if (n >= 1000) return (n / 1000).toFixed(1) + "K"
    return String(n)
}

interface UsagePatternHeatmapProps {
    userId: number
}

export function UsagePatternHeatmap({ userId }: UsagePatternHeatmapProps) {
    const { data, isLoading } = useUserUsagePattern(userId, 30)

    const maxCount = useMemo(() => {
        if (!data?.length) return 1
        return Math.max(...data.map((p) => p.count), 1)
    }, [data])

    const peakHour = useMemo(() => {
        if (!data?.length) return null
        let max = data[0]
        for (const p of data) {
            if (p.count > max.count) max = p
        }
        return max.count > 0 ? max : null
    }, [data])

    const totalConnections = useMemo(() => {
        if (!data?.length) return 0
        return data.reduce((sum, p) => sum + p.count, 0)
    }, [data])

    function getIntensity(count: number): string {
        if (count === 0) return "bg-muted/30"
        const ratio = count / maxCount
        if (ratio < 0.2) return "bg-emerald-500/20"
        if (ratio < 0.4) return "bg-emerald-500/40"
        if (ratio < 0.6) return "bg-emerald-500/60"
        if (ratio < 0.8) return "bg-emerald-500/80"
        return "bg-emerald-500"
    }

    if (isLoading) {
        return (
            <div className="space-y-3">
                <Skeleton className="h-6 w-40" />
                <Skeleton className="h-32" />
            </div>
        )
    }

    return (
        <div className="space-y-3">
            <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                    <h4 className="text-sm font-semibold">Usage Pattern</h4>
                    <p className="text-xs text-muted-foreground">
                        {totalConnections > 0
                            ? `${formatNumber(totalConnections)} connections, peak at ${peakHour ? HOUR_LABELS[peakHour.hour] : "—"}`
                            : "No activity data available"}
                    </p>
                </div>
                <span className="text-[10px] text-muted-foreground">Last 30 days</span>
            </div>

            {/* Heatmap grid: 24 hours */}
            <div className="space-y-1.5">
                <div className="grid gap-1" style={{ gridTemplateColumns: "repeat(24, minmax(0, 1fr))" }}>
                    {(data ?? []).map((point) => (
                        <div
                            key={point.hour}
                            className={`aspect-square rounded-sm ${getIntensity(point.count)} transition-colors cursor-default`}
                            title={`${HOUR_LABELS[point.hour]}: ${formatNumber(point.count)} connections`}
                        />
                    ))}
                </div>
                <div className="flex justify-between text-[9px] text-muted-foreground px-0.5">
                    <span>12am</span>
                    <span>6am</span>
                    <span>12pm</span>
                    <span>6pm</span>
                    <span>11pm</span>
                </div>
            </div>

            {/* Legend */}
            <div className="flex items-center justify-end gap-1.5 text-[9px] text-muted-foreground">
                <span>Less</span>
                <div className="flex gap-0.5">
                    <div className="w-3 h-3 rounded-sm bg-muted/30" />
                    <div className="w-3 h-3 rounded-sm bg-emerald-500/20" />
                    <div className="w-3 h-3 rounded-sm bg-emerald-500/40" />
                    <div className="w-3 h-3 rounded-sm bg-emerald-500/60" />
                    <div className="w-3 h-3 rounded-sm bg-emerald-500/80" />
                    <div className="w-3 h-3 rounded-sm bg-emerald-500" />
                </div>
                <span>More</span>
            </div>
        </div>
    )
}
