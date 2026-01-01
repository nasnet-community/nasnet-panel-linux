import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listBackups,
    createBackup,
    deleteBackup,
    restoreBackup,
    restoreFromExisting,
} from "@/lib/admin-api"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Queries ====================

export function useBackups() {
    return useQuery({
        queryKey: queryKeys.backupList(),
        queryFn: async () => {
            const res = await listBackups()
            if (!res.success) throw new Error(res.error || "Failed to fetch backups")
            return res.data || []
        },
    })
}

// ==================== Mutations ====================

export function useCreateBackup() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async () => {
            const res = await createBackup()
            if (!res.success) throw new Error(res.error || "Failed to create backup")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(`Backup created: ${data.filename} (${data.size_human})`)
            queryClient.invalidateQueries({ queryKey: queryKeys.backups })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useDeleteBackup() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (filename: string) => {
            const res = await deleteBackup(filename)
            if (!res.success) throw new Error(res.error || "Failed to delete backup")
        },
        onSuccess: () => {
            toast.success("Backup deleted")
            queryClient.invalidateQueries({ queryKey: queryKeys.backups })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useRestoreBackup() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (file: File) => {
            const res = await restoreBackup(file)
            if (!res.success) throw new Error(res.error || "Failed to restore backup")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(
                `${data.message}. Safety backup: ${data.safety_backup}`,
                { duration: 15000 }
            )
            queryClient.invalidateQueries()
        },
        onError: (error: Error) => {
            toast.error(error.message, { duration: 10000 })
        },
    })
}

export function useRestoreFromExisting() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (filename: string) => {
            const res = await restoreFromExisting(filename)
            if (!res.success) throw new Error(res.error || "Failed to restore backup")
            return res.data!
        },
        onSuccess: (data) => {
            toast.success(
                `${data.message}. Safety backup: ${data.safety_backup}`,
                { duration: 15000 }
            )
            queryClient.invalidateQueries()
        },
        onError: (error: Error) => {
            toast.error(error.message, { duration: 10000 })
        },
    })
}
