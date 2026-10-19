import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn, formatBytes, parseDateLocal } from "@/lib/utils"
import { AlertCircle, AlertTriangle, Activity } from "lucide-react"
import type { ReactNode } from "react"
import type { SubPanelData } from "@/lib/types/sub-panel"
import { hasEndDate, hasEnded, isCancelled, shortDate, TREND_STYLE } from "./plan-state"

function ForecastFrame({ children }: { children: ReactNode }) {
    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-emerald-400" />
                    <h2 className="text-sm font-medium text-muted-foreground">Data forecast</h2>
                </div>
                {children}
            </CardContent>
        </Card>
    )
}

function Figure({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
    return (
        <div className="space-y-0.5">
            <p className="text-xs text-muted-foreground">{label}</p>
            <div className={cn("flex items-center gap-1 text-sm font-semibold", className)}>{children}</div>
        </div>
    )
}

interface DataForecastProps {
    data: SubPanelData
    uuid: string
}

export function DataForecast({ data: sub, uuid }: DataForecastProps) {
    const { data, isLoading, isError } = useSubExhaustionPrediction(uuid)

    if (isLoading) {
        return (
            <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
                <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                    <Skeleton className="h-4 w-32" />
                    <div className="grid grid-cols-2 gap-3">
                        <Skeleton className="h-12" />
                        <Skeleton className="h-12" />
                    </div>
                </CardContent>
            </Card>
        )
    }

    // Unlimited plans have no exhaustion concept — nothing to show.
    if (data?.unlimited) return null

    if (hasEnded(sub)) {
        return (
            <ForecastFrame>
                <div className="flex items-start gap-2 rounded-md bg-red-500/10 border border-red-500/20 p-2.5 text-xs text-red-600 dark:text-red-400">
                    <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                    <span>
                        {isCancelled(sub)
                            ? "Your plan was cancelled."
                            : `Your plan ended${sub.end_date ? ` on ${shortDate(new Date(sub.end_date))}` : ""}. Once it's renewed, your remaining ${sub.data_remaining_display} works again.`}
                    </span>
                </div>
            </ForecastFrame>
        )
    }

    if (isError) {
        return <ForecastFrame><p className="text-xs text-muted-foreground">Forecast unavailable right now. It will appear once usage data syncs.</p></ForecastFrame>
    }
    if (!data || data.confidence === 0 || data.daily_avg_bytes === 0) {
        return <ForecastFrame><p className="text-xs text-muted-foreground">Not enough usage history yet. Check back after a day or two of activity.</p></ForecastFrame>
    }

    const trend = TREND_STYLE[data.usage_trend]
    const TrendIcon = trend.icon
    // How much earlier the data runs out than the plan ends.
    const daysShort = data.days_until_expiry - data.days_remaining

    return (
        <ForecastFrame>
            <div className="grid grid-cols-2 gap-3">
                <Figure label="Daily use">{formatBytes(data.daily_avg_bytes, 1)}</Figure>
                <Figure label="Trend" className={trend.color}>
                    <TrendIcon className="w-3.5 h-3.5" />
                    {trend.label}
                </Figure>
                <Figure label="Data lasts until">
                    {data.exhaustion_date ? `~${shortDate(parseDateLocal(data.exhaustion_date))}` : "—"}
                </Figure>
                {hasEndDate(sub) && sub.end_date ? (
                    <Figure label="Plan ends">{shortDate(new Date(sub.end_date))}</Figure>
                ) : (
                    <Figure label="Days of data left">{data.days_remaining} days</Figure>
                )}
            </div>

            {data.will_exhaust_first && (
                <div className="flex items-start gap-2 rounded-md bg-amber-500/10 border border-amber-500/20 p-2.5 text-xs text-amber-700 dark:text-amber-400">
                    <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                    <span>
                        {daysShort > 0
                            ? `At this pace your data runs out ${daysShort} ${daysShort === 1 ? "day" : "days"} before your plan ends.`
                            : "At this pace your data runs out before your plan ends."}
                    </span>
                </div>
            )}
        </ForecastFrame>
    )
}
