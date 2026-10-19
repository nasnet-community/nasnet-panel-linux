import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listReverseTunnels,
    createReverseTunnel,
    updateReverseTunnel,
    deleteReverseTunnel,
    rotateReverseTunnel,
    getReverseTunnelPeerConfig,
} from "@/lib/api/routing"
import type { ReverseTunnel, ReverseTunnelInput, ReverseTunnelPeerConfig } from "@/lib/types"
import { queryKeys } from "./keys"
import { toast } from "@/lib/toast"

// Tunnels where the node is either end. Status changes arrive as events; the
// poll keeps traffic current while the tab is open.
export function useReverseTunnels(nodeId: number) {
    return useQuery<ReverseTunnel[]>({
        queryKey: queryKeys.nodeReverseTunnels(nodeId),
        queryFn: async () => {
            const res = await listReverseTunnels(nodeId)
            if (!res.success) throw new Error(res.error || "Failed to load reverse tunnels")
            return res.data || []
        },
        enabled: nodeId > 0,
        refetchInterval: 30_000,
    })
}

export function useReverseTunnelPeerConfig(id: number | null) {
    return useQuery<ReverseTunnelPeerConfig>({
        queryKey: queryKeys.reverseTunnelPeer(id ?? 0),
        queryFn: async () => {
            const res = await getReverseTunnelPeerConfig(id!)
            if (!res.success) throw new Error(res.error || "Failed to load the peer config")
            return res.data!
        },
        enabled: id != null && id > 0,
        staleTime: 0,
    })
}

// A tunnel spans two nodes and may own a routing rule on its portal, so a
// change refreshes every node's tunnels and routing.
function useTunnelInvalidation() {
    const queryClient = useQueryClient()
    return () => {
        queryClient.invalidateQueries({ queryKey: queryKeys.reverseTunnels() })
        queryClient.invalidateQueries({ queryKey: [...queryKeys.nodes, 'routing'] })
    }
}

export function useCreateReverseTunnel() {
    const invalidate = useTunnelInvalidation()
    return useMutation({
        mutationFn: async (data: ReverseTunnelInput) => {
            const res = await createReverseTunnel(data)
            if (!res.success) throw new Error(res.error || "Failed to create the reverse tunnel")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Reverse tunnel created")
            invalidate()
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useUpdateReverseTunnel() {
    const invalidate = useTunnelInvalidation()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: ReverseTunnelInput }) => {
            const res = await updateReverseTunnel(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update the reverse tunnel")
            return res.data!
        },
        onSuccess: () => {
            toast.success("Reverse tunnel updated")
            invalidate()
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useDeleteReverseTunnel() {
    const invalidate = useTunnelInvalidation()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteReverseTunnel(id)
            if (!res.success) throw new Error(res.error || "Failed to delete the reverse tunnel")
        },
        onSuccess: () => {
            toast.success("Reverse tunnel deleted")
            invalidate()
        },
        onError: (error: Error) => toast.error(error.message),
    })
}

export function useRotateReverseTunnel() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await rotateReverseTunnel(id)
            if (!res.success) throw new Error(res.error || "Failed to rotate the tunnel's id")
        },
        onSuccess: () => {
            toast.success("Tunnel id rotated")
            queryClient.invalidateQueries({ queryKey: queryKeys.reverseTunnels() })
        },
        onError: (error: Error) => toast.error(error.message),
    })
}
