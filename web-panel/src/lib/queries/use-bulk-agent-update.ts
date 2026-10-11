import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { bulkUpdateAgent, getAgentUpdates } from "@/lib/api/nodes"
import { queryKeys } from "./keys"

/** Which remote full nodes run another agent than the panel ships, by node id. */
export function useAgentUpdates(enabled = true) {
    return useQuery({
        queryKey: queryKeys.agentUpdates(),
        queryFn: async () => {
            const res = await getAgentUpdates()
            if (!res.success || !res.data) throw new Error(res.error || "Failed to check for agent updates")
            return res.data
        },
        enabled,
        staleTime: 15_000,
    })
}

export function useBulkUpdateAgent() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (ids: number[]) => {
            const res = await bulkUpdateAgent(ids)
            if (!res.success || !res.data) throw new Error(res.error || "Bulk agent update failed")
            return res.data
        },
        retry: false,
        // Fail visibly offline instead of queuing an agent restart until a
        // later reconnect while the confirmation dialog remains locked.
        networkMode: "always",
        // Even a lost response can follow an accepted update. Refresh status
        // in either case, and leave errors visible in the dialog for review.
        onSettled: (_data, _error, ids) => {
            void queryClient.invalidateQueries({ queryKey: queryKeys.nodeList() })
            void queryClient.invalidateQueries({ queryKey: queryKeys.nodeStatsBulkAll() })
            void queryClient.invalidateQueries({ queryKey: queryKeys.agentUpdates() })
            for (const id of ids) {
                void queryClient.invalidateQueries({ queryKey: queryKeys.nodeOverview(id) })
                void queryClient.invalidateQueries({ queryKey: queryKeys.nodeStats(id) })
                void queryClient.invalidateQueries({ queryKey: queryKeys.nodeDetails(id) })
            }
        },
    })
}
