import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type { User, Subscription, Inbound, Node } from "@/lib/types"

// Account type with full relationship data as returned by the API
export interface Account {
    id: number
    inbound_id: number
    inbound?: Inbound & { node?: Node }
    email: string
    uuid: string
    flow: string
    source: string
    status: 'active' | 'disabled' | 'expired'
    data_limit: number
    data_used: number
    expires_at?: string
    created_at: string
    subscription_id?: number
    subscription?: Subscription & { user?: User }
    last_activity_at?: string
}

export interface CreateAccountRequest {
    inbound_id: number
    email: string
    uuid?: string
    flow?: string
}

export interface ListAccountsParams {
    offset?: number
    limit?: number
    status?: string
    search?: string
    exhausted?: boolean
    node_id?: number
    inbound_id?: number
    source?: string
}

export interface AccountCounts {
    all: number
    active: number
    disabled: number
    expired: number
}

export interface UpdateAccountRequest {
    email: string
    uuid: string
    flow?: string
    data_limit: number
    expires_at?: string | null
    enabled: boolean
}

// ==================== Account CRUD ====================

export async function getAccounts(params: ListAccountsParams = {}): Promise<ApiResponse<Account[]> & { meta?: { offset: number; limit: number; total: number } }> {
    const qs = buildQueryString({
        offset: params.offset,
        limit: params.limit,
        status: params.status !== "all" ? params.status : undefined,
        search: params.search,
        exhausted: params.exhausted,
        node_id: params.node_id,
        inbound_id: params.inbound_id,
        source: params.source !== "all" ? params.source : undefined,
    })
    const url = `/api/v1/admin/accounts${qs}`
    return api.get<Account[]>(url) as Promise<ApiResponse<Account[]> & { meta?: { offset: number; limit: number; total: number } }>
}

export async function getAccountsByNode(nodeId: number): Promise<ApiResponse<Account[]>> {
    return api.get<Account[]>(`/api/v1/admin/accounts/nodes/${nodeId}`)
}

export async function getAccountsBySubscription(subId: number): Promise<ApiResponse<Account[]>> {
    return api.get<Account[]>(`/api/v1/admin/accounts/subscription/${subId}`)
}

export async function createAccount(data: CreateAccountRequest): Promise<ApiResponse<Account>> {
    return api.post<Account>("/api/v1/admin/accounts", data)
}

export async function createAccountManual(
    inboundId: number,
    email: string,
    uuid: string,
    flow?: string
): Promise<ApiResponse<Account>> {
    return api.post<Account>("/api/v1/admin/accounts", {
        inbound_id: inboundId,
        email,
        uuid,
        flow,
    })
}

export async function updateAccount(id: number, data: UpdateAccountRequest): Promise<ApiResponse> {
    return api.put(`/api/v1/admin/accounts/${id}`, data)
}

export async function deleteAccount(id: number): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/admin/accounts/${id}`)
}

export async function disableAccount(id: number): Promise<ApiResponse> {
    return api.post(`/api/v1/admin/accounts/${id}/disable`)
}

export async function enableAccount(id: number): Promise<ApiResponse> {
    return api.post(`/api/v1/admin/accounts/${id}/enable`)
}

export async function getAccountLink(id: number): Promise<ApiResponse<{ link: string }>> {
    return api.get<{ link: string }>(`/api/v1/admin/accounts/${id}/link`)
}

export async function syncAccountStats(id: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/admin/accounts/${id}/sync`)
}

export async function migrateAccount(id: number, targetInboundId: number): Promise<ApiResponse<void>> {
    return api.post<void>(`/api/v1/admin/accounts/${id}/migrate`, {
        target_inbound_id: targetInboundId,
    })
}

// Account counts (fires parallel requests for tab counts)
export async function getAccountCounts(): Promise<ApiResponse<AccountCounts>> {
    const [allRes, activeRes, disabledRes, expiredRes] = await Promise.all([
        api.getRaw<{ success: boolean; count: number }>("/api/v1/admin/accounts/count"),
        api.getRaw<{ success: boolean; count: number }>("/api/v1/admin/accounts/count?status=active"),
        api.getRaw<{ success: boolean; count: number }>("/api/v1/admin/accounts/count?status=disabled"),
        api.getRaw<{ success: boolean; count: number }>("/api/v1/admin/accounts/count?status=expired"),
    ])

    return {
        success: true,
        data: {
            all: allRes?.count ?? 0,
            active: activeRes?.count ?? 0,
            disabled: disabledRes?.count ?? 0,
            expired: expiredRes?.count ?? 0,
        },
    }
}
