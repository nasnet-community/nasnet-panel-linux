import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { CircleCheck, Loader2, PauseCircle, PlugZap } from "lucide-react"
import { cn } from "@/lib/utils"
import { SegmentedControl, T } from "./node-ui"
import { OverviewCard } from "./overview/overview-card"
import { useNodeDisconnections, useNodeUptimeSummary } from "@/lib/queries/use-nodes"
import type { NodeOutage, NodeTransitionReason } from "@/lib/types"

interface DisconnectionHistoryProps {
    nodeId: number
    enabled?: boolean
    /** The built-in node: no agent, and panel-down time is an outage. */
    local?: boolean
}

const REASON_LABELS: Partial<Record<NodeTransitionReason, string>> = {
    heartbeat_timeout: "Heartbeat timed out",
    agent_unreachable: "Agent unreachable",
    health_check_failed: "Health check failed",
    hub_shutdown: "Panel stopped",
    admin_disabled: "Disabled by operator",
    maintenance: "Maintenance",
    panel_down: "Panel down",
}

// The built-in node has no agent (D19): name what failed without one.
const LOCAL_REASON_LABELS: Partial<Record<NodeTransitionReason, string>> = {
    agent_unreachable: "Not responding",
    heartbeat_timeout: "Not responding",
}

const WINDOWS = [
    { hours: 24, label: "24h", days: 1 },
    { hours: 168, label: "7d", days: 7 },
    { hours: 720, label: "30d", days: 30 },
] as const

function formatDuration(sec: number): string {
    if (sec < 60) return `${sec}s`
    const min = Math.floor(sec / 60)
    if (min < 60) return `${min}m`
    const h = Math.floor(min / 60)
    const m = min % 60
    if (h < 24) return m > 0 ? `${h}h ${m}m` : `${h}h`
    const d = Math.floor(h / 24)
    const rh = h % 24
    return rh > 0 ? `${d}d ${rh}h` : `${d}d`
}

function formatWhen(iso: string): string {
    const d = new Date(iso)
    return d.toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    })
}

function OutageRow({ outage, local }: { outage: NodeOutage; local?: boolean }) {
    const label = outage.reason
        ? (local ? LOCAL_REASON_LABELS[outage.reason] : undefined) ?? REASON_LABELS[outage.reason] ?? outage.reason
        : "Unknown cause"
    const Icon = outage.planned ? PauseCircle : PlugZap
    const end = outage.ended_at
        ? new Date(outage.ended_at).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" })
        : null
    return (
        <div className="flex items-start gap-3 border-t border-border/60 py-2.5 first:border-0">
            <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", outage.planned ? "text-amber-500" : "text-red-400")} aria-hidden />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[14px] font-medium tabular-nums">
                    <span>{formatWhen(outage.started_at)}</span>
                    {end && <span className="font-normal text-muted-foreground">to {end}</span>}
                    {outage.ongoing && <Badge variant="danger" className="h-4 px-1.5 text-[11px] font-bold">ongoing</Badge>}
                    {outage.planned && <Badge variant="outline" className="h-4 border-amber-500/40 px-1.5 text-[11px] font-bold text-amber-500">planned</Badge>}
                </div>
                <p className="mt-0.5 truncate text-xs text-muted-foreground">{label}</p>
                {outage.detail && (
                    <p className="mt-0.5 truncate font-mono text-[12px] text-muted-foreground/70" title={outage.detail}>{outage.detail}</p>
                )}
            </div>
            <span className={cn("shrink-0 text-[14px] font-bold tabular-nums", outage.planned ? "text-amber-500" : outage.ongoing ? "text-red-500" : "")}>
                {formatDuration(outage.duration_sec)}
            </span>
        </div>
    )
}

/**
 * DisconnectionHistory lists a node's outages with cause and duration, plus
 * the rollup totals for the window. Planned stretches (operator disable,
 * maintenance) are shown but excluded from downtime — mixing them in makes the
 * uptime figure meaningless.
 */
export function DisconnectionHistory({ nodeId, enabled = true, local = false }: DisconnectionHistoryProps) {
    const [windowIdx, setWindowIdx] = useState(1)
    const win = WINDOWS[windowIdx]
    const { data: outages, isLoading } = useNodeDisconnections(nodeId, win.hours, 50, enabled)
    const { data: summary } = useNodeUptimeSummary(nodeId, win.days, enabled)

    const unplannedCount = useMemo(() => (outages ?? []).filter((o) => !o.planned).length, [outages])

    // The daily rollup lags the live outage list (today's row fills in
    // later), so take whichever is larger rather than show "0 outages" next
    // to a listed one.
    const { longest, listedDowntime } = useMemo(() => {
        const unplanned = (outages ?? []).filter(o => !o.planned)
        return {
            longest: unplanned.reduce((m, o) => Math.max(m, o.duration_sec), 0),
            listedDowntime: unplanned.reduce((s, o) => s + o.duration_sec, 0),
        }
    }, [outages])
    const outageCount = Math.max(summary?.outage_count ?? 0, unplannedCount)
    const downtime = Math.max(summary?.downtime_sec ?? 0, listedDowntime)

    return (
        <OverviewCard
            title="Disconnections"
            aside={
                <div className="flex items-center gap-2">
                    {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground/50" aria-hidden />}
                    <SegmentedControl
                        value={String(windowIdx)}
                        options={WINDOWS.map((w, i) => ({ value: String(i), label: w.label }))}
                        onChange={(v) => setWindowIdx(Number(v))}
                    />
                </div>
            }
        >
            {/* Downtime counts unplanned outages only, against observed time. */}
            <div className="grid grid-cols-3 gap-2 tabular-nums">
                {([
                    ["Outages", String(outageCount)],
                    ["Downtime", formatDuration(downtime)],
                    ["Longest", longest > 0 ? formatDuration(longest) : "—"],
                ] as const).map(([label, value]) => (
                    <div key={label} className="rounded-lg bg-muted/20 px-3 py-2">
                        <p className={T.meta}>{label}</p>
                        <p className="mt-0.5 text-base font-bold">{value}</p>
                    </div>
                ))}
            </div>

            {outages && outages.length > 0 ? (
                <div className="max-h-72 overflow-y-auto pr-1">
                    {outages.map((o, i) => (
                        <OutageRow key={`${o.started_at}-${i}`} outage={o} local={local} />
                    ))}
                </div>
            ) : (
                !isLoading && (
                    <div className="flex items-center gap-2 py-2 text-muted-foreground">
                        <CircleCheck className="h-4 w-4 text-emerald-500" aria-hidden />
                        <span className="text-xs">No disconnections in this window.</span>
                    </div>
                )
            )}

            <p className={T.meta}>
                {local
                    ? "Planned time (disabled or in maintenance) doesn't count as downtime. Time the panel was down does: xray stops with it"
                    : "Planned time (disabled or in maintenance) and time the panel was down don't count as downtime"}
                {summary && summary.planned_sec > 0 ? `; ${formatDuration(summary.planned_sec)} planned in this window.` : "."}
            </p>
        </OverviewCard>
    )
}
