import { useRef, type MouseEvent, type PointerEvent, type ReactNode } from "react"
import { Link } from "react-router"
import { Check } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import type { UserListItem } from "@/lib/types"
import { Avatar, DataBar, NoTelegramTag, UserTags } from "./cells"
import { identityKind, listName, seenLine, SEEN_TEXT, subLine, TONE_DOT } from "./format"

/** Touch long-press; mice keep their normal click. */
function useLongPress(onLongPress: () => void, ms = 500) {
    const timer = useRef<number | undefined>(undefined)
    const fired = useRef(false)
    const cancel = () => window.clearTimeout(timer.current)
    return {
        fired,
        handlers: {
            onPointerDown: (e: PointerEvent) => {
                if (e.pointerType === "mouse") return
                fired.current = false
                timer.current = window.setTimeout(() => {
                    fired.current = true
                    navigator.vibrate?.(10)
                    onLongPress()
                }, ms)
            },
            onPointerUp: cancel,
            onPointerLeave: cancel,
            onPointerCancel: cancel,
            onContextMenu: (e: MouseEvent) => e.preventDefault(),
        },
    }
}

function UserCard({ user, selecting, selected, onToggle }: { user: UserListItem; selecting: boolean; selected: boolean; onToggle: (u: UserListItem) => void }) {
    const { fired, handlers } = useLongPress(() => onToggle(user))
    const name = listName(user)
    const line = subLine(user)
    const seen = seenLine(user)
    return (
        <Link
            to={`/users/${user.id}`}
            {...handlers}
            onClick={(e) => {
                if (fired.current) {
                    e.preventDefault()
                    fired.current = false
                    return
                }
                if (selecting) {
                    e.preventDefault()
                    onToggle(user)
                }
            }}
            aria-pressed={selecting ? selected : undefined}
            className={cn("flex h-[66px] select-none items-center gap-3 border-b border-border/40 px-3 last:border-0 [-webkit-touch-callout:none]", selected && "bg-muted/50")}
        >
            {selecting ? (
                <span className={cn("flex size-9 shrink-0 items-center justify-center rounded-full border", selected ? "border-foreground bg-foreground text-background" : "border-border")}>
                    {selected && <Check className="size-4" />}
                </span>
            ) : (
                <Avatar name={name} className="size-9 text-sm" />
            )}
            <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate text-[15px] font-medium">{name}</span>
                    {identityKind(user) === "none" && <NoTelegramTag />}
                    <UserTags user={user} />
                </span>
                <span className="flex items-center gap-2 whitespace-nowrap text-xs">
                    {line ? (
                        <>
                            <span aria-hidden className={cn("size-[7px] shrink-0 rounded-full", TONE_DOT[line.tone])} />
                            {line.live ? (
                                <>
                                    <span className={line.time?.warn ? "text-status-warning" : line.time?.muted ? "text-muted-foreground" : "text-text-secondary"}>{line.time?.text}</span>
                                    {line.data ? (
                                        <>
                                            <DataBar pct={line.data.pct} warn={line.data.warn} className="w-10" />
                                            <span className={cn("font-mono text-[11.5px]", line.data.warn ? "text-status-warning" : "text-text-secondary")}>{line.data.used}/{line.data.limit} GB</span>
                                        </>
                                    ) : (
                                        <span className="text-muted-foreground">Unlimited</span>
                                    )}
                                </>
                            ) : (
                                <span className="text-text-secondary">{line.reason}</span>
                            )}
                        </>
                    ) : (
                        <span className="text-muted-foreground">No subscription</span>
                    )}
                </span>
            </span>
            <span className={cn("flex shrink-0 items-center gap-1 text-xs", SEEN_TEXT[seen.tone])}>
                {seen.tone === "online" && <span aria-hidden className="size-1.5 rounded-full bg-status-success" />}
                {seen.text}
            </span>
        </Link>
    )
}

export function UsersCards({ users, loading, selected, onToggle, empty }: {
    users: UserListItem[]
    loading: boolean
    selected: ReadonlyMap<number, boolean>
    onToggle: (u: UserListItem) => void
    empty: ReactNode
}) {
    if (loading) {
        return (
            <div className="overflow-hidden rounded-xl border bg-card">
                {Array.from({ length: 8 }, (_, i) => (
                    <div key={i} className="flex h-[66px] items-center gap-3 border-b border-border/40 px-3 last:border-0">
                        <Skeleton className="size-9 rounded-full" />
                        <div className="flex-1 space-y-1.5"><Skeleton className="h-3.5 w-32" /><Skeleton className="h-3 w-44" /></div>
                    </div>
                ))}
            </div>
        )
    }
    if (users.length === 0) return <div className="rounded-xl border bg-card px-6 py-12">{empty}</div>
    const selecting = selected.size > 0
    return (
        <div className="overflow-hidden rounded-xl border bg-card">
            {users.map((u) => <UserCard key={u.id} user={u} selecting={selecting} selected={selected.has(u.id)} onToggle={onToggle} />)}
        </div>
    )
}
