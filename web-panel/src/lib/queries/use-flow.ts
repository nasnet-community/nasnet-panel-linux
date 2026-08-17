import { useMutation, useQuery } from "@tanstack/react-query"
import { getFlow, getFlowConns, getFlowEvents, postTrace } from "@/lib/api/flow"
import { queryKeys } from "@/lib/queries/keys"
import type { TraceRequest } from "@/lib/types/flow"

// Router mode off 404s every one of these, so a retry is just a slower failure.

export function useFlowGraph(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkFlow(),
        queryFn: async () => {
            const res = await getFlow()
            if (!res.success) throw new Error(res.error || "Failed to read the flow state")
            return res.data!
        },
        enabled,
        // Rates need consecutive samples, so this polls fast — but only while
        // the tab is actually in front of someone.
        refetchInterval: enabled ? 3 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 1500,
        retry: false,
    })
}

export function useFlowConns(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkFlowConns(),
        queryFn: async () => {
            const res = await getFlowConns()
            if (!res.success) throw new Error(res.error || "Failed to read the connections")
            return res.data!
        },
        enabled,
        refetchInterval: enabled ? 3 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 1500,
        retry: false,
    })
}

export function useFlowEvents(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkFlowEvents(),
        queryFn: async () => {
            const res = await getFlowEvents()
            if (!res.success) throw new Error(res.error || "Failed to read recent events")
            return res.data ?? []
        },
        enabled,
        refetchInterval: enabled ? 10 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 5000,
        retry: false,
    })
}

export function useTraceFlow() {
    return useMutation({
        mutationFn: async (req: TraceRequest) => {
            const res = await postTrace(req)
            if (!res.success) throw new Error(res.error || "The trace failed")
            return res.data!
        },
    })
}
