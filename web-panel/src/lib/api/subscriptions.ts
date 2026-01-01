import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type { Subscription, SubscriptionIP } from "@/lib/types"

interface ListSubscriptionsParams {
    status?: string
    page?: number
    per_page?: number
    search?: string
    plan_id?: number
    source?: string      // "manual" | "plan" | "trial"
    exhausted?: boolean
    sort?: string
    order?: string
}

export async function listSubscriptions(params: ListSubscriptionsParams = {}): Promise<ApiResponse<Subscription[]>> {
    const qs = buildQueryString({
        status: params.status,
        search: params.search,
        page: params.page,
        per_page: params.per_page,
        plan_id: params.plan_id,
        source: params.source,
        exhausted: params.exhausted,
        sort: params.sort,
        order: params.order,
    })
    return api.get<Subscription[]>(`/api/v1/admin/subscriptions${qs}`)
}

export async function getSubscription(id: number): Promise<ApiResponse<Subscription>> {
    return api.get<Subscription>(`/api/v1/admin/subscriptions/${id}`)
}

export async function extendSubscription(id: number, days: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/extend`, { days })
}

export async function revokeSubscription(id: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/admin/subscriptions/${id}/revoke`)
}

export async function pauseSubscription(id: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/pause`)
}

export async function resumeSubscription(id: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/resume`)
}

export async function setSubscriptionDataLimit(id: number, limit_gb: number | null): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${id}/data-limit`, { limit_gb })
}

export async function setSubscriptionExpiry(id: number, end_date: string | null, unlimited?: boolean): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${id}/end-date`, { end_date, unlimited })
}

export async function setSubscriptionBandwidthLimit(id: number, limit_mbps: number | null): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${id}/bandwidth-limit`, { limit_mbps })
}

// max_devices: 0 = inherit the plan's device limit, >0 = cap this subscription
export async function setSubscriptionMaxDevices(id: number, max_devices: number): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${id}/max-devices`, { max_devices })
}

export async function addSubscriptionData(id: number, amount_gb: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/add-data`, { amount_gb })
}

export async function resetSubscriptionData(id: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/reset-data`)
}

export async function renameSubscription(id: number, label: string): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${id}/label`, { label })
}

export async function regenerateSubscriptionKey(id: number, key?: string): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/regenerate-key`, key ? { key } : undefined)
}

export async function regenerateUUID(id: number): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>(`/api/v1/admin/subscriptions/${id}/regenerate-uuid`)
}

export async function setSubscriptionUUID(id: number, uuid: string): Promise<ApiResponse<{ updated: number }>> {
    return api.put<{ updated: number }>(`/api/v1/admin/subscriptions/${id}/uuid`, { uuid })
}

export interface UsageHistoryPoint {
    date: string      // YYYY-MM-DD
    data_used: number // bytes used that day (delta)
}

export async function getSubscriptionUsageHistory(id: number, days = 30): Promise<ApiResponse<UsageHistoryPoint[]>> {
    return api.get<UsageHistoryPoint[]>(`/api/v1/admin/subscriptions/${id}/usage-history?days=${days}`)
}

export async function setSubscriptionPanelPassword(
    id: number,
    mode: "default" | "custom" | "disabled",
    password?: string
): Promise<ApiResponse<void>> {
    return api.put<void>(`/api/v1/admin/subscriptions/${id}/panel-password`, { mode, password })
}

// Manual subscription creation
export interface CreateManualSubscriptionRequest {
    label: string
    inbound_ids: number[]
    data_limit_gb: number | null
    bandwidth_limit: number // Mbps, 0 = unlimited
    max_devices: number
    end_date: string | null
    user_id?: number | null
}

export async function createManualSubscription(req: CreateManualSubscriptionRequest): Promise<ApiResponse<Subscription>> {
    return api.post<Subscription>("/api/v1/admin/subscriptions/manual", req)
}

// Delete subscription permanently
export async function deleteSubscription(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/admin/subscriptions/${id}`)
}

// Bulk subscription action
export interface BulkActionResult {
    succeeded: number
    failed: number
    errors?: string[]
}

export async function bulkSubscriptionAction(action: string, ids: number[]): Promise<ApiResponse<BulkActionResult>> {
    return api.post<BulkActionResult>("/api/v1/admin/subscriptions/bulk", { action, ids })
}

export async function bulkSetBandwidthLimit(ids: number[], bandwidthLimit: number | null): Promise<ApiResponse<BulkActionResult>> {
    return api.post<BulkActionResult>("/api/v1/admin/subscriptions/bulk-bandwidth", {
        ids,
        bandwidth_limit: bandwidthLimit,
    })
}

// Subscription counts
export interface SubscriptionCounts {
    all: number
    active: number
    paused: number
    expired: number
    cancelled: number
    traffic_exhausted: number
}

export async function getSubscriptionCounts(): Promise<ApiResponse<SubscriptionCounts>> {
    return api.get<SubscriptionCounts>("/api/v1/admin/subscriptions/counts")
}

// Subscription IPs
export async function getSubscriptionIPs(id: number): Promise<ApiResponse<SubscriptionIP[]>> {
    return api.get<SubscriptionIP[]>(`/api/v1/admin/subscriptions/${id}/ips`)
}

export async function getSubscriptionActiveIPs(id: number): Promise<ApiResponse<SubscriptionIP[]>> {
    return api.get<SubscriptionIP[]>(`/api/v1/admin/subscriptions/${id}/ips/active`)
}

// Assign user to subscription
export async function assignSubscriptionUser(subId: number, userId: number | null): Promise<ApiResponse<Subscription>> {
    return api.put<Subscription>(`/api/v1/admin/subscriptions/${subId}/user`, { user_id: userId })
}

export async function assignSubscriptionToInbound(subscriptionId: number, inboundId: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/subscriptions/${subscriptionId}/assign-inbound`, {
        inbound_id: inboundId,
    })
}

export interface BulkManageInboundsRequest {
    subscription_ids: number[]
    add_inbound_ids: number[]
    remove_inbound_ids: number[]
}

export interface BulkInboundResult {
    subscriptions_affected: number
    accounts_added: number
    accounts_marked_for_removal: number
    skipped: number
    errors?: string[]
}

export interface BulkInboundSummary {
    inbound_counts: Record<number, number>
    total_subscriptions: number
}

export async function bulkManageInbounds(req: BulkManageInboundsRequest): Promise<ApiResponse<BulkInboundResult>> {
    return api.post<BulkInboundResult>("/api/v1/admin/subscriptions/bulk-inbounds", req)
}

export async function getBulkInboundSummary(subscriptionIds: number[]): Promise<ApiResponse<BulkInboundSummary>> {
    return api.post<BulkInboundSummary>("/api/v1/admin/subscriptions/bulk-inbound-summary", {
        subscription_ids: subscriptionIds,
    })
}
