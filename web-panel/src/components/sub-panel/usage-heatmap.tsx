import { useMemo } from "react"
import { useSubUsagePattern } from "@/lib/queries/use-sub-panel-analytics"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Clock } from "lucide-react"

const HOUR_LABELS = [
    "12a", "1a", "2a", "3a", "4a", "5a", "6a", "7a",
    "8a", "9a", "10a", "11a", "12p", "1p", "2p", "3p",
    "4p", "5p", "6p", "7p", "8p", "9p", "10p", "11p",
]

/** Dense 24-element array (index = hour 0..23) of connection counts,
 *  backfilling hours the server omitted with 0. Ignores out-of-range hours. */
function buildHourBuckets(points?: { hour: number; count: number }[]): number[] {
    const buckets = new Array(24).fill(0)
    for (const p of points ?? []) {
        if (p.hour >= 0 && p.hour < 24) buckets[p.hour] = p.count
    }
    return buckets
}

function formatNumber(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + "M"
    if (n >= 1000) return (n / 1000).toFixed(1) + "K"
    return String(n)
}

interface UsageHeatmapProps {
    uuid: string
}

export function UsageHeatmap({ uuid }: UsageHeatmapProps) {
    const { data, isLoading, isError } = useSubUsagePattern(uuid)

    const buckets = useMemo(() => buildHourBuckets(data), [data])
    const maxCount = useMemo(() => Math.max(...buckets, 1), [buckets])

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
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
                <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                    <Skeleton className="h-4 w-36" />
                    <Skeleton className="h-10" />
                </CardContent>
            </Card>
        )
    }

    if (isError) {
        return (
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
                <CardContent className="p-3.5 sm:p-4 md:p-5">
                    <div className="flex items-center gap-2 mb-2">
                        <Clock className="w-4 h-4 text-emerald-400" />
                        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Activity Pattern</h2>
                    </div>
                    <p className="text-xs text-muted-foreground">Activity data unavailable right now.</p>
                </CardContent>
            </Card>
        )
    }
    if (totalConnections === 0) {
        return (
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
                <CardContent className="p-3.5 sm:p-4 md:p-5">
                    <div className="flex items-center gap-2 mb-2">
                        <Clock className="w-4 h-4 text-emerald-400" />
                        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Activity Pattern</h2>
                    </div>
                    <p className="text-xs text-muted-foreground">No activity recorded yet — your hourly usage pattern will appear here once you connect.</p>
                </CardContent>
            </Card>
        )
    }

    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                {/* Stacked on phones — at 12px the summary no longer fits beside the
                    title, and wrapping the title mid-phrase reads worse. */}
                <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-center gap-2">
                        <Clock className="w-4 h-4 text-emerald-400" />
                        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider whitespace-nowrap">
                            Activity Pattern
                        </h2>
                    </div>
                    <p className="text-xs text-muted-foreground">
                        {formatNumber(totalConnections)} connections{peakHour && HOUR_LABELS[peakHour.hour] ? `, peak at ${HOUR_LABELS[peakHour.hour]}` : ""}
                    </p>
                </div>

                {/* Heatmap grid: 24 hours */}
                <div className="space-y-1.5">
                    <div className="grid gap-1" style={{ gridTemplateColumns: "repeat(24, minmax(0, 1fr))" }}>
                        {buckets.map((count, hour) => (
                            <div
                                key={hour}
                                className={`aspect-square rounded-sm ${getIntensity(count)} transition-colors cursor-default`}
                                title={`${HOUR_LABELS[hour]}: ${formatNumber(count)} connections`}
                            />
                        ))}
                    </div>
                    <div className="flex justify-between text-xs text-muted-foreground px-0.5">
                        <span>12am</span>
                        <span>6am</span>
                        <span>12pm</span>
                        <span>6pm</span>
                        <span>11pm</span>
                    </div>
                </div>

                {/* Legend */}
                <div className="flex items-center justify-end gap-1.5 text-xs text-muted-foreground">
                    <span>Less</span>
                    <div className="flex gap-0.5">
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-muted/30" />
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-emerald-500/20" />
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-emerald-500/40" />
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-emerald-500/60" />
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-emerald-500/80" />
                        <div className="w-2.5 h-2.5 sm:w-3 sm:h-3 rounded-sm bg-emerald-500" />
                    </div>
                    <span>More</span>
                </div>
            </CardContent>
        </Card>
    )
}
