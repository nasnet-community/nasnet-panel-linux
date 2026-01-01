import { useQuery } from "@tanstack/react-query"
import type { SubPanelData } from "@/lib/types/sub-panel"
import { getApiBaseUrl } from '@/lib/config'

const API_BASE_URL = getApiBaseUrl()

export class AuthRequiredError extends Error {
    label: string
    constructor(label: string) {
        super("Password required")
        this.name = "AuthRequiredError"
        this.label = label
    }
}

export class SubPanelNotFoundError extends Error {
    constructor() {
        super("Subscription not found")
        this.name = "SubPanelNotFoundError"
    }
}

export async function fetchSubPanel(uuid: string): Promise<SubPanelData> {
    const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}`, {
        credentials: "include",
    })
    if (res.status === 403) {
        const json = await res.json().catch(() => null)
        if (json?.data?.auth_required) {
            throw new AuthRequiredError(json.data.label || "Subscription")
        }
    }
    if (res.status === 404) {
        throw new SubPanelNotFoundError()
    }
    if (!res.ok) {
        throw new Error(`Failed to load subscription (HTTP ${res.status})`)
    }
    const json = await res.json()
    if (!json.success) {
        throw new Error(json.error || "Failed to load subscription")
    }
    return json.data as SubPanelData
}

export function useSubPanel(uuid: string) {
    return useQuery({
        queryKey: ["sub-panel", uuid],
        queryFn: () => fetchSubPanel(uuid),
        retry: (count, error) => {
            if (error instanceof AuthRequiredError) return false
            if (error instanceof SubPanelNotFoundError) return false
            return count < 2
        },
        staleTime: 30_000,
    })
}
