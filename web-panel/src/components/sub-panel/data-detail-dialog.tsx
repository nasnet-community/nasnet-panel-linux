import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { CircularProgress } from "@/components/ui/circular-progress"
import { Skeleton } from "@/components/ui/skeleton"
import { useSubExhaustionPrediction } from "@/lib/queries/use-sub-panel-analytics"
import { formatBytes, parseDateLocal } from "@/lib/utils"
import { AlertTriangle } from "lucide-react"
import type { ReactNode } from "react"
import type { SubPanelData } from "@/lib/types/sub-panel"
import { cleanServerName, dataLeftPercent, dataTone, shortDate, TREND_STYLE } from "./plan-state"

interface DataDetailDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    data: SubPanelData
    uuid: string
}

const PROTOCOL_LABEL: Record<string, string> = { WIREGUARD: "WireGuard" }

function Metric({ label, loading, children }: { label: string; loading?: boolean; children: ReactNode }) {
    return (
        <div className="space-y-0.5">
            <p className="text-xs text-muted-foreground font-medium">{label}</p>
            {loading ? <Skeleton className="h-5 w-20" /> : children}
        </div>
    )
}

export function DataDetailDialog({ open, onOpenChange, data, uuid }: DataDetailDialogProps) {
    const { data: prediction, isLoading } = useSubExhaustionPrediction(uuid)

    const hasForecast = !!prediction && prediction.confidence > 0 && prediction.daily_avg_bytes > 0
    const trend = prediction ? TREND_STYLE[prediction.usage_trend] : null
    const TrendIcon = trend?.icon

    // Same server can appear once per protocol ("Main" over VLESS and WireGuard).
    const servers = (data.servers ?? [])
        .filter((s) => s.protocol !== "INFO")
        .sort((a, b) => b.data_used - a.data_used)
    const totalDataUsed = servers.reduce((sum, s) => sum + s.data_used, 0)
    const daysShort = prediction ? prediction.days_until_expiry - prediction.days_remaining : 0

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Data usage</DialogTitle>
                </DialogHeader>

                <div className="space-y-4">
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
                                value={dataLeftPercent(data)}
                                size={120}
                                strokeWidth={8}
                                color={dataTone(data, prediction)}
                            >
                                <span className="text-2xl font-bold tracking-tight leading-none">
                                    {Math.round(dataLeftPercent(data))}%
                                </span>
                                <span className="text-xs font-medium text-muted-foreground mt-0.5">left</span>
                            </CircularProgress>
                        )}
                        <p className="text-sm text-muted-foreground">
                            {data.is_unlimited
                                ? `${data.data_used_display} used`
                                : `${data.data_used_display} of ${data.data_limit_display} used`}
                        </p>
                    </div>

                    <div className="grid grid-cols-2 gap-3">
                        <Metric label="Left">
                            <p className="text-sm font-semibold">{data.data_remaining_display}</p>
                        </Metric>
                        <Metric label="Daily use" loading={isLoading}>
                            <p className="text-sm font-semibold">
                                {hasForecast ? formatBytes(prediction.daily_avg_bytes, 1) : "—"}
                            </p>
                        </Metric>
                        <Metric label="Trend" loading={isLoading}>
                            {hasForecast && trend && TrendIcon ? (
                                <div className={`flex items-center gap-1 text-sm font-semibold ${trend.color}`}>
                                    <TrendIcon className="w-3.5 h-3.5" />
                                    <span>{trend.label}</span>
                                </div>
                            ) : (
                                <p className="text-sm font-semibold text-muted-foreground">&mdash;</p>
                            )}
                        </Metric>
                        <Metric label="Lasts until" loading={isLoading}>
                            <p className="text-sm font-semibold">
                                {hasForecast && prediction.exhaustion_date
                                    ? `~${shortDate(parseDateLocal(prediction.exhaustion_date))}`
                                    : "—"}
                            </p>
                        </Metric>
                    </div>

                    {servers.length > 0 && (
                        <div className="space-y-2">
                            <p className="text-xs text-muted-foreground font-medium">Usage by server</p>
                            <div className="space-y-2">
                                {servers.map((server, i) => {
                                    const barPercent =
                                        totalDataUsed > 0
                                            ? (server.data_used / totalDataUsed) * 100
                                            : 0

                                    return (
                                        <div key={`${server.address}-${server.port}-${i}`} className="space-y-1">
                                            <div className="flex items-center justify-between gap-2 text-xs">
                                                <span className="min-w-0 truncate">
                                                    <span className="font-medium">
                                                        {server.flag} {cleanServerName(server)}
                                                    </span>
                                                    <span className="text-muted-foreground">
                                                        {" · "}{PROTOCOL_LABEL[server.protocol] ?? server.protocol}
                                                    </span>
                                                </span>
                                                <span className="shrink-0 text-muted-foreground tabular-nums">
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

                    {prediction?.will_exhaust_first && (
                        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 border border-amber-500/20 p-2.5 text-xs text-amber-700 dark:text-amber-400">
                            <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                            <span>
                                {daysShort > 0
                                    ? `At this pace your data runs out ${daysShort} ${daysShort === 1 ? "day" : "days"} before your plan ends.`
                                    : "At this pace your data runs out before your plan ends."}
                            </span>
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
