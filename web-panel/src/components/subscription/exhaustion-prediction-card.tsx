import { useExhaustionPrediction } from "@/lib/queries/use-analytics"
import { Skeleton } from "@/components/ui/skeleton"
import { TrendingUp, TrendingDown, Minus, AlertTriangle, Infinity as InfinityIcon } from "lucide-react"

function formatBytes(bytes: number): string {
    if (bytes === 0) return "0 B"
    const sizes = ["B", "KB", "MB", "GB", "TB"]
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(1) + " " + sizes[i]
}

interface ExhaustionPredictionCardProps {
    subscriptionId: number
    compact?: boolean
}

export function ExhaustionPredictionCard({ subscriptionId, compact = false }: ExhaustionPredictionCardProps) {
    const { data, isLoading } = useExhaustionPrediction(subscriptionId)

    if (isLoading) {
        return <Skeleton className={compact ? "h-6 w-32" : "h-28"} />
    }

    if (!data) return null

    if (data.unlimited) {
        if (compact) {
            return (
                <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                    <InfinityIcon className="w-3 h-3" />
                    Unlimited
                </span>
            )
        }
        return (
            <div className="rounded-lg border border-border/50 bg-muted/20 p-4">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <InfinityIcon className="w-4 h-4" />
                    <span>Unlimited data plan</span>
                </div>
            </div>
        )
    }

    const trendIcon = {
        increasing: <TrendingUp className="w-3.5 h-3.5 text-red-500" />,
        decreasing: <TrendingDown className="w-3.5 h-3.5 text-emerald-500" />,
        stable: <Minus className="w-3.5 h-3.5 text-muted-foreground" />,
    }[data.usage_trend]

    const trendLabel = {
        increasing: "Increasing",
        decreasing: "Decreasing",
        stable: "Stable",
    }[data.usage_trend]

    const trendColor = {
        increasing: "text-red-500",
        decreasing: "text-emerald-500",
        stable: "text-muted-foreground",
    }[data.usage_trend]

    const confidenceLabel = data.confidence >= 0.8 ? "High" : data.confidence >= 0.5 ? "Medium" : "Low"

    // Compact inline badge for list views
    if (compact) {
        if (data.daily_avg_bytes === 0) {
            return (
                <span className="text-xs text-muted-foreground">No usage data</span>
            )
        }
        if (data.will_exhaust_first) {
            return (
                <span className="inline-flex items-center gap-1 text-xs font-medium text-red-500">
                    <AlertTriangle className="w-3 h-3" />
                    Exhausts in {data.days_remaining}d
                </span>
            )
        }
        return (
            <span className="inline-flex items-center gap-1 text-xs text-emerald-600">
                {trendIcon}
                {data.days_remaining}d of data left
            </span>
        )
    }

    // Full card
    return (
        <div className="space-y-2.5">
            <div className="flex items-center justify-between">
                <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Exhaustion Forecast</h4>
                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${
                    data.confidence >= 0.8 ? "bg-emerald-500/10 text-emerald-600" :
                    data.confidence >= 0.5 ? "bg-amber-500/10 text-amber-600" :
                    "bg-muted text-muted-foreground"
                }`}>
                    {confidenceLabel} confidence
                </span>
            </div>

            {data.daily_avg_bytes === 0 ? (
                <p className="text-xs text-muted-foreground">Not enough usage data to predict exhaustion.</p>
            ) : (
                <>
                    {/* Prediction details */}
                    <div className="grid grid-cols-2 gap-2 text-sm">
                        <div>
                            <p className="text-[10px] text-muted-foreground uppercase tracking-wider">Daily Average</p>
                            <p className="font-semibold">{formatBytes(data.daily_avg_bytes)}/day</p>
                        </div>
                        <div>
                            <p className="text-[10px] text-muted-foreground uppercase tracking-wider">Trend</p>
                            <div className={`flex items-center gap-1 font-semibold ${trendColor}`}>
                                {trendIcon}
                                <span>{trendLabel}</span>
                            </div>
                        </div>
                        <div>
                            <p className="text-[10px] text-muted-foreground uppercase tracking-wider">Data Runs Out</p>
                            <p className={`font-semibold ${data.will_exhaust_first ? "text-red-500" : ""}`}>
                                {data.exhaustion_date ?? "—"}
                            </p>
                        </div>
                        <div>
                            <p className="text-[10px] text-muted-foreground uppercase tracking-wider">Subscription Ends</p>
                            <p className="font-semibold">{data.end_date ?? "No expiry"}</p>
                        </div>
                    </div>

                    {/* Warning */}
                    {data.will_exhaust_first && (
                        <div className="flex items-start gap-2 rounded-md bg-red-500/10 border border-red-500/20 p-2.5 text-xs text-red-600">
                            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                            <span>
                                Data will run out <strong>{data.days_remaining} days</strong> before the subscription expires
                                ({data.days_until_expiry - data.days_remaining} days early).
                            </span>
                        </div>
                    )}
                </>
            )}
        </div>
    )
}
