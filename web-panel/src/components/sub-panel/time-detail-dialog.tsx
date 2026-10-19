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
import {
    compactRemaining,
    DAY_MS,
    hasEndDate,
    hasEnded,
    hourLabel,
    isCancelled,
    localHourBuckets,
    timeLeftPercent,
    timeTone,
    wholeDaysLeft,
    type Tone,
} from "./plan-state"
import { HourBars } from "./usage-heatmap"

interface TimeDetailDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    data: SubPanelData
    uuid: string
}

function formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
    })
}

const barColorMap: Record<Tone, string> = {
    emerald: "bg-emerald-500",
    amber: "bg-amber-500",
    red: "bg-red-500",
    zinc: "bg-zinc-500",
}

function PeakActivityChart({ data }: { data: SubPanelHourlyUsagePoint[] }) {
    // The API buckets by UTC hour; show the visitor's own hours.
    const hours = useMemo(() => localHourBuckets(data), [data])
    const maxCount = Math.max(...hours)

    if (maxCount === 0) return null

    return (
        <div className="space-y-2">
            <div className="flex items-center gap-1.5">
                <Clock className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-xs text-muted-foreground font-medium">Busiest hours</span>
            </div>

            <HourBars hours={hours} className="h-12" />

            <div className="flex justify-between">
                <span className="text-xs text-muted-foreground">12 am</span>
                <span className="text-xs text-muted-foreground">11 pm</span>
            </div>

            <p className="text-xs text-muted-foreground">
                Busiest around <span className="font-semibold text-foreground">{hourLabel(hours.indexOf(maxCount))}</span>
            </p>
        </div>
    )
}

function Metric({ label, value, wide }: { label: string; value: string; wide?: boolean }) {
    return (
        <div className={wide ? "col-span-2 space-y-0.5" : "space-y-0.5"}>
            <p className="text-xs text-muted-foreground font-medium">{label}</p>
            <p className="text-sm font-semibold">{value}</p>
        </div>
    )
}

export function TimeDetailDialog({ open, onOpenChange, data, uuid }: TimeDetailDialogProps) {
    const { data: usagePattern, isLoading: patternLoading } = useSubUsagePattern(uuid)

    const hasEnd = hasEndDate(data)
    const ended = hasEnded(data)
    const displayDays = hasEnd ? wholeDaysLeft(data) : 0
    const tone = timeTone(data)
    const compactLeft = compactRemaining(data)
    const cancelled = isCancelled(data)

    const daysElapsed = useMemo(() => {
        if (!data.start_date) return 0
        return Math.max(0, Math.floor((Date.now() - new Date(data.start_date).getTime()) / DAY_MS))
    }, [data.start_date])

    // The whole span, start to end. Custom end dates make it differ from the
    // plan's own length, which is what the old "Duration" showed.
    const spanDays = data.start_date && data.end_date
        ? Math.round((new Date(data.end_date).getTime() - new Date(data.start_date).getTime()) / DAY_MS)
        : null
    const elapsedPercent = spanDays ? Math.min(100, (daysElapsed / spanDays) * 100) : 0
    const createdDiffers = !!data.created_at && (!data.start_date
        || Math.abs(new Date(data.created_at).getTime() - new Date(data.start_date).getTime()) > DAY_MS)

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Clock className="w-4 h-4 text-muted-foreground" />
                        Time
                    </DialogTitle>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="flex flex-col items-center gap-2">
                        {hasEnd ? (
                            <CircularProgress
                                value={ended ? 0 : timeLeftPercent(data)}
                                size={120}
                                strokeWidth={8}
                                color={tone}
                            >
                                {ended ? (
                                    <span className="text-xl font-bold tracking-tight leading-none">{cancelled ? "Cancelled" : "Ended"}</span>
                                ) : (
                                    <>
                                        <span className="text-2xl font-bold tracking-tight leading-none">
                                            {displayDays > 0 ? displayDays : compactLeft}
                                        </span>
                                        <span className="text-xs font-medium text-muted-foreground mt-0.5">
                                            {displayDays === 1 ? "day left" : displayDays > 1 ? "days left" : "left"}
                                        </span>
                                    </>
                                )}
                            </CircularProgress>
                        ) : (
                            <div className="flex flex-col items-center justify-center" style={{ width: 120, height: 120 }}>
                                <span className="text-5xl font-bold text-emerald-500 leading-none">&infin;</span>
                            </div>
                        )}
                        <p className="text-xs text-muted-foreground">
                            {!hasEnd ? "No end date" : cancelled ? "Cancelled" : ended ? `Ended ${data.end_date ? formatDate(data.end_date) : ""}` : `${compactLeft} left`}
                        </p>
                    </div>

                    {/* Timeline: how far through the whole span today is. */}
                    {hasEnd && data.start_date && data.end_date && (
                        <div className="space-y-1.5">
                            <div className="relative h-2 rounded-full bg-muted/30 overflow-hidden">
                                <div
                                    className={`absolute inset-y-0 left-0 rounded-full ${barColorMap[tone]}`}
                                    style={{ width: `${elapsedPercent}%` }}
                                />
                                <div
                                    className="absolute top-1/2 -translate-y-1/2 w-2.5 h-2.5 rounded-full border-2 border-background bg-foreground"
                                    style={{
                                        left: `${elapsedPercent}%`,
                                        transform: "translate(-50%, -50%)",
                                    }}
                                />
                            </div>
                            <div className="flex justify-between">
                                <span className="text-xs text-muted-foreground">{formatDate(data.start_date)}</span>
                                <span className="text-xs text-muted-foreground">{formatDate(data.end_date)}</span>
                            </div>
                        </div>
                    )}

                    <div className="grid grid-cols-2 gap-3">
                        <Metric label="Elapsed" value={data.start_date ? `${daysElapsed} days` : "—"} />
                        <Metric label="Left" value={!hasEnd ? "No end date" : ended ? "None" : compactLeft} />
                        <Metric label="Plan" value={data.plan_name || "—"} />
                        <Metric
                            label="Length"
                            value={spanDays !== null ? `${spanDays} days` : data.plan_duration ? `${data.plan_duration} days` : "—"}
                        />
                        {createdDiffers && <Metric label="Created" value={formatDate(data.created_at)} wide />}
                    </div>

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
