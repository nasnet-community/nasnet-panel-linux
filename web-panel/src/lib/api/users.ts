import { api, type ApiResponse } from "@/lib/api"
import { buildQueryString } from "./utils"
import type {
    User,
    UserListItem,
    UserDetails,
    UserDailyUsagePoint,
    UserActivityEvent,
    UserAccountInfo,
    Subscription,
} from "@/lib/types"

export interface ListUsersParams {
    page?: number
    per_page?: number
    search?: string
    filter?: string
    sort?: string
    order?: string
}

export interface ListUsersResponse {
    users: UserListItem[]
    total: number
}

export async function listUsers(params: ListUsersParams = {}): Promise<ApiResponse<ListUsersResponse>> {
    const { page, per_page, ...rest } = params
    const qs = buildQueryString(
        { search: rest.search, filter: rest.filter !== "all" ? rest.filter : undefined, sort: rest.sort, order: rest.order },
        { page, perPage: per_page }
    )
    const endpoint = `/api/v1/admin/users${qs}`

    // Use api.getRaw to preserve meta fields while getting proper token refresh
    const json = await api.getRaw<{ success: boolean; data?: UserListItem[]; meta?: { total: number }; error?: string }>(endpoint)

    if (json.success && json.data) {
        const users = Array.isArray(json.data) ? json.data : []
        const total = json.meta?.total || users.length
        return {
            success: true,
            data: { users, total }
        }
    }
    return { success: false, error: json.error || "Failed to load users" }
}

export async function getUserDetails(id: number): Promise<ApiResponse<UserDetails>> {
    return api.get<UserDetails>(`/api/v1/admin/users/${id}/details`)
}

export async function banUser(id: number): Promise<ApiResponse<User>> {
    return api.post<User>(`/api/v1/admin/users/${id}/ban`)
}

export async function unbanUser(id: number): Promise<ApiResponse<User>> {
    return api.post<User>(`/api/v1/admin/users/${id}/unban`)
}

export async function setUserAdmin(id: number, isAdmin: boolean): Promise<ApiResponse<User>> {
    return api.put<User>(`/api/v1/admin/users/${id}/admin`, { is_admin: isAdmin })
}

export async function getUserSubscriptions(userId: number): Promise<ApiResponse<Subscription[]>> {
    return api.get<Subscription[]>(`/api/v1/subscriptions/user/${userId}`)
}

export async function updateUserTelegramID(id: number, telegramId: number): Promise<ApiResponse<void>> {
    return api.put<void>(`/api/v1/admin/users/${id}/telegram-id`, { telegram_id: telegramId })
}

export async function getUserUsageHistory(userId: number, days: number = 30): Promise<ApiResponse<UserDailyUsagePoint[]>> {
    return api.get<UserDailyUsagePoint[]>(`/api/v1/admin/users/${userId}/usage-history?days=${days}`)
}

export async function getUserActivity(userId: number, offset: number = 0, limit: number = 20): Promise<ApiResponse<UserActivityEvent[]>> {
    return api.get<UserActivityEvent[]>(`/api/v1/admin/users/${userId}/activity?offset=${offset}&limit=${limit}`)
}

export async function updateUserNotes(userId: number, notes: string): Promise<ApiResponse<void>> {
    return api.put<void>(`/api/v1/admin/users/${userId}/notes`, { notes })
}

export async function getUserAccounts(userId: number): Promise<ApiResponse<UserAccountInfo[]>> {
    return api.get<UserAccountInfo[]>(`/api/v1/admin/users/${userId}/accounts`)
}

export async function createUser(data: { username: string; first_name: string; last_name?: string; telegram_id?: number }): Promise<ApiResponse<User>> {
    return api.post<User>("/api/v1/admin/users", data)
}

export async function getOnlineUsersWithIPs(): Promise<ApiResponse<Record<string, string[]>>> {
    return api.get<Record<string, string[]>>("/api/v1/admin/users/online/ips")
}
