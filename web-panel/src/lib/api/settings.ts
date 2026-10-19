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
    /** table size with indexes; Postgres only */
    size_bytes?: number | null
}

export interface CertInfo {
    issuer?: string
    subject?: string
    domains?: string[]
    not_after: string
    days_left: number
}

export interface BackupRunResult {
    at: string
    ok: boolean
    sent: boolean
    size_bytes?: number
    /** "too_big", "bot_off", or the error */
    reason?: string
}

/** What the running panel has in effect, as opposed to what is saved. */
export interface SettingsStatus {
    hub: { started_at: string; version: string; port: number }
    /** restart-only keys whose saved value differs from the one the panel booted with */
    restart_pending: string[]
    bot: {
        state: "disabled" | "starting" | "running" | "rejected"
        username?: string
        since?: string
        error?: string
        route: "outbound_proxy" | "legacy_proxy" | "direct"
        legacy?: string
        token_set: boolean
    }
    https: { mode: "files" | "acme" | "plain"; error?: string; cert?: CertInfo; on_disk?: CertInfo }
    reverse_hub: { listening: boolean; port: number; connected: number }
    backup: { last_run_at?: string; result?: BackupRunResult }
    maintenance: { since?: string; active: number; reach: number }
    admin: { username: string; chats: number }
    metrics: { endpoint: boolean }
    database: { size_bytes?: number }
}

export interface SettingsImportResult {
    applied: string[]
    /** keys this panel doesn't have */
    unknown: string[]
    /** admin-only and protected keys */
    refused: string[]
    /** secrets, masked in the export */
    masked: number
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

    importAll: async (settings: Pick<Setting, "key" | "value">[]): Promise<SettingsImportResult> => {
        const resp = await api.post<SettingsImportResult>("/api/v1/settings/import", settings);
        return resp.data ?? { applied: [], unknown: [], refused: [], masked: 0 };
    },

    /** Saves only the given keys. */
    save: async (values: Record<string, string>): Promise<void> => {
        await api.put("/api/v1/settings", Object.entries(values).map(([key, value]) => ({ key, value })));
    },

    getStatus: async (): Promise<SettingsStatus> => {
        const resp = await api.get<SettingsStatus>("/api/v1/settings/status");
        if (!resp.data) throw new Error(resp.error || "Couldn't load the panel's status");
        return resp.data;
    },

    testProxy: async (url: string): Promise<{ reachable: boolean; status?: number; error?: string }> => {
        const resp = await api.post<never>("/api/v1/settings/test-proxy", { url }) as unknown as { success: boolean; reachable?: boolean; status?: number; error?: string };
        return { reachable: !!resp.reachable, status: resp.status, error: resp.error };
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
