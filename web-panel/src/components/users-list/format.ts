import type { UserListItem, UserListSubscription } from "@/lib/types"
import { fmtDay, telegramHandle } from "@/components/user/format"

const GB = 1024 ** 3
const HOUR = 3_600_000
const DAY = 86_400_000
export const ENDING_DAYS = 7
export const ENDING_RATIO = 0.8

/** GB with one decimal: "12.4"; "0" for nothing. */
export function gb(bytes: number): string {
    if (!bytes || bytes <= 0) return "0"
    return (bytes / GB).toFixed(1)
}

/** A limit in whole GB: "50"; one decimal under 1 GB. */
export function gbLimit(bytes: number): string {
    const v = bytes / GB
    return v >= 1 ? String(Math.round(v)) : v.toFixed(1)
}

export type IdentityKind = "telegram" | "no-username" | "none"

export function identityKind(u: Pick<UserListItem, "username" | "telegram_id">): IdentityKind {
    if (u.telegram_id <= 0) return "none"
    return telegramHandle(u) ? "telegram" : "no-username"
}

/** First + last name, else the stored username (never with "@"), else "User #id". */
export function listName(u: Pick<UserListItem, "id" | "username" | "first_name" | "last_name">): string {
    const part = (s?: string) => (s && s !== "-" ? s.trim() : "")
    const name = [part(u.first_name), part(u.last_name)].filter(Boolean).join(" ")
    return name || u.username || `User #${u.id}`
}

/** "just now", "12m ago", "5h ago", "3d ago", "2mo ago", "1y ago". */
export function fmtAgo(iso: string, now = Date.now()): string {
    const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000))
    if (s < 60) return "just now"
    const m = Math.floor(s / 60)
    if (m < 60) return `${m}m ago`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h}h ago`
    const d = Math.floor(h / 24)
    if (d < 30) return `${d}d ago`
    const mo = Math.floor(d / 30)
    if (mo < 12) return `${mo}mo ago`
    return `${Math.floor(mo / 12)}y ago`
}

export interface TimeLeft { text: string; warn: boolean; muted: boolean }

/** "21d left", "5h left", "overdue", or a muted "no end date". Amber at 7 days or less. */
export function timeLeft(endISO: string | null, now = Date.now()): TimeLeft {
    if (!endISO) return { text: "no end date", warn: false, muted: true }
    const ms = new Date(endISO).getTime() - now
    if (!Number.isFinite(ms)) return { text: "no end date", warn: false, muted: true }
    if (ms < 0) return { text: "overdue", warn: true, muted: false }
    const warn = ms <= ENDING_DAYS * DAY
    const days = Math.floor(ms / DAY)
    if (days >= 1) return { text: `${days}d left`, warn, muted: false }
    return { text: `${Math.max(1, Math.floor(ms / HOUR))}h left`, warn, muted: false }
}

export interface DataUse { used: string; limit: string; pct: number; warn: boolean }

/** Usage of a limited subscription; undefined means unlimited data. */
export function dataUse(s: Pick<UserListSubscription, "data_used" | "data_limit">): DataUse | undefined {
    if (!s.data_limit || s.data_limit <= 0) return undefined
    const ratio = Math.max(0, s.data_used) / s.data_limit
    return { used: gb(s.data_used), limit: gbLimit(s.data_limit), pct: Math.min(100, Math.round(ratio * 100)), warn: ratio >= ENDING_RATIO }
}

export function subscriptionTitle(s: Pick<UserListSubscription, "id" | "label">): { title: string; muted: boolean } {
    return s.label ? { title: s.label, muted: false } : { title: `Subscription #${s.id}`, muted: true }
}

export type SubTone = "active" | "ending" | "exhausted" | "expired" | "paused" | "cancelled"

export interface SubLine {
    tone: SubTone
    title: string
    titleMuted: boolean
    /** Other active subscriptions beyond the shown one. */
    extra: number
    live: boolean
    time?: TimeLeft
    /** Live only; undefined means unlimited data. */
    data?: DataUse
    /** Not live: "Out of data", "Expired Aug 27", "Paused". */
    reason?: string
}

/** What the Subscription cell shows, or null for "—". */
export function subLine(u: UserListItem, now = Date.now()): SubLine | null {
    const s = u.subscription
    if (!s) return null
    const { title, muted } = subscriptionTitle(s)
    if (s.status === "active") {
        const time = timeLeft(s.end_date, now)
        const data = dataUse(s)
        return {
            tone: time.warn || data?.warn ? "ending" : "active",
            title,
            titleMuted: muted,
            extra: Math.max(0, u.active_subscriptions - 1),
            live: true,
            time,
            data,
        }
    }
    const base = { title, titleMuted: muted, extra: 0, live: false }
    switch (s.status) {
        case "traffic_exhausted":
            return { ...base, tone: "exhausted", reason: "Out of data" }
        case "expired":
            return { ...base, tone: "expired", reason: s.end_date ? `Expired ${fmtDay(s.end_date)}` : "Expired" }
        case "paused":
            return { ...base, tone: "paused", reason: s.pause_reason === "ban" ? "Paused by ban" : "Paused" }
        default:
            return { ...base, tone: "cancelled", reason: "Cancelled" }
    }
}

export const TONE_DOT: Record<SubTone, string> = {
    active: "bg-sub-active",
    ending: "bg-sub-expiring",
    exhausted: "bg-sub-exhausted",
    expired: "bg-sub-expired",
    paused: "bg-sub-paused",
    cancelled: "bg-sub-cancelled",
}

export type SeenTone = "online" | "recent" | "week" | "old" | "none"

export function seenLine(u: UserListItem, now = Date.now()): { text: string; tone: SeenTone; title?: string } {
    const exact = u.last_active_at ? new Date(u.last_active_at).toLocaleString() : undefined
    if (u.online) return { text: "online", tone: "online", title: exact }
    if (!u.last_active_at) return u.total_subscriptions > 0 ? { text: "Never connected", tone: "old" } : { text: "—", tone: "none" }
    const age = now - new Date(u.last_active_at).getTime()
    const tone: SeenTone = age < DAY ? "recent" : age < 7 * DAY ? "week" : "old"
    return { text: fmtAgo(u.last_active_at, now), tone, title: exact }
}

export const SEEN_TEXT: Record<SeenTone, string> = {
    online: "text-status-success",
    recent: "text-status-success",
    week: "text-text-secondary",
    old: "text-muted-foreground",
    none: "text-muted-foreground/60",
}
