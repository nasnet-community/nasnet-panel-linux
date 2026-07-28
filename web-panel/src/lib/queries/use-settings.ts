import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { settingsApi } from "@/lib/api/settings"
import { queryKeys } from "./keys"
import type { Setting } from "@/lib/domain/setting"
import { toast } from "sonner"

// ==================== Queries ====================

// Get all settings (grouped by category)
export function useSettings() {
    return useQuery({
        queryKey: queryKeys.settings,
        queryFn: async () => {
            return await settingsApi.getAll()
        },
        staleTime: 5 * 60 * 1000, // 5 minutes — settings rarely change
    })
}

// ==================== Mutations ====================

// Update multiple settings
export function useUpdateSettings() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (settings: Setting[]) => {
            return await settingsApi.updateMany(settings)
        },
        onSuccess: () => {
            toast.success("Settings saved", {
                description: "System settings have been updated successfully.",
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.settings })
        },
        onError: () => {
            toast.error("Error saving settings", {
                description: "Failed to save changes. Please try again.",
            })
        },
    })
}

// Export all settings
export function useExportSettings() {
    return useMutation({
        mutationFn: async () => {
            return await settingsApi.exportAll()
        },
        onSuccess: (data) => {
            // Download as JSON file
            const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" })
            const url = URL.createObjectURL(blob)
            const a = document.createElement("a")
            a.href = url
            a.download = `settings-export-${new Date().toISOString().split("T")[0]}.json`
            a.click()
            URL.revokeObjectURL(url)
            toast.success("Settings exported", {
                description: "Settings have been downloaded as a JSON file.",
            })
        },
        onError: () => {
            toast.error("Export failed", {
                description: "Failed to export settings. Please try again.",
            })
        },
    })
}

// Import settings
export function useImportSettings() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (settings: Setting[]) => {
            return await settingsApi.importAll(settings)
        },
        onSuccess: (data) => {
            toast.success("Settings imported", {
                description: `${data.count} settings have been imported successfully.`,
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.settings })
        },
        onError: () => {
            toast.error("Import failed", {
                description: "Failed to import settings. Please check the file format.",
            })
        },
    })
}

// Change admin password
export function useChangePassword() {
    return useMutation({
        mutationFn: async (data: {
            current_password: string
            new_password: string
            confirm_password: string
        }) => {
            return await settingsApi.changePassword(data)
        },
        onSuccess: () => {
            toast.success("Password changed", {
                description: "Your admin password has been updated successfully.",
            })
        },
        onError: (error: Error) => {
            toast.error("Password change failed", {
                description: error.message || "Please check your current password and try again.",
            })
        },
    })
}

// ==================== Retention ====================

// Per-table row counts + oldest-row dates, shown under each retention field.
// Low churn (row counts only drift at the stats-sync cadence), so 1-minute
// staleTime is plenty; the cleanup mutation invalidates this key on success.
// `enabled` lets callers keep the hook call unconditional (React requires a
// stable hook order) while still skipping the request. Categories other than
// Data Retention pass false so they don't pay the query cost on mount.
export function useRetentionStats(enabled = true) {
    return useQuery({
        queryKey: queryKeys.retentionStats(),
        queryFn: async () => await settingsApi.getRetentionStats(),
        staleTime: 60 * 1000,
        enabled,
    })
}

// Trigger the retention sweep synchronously. Toasts the total-row summary
// and refreshes the stats query so the UI reflects post-cleanup sizes.
export function useRunRetentionCleanup() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async () => await settingsApi.runRetentionCleanup(),
        onSuccess: (result) => {
            const taskCount = result.task_count
            const total = result.total_rows
            if (total === 0) {
                toast.success("Nothing to clean", {
                    description: "All retention tables are already within their window.",
                })
            } else {
                toast.success(`Deleted ${total.toLocaleString()} rows`, {
                    description: `Cleaned up across ${taskCount} table${taskCount === 1 ? "" : "s"}.`,
                })
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.retentionStats() })
        },
        onError: () => {
            toast.error("Cleanup failed", {
                description: "Retention sweep did not complete. Check server logs.",
            })
        },
    })
}
