import { api } from "@/lib/api";

export interface AuditLog {
    id: number;
    action: string;
    actor_id: number;
    actor_name: string;
    entity_type: string;
    entity_id: number;
    old_values: string;
    new_values: string;
    ip_address: string;
    request_id: string;
    source: string;
    created_at: string;
}

export interface AuditListParams {
    action?: string;
    entity_type?: string;
    entity_id?: number;
    actor_id?: number;
    date_from?: string;
    date_to?: string;
    offset?: number;
    limit?: number;
}

export interface AuditListResponse {
    success: boolean;
    data: AuditLog[];
    meta: {
        total: number;
        offset: number;
        limit: number;
    };
}

export const auditApi = {
    list: async (params: AuditListParams = {}): Promise<AuditListResponse> => {
        const searchParams = new URLSearchParams();
        if (params.action) searchParams.set("action", params.action);
        if (params.entity_type) searchParams.set("entity_type", params.entity_type);
        if (params.entity_id) searchParams.set("entity_id", String(params.entity_id));
        if (params.actor_id) searchParams.set("actor_id", String(params.actor_id));
        if (params.date_from) searchParams.set("date_from", params.date_from);
        if (params.date_to) searchParams.set("date_to", params.date_to);
        searchParams.set("offset", String(params.offset || 0));
        searchParams.set("limit", String(params.limit || 20));

        const qs = searchParams.toString();
        return await api.getRaw<AuditListResponse>(`/api/v1/admin/audit?${qs}`);
    },
};
