import { useQuery } from "@tanstack/react-query"
import type { UsageTrendRange, UsageTrendResponse } from "@/lib/types/sub-panel"
import { getApiBaseUrl } from "@/lib/config"

const API_BASE_URL = getApiBaseUrl()

async function fetchUsageTrend(uuid: string, range: UsageTrendRange): Promise<UsageTrendResponse> {
  const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}/usage-trend?range=${range}`, {
    credentials: "include",
  })
  if (!res.ok) {
    throw new Error(`usage-trend request failed: ${res.status}`)
  }
  const json = await res.json()
  if (!json.success) {
    throw new Error(json.error || "usage-trend request failed")
  }
  return json.data as UsageTrendResponse
}

export function useUsageTrend(uuid: string, range: UsageTrendRange) {
  return useQuery({
    queryKey: ["usage-trend", uuid, range],
    queryFn: () => fetchUsageTrend(uuid, range),
    staleTime: 60_000,
    enabled: Boolean(uuid),
  })
}
