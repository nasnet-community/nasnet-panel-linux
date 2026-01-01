import { useQuery } from "@tanstack/react-query"
import type { SubPanelExhaustionPrediction, SubPanelHourlyUsagePoint } from "@/lib/types/sub-panel"
import { getApiBaseUrl } from '@/lib/config'

const API_BASE_URL = getApiBaseUrl()

export function useSubExhaustionPrediction(uuid: string) {
    return useQuery({
        queryKey: ["sub-panel", uuid, "exhaustion"],
        queryFn: async () => {
            const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}/exhaustion-prediction`)
            if (!res.ok) throw new Error("Failed to fetch prediction")
            const json = await res.json()
            if (!json.success) throw new Error(json.error || "Failed to fetch prediction")
            return json.data as SubPanelExhaustionPrediction
        },
        staleTime: 60_000,
        enabled: uuid.length > 0,
    })
}

export function useSubUsagePattern(uuid: string) {
    return useQuery({
        queryKey: ["sub-panel", uuid, "usage-pattern"],
        queryFn: async () => {
            const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}/usage-pattern`)
            if (!res.ok) throw new Error("Failed to fetch usage pattern")
            const json = await res.json()
            if (!json.success) throw new Error(json.error || "Failed to fetch usage pattern")
            return json.data as SubPanelHourlyUsagePoint[]
        },
        staleTime: 5 * 60_000,
        enabled: uuid.length > 0,
    })
}
