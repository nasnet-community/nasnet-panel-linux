import { useQuery } from "@tanstack/react-query"
import { getPeakHours, getBlockedDomainStats, getUserUsagePattern, getExhaustionPrediction } from "@/lib/admin-api"
import { queryKeys } from "./keys"

export function usePeakHours(days = 7, nodeIds?: number[]) {
    return useQuery({
        queryKey: queryKeys.peakHours(days, nodeIds),
        queryFn: async () => {
            const res = await getPeakHours(days, nodeIds)
            if (!res.success) throw new Error(res.error || "Failed to fetch peak hours")
            return res.data!
        },
        staleTime: 5 * 60 * 1000,
    })
}

export function useBlockedDomainStats(params: { days?: number; nodeIds?: number[]; top?: number } = {}) {
    return useQuery({
        queryKey: queryKeys.blockedDomains(params),
        queryFn: async () => {
            const res = await getBlockedDomainStats(params)
            if (!res.success) throw new Error(res.error || "Failed to fetch blocked domain stats")
            return res.data!
        },
        staleTime: 5 * 60 * 1000,
    })
}

export function useUserUsagePattern(userId: number, days = 7) {
    return useQuery({
        queryKey: queryKeys.userUsagePattern(userId, days),
        queryFn: async () => {
            const res = await getUserUsagePattern(userId, days)
            if (!res.success) throw new Error(res.error || "Failed to fetch usage pattern")
            return res.data!
        },
        staleTime: 5 * 60 * 1000,
        enabled: userId > 0,
    })
}

export function useExhaustionPrediction(subId: number, enabled = true) {
    return useQuery({
        queryKey: queryKeys.exhaustionPrediction(subId),
        queryFn: async () => {
            const res = await getExhaustionPrediction(subId)
            if (!res.success) throw new Error(res.error || "Failed to fetch exhaustion prediction")
            return res.data!
        },
        staleTime: 60 * 1000,
        enabled: enabled && subId > 0,
    })
}
