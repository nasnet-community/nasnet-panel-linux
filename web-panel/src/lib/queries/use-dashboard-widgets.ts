import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { queryKeys } from "./keys"
import { useNodes, useNodesStatsBulk } from "./use-nodes"

// Node aggregate stats derived from existing useNodes + useNodesStatsBulk
// caches. SSE node.stats_updated patches the bulk cache, so this widget
// refreshes without explicit invalidation.
export function useNodeAggregateStats() {
    const { data: nodes = [], isLoading: nodesLoading } = useNodes()

    const onlineIds = useMemo(
        () => nodes.filter((n) => n.is_online).map((n) => n.id),
        [nodes],
    )

    const { data: bulk, isLoading: bulkLoading } = useNodesStatsBulk(
        onlineIds,
        onlineIds.length > 0,
    )

    const data = useMemo(() => {
        let totalTraffic = 0
        let totalCpu = 0
        let totalConnections = 0
        let totalUpSpeed = 0
        let totalDownSpeed = 0
        let cpuCount = 0

        const onlineNodes = nodes.filter((n) => n.is_online)
        const nodeRows = onlineNodes.map((node) => {
            const stats = bulk?.[String(node.id)]?.stats
            if (stats) {
                totalTraffic += (stats.total_uplink || 0) + (stats.total_downlink || 0)
                totalCpu += stats.cpu_percent || 0
                totalConnections += (stats.tcp_count || 0) + (stats.udp_count || 0)
                totalUpSpeed += stats.up_speed || 0
                totalDownSpeed += stats.down_speed || 0
                cpuCount++
            }
            return {
                id: node.id,
                name: node.name,
                ip: node.ip,
                isOnline: node.is_online,
                countryCode: node.country_code,
                cpuPercent: stats?.cpu_percent ?? 0,
                memoryPercent: stats?.memory_percent ?? 0,
                diskPercent: stats?.disk_percent ?? 0,
                onlineUsers: stats?.online_users ?? 0,
                upSpeed: stats?.up_speed ?? 0,
                downSpeed: stats?.down_speed ?? 0,
                totalUplink: stats?.total_uplink ?? 0,
                totalDownlink: stats?.total_downlink ?? 0,
                memoryUsedMb: stats?.memory_used_mb ?? 0,
                memoryTotalMb: stats?.memory_total_mb ?? 0,
                diskUsedGb: stats?.disk_used_gb ?? 0,
                diskTotalGb: stats?.disk_total_gb ?? 0,
            }
        })

        return {
            nodes: nodeRows,
            totalTraffic,
            avgCpu: cpuCount > 0 ? totalCpu / cpuCount : 0,
            totalConnections,
            totalUpSpeed,
            totalDownSpeed,
            onlineCount: onlineNodes.length,
            totalCount: nodes.length,
        }
    }, [nodes, bulk])

    return { data, isLoading: nodesLoading || bulkLoading }
}

// User activity heatmap - generates synthetic data based on available info
// When backend provides the endpoint, this can be updated to use real data
export function useUserActivityHeatmap() {
    return useQuery({
        queryKey: [...queryKeys.dashboard, "activity-heatmap"],
        queryFn: async () => {
            // Try to fetch from backend first
            try {
                const res = await api.get<{ hours: Array<{ hour: number; count: number }> }>(
                    "/api/v1/admin/dashboard/activity-heatmap"
                )
                if (res.success && res.data) {
                    return res.data.hours
                }
            } catch {
                // Endpoint doesn't exist yet - return empty data
            }
            return null
        },
        staleTime: 5 * 60 * 1000,
    })
}
