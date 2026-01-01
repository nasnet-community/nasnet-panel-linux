import { useMutation, useQueryClient } from "@tanstack/react-query"
import { cleanupDatabase } from "@/lib/admin-api"
import type { CleanupResult } from "@/lib/admin-api"
import { toast } from "sonner"

export function useDatabaseCleanup() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await cleanupDatabase()
            if (!res.success) throw new Error(res.error || "Failed to cleanup database")
            return res.data!
        },
        onSuccess: (data: CleanupResult) => {
            const total =
                data.accounts_removed +
                data.subscriptions_deleted +
                data.users_deleted +
                data.audit_logs_deleted +
                data.notification_logs_deleted
            toast.success(`Database cleaned. ${total} records removed.`, { duration: 10000 })
            queryClient.invalidateQueries()
        },
        onError: (error: Error) => {
            toast.error(error.message, { duration: 10000 })
        },
    })
}
