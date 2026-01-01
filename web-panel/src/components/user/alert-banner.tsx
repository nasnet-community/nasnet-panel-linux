import { useMemo, useState } from "react"
import { cn } from "@/lib/utils"
import type { UserDetails, Subscription } from "@/lib/types"

interface UserAlert {
    id: string
    priority: number
    message: string
    variant: "danger" | "warning" | "info"
}

export function useUserAlerts(
    user: UserDetails | undefined,
    subscriptions: Subscription[]
): UserAlert[] {
    return useMemo(() => {
        if (!user) return []
        const alerts: UserAlert[] = []

        if (user.is_banned) {
            alerts.push({ id: "banned", priority: 0, message: "User is banned", variant: "danger" })
        }

        for (const sub of subscriptions) {
            if (sub.status === "traffic_exhausted") {
                alerts.push({
                    id: `exhausted-${sub.id}`,
                    priority: 1,
                    message: `Subscription #${sub.id} data exhausted`,
                    variant: "danger",
                })
            }

            if (sub.status === "active") {
                const limit = sub.custom_data_limit ?? sub.data_limit
                if (limit > 0) {
                    const pct = (sub.data_used / limit) * 100
                    if (pct > 70) {
                        alerts.push({
                            id: `data-${sub.id}`,
                            priority: 2,
                            message: `Subscription #${sub.id} at ${pct.toFixed(1)}% data usage`,
                            variant: pct > 90 ? "danger" : "warning",
                        })
                    }
                }

                const expiryDate = sub.is_end_date_custom ? sub.custom_end_date : sub.end_date
                if (expiryDate) {
                    const daysLeft = Math.ceil(
                        (new Date(expiryDate).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
                    )
                    if (daysLeft >= 0 && daysLeft <= 3) {
                        alerts.push({
                            id: `expiry-${sub.id}`,
                            priority: 3,
                            message: `Subscription #${sub.id} expires in ${daysLeft}d`,
                            variant: "warning",
                        })
                    }
                }
            }
        }

        return alerts.sort((a, b) => a.priority - b.priority)
    }, [user, subscriptions])
}

interface AlertBannerProps {
    alerts: UserAlert[]
}

export function AlertBanner({ alerts }: AlertBannerProps) {
    const [expanded, setExpanded] = useState(false)

    if (alerts.length === 0) return null

    const topAlert = alerts[0]
    const variantStyles = {
        danger: "bg-red-500/10 border-red-500/30 text-red-400",
        warning: "bg-amber-500/10 border-amber-500/30 text-amber-400",
        info: "bg-blue-500/10 border-blue-500/30 text-blue-400",
    }
    const iconMap = { danger: "\u{1F534}", warning: "\u26A0", info: "\u2139" }

    return (
        <div className="relative">
            <button
                onClick={() => alerts.length > 1 && setExpanded(!expanded)}
                className={cn(
                    "w-full flex items-center justify-between rounded-xl border px-4 py-2.5 text-sm transition-colors",
                    variantStyles[topAlert.variant],
                    alerts.length > 1 && "cursor-pointer hover:opacity-90"
                )}
                role="alert"
                aria-expanded={alerts.length > 1 ? expanded : undefined}
            >
                <div className="flex items-center gap-2">
                    <span>{iconMap[topAlert.variant]}</span>
                    <span className="font-medium">{topAlert.message}</span>
                </div>
                {alerts.length > 1 && (
                    <div className="flex items-center gap-2">
                        <span className={cn(
                            "text-xs font-bold px-2 py-0.5 rounded-full",
                            topAlert.variant === "danger" ? "bg-red-500/20" :
                            topAlert.variant === "warning" ? "bg-amber-500/20" : "bg-blue-500/20"
                        )}>
                            {alerts.length} alerts
                        </span>
                        <span className="text-xs">{expanded ? "\u25B2" : "\u25BC"}</span>
                    </div>
                )}
            </button>

            {expanded && alerts.length > 1 && (
                <div className="absolute top-full left-0 right-0 z-20 mt-1 rounded-xl border bg-card shadow-lg overflow-hidden">
                    {alerts.map((alert) => (
                        <div
                            key={alert.id}
                            className={cn(
                                "flex items-center gap-2 px-4 py-2.5 text-sm border-b last:border-b-0",
                                variantStyles[alert.variant]
                            )}
                        >
                            <span>{iconMap[alert.variant]}</span>
                            <span>{alert.message}</span>
                        </div>
                    ))}
                </div>
            )}
        </div>
    )
}
