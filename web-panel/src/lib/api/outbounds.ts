import { api, type ApiResponse } from "@/lib/api"
import type { Outbound, OutboundTestEntry, OutboundTestSettings } from "@/lib/types"

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

// The default 30s client timeout is far below what a test may legitimately
// take, so the browser would abort a run the agent goes on to finish. These
// sit above the hard budget ceilings the usecase enforces.
const TEST_TIMEOUT_MS = 120_000
const SPEEDTEST_TIMEOUT_MS = 300_000

export async function testOutbound(
    outboundId: number,
    opts?: { testUrl?: string; speedtest?: boolean },
): Promise<ApiResponse<OutboundTestEntry>> {
    const body: Record<string, unknown> = {}
    if (opts?.testUrl) body.test_url = opts.testUrl
    if (opts?.speedtest) body.speedtest = true
    return api.post<OutboundTestEntry>(
        `/api/v1/outbounds/${outboundId}/test`,
        body,
        opts?.speedtest ? SPEEDTEST_TIMEOUT_MS : TEST_TIMEOUT_MS,
    )
}

export async function getOutboundTestSettings(nodeId: number): Promise<ApiResponse<OutboundTestSettings>> {
    return api.get<OutboundTestSettings>(`/api/v1/nodes/${nodeId}/outbound-test-settings`)
}

export async function updateOutboundTestSettings(
    nodeId: number,
    settings: OutboundTestSettings,
): Promise<ApiResponse<OutboundTestSettings>> {
    return api.put<OutboundTestSettings>(`/api/v1/nodes/${nodeId}/outbound-test-settings`, settings)
}

export async function toggleOutboundDisabled(outboundId: number): Promise<ApiResponse<Outbound>> {
    return api.post<Outbound>(`/api/v1/outbounds/${outboundId}/toggle`)
}
