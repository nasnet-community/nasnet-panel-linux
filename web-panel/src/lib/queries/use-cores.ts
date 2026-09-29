import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "@/lib/toast"
import {
    listCoreInbounds,
    listAllCoreInbounds,
    createCoreInbound,
    updateCoreInbound,
    deleteCoreInbound,
    getCoreInboundStatus,
    listCoreUsers,
    createCoreUser,
    updateCoreUser,
    toggleCoreUser,
    deleteCoreUser,
    getCoreUserConfig,
    listNodeProtocols,
    toggleNodeProtocol,
} from "@/lib/api/cores"
import type { QueryClient } from "@tanstack/react-query"
import type {
    CoreInboundInput,
    CoreUserInput,
    CoreUserUpdateInput,
} from "@/lib/types/cores"
import { probeNodeDeps } from "@/lib/api/core-deps"
import { queryKeys } from "./keys"

/** Toast a non-empty reconcile_warning (the DB change committed; the agent push failed). */
function surfaceReconcileWarning(warning?: string) {
    if (warning && warning.trim() !== "") {
        // A missing binary is fixable from the Dependencies section of the
        // node's Tunnels tab — point the operator there instead of a dead end.
        const missingDep = /not installed on this node|executable file not found/i.test(warning)
        const detail = warning.trim().replace(/\.$/, "")
        toast.warning("Saved, but the node push failed", {
            description: missingDep ? `${detail}. Install it under Dependencies on the Tunnels tab.` : detail,
        })
    }
}

/**
 * Invalidate a node's tunnel list AND the cross-node overview, so an edit made
 * on either surface refreshes the other. The overview uses a distinct
 * coreAllInbounds() key that a node-scoped invalidation would otherwise miss.
 */
function invalidateInboundLists(qc: QueryClient, nodeId?: number) {
    if (nodeId) qc.invalidateQueries({ queryKey: queryKeys.coreInbounds(nodeId) })
    qc.invalidateQueries({ queryKey: queryKeys.coreAllInbounds() })
}

// Queries --------------------------------------------------------------------

export function useCoreInbounds(nodeId: number | undefined) {
    return useQuery({
        queryKey: queryKeys.coreInbounds(nodeId ?? 0),
        enabled: !!nodeId,
        queryFn: async () => {
            const res = await listCoreInbounds(nodeId as number)
            if (!res.success) throw new Error(res.error || "Failed to load tunnels")
            return res.data?.inbounds ?? []
        },
        staleTime: 30 * 1000,
    })
}

/** Every core inbound across all nodes — the cross-node "All Tunnels" overview. */
export function useAllCoreInbounds() {
    return useQuery({
        queryKey: queryKeys.coreAllInbounds(),
        queryFn: async () => {
            const res = await listAllCoreInbounds()
            if (!res.success) throw new Error(res.error || "Failed to load tunnels")
            return res.data?.inbounds ?? []
        },
        staleTime: 30 * 1000,
    })
}

export function useCoreUsers(inboundId: number | undefined) {
    return useQuery({
        queryKey: queryKeys.coreUsers(inboundId ?? 0),
        enabled: !!inboundId,
        queryFn: async () => {
            const res = await listCoreUsers(inboundId as number)
            if (!res.success) throw new Error(res.error || "Failed to load users")
            return res.data?.users ?? []
        },
        staleTime: 30 * 1000,
    })
}

/** Live core status — enabled only when the node is online. Best-effort. */
export function useCoreInboundStatus(inboundId: number | undefined, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.coreInboundStatus(inboundId ?? 0),
        enabled: enabled && !!inboundId,
        retry: false,
        staleTime: 10 * 1000,
        refetchInterval: 20 * 1000,
        queryFn: async () => {
            const res = await getCoreInboundStatus(inboundId as number)
            if (!res.success) throw new Error(res.error || "Status unavailable")
            return res.data!
        },
    })
}

/** Lazy config fetch — only runs when explicitly enabled (on demand). */
export function useCoreUserConfig(
    userId: number | undefined,
    host: string | undefined,
    enabled: boolean,
) {
    return useQuery({
        queryKey: queryKeys.coreUserConfig(userId ?? 0, host),
        enabled: enabled && !!userId,
        queryFn: async () => {
            const res = await getCoreUserConfig(userId as number, host)
            if (!res.success) throw new Error(res.error || "Failed to fetch client config")
            return res.data!
        },
        staleTime: 0,
        gcTime: 0,
    })
}

// Per-node protocol enable/disable -------------------------------------------

/** Every core protocol's enable state + inbound count on a node (all 9). */
export function useNodeProtocols(nodeId: number | undefined) {
    return useQuery({
        queryKey: queryKeys.coreNodeProtocols(nodeId ?? 0),
        enabled: !!nodeId,
        queryFn: async () => {
            const res = await listNodeProtocols(nodeId as number)
            if (!res.success) throw new Error(res.error || "Failed to load protocols")
            return res.data?.protocols ?? []
        },
        staleTime: 30 * 1000,
    })
}

/** Flip a protocol's per-node enable flag; reconciles the node on the server. */
export function useToggleNodeProtocol(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (protocol: string) => {
            const res = await toggleNodeProtocol(nodeId, protocol)
            if (!res.success) throw new Error(res.error || "Failed to toggle protocol")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(data.enabled ? "Protocol enabled" : "Protocol disabled")
            surfaceReconcileWarning(data.reconcile_warning)
            qc.invalidateQueries({ queryKey: queryKeys.coreNodeProtocols(nodeId) })
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// Inbound mutations ----------------------------------------------------------

export function useCreateCoreInbound(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (body: CoreInboundInput) => {
            const res = await createCoreInbound(body)
            if (!res.success) throw new Error(res.error || "Failed to create tunnel")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("Tunnel created")
            surfaceReconcileWarning(data.reconcile_warning)
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateCoreInbound(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, body }: { id: number; body: CoreInboundInput }) => {
            const res = await updateCoreInbound(id, body)
            if (!res.success) throw new Error(res.error || "Failed to update tunnel")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("Tunnel updated")
            surfaceReconcileWarning(data.reconcile_warning)
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteCoreInbound(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteCoreInbound(id)
            if (!res.success) throw new Error(res.error || "Failed to delete tunnel")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("Tunnel deleted")
            surfaceReconcileWarning(data.reconcile_warning)
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

// User mutations -------------------------------------------------------------

export function useCreateCoreUser(inboundId: number, nodeId?: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (body: CoreUserInput) => {
            const res = await createCoreUser(inboundId, body)
            if (!res.success) throw new Error(res.error || "Failed to add user")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("User added")
            surfaceReconcileWarning(data.reconcile_warning)
            qc.invalidateQueries({ queryKey: queryKeys.coreUsers(inboundId) })
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateCoreUser(inboundId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, body }: { id: number; body: CoreUserUpdateInput }) => {
            const res = await updateCoreUser(id, body)
            if (!res.success) throw new Error(res.error || "Failed to update user")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("User updated")
            surfaceReconcileWarning(data.reconcile_warning)
            qc.invalidateQueries({ queryKey: queryKeys.coreUsers(inboundId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useToggleCoreUser(inboundId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await toggleCoreUser(id)
            if (!res.success) throw new Error(res.error || "Failed to toggle user")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(data.user.enabled ? "User enabled" : "User disabled")
            surfaceReconcileWarning(data.reconcile_warning)
            qc.invalidateQueries({ queryKey: queryKeys.coreUsers(inboundId) })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteCoreUser(inboundId: number, nodeId?: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteCoreUser(id)
            if (!res.success) throw new Error(res.error || "Failed to delete user")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success("User deleted")
            surfaceReconcileWarning(data.reconcile_warning)
            qc.invalidateQueries({ queryKey: queryKeys.coreUsers(inboundId) })
            invalidateInboundLists(qc, nodeId)
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

/** What the node has installed for each protocol (the Dependencies section). */
export function useNodeDeps(nodeId: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.coreNodeDeps(nodeId),
        queryFn: async () => {
            const res = await probeNodeDeps(nodeId)
            if (!res.success) throw new Error(res.error || "Unable to check dependencies")
            return res.data?.reports ?? []
        },
        enabled,
        staleTime: 30_000,
    })
}
