import { useQuery } from "@tanstack/react-query"
import { queryKeys } from "./keys"
import {
    getSubscriptionAccessHistory,
    searchSubscriptionAccessHistory,
    searchGlobalAccessHistory,
} from "@/lib/admin-api"
import type {
    AccessHistoryParams,
    AccessHistorySearchParams,
    AccessHistoryGlobalSearchParams,
} from "@/lib/types"

export function useSubscriptionAccessHistory(
    subId: number | undefined,
    params: AccessHistoryParams,
    enabled: boolean,
) {
    const granularity = params.granularity ?? "auto"
    return useQuery({
        queryKey: queryKeys.subscriptionAccessHistory(
            subId ?? 0,
            params.from,
            params.to,
            granularity,
            !!params.include_ips,
        ),
        queryFn: async () => {
            if (!subId) throw new Error("subscription id missing")
            const res = await getSubscriptionAccessHistory(subId, params)
            if (!res.success) throw new Error(res.error || "Failed to fetch access history")
            return res.data!
        },
        enabled: enabled && !!subId && !!params.from && !!params.to,
        staleTime: 60_000,
        // History data is mostly static within a window; aggressive refetch
        // would just hammer the DB. The user has an explicit refresh button.
        refetchInterval: false,
        refetchOnWindowFocus: false,
    })
}

export function useSubscriptionAccessSearch(
    subId: number | undefined,
    params: AccessHistorySearchParams,
    enabled: boolean,
) {
    const kinds = params.kinds ?? []
    return useQuery({
        queryKey: queryKeys.subscriptionAccessSearch(
            subId ?? 0,
            params.from,
            params.to,
            params.q,
            kinds,
            !!params.include_ips,
        ),
        queryFn: async () => {
            if (!subId) throw new Error("subscription id missing")
            const res = await searchSubscriptionAccessHistory(subId, params)
            if (!res.success) throw new Error(res.error || "Failed to search access history")
            return res.data!
        },
        // Require ≥2 chars before firing — server enforces it but we don't
        // want to waste a network round-trip on the typing state either.
        enabled:
            enabled &&
            !!subId &&
            !!params.from &&
            !!params.to &&
            (params.q?.trim().length ?? 0) >= 2,
        staleTime: 30_000,
        refetchInterval: false,
        refetchOnWindowFocus: false,
    })
}

export function useGlobalAccessHistorySearch(
    params: AccessHistoryGlobalSearchParams,
    enabled: boolean,
) {
    const kinds = params.kinds ?? []
    const nodeIds = params.node_ids ?? []
    const subscriptionIds = params.subscription_ids ?? []
    const emails = params.emails ?? []
    return useQuery({
        queryKey: queryKeys.accessHistoryGlobalSearch(
            params.from,
            params.to,
            params.q,
            kinds,
            nodeIds,
            subscriptionIds,
            emails,
            !!params.include_ips,
            params.limit ?? 0,
        ),
        queryFn: async () => {
            const res = await searchGlobalAccessHistory(params)
            if (!res.success) throw new Error(res.error || "Failed to search access history")
            return res.data!
        },
        enabled:
            enabled &&
            !!params.from &&
            !!params.to &&
            (params.q?.trim().length ?? 0) >= 2,
        // Slightly longer cache than per-sub: global queries cost more to
        // re-run, but the data answers an exploratory question — admins
        // typically iterate quickly on the same window.
        staleTime: 60_000,
        refetchInterval: false,
        refetchOnWindowFocus: false,
    })
}
