import { CheckCircle2, Loader2, RefreshCw, XCircle } from "lucide-react"
import type { MtprotoAllowlist, MtprotoNode } from "@/lib/api/mtproto"
import type { NodeOverview } from "@/lib/types"
import { cn, formatBytes, formatRelativeTime, formatUptime } from "@/lib/utils"
import { network } from "@/lib/design/palette"
import { T } from "@/components/node/node-ui"
import { OverviewCard, RowDivider, StatRow } from "@/components/node/overview/overview-card"
import { avgRtt } from "./use-mtproto-live"
import { newAgentVersion, shortBuild } from "./mtproto-health"

// the agent's GOMEMLIMIT: past it the Go runtime collects hard, so show the headroom
const AGENT_MEMORY_LIMIT = 256 * 1024 * 1024

function since(iso?: string): string {
    if (!iso) return ""
    const s = Math.max(0, Math.floor((Date.now() - Date.parse(iso)) / 1000))
    return formatUptime(s)
}

export function LinkCard({ r }: { r: MtprotoNode }) {
    const conns = r.link?.conns ?? []
    const up = r.link?.up ?? 0
    const rtt = avgRtt(r)
    return (
        <OverviewCard glow="sky" title="Link" aside={<span className={cn(T.meta, "tabular-nums")}>{up} of {conns.length} up</span>}>
            <div>
                <span className={cn(T.value, up === 0 && "text-red-500")}>{rtt ?? "—"}</span>
                {rtt !== null && <span className={cn(T.unit, "ml-1")}>ms</span>}
                <p className={cn(T.meta, "mt-0.5")}>{up === 0 ? "The panel can't reach the agent" : "Average round trip from the panel"}</p>
            </div>
            <ul className="flex flex-col gap-1" aria-label="Link connections">
                {conns.length === 0 && <li className={T.meta}>No connections yet</li>}
                {conns.map((c) => (
                    <li key={c.name} className="flex min-h-6 items-center justify-between gap-2 text-xs">
                        <span className="flex min-w-0 items-center gap-1.5">
                            <span className={cn("size-1.5 shrink-0 rounded-full", c.up ? "bg-emerald-500" : "bg-red-500")} aria-hidden />
                            <span className="truncate font-mono">{c.name}</span>
                        </span>
                        <span className="min-w-0 truncate text-right tabular-nums text-muted-foreground" title={c.error}>
                            {c.up ? `${c.rtt_ms} ms · ${c.streams} streams · ${since(c.since)}` : (c.error || "connecting…")}
                        </span>
                    </li>
                ))}
            </ul>
        </OverviewCard>
    )
}

export function TrafficCard({ r, today }: { r: MtprotoNode; today?: NodeOverview["today"] }) {
    const toDC = r.link?.bytes_to_dc ?? 0
    const fromDC = r.link?.bytes_from_dc ?? 0
    return (
        <OverviewCard glow="purple" title="Traffic" aside={<span className={T.meta}>today</span>}>
            <div>
                <StatRow label={<span><span style={{ color: network.rx }}>↓</span> From Telegram</span>}>{today ? formatBytes(today.downlink) : "—"}</StatRow>
                <StatRow label={<span><span style={{ color: network.tx }}>↑</span> To Telegram</span>}>{today ? formatBytes(today.uplink) : "—"}</StatRow>
                <RowDivider />
                <StatRow label="Total today">{today ? formatBytes(today.downlink + today.uplink) : "—"}</StatRow>
                <StatRow label={<span title="Counted by the link since the panel last started">Since panel start</span>}>{formatBytes(toDC + fromDC)}</StatRow>
            </div>
        </OverviewCard>
    )
}

/** "From core.telegram.org · refreshed 2h ago", or why it isn't refreshing. */
export function listSource(a: MtprotoAllowlist): string {
    if (!a.url) return "Built-in list · daily refresh off"
    let host = a.url
    try { host = new URL(a.url).host } catch { /* show it as typed */ }
    if (a.last_error) return `From ${host} · last refresh failed`
    return `From ${host} · ${a.refreshed_at ? `refreshed ${formatRelativeTime(a.refreshed_at)}` : "refreshes daily"}`
}

export function EgressCard({ r, allow, noVPN, onRefresh, refreshing }: { r: MtprotoNode; allow?: MtprotoAllowlist; noVPN?: boolean; onRefresh: () => void; refreshing: boolean }) {
    const refused = r.link?.violations ?? 0
    const failed = r.link?.dial_failures ?? 0
    const a = r.agent
    return (
        <OverviewCard glow="orange" title="Telegram egress">
            <div>
                <StatRow label="Connections made">{(r.link?.relayed ?? 0).toLocaleString()}</StatRow>
                <StatRow label="Failed to connect"><span className={cn(failed > 0 && "text-amber-500")}>{failed.toLocaleString()}</span></StatRow>
                {noVPN && <p className={cn(T.meta, "text-red-500")}>The VPN isn't connected, so nothing reaches Telegram.</p>}
                <StatRow label="Refused, not Telegram"><span className={cn(refused > 0 && "text-amber-500")}>{refused.toLocaleString()}</span></StatRow>
                <StatRow label="Capture rules">
                    {!a ? "—" : a.capture.rules
                        ? <><CheckCircle2 className="size-3.5 text-emerald-500" aria-hidden />In place</>
                        : <><XCircle className="size-3.5 text-red-500" aria-hidden />Missing</>}
                </StatRow>
                <RowDivider />
                <StatRow label="Address list">
                    <span className={cn(allow?.placeholder && "text-red-500")}>{allow ? `${allow.prefixes} ranges` : "—"}</span>
                    <button
                        type="button"
                        onClick={onRefresh}
                        disabled={refreshing}
                        title={allow?.refreshed_at ? `Refreshed ${formatRelativeTime(allow.refreshed_at)}` : "Never refreshed"}
                        aria-label="Refresh the Telegram address list"
                        className="-my-1 inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                    >
                        {refreshing ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
                    </button>
                </StatRow>
                {allow && <p className={cn(T.meta, "truncate")} title={allow.url || undefined}>{listSource(allow)}</p>}
            </div>
        </OverviewCard>
    )
}

export function AgentCard({ r, onUpdate, linkUp }: { r: MtprotoNode; onUpdate: () => void; linkUp: boolean }) {
    const a = r.agent
    const version = a?.agent_version || r.agent_version
    const due = !!r.updates?.agent
    const next = newAgentVersion(r)
    const build = shortBuild(a?.agent_build)
    const rss = a?.rss_bytes ?? 0
    const pct = Math.min(100, (rss / AGENT_MEMORY_LIMIT) * 100)
    return (
        <OverviewCard glow="cyan" title="MTProto agent" aside={due ? <span className={cn(T.meta, "text-amber-500")}>update out</span> : undefined}>
            <div>
                <div className="flex items-center justify-between text-xs">
                    <span className="text-muted-foreground">Memory</span>
                    <span className="font-semibold tabular-nums">{rss ? `${formatBytes(rss, 1)} of ${formatBytes(AGENT_MEMORY_LIMIT, 0)}` : "—"}</span>
                </div>
                <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted/40">
                    <div className={cn("h-full rounded-full bg-cyan-500 transition-[width] duration-700", pct >= 90 && "bg-amber-500")} style={{ width: `${pct}%` }} />
                </div>
            </div>
            <div>
                <StatRow label="Up for">{a ? formatUptime(a.uptime_sec) : "—"}</StatRow>
                <StatRow label="Last seen">{r.last_seen_at ? formatRelativeTime(r.last_seen_at) : "—"}</StatRow>
                <StatRow label="Version">
                    <span className="font-mono" title={a?.agent_build ? `Build ${a.agent_build}` : undefined}>{version || "—"}{build && <span className="text-muted-foreground"> · {build}</span>}</span>
                </StatRow>
                <StatRow label="Latest">
                    {due ? (
                        <button type="button" onClick={onUpdate} disabled={!linkUp} className="text-amber-500 underline-offset-2 hover:underline disabled:no-underline disabled:opacity-60">
                            {next || "New build"} · update
                        </button>
                    ) : version && r.latest ? <span className="text-muted-foreground">Up to date</span> : "—"}
                </StatRow>
            </div>
        </OverviewCard>
    )
}
