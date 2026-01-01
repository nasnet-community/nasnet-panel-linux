import { useQuery } from "@tanstack/react-query"
import { getSubMaintenanceStatus, type MaintenanceStatus } from "@/lib/api/maintenance"

export function useMaintenanceStatus(uuid: string | undefined) {
  return useQuery<MaintenanceStatus | null>({
    queryKey: ["maintenance", "sub", uuid],
    queryFn: async () => {
      if (!uuid) return null
      const r = await getSubMaintenanceStatus(uuid)
      if (!r.success || !r.data) return null
      return r.data
    },
    enabled: !!uuid,
    staleTime: 10_000,
    refetchOnWindowFocus: true,
  })
}
