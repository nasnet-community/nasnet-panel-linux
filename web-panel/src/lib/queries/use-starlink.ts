import { useQuery } from "@tanstack/react-query"
import { queryKeys } from "./keys"
import { getStarlinkStatus, getStarlinkObstructionMap, getStarlinkHistory } from "@/lib/admin-api"

const TIME_RANGE_MS: Record<string, number> = {
    "1h": 60 * 60 * 1000,
    "6h": 6 * 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
    "7d": 7 * 24 * 60 * 60 * 1000,
}

export function useStarlinkStatus(nodeId: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.starlinkStatus(nodeId),
        queryFn: async () => {
            const res = await getStarlinkStatus(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch starlink status")
            return res.data!
        },
        enabled,
        refetchInterval: 10_000,
        staleTime: 5_000,
    })
}

export function useStarlinkObstructionMap(nodeId: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.starlinkMap(nodeId),
        queryFn: async () => {
            const res = await getStarlinkObstructionMap(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch obstruction map")
            return res.data!
        },
        enabled,
        refetchInterval: 60_000,
        staleTime: 55_000,
    })
}

export function useStarlinkHistory(
    nodeId: number,
    enabled: boolean,
    timeRange: string,
    limit?: number,
    refetchInterval: number = 30_000,
) {
    return useQuery({
        queryKey: queryKeys.starlinkHistory(nodeId, timeRange),
        queryFn: async () => {
            const ms = TIME_RANGE_MS[timeRange]
            const since = ms ? new Date(Date.now() - ms).toISOString() : undefined
            const res = await getStarlinkHistory(nodeId, limit, since)
            if (!res.success) throw new Error(res.error || "Failed to fetch starlink history")
            return res.data!
        },
        enabled,
        refetchInterval,
        staleTime: Math.max(refetchInterval - 1000, 1000),
    })
}
