import { useMemo, useState } from "react"
import { AlertOctagon, AlertTriangle, CheckCircle2, Info } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import type { StarlinkEvent } from "@/lib/types"
import { type EventRange, EVENT_RANGE_MS, eventReasonLabel, eventSeverityTone, formatDurationSecs, isOutageReason } from "./starlink-helpers"
import { SegmentedControl, StarlinkCard, T } from "./starlink-ui"

interface StarlinkEventsCardProps {
    /** From the DB. */
    events: StarlinkEvent[] | undefined
    /** The dish's own log — used while the DB has nothing yet (fresh install). */
    liveEvents: StarlinkEvent[] | undefined
    isLoading: boolean
    range: EventRange
    onRangeChange: (r: EventRange) => void
    /** False when the agent cannot sync dish events — an empty list then means "unknown", not "clean". */
    historyAvailable: boolean
}

const RANGE_OPTIONS = (["24h", "7d", "30d"] as const).map((r) => ({ value: r, label: r }))
const TONE = {
    red: { bar: "bg-red-500", text: "text-red-400", Icon: AlertOctagon },
    amber: { bar: "bg-amber-400", text: "text-amber-400", Icon: AlertTriangle },
    blue: { bar: "bg-blue-400", text: "text-blue-400", Icon: Info },
} as const
const COLLAPSED = 8

function formatWhen(iso: string, range: EventRange): string {
    const d = new Date(iso)
    const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    return range === "24h" ? time : `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`
}

export function StarlinkEventsCard({ events, liveEvents, isLoading, range, onRangeChange, historyAvailable }: StarlinkEventsCardProps) {
    const [expanded, setExpanded] = useState(false)
    const now = Date.now()
    const from = now - EVENT_RANGE_MS[range]

    const list = useMemo(() => {
        const src = events && events.length > 0 ? events : (liveEvents ?? [])
        return src
            .filter((e) => new Date(e.start_at).getTime() >= from)
            .sort((a, b) => new Date(b.start_at).getTime() - new Date(a.start_at).getTime())
    }, [events, liveEvents, from])

    const outageSecs = list
        .filter((e) => isOutageReason(e.reason))
        .reduce((s, e) => s + e.duration_ms / 1000, 0)
    const shown = expanded ? list : list.slice(0, COLLAPSED)

    return (
        <StarlinkCard title="Events" aside={<SegmentedControl value={range} options={RANGE_OPTIONS} onChange={onRangeChange} />}>
            {isLoading && !events && !liveEvents ? (
                <Skeleton className="h-40 w-full rounded-xl" />
            ) : (
                <>
                    {/* Timeline: one bar per event, placed by start time. */}
                    <div className="relative h-6 overflow-hidden rounded-md bg-muted/30" role="img"
                        aria-label={`${list.length} events in the last ${range}`}>
                        {list.map((e, i) => {
                            const start = new Date(e.start_at).getTime()
                            const left = ((start - from) / (now - from)) * 100
                            const width = Math.max((e.duration_ms / (now - from)) * 100, 0.35)
                            return (
                                <span key={i} className={`absolute inset-y-1 rounded-sm ${TONE[eventSeverityTone(e.severity)].bar}`}
                                    style={{ left: `${left}%`, width: `${width}%` }} />
                            )
                        })}
                    </div>
                    <p className={T.meta}>
                        {list.length === 0
                            ? historyAvailable ? `No events in the last ${range}.` : "Dish event history needs an agent update."
                            : `${list.length} events · ${formatDurationSecs(outageSecs)} total outage`}
                    </p>

                    {list.length === 0 ? historyAvailable && (
                        <div className="flex items-center gap-2 text-sm text-emerald-400">
                            <CheckCircle2 className="h-4 w-4" /> Clean link in this window
                        </div>
                    ) : (
                        <ul className="divide-y divide-border">
                            {shown.map((e, i) => {
                                const tone = TONE[eventSeverityTone(e.severity)]
                                return (
                                    <li key={i} className="flex items-center gap-2.5 py-2 text-xs leading-4">
                                        <tone.Icon className={`h-3.5 w-3.5 shrink-0 ${tone.text}`} aria-hidden />
                                        <span className="min-w-0 flex-1 truncate font-medium">{eventReasonLabel(e.reason)}</span>
                                        <span className="tabular-nums text-muted-foreground">{formatDurationSecs(e.duration_ms / 1000)}</span>
                                        <span className="w-24 text-right tabular-nums text-muted-foreground">{formatWhen(e.start_at, range)}</span>
                                    </li>
                                )
                            })}
                        </ul>
                    )}
                    {list.length > COLLAPSED && (
                        <button type="button" onClick={() => setExpanded((v) => !v)}
                            className="self-start text-xs font-medium text-muted-foreground hover:text-foreground">
                            {expanded ? "Show fewer" : `Show all ${list.length}`}
                        </button>
                    )}
                </>
            )}
        </StarlinkCard>
    )
}
