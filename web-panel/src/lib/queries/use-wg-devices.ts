import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { getApiBaseUrl } from "@/lib/config"
import { AuthRequiredError } from "@/lib/queries/use-sub-panel"
import type { WgDevice, WgDevicesResponse, WgServerOption } from "@/lib/types/sub-panel"

const API_BASE_URL = getApiBaseUrl()

export class ApiError extends Error {
    status: number
    constructor(message: string, status: number) {
        super(message)
        this.name = "ApiError"
        this.status = status
    }
}

// panelRequest hits the public sub-panel API with the same cookie-based auth as
// the rest of the panel. A 403 with auth_required surfaces as AuthRequiredError
// (consistent with useSubPanel) so the page can show the password gate.
async function panelRequest<T>(uuid: string, path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}${path}`, {
        credentials: "include",
        headers: init?.body ? { "Content-Type": "application/json" } : undefined,
        ...init,
    })
    if (res.status === 403) {
        const json = await res.json().catch(() => null)
        if (json?.data?.auth_required) {
            throw new AuthRequiredError(json.data.label || "Subscription")
        }
    }
    const json = await res.json().catch(() => null)
    if (!res.ok || !json?.success) {
        throw new ApiError(json?.error || `HTTP ${res.status}`, res.status)
    }
    return json.data as T
}

const noAuthRetry = (count: number, error: unknown) =>
    !(error instanceof AuthRequiredError) && count < 2

export function useWgServers(uuid: string, enabled: boolean) {
    return useQuery({
        queryKey: ["wg-servers", uuid],
        queryFn: () => panelRequest<WgServerOption[]>(uuid, "/wg/servers"),
        enabled,
        retry: noAuthRetry,
        staleTime: 60_000,
    })
}

export function useWgDevices(uuid: string, enabled: boolean) {
    return useQuery({
        queryKey: ["wg-devices", uuid],
        queryFn: () => panelRequest<WgDevicesResponse>(uuid, "/devices"),
        enabled,
        retry: noAuthRetry,
        staleTime: 15_000,
    })
}

export function useAddWgDevice(uuid: string) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: (body: { inbound_id?: number; host_id?: number; label?: string }) =>
            panelRequest<{ device: WgDevice; config: string }>(uuid, "/devices", {
                method: "POST",
                body: JSON.stringify(body),
            }),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["wg-devices", uuid] }),
    })
}

// useWgDeviceConfig re-fetches an existing device's .conf on demand (lost the
// file, new phone). A mutation rather than a query: it's user-triggered and the
// config shouldn't sit in the query cache.
export function useWgDeviceConfig(uuid: string) {
    return useMutation({
        mutationFn: (deviceId: number) =>
            panelRequest<{ device: WgDevice; config: string }>(uuid, `/devices/${deviceId}/config`),
    })
}

export function useRotateWgDevice(uuid: string) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: (deviceId: number) =>
            panelRequest<{ device: WgDevice; config: string }>(uuid, `/devices/${deviceId}/rotate`, {
                method: "POST",
                body: "{}",
            }),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["wg-devices", uuid] }),
    })
}

export function useRemoveWgDevice(uuid: string) {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: (deviceId: number) =>
            panelRequest<{ removed: boolean }>(uuid, `/devices/${deviceId}`, { method: "DELETE" }),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["wg-devices", uuid] }),
    })
}
