import { useMemo, useRef, type MouseEvent, type PointerEvent, type ReactNode } from "react"
import { AlertTriangle, ChevronRight, Clock, Download, FileDiff, OctagonX, RefreshCw, Router, SatelliteDish, Send, Terminal, Undo2, Unlink } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { CountryFlag } from "@/components/ui/country-flag"
import { Skeleton } from "@/components/ui/skeleton"
import { AreaSparkline, type SparkPoint } from "@/components/node/overview/area-sparkline"
import { HOST_COLORS } from "@/components/node/overview/overview-helpers"
import { T } from "@/components/node/node-ui"
import { PROXY_LABEL } from "@/components/mtproto/overview/mtproto-health"
import { chart } from "@/lib/design/palette"
import { nodeAddress, outageReasonLabel } from "@/lib/node-kind"
import { useLongPress } from "@/lib/hooks/use-long-press"
import { cn, copyToClipboard, formatSpeed } from "@/lib/utils"
import { toast } from "@/lib/toast"
import { useNodesStore } from "@/store/nodes-store"
import type { MtprotoNodeSummary } from "@/lib/api/mtproto"
import type { NodeDigest } from "@/lib/types"
import {
    clockTime, downUp, liteProxy, outagesText, shortDuration, speedOf, splitBytes, uptimeText, usageTone, usersOf, usersOnline,
    type IssueKind, type NodeIssue, type NodeView, type Tone,
} from "./facts"
import { NodeMenu, type NodeActions } from "./node-menu"
import { UptimeStrip } from "./uptime-strip"

// ─── Shared bits (card, row, phone row) ─────────────────────────────

const ISSUE_ICON: Record<IssueKind, typeof AlertTriangle> = {
    xray: OctagonX,
    memory: AlertTriangle,
    disk: AlertTriangle,
    cpu: AlertTriangle,
    drift: FileDiff,
    agent: Download,
    proxy: OctagonX,
    link: Unlink,
}

const ISSUE_TONE: Record<NodeIssue["tone"] | "neutral", string> = {
    crit: "bg-red-500/10 text-red-500",
    warn: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
    info: "bg-blue-500/10 text-blue-500",
    neutral: "bg-muted text-muted-foreground",
}

export function IssueChip({ issue, iconOnly, className }: { issue: NodeIssue; iconOnly?: boolean; className?: string }) {
    const Icon = ISSUE_ICON[issue.kind]
    return (
        <span
            title={issue.hint ?? issue.text}
            className={cn("inline-flex h-6 shrink-0 items-center gap-1.5 rounded-md px-2 text-[12.5px] font-medium", ISSUE_TONE[issue.tone], iconOnly && "h-5 px-1", className)}
        >
            <Icon className="size-3.5" aria-hidden />
            {iconOnly ? <span className="sr-only">{issue.text}</span> : issue.text}
        </span>
    )
}

export function StatusDot({ view, className }: { view: NodeView; className?: string }) {
    const label = view.status === "down" ? "Down" : view.status === "maintenance" ? "In maintenance" : view.status === "pending" ? "Never connected" : view.stale ? "Online, readings paused" : "Online"
    return (
        <span
            role="img"
            aria-label={label}
            title={label}
            className={cn(
                "size-2.5 shrink-0 rounded-full",
                view.status === "down" && "bg-red-500 ring-4 ring-red-500/20",
                view.status === "maintenance" && "bg-blue-500",
                view.status === "pending" && "ring-2 ring-inset ring-amber-500",
                view.status === "online" && (view.stale ? "bg-muted-foreground" : "bg-emerald-500"),
                className,
            )}
        />
    )
}

/** "Up 2d 19h", "Down 12m", "Maintenance", "Never connected". */
export function statusText(view: NodeView, now: number): { text: string; className: string } {
    const n = view.node
    switch (view.status) {
        case "down":
            return { text: n.last_disconnected_at ? `Down ${shortDuration(now - Date.parse(n.last_disconnected_at))}` : "Down", className: "font-semibold text-red-500" }
        case "maintenance":
            return { text: "Maintenance", className: "font-medium text-blue-500" }
        case "pending":
            return { text: "Never connected", className: "text-muted-foreground" }
        default: {
            const since = n.last_connected_at ?? n.created_at
            return { text: `Up ${shortDuration(now - Date.parse(since))}`, className: "text-muted-foreground" }
        }
    }
}

export function NodeTags({ view, iconsOnly }: { view: NodeView; iconsOnly?: boolean }) {
    const n = view.node
    const starlink = !!n.starlink_settings?.enabled && !n.is_stealth
    if (n.connect_mode !== "reverse" && !starlink && !view.local && !view.lite) return null
    const chip = "inline-flex h-[22px] shrink-0 items-center gap-1 rounded-full border px-2 text-[12px] text-muted-foreground"
    return (
        <>
            {view.local && (
                <span className={cn(chip, "border-transparent bg-status-info-soft text-status-info", iconsOnly && "px-1.5")} title="Runs inside this panel; no agent">
                    <Router className="size-3.5" aria-hidden />
                    <span className={cn(iconsOnly ? "sr-only" : "@max-[420px]:sr-only")}>This router</span>
                </span>
            )}
            {view.lite && (
                <span className={cn(chip, iconsOnly && "px-1.5")} title="Runs only the MTProto agent">
                    <Send className="size-3.5" aria-hidden />
                    <span className={cn(iconsOnly ? "sr-only" : "@max-[420px]:sr-only")}>MTProto</span>
                </span>
            )}
            {n.connect_mode === "reverse" && (
                <span className={cn(chip, iconsOnly && "px-1.5")} title="Reverse: the node dials the panel">
                    <Undo2 className="size-3.5" aria-hidden />
                    <span className={cn(iconsOnly ? "sr-only" : "@max-[420px]:sr-only")}>Reverse</span>
                </span>
            )}
            {starlink && (
                <span className={cn(chip, iconsOnly && "px-1.5")} title="On Starlink">
                    <SatelliteDish className="size-3.5" aria-hidden />
                    <span className={cn(iconsOnly ? "sr-only" : "@max-[420px]:sr-only")}>Starlink</span>
                </span>
            )}
        </>
    )
}

export function CopyIP({ ip, className }: { ip: string; className?: string }) {
    const copy = async (e: MouseEvent) => {
        e.stopPropagation()
        if (await copyToClipboard(ip)) toast.success("IP copied", { description: ip })
    }
    return (
        <button type="button" onClick={copy} title="Copy IP" className={cn("shrink-0 font-mono text-[12px] text-muted-foreground hover:text-foreground", className)}>
            {ip}
        </button>
    )
}

/** The digest's 15-minute buckets laid on the full day, so a missing bucket is a gap. */
export function throughputPoints(digest: NodeDigest | undefined, now: number): SparkPoint[] {
    if (!digest) return []
    const step = 900
    const byT = new Map(digest.series.map((p) => [p.t, p.bps]))
    const end = Math.floor(now / 1000)
    const out: SparkPoint[] = []
    for (let t = Math.floor((end - 86400) / step) * step; t <= end; t += step) {
        out.push({ t: t * 1000, v: byT.get(t) ?? null })
    }
    return out
}

const TONE_TEXT: Record<Tone, string> = { "": "", warn: "text-amber-500", crit: "text-red-500" }

function Meter({ label, value, text, color, tone }: { label: string; value?: number; text?: string; color: string; tone: Tone }) {
    const pct = Math.min(Math.max(value ?? 0, 0), 100)
    return (
        <div className="min-w-0">
            <div className="flex items-baseline justify-between gap-2 text-[12px] text-muted-foreground">
                <span>{label}</span>
                <span className={cn("truncate text-[13px] font-semibold tabular-nums text-foreground", TONE_TEXT[tone])}>{text ?? (value === undefined ? "—" : `${Math.round(value)}%`)}</span>
            </div>
            <div
                className="mt-1.5 h-1 overflow-hidden rounded-full bg-foreground/8"
                role="progressbar"
                aria-label={label}
                aria-valuenow={Math.round(pct)}
                aria-valuemin={0}
                aria-valuemax={100}
            >
                <div
                    className="h-full rounded-full"
                    style={{ width: `${pct}%`, background: tone === "crit" ? chart.red : tone === "warn" ? chart.amber : color, opacity: tone ? 1 : 0.85 }}
                />
            </div>
        </div>
    )
}

function Figure({ label, value, unit, sub }: { label: string; value: ReactNode; unit?: string; sub?: ReactNode }) {
    return (
        <div className="min-w-0">
            <div className={T.eyebrow}>{label}</div>
            <div className="whitespace-nowrap text-[22px] font-semibold leading-7 tracking-[-0.02em] tabular-nums">
                {value}
                {unit && <span className="ml-1 text-[13px] font-medium tracking-normal text-muted-foreground">{unit}</span>}
            </div>
            <div className={cn(T.meta, "truncate")}>{sub ?? " "}</div>
        </div>
    )
}

/** An MTProto node has no host readings: its streams, link and proxy instead. */
function LiteMeters({ s }: { s?: MtprotoNodeSummary }) {
    if (!s) return <div className="grid grid-cols-3 gap-4">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-6" />)}</div>
    const streams = s.max_streams ? (s.streams / s.max_streams) * 100 : 0
    const link = s.link_conns ? (s.link_up / s.link_conns) * 100 : 0
    const proxy = liteProxy(s)
    return (
        <div className="grid grid-cols-3 gap-4">
            <Meter label="Streams" value={streams} text={s.streams.toLocaleString()} color={HOST_COLORS.cpu} tone={streams >= 95 ? "crit" : streams >= 80 ? "warn" : ""} />
            <Meter label="Link" value={link} text={s.rtt_ms !== undefined ? `${s.rtt_ms} ms` : "—"} color={chart.emerald} tone={s.link_up === 0 ? "crit" : link < 100 ? "warn" : ""} />
            <Meter
                label="Proxy"
                value={proxy === "running" ? 100 : 0}
                text={PROXY_LABEL[proxy]}
                color={chart.emerald}
                tone={proxy === "crashing" || proxy === "stopped" ? "crit" : proxy === "held" ? "warn" : ""}
            />
        </div>
    )
}

// ─── Card ───────────────────────────────────────────────────────────

export interface NodeCardProps {
    view: NodeView
    now: number
    actions: NodeActions
    selectMode: boolean
    selected: boolean
    /** Phone: no chart, no IP, no chevron. */
    compact?: boolean
    onActivate: (view: NodeView, e: MouseEvent) => void
    onToggle: (view: NodeView) => void
    onLongPress: (view: NodeView) => void
}

export function NodeCard({ view, now, actions, selectMode, selected, compact, onActivate, onToggle, onLongPress }: NodeCardProps) {
    const { node: n, stats, digest } = view
    const down = view.status === "down"
    const status = statusText(view, now)
    // A long-press is followed by a click the finger didn't mean; swallow
    // that one, and forget it at the next touch in case no click came.
    const pressed = useRef(false)
    const longPress = useLongPress({ onLongPress: () => { pressed.current = true; onLongPress(view) } })
    const pressHandlers = { ...longPress, onPointerDown: (e: PointerEvent) => { pressed.current = false; longPress.onPointerDown(e) } }

    const click = (e: MouseEvent) => {
        if (pressed.current) { pressed.current = false; return }
        // Portaled content (the ⋯ menu) bubbles here through React, not the DOM.
        if (!e.currentTarget.contains(e.target as globalThis.Node)) return
        if ((e.target as HTMLElement).closest("a, button, input, label, [role=checkbox], [role=menuitem], [role=dialog]")) return
        onActivate(view, e)
    }
    // The day's grid only moves by the minute.
    const minute = Math.floor(now / 60_000) * 60_000
    const points = useMemo(() => throughputPoints(digest, minute), [digest, minute])

    const speed = speedOf(view)
    const live = speed && !down ? speed.up + speed.down : 0
    const lastSpeed = down && speed ? speed.up + speed.down : 0
    const users = usersOnline(view)
    const usersTotal = usersOf(view)
    const today = digest ? digest.today.uplink + digest.today.downlink : undefined
    const readingAt = stats?.received_at ?? (stats?.sampled_at ? Date.parse(stats.sampled_at) : undefined)
    const faded = down || view.stale

    return (
        <article
            role="link"
            tabIndex={0}
            aria-label={`${n.name}, ${status.text}`}
            onClick={click}
            onAuxClick={(e) => e.button === 1 && e.currentTarget.contains(e.target as globalThis.Node) && onActivate(view, e)}
            onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) onActivate(view, e as unknown as MouseEvent) }}
            {...(compact ? pressHandlers : {})}
            className={cn(
                "@container group relative flex cursor-pointer flex-col gap-3.5 rounded-2xl border bg-card/50 p-4 backdrop-blur-sm transition-[box-shadow,border-color] duration-300 select-none sm:px-[18px]",
                "hover:border-foreground/20 hover:shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                down && "border-red-500/50 bg-red-500/[0.04]",
                selected && "border-foreground ring-1 ring-foreground",
            )}
        >
            <div className="grid gap-1">
                <div className="flex min-w-0 items-center gap-2.5">
                    {selectMode && (
                        <Checkbox checked={selected} aria-label={`Select ${n.name}`} onCheckedChange={() => onToggle(view)} />
                    )}
                    <StatusDot view={view} />
                    <h3 className="min-w-0 truncate text-[17px] font-semibold leading-6 tracking-[-0.01em]">{n.name}</h3>
                    <NodeTags view={view} iconsOnly={compact} />
                    <span className={cn("ml-auto whitespace-nowrap text-[13px] tabular-nums", status.className)}>{status.text}</span>
                    <NodeMenu view={view} actions={actions} className={cn(compact && "-my-2 -mr-2 size-10")} />
                    {!compact && <ChevronRight className="size-4 shrink-0 text-muted-foreground/60" aria-hidden />}
                </div>
                <div className={cn("flex min-w-0 items-center gap-2 text-[13px] text-muted-foreground", selectMode ? "pl-[46px]" : "pl-5")}>
                    <CountryFlag code={n.country_code} className="h-3 w-[18px]" />
                    <span className="min-w-0 truncate" title={n.datacenter}>{n.datacenter || (view.local ? "This router" : n.country_code || "Unknown network")}</span>
                    {!compact && nodeAddress(n) === n.ip && <CopyIP ip={n.ip} className="ml-auto @max-[420px]:hidden" />}
                </div>
            </div>

            <CardNotice view={view} now={now} actions={actions} readingAt={readingAt} />

            {(view.issues.length > 0 || view.stale) && (
                <div className="flex flex-wrap gap-1.5">
                    {view.stale && readingAt !== undefined && (
                        <span className={cn("inline-flex h-6 items-center gap-1.5 rounded-md px-2 text-[12.5px] font-medium", ISSUE_TONE.neutral)}>
                            <Clock className="size-3.5" aria-hidden /> Last reading {clockTime(readingAt)}
                        </span>
                    )}
                    {view.issues.map((i) => <IssueChip key={i.kind} issue={i} />)}
                </div>
            )}

            {view.status !== "pending" && (
                <div className={cn("grid gap-3.5", faded && "[&_.fade]:opacity-45")}>
                    <div className="fade grid grid-cols-3 gap-3">
                        <Figure
                            label={compact ? "Users" : "Users online"}
                            value={users ?? "—"}
                            unit={usersTotal !== undefined ? `of ${usersTotal}` : undefined}
                            sub={usersTotal !== undefined ? (view.lite ? "users" : "active accounts") : undefined}
                        />
                        {today === undefined ? (
                            <div className="space-y-1.5 pt-1"><Skeleton className="h-3 w-12" /><Skeleton className="h-6 w-16" /></div>
                        ) : (
                            <Figure
                                label="Today"
                                value={today ? splitBytes(today)[0] : "—"}
                                unit={today ? splitBytes(today)[1] : undefined}
                                sub={today ? downUp(digest!.today.downlink, digest!.today.uplink) : undefined}
                            />
                        )}
                        {down ? (
                            <Figure
                                label="Last speed"
                                value={lastSpeed ? splitBytes(lastSpeed, true)[0] : "—"}
                                unit={lastSpeed ? splitBytes(lastSpeed, true)[1] : undefined}
                                sub={readingAt !== undefined ? `at ${clockTime(readingAt)}` : undefined}
                            />
                        ) : (
                            <Figure
                                label="Now"
                                value={live ? splitBytes(live, true)[0] : "—"}
                                unit={live ? splitBytes(live, true)[1] : undefined}
                                sub={live && !compact ? <span className="@max-[420px]:hidden">{downUp(speed!.down, speed!.up, true)}</span> : undefined}
                            />
                        )}
                    </div>
                    {!compact && !view.lite && (
                        <div className="fade grid gap-1">
                            {digest ? (
                                <AreaSparkline
                                    data={points}
                                    color={chart.emerald}
                                    height={40}
                                    max={Math.max(1, ...digest.series.map((p) => p.bps))}
                                    format={formatSpeed}
                                    label="Throughput"
                                />
                            ) : <Skeleton className="h-10 w-full" />}
                            <div className="flex justify-between gap-2">
                                <span className={T.meta}>Throughput, last 24 hours</span>
                                {digest && digest.today.peak_bps > 0 && <span className={T.meta}>Peak {formatSpeed(digest.today.peak_bps)}</span>}
                            </div>
                        </div>
                    )}
                    {view.lite ? (
                        <div className="fade"><LiteMeters s={view.mtproto} /></div>
                    ) : (
                        <div className="fade grid grid-cols-3 gap-4">
                            <Meter label="CPU" value={stats?.cpu_percent} color={HOST_COLORS.cpu} tone={usageTone("cpu", stats?.cpu_percent)} />
                            <Meter label="Memory" value={stats?.memory_percent} color={HOST_COLORS.memory} tone={usageTone("memory", stats?.memory_percent)} />
                            <Meter label="Disk" value={stats?.disk_percent} color={HOST_COLORS.disk} tone={usageTone("disk", stats?.disk_percent)} />
                        </div>
                    )}
                </div>
            )}

            {view.status !== "pending" && (
                <div className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-border/60 pt-3">
                    {digest ? (
                        <>
                            <UptimeStrip days={digest.uptime.days} />
                            <span className="text-[13px] font-semibold tabular-nums">{digest.uptime.days.length ? uptimeText(digest.uptime.pct) : "—"}</span>
                            <span className={T.meta}>{outagesText(digest.uptime.outages)} in 30 days</span>
                        </>
                    ) : <Skeleton className="h-3.5 w-48" />}
                </div>
            )}
        </article>
    )
}

/** The one-paragraph explanation a down, new or maintenance node needs. */
function CardNotice({ view, now, actions, readingAt }: { view: NodeView; now: number; actions: NodeActions; readingAt?: number }) {
    const n = view.node
    const checking = useNodesStore((s) => s.checkingHealthId === n.id)
    const remote = !view.local && !view.lite
    if (view.status === "pending") {
        return (
            <div className="grid gap-2 rounded-xl bg-muted px-3 py-2.5 text-[13px] leading-[18px]">
                {remote ? (
                    <>
                        <p><b className="font-semibold">Waiting for the agent.</b> Run the install command on the server, or let the panel install it over SSH.</p>
                        <div><Button size="sm" onClick={() => actions.deploy(n)}><Terminal className="size-3.5" />Deploy agent</Button></div>
                    </>
                ) : (
                    <p><b className="font-semibold">Not connected yet.</b> Open the node to see where it stands.</p>
                )}
            </div>
        )
    }
    if (view.status === "down") {
        const reason = n.last_offline_reason ? outageReasonLabel(n.last_offline_reason, view.local) : "Offline"
        const since = n.last_disconnected_at ? ` since ${clockTime(n.last_disconnected_at)}` : ""
        return (
            <div className="grid gap-2 rounded-xl bg-red-500/10 px-3 py-2.5 text-[13px] leading-[18px]">
                <p>
                    <b className="font-semibold">{reason}{since}.</b>{" "}
                    {n.connect_mode === "reverse"
                        ? `${n.name} dials the panel, so it comes back on its own once its network does.`
                        : "The panel keeps trying to reach it."}
                    {readingAt !== undefined && ` Last reading ${clockTime(readingAt)}.`}
                </p>
                <div className="flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" disabled={checking} onClick={() => actions.checkConnection(n)}><RefreshCw className={cn("size-3.5", checking && "animate-spin")} />{checking ? "Checking…" : "Check now"}</Button>
                    {remote && n.connect_mode !== "reverse" && <Button size="sm" variant="outline" onClick={() => actions.deploy(n)}><Terminal className="size-3.5" />Deploy agent</Button>}
                </div>
            </div>
        )
    }
    if (view.status === "maintenance") {
        const since = n.maintenance_since ? ` for ${shortDuration(now - Date.parse(n.maintenance_since))}` : ""
        return (
            <div className="grid gap-2 rounded-xl bg-blue-500/10 px-3 py-2.5 text-[13px] leading-[18px]">
                <p>
                    <b className="font-semibold">In maintenance{since}.</b>{" "}
                    {n.maintenance_message ? `Users see: "${n.maintenance_message}"` : "Users see the standard notice."}
                    {!n.is_online && " The node is offline too."}
                </p>
                <div><Button size="sm" variant="outline" onClick={() => actions.endMaintenance(n)}>End maintenance</Button></div>
            </div>
        )
    }
    return null
}

export function NodeCardSkeleton() {
    return (
        <div className="flex flex-col gap-4 rounded-2xl border p-4">
            <div className="flex items-center gap-2.5"><Skeleton className="size-2.5 rounded-full" /><Skeleton className="h-5 w-32" /><Skeleton className="ml-auto h-4 w-16" /></div>
            <Skeleton className="h-3.5 w-56" />
            <div className="grid grid-cols-3 gap-3">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-12" />)}</div>
            <Skeleton className="h-10 w-full" />
            <div className="grid grid-cols-3 gap-4">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-6" />)}</div>
            <Skeleton className="h-3.5 w-48" />
        </div>
    )
}
