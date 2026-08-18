import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { parseDateLocal } from "@/lib/utils"
import { TrendingUp, TrendingDown, Minus, AlertTriangle, Activity } from "lucide-react"
import type { ReactNode } from "react"

function formatBytes(bytes: number): string {
    if (bytes === 0) return "0 B"
    const sizes = ["B", "KB", "MB", "GB", "TB"]
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(1) + " " + sizes[i]
}

/**
 * A forecast can land in a different year than today — "Mar 7" alone then reads
 * as a date that already passed. Include the year whenever it isn't this one.
 */
function formatForecastDate(iso: string): string {
    const d = parseDateLocal(iso)
    const sameYear = d.getFullYear() === new Date().getFullYear()
    return d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        ...(sameYear ? {} : { year: "numeric" }),
    })
}

function ForecastFrame({ children }: { children: ReactNode }) {
    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-3">
                <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-emerald-400" />
                    <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
                        Data Forecast
                    </h2>
                </div>
                {children}
            </CardContent>
        </Card>
    )
}

interface DataForecastProps {
    uuid: string
}

export function DataForecast({ uuid }: DataForecastProps) {
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

    if (isError) {
        return <ForecastFrame><p className="text-xs text-muted-foreground">Forecast unavailable right now. It will appear once usage data syncs.</p></ForecastFrame>
    }
    if (!data || data.confidence === 0 || data.daily_avg_bytes === 0) {
        return <ForecastFrame><p className="text-xs text-muted-foreground">Not enough usage history yet — check back after a day or two of activity.</p></ForecastFrame>
    }

    const trendConfig = {
        increasing: {
            icon: <TrendingUp className="w-3.5 h-3.5" />,
            label: "Increasing",
            color: "text-red-600 dark:text-red-400",
        },
        decreasing: {
            icon: <TrendingDown className="w-3.5 h-3.5" />,
            label: "Decreasing",
            color: "text-emerald-700 dark:text-emerald-400",
        },
        stable: {
            icon: <Minus className="w-3.5 h-3.5" />,
            label: "Stable",
            color: "text-muted-foreground",
        },
    }[data.usage_trend]

    return (
        <ForecastFrame>
            <div className="grid grid-cols-2 gap-3">
                <div className="space-y-0.5">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Daily Usage</p>
                    <p className="text-sm font-semibold">{formatBytes(data.daily_avg_bytes)}/day</p>
                </div>
                <div className="space-y-0.5">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Trend</p>
                    <div className={`flex items-center gap-1 text-sm font-semibold ${trendConfig.color}`}>
                        {trendConfig.icon}
                        <span>{trendConfig.label}</span>
                    </div>
                </div>
                <div className="space-y-0.5">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Data Lasts Until</p>
                    <p className={`text-sm font-semibold ${data.will_exhaust_first ? "text-red-600 dark:text-red-400" : ""}`}>
                        {data.exhaustion_date ? formatForecastDate(data.exhaustion_date) : "—"}
                    </p>
                </div>
                <div className="space-y-0.5">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Days of Data Left</p>
                    <p className={`text-sm font-semibold ${data.days_remaining <= 3 ? "text-red-600 dark:text-red-400" : data.days_remaining <= 7 ? "text-amber-600 dark:text-amber-400" : ""}`}>
                        {data.days_remaining} days
                    </p>
                </div>
            </div>
            <p className="text-xs text-muted-foreground">
                Projected from your recent daily average. It moves as your usage changes.
            </p>

            {data.will_exhaust_first && (
                <div className="flex items-start gap-2 rounded-md bg-red-500/10 border border-red-500/20 p-2.5 text-xs text-red-600 dark:text-red-400">
                    <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                    <span>
                        Your data may run out <strong>{data.days_remaining} days</strong> before your subscription expires.
                    </span>
                </div>
            )}
        </ForecastFrame>
    )
}
