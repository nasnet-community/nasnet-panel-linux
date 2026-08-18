import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { CircularProgress } from "@/components/ui/circular-progress"
import { Skeleton } from "@/components/ui/skeleton"
import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import { formatBytes, parseDateLocal } from "@/lib/utils"
import { TrendingUp, TrendingDown, Minus, AlertTriangle } from "lucide-react"
import type { SubPanelData } from "@/lib/types/sub-panel"

function getColor(percent: number): "emerald" | "amber" | "red" {
    if (percent > 90) return "red"
    if (percent > 70) return "amber"
    return "emerald"
}

interface DataDetailDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    data: SubPanelData
    uuid: string
}

export function DataDetailDialog({ open, onOpenChange, data, uuid }: DataDetailDialogProps) {
    const { data: prediction, isLoading } = useSubExhaustionPrediction(uuid)

    const color = data.is_unlimited ? "emerald" : getColor(data.data_usage_percent)

    const trendConfig = prediction
        ? {
              increasing: {
                  icon: <TrendingUp className="w-3.5 h-3.5" />,
                  label: "Increasing",
                  color: "text-red-400",
              },
              decreasing: {
                  icon: <TrendingDown className="w-3.5 h-3.5" />,
                  label: "Decreasing",
                  color: "text-emerald-400",
              },
              stable: {
                  icon: <Minus className="w-3.5 h-3.5" />,
                  label: "Stable",
                  color: "text-muted-foreground",
              },
          }[prediction.usage_trend]
        : null

    const servers = data.servers ?? []
    const totalDataUsed = servers.reduce((sum, s) => sum + s.data_used, 0)

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Data Usage</DialogTitle>
                </DialogHeader>

                <div className="space-y-4">
                    {/* Hero section */}
                    <div className="flex flex-col items-center gap-2">
                        {data.is_unlimited ? (
                            <div
                                className="flex items-center justify-center"
                                style={{ width: 120, height: 120 }}
                            >
                                <span className="text-5xl font-bold text-emerald-500 leading-none">
                                    &infin;
                                </span>
                            </div>
                        ) : (
                            <CircularProgress
                                value={data.data_usage_percent}
                                size={120}
                                strokeWidth={8}
                                color={color}
                                showValue
                            />
                        )}
                        <p className="text-sm text-muted-foreground">
                            {data.data_used_display}
                            {" / "}
                            {data.is_unlimited ? (
                                <span>&infin;</span>
                            ) : (
                                data.data_limit_display
                            )}
                        </p>
                    </div>

                    {/* Metrics grid */}
                    <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Remaining
                            </p>
                            <p className="text-sm font-semibold">
                                {data.data_remaining_display}
                            </p>
                        </div>

                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Daily Avg
                            </p>
                            {isLoading ? (
                                <Skeleton className="h-5 w-20" />
                            ) : prediction ? (
                                <p className="text-sm font-semibold">
                                    {formatBytes(prediction.daily_avg_bytes)}/day
                                </p>
                            ) : (
                                <p className="text-sm font-semibold text-muted-foreground">&mdash;</p>
                            )}
                        </div>

                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Trend
                            </p>
                            {isLoading ? (
                                <Skeleton className="h-5 w-24" />
                            ) : trendConfig ? (
                                <div
                                    className={`flex items-center gap-1 text-sm font-semibold ${trendConfig.color}`}
                                >
                                    {trendConfig.icon}
                                    <span>{trendConfig.label}</span>
                                </div>
                            ) : (
                                <p className="text-sm font-semibold text-muted-foreground">&mdash;</p>
                            )}
                        </div>

                        <div className="space-y-0.5">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Data Lasts Until
                            </p>
                            {isLoading ? (
                                <Skeleton className="h-5 w-16" />
                            ) : prediction?.exhaustion_date ? (
                                <p className="text-sm font-semibold">
                                    {parseDateLocal(prediction.exhaustion_date).toLocaleDateString(
                                        "en-US",
                                        { month: "short", day: "numeric" }
                                    )}
                                </p>
                            ) : (
                                <p className="text-sm font-semibold text-muted-foreground">&mdash;</p>
                            )}
                        </div>
                    </div>

                    {/* Server breakdown */}
                    {servers.length > 0 && (
                        <div className="space-y-2">
                            <p className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
                                Usage by Server
                            </p>
                            <div className="space-y-2">
                                {servers.map((server) => {
                                    const barPercent =
                                        totalDataUsed > 0
                                            ? (server.data_used / totalDataUsed) * 100
                                            : 0

                                    return (
                                        <div key={server.name} className="space-y-1">
                                            <div className="flex items-center justify-between text-xs">
                                                <span className="font-medium">
                                                    {server.flag} {server.name}
                                                </span>
                                                <span className="text-muted-foreground">
                                                    {server.data_used_display}
                                                </span>
                                            </div>
                                            <div className="h-1.5 w-full rounded-full bg-emerald-500/20">
                                                <div
                                                    className="h-full rounded-full bg-emerald-500 transition-all"
                                                    style={{ width: `${barPercent}%` }}
                                                />
                                            </div>
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    )}

                    {/* Warning banner */}
                    {prediction?.will_exhaust_first && (
                        <div className="flex items-start gap-2 rounded-md bg-red-500/10 border border-red-500/20 p-2.5 text-xs text-red-400">
                            <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                            <span>
                                Your data may run out{" "}
                                <strong>{prediction.days_remaining} days</strong> before your
                                subscription expires.
                            </span>
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
