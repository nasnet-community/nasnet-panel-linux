import { api, type ApiResponse } from "@/lib/api"

export type MaintenanceScope = "" | "global" | "node" | "subscription"

export interface MaintenanceStatus {
  active: boolean
  scope: MaintenanceScope
  message: string
  since?: string
}

export interface SetGlobalPayload {
  enabled: boolean
  message: string
  notify: boolean
}

export interface SetEntityPayload {
  enabled: boolean
  message: string
}

// Public — used by the subscription panel (unauth).
export function getSubMaintenanceStatus(uuid: string): Promise<ApiResponse<MaintenanceStatus>> {
  return api.get<MaintenanceStatus>(`/api/v1/public/sub/${encodeURIComponent(uuid)}/maintenance`)
}

// Admin — global toggle + broadcast.
export function setGlobalMaintenance(payload: SetGlobalPayload): Promise<ApiResponse<{ enabled: boolean }>> {
  return api.post<{ enabled: boolean }>(`/api/v1/admin/maintenance/global`, payload)
}

// Admin — per-node maintenance.
export function setNodeMaintenance(id: number, payload: SetEntityPayload): Promise<ApiResponse<null>> {
  return api.post<null>(`/api/v1/admin/maintenance/nodes/${id}`, payload)
}

// Admin — per-subscription maintenance.
export function setSubscriptionMaintenance(id: number, payload: SetEntityPayload): Promise<ApiResponse<null>> {
  return api.post<null>(`/api/v1/admin/maintenance/subscriptions/${id}`, payload)
}
