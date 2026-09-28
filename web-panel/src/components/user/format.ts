// Helpers shared by the user page. Times render in the viewer's zone.
import type { Subscription, User, UserDetails } from "@/lib/types"
import { deriveSubscription } from "@/lib/subscription-derived"

const GB = 1024 ** 3

/** "34.4 GB", "512 MB", "0 B". One decimal from GB up. */
export function fmtSize(bytes: number): string {
    if (!bytes || bytes <= 0) return "0 B"
    const units = ["B", "KB", "MB", "GB", "TB", "PB"]
    const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
    const v = bytes / 1024 ** i
    return `${i >= 3 ? v.toFixed(1).replace(/\.0$/, "") : Math.round(v)} ${units[i]}`
}

/** Axis ticks: "2 GB", "500 MB". */
export function fmtAxisSize(bytes: number): string {
    if (!bytes || bytes < 1) return "0"
    if (bytes >= GB) return `${+(bytes / GB).toFixed(1)} GB`
    return `${Math.round(bytes / 1024 ** 2)} MB`
}

/** "Oct 27" (with the year when it isn't this year). */
export function fmtDay(v: string | number | Date): string {
    const d = new Date(v)
    const opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" }
    if (d.getFullYear() !== new Date().getFullYear()) opts.year = "numeric"
    return d.toLocaleDateString([], opts)
}

/** A YYYY-MM-DD date from the hub (UTC day) as "Oct 3". */
export function fmtUTCDay(ymd: string): string {
    const [y, m, d] = ymd.split("-").map(Number)
    return new Date(y, m - 1, d).toLocaleDateString([], { month: "short", day: "numeric" })
}

/** "Sun, Sep 13" / "Today" / "Yesterday" for timeline day headers. */
export function fmtDayHeader(v: string | number | Date): string {
    const d = new Date(v)
    const today = new Date()
    const yday = new Date(Date.now() - 86_400_000)
    if (d.toDateString() === today.toDateString()) return "Today"
    if (d.toDateString() === yday.toDateString()) return "Yesterday"
    const opts: Intl.DateTimeFormatOptions = { weekday: "short", month: "short", day: "numeric" }
    if (d.getFullYear() !== today.getFullYear()) opts.year = "numeric"
    return d.toLocaleDateString([], opts)
}

export function fmtClock(v: string | number | Date): string {
    return new Date(v).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false })
}

export function daysUntil(v: string | Date): number {
    return Math.ceil((new Date(v).getTime() - Date.now()) / 86_400_000)
}

export function plural(n: number, one: string, many = `${one}s`) {
    return `${n} ${n === 1 ? one : many}`
}

/** Name to show: first/last name, then @username, then "User #id". */
export function displayName(u: Pick<UserDetails, "id" | "username" | "first_name" | "last_name">): string {
    const part = (s?: string) => (s && s !== "-" ? s.trim() : "")
    const name = [part(u.first_name), part(u.last_name)].filter(Boolean).join(" ")
    if (name) return name
    if (u.username) return `@${u.username}`
    return `User #${u.id}`
}

export function languageName(code?: string): string {
    if (!code) return "—"
    try {
        return new Intl.DisplayNames(["en"], { type: "language" }).of(code) ?? code.toUpperCase()
    } catch {
        return code.toUpperCase()
    }
}

const TELEGRAM_HANDLE = /^[A-Za-z][A-Za-z0-9_]{3,31}$/
const BOT_FALLBACK_NAME = /^user_\d+$/

/** The user's public Telegram username; null for stand-ins, the bot's
 *  `user_<id>` fallback name and imported names that aren't handles. */
export function telegramHandle(u: Pick<User, "username" | "telegram_id">): string | null {
    if (u.telegram_id <= 0 || !u.username) return null
    if (BOT_FALLBACK_NAME.test(u.username) || !TELEGRAM_HANDLE.test(u.username)) return null
    return u.username
}

/** t.me for a public username, the numeric deep link without one, null without Telegram. */
export function telegramLink(u: Pick<User, "username" | "telegram_id">): string | null {
    const handle = telegramHandle(u)
    if (handle) return `https://t.me/${handle}`
    if (u.telegram_id > 0) return `tg://user?id=${u.telegram_id}`
    return null
}

/** The label, falling back to "Subscription #id". */
export function subscriptionName(s: Subscription): string {
    return s.label || `Subscription #${s.id}`
}

/** "30 days" from start to the effective end, or "No end date". */
export function periodLabel(s: Subscription): string {
    const d = deriveSubscription(s)
    if (!d?.effectiveEndDate || !s.start_date) return "No end date"
    const days = Math.round((new Date(d.effectiveEndDate).getTime() - new Date(s.start_date).getTime()) / 86_400_000)
    return days > 0 ? plural(days, "day") : "No end date"
}

/** "50 GB · 30 days" / "Unlimited data · No end date". */
export function limitLine(s: Subscription): string {
    const limit = deriveSubscription(s)?.effectiveDataLimit ?? s.data_limit
    return `${limit > 0 ? fmtSize(limit) : "Unlimited data"} · ${periodLabel(s)}`
}

export type PanelState = "exhausted" | "expiring" | "paused" | "banPaused" | "healthy"

export const isEnded = (s: Subscription) => s.status === "expired" || s.status === "cancelled"

/** Which state buttons a live subscription panel shows first. A ban's pause
 *  is locked only while the user is still banned; after that it's a pause. */
export function panelState(s: Subscription, userBanned = false): PanelState {
    if (s.status === "traffic_exhausted") return "exhausted"
    if (s.status === "paused") return s.pause_reason === "ban" && userBanned ? "banPaused" : "paused"
    const d = deriveSubscription(s)
    if (d && !d.isUnlimitedExpiry && d.effectiveEndDate && daysUntil(d.effectiveEndDate) <= 3) return "expiring"
    return "healthy"
}

/** Offset of the viewer's zone from UTC in whole hours (half-hour zones round). */
export function localHourOffset(): number {
    return Math.round(-new Date().getTimezoneOffset() / 60)
}
