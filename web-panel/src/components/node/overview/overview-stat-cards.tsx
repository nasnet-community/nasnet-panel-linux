import type { Node, NodeOverview, NodeStats } from "@/lib/types"
import { cn, formatBytes, formatSpeed } from "@/lib/utils"
import { network } from "@/lib/design/palette"
import { T } from "../node-ui"
import { OverviewCard, RowDivider, StatRow } from "./overview-card"
import { clockTime } from "./overview-helpers"

interface CardBase {
    stats?: NodeStats
    /** False while showing a last reading rather than a live one. */
    live: boolean
    compact?: boolean
}

function StaleTime({ stats }: { stats?: NodeStats }) {
    const when = stats?.received_at ?? (stats?.sampled_at ? Date.parse(stats.sampled_at) : undefined)
    if (when === undefined) return null
    return <span className={T.meta}>{clockTime(when)}</span>
}

/** A stored reading may lack some fields (the DB keeps only the 1m load). */
function loadValue(stats: NodeStats | undefined, key: "load_avg_1" | "load_avg_5" | "load_avg_15", live: boolean): number | undefined {
    const v = stats?.[key]
    if (v === undefined || (!live && v === 0 && key !== "load_avg_1")) return undefined
    return v
}

const num = (v: number | undefined) => (v === undefined ? "—" : v.toLocaleString())

// ─── Load average ───────────────────────────────────────────────────

const LOAD_PERIODS = [
    { key: "load_avg_1", label: "1m" },
    { key: "load_avg_5", label: "5m" },
    { key: "load_avg_15", label: "15m" },
] as const

function loadTone(load: number, cores: number) {
    const ratio = load / Math.max(cores, 1)
    if (ratio >= 1) return { bar: "bg-red-500", text: "text-red-500" }
    if (ratio >= 0.7) return { bar: "bg-amber-500", text: "text-amber-500" }
    return { bar: "bg-emerald-500", text: "" }
}

export function LoadAverageCard({ stats, live, compact, cores }: CardBase & { cores?: number }) {
    const c = cores && cores > 0 ? cores : 1
    return (
        <OverviewCard
            glow="orange"
            title={compact ? "Load avg" : "Load average"}
            aside={live ? <span className={T.meta}>{cores ? `${cores} ${compact ? "c" : "cores"}` : ""}</span> : <StaleTime stats={stats} />}
        >
            {compact ? (
                <div className={cn(!live && "text-muted-foreground")}>
                    {LOAD_PERIODS.map(({ key, label }) => {
                        const v = loadValue(stats, key, live)
                        return (
                            <StatRow key={key} label={label}>
                                <span className={live ? loadTone(v ?? 0, c).text : ""}>{v !== undefined ? v.toFixed(2) : "—"}</span>
                            </StatRow>
                        )
                    })}
                </div>
            ) : (
                <>
                    <div className={cn("flex flex-col gap-2.5", !live && "opacity-60")}>
                        {LOAD_PERIODS.map(({ key, label }) => {
                            const known = loadValue(stats, key, live)
                            const v = known ?? 0
                            const tone = loadTone(v, c)
                            return (
                                <div key={key} className="flex items-center gap-2.5">
                                    <span className={cn(T.meta, "w-6 shrink-0 font-semibold")}>{label}</span>
                                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted/40">
                                        <div className={cn("h-full rounded-full transition-[width] duration-700", tone.bar)} style={{ width: `${Math.min((v / c) * 100, 100)}%` }} />
                                    </div>
                                    <span className={cn("w-10 text-right text-[14px] font-semibold tabular-nums", live ? tone.text : "text-muted-foreground")}>{known !== undefined ? v.toFixed(2) : "—"}</span>
                                </div>
                            )
                        })}
                    </div>
                    {cores ? <p className={cn(T.meta, "mt-auto")}>Bars fill at {cores.toFixed(2)}, one per core</p> : null}
                </>
            )}
        </OverviewCard>
    )
}

// ─── Network load ───────────────────────────────────────────────────

export function NetworkLoadCard({ stats, live, compact }: CardBase) {
    const active = stats?.active_accounts
    return (
        <OverviewCard glow="emerald" title={compact ? "Network" : "Network load"} aside={live ? undefined : <StaleTime stats={stats} />}>
            <div className={cn(!live && "text-muted-foreground")}>
                <StatRow label={compact ? "TCP" : "TCP connections"}>{num(stats?.tcp_count)}</StatRow>
                <StatRow label={compact ? "UDP" : "UDP connections"}>{num(stats?.udp_count)}</StatRow>
                {!compact && <StatRow label="Open files">{num(stats?.fd_count)}</StatRow>}
                {!compact && <RowDivider />}
                <StatRow label={compact ? "Online" : "Online users"}>
                    {compact ? (
                        <>{num(stats?.online_users)}{active !== undefined && ` / ${active}`}</>
                    ) : (
                        <>
                            {num(stats?.online_users)}
                            {active !== undefined && <span className="font-normal text-muted-foreground">of {active.toLocaleString()} active</span>}
                        </>
                    )}
                </StatRow>
            </div>
        </OverviewCard>
    )
}

// ─── Total traffic ──────────────────────────────────────────────────

export function TotalTrafficCard({ stats, today, compact }: Omit<CardBase, "live"> & { today?: NodeOverview["today"] }) {
    const up = stats?.total_uplink ?? 0
    const down = stats?.total_downlink ?? 0
    const total = up + down
    const todayTotal = today ? today.uplink + today.downlink : undefined
    const upPct = total > 0 ? (up / total) * 100 : 0
    return (
        <OverviewCard glow="cyan" title={compact ? "Traffic" : "Total traffic"} aside={compact ? undefined : <span className={T.meta}>since added</span>}>
            <div>
                <StatRow label="Today">{todayTotal !== undefined ? formatBytes(todayTotal) : "—"}</StatRow>
                <StatRow label={compact ? "Up" : "Upload"}>{stats ? formatBytes(up) : "—"}</StatRow>
                <StatRow label={compact ? "Down" : "Download"}>{stats ? formatBytes(down) : "—"}</StatRow>
                {!compact && <RowDivider />}
                {!compact && <StatRow label="Total">{stats ? formatBytes(total) : "—"}</StatRow>}
            </div>
            {!compact && total > 0 && (
                <div className="mt-auto flex h-1 overflow-hidden rounded-full" role="img" aria-label={`${upPct.toFixed(0)}% upload, ${(100 - upPct).toFixed(0)}% download`}>
                    <span style={{ width: `${upPct}%`, background: network.tx }} />
                    <span style={{ width: `${100 - upPct}%`, background: network.rx }} />
                </div>
            )}
        </OverviewCard>
    )
}

// ─── Bandwidth ──────────────────────────────────────────────────────

function Dot({ color }: { color: string }) {
    return <span className="h-[7px] w-[7px] shrink-0 rounded-full" style={{ background: color }} aria-hidden />
}

export function BandwidthCard({ node, stats, live, compact, peakBps }: CardBase & { node: Node; peakBps?: number }) {
    const limitMbps = node.bandwidth_settings?.enabled ? node.bandwidth_settings.total_bw ?? 0 : 0
    const nowMbps = (((stats?.up_speed ?? 0) + (stats?.down_speed ?? 0)) * 8) / 1_000_000
    const usedPct = limitMbps > 0 ? Math.min((nowMbps / limitMbps) * 100, 100) : 0
    const rate = (v: number | undefined) => (live && v !== undefined ? formatSpeed(v) : "No live reading")
    return (
        <OverviewCard glow="teal" title="Bandwidth" aside={compact || !live ? undefined : <span className={T.meta}>now</span>}>
            <div>
                <StatRow label={<span className="inline-flex items-center gap-1.5"><Dot color={network.rx} />{compact ? "Down" : "Download"}</span>}>
                    <span className={cn(!live && "font-medium text-muted-foreground")}>{rate(stats?.down_speed)}</span>
                </StatRow>
                <StatRow label={<span className="inline-flex items-center gap-1.5"><Dot color={network.tx} />{compact ? "Up" : "Upload"}</span>}>
                    <span className={cn(!live && "font-medium text-muted-foreground")}>{rate(stats?.up_speed)}</span>
                </StatRow>
                {!compact && <StatRow label="Peak in 24h">{peakBps ? formatSpeed(peakBps) : "—"}</StatRow>}
                {!compact && <RowDivider />}
                <StatRow label="Limit">
                    {limitMbps > 0 ? (
                        <span>{limitMbps} Mbps{live && <span className="font-normal text-muted-foreground">, {usedPct.toFixed(0)}% used</span>}</span>
                    ) : (
                        <span className="font-normal text-muted-foreground">{compact ? "None" : "None set"}</span>
                    )}
                </StatRow>
            </div>
            {!compact && limitMbps > 0 && live && (
                <div className="mt-auto h-1 overflow-hidden rounded-full bg-muted/40">
                    <div className={cn("h-full rounded-full", usedPct >= 85 ? "bg-red-500" : usedPct >= 60 ? "bg-amber-500" : "bg-emerald-500")} style={{ width: `${usedPct}%` }} />
                </div>
            )}
        </OverviewCard>
    )
}
