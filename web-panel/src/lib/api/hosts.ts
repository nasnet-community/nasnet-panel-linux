import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type { Host, HostWithRelations, HostTemplate } from "@/lib/types"

// ==================== Inbound Hosts ====================

export async function listInboundHosts(inboundId: number): Promise<ApiResponse<Host[]>> {
    return api.get<Host[]>(`/api/v1/inbounds/${inboundId}/hosts`)
}

export async function addInboundHost(inboundId: number, host: Partial<Host>): Promise<ApiResponse<Host>> {
    return api.post<Host>(`/api/v1/inbounds/${inboundId}/hosts`, host)
}

export async function getHost(hostId: number): Promise<ApiResponse<Host>> {
    return api.get<Host>(`/api/v1/hosts/${hostId}`)
}

export async function updateHost(hostId: number, host: Partial<Host>): Promise<ApiResponse<Host>> {
    return api.put<Host>(`/api/v1/hosts/${hostId}`, host)
}

export async function deleteHost(hostId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/hosts/${hostId}`)
}

// ==================== Global Hosts ====================

export interface ListHostsParams {
    search?: string
    node_id?: number
    inbound_id?: number
    plan_id?: number
    disabled?: boolean
    host_type?: "server" | "info"
    tag?: string
    sort_by?: string
    sort_order?: "asc" | "desc"
    offset?: number
    limit?: number
}

export async function listAllHosts(params: ListHostsParams = {}): Promise<ApiResponse<HostWithRelations[]> & { meta?: { total: number; offset: number; limit: number } }> {
    const qs = buildQueryString(params)
    const url = `/api/v1/hosts${qs}`
    return api.get<HostWithRelations[]>(url) as Promise<ApiResponse<HostWithRelations[]> & { meta?: { total: number; offset: number; limit: number } }>
}

export async function createHost(host: Partial<Host> & ({ inbound_id: number } | { plan_id: number })): Promise<ApiResponse<Host>> {
    return api.post<Host>("/api/v1/hosts", host)
}

export async function bulkCreateInfoHosts(host: Partial<Host>, planIds: number[]): Promise<ApiResponse<Host[]>> {
    return api.post<Host[]>("/api/v1/hosts/bulk-create", { host, plan_ids: planIds })
}

export async function duplicateHost(id: number): Promise<ApiResponse<Host>> {
    return api.post<Host>(`/api/v1/hosts/${id}/duplicate`)
}

export async function bulkUpdateHosts(ids: number[], fields: Partial<Host>): Promise<ApiResponse<{ updated: number }>> {
    return api.put<{ updated: number }>("/api/v1/hosts/bulk", { ids, fields })
}

export async function listHostTags(): Promise<ApiResponse<string[]>> {
    return api.get<string[]>("/api/v1/hosts/tags")
}

// ==================== Host Templates ====================

export async function listHostTemplates(): Promise<ApiResponse<HostTemplate[]>> {
    return api.get<HostTemplate[]>("/api/v1/host-templates")
}

export async function createHostTemplate(template: Partial<HostTemplate>): Promise<ApiResponse<HostTemplate>> {
    return api.post<HostTemplate>("/api/v1/host-templates", template)
}

export async function getHostTemplate(id: number): Promise<ApiResponse<HostTemplate>> {
    return api.get<HostTemplate>(`/api/v1/host-templates/${id}`)
}

export async function updateHostTemplate(id: number, template: Partial<HostTemplate>): Promise<ApiResponse<HostTemplate>> {
    return api.put<HostTemplate>(`/api/v1/host-templates/${id}`, template)
}

export async function deleteHostTemplate(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/host-templates/${id}`)
}

export async function applyHostTemplate(id: number, hostIds: number[]): Promise<ApiResponse<{ updated: number }>> {
    return api.post<{ updated: number }>(`/api/v1/host-templates/${id}/apply`, { host_ids: hostIds })
}
