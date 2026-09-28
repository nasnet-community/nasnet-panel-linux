import type { UserGroup } from "@/lib/types"

export type SortKey = "username" | "time_left" | "last_active_at" | "data_used" | "created_at"
export type SortOrder = "asc" | "desc"

export interface GroupDef {
    id: UserGroup
    label: string
    sort: SortKey
    order: SortOrder
    empty: { title: string; body: string }
}

export const GROUPS: GroupDef[] = [
    { id: "all", label: "All", sort: "created_at", order: "desc", empty: { title: "No users yet", body: "People show up here when they start the bot or when you import them." } },
    { id: "active", label: "Active", sort: "time_left", order: "asc", empty: { title: "Nobody is active", body: "Users show up here while they have an active subscription." } },
    { id: "ending", label: "Ending soon", sort: "time_left", order: "asc", empty: { title: "Nobody is ending soon", body: "Active users show up here when a subscription has 7 days or less left, or has used 80% of its data." } },
    { id: "lapsed", label: "Lapsed", sort: "last_active_at", order: "desc", empty: { title: "Nobody has lapsed", body: "Users show up here when they had subscriptions and none is active now." } },
    { id: "never", label: "Never subscribed", sort: "created_at", order: "desc", empty: { title: "Nobody here", body: "Telegram users who never had a subscription show up here." } },
    { id: "banned", label: "Banned", sort: "created_at", order: "desc", empty: { title: "Nobody is banned", body: "Banned users show up here." } },
]

export function groupDef(id: UserGroup): GroupDef {
    return GROUPS.find((g) => g.id === id) ?? GROUPS[0]
}

/** The direction a column sorts in on its first click. */
export const NATURAL_ORDER: Record<SortKey, SortOrder> = {
    username: "asc", time_left: "asc", last_active_at: "desc", data_used: "desc", created_at: "desc",
}

export const SORT_LABEL: Record<SortKey, string> = {
    created_at: "Joined", username: "Name", time_left: "Time left", last_active_at: "Last seen", data_used: "Data used",
}

/** Groups where nobody has an active subscription, so "time left" can't sort. */
export const NO_TIME_LEFT: UserGroup[] = ["lapsed", "never", "banned"]
