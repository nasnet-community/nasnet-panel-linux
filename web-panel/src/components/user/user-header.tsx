import { ArrowLeft, Ban, Calendar, Copy, Hash, Languages, MoreHorizontal, Pencil, Send, ShieldCheck, ShieldOff } from "lucide-react"
import { useNavigate } from "react-router"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { ActorAt, UserDetails } from "@/lib/types"
import { copyToClipboard, cn } from "@/lib/utils"
import { fmtUTC } from "@/components/access-history/format"
import { displayName, fmtDay, languageName, telegramLink } from "./format"
import { Tag } from "./subscription-panel"
import { lastUsersListUrl } from "@/components/users-list/last-view"

export interface HeaderActions {
    onEditTelegramId: () => void
    onToggleAdmin: () => void
    onBan: () => void
    onUnban: () => void
}

async function copyTelegramId(id: number) {
    await copyToClipboard(String(id))
    toast.success("Telegram ID copied")
}

export function UserHeader({ user, isRefetching, dataUpdatedAt, busy, actions }: {
    user: UserDetails
    isRefetching: boolean
    dataUpdatedAt: number
    busy: boolean
    actions: HeaderActions
}) {
    const navigate = useNavigate()
    const name = displayName(user)
    const tg = telegramLink(user)
    const showHandle = !!user.username && name !== `@${user.username}`
    const joined = new Date(user.created_at).toLocaleDateString([], { month: "short", day: "numeric", year: "numeric" })

    const back = (
        <Button variant="ghost" size="icon" onClick={() => navigate(lastUsersListUrl())} aria-label="Back to users" className="-ml-2.5 size-10 shrink-0 text-muted-foreground sm:size-9">
            <ArrowLeft className="size-[18px]" />
        </Button>
    )
    const avatar = (
        <span className="flex size-[34px] shrink-0 items-center justify-center rounded-full bg-muted text-sm font-bold sm:size-10 sm:text-[15px]">
            {name.replace(/^@/, "").charAt(0).toUpperCase()}
        </span>
    )
    const badges = (
        <>
            {user.is_banned && <Tag kind="bad"><Ban className="size-3" />Banned</Tag>}
            {user.is_admin && <Tag kind="muted" className="border-violet-500/40 bg-violet-500/10 text-violet-700 dark:text-violet-300">Bot admin</Tag>}
        </>
    )
    const idButton = user.telegram_id > 0 ? (
        <button type="button" onClick={() => copyTelegramId(user.telegram_id)} title="Copy Telegram ID" className="inline-flex items-center gap-1.5 whitespace-nowrap rounded hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <Hash className="size-3" /><span className="font-mono">{user.telegram_id}</span><Copy className="size-3 opacity-70" />
        </button>
    ) : <span className="whitespace-nowrap">No Telegram ID</span>
    const menu = (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" className="size-10 sm:size-8" aria-label="More actions" disabled={busy}>
                    <MoreHorizontal className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
                {user.telegram_id > 0 && <DropdownMenuItem onClick={() => copyTelegramId(user.telegram_id)}><Copy className="mr-2 size-4" />Copy Telegram ID</DropdownMenuItem>}
                <DropdownMenuItem onClick={actions.onEditTelegramId}><Pencil className="mr-2 size-4" />Edit Telegram ID</DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={actions.onToggleAdmin}>
                    {user.is_admin ? <><ShieldOff className="mr-2 size-4" />Remove bot admin…</> : <><ShieldCheck className="mr-2 size-4" />Make bot admin…</>}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                {user.is_banned
                    ? <DropdownMenuItem onClick={actions.onUnban}><Ban className="mr-2 size-4" />Unban user…</DropdownMenuItem>
                    : <DropdownMenuItem onClick={actions.onBan} className="text-red-600 focus:text-red-600 dark:text-red-400"><Ban className="mr-2 size-4" />Ban user…</DropdownMenuItem>}
            </DropdownMenuContent>
        </DropdownMenu>
    )
    const message = tg ? (
        <Button asChild size="sm" className="h-10 w-10 gap-1.5 px-0 sm:h-8 sm:w-auto sm:px-3.5" aria-label="Message on Telegram">
            <a href={tg} target="_blank" rel="noreferrer"><Send className="size-4 sm:size-3.5" /><span className="hidden sm:inline">Message</span></a>
        </Button>
    ) : null

    return (
        <header className="border-b border-border px-4 py-2.5 sm:px-6 sm:py-3.5">
            {/* Phone */}
            <div className="flex flex-col gap-2 sm:hidden">
                <div className="flex items-center gap-2">
                    {back}
                    {avatar}
                    <h1 className="min-w-0 flex-1 truncate text-[17px] font-bold leading-tight">{name}</h1>
                    {message}
                    {menu}
                </div>
                <div className="flex items-center justify-between gap-2.5 pl-8">
                    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[13px] text-muted-foreground">
                        {showHandle && <span className="whitespace-nowrap">@{user.username}</span>}
                        {idButton}
                        {badges}
                    </div>
                    <AutoRefreshControl isRefreshing={isRefetching} dataUpdatedAt={dataUpdatedAt} />
                </div>
            </div>

            {/* Desktop */}
            <div className="hidden items-center justify-between gap-4 sm:flex">
                <div className="flex min-w-0 items-center gap-3">
                    {back}
                    {avatar}
                    <div className="flex min-w-0 flex-col gap-[3px]">
                        <div className="flex min-w-0 items-center gap-2">
                            <h1 className="truncate text-lg font-bold leading-tight tracking-tight">{name}</h1>
                            {showHandle && <span className="shrink-0 text-sm text-muted-foreground">@{user.username}</span>}
                            {badges}
                        </div>
                        <div className="flex items-center gap-4 text-[13px] text-muted-foreground">
                            {idButton}
                            <span className="inline-flex items-center gap-1.5"><Languages className="size-3" />{languageName(user.language)}</span>
                            <span className="inline-flex items-center gap-1.5" title={fmtUTC(user.created_at)}><Calendar className="size-3" />Customer since {joined}</span>
                        </div>
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                    <AutoRefreshControl isRefreshing={isRefetching} dataUpdatedAt={dataUpdatedAt} />
                    {message}
                    {menu}
                </div>
            </div>
        </header>
    )
}

/** Full-width strip under the header while the user is banned. */
export function BannedStrip({ ban, pausedIds, onUnban, busy }: { ban?: ActorAt; pausedIds: number[]; onUnban: () => void; busy: boolean }) {
    const when = ban ? `Banned on ${fmtDay(ban.at)}${ban.actor ? ` by ${ban.actor}` : ""}` : "Banned"
    const paused = pausedIds.length ? `${pausedIds.map(id => `#${id}`).join(", ")} ${pausedIds.length === 1 ? "is" : "are"} paused. Unban resumes ${pausedIds.length === 1 ? "it" : "them"}.` : "No subscription was paused."
    return (
        <div role="status" className="flex flex-col gap-3 border-b border-red-500/30 bg-red-500/[0.08] px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
            <div className="flex items-start gap-3">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-red-500/15 text-red-600 dark:text-red-400"><Ban className="size-[15px]" /></span>
                <div className="flex flex-col gap-px">
                    <span className={cn("text-sm font-semibold text-red-700 dark:text-red-200")} title={ban ? fmtUTC(ban.at) : undefined}>{when}</span>
                    <span className="text-[13px] text-foreground/80">The bot ignores them, notices and auto-renew are off. {paused}</span>
                </div>
            </div>
            <Button variant="outline" size="sm" className="h-10 shrink-0 border-red-500/45 bg-transparent text-red-700 hover:bg-red-500/10 sm:h-8 dark:text-red-200" onClick={onUnban} disabled={busy}>
                Unban
            </Button>
        </div>
    )
}
