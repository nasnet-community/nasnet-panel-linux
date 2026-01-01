import { api, type ApiResponse } from "@/lib/api"
import { getApiBaseUrl } from "../config"

// ==================== Database Backup & Restore ====================

export interface BackupInfo {
    filename: string
    size: number
    size_human: string
    created_at: string
}

export interface RestoreResult {
    message: string
    safety_backup: string
    requires_restart: boolean
    recovered?: boolean
}

export async function createBackup(): Promise<ApiResponse<BackupInfo>> {
    return api.post<BackupInfo>("/api/v1/admin/backups")
}

export async function listBackups(): Promise<ApiResponse<BackupInfo[]>> {
    return api.get<BackupInfo[]>("/api/v1/admin/backups")
}

export async function deleteBackup(filename: string): Promise<ApiResponse<void>> {
    return api.delete<void>(`/api/v1/admin/backups/${encodeURIComponent(filename)}`)
}

export function getBackupDownloadUrl(filename: string): string {
    return `${getApiBaseUrl()}/api/v1/admin/backups/${encodeURIComponent(filename)}/download`
}

export async function restoreBackup(file: File): Promise<ApiResponse<RestoreResult>> {
    const formData = new FormData()
    formData.append("backup_file", file)
    return api.postForm<RestoreResult>("/api/v1/admin/backups/restore", formData)
}

export async function restoreFromExisting(filename: string): Promise<ApiResponse<RestoreResult>> {
    return api.post<RestoreResult>(`/api/v1/admin/backups/${encodeURIComponent(filename)}/restore`)
}

// ==================== Database Cleanup ====================

export interface CleanupResult {
    accounts_removed: number
    subscriptions_deleted: number
    users_deleted: number
    audit_logs_deleted: number
    notification_logs_deleted: number
    provisioning_tasks_deleted: number
    user_daily_usage_deleted: number
    node_stats_deleted: number
    used_tokens_deleted: number
    admins_reset: number
    conversations_deleted: number
}

export async function cleanupDatabase(): Promise<ApiResponse<CleanupResult>> {
    return api.post<CleanupResult>("/api/v1/admin/database/cleanup")
}

// ==================== Server Management ====================

export async function restartServer(): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>("/api/v1/admin/server/restart")
}
