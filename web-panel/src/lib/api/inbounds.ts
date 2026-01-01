import { api, type ApiResponse } from "@/lib/api"
import type { Inbound } from "@/lib/types"

export async function toggleInboundDisabled(inboundId: number): Promise<ApiResponse<Inbound>> {
    return api.post<Inbound>(`/api/v1/inbounds/${inboundId}/toggle`)
}
