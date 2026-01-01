import { useState, useMemo, useCallback } from "react"
import { Card } from "@/components/ui/card"
import { HiOutlineClock } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { useNodeUptimeEvents } from "@/lib/queries/use-nodes"
import type { NodeUptimeEvent } from "@/lib/types"

interface UptimeTimelineProps {
    nodeId: number
    isOnline: boolean
    enabled?: boolean
}

interface Segment {
    startPct: number
    widthPct: number
    status: "online" | "offline"
    startTime: Date
    endTime: Date
    durationMs: number
}

export function UptimeTimeline({ nodeId, isOnline, enabled = true }: UptimeTimelineProps) {
    const [hours, setHours] = useState<24 | 168>(168)
    const { data: events, isLoading } = useNodeUptimeEvents(nodeId, hours, enabled)

    const { segments, uptimePct } = useMemo(() => {
        const now = new Date()
        const start = new Date(now.getTime() - hours * 60 * 60 * 1000)
        const totalMs = now.getTime() - start.getTime()

        if (!events || events.length === 0) {
            // No events — assume current status for entire period
            return {
                segments: [{
                    startPct: 0,
                    widthPct: 100,
                    status: isOnline ? "online" as const : "offline" as const,
                    startTime: start,
                    endTime: now,
                    durationMs: totalMs,
                }],
                uptimePct: isOnline ? 100 : 0,
            }
        }

        // Build segments from events
        const segs: Segment[] = []
        let onlineMs = 0

        // Sort events by timestamp
        const sorted = [...events].sort((a, b) =>
            new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
        )

        // Determine initial state: look at first event — if it's "online", the node was offline before it
        // If it's "offline", the node was online before it
        let currentStatus: "online" | "offline" = sorted[0].status === "online" ? "offline" : "online"
        let currentStart = start

        for (const event of sorted) {
            const eventTime = new Date(event.timestamp)
            if (eventTime < start) {
                // Event before our window — just update state
                currentStatus = event.status
                currentStart = start
                continue
            }

            const clampedStart = currentStart < start ? start : currentStart
            const segEnd = eventTime > now ? now : eventTime

            if (segEnd > clampedStart) {
                const startPct = ((clampedStart.getTime() - start.getTime()) / totalMs) * 100
                const widthPct = ((segEnd.getTime() - clampedStart.getTime()) / totalMs) * 100
                const durationMs = segEnd.getTime() - clampedStart.getTime()

                segs.push({
                    startPct,
                    widthPct,
                    status: currentStatus,
                    startTime: clampedStart,
                    endTime: segEnd,
                    durationMs,
                })

                if (currentStatus === "online") onlineMs += durationMs
            }

            currentStatus = event.status
            currentStart = eventTime
        }

        // Final segment from last event to now
        const clampedStart = currentStart < start ? start : currentStart
        if (now > clampedStart) {
            const startPct = ((clampedStart.getTime() - start.getTime()) / totalMs) * 100
            const widthPct = ((now.getTime() - clampedStart.getTime()) / totalMs) * 100
            const durationMs = now.getTime() - clampedStart.getTime()

            segs.push({
                startPct,
                widthPct,
                status: currentStatus,
                startTime: clampedStart,
                endTime: now,
                durationMs,
            })

            if (currentStatus === "online") onlineMs += durationMs
        }

        return {
            segments: segs,
            uptimePct: totalMs > 0 ? (onlineMs / totalMs) * 100 : 0,
        }
    }, [events, hours, isOnline])

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
            // Every day for 7 days
            for (let i = 0; i <= 7; i++) {
                const t = new Date(start.getTime() + i * 24 * 60 * 60 * 1000)
                labels.push({
                    pct: (i / 7) * 100,
                    label: t.toLocaleDateString("en-US", { month: "short", day: "numeric" }),
                })
            }
        }
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

    return (
        <Card className="relative overflow-hidden transition-shadow duration-300 hover:shadow-lg rounded-2xl p-4 md:p-5 bg-card/50 backdrop-blur-sm border-white/5">
            <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                    <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Uptime</p>
                    {isLoading && <Loader2 className="w-3 h-3 animate-spin text-muted-foreground/50" />}
                </div>
                <div className="flex items-center gap-3">
                    <span className={`text-sm font-bold font-mono ${uptimePct >= 99 ? "text-emerald-500" : uptimePct >= 95 ? "text-amber-500" : "text-red-500"}`}>
                        {uptimePct.toFixed(1)}%
                    </span>
                    <div className="flex items-center gap-1 bg-muted/30 rounded-lg p-0.5">
                        {([24, 168] as const).map((h) => (
                            <button
                                key={h}
                                onClick={() => setHours(h)}
                                className={`px-2 py-0.5 rounded-md text-[10px] font-bold transition-all ${
                                    hours === h
                                        ? "bg-foreground text-background shadow-sm"
                                        : "text-muted-foreground hover:text-foreground"
                                }`}
                            >
                                {h === 24 ? "24h" : "7d"}
                            </button>
                        ))}
                    </div>
                    {/* Current status indicator */}
                    <div className="flex items-center gap-1.5">
                        <div className={`w-2 h-2 rounded-full ${isOnline ? "bg-emerald-500 animate-pulse" : "bg-red-500"}`} />
                        <span className="text-[10px] font-bold text-muted-foreground uppercase">
                            {isOnline ? "Online" : "Offline"}
                        </span>
                    </div>
                </div>
            </div>

            {/* Timeline bar */}
            <div className="relative">
                <div className="relative h-6 md:h-7 bg-muted/20 rounded-lg overflow-hidden">
                    {segments.map((seg, i) => (
                        <div
                            key={i}
                            className={`absolute top-0 bottom-0 transition-opacity ${
                                seg.status === "online"
                                    ? "bg-emerald-500/60 hover:bg-emerald-500/80"
                                    : "bg-red-500/60 hover:bg-red-500/80"
                            }`}
                            style={{
                                left: `${seg.startPct}%`,
                                width: `${Math.max(seg.widthPct, 0.3)}%`,
                            }}
                            onMouseEnter={() => setHoveredSeg(i)}
                            onMouseLeave={() => setHoveredSeg(null)}
                        />
                    ))}

                    {/* Tooltip */}
                    {hoveredSeg !== null && segments[hoveredSeg] && (
                        <div
                            className="absolute -top-12 z-20 pointer-events-none"
                            style={{
                                left: `${Math.min(Math.max(segments[hoveredSeg].startPct + segments[hoveredSeg].widthPct / 2, 10), 90)}%`,
                                transform: "translateX(-50%)",
                            }}
                        >
                            <div className="bg-card/95 backdrop-blur-sm border border-white/10 rounded-lg px-3 py-1.5 shadow-xl whitespace-nowrap">
                                <div className="flex items-center gap-1.5">
                                    <div className={`w-1.5 h-1.5 rounded-full ${segments[hoveredSeg].status === "online" ? "bg-emerald-500" : "bg-red-500"}`} />
                                    <span className="text-[11px] font-bold capitalize">{segments[hoveredSeg].status}</span>
                                    <span className="text-[10px] text-muted-foreground font-mono">{formatDuration(segments[hoveredSeg].durationMs)}</span>
                                </div>
                            </div>
                        </div>
                    )}
                </div>

                {/* Time labels */}
                <div className="relative h-4 mt-1">
                    {timeLabels.map((tl, i) => (
                        <span
                            key={i}
                            className="absolute text-[9px] md:text-[10px] text-muted-foreground/50 font-mono"
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
        </Card>
    )
}
