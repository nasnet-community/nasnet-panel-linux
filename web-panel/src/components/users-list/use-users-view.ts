import { useCallback, useEffect, useMemo, useState } from "react"
import { useSearchParams } from "react-router"
import type { UserGroup } from "@/lib/types"
import type { ListUsersParams } from "@/lib/api/users"
import { GROUPS, NATURAL_ORDER, groupDef, type SortKey, type SortOrder } from "./groups"
import { rememberUsersView } from "./last-view"

export type TelegramFilter = "linked" | "unlinked"
export type SeenFilter = "online" | "24h" | "7d" | "30d" | "over30d" | "never"
export type JoinedFilter = "7d" | "30d" | "90d" | "custom"

export interface UsersFilters {
    telegram?: TelegramFilter
    seen?: SeenFilter
    joined?: JoinedFilter
    from?: string
    to?: string
    admin?: boolean
}

export interface UsersView extends UsersFilters {
    group: UserGroup
    q: string
    sort: SortKey
    order: SortOrder
    page: number
}

export const PER_PAGE_OPTIONS = [10, 25, 50, 100] as const
const PER_PAGE_KEY = "users-per-page"
const SORT_KEYS: SortKey[] = ["username", "time_left", "last_active_at", "data_used", "created_at"]
const DATE = /^\d{4}-\d{2}-\d{2}$/

function pick<T extends string>(value: string | null, allowed: readonly T[]): T | undefined {
    return allowed.find((a) => a === value)
}

/** Reads the view from the URL; anything unknown falls back to its default. */
export function readView(sp: URLSearchParams): UsersView {
    const group = pick(sp.get("group"), GROUPS.map((g) => g.id)) ?? "all"
    const def = groupDef(group)
    const sort = pick(sp.get("sort"), SORT_KEYS) ?? def.sort
    const order = pick(sp.get("order"), ["asc", "desc"] as const) ?? (sort === def.sort ? def.order : NATURAL_ORDER[sort])
    const joined = pick(sp.get("joined"), ["7d", "30d", "90d", "custom"] as const)
    const from = sp.get("from") ?? ""
    const to = sp.get("to") ?? ""
    return {
        group,
        q: sp.get("q") ?? "",
        sort,
        order,
        page: Math.max(1, Math.floor(Number(sp.get("page")) || 1)),
        telegram: pick(sp.get("telegram"), ["linked", "unlinked"] as const),
        seen: pick(sp.get("seen"), ["online", "24h", "7d", "30d", "over30d", "never"] as const),
        joined,
        from: joined === "custom" && DATE.test(from) ? from : undefined,
        to: joined === "custom" && DATE.test(to) ? to : undefined,
        admin: sp.get("admin") === "1" || undefined,
    }
}

/** Writes a view back, leaving defaults out so plain /users stays plain. */
export function writeView(v: UsersView): URLSearchParams {
    const sp = new URLSearchParams()
    const def = groupDef(v.group)
    if (v.group !== "all") sp.set("group", v.group)
    if (v.q) sp.set("q", v.q)
    if (v.telegram) sp.set("telegram", v.telegram)
    if (v.seen) sp.set("seen", v.seen)
    if (v.joined) sp.set("joined", v.joined)
    if (v.joined === "custom" && v.from) sp.set("from", v.from)
    if (v.joined === "custom" && v.to) sp.set("to", v.to)
    if (v.admin) sp.set("admin", "1")
    if (v.sort !== def.sort || v.order !== def.order) {
        sp.set("sort", v.sort)
        sp.set("order", v.order)
    }
    if (v.page > 1) sp.set("page", String(v.page))
    return sp
}

export function filterCount(v: UsersFilters): number {
    return [v.telegram, v.seen, v.joined, v.admin].filter(Boolean).length
}

/** The list API's parameters for a view. */
export function toListParams(v: UsersView, perPage: number): ListUsersParams {
    return {
        page: v.page,
        per_page: perPage,
        search: v.q.trim() || undefined,
        group: v.group,
        telegram: v.telegram,
        seen: v.seen,
        joined: v.joined,
        joined_from: v.joined === "custom" ? v.from : undefined,
        joined_to: v.joined === "custom" ? v.to : undefined,
        admin: v.admin,
        sort: v.sort,
        order: v.order,
    }
}

function readPerPage(): number {
    try {
        const n = Number(localStorage.getItem(PER_PAGE_KEY))
        return (PER_PAGE_OPTIONS as readonly number[]).includes(n) ? n : 25
    } catch {
        return 25
    }
}

export function useUsersView() {
    const [searchParams, setSearchParams] = useSearchParams()
    const view = useMemo(() => readView(searchParams), [searchParams])
    useEffect(() => rememberUsersView(searchParams.toString()), [searchParams])
    const [perPage, setPerPageState] = useState(readPerPage)

    const update = useCallback((next: Partial<UsersView>, opts?: { replace?: boolean }) => {
        setSearchParams((prev) => writeView({ ...readView(prev), ...next }), { replace: opts?.replace })
    }, [setSearchParams])

    const setGroup = useCallback((group: UserGroup) => {
        const def = groupDef(group)
        update({ group, sort: def.sort, order: def.order, page: 1 })
    }, [update])

    // Typing replaces the history entry so Back doesn't step through keystrokes.
    const setSearch = useCallback((q: string) => update({ q, page: 1 }, { replace: true }), [update])
    const setFilters = useCallback((filters: UsersFilters) => update({ ...filters, page: 1 }), [update])
    const clearFilters = useCallback(() => update({
        telegram: undefined, seen: undefined, joined: undefined, from: undefined, to: undefined, admin: undefined, page: 1,
    }), [update])

    const setSort = useCallback((sort: SortKey) => {
        const order: SortOrder = sort === view.sort ? (view.order === "asc" ? "desc" : "asc") : NATURAL_ORDER[sort]
        update({ sort, order, page: 1 })
    }, [update, view.sort, view.order])
    const setSortTo = useCallback((sort: SortKey, order: SortOrder) => update({ sort, order, page: 1 }), [update])

    const setPage = useCallback((page: number, opts?: { replace?: boolean }) => update({ page }, opts), [update])
    const setPerPage = useCallback((n: number) => {
        setPerPageState(n)
        try {
            localStorage.setItem(PER_PAGE_KEY, String(n))
        } catch {
            // a preference only; the session value still applies
        }
        update({ page: 1 })
    }, [update])

    // The selection belongs to one view; any of these changing clears it.
    const scopeKey = [view.group, view.q, view.telegram, view.seen, view.joined, view.from, view.to, view.admin].join("|")

    return { view, perPage, scopeKey, setGroup, setSearch, setFilters, clearFilters, setSort, setSortTo, setPage, setPerPage }
}
