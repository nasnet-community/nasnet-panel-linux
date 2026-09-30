import { useQuery, useInfiniteQuery, useMutation, useQueryClient, keepPreviousData } from "@tanstack/react-query"
import {
    listUsers,
    getUserDetails,
    banUser,
    unbanUser,
    setUserAdmin,
    getUserSubscriptions,
    updateUserTelegramID,
    createUser,
    getUserOverview,
    getUserTimeline,
    getSubscriptionUsagePattern,
    updateUserNotes,
    getUserAccounts,
} from "@/lib/api/users"
import type { User, TimelineKind } from "@/lib/types"
import type { ListUsersParams } from "@/lib/api/users"
import { queryKeys } from "./keys"
import { useRefreshInterval } from "@/hooks/use-refresh-interval"
import { toast } from "@/lib/toast"

// ==================== Types ====================


// ==================== Queries ====================

// List users with filters and pagination
export function useUsers(params: ListUsersParams) {
    return useQuery({
        queryKey: queryKeys.userList(params),
        queryFn: async () => {
            const res = await listUsers(params)
            if (!res.success) throw new Error(res.error || "Failed to fetch users")
            return res.data!
        },
        placeholderData: keepPreviousData,
    })
}

// Get single user details
export function useUserDetails(id: number) {
    const refetchInterval = useRefreshInterval()
    return useQuery({
        queryKey: queryKeys.userDetails(id),
        queryFn: async () => {
            const res = await getUserDetails(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch user details")
            return res.data!
        },
        enabled: id > 0,
        refetchInterval,
        refetchIntervalInBackground: false,
    })
}

// Get user's subscriptions
export function useUserSubscriptions(userId: number) {
    const refetchInterval = useRefreshInterval()
    return useQuery({
        queryKey: queryKeys.userSubscriptions(userId),
        queryFn: async () => {
            const res = await getUserSubscriptions(userId)
            if (!res.success) throw new Error(res.error || "Failed to fetch user subscriptions")
            return res.data || []
        },
        enabled: userId > 0,
        refetchInterval,
        refetchIntervalInBackground: false,
    })
}

// ==================== Mutations ====================

// Ban/Unban user
export function useBanUser() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ userId, isBanned }: { userId: number; isBanned: boolean }) => {
            const res = isBanned ? await unbanUser(userId) : await banUser(userId)
            if (!res.success) throw new Error(res.error || "Failed to update user")
            return res.data
        },
        onSuccess: (changes, { isBanned }) => {
            const n = changes?.changed.length ?? 0
            const subs = `${n} subscription${n === 1 ? "" : "s"}`
            toast.success(isBanned
                ? (n ? `User unbanned, ${subs} resumed` : "User unbanned")
                : (n ? `User banned, ${subs} paused` : "User banned"))
            if (changes?.failed?.length) {
                toast.error(`Couldn't ${isBanned ? "resume" : "pause"} #${changes.failed.join(", #")}`)
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Toggle admin status
export function useToggleAdmin() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ userId, isAdmin }: { userId: number; isAdmin: boolean }) => {
            const res = await setUserAdmin(userId, !isAdmin)
            if (!res.success) throw new Error(res.error || "Failed to update user")
            return res.data
        },
        onSuccess: () => {
            toast.success("Admin status updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Bulk ban/unban: one call per user, so some can fail while the rest go
// through. Report those and refresh either way; each ban also pauses (and
// each unban resumes) the user's subscriptions.
function useBulkBanToggle(ban: boolean) {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (userIds: number[]) => {
            const results = await Promise.allSettled(userIds.map(async id => {
                const res = ban ? await banUser(id) : await unbanUser(id)
                if (!res.success) throw new Error(res.error || "Failed to update user")
                return res.data
            }))
            const failed = userIds.filter((_, i) => results[i].status === "rejected")
            if (failed.length === userIds.length) {
                const first = results.find(r => r.status === "rejected")
                throw first?.status === "rejected" && first.reason instanceof Error ? first.reason : new Error("Failed to update users")
            }
            const subsFailed = results.flatMap(r => r.status === "fulfilled" ? r.value?.failed ?? [] : [])
            return { done: userIds.length - failed.length, failed, subsFailed }
        },
        onSuccess: ({ done, failed, subsFailed }) => {
            toast.success(`${done} user${done === 1 ? "" : "s"} ${ban ? "banned" : "unbanned"}`)
            if (failed.length) toast.error(`Couldn't ${ban ? "ban" : "unban"} user #${failed.join(", #")}`)
            if (subsFailed.length) toast.error(`Couldn't ${ban ? "pause" : "resume"} #${subsFailed.join(", #")}`)
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
    })
}

// Bulk ban users
export function useBulkBan() {
    return useBulkBanToggle(true)
}

// Bulk unban users
export function useBulkUnban() {
    return useBulkBanToggle(false)
}

// Update user Telegram ID
export function useUpdateTelegramID() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ userId, telegramId }: { userId: number; telegramId: number }) => {
            const res = await updateUserTelegramID(userId, telegramId)
            if (!res.success) throw new Error(res.error || "Failed to update Telegram ID")
        },
        onSuccess: () => {
            toast.success("Telegram ID updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Create user
export function useCreateUser() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (data: { username: string; first_name: string; last_name?: string; telegram_id?: number }) => {
            const res = await createUser(data)
            if (!res.success) throw new Error(res.error || "Failed to create user")
            return res.data as User
        },
        onSuccess: () => {
            toast.success("User created")
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// ==================== User Detail Page Hooks ====================

// Per-subscription panels: online IPs, forecast, node accounts, chat counts
export function useUserOverview(userId: number) {
    const refetchInterval = useRefreshInterval()
    return useQuery({
        queryKey: queryKeys.userOverview(userId),
        queryFn: async () => {
            const res = await getUserOverview(userId)
            if (!res.success) throw new Error(res.error || "Failed to fetch user overview")
            return res.data!
        },
        enabled: userId > 0,
        refetchInterval,
        refetchIntervalInBackground: false,
    })
}

const TIMELINE_PAGE = 100

// Customer timeline, paged by cursor
export function useUserTimeline(userId: number, kind: TimelineKind | undefined, includeViews: boolean) {
    return useInfiniteQuery({
        queryKey: queryKeys.userTimeline(userId, kind ?? "all", includeViews),
        queryFn: async ({ pageParam }) => {
            const res = await getUserTimeline(userId, { kind, includeViews, before: pageParam, limit: TIMELINE_PAGE })
            if (!res.success) throw new Error(res.error || "Failed to fetch timeline")
            return res.data!
        },
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (last) => last.next_cursor ?? undefined,
        placeholderData: keepPreviousData,
        enabled: userId > 0,
    })
}

// Hourly connection counts for one subscription (UTC hours)
export function useSubscriptionUsagePattern(subId: number, days: number, enabled = true) {
    return useQuery({
        queryKey: queryKeys.subscriptionUsagePattern(subId, days),
        queryFn: async () => {
            const res = await getSubscriptionUsagePattern(subId, days)
            if (!res.success) throw new Error(res.error || "Failed to fetch usage pattern")
            return res.data || []
        },
        enabled: enabled && subId > 0,
        staleTime: 5 * 60_000,
    })
}

// User accounts (nodes)
export function useUserAccounts(userId: number) {
    return useQuery({
        queryKey: [...queryKeys.userDetails(userId), 'accounts'],
        queryFn: async () => {
            const res = await getUserAccounts(userId)
            if (!res.success) throw new Error(res.error || "Failed to fetch accounts")
            return res.data || []
        },
        enabled: userId > 0,
    })
}

// Update admin notes
export function useUpdateUserNotes() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ userId, notes }: { userId: number; notes: string }) => {
            const res = await updateUserNotes(userId, notes)
            if (!res.success) throw new Error(res.error || "Failed to update notes")
        },
        onSuccess: (_, { userId }) => {
            queryClient.invalidateQueries({ queryKey: queryKeys.userDetails(userId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
