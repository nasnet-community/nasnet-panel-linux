import { useState, useRef, useEffect } from "react"
import { useUserActivityInfinite } from "@/lib/queries"
import type { UserActivityEvent } from "@/lib/types"
import { Skeleton } from "@/components/ui/skeleton"
import {
    HiOutlineCash,
    HiOutlineGlobeAlt,
    HiOutlineShieldCheck,
    HiOutlineBan,
    HiOutlinePencil,
    HiOutlinePlus,
    HiOutlineRefresh,
} from "react-icons/hi"
import { cn } from "@/lib/utils"

interface ActivityFeedProps {
    userId: number
}

function getActionIcon(action: string) {
    if (action.includes("ban")) return { icon: HiOutlineBan, color: "text-red-500 bg-red-500/10" }
    if (action.includes("admin")) return { icon: HiOutlineShieldCheck, color: "text-purple-500 bg-purple-500/10" }
    if (action.includes("balance") || action.includes("payment")) return { icon: HiOutlineCash, color: "text-emerald-500 bg-emerald-500/10" }
    if (action.includes("subscription") || action.includes("renew") || action.includes("extend")) return { icon: HiOutlineGlobeAlt, color: "text-blue-500 bg-blue-500/10" }
    if (action.includes("create") || action.includes("add")) return { icon: HiOutlinePlus, color: "text-primary bg-primary/10" }
    if (action.includes("update") || action.includes("edit") || action.includes("notes")) return { icon: HiOutlinePencil, color: "text-amber-500 bg-amber-500/10" }
    return { icon: HiOutlineRefresh, color: "text-muted-foreground bg-muted" }
}

function formatAction(action: string): string {
    // Map "entity.verb" to human-readable "Entity Verb" format
    const actionMap: Record<string, string> = {
        "payment.approve": "Payment Approved",
        "payment.reject": "Payment Rejected",
        "payment.refund": "Payment Refunded",
        "payment.create": "Payment Created",
        "subscription.create": "Subscription Created",
        "subscription.extend": "Subscription Extended",
        "subscription.pause": "Subscription Paused",
        "subscription.resume": "Subscription Resumed",
        "subscription.revoke": "Subscription Revoked",
        "subscription.renew": "Subscription Renewed",
        "subscription.reset_data": "Data Usage Reset",
        "subscription.set_data_limit": "Data Limit Changed",
        "subscription.set_expiry": "Expiry Changed",
        "subscription.delete": "Subscription Deleted",
        "user.ban": "User Banned",
        "user.unban": "User Unbanned",
        "user.set_admin": "Admin Status Changed",
        "user.add_balance": "Balance Added",
        "user.set_balance": "Balance Set",
        "user.update_notes": "Notes Updated",
        "user.update_telegram_id": "Telegram ID Updated",
        "user.create": "User Created",
        "account.bulk_action": "Bulk Account Action",
        "subscription.bulk_action": "Bulk Subscription Action",
    }

    if (actionMap[action]) return actionMap[action]

    // Fallback: extract entity + verb
    const parts = action.split(".")
    if (parts.length === 2) {
        const entity = parts[0].charAt(0).toUpperCase() + parts[0].slice(1)
        const verb = parts[1].replace(/_/g, " ").replace(/\b\w/g, c => c.toUpperCase())
        return `${entity} ${verb}`
    }

    return action.replace(/_/g, " ").replace(/\b\w/g, c => c.toUpperCase())
}

function relativeTime(dateStr: string): string {
    const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000)
    if (seconds < 60) return "just now"
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
    if (seconds < 604800) return `${Math.floor(seconds / 86400)}d ago`
    return new Date(dateStr).toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

function DiffBlock({ oldValues, newValues }: { oldValues?: string; newValues?: string }) {
    const [expanded, setExpanded] = useState(false)
    if (!oldValues && !newValues) return null

    let oldObj: Record<string, any> = {}
    let newObj: Record<string, any> = {}
    try { if (oldValues) oldObj = JSON.parse(oldValues) } catch { /* malformed audit payload — keep {} */ }
    try { if (newValues) newObj = JSON.parse(newValues) } catch { /* malformed audit payload — keep {} */ }

    const allKeys = [...new Set([...Object.keys(oldObj), ...Object.keys(newObj)])]
    if (allKeys.length === 0) return null

    return (
        <div className="mt-1.5">
            <button
                onClick={() => setExpanded(!expanded)}
                className="text-[10px] text-muted-foreground/50 hover:text-muted-foreground transition-colors"
            >
                {expanded ? "Hide changes ▲" : "Show changes ▼"}
            </button>
            {expanded && (
                <div className="bg-muted/30 rounded-md p-2 mt-1 font-mono text-[10px] space-y-0.5">
                    {allKeys.map((key) => {
                        const oldVal = oldObj[key]
                        const newVal = newObj[key]
                        if (oldVal !== undefined && newVal === undefined) {
                            return <div key={key} className="text-red-400">- {key}: {String(oldVal)}</div>
                        }
                        if (oldVal === undefined && newVal !== undefined) {
                            return <div key={key} className="text-emerald-400">+ {key}: {String(newVal)}</div>
                        }
                        if (String(oldVal) !== String(newVal)) {
                            return (
                                <div key={key}>
                                    <div className="text-red-400">- {key}: {String(oldVal)}</div>
                                    <div className="text-emerald-400">+ {key}: {String(newVal)}</div>
                                </div>
                            )
                        }
                        return null
                    })}
                </div>
            )}
        </div>
    )
}

function ActivityItem({ event }: { event: UserActivityEvent }) {
    const { icon: Icon, color } = getActionIcon(event.action)
    const entityRef = event.entity_type && event.entity_id
        ? `#${event.entity_id}`
        : null

    return (
        <div className="flex gap-3 group">
            <div className="flex flex-col items-center">
                <div className={cn("w-6 h-6 rounded-full flex items-center justify-center shrink-0", color)}>
                    <Icon className="w-3 h-3" />
                </div>
                <div className="w-px flex-1 bg-border group-last:bg-transparent" />
            </div>
            <div className="pb-3 min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                    <p className="text-[13px] font-medium truncate">
                        {formatAction(event.action)}
                        {entityRef && (
                            <span className="text-[11px] text-muted-foreground/60 font-normal ml-1">{entityRef}</span>
                        )}
                    </p>
                    <span className="text-[10px] text-muted-foreground/50 whitespace-nowrap shrink-0">{relativeTime(event.created_at)}</span>
                </div>
                {event.actor_name && (
                    <p className="text-[11px] text-muted-foreground/50 mt-0.5">{event.actor_name}</p>
                )}
                <DiffBlock oldValues={event.old_values} newValues={event.new_values} />
            </div>
        </div>
    )
}

const categories = [
    { key: "all", label: "All" },
    { key: "payment", label: "Payments" },
    { key: "subscription", label: "Subs" },
    { key: "user", label: "Admin" },
]

export function ActivityFeed({ userId }: ActivityFeedProps) {
    const [categoryFilter, setCategoryFilter] = useState<string>("all")
    const sentinelRef = useRef<HTMLDivElement>(null)
    const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useUserActivityInfinite(userId)
    const events = data?.pages.flatMap(p => p.data) ?? []

    useEffect(() => {
        const sentinel = sentinelRef.current
        if (!sentinel || !hasNextPage) return
        const observer = new IntersectionObserver(([entry]) => {
            if (entry.isIntersecting) fetchNextPage()
        })
        observer.observe(sentinel)
        return () => observer.disconnect()
    }, [hasNextPage, fetchNextPage])

    if (isLoading) {
        return (
            <div className="space-y-3">
                {[1, 2, 3].map(i => (
                    <div key={i} className="flex gap-3">
                        <Skeleton className="w-7 h-7 rounded-full shrink-0" />
                        <div className="flex-1 space-y-1">
                            <Skeleton className="h-4 w-32" />
                            <Skeleton className="h-3 w-20" />
                        </div>
                    </div>
                ))}
            </div>
        )
    }

    if (!events || events.length === 0) {
        return (
            <p className="text-xs text-muted-foreground/60 text-center py-4">No activity yet</p>
        )
    }

    const filtered = categoryFilter === "all" ? events : events.filter(e => e.action.startsWith(categoryFilter + "."))

    return (
        <div>
            <div className="flex gap-1.5 mb-4 overflow-x-auto">
                {categories.map((cat) => (
                    <button
                        key={cat.key}
                        onClick={() => setCategoryFilter(cat.key)}
                        className={cn(
                            "px-3 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap",
                            categoryFilter === cat.key
                                ? "bg-foreground text-background"
                                : "text-muted-foreground hover:text-foreground hover:bg-muted/50 border border-border"
                        )}
                    >
                        {cat.label}
                    </button>
                ))}
            </div>
            {filtered.map(event => (
                <ActivityItem key={event.id} event={event} />
            ))}
            <div ref={sentinelRef} className="h-1" />
            {isFetchingNextPage && (
                <p className="text-xs text-muted-foreground text-center py-2">Loading...</p>
            )}
        </div>
    )
}
