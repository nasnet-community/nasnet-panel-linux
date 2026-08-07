import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
    applyNetworkChange,
    confirmNetworkApply,
    getNetworkInterfaces,
    getNetworkState,
    planNetworkChange,
    rollbackNetworkApply,
} from "@/lib/api/network"
import { queryKeys } from "@/lib/queries/keys"
import type { AssignRoleRequest } from "@/lib/types/network"

/** Router mode off 404s every route; not worth retrying, so the page can hide. */
export function useNetworkInterfaces(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkInterfaces(),
        queryFn: async () => {
            const res = await getNetworkInterfaces()
            if (!res.success) throw new Error(res.error || "Failed to fetch interfaces")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

export function useNetworkState(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkState(),
        queryFn: async () => {
            const res = await getNetworkState()
            if (!res.success) throw new Error(res.error || "Failed to fetch network state")
            return res.data!
        },
        enabled,
        staleTime: 5 * 1000,
        refetchInterval: 15 * 1000,
        retry: false,
    })
}

export function usePlanNetworkChange() {
    return useMutation({
        mutationFn: async (req: AssignRoleRequest) => {
            const res = await planNetworkChange(req)
            if (!res.success) throw new Error(res.error || "Failed to plan the change")
            return res.data!
        },
    })
}

export function useApplyNetworkChange() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (req: AssignRoleRequest) => {
            const res = await applyNetworkChange(req)
            if (!res.success) throw new Error(res.error || "Failed to apply the change")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useConfirmNetworkApply() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (planId: number) => {
            const res = await confirmNetworkApply(planId)
            if (!res.success) throw new Error(res.error || "Failed to confirm")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function useRollbackNetworkApply() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await rollbackNetworkApply()
            if (!res.success) throw new Error(res.error || "Failed to roll back")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}
