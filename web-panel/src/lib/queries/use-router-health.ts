import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { getRouterHealth, setUplinkForce } from "@/lib/api/network"
import { queryKeys } from "@/lib/queries/keys"

// Router mode off 404s this, so a retry is just a slower failure.
export function useRouterHealth(enabled = true) {
    return useQuery({
        queryKey: queryKeys.networkHealth(),
        queryFn: async () => {
            const res = await getRouterHealth()
            if (!res.success) throw new Error(res.error || "Failed to read uplink health")
            return res.data!
        },
        enabled,
        // The probe ticks every 5s; polling faster re-reads the same tick.
        refetchInterval: enabled ? 5 * 1000 : false,
        refetchIntervalInBackground: false,
        staleTime: 2500,
        retry: false,
    })
}

export function useSetUplinkForce() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ ifName, state }: { ifName: string; state: "" | "up" | "down" }) => {
            const res = await setUplinkForce(ifName, state)
            if (!res.success) throw new Error(res.error || "Failed to set the force state")
            return res.data!
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: queryKeys.networkHealth() })
            void qc.invalidateQueries({ queryKey: queryKeys.networkState() })
        },
    })
}
