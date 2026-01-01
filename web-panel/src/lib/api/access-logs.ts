import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type {
    AccessLogEntry,
    AggregatedAccessLogEntry,
    AccessLogSummary,
    DomainCount,
} from "@/lib/types"

export async function getAccessLogs(
    nodeId: number,
    email?: string,
    limit?: number
): Promise<ApiResponse<AccessLogEntry[]>> {
    return api.get<AccessLogEntry[]>(
        `/api/v1/nodes/${nodeId}/access-logs${buildQueryString({ email, limit })}`
    )
}

export interface AggregatedAccessLogsParams {
    node_ids?: number[]
    email?: string
    limit?: number
}

export async function getAggregatedAccessLogs(
    params: AggregatedAccessLogsParams = {}
): Promise<ApiResponse<AggregatedAccessLogEntry[]>> {
    return api.get<AggregatedAccessLogEntry[]>(
        `/api/v1/access-logs${buildQueryString({ node_ids: params.node_ids, email: params.email, limit: params.limit })}`
    )
}

export interface AccessLogAnalyticsParams {
    node_ids?: number[]
    email?: string
    from?: string
    to?: string
    limit?: number
    offset?: number
}

export async function getAccessLogAnalytics(
    params: AccessLogAnalyticsParams = {}
): Promise<ApiResponse<AccessLogSummary[]> & { total?: number }> {
    return api.get(
        `/api/v1/access-logs/analytics${buildQueryString(params)}`
    )
}

export async function getAccessLogTopDomains(
    params: AccessLogAnalyticsParams & { top?: number } = {}
): Promise<ApiResponse<DomainCount[]>> {
    return api.get<DomainCount[]>(
        `/api/v1/access-logs/top-domains${buildQueryString({ node_ids: params.node_ids, email: params.email, from: params.from, to: params.to, top: params.top })}`
    )
}
