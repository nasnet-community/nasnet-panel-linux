import { useMutation, useQueryClient } from "@tanstack/react-query"
import { bulkUpdateAgent } from "@/lib/api/nodes"
import { queryKeys } from "./keys"

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
            for (const id of ids) {
                void queryClient.invalidateQueries({ queryKey: queryKeys.nodeStats(id) })
                void queryClient.invalidateQueries({ queryKey: queryKeys.nodeDetails(id) })
            }
        },
    })
}
