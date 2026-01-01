import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    getAccounts,
    deleteAccount as deleteAccountAdmin,
    syncAccountStats,
    getAccountCounts,
    type ListAccountsParams,
    type Account,
} from "@/lib/admin-api"
import { getAccountLink } from "@/lib/api/accounts"
import { copyToClipboard } from "@/lib/utils"
import {
    disableAccount,
    enableAccount,
    updateAccount,
    type UpdateAccountRequest,
} from "@/lib/api/accounts"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Types ====================

export interface UseAccountListParams {
    status?: string
    page: number
    perPage: number
    search?: string
    exhausted?: string
    nodeId?: number
    inboundId?: number
    source?: string
}

// ==================== Queries ====================

export function useAccountList(params: UseAccountListParams) {
    return useQuery({
        queryKey: queryKeys.accountList(params),
        queryFn: async () => {
            const apiParams: ListAccountsParams = {
                offset: (params.page - 1) * params.perPage,
                limit: params.perPage,
            }
            if (params.status && params.status !== "all") apiParams.status = params.status
            if (params.search) apiParams.search = params.search
            if (params.exhausted && params.exhausted !== "all") apiParams.exhausted = params.exhausted === "true"
            if (params.nodeId) apiParams.node_id = params.nodeId
            if (params.inboundId) apiParams.inbound_id = params.inboundId
            if (params.source && params.source !== "all") apiParams.source = params.source

            const res = await getAccounts(apiParams)
            if (!res.success) throw new Error(res.error || "Failed to fetch accounts")
            return {
                accounts: Array.isArray(res.data) ? res.data : [],
                total: res.meta?.total ?? 0,
            }
        },
    })
}

export function useAccountCounts() {
    return useQuery({
        queryKey: queryKeys.accountCounts(),
        queryFn: async () => {
            const res = await getAccountCounts()
            if (!res.success) throw new Error(res.error || "Failed to fetch counts")
            return res.data!
        },
    })
}

// ==================== Mutations ====================

export function useDeleteAccountMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteAccountAdmin(id)
            if (!res.success) throw new Error(res.error || "Failed to delete account")
        },
        onSuccess: () => {
            toast.success("Account deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDisableAccountMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await disableAccount(id)
            if (!res.success) throw new Error("Failed to disable account")
        },
        onSuccess: () => {
            toast.success("Account disabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useEnableAccountMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await enableAccount(id)
            if (!res.success) throw new Error("Failed to enable account")
        },
        onSuccess: () => {
            toast.success("Account enabled")
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useSyncAccountMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await syncAccountStats(id)
            if (!res.success) throw new Error(res.error || "Failed to sync account")
        },
        onSuccess: () => {
            toast.success("Sync requested")
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useCopyAccountLink() {
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await getAccountLink(id)
            if (!res.success || !res.data?.link) throw new Error("Failed to get link")
            await copyToClipboard(res.data.link)
        },
        onSuccess: () => {
            toast.success("Subscription link copied")
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useUpdateAccountMutation() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, data }: { id: number; data: UpdateAccountRequest }) => {
            const res = await updateAccount(id, data)
            if (!res.success) throw new Error(res.error || "Failed to update account")
        },
        onSuccess: () => {
            toast.success("Account updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export type BulkAccountAction = "sync" | "disable" | "enable" | "delete"

export function useBulkAccountAction() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ action, ids }: { action: BulkAccountAction; ids: number[] }) => {
            const actionFn = {
                sync: (id: number) => syncAccountStats(id),
                disable: (id: number) => disableAccount(id),
                enable: (id: number) => enableAccount(id),
                delete: (id: number) => deleteAccountAdmin(id),
            }[action]

            const results = await Promise.allSettled(ids.map(id => actionFn(id)))
            const succeeded = results.filter(r => r.status === "fulfilled").length
            const failed = results.filter(r => r.status === "rejected").length
            return { succeeded, failed }
        },
        onSuccess: (data, { action }) => {
            if (data.failed > 0) {
                toast.warning(`Bulk ${action}: ${data.succeeded} succeeded, ${data.failed} failed`)
            } else {
                toast.success(`Bulk ${action}: ${data.succeeded} succeeded`)
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}
