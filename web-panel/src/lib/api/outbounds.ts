import { api, type ApiResponse } from "@/lib/api"
import type { Outbound, OutboundTestResult } from "@/lib/types"

export async function listNodeOutbounds(nodeId: number): Promise<ApiResponse<Outbound[]>> {
    return api.get<Outbound[]>(`/api/v1/nodes/${nodeId}/outbounds`)
}

export async function addNodeOutbound(nodeId: number, outbound: Partial<Outbound>): Promise<ApiResponse<Outbound>> {
    return api.post<Outbound>(`/api/v1/nodes/${nodeId}/outbounds`, outbound)
}

export async function updateNodeOutbound(nodeId: number, outboundId: number, outbound: Partial<Outbound>): Promise<ApiResponse<Outbound>> {
    return api.put<Outbound>(`/api/v1/outbounds/${outboundId}`, outbound)
}

export async function deleteNodeOutbound(nodeId: number, outboundId: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/outbounds/${outboundId}`)
}

export async function testOutbound(outboundId: number, testUrl?: string): Promise<ApiResponse<OutboundTestResult>> {
    return api.post<OutboundTestResult>(`/api/v1/outbounds/${outboundId}/test`, testUrl ? { test_url: testUrl } : {})
}

export async function toggleOutboundDisabled(outboundId: number): Promise<ApiResponse<Outbound>> {
    return api.post<Outbound>(`/api/v1/outbounds/${outboundId}/toggle`)
}
