import { api, type ApiResponse } from "@/lib/api"

export type AlertRuleType = "node_offline" | "node_crash_loop" | "high_cpu" | "high_disk"
export type AlertScope = "global" | "node_ids" | "tag"

export interface AlertThreshold {
    value?: number
    count?: number
    window_sec?: number
    duration_sec?: number
}

export interface AlertRule {
    id: number
    name: string
    rule_type: AlertRuleType
    scope: AlertScope
    scope_value?: string
    threshold: AlertThreshold
    cooldown_sec: number
    enabled: boolean
    description: string
    last_fired_at?: string | null
    created_at: string
    updated_at: string
}

export type AlertEventStatus = "fired" | "resolved"

export interface AlertEvent {
    id: number
    rule_id: number
    entity_key: string
    status: AlertEventStatus
    title: string
    message: string
    value_json?: string
    created_at: string
}

export async function listAlertRules(): Promise<ApiResponse<AlertRule[]>> {
    return api.get<AlertRule[]>(`/api/v1/alerts/rules`)
}

export async function setAlertRuleEnabled(id: number, enabled: boolean): Promise<ApiResponse<void>> {
    return api.patch<void>(`/api/v1/alerts/rules/${id}/enabled`, { enabled })
}

export async function setAlertRuleThreshold(
    id: number,
    threshold: AlertThreshold,
    cooldownSec?: number,
): Promise<ApiResponse<void>> {
    return api.patch<void>(`/api/v1/alerts/rules/${id}/threshold`, {
        threshold,
        cooldown_sec: cooldownSec,
    })
}

export async function testAlertRule(id: number): Promise<ApiResponse<{ message: string }>> {
    return api.post<{ message: string }>(`/api/v1/alerts/rules/${id}/test`)
}

export async function listAlertEvents(limit = 100): Promise<ApiResponse<AlertEvent[]>> {
    return api.get<AlertEvent[]>(`/api/v1/alerts/events?limit=${limit}`)
}
