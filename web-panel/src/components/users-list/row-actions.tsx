import { Link } from "react-router"
import { ArrowUpRight, Ban, Hash, Link2, MoreVertical, Plus, Send, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator,
    DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { telegramLink } from "@/components/user/format"
import { cn } from "@/lib/utils"
import type { UserListItem, UserListSubscription } from "@/lib/types"
import { dataUse, listName, subscriptionTitle, timeLeft, TONE_DOT } from "./format"

export interface UserRowActions {
    busy: boolean
    copyTelegramId: (u: UserListItem) => void
    copyLink: (s: UserListSubscription) => void
    newSubscription: (u: UserListItem) => void
    linkTelegram: (u: UserListItem) => void
    toggleAdmin: (u: UserListItem) => void
    toggleBan: (u: UserListItem) => void
}

function LinkSummary({ s }: { s: UserListSubscription }) {
    const time = timeLeft(s.end_date)
    const data = dataUse(s)
    return (
        <>
            <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", time.warn || data?.warn ? TONE_DOT.ending : TONE_DOT.active)} />
            <span className="font-mono text-xs">{data ? `${data.used} / ${data.limit} GB` : "Unlimited data"}</span>
            <span className="ml-auto pl-3 text-xs text-muted-foreground">{time.text}</span>
        </>
    )
}

/** The hover shortcut to message a user; nothing for users without Telegram. */
export function TelegramButton({ user, className }: { user: UserListItem; className?: string }) {
    const href = telegramLink(user)
    if (!href) return null
    return (
        <Button asChild variant="ghost" size="icon" className={cn("size-7", className)}>
            <a href={href} target="_blank" rel="noreferrer" aria-label={`Message ${listName(user)} on Telegram`}><Send className="size-3.5" /></a>
        </Button>
    )
}

const ITEM = "gap-2.5 [&_svg]:size-4 [&_svg]:text-muted-foreground"

export function RowMenu({ user, actions, className }: { user: UserListItem; actions: UserRowActions; className?: string }) {
    const name = listName(user)
    const tg = telegramLink(user)
    const links = (user.active_links ?? []).filter((l) => l.subscription_url)
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" className={cn("size-7", className)} aria-label={`Actions for ${name}`} disabled={actions.busy}>
                    <MoreVertical className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-60">
                <DropdownMenuItem asChild className={ITEM}>
                    <Link to={`/users/${user.id}`}><ArrowUpRight />Open</Link>
                </DropdownMenuItem>
                {tg ? (
                    <>
                        <DropdownMenuItem asChild className={ITEM}>
                            <a href={tg} target="_blank" rel="noreferrer"><Send />Message on Telegram</a>
                        </DropdownMenuItem>
                        <DropdownMenuItem className={ITEM} onSelect={() => actions.copyTelegramId(user)}><Hash />Copy Telegram ID</DropdownMenuItem>
                    </>
                ) : (
                    <DropdownMenuItem className={ITEM} onSelect={() => actions.linkTelegram(user)}><Hash />Link Telegram ID…</DropdownMenuItem>
                )}
                {links.length === 1 && (
                    <DropdownMenuItem className={ITEM} onSelect={() => actions.copyLink(links[0])}><Link2 />Copy subscription link</DropdownMenuItem>
                )}
                {links.length > 1 && (
                    <DropdownMenuSub>
                        <DropdownMenuSubTrigger className={ITEM}><Link2 />Copy subscription link</DropdownMenuSubTrigger>
                        <DropdownMenuSubContent className="w-64">
                            {links.map((l) => (
                                <DropdownMenuItem key={l.id} className="gap-2" onSelect={() => actions.copyLink(l)} aria-label={`Copy the link for ${subscriptionTitle(l).title}`}>
                                    <LinkSummary s={l} />
                                </DropdownMenuItem>
                            ))}
                        </DropdownMenuSubContent>
                    </DropdownMenuSub>
                )}
                <DropdownMenuItem className={ITEM} onSelect={() => actions.newSubscription(user)}><Plus />New subscription</DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem className={ITEM} onSelect={() => actions.toggleAdmin(user)}><ShieldCheck />{user.is_admin ? "Remove bot admin" : "Make bot admin"}</DropdownMenuItem>
                <DropdownMenuItem
                    className={cn(ITEM, !user.is_banned && "text-status-danger focus:text-status-danger [&_svg]:text-status-danger")}
                    onSelect={() => actions.toggleBan(user)}
                >
                    <Ban />{user.is_banned ? "Unban" : "Ban"}
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )
}
