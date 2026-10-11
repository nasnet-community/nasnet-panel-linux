import { useCallback } from "react"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "@/lib/toast"
import type { ApiResponse } from "@/lib/api"
import { updateNode } from "@/lib/api/nodes"
import {
    checkMtprotoDisguise,
    getMtprotoAllowlist,
    getMtprotoLogs,
    getMtprotoEvents,
    getMtprotoNode,
    listMtprotoNodes,
    refreshMtprotoAllowlist,
    restartMtprotoTelemt,
    setMtprotoProxyStopped,
    updateMtprotoSettings,
    type MtprotoSettingsBody,
} from "@/lib/api/mtproto"
import { queryKeys } from "./keys"

function unwrap<T>(res: ApiResponse<T>, fallback: string): T {
    if (!res.success || res.data === undefined) throw new Error(res.error || fallback)
    return res.data
}

/** An MTProto node's live state: link, agent, telemt. Polls while shown. */
export function useMtprotoNode(nodeId: number, enabled = true) {
    return useQuery({
        queryKey: queryKeys.mtprotoNode(nodeId),
        queryFn: async () => unwrap(await getMtprotoNode(nodeId), "Could not load the MTProto node"),
        enabled: enabled && nodeId > 0,
        refetchInterval: 10_000,
        staleTime: 5_000,
    })
}

/** Every MTProto node's headline numbers, for the nodes list. */
export function useMtprotoNodes(enabled = true) {
    return useQuery({
        queryKey: queryKeys.mtprotoNodes(),
        queryFn: async () => unwrap(await listMtprotoNodes(), "Could not load the MTProto nodes") ?? [],
        enabled,
        refetchInterval: 10_000,
        staleTime: 5_000,
    })
}

/** What happened on a node since the panel started. */
export function useMtprotoEvents(nodeId: number, limit = 50, enabled = true) {
    return useQuery({
        queryKey: [...queryKeys.mtprotoEvents(nodeId), limit],
        queryFn: async () => unwrap(await getMtprotoEvents(nodeId, limit), "Could not load the node's activity"),
        enabled: enabled && nodeId > 0,
        refetchInterval: 15_000,
        staleTime: 5_000,
    })
}

/** Saves the stream limit or the SSH login the panel uses for repairs. */
export function useUpdateMtprotoSettings(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (body: MtprotoSettingsBody) => unwrap(await updateMtprotoSettings(nodeId, body), "Could not save the settings"),
        onSuccess: (data) => {
            toast.success("Settings saved")
            qc.setQueryData(queryKeys.mtprotoNode(nodeId), data)
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNode(nodeId) })
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNodes() })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

/** The agent keeps its log in memory only; this fetches the tail on demand. */
export function useMtprotoLogs(nodeId: number, lines: number, enabled: boolean) {
    return useQuery({
        queryKey: queryKeys.mtprotoLogs(nodeId, lines),
        queryFn: async () => unwrap(await getMtprotoLogs(nodeId, lines), "Could not read the agent's log").lines ?? [],
        enabled: enabled && nodeId > 0,
        // changing the line count keeps the old lines up until the new ones arrive
        placeholderData: keepPreviousData,
        staleTime: 0,
        gcTime: 0,
        retry: false,
    })
}

export function useMtprotoAllowlist() {
    return useQuery({
        queryKey: queryKeys.mtprotoAllowlist(),
        queryFn: async () => unwrap(await getMtprotoAllowlist(), "Could not load the address list"),
        staleTime: 60_000,
    })
}

export function useRefreshMtprotoAllowlist() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async () => unwrap(await refreshMtprotoAllowlist(), "Could not refresh the address list"),
        onSuccess: () => {
            toast.success("Address list refreshed")
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoAllowlist() })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useRestartMtprotoTelemt(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async () => unwrap(await restartMtprotoTelemt(nodeId), "Could not restart the proxy"),
        onSuccess: () => {
            toast.success("Proxy restarted")
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNode(nodeId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useSetMtprotoProxyStopped(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (stopped: boolean) =>
            unwrap(await setMtprotoProxyStopped(nodeId, stopped), stopped ? "Could not stop the proxy" : "Could not start the proxy"),
        onSuccess: (view, stopped) => {
            toast.success(stopped ? "Proxy stopped" : "Proxy starting")
            qc.setQueryData(queryKeys.mtprotoNode(nodeId), view)
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNode(nodeId) })
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNodes() })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

/** The details show inline; the toast gives the verdict. */
export function useCheckMtprotoDisguise(nodeId: number) {
    return useMutation({
        mutationFn: async (domain: string) => unwrap(await checkMtprotoDisguise(nodeId, domain), "Could not check that domain"),
        onSuccess: (res, domain) => {
            if (res.ok) toast.success(`${domain} works as a cover site`)
            else toast.warning(`${domain} can't be the cover site`, { description: res.err || "The check failed" })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export type MtprotoNodeChange = { name: string } | { is_active: boolean }

/** Renames or enables/disables an MTProto node through the generic node update. */
export function useUpdateMtprotoNodeSettings(nodeId: number) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async (data: MtprotoNodeChange) => unwrap(await updateNode(nodeId, data), "Could not update the MTProto node"),
        onSuccess: (_, data) => {
            if ("name" in data) toast.success("MTProto node renamed")
            else toast.success(data.is_active ? "MTProto node is back in service" : "MTProto node taken out of service")
            qc.invalidateQueries({ queryKey: queryKeys.nodeDetails(nodeId) })
            qc.invalidateQueries({ queryKey: queryKeys.nodeList() })
            qc.invalidateQueries({ queryKey: queryKeys.mtprotoNode(nodeId) })
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

/** Refetches the node list, e.g. after an MTProto node is removed. */
export function useRefreshNodes() {
    const qc = useQueryClient()
    return useCallback(() => {
        void qc.invalidateQueries({ queryKey: queryKeys.nodes })
    }, [qc])
}
