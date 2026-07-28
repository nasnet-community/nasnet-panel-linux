import { Setting, SettingsGrouped } from "../domain/setting";
import { api } from "@/lib/api";

export interface TLSTestResult {
    success: boolean
    error?: string
    subject?: string
    issuer?: string
    domains?: string[]
    not_before?: string
    not_after?: string
    days_until_expiry?: number
    expired?: boolean
    not_yet_valid?: boolean
    warning?: string
}

// RetentionStat is one row of the admin retention-stats endpoint. Shown
// under the corresponding field on the settings panel so admins can see row
// counts and oldest-row dates before changing a retention window.
export interface RetentionStat {
    setting_key: string
    table: string
    rows: number
    oldest_at?: string | null
}

export interface RetentionCleanupResult {
    deleted: Record<string, number>
    total_rows: number
    task_count: number
}

export const settingsApi = {
    getAll: async (): Promise<SettingsGrouped> => {
        return await api.getRaw<SettingsGrouped>("/api/v1/settings");
    },

    updateMany: async (settings: Setting[]): Promise<void> => {
        await api.put("/api/v1/settings", settings);
    },

    changePassword: async (data: {
        current_password: string;
        new_password: string;
        confirm_password: string;
    }): Promise<void> => {
        await api.post("/api/v1/auth/change-password", data);
    },

    exportAll: async (): Promise<Setting[]> => {
        return await api.getRaw<Setting[]>("/api/v1/settings/export");
    },

    importAll: async (settings: Setting[]): Promise<{ message: string; count: number }> => {
        const resp = await api.post<{ message: string; count: number }>("/api/v1/settings/import", settings);
        return resp.data ?? { message: "Imported", count: settings.length };
    },

    testTLS: async (certFile: string, keyFile: string): Promise<TLSTestResult> => {
        const resp = await api.post<TLSTestResult>("/api/v1/settings/test-tls", {
            cert_file: certFile,
            key_file: keyFile,
        });
        if (!resp.success || !resp.data) {
            return { success: false, error: resp.error || "Failed to test TLS certificate" };
        }
        return { ...resp.data, success: true };
    },

    getRetentionStats: async (): Promise<RetentionStat[]> => {
        const resp = await api.get<RetentionStat[]>("/api/v1/admin/retention/stats");
        return resp.data ?? [];
    },

    runRetentionCleanup: async (): Promise<RetentionCleanupResult> => {
        const resp = await api.post<RetentionCleanupResult>("/api/v1/admin/retention/cleanup", {});
        return (
            resp.data ?? { deleted: {}, total_rows: 0, task_count: 0 }
        );
    },
};
