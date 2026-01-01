import { useQuery, keepPreviousData } from "@tanstack/react-query"
import { queryKeys } from "./keys"
import {
    getAggregatedAccessLogs,
    getAccessLogAnalytics,
    getAccessLogTopDomains,
    type AggregatedAccessLogsParams,
    type AccessLogAnalyticsParams,
} from "@/lib/admin-api"

export interface UseAccessLogsParams {
    nodeIds?: number[]
    email?: string
    limit?: number
    refreshInterval?: number // ms, 0 = off
}

export function useAggregatedAccessLogs(params: UseAccessLogsParams) {
    const apiParams: AggregatedAccessLogsParams = {
        node_ids: params.nodeIds?.length ? params.nodeIds : undefined,
        email: params.email || undefined,
        limit: params.limit || 500,
    }

    return useQuery({
        queryKey: queryKeys.accessLogList(apiParams),
        queryFn: async () => {
            const res = await getAggregatedAccessLogs(apiParams)
            if (!res.success) throw new Error(res.error || "Failed to fetch access logs")
            return res.data || []
        },
        refetchInterval: params.refreshInterval ?? 10_000,
        placeholderData: keepPreviousData,
    })
}

export function useAccessLogAnalytics(params: AccessLogAnalyticsParams & { enabled?: boolean }) {
    return useQuery({
        queryKey: ["accessLogAnalytics", params],
        queryFn: async () => {
            const res = await getAccessLogAnalytics(params)
            if (!res.success) throw new Error(res.error || "Failed to fetch analytics")
            return { data: res.data || [], total: (res as any).total || 0 }
        },
        enabled: params.enabled !== false,
    })
}

export function useAccessLogTopDomains(params: AccessLogAnalyticsParams & { top?: number; enabled?: boolean }) {
    return useQuery({
        queryKey: ["accessLogTopDomains", params],
        queryFn: async () => {
            const res = await getAccessLogTopDomains(params)
            if (!res.success) throw new Error(res.error || "Failed to fetch top domains")
            return res.data || []
        },
        enabled: params.enabled !== false,
    })
}
