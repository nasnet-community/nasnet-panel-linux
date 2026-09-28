import { useMemo, useState, type ComponentType } from "react"
import { Bell, Globe, Loader2, ShieldCheck, UserPlus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useUserTimeline } from "@/lib/queries"
import type { TimelineEvent, TimelineKind } from "@/lib/types"
import { cn } from "@/lib/utils"
import { fmtUTC } from "@/components/access-history/format"
import { fmtClock, fmtDayHeader } from "./format"
import { Tag } from "./subscription-panel"

const CHIPS: { kind?: TimelineKind; label: string; dot?: string }[] = [
    { label: "All" },
    { kind: "service", label: "Service", dot: "bg-blue-400" },
    { kind: "notice", label: "Notices", dot: "bg-violet-400" },
    { kind: "admin", label: "Admin", dot: "bg-zinc-400" },
]

const AUDIT_TITLES: Record<string, string> = {
    "user.ban": "Banned",
    "user.unban": "Unbanned",
    "user.toggle_admin": "Bot admin changed",
    "user.update_notes": "Notes updated",
    "subscription.extend": "Extended",
    "subscription.revoke": "Revoked",
    "subscription.pause": "Paused",
    "subscription.resume": "Resumed",
    "subscription.set_data": "Data usage set",
    "subscription.add_data": "Data added",
    "subscription.reset_data": "Usage reset",
    "subscription.set_expiry": "Expiry changed",
    "subscription.regen_key": "Link regenerated",
    "subscription.rename": "Renamed",
    "subscription.delete": "Deleted",
    "subscription.set_data_limit": "Data limit changed",
    "subscription.view_access_history": "Viewed access history",
    "subscription.search_access_history": "Searched access history",
}

const NOTICES: Record<string, string> = {
    expiry_7d: "expires in 7 days",
    expiry_3d: "expires in 3 days",
    expiry_1d: "expires tomorrow",
    expired: "expired",
    data_warning: "data running low",
    data_exhausted: "data exhausted",
}

function humanize(action: string): string {
    const verb = action.split(".").pop() ?? action
    const s = verb.replace(/_/g, " ")
    return s.charAt(0).toUpperCase() + s.slice(1)
}

type Icon = ComponentType<{ className?: string }>
interface Look { icon: Icon; ring: string }
const LOOK: Record<string, Look> = {
    service: { icon: Globe, ring: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
    notice: { icon: Bell, ring: "bg-violet-500/15 text-violet-600 dark:text-violet-300" },
    admin: { icon: ShieldCheck, ring: "bg-zinc-500/15 text-zinc-600 dark:text-zinc-300" },
    joined: { icon: UserPlus, ring: "bg-zinc-500/15 text-zinc-600 dark:text-zinc-300" },
}

function parseValues(v?: string): Record<string, unknown> | null {
    if (!v) return null
    try {
        const o = JSON.parse(v)
        return o && typeof o === "object" && !Array.isArray(o) ? o as Record<string, unknown> : null
    } catch {
        return null
    }
}

const show = (v: unknown) => (typeof v === "string" ? v : JSON.stringify(v))

function Diff({ oldV, newV }: { oldV?: string; newV?: string }) {
    const o = parseValues(oldV)
    const n = parseValues(newV)
    if (!o && !n) {
        if (!newV && !oldV) return null
        return (
            <span className="mt-1.5 inline-flex gap-2 self-start rounded-lg bg-muted/40 px-2.5 py-1.5 font-mono text-[12px]">
                {oldV && <span className="text-red-500 line-through">{oldV}</span>}
                {oldV && newV && <span className="text-muted-foreground">to</span>}
                {newV && <span className="text-emerald-600 dark:text-emerald-400">{newV}</span>}
            </span>
        )
    }
    const keys = [...new Set([...Object.keys(o ?? {}), ...Object.keys(n ?? {})])]
    return (
        <span className="mt-1.5 grid grid-cols-[auto_auto] gap-x-3 gap-y-0.5 self-start rounded-lg bg-muted/40 px-2.5 py-1.5 font-mono text-[12px]">
            {keys.map(k => (
                <span key={k} className="contents">
                    <span className="text-muted-foreground">{k}</span>
                    <span>
                        {o && k in o && <span className="text-red-500 line-through">{show(o[k])}</span>}
                        {o && k in o && n && k in n && <span className="text-muted-foreground"> to </span>}
                        {n && k in n && <span className="text-emerald-600 dark:text-emerald-400">{show(n[k])}</span>}
                    </span>
                </span>
            ))}
        </span>
    )
}

function describe(e: TimelineEvent): { title: string; detail: string; look: Look; tag?: { kind: "warn" | "bad" | "muted"; text: string }; diff?: boolean } {
    switch (e.kind) {
        case "notice":
            return { title: `Notice sent: ${NOTICES[e.type.replace(/^notice\./, "")] ?? e.type}`, detail: "Telegram bot", look: LOOK.notice }
        case "admin": {
            let detail = e.actor ? `By ${e.actor}` : ""
            let diff = true
            const n = parseValues(e.new_values)
            const ids = (n?.paused_subscriptions ?? n?.resumed_subscriptions) as number[] | undefined
            if ((e.type === "user.ban" || e.type === "user.unban") && Array.isArray(ids)) {
                detail += ids.length ? `, ${e.type === "user.ban" ? "paused" : "resumed"} ${ids.map(id => `#${id}`).join(", ")}` : ""
                diff = false
            }
            return { title: AUDIT_TITLES[e.type] ?? humanize(e.type), detail, look: LOOK.admin, diff }
        }
        default:
            if (e.type === "user.joined") return { title: "Joined", detail: "Started the Telegram bot", look: LOOK.joined }
            if (e.type === "subscription.ended") return { title: e.status === "cancelled" ? "Subscription cancelled" : "Subscription expired", detail: e.description ?? "", look: LOOK.service }
            return { title: "Subscription created", detail: e.description ?? "", look: LOOK.service }
    }
}

export function Timeline({ userId, onOpenSubscription }: { userId: number; onOpenSubscription: (id: number) => void }) {
    const [kind, setKind] = useState<TimelineKind | undefined>()
    const [views, setViews] = useState(false)
    const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useUserTimeline(userId, kind, views)
    const events = useMemo(() => data?.pages.flatMap(p => p.events) ?? [], [data])
    const counts = data?.pages[0]?.counts
    const groups = useMemo(() => {
        const out: { key: string; label: string; events: TimelineEvent[] }[] = []
        for (const e of events) {
            const key = new Date(e.at).toDateString()
            if (out[out.length - 1]?.key !== key) out.push({ key, label: fmtDayHeader(e.at), events: [] })
            out[out.length - 1].events.push(e)
        }
        return out
    }, [events])

    return (
        <div className="space-y-3">
            <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
                <div className="-mx-4 flex gap-1.5 overflow-x-auto px-4 pb-0.5 [scrollbar-width:none] sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0">
                    {CHIPS.map(c => {
                        const on = kind === c.kind
                        const n = counts ? (c.kind ? counts[c.kind] : counts.all) : undefined
                        return (
                            <button
                                key={c.label}
                                type="button"
                                aria-pressed={on}
                                onClick={() => setKind(c.kind)}
                                className={cn(
                                    "inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg border px-3 text-[13px] font-medium transition-colors sm:h-[30px]",
                                    on ? "border-primary bg-primary text-primary-foreground" : "border-input bg-background text-foreground/80 hover:bg-accent",
                                )}
                            >
                                {c.dot && <span className={cn("size-[7px] rounded-full", c.dot)} />}
                                {c.label}
                                {n != null && <span className="font-mono text-xs opacity-70">{n}</span>}
                            </button>
                        )
                    })}
                </div>
                <label className="inline-flex cursor-pointer items-center gap-2 text-[13px] text-muted-foreground">
                    <input type="checkbox" checked={views} onChange={(e) => setViews(e.target.checked)} className="size-4 accent-zinc-400" />
                    Show admin views{counts && !views && counts.views > 0 ? ` (${counts.views})` : ""}
                </label>
            </div>

            <section aria-label="Timeline" className="rounded-xl border-border sm:border sm:bg-card sm:py-2">
                {isLoading && (
                    <div className="space-y-4 p-5">
                        {[0, 1, 2].map(i => <div key={i} className="flex gap-3"><Skeleton className="size-[30px] rounded-full" /><div className="flex-1 space-y-1.5"><Skeleton className="h-4 w-48" /><Skeleton className="h-3 w-32" /></div></div>)}
                    </div>
                )}
                {!isLoading && events.length === 0 && <p className="py-10 text-center text-sm text-muted-foreground">Nothing here yet.</p>}
                {groups.map(g => (
                    <div key={g.key}>
                        <div className="flex items-center gap-3 pb-1.5 pt-3.5 sm:px-5">
                            <span className="whitespace-nowrap text-[13px] font-semibold text-foreground/80">{g.label}</span>
                            <span className="h-px flex-1 bg-border/60" />
                        </div>
                        {g.events.map(e => <Row key={e.id} e={e} onOpenSubscription={onOpenSubscription} />)}
                    </div>
                ))}
                {!isLoading && events.length > 0 && (
                    <div className="flex items-center justify-center gap-2 px-5 pb-2 pt-4 text-[12px] text-muted-foreground">
                        {hasNextPage ? (
                            <Button variant="outline" size="sm" onClick={() => fetchNextPage()} disabled={isFetchingNextPage} className="h-9">
                                {isFetchingNextPage && <Loader2 className="mr-1.5 size-3.5 animate-spin" />}Load older
                            </Button>
                        ) : (
                            <><span className="h-px w-10 bg-border/60" />Start of history<span className="h-px w-10 bg-border/60" /></>
                        )}
                    </div>
                )}
            </section>
        </div>
    )
}

function Row({ e, onOpenSubscription }: { e: TimelineEvent; onOpenSubscription: (id: number) => void }) {
    const d = describe(e)
    const Icon = d.look.icon
    const title = <span className="text-sm font-medium">{d.title}</span>
    const chip = e.subscription_id != null && (
        <button type="button" onClick={() => onOpenSubscription(e.subscription_id!)} className="inline-flex h-[22px] items-center rounded-md bg-muted px-1.5 font-mono text-[12px] hover:bg-muted/70">#{e.subscription_id}</button>
    )
    const time = <span className="font-mono text-[12px] text-muted-foreground" title={fmtUTC(e.at)}>{fmtClock(e.at)}</span>
    return (
        <div className="grid grid-cols-[30px_minmax(0,1fr)] gap-x-3 py-2 sm:grid-cols-[30px_minmax(0,1fr)_auto_56px] sm:gap-x-3.5 sm:px-5 sm:hover:bg-muted/30">
            <span className={cn("flex size-[30px] items-center justify-center rounded-full", d.look.ring)}><Icon className="size-[15px]" /></span>
            <span className="flex min-w-0 flex-col gap-0.5 pt-px">
                <span className="flex flex-wrap items-center gap-2">{title}{d.tag && <Tag kind={d.tag.kind} className="h-5 px-1.5 text-[11px]">{d.tag.text}</Tag>}</span>
                {d.detail && <span className="text-[13px] text-muted-foreground [overflow-wrap:anywhere]">{d.detail}</span>}
                {e.kind === "admin" && d.diff && <Diff oldV={e.old_values} newV={e.new_values} />}
                <span className="mt-0.5 flex items-center gap-2 sm:hidden">{time}{chip}</span>
            </span>
            <span className="hidden items-center gap-2 pt-1 sm:flex">{chip}</span>
            <span className="hidden pt-[5px] text-right sm:block">{time}</span>
        </div>
    )
}
