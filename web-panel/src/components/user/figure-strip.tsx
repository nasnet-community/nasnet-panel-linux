import { ArrowRight, ChevronDown } from "lucide-react"
import type { Subscription, UserDetails, UserOverview } from "@/lib/types"
import { deriveSubscription } from "@/lib/subscription-derived"
import { cn } from "@/lib/utils"
import { fmtAgo } from "@/components/access-history/format"
import { daysUntil, fmtSize, fmtUTCDay, isEnded, panelState, plural } from "./format"

export type UserTab = "subscriptions" | "activity" | "profile"

type ToneKey = "muted" | "warn" | "bad"
const toneClass: Record<ToneKey, string> = {
    muted: "text-muted-foreground",
    warn: "text-amber-600 dark:text-amber-400",
    bad: "text-red-600 dark:text-red-400",
}

interface Tile {
    label: string
    value: string
    unit?: string
    valueMuted?: boolean
    sub: string
    tone: ToneKey
    edge: string
    tab: UserTab
}

function serviceTile(user: UserDetails, subs: Subscription[], overview?: UserOverview): Tile {
    const base = { label: "Service", edge: "border-l-zinc-300 dark:border-l-zinc-200", tab: "subscriptions" as const }
    if (subs.length === 0) {
        return { ...base, value: "None", valueMuted: true, sub: "No subscription yet", tone: "muted" }
    }
    const live = subs.filter(s => !isEnded(s))
    const active = subs.filter(s => s.status === "active").length
    const value = { value: String(active), unit: "active" }
    const exhausted = live.filter(s => s.status === "traffic_exhausted")
    if (exhausted.length) return { ...base, ...value, sub: exhausted.length === 1 ? `#${exhausted[0].id} out of data` : `${exhausted.length} out of data`, tone: "bad" }
    const banPaused = live.filter(s => panelState(s, user.is_banned) === "banPaused")
    if (banPaused.length) return { ...base, ...value, sub: `${banPaused.length} paused by the ban`, tone: "warn" }
    const expiring = live.filter(s => panelState(s, user.is_banned) === "expiring")
    if (expiring.length) {
        const soonest = Math.min(...expiring.map(s => Math.max(0, daysUntil(deriveSubscription(s)!.effectiveEndDate!))))
        return { ...base, ...value, sub: `Expires in ${plural(soonest, "day")}`, tone: "warn" }
    }
    const runsOut = (overview?.subscriptions ?? [])
        .map(o => o.forecast)
        .filter((f): f is NonNullable<typeof f> => !!f?.will_exhaust_first && !!f.exhaustion_date)
        .map(f => f.exhaustion_date!)
        .sort()[0]
    if (runsOut) return { ...base, ...value, sub: `Data runs out ≈ ${fmtUTCDay(runsOut)}`, tone: "warn" }
    const paused = live.filter(s => s.status === "paused").length
    if (paused) return { ...base, ...value, sub: `${paused} paused`, tone: "muted" }
    const ends = live.map(s => deriveSubscription(s)!.effectiveEndDate).filter((e): e is string => !!e).map(daysUntil).filter(n => n >= 0)
    if (ends.length) return { ...base, ...value, sub: `Expires in ${plural(Math.min(...ends), "day")}`, tone: "muted" }
    const ended = subs.length - live.length
    return { ...base, ...value, sub: ended ? `${ended} ended` : "No expiry", tone: "muted" }
}

export function buildTiles(user: UserDetails, subs: Subscription[], overview?: UserOverview): Tile[] {
    const online = overview?.online_ips ?? 0
    const lastSeen = user.last_active_at || overview?.last_seen_at
    const lifetime = user.total_data_upload + user.total_data_download
    const traffic = overview?.traffic_30d ?? 0
    return [
        serviceTile(user, subs, overview),
        {
            label: "Online now", edge: "border-l-amber-500", tab: "subscriptions",
            value: subs.length || online ? String(online) : "—", unit: subs.length || online ? "IPs" : undefined, valueMuted: !subs.length && !online,
            sub: online > 0 ? `${overview?.ips_30d ?? 0} IPs in 30 days` : lastSeen ? `Last seen ${fmtAgo(lastSeen)}` : "Never connected",
            tone: "muted",
        },
        {
            label: "Traffic", edge: "border-l-blue-500", tab: "subscriptions",
            value: traffic > 0 ? fmtSize(traffic) : "—", valueMuted: traffic <= 0,
            sub: traffic > 0 ? "Last 30 days" : lifetime > 0 ? "Nothing in 30 days" : "No usage yet",
            tone: "muted",
        },
        {
            label: "Data used", edge: "border-l-emerald-500", tab: "subscriptions",
            value: lifetime > 0 ? fmtSize(lifetime) : "—", valueMuted: lifetime <= 0,
            sub: lifetime > 0 ? `Across ${plural(subs.length, "subscription")}` : "No usage yet",
            tone: "muted",
        },
    ]
}

export function FigureStrip({ tiles, onNavigate }: { tiles: Tile[]; onNavigate: (tab: UserTab) => void }) {
    return (
        <div className="grid grid-cols-2 gap-2.5 lg:grid-cols-4 lg:gap-3">
            {tiles.map(t => (
                <button
                    key={t.label}
                    type="button"
                    onClick={() => onNavigate(t.tab)}
                    className={cn(
                        "group flex min-w-0 flex-col gap-1 rounded-xl border-l-[3px] bg-card/55 px-3.5 pb-3.5 pt-3 text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:px-[18px] sm:pb-4 sm:pt-[15px]",
                        t.edge,
                    )}
                >
                    <span className="flex items-center justify-between">
                        <span className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">{t.label}</span>
                        <ArrowRight className="hidden size-3.5 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground/70 sm:block" />
                    </span>
                    <span className="mt-0.5 flex items-baseline gap-1.5">
                        <span className={cn("text-[19px] font-extrabold leading-snug tracking-tight sm:text-xl", t.valueMuted && "text-muted-foreground")}>{t.value}</span>
                        {t.unit && <span className="text-[13px] text-foreground/80 sm:text-sm">{t.unit}</span>}
                    </span>
                    <span className={cn("text-[12px] leading-snug", toneClass[t.tone])}>{t.sub}</span>
                </button>
            ))}
        </div>
    )
}

/** Phone: the strip folded into one sticky line once it scrolls away. */
export function FigureTicker({ tiles, onExpand }: { tiles: Tile[]; onExpand: () => void }) {
    const [service, online, traffic, used] = tiles
    return (
        <button type="button" onClick={onExpand} aria-label="Show the summary" className="flex h-10 w-full items-center justify-between gap-2.5 border-b border-border bg-background/95 px-4 text-[12px] text-muted-foreground backdrop-blur">
            <span className="flex min-w-0 items-center gap-3.5 overflow-hidden whitespace-nowrap">
                <span><b className="font-mono font-semibold text-foreground">{service.value}</b> {service.unit ?? ""}</span>
                <span className="inline-flex items-center gap-1.5"><span className={cn("size-1.5 rounded-full", online.value !== "0" && online.value !== "—" ? "bg-emerald-500" : "bg-zinc-500")} /><b className="font-mono font-semibold text-foreground">{online.value}</b> online</span>
                <span><b className="font-mono font-semibold text-foreground">{traffic.value}</b> 30d</span>
                <span><b className="font-mono font-semibold text-foreground">{used.value}</b> used</span>
            </span>
            <ChevronDown className="size-3.5 shrink-0" />
        </button>
    )
}
