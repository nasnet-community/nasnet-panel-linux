import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    getAccountsByNode,
    createAccount,
    deleteAccount,
    disableAccount,
    enableAccount,
    getAccountLink,
    getAccountsBySubscription,
    type CreateAccountRequest
} from "@/lib/api/accounts"
import { toast } from "sonner"

export function useAccountsByNode(nodeId: number, options?: { refetchInterval?: number | false }) {
    return useQuery({
        queryKey: ["accounts", "node", nodeId],
        queryFn: async () => {
            const res = await getAccountsByNode(nodeId)
            return res.data
        },
        enabled: !!nodeId,
        refetchInterval: options?.refetchInterval,
    })
}

export function useCreateAccount(nodeId: number) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (data: Omit<CreateAccountRequest, "inbound_id"> & { inbound_id: number }) => createAccount(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            toast.success("Account created successfully")
        },
        onError: (error: Error) => {
            toast.error(`Failed to create account: ${error.message}`)
        }
    })
}

export function useDeleteAccount(nodeId: number) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (id: number) => deleteAccount(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            toast.success("Account deleted successfully")
        },
        onError: (error: Error) => {
            toast.error(`Failed to delete account: ${error.message}`)
        }
    })
}

export function useDisableAccount(nodeId: number) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (id: number) => disableAccount(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            toast.success("Account disabled")
        },
        onError: (error: Error) => {
            toast.error(`Failed to disable account: ${error.message}`)
        }
    })
}

export function useEnableAccount(nodeId: number) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (id: number) => enableAccount(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["accounts", "node", nodeId] })
            toast.success("Account enabled")
        },
        onError: (error: Error) => {
            toast.error(`Failed to enable account: ${error.message}`)
        }
    })
}

export function useAccountLink() {
    return useMutation({
        mutationFn: (id: number) => getAccountLink(id),
        onError: (error: Error) => {
            toast.error(`Failed to get link: ${error.message}`)
        }
    })
}

export function useAccountsBySubscription(subId: number | undefined) {
    return useQuery({
        queryKey: ["accounts", "subscription", subId],
        queryFn: async () => {
            if (!subId) return []
            const res = await getAccountsBySubscription(subId)
            return res.data
        },
        enabled: !!subId,
    })
}
