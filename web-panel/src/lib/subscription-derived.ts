import { useMemo } from "react"
import type { Subscription } from "@/lib/types"

export interface SubscriptionDerived {
    effectiveDataLimit: number          // bytes; 0 = unlimited
    effectiveBandwidth: number           // Mbps; 0 = unlimited
    effectiveEndDate: string | undefined // ISO string, or undefined when unlimited
    isUnlimitedExpiry: boolean
    isUnlimitedData: boolean
    daysRemaining: number                // clamped to >= 0; meaningless when isUnlimitedExpiry
    usagePercent: number                 // 0..100, clamped
    isActive: boolean
    limitDisplay: string                 // "12 GB" or "Unlimited"
    bandwidthDisplay: string             // "50 Mbps" or "Unlimited"
}

function getDaysRemaining(endDate?: string): number {
    if (!endDate) return 0
    const end = new Date(endDate).getTime()
    if (Number.isNaN(end)) return 0
    const diff = end - Date.now()
    return Math.max(0, Math.ceil(diff / 86_400_000))
}

function formatBytes(bytes: number): string {
    if (bytes <= 0) return "Unlimited"
    const units = ["B", "KB", "MB", "GB", "TB"]
    const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
    return `${(bytes / Math.pow(1024, i)).toFixed(i >= 3 ? 1 : 0)} ${units[i]}`
}

export function deriveSubscription(sub: Subscription | null | undefined): SubscriptionDerived | null {
    if (!sub) return null

    const effectiveDataLimit = sub.custom_data_limit ?? sub.data_limit
    const effectiveBandwidth = sub.custom_bandwidth_limit ?? 0

    const isUnlimitedExpiry = !!sub.is_end_date_custom && !sub.custom_end_date
    const effectiveEndDate = isUnlimitedExpiry
        ? undefined
        : (sub.is_end_date_custom ? sub.custom_end_date : sub.end_date)

    const isUnlimitedData = effectiveDataLimit <= 0
    const usagePercent = isUnlimitedData
        ? 0
        : Math.min(100, (sub.data_used / effectiveDataLimit) * 100)

    return {
        effectiveDataLimit,
        effectiveBandwidth,
        effectiveEndDate,
        isUnlimitedExpiry,
        isUnlimitedData,
        daysRemaining: isUnlimitedExpiry ? 0 : getDaysRemaining(effectiveEndDate),
        usagePercent,
        isActive: sub.status === "active",
        limitDisplay: isUnlimitedData ? "Unlimited" : formatBytes(effectiveDataLimit),
        bandwidthDisplay: effectiveBandwidth > 0 ? `${effectiveBandwidth} Mbps` : "Unlimited",
    }
}

export function useSubscriptionDerived(sub: Subscription | null | undefined): SubscriptionDerived | null {
    return useMemo(() => deriveSubscription(sub), [sub])
}
