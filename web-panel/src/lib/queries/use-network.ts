import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
    applyNetworkChange,
    confirmNetworkApply,
    createPortForward,
    deletePortForward,
    getLAN,
    getNetworkInterfaces,
    getNetworkState,
    getPortForwards,
    planNetworkChange,
    rollbackNetworkApply,
    updateLAN,
    updatePortForward,
    type PortForwardInput,
} from "@/lib/api/network"
import { queryKeys } from "@/lib/queries/keys"
import type { AssignRoleRequest, LANConfig } from "@/lib/types/network"

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

export function useLAN(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkLAN(),
        queryFn: async () => {
            const res = await getLAN()
            if (!res.success) throw new Error(res.error || "Failed to fetch the LAN config")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

/** Enabling the LAN goes through the two-phase apply, so this returns a plan id
 *  and a confirm deadline rather than taking effect. */
export function useUpdateLAN() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (cfg: Partial<LANConfig>) => {
            const res = await updateLAN(cfg)
            if (!res.success) throw new Error(res.error || "Failed to update the LAN")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.network })
        },
    })
}

export function usePortForwards(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkPortForwards(),
        queryFn: async () => {
            const res = await getPortForwards()
            if (!res.success) throw new Error(res.error || "Failed to fetch port forwards")
            return res.data!
        },
        enabled,
        staleTime: 10 * 1000,
        retry: false,
    })
}

/** Forwards touch only the nft table, not addressing, so they apply at once —
 *  no dead-man countdown, unlike the LAN. */
export function useSavePortForward() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (pf: PortForwardInput) => {
            const res = pf.id ? await updatePortForward(pf.id, pf) : await createPortForward(pf)
            if (!res.success) throw new Error(res.error || "Failed to save the forward")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkPortForwards() })
        },
    })
}

export function useDeletePortForward() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deletePortForward(id)
            if (!res.success) throw new Error(res.error || "Failed to delete the forward")
            return res.data
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkPortForwards() })
        },
    })
}
