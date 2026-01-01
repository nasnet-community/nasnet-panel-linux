import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type {
    DashboardStats,
    XraySystemStats,
    OnlineUsersHistoryPoint,
    HourlyUsagePoint,
    PeakHourPoint,
    BlockedDomainSummary,
    ExhaustionPrediction,
} from "@/lib/types"

// ==================== Dashboard ====================

export async function getDashboardStats(): Promise<ApiResponse<DashboardStats>> {
    return api.get<DashboardStats>("/api/v1/admin/dashboard")
}

export async function getXraySystemStats(): Promise<ApiResponse<XraySystemStats>> {
    return api.get<XraySystemStats>("/api/v1/admin/xray/stats")
}

export async function getOnlineUsers(): Promise<ApiResponse<string[]>> {
    return api.get<string[]>("/api/v1/admin/users/online")
}

export async function getOnlineUsersHistory(minutes: number = 15): Promise<ApiResponse<{ points: OnlineUsersHistoryPoint[] }>> {
    return api.get<{ points: OnlineUsersHistoryPoint[] }>(`/api/v1/admin/dashboard/online-users-history?minutes=${minutes}`)
}

// ==================== Analytics ====================

export async function getPeakHours(days: number = 7, nodeIds?: number[]): Promise<ApiResponse<PeakHourPoint[]>> {
    return api.get<PeakHourPoint[]>(`/api/v1/admin/analytics/peak-hours${buildQueryString({ days, node_ids: nodeIds })}`)
}

export async function getBlockedDomainStats(
    params: { days?: number; nodeIds?: number[]; top?: number } = {}
): Promise<ApiResponse<BlockedDomainSummary>> {
    return api.get<BlockedDomainSummary>(
        `/api/v1/admin/analytics/blocked-domains${buildQueryString({ days: params.days, node_ids: params.nodeIds, top: params.top })}`
    )
}

export async function getUserUsagePattern(userId: number, days: number = 7): Promise<ApiResponse<HourlyUsagePoint[]>> {
    return api.get<HourlyUsagePoint[]>(`/api/v1/admin/users/${userId}/usage-pattern?days=${days}`)
}

export async function getExhaustionPrediction(subId: number): Promise<ApiResponse<ExhaustionPrediction>> {
    return api.get<ExhaustionPrediction>(`/api/v1/admin/subscriptions/${subId}/exhaustion-prediction`)
}
