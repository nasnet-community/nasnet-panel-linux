import { useMemo } from "react"
import type { ReactNode } from "react"
import { useSubUsagePattern } from "@/lib/queries/use-sub-panel-analytics"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Clock } from "lucide-react"
import { cn } from "@/lib/utils"
import { hourLabel, localHourBuckets } from "./plan-state"

function HoursFrame({ aside, children }: { aside?: string; children: ReactNode }) {
    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                        <Clock className="w-4 h-4 text-emerald-400" />
                        <h2 className="text-sm font-medium text-muted-foreground">Busiest hours</h2>
                    </div>
                    {aside && <span className="text-xs text-muted-foreground">{aside}</span>}
                </div>
                {children}
            </CardContent>
        </Card>
    )
}

/** 24 hour bars, peak hour picked out; shared by the card and the time dialog. */
export function HourBars({ hours, className }: { hours: number[]; className?: string }) {
    const max = Math.max(...hours)
    const total = hours.reduce((sum, n) => sum + n, 0)
    const peak = hours.indexOf(max)
    return (
        <div className={cn("flex items-end gap-[3px]", className)} role="img" aria-label={`Busiest around ${hourLabel(peak)}`}>
            {hours.map((count, hour) => (
                <div
                    key={hour}
                    className={cn("flex-1 rounded-t-[2px]", hour === peak ? "bg-emerald-500" : "bg-emerald-500/30")}
                    style={{ height: `${max > 0 ? (count / max) * 100 : 0}%`, minHeight: 2 }}
                    title={`${hourLabel(hour)}: ${total > 0 ? Math.round((count / total) * 100) : 0}% of your activity`}
                />
            ))}
        </div>
    )
}

interface UsageHeatmapProps {
    uuid: string
}

export function UsageHeatmap({ uuid }: UsageHeatmapProps) {
    const { data, isLoading, isError } = useSubUsagePattern(uuid)

    // The API buckets by UTC hour; show the visitor's own hours.
    const buckets = useMemo(() => localHourBuckets(data), [data])
    const total = buckets.reduce((sum, n) => sum + n, 0)
    const max = Math.max(...buckets)
    const peak = buckets.indexOf(max)

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
        return <HoursFrame><p className="text-xs text-muted-foreground">Activity data unavailable right now.</p></HoursFrame>
    }
    if (total === 0) {
        return <HoursFrame><p className="text-xs text-muted-foreground">No activity yet. Your busiest hours show up once you connect.</p></HoursFrame>
    }

    return (
        <HoursFrame aside={`around ${hourLabel(peak)}`}>
            <div className="space-y-1.5">
                <HourBars hours={buckets} className="h-10" />
                <div className="flex justify-between text-xs text-muted-foreground px-0.5">
                    <span>12 am</span>
                    <span>6 am</span>
                    <span>12 pm</span>
                    <span>6 pm</span>
                    <span>11 pm</span>
                </div>
            </div>
        </HoursFrame>
    )
}
