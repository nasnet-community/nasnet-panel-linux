import { useQuery } from "@tanstack/react-query"
import { getApiBaseUrl } from "@/lib/config"

const API_BASE_URL = getApiBaseUrl()

export interface IpGeoData {
    country: string
    country_code: string
    flag: string
    city: string
}

/** Resolve IPs via GET /api/v1/public/sub/:uuid/ip-geo (MaxMind GeoLite2-City). */
export function useIpGeolocation(uuid: string, ips?: string[]) {
    return useQuery({
        queryKey: ["ip-geo", uuid, ...(ips ?? []).slice().sort()],
        queryFn: async () => {
            const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}/ip-geo`, {
                credentials: "include",
            })
            if (!res.ok) return {} as Record<string, IpGeoData>
            const json = await res.json()
            if (!json.success) return {} as Record<string, IpGeoData>
            return (json.data ?? {}) as Record<string, IpGeoData>
        },
        staleTime: 10 * 60_000,
        gcTime: 30 * 60_000,
        enabled: !!ips && ips.length > 0,
    })
}
