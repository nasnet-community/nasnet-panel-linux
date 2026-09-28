import { api, type ApiResponse } from "@/lib/api"
import { getApiBaseUrl } from "@/lib/config"
import { buildQueryString } from "./utils"
import type {
    User,
    UserListItem,
    UserGroup,
    UserGroupCounts,
    UserDetails,
    UserOverview,
    UserTimeline,
    TimelineKind,
    HourlyUsagePoint,
    SubscriptionChanges,
    UserAccountInfo,
    Subscription,
} from "@/lib/types"

export interface ListUsersParams {
    page?: number
    per_page?: number
    search?: string
    group?: UserGroup
    telegram?: "linked" | "unlinked"
    seen?: "online" | "24h" | "7d" | "30d" | "over30d" | "never"
    joined?: "7d" | "30d" | "90d" | "custom"
    joined_from?: string
    joined_to?: string
    admin?: boolean
    sort?: string
    order?: string
}

export interface ListUsersResponse {
    users: UserListItem[]
    total: number
    groups: UserGroupCounts
}

const NO_COUNTS: UserGroupCounts = { all: 0, active: 0, ending: 0, lapsed: 0, never: 0, banned: 0 }

/** Query parameters shared by the list and its CSV export. */
function listFilters(params: ListUsersParams) {
    return {
        search: params.search,
        group: params.group && params.group !== "all" ? params.group : undefined,
        telegram: params.telegram,
        seen: params.seen,
        joined: params.joined,
        joined_from: params.joined_from,
        joined_to: params.joined_to,
        admin: params.admin ? 1 : undefined,
        sort: params.sort,
        order: params.order,
    }
}

export async function listUsers(params: ListUsersParams = {}): Promise<ApiResponse<ListUsersResponse>> {
    // page/per_page go as-is: buildQueryString's pagination argument sends offset/limit.
    const qs = buildQueryString({ ...listFilters(params), page: params.page, per_page: params.per_page })
    // Use api.getRaw to preserve meta fields while getting proper token refresh
    const json = await api.getRaw<{ success: boolean; data?: UserListItem[]; meta?: { total: number; groups?: UserGroupCounts }; error?: string }>(
        `/api/v1/admin/users${qs}`,
    )
    if (json.success && json.data) {
        const users = Array.isArray(json.data) ? json.data : []
        return { success: true, data: { users, total: json.meta?.total ?? users.length, groups: json.meta?.groups ?? NO_COUNTS } }
    }
    return { success: false, error: json.error || "Failed to load users" }
}

/** Download URL for the users CSV: every page of a view, or only `ids`. */
export function usersExportUrl(params: ListUsersParams, ids?: number[]): string {
    const qs = buildQueryString({ ...listFilters(params), format: "csv", ids: ids && ids.length > 0 ? ids : undefined })
    return `${getApiBaseUrl()}/api/v1/admin/export/users${qs}`
}

export async function getUserDetails(id: number): Promise<ApiResponse<UserDetails>> {
    return api.get<UserDetails>(`/api/v1/admin/users/${id}/details`)
}

/** Bans the user and pauses their active subscriptions. */
export async function banUser(id: number): Promise<ApiResponse<SubscriptionChanges>> {
    return api.post<SubscriptionChanges>(`/api/v1/admin/users/${id}/ban`)
}

/** Lifts the ban and resumes the subscriptions the ban paused. */
export async function unbanUser(id: number): Promise<ApiResponse<SubscriptionChanges>> {
    return api.post<SubscriptionChanges>(`/api/v1/admin/users/${id}/unban`)
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

export async function getUserOverview(userId: number): Promise<ApiResponse<UserOverview>> {
    return api.get<UserOverview>(`/api/v1/admin/users/${userId}/overview`)
}

export interface TimelineParams {
    kind?: TimelineKind
    includeViews?: boolean
    before?: string
    limit?: number
}

export async function getUserTimeline(userId: number, p: TimelineParams = {}): Promise<ApiResponse<UserTimeline>> {
    const qs = new URLSearchParams()
    if (p.kind) qs.set("kind", p.kind)
    if (p.includeViews) qs.set("include_views", "1")
    if (p.before) qs.set("before", p.before)
    qs.set("limit", String(p.limit ?? 100))
    return api.get<UserTimeline>(`/api/v1/admin/users/${userId}/timeline?${qs.toString()}`)
}

export async function getSubscriptionUsagePattern(subId: number, days = 30): Promise<ApiResponse<HourlyUsagePoint[]>> {
    return api.get<HourlyUsagePoint[]>(`/api/v1/admin/subscriptions/${subId}/usage-pattern?days=${days}`)
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
