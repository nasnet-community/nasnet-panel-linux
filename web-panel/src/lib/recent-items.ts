export type RecentKind = "page" | "user" | "subscription" | "node"

const KINDS: readonly string[] = ["page", "user", "subscription", "node"]

export interface RecentItem {
    kind: RecentKind
    label: string
    href: string
    /** Subscriptions have no route of their own; they reopen by link key. */
    key?: string
}

const KEY = "command-palette-recent"
const LIMIT = 8

function isRecentItem(value: unknown): value is RecentItem {
    if (!value || typeof value !== "object") return false
    const item = value as Record<string, unknown>
    return KINDS.includes(item.kind as string)
        && typeof item.label === "string"
        && typeof item.href === "string"
        && (item.key === undefined || typeof item.key === "string")
}

export function readRecent(): RecentItem[] {
    try {
        const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]")
        return Array.isArray(raw) ? raw.filter(isRecentItem).slice(0, LIMIT) : []
    } catch {
        return []
    }
}

export function pushRecent(item: RecentItem) {
    const id = item.key ?? item.href
    const next = [item, ...readRecent().filter((r) => (r.key ?? r.href) !== id)].slice(0, LIMIT)
    try {
        localStorage.setItem(KEY, JSON.stringify(next))
    } catch {
        // storage full or blocked; recents are a convenience
    }
}
