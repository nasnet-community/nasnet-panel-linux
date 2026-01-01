import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
    listAlertRules,
    setAlertRuleEnabled,
    setAlertRuleThreshold,
    testAlertRule,
    listAlertEvents,
    type AlertRule,
    type AlertEvent,
    type AlertThreshold,
} from "@/lib/api/alerts"
import { toast } from "sonner"

export const alertKeys = {
    all: ["alerts"] as const,
    rules: () => [...alertKeys.all, "rules"] as const,
    events: (limit: number) => [...alertKeys.all, "events", limit] as const,
}

export function useAlertRules() {
    return useQuery<AlertRule[]>({
        queryKey: alertKeys.rules(),
        queryFn: async () => {
            const res = await listAlertRules()
            if (!res.success) throw new Error(res.error || "Failed to fetch alert rules")
            return res.data || []
        },
    })
}

export function useAlertEvents(limit = 100) {
    return useQuery<AlertEvent[]>({
        queryKey: alertKeys.events(limit),
        queryFn: async () => {
            const res = await listAlertEvents(limit)
            if (!res.success) throw new Error(res.error || "Failed to fetch alert events")
            return res.data || []
        },
        // Poll modestly — an operator watching the page wants fresh data.
        refetchInterval: 15_000,
    })
}

export function useSetAlertRuleEnabled() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, enabled }: { id: number; enabled: boolean }) => {
            const res = await setAlertRuleEnabled(id, enabled)
            if (!res.success) throw new Error(res.error || "Toggle failed")
        },
        onSuccess: (_d, vars) => {
            qc.invalidateQueries({ queryKey: alertKeys.rules() })
            toast.success(vars.enabled ? "Rule enabled" : "Rule disabled")
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useSetAlertRuleThreshold() {
    const qc = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, threshold, cooldownSec }: { id: number; threshold: AlertThreshold; cooldownSec?: number }) => {
            const res = await setAlertRuleThreshold(id, threshold, cooldownSec)
            if (!res.success) throw new Error(res.error || "Update failed")
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: alertKeys.rules() })
            toast.success("Threshold updated")
        },
        onError: (e: Error) => toast.error(e.message),
    })
}

export function useTestAlertRule() {
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await testAlertRule(id)
            if (!res.success) throw new Error(res.error || "Test failed")
        },
        onSuccess: () => toast.success("Test alert dispatched — check your Telegram"),
        onError: (e: Error) => toast.error(e.message),
    })
}
