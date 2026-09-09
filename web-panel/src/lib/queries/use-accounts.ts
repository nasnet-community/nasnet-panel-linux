import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    getAccountsByNode,
    createAccount,
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
