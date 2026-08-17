import { api, type ApiResponse } from "@/lib/api"
import type {
    FlowConnsView,
    FlowEvent,
    FlowView,
    TraceRequest,
    TraceView,
} from "@/lib/types/flow"

export async function getFlow(): Promise<ApiResponse<FlowView>> {
    return api.get<FlowView>("/api/v1/network/flow")
}

export async function postTrace(req: TraceRequest): Promise<ApiResponse<TraceView>> {
    return api.post<TraceView>("/api/v1/network/flow/trace", req)
}

export async function getFlowConns(): Promise<ApiResponse<FlowConnsView>> {
    return api.get<FlowConnsView>("/api/v1/network/flow/conns")
}

export async function getFlowEvents(): Promise<ApiResponse<FlowEvent[]>> {
    return api.get<FlowEvent[]>("/api/v1/network/flow/events")
}
