import { useState, useMemo, useCallback } from "react"
import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { SegmentedControl, T } from "./node-ui"
import { OverviewCard } from "./overview/overview-card"
import { useHubGaps, useNodeUptimeEvents } from "@/lib/queries/use-nodes"
import type { NodeTransitionReason, NodeUptimeEvent } from "@/lib/types"

interface UptimeTimelineProps {
    nodeId: number
    isOnline: boolean
    enabled?: boolean
    /** The built-in node: its xray stops with the panel, so a panel-down
     *  window is downtime ("Panel down"), not unobserved time. */
    local?: boolean
    /** When the node was added; time before it is neither up nor down. */
    createdAt?: string
}

/** planned = operator action (disable, maintenance); unknown = the panel
 *  itself wasn't running, so nothing observed this node. */
type SegmentKind = "online" | "offline" | "planned" | "unknown"

interface Segment {
    startPct: number
    widthPct: number
    status: SegmentKind
    startTime: Date
    endTime: Date
    durationMs: number
    reason?: NodeTransitionReason
    detail?: string
}

const PLANNED_REASONS: NodeTransitionReason[] = ["admin_disabled", "maintenance"]

function isPlanned(reason?: NodeTransitionReason): boolean {
    return !!reason && PLANNED_REASONS.includes(reason)
}

export const REASON_LABELS: Record<NodeTransitionReason, string> = {
    heartbeat_timeout: "Heartbeat timed out",
    agent_unreachable: "Agent unreachable",
    health_check_failed: "Health check failed",
    hub_shutdown: "Panel stopped",
    admin_disabled: "Disabled by operator",
    maintenance: "Maintenance mode",
    heartbeat_ok: "Heartbeat restored",
    agent_reachable: "Agent reachable",
    admin_enabled: "Enabled by operator",
    hub_start: "Panel started",
    panel_down: "Panel down",
}

const DOT_CLASSES: Record<SegmentKind, string> = {
    online: "bg-emerald-500",
    offline: "bg-red-500",
    planned: "bg-amber-500",
    unknown: "bg-muted-foreground/60",
}

const SEGMENT_CLASSES: Record<SegmentKind, string> = {
    online: "bg-emerald-500/60 hover:bg-emerald-500/80",
    offline: "bg-red-500/60 hover:bg-red-500/80",
    planned: "bg-amber-500/50 hover:bg-amber-500/70",
    unknown: "bg-muted-foreground/25 hover:bg-muted-foreground/40",
}

export function UptimeTimeline({ nodeId, isOnline, enabled = true, local = false, createdAt }: UptimeTimelineProps) {
    const [hours, setHours] = useState<24 | 168 | 720>(168)
    const { data: events, isLoading } = useNodeUptimeEvents(nodeId, hours, enabled)
    // Windows where the panel wasn't running: shown as unknown rather than
    // letting the last known state stretch across time nobody observed.
    const { data: hubGaps } = useHubGaps(hours, enabled)

    const { segments, uptimePct } = useMemo(() => {
        const now = new Date()
        const start = new Date(now.getTime() - hours * 60 * 60 * 1000)
        const totalMs = now.getTime() - start.getTime()
        // A node younger than the window has no history before it was added:
        // start the bar there instead of inferring an outage from nothing.
        const born = createdAt ? new Date(createdAt) : start
        const from = born > start ? (born < now ? born : now) : start
        const unbornMs = from.getTime() - start.getTime()

        if (!events || events.length === 0) {
            // No events — assume current status since the node existed
            return {
                segments: [{
                    startPct: (unbornMs / totalMs) * 100,
                    widthPct: ((totalMs - unbornMs) / totalMs) * 100,
                    status: isOnline ? "online" as const : "offline" as const,
                    startTime: from,
                    endTime: now,
                    durationMs: totalMs - unbornMs,
                }],
                uptimePct: isOnline ? 100 : 0,
            }
        }

        // Build segments from events
        const segs: Segment[] = []
        let onlineMs = 0
        let plannedMs = 0

        // Sort events by timestamp
        const sorted = [...events].sort((a, b) =>
            new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
        )

        // Determine initial state: look at first event — if it's "online", the node was offline before it
        // If it's "offline", the node was online before it
        let currentStatus: "online" | "offline" = sorted[0].status === "online" ? "offline" : "online"
        let currentReason: NodeTransitionReason | undefined
        let currentDetail: string | undefined
        let currentStart = from

        for (const event of sorted) {
            const eventTime = new Date(event.timestamp)
            if (eventTime < from) {
                // Event before our window — just update state
                currentStatus = event.status
                currentReason = event.reason
                currentDetail = event.detail
                currentStart = from
                continue
            }

            // Consecutive events with the same status carry no transition.
            // Skip them so a continuously-online node renders one solid bar
            // instead of many abutting translucent segments — their 0.3%
            // min-width edges overlap and the stacked emerald/60 layers
            // composite into brighter green seams.
            if (event.status === currentStatus) continue

            const clampedStart = currentStart < from ? from : currentStart
            const segEnd = eventTime > now ? now : eventTime

            if (segEnd > clampedStart) {
                const startPct = ((clampedStart.getTime() - start.getTime()) / totalMs) * 100
                const widthPct = ((segEnd.getTime() - clampedStart.getTime()) / totalMs) * 100
                const durationMs = segEnd.getTime() - clampedStart.getTime()

                const kind: SegmentKind =
                    currentStatus === "offline" && isPlanned(currentReason) ? "planned" : currentStatus
                segs.push({
                    startPct,
                    widthPct,
                    status: kind,
                    startTime: clampedStart,
                    endTime: segEnd,
                    durationMs,
                    reason: currentReason,
                    detail: currentDetail,
                })

                if (currentStatus === "online") onlineMs += durationMs
                if (kind === "planned") plannedMs += durationMs
            }

            currentStatus = event.status
            currentReason = event.reason
            currentDetail = event.detail
            currentStart = eventTime
        }

        // Final segment from last event to now
        const clampedStart = currentStart < from ? from : currentStart
        if (now > clampedStart) {
            const startPct = ((clampedStart.getTime() - start.getTime()) / totalMs) * 100
            const widthPct = ((now.getTime() - clampedStart.getTime()) / totalMs) * 100
            const durationMs = now.getTime() - clampedStart.getTime()

            const kind: SegmentKind =
                currentStatus === "offline" && isPlanned(currentReason) ? "planned" : currentStatus
            segs.push({
                startPct,
                widthPct,
                status: kind,
                startTime: clampedStart,
                endTime: now,
                durationMs,
                reason: currentReason,
                detail: currentDetail,
            })

            if (currentStatus === "online") onlineMs += durationMs
            if (kind === "planned") plannedMs += durationMs
        }

        // Unknown windows are laid over the top: the panel was down, so no
        // claim about this node is defensible there. They leave the uptime
        // denominator entirely — as do planned stretches.
        const unknownSegs: Segment[] = []
        let unknownMs = 0
        for (const gap of hubGaps ?? []) {
            const gapStart = new Date(gap.from) < start ? start : new Date(gap.from)
            const gapEnd = new Date(gap.to) > now ? now : new Date(gap.to)
            if (gapEnd <= gapStart) continue
            const durationMs = gapEnd.getTime() - gapStart.getTime()
            unknownMs += durationMs
            unknownSegs.push({
                startPct: ((gapStart.getTime() - start.getTime()) / totalMs) * 100,
                widthPct: (durationMs / totalMs) * 100,
                status: local ? "offline" : "unknown",
                startTime: gapStart,
                endTime: gapEnd,
                durationMs,
                reason: local ? "panel_down" : undefined,
            })
        }

        // Built-in node: panel-down time is downtime, so it stays in the
        // denominator; remote nodes leave it out as unobserved.
        const observedMs = Math.max(totalMs - unbornMs - (local ? 0 : unknownMs) - plannedMs, 0)
        return {
            segments: [...segs, ...unknownSegs],
            uptimePct: observedMs > 0 ? (Math.max(onlineMs - unknownMs, 0) / observedMs) * 100 : 0,
        }
    }, [events, hours, isOnline, hubGaps, local, createdAt])

    const timeLabels = useMemo(() => {
        const now = new Date()
        const start = new Date(now.getTime() - hours * 60 * 60 * 1000)
        const labels: { pct: number; label: string }[] = []

        if (hours === 24) {
            // Every 4 hours
            for (let i = 0; i <= 24; i += 4) {
                const t = new Date(start.getTime() + i * 60 * 60 * 1000)
                labels.push({
                    pct: (i / 24) * 100,
                    label: t.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false }),
                })
            }
        } else {
            // Daily for 7 days, every 5 days for 30
            const days = hours / 24
            const every = days > 7 ? 5 : 1
            for (let i = 0; i <= days; i += every) {
                const t = new Date(start.getTime() + i * 24 * 60 * 60 * 1000)
                labels.push({
                    pct: (i / days) * 100,
                    label: t.toLocaleDateString("en-US", { month: "short", day: "numeric" }),
                })
            }
        }
        labels[labels.length - 1].label = "Now"
        return labels
    }, [hours])

    const formatDuration = useCallback((ms: number) => {
        const totalSec = Math.floor(ms / 1000)
        if (totalSec < 60) return `${totalSec}s`
        const totalMin = Math.floor(totalSec / 60)
        if (totalMin < 60) return `${totalMin}m`
        const h = Math.floor(totalMin / 60)
        const m = totalMin % 60
        if (h < 24) return `${h}h ${m}m`
        const d = Math.floor(h / 24)
        const rh = h % 24
        return `${d}d ${rh}h`
    }, [])

    const [hoveredSeg, setHoveredSeg] = useState<number | null>(null)

    const hovered = hoveredSeg !== null ? segments[hoveredSeg] : undefined
    const when = (d: Date) => d.toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })

    return (
        <OverviewCard
            title="Uptime"
            aside={
                <div className="flex items-center gap-3">
                    {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground/50" aria-hidden />}
                    <span className={cn("text-sm font-bold tabular-nums", uptimePct >= 99 ? "text-emerald-500" : uptimePct >= 95 ? "text-amber-500" : "text-red-500")}>
                        {uptimePct.toFixed(1)}%
                    </span>
                    <SegmentedControl
                        value={String(hours)}
                        options={[{ value: "24", label: "24h" }, { value: "168", label: "7d" }, { value: "720", label: "30d" }]}
                        onChange={(v) => setHours(Number(v) as 24 | 168 | 720)}
                    />
                </div>
            }
            className="gap-2.5"
        >
            <div className="relative pt-1">
                {hovered && (
                    <div
                        className="pointer-events-none absolute bottom-[calc(100%-2px)] z-20"
                        style={{
                            left: `${Math.min(Math.max(hovered.startPct + hovered.widthPct / 2, 14), 86)}%`,
                            transform: "translateX(-50%)",
                        }}
                    >
                        <div className="whitespace-nowrap rounded-lg border border-border bg-popover px-3 py-1.5 shadow-xl">
                            <div className="flex items-center justify-between gap-4 text-xs font-semibold">
                                <span className="flex items-center gap-1.5">
                                    <span className={cn("h-1.5 w-1.5 rounded-full", DOT_CLASSES[hovered.status])} />
                                    {hovered.status === "unknown" ? "Panel not running" : hovered.reason === "panel_down" ? "Panel down" : hovered.status === "planned" ? "Planned" : hovered.status === "online" ? "Online" : "Offline"}
                                </span>
                                <span className="tabular-nums">{formatDuration(hovered.durationMs)}</span>
                            </div>
                            <p className={cn(T.meta, "mt-0.5")}>
                                {hovered.reason && hovered.status !== "online" && hovered.reason !== "panel_down" ? `${REASON_LABELS[hovered.reason] ?? hovered.reason}, ` : ""}
                                {when(hovered.startTime)} to {hovered.endTime.getTime() >= Date.now() - 60_000 ? "now" : when(hovered.endTime)}
                            </p>
                        </div>
                    </div>
                )}

                <div className="relative h-6 overflow-hidden rounded-lg bg-muted/20 md:h-7" onMouseLeave={() => setHoveredSeg(null)}>
                    {segments.map((seg, i) => (
                        <div
                            key={i}
                            className={cn("absolute top-0 bottom-0 cursor-default transition-opacity", SEGMENT_CLASSES[seg.status], hoveredSeg === i && seg.status !== "online" && "ring-2 ring-inset ring-foreground/80")}
                            style={{
                                left: `${seg.startPct}%`,
                                width: `${Math.max(seg.widthPct, 0.3)}%`,
                            }}
                            onMouseEnter={() => setHoveredSeg(i)}
                            onClick={() => setHoveredSeg(prev => (prev === i ? null : i))}
                        />
                    ))}
                </div>

                <div className="relative mt-1.5 h-4">
                    {timeLabels.map((tl, i) => (
                        <span
                            key={i}
                            className={cn(
                                T.meta,
                                "absolute tabular-nums text-muted-foreground/70",
                                // Every other label on phones so dates never collide.
                                i % 2 === 1 && i !== timeLabels.length - 1 && "hidden sm:block",
                            )}
                            style={{
                                left: `${tl.pct}%`,
                                transform: i === 0 ? "none" : i === timeLabels.length - 1 ? "translateX(-100%)" : "translateX(-50%)",
                            }}
                        >
                            {tl.label}
                        </span>
                    ))}
                </div>
            </div>

            <div className={cn(T.meta, "flex flex-wrap gap-x-4 gap-y-1")}>
                {(local
                    ? [["online", "Online"], ["offline", "Offline or panel down"], ["planned", "Planned"]] as const
                    : [["online", "Online"], ["offline", "Offline"], ["planned", "Planned"], ["unknown", "Panel not running"]] as const
                ).map(([k, label]) => (
                    <span key={k} className="inline-flex items-center gap-1.5">
                        <span className={cn("h-2.5 w-2.5 rounded-[3px]", SEGMENT_CLASSES[k].split(" ")[0])} aria-hidden />
                        {label}
                    </span>
                ))}
            </div>
        </OverviewCard>
    )
}
