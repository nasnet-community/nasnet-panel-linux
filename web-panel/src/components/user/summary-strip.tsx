import type { UserDetails, Subscription } from "@/lib/types"
import { formatBytes } from "@/lib/utils"
import { cn } from "@/lib/utils"
import { Skeleton } from "@/components/ui/skeleton"

interface SummaryStripProps {
    user: UserDetails
    subscriptions: Subscription[]
    onNavigateTab: (tab: string) => void
}

function SummaryCard({
    label,
    value,
    secondary,
    color,
    onClick,
    ariaLabel,
}: {
    label: string
    value: string
    secondary: string
    color: string
    onClick: () => void
    ariaLabel: string
}) {
    return (
        <button
            onClick={onClick}
            role="link"
            aria-label={ariaLabel}
            className={cn(
                "bg-card/50 rounded-xl p-4 border-l-[3px] text-left cursor-pointer",
                "hover:bg-muted/50 transition-colors group",
                color
            )}
        >
            <div className="flex justify-between items-center">
                <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                    {label}
                </span>
                <span className="text-muted-foreground/30 group-hover:text-muted-foreground/60 transition-colors text-xs">
                    →
                </span>
            </div>
            <div className="text-xl font-extrabold mt-1 mb-0.5">{value}</div>
            <div className="text-[10px] text-muted-foreground/60">{secondary}</div>
        </button>
    )
}

export function SummaryStripSkeleton() {
    return (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            {[1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-[88px] rounded-xl" />
            ))}
        </div>
    )
}

export function SummaryStrip({ user, subscriptions, onNavigateTab }: SummaryStripProps) {
    const activeSubs = subscriptions.filter((s) => s.status === "active")
    const maxDataPercent = activeSubs.reduce((max, s) => {
        const limit = s.custom_data_limit ?? s.data_limit
        if (limit <= 0) return max
        return Math.max(max, (s.data_used / limit) * 100)
    }, 0)

    const dataWarning = maxDataPercent > 70
        ? `${Math.round(maxDataPercent)}% data used`
        : `${activeSubs.length} active`

    const totalData = user.total_data_upload + user.total_data_download
    const lastActive = user.last_active_at
        ? new Date(user.last_active_at).toLocaleDateString(undefined, { month: "short", day: "numeric" })
        : "Never"
    const joinedDate = new Date(user.created_at).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })

    return (
        <div className="grid grid-cols-2 lg:grid-cols-3 gap-3">
            <SummaryCard
                label="Subscriptions"
                value={String(user.active_subscriptions)}
                secondary={dataWarning}
                color="border-l-primary"
                onClick={() => onNavigateTab("subscriptions")}
                ariaLabel={`View subscriptions — ${user.active_subscriptions} active, ${dataWarning}`}
            />
            <SummaryCard
                label="Traffic"
                value={formatBytes(totalData)}
                secondary={`↑${formatBytes(user.total_data_upload)} · ↓${formatBytes(user.total_data_download)}`}
                color="border-l-blue-500"
                onClick={() => onNavigateTab("analytics")}
                ariaLabel={`View analytics — ${formatBytes(totalData)} total traffic`}
            />
            <SummaryCard
                label="Last Active"
                value={lastActive}
                secondary={`Joined ${joinedDate}`}
                color="border-l-amber-500"
                onClick={() => onNavigateTab("profile")}
                ariaLabel={`View profile — last active ${lastActive}, joined ${joinedDate}`}
            />
        </div>
    )
}
