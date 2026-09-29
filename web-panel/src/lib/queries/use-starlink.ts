import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ApiError } from "@/lib/api"
import { queryKeys } from "./keys"
import {
    getStarlinkStatus, getStarlinkObstructionMap, getStarlinkHistory,
    getStarlinkLive, getStarlinkDevice, getStarlinkEvents, runStarlinkAction,
} from "@/lib/api/nodes"
import type { StarlinkAction } from "@/lib/types"

const TIME_RANGE_MS: Record<string, number> = {
    "15m": 15 * 60 * 1000,
    "1h": 60 * 60 * 1000,
    "6h": 6 * 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
    "7d": 7 * 24 * 60 * 60 * 1000,
    "30d": 30 * 24 * 60 * 60 * 1000,
}

// An agent without the passthrough RPC answers 422 STARLINK_AGENT_OUTDATED;
// retrying that only delays the fallback UI.
function retryUnlessOutdated(count: number, err: unknown) {
    if (err instanceof ApiError && err.code === "STARLINK_AGENT_OUTDATED") return false
    return count < 2
}

export function useStarlinkStatus(nodeId: number, enabled: boolean, intervalMs = 10_000) {
    return useQuery({
        queryKey: queryKeys.starlinkStatus(nodeId),
        queryFn: async () => {
            const res = await getStarlinkStatus(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch starlink status")
            return res.data!
        },
        enabled,
        refetchInterval: intervalMs,
        staleTime: Math.min(5_000, intervalMs - 500),
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

export function useStarlinkLive(nodeId: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.starlinkLive(nodeId),
        queryFn: async () => {
            const res = await getStarlinkLive(nodeId, 5)
            if (!res.success) throw new Error(res.error || "Failed to fetch live dish history")
            return res.data!
        },
        enabled,
        refetchInterval: 15_000,
        staleTime: 10_000,
        retry: retryUnlessOutdated,
    })
}

export function useStarlinkDevice(nodeId: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.starlinkDevice(nodeId),
        queryFn: async () => {
            const res = await getStarlinkDevice(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to fetch dish device info")
            return res.data!
        },
        enabled,
        refetchInterval: 60_000,
        staleTime: 55_000,
        retry: retryUnlessOutdated,
    })
}

export function useStarlinkEvents(nodeId: number, enabled: boolean, range: "24h" | "7d" | "30d") {
    return useQuery({
        queryKey: queryKeys.starlinkEvents(nodeId, range),
        queryFn: async () => {
            const since = new Date(Date.now() - TIME_RANGE_MS[range]).toISOString()
            const res = await getStarlinkEvents(nodeId, since)
            if (!res.success) throw new Error(res.error || "Failed to fetch dish events")
            return res.data!
        },
        enabled,
        refetchInterval: 60_000,
        staleTime: 55_000,
    })
}

export function useStarlinkAction(nodeId: number) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ action, confirm }: { action: StarlinkAction; confirm?: string }) => {
            const res = await runStarlinkAction(nodeId, action, confirm)
            if (!res.success) throw new Error(res.error || "Dish action failed")
            return res.data!
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: queryKeys.starlinkStatus(nodeId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.starlinkDevice(nodeId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.starlinkLive(nodeId) })
            queryClient.invalidateQueries({ queryKey: queryKeys.starlinkMap(nodeId) })
        },
    })
}
