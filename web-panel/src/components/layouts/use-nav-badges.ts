import type { NavBadgeValue } from "@/components/sidebar/nav-badge"
import { useDashboardStats } from "@/lib/queries/use-dashboard"
import type { NavBadgeKey } from "./nav-config"

export type NavBadges = Partial<Record<NavBadgeKey, NavBadgeValue>>

export function useNavBadges(): NavBadges {
    // Same query key as the status strip, so this adds no request.
    const { data: stats } = useDashboardStats()
    const errors = stats?.alerts_error ?? 0
    const warns = stats?.alerts_warn ?? 0
    return {
        alerts: { count: errors + warns, tone: errors > 0 ? "danger" : "warning" },
    }
}
