import { KpiCard } from "@/components/ui/kpi-card"
import { Skeleton } from "@/components/ui/skeleton"
import {
    HiOutlineUsers,
    HiOutlineGlobeAlt,
    HiOutlineStatusOnline,
} from "react-icons/hi"
import type { DashboardStats } from "@/lib/types"

interface StatsRowProps {
    stats: DashboardStats | undefined
    isLoading: boolean
}

export function StatsRow({ stats, isLoading }: StatsRowProps) {
    if (isLoading) {
        return (
            <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 md:gap-4 lg:gap-5">
                {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-24 rounded-lg" />
                ))}
            </div>
        )
    }

    if (!stats) {
        return (
            <div className="text-center py-4 text-sm text-muted-foreground">
                No stats available
            </div>
        )
    }

    return (
        <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 md:gap-4 lg:gap-5">
            <KpiCard
                title="Total Users"
                value={stats.total_users}
                description={`${stats.active_users} active`}
                icon={HiOutlineUsers}
                variant="default"
                compact
            />
            <KpiCard
                title="Subscriptions"
                value={stats.active_subscriptions}
                description={`${stats.expired_subscriptions} expired`}
                icon={HiOutlineGlobeAlt}
                variant="success"
                compact
            />
            <KpiCard
                title="Online Now"
                value={stats.online_users}
                description="Connected users"
                icon={HiOutlineStatusOnline}
                variant="success"
                compact
            />
        </div>
    )
}
