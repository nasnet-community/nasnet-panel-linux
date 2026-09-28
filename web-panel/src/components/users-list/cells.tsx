import { Link } from "react-router"
import { Tag } from "@/components/user/tag"
import { telegramHandle } from "@/components/user/format"
import { cn } from "@/lib/utils"
import type { UserListItem } from "@/lib/types"
import { identityKind, listName, seenLine, SEEN_TEXT, subLine, TONE_DOT } from "./format"

export function Avatar({ name, className }: { name: string; className?: string }) {
    return (
        <span aria-hidden className={cn("flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-[13px] font-semibold", className)}>
            {name.charAt(0).toUpperCase()}
        </span>
    )
}

export function UserTags({ user }: { user: UserListItem }) {
    return (
        <>
            {user.is_admin && <Tag kind="muted" className="h-[18px] border-violet-500/40 bg-violet-500/10 px-1.5 text-[11px] text-violet-700 dark:text-violet-300">Admin</Tag>}
            {user.is_banned && <Tag kind="bad" className="h-[18px] px-1.5 text-[11px]">Banned</Tag>}
        </>
    )
}

export function NoTelegramTag() {
    return <Tag kind="muted" className="h-[18px] px-1.5 text-[11px]">No Telegram</Tag>
}

export function UserCell({ user }: { user: UserListItem }) {
    const name = listName(user)
    const kind = identityKind(user)
    return (
        <div className="flex min-w-0 items-center gap-2.5">
            <Avatar name={name} />
            <div className="flex min-w-0 flex-col gap-0.5">
                <div className="flex min-w-0 items-center gap-1.5">
                    <Link to={`/users/${user.id}`} className="truncate text-sm font-medium text-foreground outline-none hover:underline focus-visible:underline">{name}</Link>
                    <UserTags user={user} />
                </div>
                {kind === "none" ? (
                    <div><NoTelegramTag /></div>
                ) : (
                    <div className="flex items-center gap-1.5 whitespace-nowrap text-xs text-muted-foreground">
                        <span className="truncate">{kind === "telegram" ? `@${telegramHandle(user)}` : "no username"}</span>
                        <span aria-hidden className="text-muted-foreground/50">·</span>
                        <span className="font-mono text-[11.5px] tabular-nums">{user.telegram_id}</span>
                    </div>
                )}
            </div>
        </div>
    )
}

/** Data used, with a notch at 80% where "Ending soon" starts. */
export function DataBar({ pct, warn, className }: { pct: number; warn: boolean; className?: string }) {
    return (
        <span aria-hidden className={cn("relative h-1 w-16 shrink-0 overflow-hidden rounded-full bg-muted", className)}>
            <span className={cn("absolute inset-y-0 left-0 rounded-full", warn ? "bg-sub-expiring" : "bg-sub-active")} style={{ width: `${pct}%` }} />
            <span className="absolute inset-y-0 left-[80%] w-px bg-background/90" />
        </span>
    )
}

export function SubscriptionCell({ user }: { user: UserListItem }) {
    const line = subLine(user)
    if (!line) return <span className="text-sm text-muted-foreground/60">—</span>
    const match = user.match
    const shownMatched = !!match && match.subscription_id === user.subscription?.id
    return (
        <div className="flex min-w-0 flex-col gap-1">
            <div className="flex min-w-0 items-center gap-2">
                <span aria-hidden className={cn("size-2 shrink-0 rounded-full", TONE_DOT[line.tone])} />
                {shownMatched && match?.field === "label" ? (
                    <mark className="truncate rounded-sm bg-status-warning-soft px-0.5 text-[13px] text-foreground">{line.title}</mark>
                ) : (
                    <span className={cn("truncate text-[13px]", line.titleMuted ? "text-muted-foreground" : "text-foreground/90")}>{line.title}</span>
                )}
                {line.extra > 0 && <span className="rounded border border-border px-1 font-mono text-[11px] text-text-secondary">+{line.extra}</span>}
                {match && !(shownMatched && match.field === "label") && (
                    <span className="truncate rounded bg-status-warning-soft px-1.5 text-[11px] font-medium text-status-warning">
                        {match.field === "link" ? "matched config link" : `matched ${match.value}`}
                    </span>
                )}
            </div>
            <div className="flex items-center gap-2.5 whitespace-nowrap pl-4 text-xs">
                {line.live ? (
                    <>
                        <span className={cn("w-[72px] shrink-0", line.time?.warn ? "text-status-warning" : line.time?.muted ? "text-muted-foreground" : "text-text-secondary")}>{line.time?.text}</span>
                        {line.data ? (
                            <>
                                <DataBar pct={line.data.pct} warn={line.data.warn} />
                                <span className={cn("font-mono text-[11.5px] tabular-nums", line.data.warn ? "text-status-warning" : "text-text-secondary")}>
                                    {line.data.used} / {line.data.limit} GB
                                </span>
                            </>
                        ) : (
                            <span className="text-muted-foreground">Unlimited data</span>
                        )}
                    </>
                ) : (
                    <span className="text-text-secondary">{line.reason}</span>
                )}
            </div>
        </div>
    )
}

export function LastSeenCell({ user }: { user: UserListItem }) {
    const seen = seenLine(user)
    return (
        <span title={seen.title} className={cn("flex items-center gap-1.5 whitespace-nowrap text-[13px]", SEEN_TEXT[seen.tone])}>
            {seen.tone === "online" && <span aria-hidden className="size-1.5 rounded-full bg-status-success ring-[3px] ring-status-success/20" />}
            {seen.text}
        </span>
    )
}

export function Figure({ value }: { value: string | null }) {
    return <span className={cn("font-mono text-[12.5px] tabular-nums", value ? "text-foreground/90" : "text-muted-foreground/60")}>{value ?? "—"}</span>
}
