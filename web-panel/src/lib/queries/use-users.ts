import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listUsers,
    getUserDetails,
    banUser,
    unbanUser,
    setUserAdmin,
    getUserSubscriptions,
    updateUserTelegramID,
    createUser,
    getUserUsageHistory,
    getUserActivity,
    updateUserNotes,
    getUserAccounts,
} from "@/lib/admin-api"
import type { User, UserListItem } from "@/lib/types"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Types ====================

export interface UseUsersParams {
    page: number
    perPage: number
    search: string
    filter: string
    sort: string
    order: string
}

// ==================== Queries ====================

// List users with filters and pagination
export function useUsers(params: UseUsersParams, options?: { refetchInterval?: number }) {
    return useQuery({
        queryKey: queryKeys.userList(params),
        queryFn: async () => {
            const res = await listUsers({
                page: params.page,
                per_page: params.perPage,
                search: params.search,
                filter: params.filter,
                sort: params.sort,
                order: params.order,
            })
            if (!res.success) throw new Error(res.error || "Failed to fetch users")
            return res.data!
        },
        refetchInterval: options?.refetchInterval,
    })
}

// Get single user details
export function useUserDetails(id: number) {
    return useQuery({
        queryKey: queryKeys.userDetails(id),
        queryFn: async () => {
            const res = await getUserDetails(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch user details")
            return res.data!
        },
        enabled: id > 0,
    })
}

// Get user's subscriptions
export function useUserSubscriptions(userId: number) {
    return useQuery({
        queryKey: queryKeys.userSubscriptions(userId),
        queryFn: async () => {
            const res = await getUserSubscriptions(userId)
            if (!res.success) throw new Error(res.error || "Failed to fetch user subscriptions")
            return res.data || []
        },
        enabled: userId > 0,
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
        onSuccess: (_, { isBanned }) => {
            toast.success(isBanned ? "User unbanned" : "User banned")
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
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

// Bulk ban users
export function useBulkBan() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (userIds: number[]) => {
            await Promise.all(userIds.map(id => banUser(id)))
        },
        onSuccess: (_, userIds) => {
            toast.success(`${userIds.length} users banned`)
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Bulk unban users
export function useBulkUnban() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (userIds: number[]) => {
            await Promise.all(userIds.map(id => unbanUser(id)))
        },
        onSuccess: (_, userIds) => {
            toast.success(`${userIds.length} users unbanned`)
            queryClient.invalidateQueries({ queryKey: queryKeys.users })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
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

// Usage history for chart
export function useUserUsageHistory(userId: number, days: number = 30) {
    return useQuery({
        queryKey: [...queryKeys.userDetails(userId), 'usage-history', days],
        queryFn: async () => {
            const res = await getUserUsageHistory(userId, days)
            if (!res.success) throw new Error(res.error || "Failed to fetch usage history")
            return res.data || []
        },
        enabled: userId > 0,
    })
}

// Activity feed
export function useUserActivity(userId: number, offset: number = 0) {
    return useQuery({
        queryKey: [...queryKeys.userDetails(userId), 'activity', offset],
        queryFn: async () => {
            const res = await getUserActivity(userId, offset)
            if (!res.success) throw new Error(res.error || "Failed to fetch activity")
            return res.data || []
        },
        enabled: userId > 0,
    })
}

// Activity feed (infinite scroll)
export function useUserActivityInfinite(userId: number) {
    return useInfiniteQuery({
        queryKey: [...queryKeys.userDetails(userId), 'activity', 'infinite'],
        queryFn: async ({ pageParam = 0 }) => {
            const res = await getUserActivity(userId, pageParam)
            if (!res.success) throw new Error(res.error || "Failed to fetch activity")
            return { data: res.data || [], offset: pageParam }
        },
        initialPageParam: 0,
        getNextPageParam: (lastPage) => {
            if (lastPage.data.length < 20) return undefined
            return lastPage.offset + 20
        },
        enabled: userId > 0,
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
            toast.success("Notes saved")
            queryClient.invalidateQueries({ queryKey: queryKeys.userDetails(userId) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
