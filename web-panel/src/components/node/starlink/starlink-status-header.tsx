import { useEffect, useState } from "react"
import { AlertOctagon, Bell, ChevronRight, ShieldAlert, ShieldCheck, ShieldX } from "lucide-react"
import type { StarlinkStatus } from "@/lib/types"
import { formatUptime, getActiveAlerts, healthDotColor } from "./starlink-helpers"
import { T } from "./starlink-ui"

interface StarlinkStatusHeaderProps {
    status: StarlinkStatus
    onAlertsClick: () => void
    dataUpdatedAt?: number
}

function formatRelative(ts: number, now: number): string {
    const diff = Math.max(0, Math.floor((now - ts) / 1000))
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
    return `${Math.floor(diff / 86400)}d ago`
}

// The strip this replaces was the only tinted, ring-bordered, 12px-radius
// element on the tab, and its five label/value cells used a type pairing
// (9px mono label over an 11px value) that appeared nowhere else. Health is
// the one thing worth a coloured surface, so only the pill keeps one.
const SEM = {
    emerald: {
        pill: "bg-emerald-500/10 border-emerald-500/25 text-emerald-300",
        Icon: ShieldCheck,
        word: "Normal",
    },
    amber: {
        pill: "bg-amber-500/10 border-amber-500/25 text-amber-300",
        Icon: ShieldAlert,
        word: "Degraded",
    },
    red: {
        pill: "bg-red-500/10 border-red-500/30 text-red-300",
        Icon: ShieldX,
        word: "Critical",
    },
} as const

export function StarlinkStatusHeader({ status, onAlertsClick, dataUpdatedAt }: StarlinkStatusHeaderProps) {
    const color = healthDotColor(status)
    const sem = SEM[color]
    const alerts = getActiveAlerts(status)
    const hasCritical = alerts.some(a => a.severity === "critical")
    const [now, setNow] = useState(() => Date.now())
    useEffect(() => {
        const id = setInterval(() => setNow(Date.now()), 5000)
        return () => clearInterval(id)
    }, [])

    const ageSec = dataUpdatedAt ? (now - dataUpdatedAt) / 1000 : 9999
    const freshness: "live" | "stale" | "error" =
        !status.available ? "error" : ageSec < 30 ? "live" : "stale"

    const connectivity = !status.available ? "Disconnected" : color === "amber" ? "Degraded" : "Connected"
    const isUpdating = !!status.software_update_state && status.software_update_state !== "IDLE" && status.software_update_state !== ""
    const updatePct = Math.max(0, Math.min(100, status.software_update_progress * 100))
    // outage_cause is empty when DishOutage is nil; UNKNOWN is the proto zero
    // value and not interesting on its own. Banner only shows real causes.
    const hasOutage = !!status.outage_cause && status.outage_cause !== "UNKNOWN"

    return (
        <div className="space-y-2">
            {/* Order matters at the two widths: on a phone the pill and the
                alerts button share the first line and the facts wrap under
                them; from sm up it is one row, facts in the middle. */}
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2 sm:flex-nowrap">
                <span
                    className={`order-1 flex h-7 shrink-0 items-center gap-2 rounded-lg border px-2.5 text-xs font-semibold ${sem.pill}`}
                >
                    <sem.Icon className="h-3.5 w-3.5" aria-hidden />
                    {sem.word} · {connectivity}
                </span>

                <div className={`order-3 flex w-full min-w-0 flex-wrap items-center gap-x-2 gap-y-1 sm:order-2 sm:w-auto sm:flex-1 ${T.body} text-muted-foreground`}>
                        {status.available && (
                            <>
                                <span>
                                    <span className="font-medium tabular-nums text-foreground">{formatUptime(status.uptime_s)}</span> uptime
                                </span>
                                <Dot />
                            </>
                        )}

                        <span className="flex items-center gap-1.5">
                            <FreshnessLED state={freshness} />
                            <span className={
                                freshness === "live" ? "font-medium text-emerald-300"
                                    : freshness === "stale" ? "font-medium text-amber-300"
                                        : "font-medium text-red-300"
                            }>
                                {freshness === "error" ? "No feed" : freshness === "live" ? "Live" : "Stale"}
                            </span>
                            {dataUpdatedAt && <span>updated {formatRelative(dataUpdatedAt, now)}</span>}
                        </span>

                        {status.available && status.software_version && (
                            <span className="hidden items-center gap-2 sm:flex">
                                <Dot />
                                <span className="truncate font-mono text-[11px]">{status.software_version}</span>
                            </span>
                        )}

                        {isUpdating && (
                            <>
                                <Dot />
                                <span className="flex items-center gap-2">
                                    <span className="font-medium text-blue-300">Updating</span>
                                    <span
                                        className="h-1 w-16 overflow-hidden rounded-full bg-blue-500/15"
                                        role="progressbar"
                                        aria-label={`Software update ${updatePct.toFixed(0)}%`}
                                        aria-valuenow={updatePct}
                                        aria-valuemin={0}
                                        aria-valuemax={100}
                                    >
                                        <span className="block h-full bg-blue-400 transition-[width] duration-700" style={{ width: `${updatePct}%` }} />
                                    </span>
                                    <span className="tabular-nums">{updatePct.toFixed(0)}%</span>
                                </span>
                            </>
                        )}
                </div>

                {/* Alerts was a cell in the strip with no affordance at all. It
                    opens a drawer, so it looks like the page header's buttons. */}
                <button
                    type="button"
                    onClick={onAlertsClick}
                    aria-label={`${alerts.length} active alerts. Open alerts panel.`}
                    className="order-2 ml-auto flex h-11 shrink-0 items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 text-xs font-medium transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:order-3 sm:h-7"
                >
                    <Bell className={`h-3.5 w-3.5 ${alerts.length === 0 ? "text-muted-foreground" : hasCritical ? "text-red-400" : "text-amber-400"}`} aria-hidden />
                    Alerts
                    <span className={`inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-full px-1.5 text-[11px] font-semibold tabular-nums ${
                        alerts.length === 0
                            ? "bg-foreground/10 text-muted-foreground"
                            : hasCritical
                                ? "bg-red-500/15 text-red-300"
                                : "bg-amber-500/15 text-amber-300"
                    }`}>
                        {alerts.length}
                    </span>
                    <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/60" aria-hidden />
                </button>
            </div>

            {/* Outage banner — full-width sibling */}
            {hasOutage && (
                <div className="relative overflow-hidden rounded-xl ring-1 ring-red-500/25 bg-red-500/[0.07]">
                    <div
                        className="absolute top-0 right-0 w-16 h-full opacity-40 pointer-events-none"
                        style={{
                            backgroundImage: "repeating-linear-gradient(45deg, rgba(239,68,68,0.3) 0 6px, transparent 6px 12px)",
                        }}
                        aria-hidden
                    />
                    <div className="relative flex items-center gap-3 px-4 py-2.5">
                        <AlertOctagon className="w-4 h-4 text-red-400 shrink-0" aria-hidden />
                        <div className="flex min-w-0 grow flex-col">
                            <span className={T.eyebrow}>Active outage</span>
                            <span className="truncate text-xs font-bold text-red-300">
                                {status.outage_cause}
                                {status.outage_duration_ns > 0 && (
                                    <span className="ml-2 font-normal tabular-nums text-red-400/70">
                                        · {(status.outage_duration_ns / 1e9).toFixed(0)}s elapsed
                                    </span>
                                )}
                            </span>
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}

function Dot() {
    return <span className="text-muted-foreground/40" aria-hidden>·</span>
}

function FreshnessLED({ state }: { state: "live" | "stale" | "error" }) {
    const cls = state === "live" ? "bg-emerald-400 shadow-emerald-500/60 animate-pulse"
        : state === "stale" ? "bg-amber-400 shadow-amber-500/40"
        : "bg-red-500 shadow-red-500/60"
    return <span className={`h-1.5 w-1.5 shrink-0 rounded-full shadow-md ${cls}`} aria-hidden />
}
