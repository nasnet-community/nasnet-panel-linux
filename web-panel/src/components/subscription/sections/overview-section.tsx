import { Upload, Download, Infinity as InfinityIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { Sparkline } from "@/components/ui/sparkline"
import { ExhaustionPredictionCard } from "@/components/subscription/exhaustion-prediction-card"
import { useSubscriptionUsageHistory } from "@/lib/queries"
import { cn, formatBytes, formatDate } from "@/lib/utils"
import { getUsageBarColor } from "@/lib/constants/usage-thresholds"
import type { Subscription } from "@/lib/types"
import type { SubscriptionDerived } from "@/lib/subscription-derived"
import { StatRow } from "./section-header"

interface OverviewSectionProps {
    subscription: Subscription
    derived: SubscriptionDerived
}

export function OverviewSection({ subscription, derived }: OverviewSectionProps) {
    const { data: usageHistory = [] } = useSubscriptionUsageHistory(subscription.id, 30)
    const sparklineData = usageHistory.map((p) => p.data_used)
    const effectiveEndDate = derived.effectiveEndDate
    const daysColor = derived.isUnlimitedExpiry
        ? "text-foreground"
        : derived.daysRemaining <= 3
            ? "text-red-500"
            : derived.daysRemaining <= 7
                ? "text-amber-500"
                : "text-foreground"
    const usageBarColor = getUsageBarColor(derived.usagePercent)

    return (
        <div className="rounded-lg border p-3 space-y-3">
            {/* Usage bar */}
            <div className="space-y-1">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>{formatBytes(subscription.data_used)}</span>
                    <span>{derived.limitDisplay}</span>
                </div>
                <div className="h-2 bg-muted rounded-full overflow-hidden">
                    <div
                        className={cn("h-full rounded-full transition-all", usageBarColor)}
                        style={{
                            width: derived.isUnlimitedData ? "3%" : `${Math.max(derived.usagePercent, 1)}%`,
                        }}
                    />
                </div>
                {!derived.isUnlimitedData && (
                    <p className="text-[10px] text-muted-foreground">{derived.usagePercent.toFixed(1)}% used</p>
                )}
            </div>

            {/* Sparkline — only renders when at least two days of data accrued */}
            {sparklineData.length >= 2 && sparklineData.some((v) => v > 0) && (
                <div className="space-y-0.5">
                    <div className="flex items-center justify-between text-[10px] text-muted-foreground uppercase tracking-wider">
                        <span>Last {sparklineData.length} days</span>
                        <span>daily usage</span>
                    </div>
                    <Sparkline data={sparklineData} height={36} color="var(--primary)" />
                </div>
            )}

            <Separator />

            <div className="space-y-0.5">
                <StatRow
                    label="Days Left"
                    value={
                        <span className={daysColor}>
                            {derived.isUnlimitedExpiry ? (
                                <InfinityIcon className="w-4 h-4 inline" aria-label="Unlimited" />
                            ) : (
                                derived.daysRemaining
                            )}
                        </span>
                    }
                />
                {/*  Speed limit feature - disabled
                <StatRow label="Speed" value={derived.bandwidthDisplay} />
                */}
                {(subscription.lifetime_data_used || 0) > 0 && (
                    <StatRow label="Lifetime" value={formatBytes(subscription.lifetime_data_used)} />
                )}
                <div className="flex items-center justify-between py-0.5">
                    <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                        <Upload className="w-3 h-3 text-blue-500" />
                        <span className="font-medium text-foreground">{formatBytes(subscription.data_upload || 0)}</span>
                    </span>
                    <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                        <Download className="w-3 h-3 text-emerald-500" />
                        <span className="font-medium text-foreground">{formatBytes(subscription.data_download || 0)}</span>
                    </span>
                </div>
            </div>

            <div className="flex items-center justify-between text-[11px] text-muted-foreground border-t pt-1.5">
                <span>Created {formatDate(subscription.created_at, "long")}</span>
                <span>
                    {derived.isUnlimitedExpiry
                        ? "No expiry"
                        : effectiveEndDate
                            ? formatDate(effectiveEndDate, "long")
                            : "No expiry"}
                    {subscription.is_end_date_custom && !derived.isUnlimitedExpiry && " (custom)"}
                </span>
            </div>

            {subscription.status === "active" && !derived.isUnlimitedData && (
                <>
                    <Separator />
                    <ExhaustionPredictionCard subscriptionId={subscription.id} />
                </>
            )}
        </div>
    )
}

/** Compact status strip rendered in the sheet header. */
export function SubscriptionStatusStrip({ subscription }: { subscription: Subscription }) {
    const statusTone: Record<string, "success" | "warning" | "danger" | "secondary"> = {
        active: "success",
        paused: "warning",
        expired: "danger",
        cancelled: "danger",
        traffic_exhausted: "danger",
    }
    const statusLabel = subscription.status === "traffic_exhausted" ? "Exhausted" : subscription.status

    return (
        <div className="flex items-center gap-2 flex-wrap pr-10">
            <Badge variant={statusTone[subscription.status] || "secondary"} className="text-xs px-2 py-0.5 uppercase">
                {statusLabel}
            </Badge>
            {subscription.is_manual && (
                <Badge variant="outline" className="text-xs px-2 py-0.5 uppercase">
                    Manual
                </Badge>
            )}
            <span className="text-xs font-mono text-muted-foreground ml-auto">#{subscription.id}</span>
        </div>
    )
}
