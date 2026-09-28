import type { MouseEvent, ReactNode } from "react"
import { useNavigate } from "react-router"
import { ArrowDown, ArrowUp } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { fmtDay } from "@/components/user/format"
import { getApiBaseUrl } from "@/lib/config"
import { cn } from "@/lib/utils"
import type { UserGroup, UserListItem } from "@/lib/types"
import { Figure, LastSeenCell, SubscriptionCell, UserCell } from "./cells"
import { gb, listName } from "./format"
import { NO_TIME_LEFT, type SortKey, type SortOrder } from "./groups"
import { RowMenu, TelegramButton, type UserRowActions } from "./row-actions"

interface Column {
    key: string
    label: string
    sort: SortKey
    className: string
    align?: "right"
}

// Narrow screens drop columns by the table's own width, never the viewport's.
const COLUMNS: Column[] = [
    { key: "user", label: "User", sort: "username", className: "min-w-[220px]" },
    { key: "subscription", label: "Subscription", sort: "time_left", className: "w-[31%] min-w-[240px]" },
    { key: "seen", label: "Last seen", sort: "last_active_at", className: "w-[130px]" },
    { key: "data", label: "Data used", sort: "data_used", className: "hidden w-[104px] @min-[760px]:table-cell", align: "right" },
    { key: "joined", label: "Joined", sort: "created_at", className: "hidden w-[92px] @min-[900px]:table-cell" },
]

export interface UsersTableProps {
    users: UserListItem[]
    loading: boolean
    skeletonRows: number
    group: UserGroup
    sort: SortKey
    order: SortOrder
    onSort: (key: SortKey) => void
    selected: ReadonlyMap<number, boolean>
    onToggle: (user: UserListItem) => void
    onTogglePage: (users: UserListItem[], select: boolean) => void
    actions: UserRowActions
    empty: ReactNode
}

function SkeletonRow() {
    return (
        <tr className="h-[52px] border-b border-border/40">
            <td className="pl-4"><Skeleton className="size-4" /></td>
            <td className="pr-3">
                <div className="flex items-center gap-2.5">
                    <Skeleton className="size-8 rounded-full" />
                    <div className="space-y-1.5"><Skeleton className="h-3.5 w-28" /><Skeleton className="h-3 w-40" /></div>
                </div>
            </td>
            <td className="pr-4"><div className="space-y-1.5"><Skeleton className="h-3.5 w-32" /><Skeleton className="h-3 w-44" /></div></td>
            <td><Skeleton className="h-3.5 w-16" /></td>
            <td className="hidden pr-5 @min-[760px]:table-cell"><Skeleton className="ml-auto h-3.5 w-14" /></td>
            <td className="hidden @min-[900px]:table-cell"><Skeleton className="h-3.5 w-12" /></td>
            <td />
        </tr>
    )
}

export function UsersTable(p: UsersTableProps) {
    const navigate = useNavigate()
    const onPage = p.users.filter((u) => p.selected.has(u.id)).length
    const allChecked = p.users.length > 0 && onPage === p.users.length

    const openUser = (e: MouseEvent, id: number) => {
        if ((e.target as HTMLElement).closest("a, button, input, label, [role=checkbox], [role=menuitem]")) return
        if (e.metaKey || e.ctrlKey || e.button === 1) {
            window.open(`${getApiBaseUrl()}/users/${id}`, "_blank", "noopener")
            return
        }
        navigate(`/users/${id}`)
    }

    return (
        // Its own scroll area: the layout's <main> is overflow-auto but never
        // scrolls (the window does), so a page-level sticky header can't pin.
        <div className="@container max-h-[calc(100dvh-18rem)] min-h-[320px] overflow-auto rounded-xl border bg-card">
            <table className="w-full border-collapse text-left">
                <thead className="sticky top-0 z-10 bg-card">
                    <tr className="h-10 text-[13px] text-muted-foreground shadow-[inset_0_-1px_0_var(--color-border)]">
                        <th className="w-12 pl-4">
                            <Checkbox
                                aria-label="Select all on this page"
                                checked={allChecked ? true : onPage > 0 ? "indeterminate" : false}
                                disabled={p.users.length === 0}
                                onCheckedChange={() => p.onTogglePage(p.users, !allChecked)}
                            />
                        </th>
                        {COLUMNS.map((c) => {
                            const sortable = !(c.sort === "time_left" && NO_TIME_LEFT.includes(p.group))
                            const active = sortable && p.sort === c.sort
                            return (
                                <th
                                    key={c.key}
                                    className={cn("font-medium", c.className, c.align === "right" && "pr-5 text-right")}
                                    aria-sort={active ? (p.order === "asc" ? "ascending" : "descending") : undefined}
                                >
                                    {sortable ? (
                                        <button type="button" onClick={() => p.onSort(c.sort)} className={cn("inline-flex items-center gap-1 hover:text-foreground", active && "text-foreground")}>
                                            {c.label}
                                            {active && (p.order === "asc" ? <ArrowUp className="size-3.5" /> : <ArrowDown className="size-3.5" />)}
                                        </button>
                                    ) : c.label}
                                </th>
                            )
                        })}
                        <th className="w-[72px]"><span className="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    {p.loading ? (
                        Array.from({ length: p.skeletonRows }, (_, i) => <SkeletonRow key={i} />)
                    ) : p.users.length === 0 ? (
                        <tr><td colSpan={COLUMNS.length + 2} className="px-6 py-14">{p.empty}</td></tr>
                    ) : (
                        p.users.map((u) => (
                            <tr
                                key={u.id}
                                onClick={(e) => openUser(e, u.id)}
                                onAuxClick={(e) => e.button === 1 && openUser(e, u.id)}
                                className={cn(
                                    "group h-[52px] cursor-pointer border-b border-border/40 last:border-0 hover:bg-muted/40",
                                    p.selected.has(u.id) && "bg-muted/30",
                                    u.is_banned && "opacity-60",
                                )}
                            >
                                <td className="pl-4">
                                    <Checkbox aria-label={`Select ${listName(u)}`} checked={p.selected.has(u.id)} onCheckedChange={() => p.onToggle(u)} />
                                </td>
                                <td className="py-2 pr-3"><UserCell user={u} /></td>
                                <td className="py-2 pr-4"><SubscriptionCell user={u} /></td>
                                <td className="py-2"><LastSeenCell user={u} /></td>
                                <td className="hidden py-2 pr-5 text-right @min-[760px]:table-cell"><Figure value={u.total_subscriptions > 0 ? `${gb(u.data_used)} GB` : null} /></td>
                                <td className="hidden py-2 text-[13px] text-muted-foreground @min-[900px]:table-cell" title={new Date(u.created_at).toLocaleString()}>{fmtDay(u.created_at)}</td>
                                <td className="py-2 pr-3">
                                    <div className="flex items-center justify-end gap-0.5">
                                        <TelegramButton user={u} className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100" />
                                        <RowMenu user={u} actions={p.actions} />
                                    </div>
                                </td>
                            </tr>
                        ))
                    )}
                </tbody>
            </table>
        </div>
    )
}
