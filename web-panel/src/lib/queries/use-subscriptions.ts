import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useMemo } from "react"
import {
    listSubscriptions,
    getSubscription,
    extendSubscription,
    pauseSubscription,
    resumeSubscription,
    revokeSubscription,
    setSubscriptionDataLimit,
    setSubscriptionExpiry,
    addSubscriptionData,
    resetSubscriptionData,
    renameSubscription,
    regenerateSubscriptionKey,
    regenerateUUID,
    createManualSubscription,
    assignSubscriptionUser,
    deleteSubscription,
    bulkSubscriptionAction,
    bulkSetBandwidthLimit,
    getSubscriptionCounts,
    setSubscriptionBandwidthLimit,
    setSubscriptionMaxDevices,
    setSubscriptionPanelPassword,
    assignSubscriptionToInbound,
} from "@/lib/admin-api"
import type { CreateManualSubscriptionRequest } from "@/lib/admin-api"
import {
    bulkManageInbounds,
    getBulkInboundSummary,
    getSubscriptionIPs,
    getSubscriptionUsageHistory,
    setSubscriptionUUID,
    type BulkManageInboundsRequest,
    type BulkInboundSummary,
    type UsageHistoryPoint,
} from "@/lib/api/subscriptions"
import { queryKeys } from "./keys"
import { toast } from "sonner"

// ==================== Types ====================

export interface UseSubscriptionsParams {
    status?: string
    page: number
    perPage: number
    search?: string
    planId?: number
    source?: string
    exhausted?: string   // "all" | "true" | "false"
    sort?: string
    order?: string
}

// ==================== Queries ====================

// List subscriptions with filters
export function useSubscriptions(params: UseSubscriptionsParams) {
    return useQuery({
        queryKey: queryKeys.subscriptionList(params),
        queryFn: async () => {
            const res = await listSubscriptions({
                status: params.status === "all" ? undefined : params.status,
                page: params.page,
                per_page: params.perPage,
                search: params.search || undefined,
                plan_id: params.planId,
                source: params.source && params.source !== "all" ? params.source : undefined,
                exhausted: params.exhausted && params.exhausted !== "all" ? params.exhausted === "true" : undefined,
                sort: params.sort || undefined,
                order: params.order || undefined,
            })
            if (!res.success) throw new Error(res.error || "Failed to fetch subscriptions")
            return Array.isArray(res.data) ? res.data : []
        },
    })
}

// Get single subscription
export function useSubscription(id: number) {
    return useQuery({
        queryKey: queryKeys.subscriptionDetails(id),
        queryFn: async () => {
            const res = await getSubscription(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch subscription")
            return res.data!
        },
        enabled: id > 0,
    })
}

// ==================== Mutations ====================

// Extend subscription
export function useExtendSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, days }: { id: number; days: number }) => {
            const res = await extendSubscription(id, days)
            if (!res.success) throw new Error(res.error || "Failed to extend subscription")
            return res.data
        },
        onSuccess: (_, { days }) => {
            toast.success(`Extended by ${days} days`)
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Pause subscription
export function usePauseSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await pauseSubscription(id)
            if (!res.success) throw new Error(res.error || "Failed to pause subscription")
            return res.data
        },
        onSuccess: () => {
            toast.success("Subscription paused")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Resume subscription
export function useResumeSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await resumeSubscription(id)
            if (!res.success) throw new Error(res.error || "Failed to resume subscription")
            return res.data
        },
        onSuccess: () => {
            toast.success("Subscription resumed")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Revoke subscription
export function useRevokeSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await revokeSubscription(id)
            if (!res.success) throw new Error(res.error || "Failed to revoke subscription")
        },
        onSuccess: () => {
            toast.success("Subscription revoked")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Set data limit
export function useSetDataLimit() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, limitGb }: { id: number; limitGb: number | null }) => {
            const res = await setSubscriptionDataLimit(id, limitGb)
            if (!res.success) throw new Error(res.error || "Failed to set data limit")
            return res.data
        },
        onSuccess: () => {
            toast.success("Data limit updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Set bandwidth limit
export function useSetBandwidthLimit() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, limitMbps }: { id: number; limitMbps: number | null }) => {
            const res = await setSubscriptionBandwidthLimit(id, limitMbps)
            if (!res.success) throw new Error(res.error || "Failed to set bandwidth limit")
            return res.data
        },
        onSuccess: () => {
            toast.success("Bandwidth limit updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Set per-subscription device limit (0 = inherit plan default)
export function useSetMaxDevices() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, maxDevices }: { id: number; maxDevices: number }) => {
            const res = await setSubscriptionMaxDevices(id, maxDevices)
            if (!res.success) throw new Error(res.error || "Failed to set device limit")
            return res.data
        },
        onSuccess: () => {
            toast.success("Device limit updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Set expiry date
export function useSetExpiry() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, expiryDate, unlimited }: { id: number; expiryDate: string | null; unlimited?: boolean }) => {
            const res = await setSubscriptionExpiry(id, expiryDate, unlimited)
            if (!res.success) throw new Error(res.error || "Failed to set expiry date")
            return res.data
        },
        onSuccess: (data, { unlimited }) => {
            toast.success(unlimited ? "Expiry set to unlimited" : "Expiry date updated")
            if (data) {
                // Update the specific subscription in the cache immediately
                queryClient.setQueryData(queryKeys.subscriptionDetails(data.id), data)

                // Also update the list if it exists
                queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            } else {
                queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            }
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Add data
export function useAddData() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, amountGb }: { id: number; amountGb: number }) => {
            const res = await addSubscriptionData(id, amountGb)
            if (!res.success) throw new Error(res.error || "Failed to add data")
            return res.data
        },
        onSuccess: (_, { amountGb }) => {
            toast.success(`Added ${amountGb} GB`)
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Reset data usage
export function useResetData() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await resetSubscriptionData(id)
            if (!res.success) throw new Error(res.error || "Failed to reset data usage")
            return res.data
        },
        onSuccess: () => {
            toast.success("Data usage reset")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Rename subscription
export function useRenameSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, label }: { id: number; label: string }) => {
            const res = await renameSubscription(id, label)
            if (!res.success) throw new Error(res.error || "Failed to rename subscription")
            return res.data
        },
        onSuccess: () => {
            toast.success("Subscription renamed")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Regenerate subscription key (URL only, keeps Xray UUID unchanged)
export function useRegenerateSubscriptionKey() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, key }: { id: number; key?: string }) => {
            const res = await regenerateSubscriptionKey(id, key)
            if (!res.success) throw new Error(res.error || "Failed to regenerate subscription key")
            return res.data
        },
        onSuccess: (data) => {
            toast.success("Subscription key updated")
            if (data) {
                queryClient.setQueryData(queryKeys.subscriptionDetails(data.id), data)
                queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            }
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Regenerate UUID (changes Xray credentials on nodes)
export function useRegenerateUUID() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await regenerateUUID(id)
            if (!res.success) throw new Error(res.error || "Failed to regenerate UUID")
            return res.data
        },
        onSuccess: (data) => {
            toast.success("UUID regenerated")
            if (data) {
                // Update the specific subscription in the cache immediately
                queryClient.setQueryData(queryKeys.subscriptionDetails(data.id), data)

                // Also update the list if it exists
                queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })

                // Refresh accounts so the UUID field updates
                queryClient.invalidateQueries({ queryKey: ["accounts", "subscription", data.id] })
            }
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Create manual subscription
export function useCreateManualSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (req: CreateManualSubscriptionRequest) => {
            const res = await createManualSubscription(req)
            if (!res.success) throw new Error(res.error || "Failed to create subscription")
            return res.data
        },
        onSuccess: () => {
            toast.success("Manual subscription created")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Assign user to subscription
export function useAssignSubscriptionUser() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ subId, userId }: { subId: number; userId: number | null }) => {
            const res = await assignSubscriptionUser(subId, userId)
            if (!res.success) throw new Error(res.error || "Failed to assign user")
            return res.data
        },
        onSuccess: (data) => {
            toast.success(data?.user_id ? "User assigned" : "User unlinked")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Delete subscription permanently
export function useDeleteSubscription() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (id: number) => {
            const res = await deleteSubscription(id)
            if (!res.success) throw new Error(res.error || "Failed to delete subscription")
        },
        onSuccess: () => {
            toast.success("Subscription deleted permanently")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Bulk subscription action
export function useBulkSubscriptionAction() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ action, ids }: { action: string; ids: number[] }) => {
            const res = await bulkSubscriptionAction(action, ids)
            if (!res.success) throw new Error(res.error || "Bulk action failed")
            return res.data!
        },
        onSuccess: (data, { action }) => {
            if (data.failed > 0) {
                toast.warning(`Bulk ${action}: ${data.succeeded} succeeded, ${data.failed} failed`)
            } else {
                toast.success(`Bulk ${action}: ${data.succeeded} succeeded`)
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useBulkSetBandwidthLimit() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ ids, bandwidthLimit }: { ids: number[]; bandwidthLimit: number | null }) => {
            const res = await bulkSetBandwidthLimit(ids, bandwidthLimit)
            if (!res.success) throw new Error(res.error || "Bulk bandwidth update failed")
            return res.data!
        },
        onSuccess: (data) => {
            if (data.failed > 0) {
                toast.warning(`Bandwidth: ${data.succeeded} updated, ${data.failed} failed`)
            } else {
                toast.success(`Bandwidth updated for ${data.succeeded} subscription${data.succeeded !== 1 ? "s" : ""}`)
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useBulkManageInbounds() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async (req: BulkManageInboundsRequest) => {
            const res = await bulkManageInbounds(req)
            if (!res.success) throw new Error(res.error || "Bulk inbound management failed")
            return res.data!
        },
        onSuccess: (data) => {
            const parts: string[] = []
            if (data.accounts_added > 0) parts.push(`${data.accounts_added} added`)
            if (data.accounts_marked_for_removal > 0) parts.push(`${data.accounts_marked_for_removal} marked for removal`)
            if (data.skipped > 0) parts.push(`${data.skipped} skipped`)
            const msg = `Inbounds updated for ${data.subscriptions_affected} subscription${data.subscriptions_affected !== 1 ? "s" : ""}${parts.length > 0 ? ": " + parts.join(", ") : ""}`
            if (data.errors && data.errors.length > 0) {
                toast.warning(msg + "\n" + data.errors.join("\n"))
            } else if (data.skipped > 0) {
                toast.warning(msg)
            } else {
                toast.success(msg)
            }
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            queryClient.invalidateQueries({ queryKey: ["accounts", "subscription"] })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useBulkInboundSummary(subscriptionIds: number[]) {
    return useQuery({
        queryKey: [...queryKeys.subscriptions, "bulk-inbound-summary", subscriptionIds],
        queryFn: async () => {
            const res = await getBulkInboundSummary(subscriptionIds)
            if (!res.success) throw new Error(res.error || "Failed to load inbound summary")
            return res.data!
        },
        enabled: subscriptionIds.length > 0,
    })
}

// Set panel password
export function useSetPanelPassword() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, mode, password }: { id: number; mode: "default" | "custom" | "disabled"; password?: string }) => {
            const res = await setSubscriptionPanelPassword(id, mode, password)
            if (!res.success) throw new Error(res.error || "Failed to update panel password")
        },
        onSuccess: () => {
            toast.success("Panel password updated")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Aggregate sub counts by status. Powers the sidebar context panel's
// portfolio distribution bar and the derived expiring-soon tiles.
export function useSubscriptionCounts() {
    return useQuery({
        queryKey: queryKeys.subscriptionCounts(),
        queryFn: async () => {
            const res = await getSubscriptionCounts()
            if (!res.success) throw new Error(res.error || "Failed to fetch subscription counts")
            return res.data!
        },
        refetchInterval: 30_000,
        staleTime: 15_000,
    })
}

// Connected IPs for a subscription. Polls while the details sheet is open so
// last_seen timestamps (and the 60s "active" badge derived from them) stay
// in sync with the 5s stats sync instead of freezing until the user refocuses.
export function useSubscriptionIPs(id: number | undefined) {
    return useQuery({
        queryKey: [...queryKeys.subscriptions, "ips", id],
        queryFn: async () => {
            if (!id) return []
            const res = await getSubscriptionIPs(id)
            if (!res.success) throw new Error(res.error || "Failed to fetch IPs")
            return res.data || []
        },
        enabled: !!id && id > 0,
        staleTime: 10_000,
        refetchInterval: 10_000,
    })
}

// Daily usage points for sparkline (returns empty array until history accrues).
export function useSubscriptionUsageHistory(id: number | undefined, days = 30) {
    return useQuery({
        queryKey: [...queryKeys.subscriptions, "usage-history", id, days],
        queryFn: async () => {
            if (!id) return [] as UsageHistoryPoint[]
            const res = await getSubscriptionUsageHistory(id, days)
            if (!res.success) throw new Error(res.error || "Failed to fetch usage history")
            return res.data || []
        },
        enabled: !!id && id > 0,
        staleTime: 5 * 60_000,
    })
}

// Atomic "set UUID across all accounts for this subscription". Replaces the
// prior client-side Promise.all(updateAccount) loop with a single request.
export function useSetSubscriptionUUID() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ id, uuid }: { id: number; uuid: string }) => {
            const res = await setSubscriptionUUID(id, uuid)
            if (!res.success) throw new Error(res.error || "Failed to set UUID")
            return res.data!
        },
        onSuccess: (data, { id }) => {
            toast.success(`UUID updated for ${data.updated} account${data.updated === 1 ? "" : "s"}`)
            queryClient.invalidateQueries({ queryKey: ["accounts", "subscription", id] })
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptionDetails(id) })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

export function useAssignSubscriptionToInbound() {
    const queryClient = useQueryClient()
    return useMutation({
        mutationFn: async ({ subscriptionId, inboundId }: { subscriptionId: number; inboundId: number }) => {
            const res = await assignSubscriptionToInbound(subscriptionId, inboundId)
            if (!res.success) throw new Error(res.error || "Failed to assign inbound")
        },
        onSuccess: () => {
            toast.success("Subscription assigned to inbound")
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
        },
        onError: (error: Error) => {
            toast.error(error.message)
        },
    })
}

// Count of active subscriptions whose end_date falls within `days`.
// Derived client-side from the existing subscription list query; capped
// at per_page=1000 — flagged as a known limit in the spec.
export function useSubsExpiringWithin(days: number) {
    const { data: subs, isLoading } = useSubscriptions({ status: "active", page: 1, perPage: 1000 })
    const count = useMemo(() => {
        if (!subs) return 0
        const cutoff = Date.now() + days * 24 * 3600 * 1000
        let n = 0
        for (const s of subs) {
            if (!s.end_date) continue
            const t = new Date(s.end_date).getTime()
            if (!Number.isFinite(t)) continue
            if (t < cutoff) n++
        }
        return n
    }, [subs, days])
    return { count, isLoading }
}

