import { useMemo } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { CircularProgress } from "@/components/ui/circular-progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Clock } from "lucide-react"
import { useSubUsagePattern } from "@/lib/queries/use-sub-panel-analytics"
import type { SubPanelData, SubPanelHourlyUsagePoint } from "@/lib/types/sub-panel"

interface TimeDetailDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    data: SubPanelData
    uuid: string
}

function getColor(percent: number): "emerald" | "amber" | "red" {
    if (percent > 90) return "red"
    if (percent > 70) return "amber"
    return "emerald"
}

function parseDaysFromRemaining(timeRemaining: string): number {
    const match = timeRemaining.match(/^(\d+)\s*days?/)
    return match ? parseInt(match[1], 10) : 0
}

function formatCompactRemaining(timeRemaining: string): string {
    const days = timeRemaining.match(/(\d+)\s*days?/)
    const hours = timeRemaining.match(/(\d+)\s*hours?/)
    const mins = timeRemaining.match(/(\d+)\s*min/)
    const parts: string[] = []
    if (days) parts.push(`${days[1]}d`)
    if (hours) parts.push(`${hours[1]}h`)
    if (!days && mins) parts.push(`${mins[1]}m`)
    return parts.join(" ") || timeRemaining
}

function formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
    })
}

function formatHourLabel(hour: number): string {
    if (hour === 0) return "12a"
    if (hour < 12) return `${hour}a`
    if (hour === 12) return "12p"
    return `${hour - 12}p`
}

const barColorMap = {
    emerald: "bg-emerald-500",
    amber: "bg-amber-500",
    red: "bg-red-500",
}

function PeakActivityChart({ data }: { data: SubPanelHourlyUsagePoint[] }) {
    const { maxCount, peakHour } = useMemo(() => {
        let max = 0
        let peak = 0
        for (const point of data) {
            if (point.count > max) {
                max = point.count
                peak = point.hour
            }
        }
        return { maxCount: max, peakHour: peak }
    }, [data])

    if (maxCount === 0) return null

    // Build a full 24-hour array, filling missing hours with 0
    const hourMap = new Map(data.map((p) => [p.hour, p.count]))
    const hours = Array.from({ length: 24 }, (_, i) => ({
        hour: i,
        count: hourMap.get(i) ?? 0,
    }))

    return (
        <div className="space-y-2">
            <div className="flex items-center gap-1.5">
                <Clock className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                    Peak Activity
                </span>
            </div>

            <div className="flex items-end gap-px h-12">
                {hours.map(({ hour, count }) => {
                    const heightPercent = maxCount > 0 ? (count / maxCount) * 100 : 0
                    return (
                        <div
                            key={hour}
                            className="flex-1 flex items-end"
                        >
                            <div
                                className={`w-full rounded-t-sm transition-all ${
                                    hour === peakHour
                                        ? "bg-emerald-400"
                                        : "bg-emerald-500/40"
                                }`}
                                style={{
                                    height: `${Math.max(heightPercent, 2)}%`,
                                    minHeight: count > 0 ? 2 : 1,
                                }}
                            />
                        </div>
                    )
                })}
            </div>

            <div className="flex justify-between">
                <span className="text-xs text-muted-foreground">12a</span>
                <span className="text-xs text-muted-foreground">11p</span>
            </div>

            <p className="text-xs text-muted-foreground">
                Peak: <span className="font-semibold text-foreground">{formatHourLabel(peakHour)}</span>
            </p>
        </div>
    )
}

export function TimeDetailDialog({ open, onOpenChange, data, uuid }: TimeDetailDialogProps) {
    const { data: usagePattern, isLoading: patternLoading } = useSubUsagePattern(uuid)

    const hasEndDate = data.days_remaining >= 0
    const displayDays = hasEndDate ? parseDaysFromRemaining(data.time_remaining) : 0
    const color = getColor(data.time_used_percent)
    const compactRemaining = formatCompactRemaining(data.time_remaining)

    const daysElapsed = useMemo(() => {
        if (!data.start_date) return 0
        const msElapsed = Date.now() - new Date(data.start_date).getTime()
        return Math.max(0, Math.floor(msElapsed / (1000 * 60 * 60 * 24)))
    }, [data.start_date])

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Clock className="w-4 h-4 text-muted-foreground" />
                        Time Details
                    </DialogTitle>
                </DialogHeader>

                <div className="space-y-4">
                    {/* Hero section */}
                    <div className="flex flex-col items-center gap-2">
                        {hasEndDate ? (
                            <CircularProgress
                                value={data.time_used_percent}
                                size={120}
                                strokeWidth={8}
                                color={color}
                                showValue={false}
                            >
                                <span className="text-2xl font-bold tracking-tight leading-none">
                                    {displayDays}
                                </span>
                                <span className="text-xs uppercase font-medium text-muted-foreground tracking-wider mt-0.5">
                                    days
                                </span>
                            </CircularProgress>
                        ) : (
                            <div className="flex flex-col items-center justify-center" style={{ width: 120, height: 120 }}>
                                <span className="text-5xl font-bold text-emerald-500 leading-none">&infin;</span>
                            </div>
                        )}
                        <p className="text-xs text-muted-foreground">
                            {hasEndDate ? `${compactRemaining} remaining` : "Unlimited time"}
                        </p>
                    </div>

                    {/* Timeline bar */}
                    {hasEndDate && data.start_date && data.end_date && (
                        <div className="space-y-1.5">
                            <div className="relative h-2 rounded-full bg-muted/30 overflow-hidden">
                                <div
                                    className={`absolute inset-y-0 left-0 rounded-full ${barColorMap[color]}`}
                                    style={{ width: `${Math.min(data.time_used_percent, 100)}%` }}
                                />
                                {/* Current position marker */}
                                <div
                                    className="absolute top-1/2 -translate-y-1/2 w-2.5 h-2.5 rounded-full border-2 border-background bg-foreground"
                                    style={{
                                        left: `${Math.min(data.time_used_percent, 100)}%`,
                                        transform: "translate(-50%, -50%)",
                                    }}
                                />
                            </div>
                            <div className="flex justify-between">
                                <span className="text-xs text-muted-foreground">
                                    {formatDate(data.start_date)}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    {formatDate(data.end_date)}
                                </span>
                            </div>
                        </div>
                    )}

                    {/* Metrics grid */}
                    <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Elapsed
                            </p>
                            <p className="text-sm font-semibold">
                                {data.start_date ? `${daysElapsed} days` : "--"}
                            </p>
                        </div>
                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Remaining
                            </p>
                            <p className="text-sm font-semibold">
                                {hasEndDate ? compactRemaining : "Unlimited"}
                            </p>
                        </div>
                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Plan
                            </p>
                            <p className="text-sm font-semibold">{data.plan_name}</p>
                        </div>
                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Duration
                            </p>
                            <p className="text-sm font-semibold">{data.plan_duration} days</p>
                        </div>
                        {data.created_at && (
                            <div className="col-span-2 space-y-0.5">
                                <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                    Created
                                </p>
                                <p className="text-sm font-semibold">
                                    {formatDate(data.created_at)}
                                </p>
                            </div>
                        )}
                    </div>

                    {/* Peak Activity Hours */}
                    {patternLoading ? (
                        <div className="space-y-2">
                            <Skeleton className="h-3.5 w-24" />
                            <Skeleton className="h-12 w-full" />
                            <Skeleton className="h-3 w-16" />
                        </div>
                    ) : usagePattern && usagePattern.length > 0 ? (
                        <PeakActivityChart data={usagePattern} />
                    ) : null}
                </div>
            </DialogContent>
        </Dialog>
    )
}
