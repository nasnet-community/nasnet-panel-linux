import { Download, Loader2, Play, RotateCw, Send, Square } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { MtprotoNode } from "@/lib/api/mtproto"
import { cn, formatSpeed, formatUptime } from "@/lib/utils"
import { chart, network } from "@/lib/design/palette"
import { T } from "@/components/node/node-ui"
import { OverviewCard } from "@/components/node/overview/overview-card"
import { AreaSparkline, type SparkPoint } from "@/components/node/overview/area-sparkline"
import { usageTone, toneText } from "@/components/node/overview/overview-helpers"
import type { MtprotoActions } from "../mtproto-actions"
import { PROXY_LABEL, proxyState, type ProxyState } from "./mtproto-health"

// users and streams are whole numbers, even averaged into a sparkline
const count = (v: number) => Math.round(v).toLocaleString()

/** Peak of a series, ignoring gaps. */
export function peakOf(data: SparkPoint[]): number | undefined {
    const vs = data.map((p) => p.v).filter((v): v is number => v !== null)
    return vs.length ? Math.max(...vs) : undefined
}

function maxOf(a: number | undefined, b: number | undefined): number | undefined {
    if (a === undefined) return b
    return b === undefined ? a : Math.max(a, b)
}

function Spark({ data, color, label, format }: { data: SparkPoint[]; color: string; label: string; format: (v: number) => string }) {
    if (!data.some((p) => p.v !== null)) return null
    const max = Math.max(1, (peakOf(data) ?? 0) * 1.15)
    return <AreaSparkline grow data={data} color={color} max={max} format={format} label={label} />
}

function Value({ value, unit, tone, dim }: { value: string; unit?: string; tone?: string; dim?: boolean }) {
    return (
        <div className={cn(dim && "text-muted-foreground")}>
            <span className={cn(T.value, !dim && tone)}>{value}</span>
            {unit && <span className={cn(T.unit, "ml-1")}>{unit}</span>}
        </div>
    )
}

export function UsersCard({ r, spark, online }: { r: MtprotoNode; spark: SparkPoint[]; online: boolean }) {
    const known = online && r.active_users !== undefined
    // the last hour is in minutes; the reading now can be above all of them
    const peak = maxOf(peakOf(spark), known ? r.active_users : undefined)
    return (
        <OverviewCard glow="emerald" title="Users online" aside={r.users !== undefined ? <span className={cn(T.meta, "tabular-nums")}>{r.users} {r.users === 1 ? "user" : "users"}</span> : undefined}>
            <div>
                <Value value={known ? String(r.active_users) : "—"} unit={known && r.users !== undefined ? `of ${r.users}` : undefined} dim={!known} />
                <p className={cn(T.meta, "mt-0.5")}>
                    {!online ? "Unknown while the node is offline" : peak !== undefined ? `Peak ${count(peak)} in the last hour` : "Users who sent traffic just now"}
                </p>
            </div>
            <Spark data={spark} color={chart.emerald} label="Users online" format={(v) => count(v)} />
        </OverviewCard>
    )
}

export function StreamsCard({ r, spark, online }: { r: MtprotoNode; spark: SparkPoint[]; online: boolean }) {
    const streams = r.link?.streams ?? 0
    const pct = r.max_streams ? (streams / r.max_streams) * 100 : 0
    const tone = usageTone(pct, 80, 95)
    const peak = maxOf(peakOf(spark), online ? streams : undefined)
    return (
        <OverviewCard glow="blue" title="Open streams">
            <div>
                <Value value={online ? streams.toLocaleString() : "—"} unit={`of ${r.max_streams.toLocaleString()}`} tone={toneText[tone]} dim={!online} />
                <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted/40" role="meter" aria-label="Streams in use" aria-valuemin={0} aria-valuemax={r.max_streams} aria-valuenow={streams}>
                    <div className={cn("h-full rounded-full transition-[width] duration-700", tone === "crit" ? "bg-red-500" : tone === "warn" ? "bg-amber-500" : "bg-blue-500")} style={{ width: `${Math.min(pct, 100)}%` }} />
                </div>
                <p className={cn(T.meta, "mt-1.5")}>
                    {peak !== undefined ? `Peak ${Math.round(peak).toLocaleString()} in the last hour` : "Telegram connections carried now"}
                </p>
            </div>
            <Spark data={spark} color={chart.blue} label="Streams" format={(v) => Math.round(v).toLocaleString()} />
        </OverviewCard>
    )
}

export function ThroughputCard({ r, spark, online, className }: { r: MtprotoNode; spark: SparkPoint[]; online: boolean; className?: string }) {
    const t = online ? r.throughput : undefined
    const total = t ? t.down_bps + t.up_bps : undefined
    const [num, unit] = total !== undefined ? splitSpeed(total) : ["—", undefined]
    return (
        <OverviewCard glow="teal" title="Throughput" aside={<span className={T.meta}>now</span>} className={className}>
            <div>
                <Value value={num} unit={unit} dim={!t} />
                <p className={cn(T.meta, "mt-0.5 tabular-nums")}>
                    {t ? (
                        <>
                            <span style={{ color: network.rx }}>↓</span> {formatSpeed(t.down_bps)} <span className="mx-1 text-border">·</span>
                            <span style={{ color: network.tx }}>↑</span> {formatSpeed(t.up_bps)}
                        </>
                    ) : online ? "Measuring…" : "Unknown while the node is offline"}
                </p>
            </div>
            <Spark data={spark} color={chart.teal} label="Throughput" format={formatSpeed} />
        </OverviewCard>
    )
}

// "1.2 MB/s" → ["1.2", "MB/s"], so the number takes the value style
function splitSpeed(bps: number): [string, string | undefined] {
    const s = formatSpeed(bps)
    const i = s.lastIndexOf(" ")
    return i > 0 ? [s.slice(0, i), s.slice(i + 1)] : [s, undefined]
}

const STATE_STYLE: Record<ProxyState, { text: string; ring: string }> = {
    running: { text: "text-emerald-500", ring: "bg-emerald-500/10 ring-emerald-500/25 text-emerald-500" },
    waiting: { text: "text-sky-400", ring: "bg-sky-500/10 ring-sky-500/25 text-sky-400" },
    held: { text: "text-amber-500", ring: "bg-amber-500/10 ring-amber-500/25 text-amber-500" },
    off: { text: "text-red-500", ring: "bg-red-500/10 ring-red-500/25 text-red-500" },
    stopped: { text: "text-red-500", ring: "bg-red-500/10 ring-red-500/25 text-red-500" },
    crashing: { text: "text-red-500", ring: "bg-red-500/10 ring-red-500/25 text-red-500" },
    unknown: { text: "text-muted-foreground", ring: "bg-muted/30 ring-muted-foreground/25 text-muted-foreground" },
}

const rowCls = "flex items-center justify-between gap-2"

/** The full node's Xray process card, for telemt: Restart and Stop, or Start once stopped. */
export function ProxyCard({ r, actions, compact, className }: {
    r: MtprotoNode
    actions: Pick<MtprotoActions, "linkUp" | "restartProxy" | "restarting" | "stopProxy" | "startProxy" | "toggling" | "updateProxy">
    compact?: boolean
    className?: string
}) {
    const { linkUp, toggling } = actions
    const t = r.agent?.telemt
    const state = proxyState(t, r.users, linkUp, r.proxy_stopped)
    const style = STATE_STYLE[state]
    const version = r.agent?.telemt_version || r.telemt_version
    const due = !!r.updates?.telemt && !!r.latest?.telemt
    const busy = actions.restarting || toggling !== null
    const btn = cn("w-full rounded-lg font-bold uppercase tracking-[0.06em]", compact ? "h-11 text-xs" : "h-7 text-[11px]")
    return (
        <OverviewCard
            glow="indigo"
            title="MTProto proxy"
            aside={
                <div className="flex items-center gap-1.5">
                    {busy && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" aria-hidden />}
                    {/* only there when the panel installs a newer telemt */}
                    {due && (
                        <button
                            type="button"
                            onClick={() => void actions.updateProxy()}
                            disabled={!linkUp}
                            title={`Update to telemt ${r.latest!.telemt}`}
                            className="inline-flex items-center gap-1 rounded-md border border-amber-500/40 px-1.5 py-0.5 text-[11px] font-semibold text-amber-500 transition-colors hover:bg-amber-500/10 disabled:opacity-60"
                        >
                            <Download className="h-3 w-3" aria-hidden />
                            Update to {r.latest!.telemt}
                        </button>
                    )}
                </div>
            }
            className={className}
        >
            <div className="flex items-center gap-3">
                <div className={cn("flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-full ring-2", style.ring)}>
                    <Send className="h-4 w-4" aria-hidden />
                </div>
                <div className="flex min-w-0 flex-col">
                    <span className={cn("text-sm font-bold leading-[18px]", style.text)}>{PROXY_LABEL[state]}</span>
                    <span className="truncate font-mono text-[12px] leading-[14px] text-muted-foreground">
                        {version ? `telemt v${version.replace(/^v/, "")}` : "—"}
                    </span>
                </div>
            </div>

            <div className={cn("flex flex-col border-t border-dashed border-border/70 pt-2.5 font-bold uppercase tracking-[0.06em]", compact ? "gap-2.5 text-xs" : "gap-[7px] text-[12px] leading-[14px]")}>
                <div className={rowCls}><span className="text-muted-foreground">Uptime</span><span className="font-mono">{t?.running ? formatUptime(t.uptime_sec) : "—"}</span></div>
                <div className={rowCls}><span className="text-muted-foreground">Restarts</span><span className={cn("font-mono", (t?.restarts ?? 0) > 0 && "text-amber-500")}>{t?.restarts ?? "—"}</span></div>
                <div className={rowCls}><span className="text-muted-foreground">Users</span><span className="font-mono">{r.users ?? "—"}</span></div>
                <div className={cn(rowCls, "normal-case tracking-normal")}>
                    <span className="font-bold uppercase tracking-[0.06em] text-muted-foreground">Last exit</span>
                    <span className="min-w-0 truncate font-mono font-medium" title={t?.last_exit}>{t?.last_exit || "—"}</span>
                </div>
            </div>

            <div className="mt-auto">
                {r.proxy_stopped ? (
                    <Button type="button" onClick={actions.startProxy} disabled={!linkUp || busy} className={cn(btn, "bg-emerald-600 text-white hover:bg-emerald-700")}>
                        {toggling === "start" ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden /> : <Play className="h-3 w-3" aria-hidden />}
                        Start proxy
                    </Button>
                ) : (
                    <div className="grid grid-cols-2 gap-2">
                        <Button type="button" variant="outline" disabled={!linkUp || busy} onClick={() => void actions.restartProxy()} className={btn}>
                            <RotateCw className={cn("h-3 w-3", actions.restarting && "animate-spin")} aria-hidden /> Restart
                        </Button>
                        <Button
                            type="button"
                            variant="outline"
                            disabled={!linkUp || busy}
                            onClick={() => void actions.stopProxy()}
                            className={cn(btn, linkUp && "border-red-500/25 text-red-500 hover:bg-red-500/10 hover:text-red-500")}
                        >
                            {toggling === "stop" ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden /> : <Square className="h-3 w-3" aria-hidden />}
                            Stop
                        </Button>
                    </div>
                )}
            </div>
        </OverviewCard>
    )
}
