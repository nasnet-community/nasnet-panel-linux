import { useQuery } from "@tanstack/react-query"
import type { NavBadgeValue } from "@/components/sidebar/nav-badge"
import { settingsApi } from "@/lib/api/settings"
import { queryKeys } from "@/lib/queries/keys"
import { useDashboardStats } from "@/lib/queries/use-dashboard"
import type { NavBadgeKey } from "./nav-config"

export type NavBadges = Partial<Record<NavBadgeKey, NavBadgeValue>>

export function useNavBadges(): NavBadges {
    // Same query key as the status strip, so this adds no request.
    const { data: stats } = useDashboardStats()
    const errors = stats?.alerts_error ?? 0
    const warns = stats?.alerts_warn ?? 0
    // the Settings page asks every 30 s; from anywhere else every 5 min is plenty
    const { data: status } = useQuery({
        queryKey: queryKeys.settingsStatus(),
        queryFn: () => settingsApi.getStatus(),
        refetchInterval: 5 * 60_000,
        staleTime: 60_000,
    })
    const update = status?.hub.update
    return {
        alerts: { count: errors + warns, tone: errors > 0 ? "danger" : "warning" },
        settings: { count: update?.available ? 1 : 0, tone: "warning", dot: true, label: update?.version ? `${update.version} is available` : undefined },
    }
}
