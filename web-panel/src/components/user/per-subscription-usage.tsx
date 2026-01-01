import type { Subscription } from "@/lib/types"
import { formatBytes, cn } from "@/lib/utils"

interface PerSubscriptionUsageProps {
    subscriptions: Subscription[]
}

export function PerSubscriptionUsage({ subscriptions }: PerSubscriptionUsageProps) {
    const active = subscriptions.filter((s) => s.status === "active" || s.status === "paused")

    if (active.length === 0) {
        return <p className="text-xs text-muted-foreground/60 text-center py-4">No active subscriptions</p>
    }

    return (
        <div className="space-y-3">
            <h4 className="text-sm font-semibold">Per-Subscription Usage</h4>
            <div className="space-y-2">
                {active.map((sub) => {
                    const limit = sub.custom_data_limit ?? sub.data_limit
                    const pct = limit > 0 ? Math.min((sub.data_used / limit) * 100, 100) : 0
                    const isUnlimited = limit === 0

                    return (
                        <div key={sub.id} className="flex items-center gap-3 p-2 rounded-lg bg-muted/30">
                            <div className={cn(
                                "w-1.5 h-1.5 rounded-full shrink-0",
                                sub.status === "active" ? "bg-emerald-500" : "bg-amber-500"
                            )} />
                            <div className="flex-1 min-w-0">
                                <div className="text-xs font-semibold truncate">
                                    #{sub.id} {sub.label || "\u2014"}
                                </div>
                                <div className="flex items-center gap-2 mt-1">
                                    <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                                        <div
                                            className={cn(
                                                "h-full rounded-full",
                                                isUnlimited ? "bg-emerald-500" :
                                                pct > 90 ? "bg-red-500" :
                                                pct > 70 ? "bg-amber-500" : "bg-emerald-500"
                                            )}
                                            style={{ width: isUnlimited ? "2%" : `${pct}%` }}
                                        />
                                    </div>
                                    <span className="text-[10px] text-muted-foreground whitespace-nowrap">
                                        {formatBytes(sub.data_used)} / {isUnlimited ? "\u221E" : formatBytes(limit)}
                                    </span>
                                </div>
                            </div>
                        </div>
                    )
                })}
            </div>
        </div>
    )
}
