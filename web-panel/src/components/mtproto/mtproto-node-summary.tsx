import { Clock, Gauge, Link2, Package, Send, TrendingDown, TrendingUp, Users } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { useMtprotoNode } from "@/lib/queries"
import type { MtprotoNodeSummary } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
import { cn, formatSpeed, formatUptime } from "@/lib/utils"
import { PROXY_LABEL, proxyState, type ProxyState } from "./overview/mtproto-health"
import { avgRtt } from "./overview/use-mtproto-live"

const DOT: Record<ProxyState, string> = {
    running: "bg-emerald-500",
    waiting: "bg-sky-400",
    held: "bg-amber-500",
    off: "bg-red-500",
    stopped: "bg-red-500",
    crashing: "bg-red-500",
    unknown: "bg-muted-foreground",
}

// streams fill up like a disk; a full link or every user online is the good case
type MeterKind = "usage" | "health" | "share"

function barColor(kind: MeterKind, pct: number): string {
    if (kind === "share") return "bg-sky-500"
    if (kind === "health") return pct >= 100 ? "bg-emerald-500" : pct > 0 ? "bg-amber-500" : "bg-red-500"
    return pct >= 95 ? "bg-red-500" : pct >= 80 ? "bg-amber-500" : "bg-emerald-500"
}

/** Label, value and an optional bar: the list card's CPU/RAM/Disk meters, for an MTProto node. */
function Meter({ label, value, pct, kind = "usage" }: { label: string; value: string; pct?: number; kind?: MeterKind }) {
    return (
        <div className="min-w-0 space-y-1">
            <div className="flex items-center justify-between gap-1">
                <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground sm:text-xs md:text-[14px]">{label}</span>
                <span className="truncate text-[11px] font-bold tabular-nums sm:text-xs md:text-[14px]">{value}</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted/50 md:h-2">
                {pct !== undefined && <div className={cn("h-full rounded-full transition-all duration-500", barColor(kind, pct))} style={{ width: `${Math.max(Math.min(pct, 100), kind === "health" ? 4 : 0)}%` }} />}
            </div>
        </div>
    )
}

/** The nodes list card's metrics block for an MTProto node. */
export function MtprotoCardMetrics({ s }: { s?: MtprotoNodeSummary }) {
    if (!s) {
        return (
            <div className="space-y-3 md:space-y-4">
                <div className="grid grid-cols-3 gap-3 md:gap-4 lg:gap-5">{[1, 2, 3].map((i) => <Skeleton key={i} className="h-6 w-full md:h-7" />)}</div>
                <Skeleton className="h-5 w-full md:h-6" />
            </div>
        )
    }
    const state = proxyState({ running: s.proxy_running, crash_loop: s.crash_loop, held: s.proxy_held }, s.users, s.link_up > 0, s.proxy_stopped)
    return (
        <div className="space-y-3 md:space-y-4 lg:space-y-5">
            <div className="grid grid-cols-3 gap-3 md:gap-4 lg:gap-5">
                <Meter label="Streams" value={s.streams.toLocaleString()} pct={s.max_streams ? (s.streams / s.max_streams) * 100 : 0} />
                <Meter label="Users" value={s.active_users !== undefined ? `${s.active_users}${s.users !== undefined ? `/${s.users}` : ""}` : (s.users !== undefined ? String(s.users) : "—")}
                    pct={s.users ? ((s.active_users ?? 0) / s.users) * 100 : 0} kind="share" />
                <Meter label="Link" value={s.rtt_ms !== undefined ? `${s.rtt_ms} ms` : "—"} pct={s.link_conns ? (s.link_up / s.link_conns) * 100 : 0} kind="health" />
            </div>
            <div className="flex items-center justify-between border-t border-border/30 pt-2 md:pt-3">
                <div className="flex items-center gap-3 text-[12px] text-muted-foreground md:gap-4 md:text-[14px]">
                    <span>↑{formatSpeed(s.up_bps)}</span>
                    <span>↓{formatSpeed(s.down_bps)}</span>
                </div>
                <span className="inline-flex items-center gap-1.5 text-[12px] font-medium md:text-[14px]">
                    <span className={cn("size-1.5 rounded-full", DOT[state])} aria-hidden />
                    {state === "unknown" ? "Proxy unknown" : `Proxy ${PROXY_LABEL[state].toLowerCase()}`}
                    {s.agent_update && <span className="text-amber-500" title="A new MTProto agent is available">· update</span>}
                </span>
            </div>
        </div>
    )
}

function Item({ icon: Icon, label, value, color }: { icon: typeof Users; label: string; value: string; color?: string }) {
    return (
        <div className="flex items-center gap-2">
            <Icon className={cn("h-3.5 w-3.5", color || "text-muted-foreground")} />
            <div>
                <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
                <p className="text-sm font-bold">{value}</p>
            </div>
        </div>
    )
}

/** The side sheet's quick stats for an MTProto node. */
export function MtprotoSheetSummary({ node }: { node: Node }) {
    const { data: r, isLoading } = useMtprotoNode(node.id)
    if (isLoading) return <div className="space-y-4">{[1, 2, 3].map((i) => <Skeleton key={i} className="h-12 w-full" />)}</div>
    if (!r) return null
    const linkUp = (r.link?.up ?? 0) > 0
    const state = proxyState(r.agent?.telemt, r.users, linkUp, r.proxy_stopped)
    const rtt = avgRtt(r)
    return (
        <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
                <Item icon={Users} label="Users online" value={r.active_users !== undefined ? `${r.active_users}${r.users !== undefined ? ` / ${r.users}` : ""}` : "—"} color="text-emerald-500" />
                <Item icon={Gauge} label="Open streams" value={`${(r.link?.streams ?? 0).toLocaleString()} / ${r.max_streams.toLocaleString()}`} color="text-blue-500" />
                <Item icon={TrendingDown} label="From Telegram" value={formatSpeed(r.throughput?.down_bps ?? 0)} color="text-orange-500" />
                <Item icon={TrendingUp} label="To Telegram" value={formatSpeed(r.throughput?.up_bps ?? 0)} color="text-purple-500" />
                <Item icon={Link2} label="Link" value={linkUp ? `${r.link?.up} of ${r.link?.conns?.length ?? 0} up${rtt !== null ? ` · ${rtt} ms` : ""}` : "Down"} color={linkUp ? "text-sky-400" : "text-red-500"} />
                <Item icon={Send} label="Proxy" value={PROXY_LABEL[state]} color={state === "running" ? "text-emerald-500" : undefined} />
                <Item icon={Clock} label="Agent up for" value={r.agent ? formatUptime(r.agent.uptime_sec) : "—"} />
                <Item icon={Package} label="MTProto agent" value={r.agent?.agent_version || r.agent_version || "—"} />
            </div>
        </div>
    )
}
