import { useEffect, useState } from "react"
import { Satellite, AlertOctagon, ShieldCheck, ShieldAlert, ShieldX, RadioTower } from "lucide-react"
import type { StarlinkStatus } from "@/lib/types"
import { formatUptime, getActiveAlerts, healthDotColor } from "./starlink-helpers"

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

const SEM = {
    emerald: { ring: "ring-emerald-500/15", bg: "bg-emerald-500/[0.04]", txt: "text-emerald-300", Icon: ShieldCheck, word: "NORMAL" },
    amber:   { ring: "ring-amber-500/20",   bg: "bg-amber-500/[0.04]",   txt: "text-amber-300",   Icon: ShieldAlert, word: "DEGRADED" },
    red:     { ring: "ring-red-500/25",     bg: "bg-red-500/[0.05]",     txt: "text-red-300",     Icon: ShieldX,     word: "CRITICAL" },
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

    const connectivity = !status.available ? "DISCONNECTED" : color === "amber" ? "DEGRADED" : "CONNECTED"
    const isUpdating = !!status.software_update_state && status.software_update_state !== "IDLE" && status.software_update_state !== ""
    const updatePct = Math.max(0, Math.min(100, status.software_update_progress * 100))
    // outage_cause is empty when DishOutage is nil; UNKNOWN is the proto zero
    // value and not interesting on its own. Banner only shows real causes.
    const hasOutage = !!status.outage_cause && status.outage_cause !== "UNKNOWN"

    return (
        <div className="space-y-2">
            {/* Strip */}
            <div className={`relative overflow-hidden rounded-xl ring-1 ${sem.ring} ${sem.bg} backdrop-blur-sm`}>
                <div className="flex flex-wrap items-stretch divide-x divide-white/[0.04]">
                    {/* Cell — semantic state */}
                    <div className="flex items-center gap-2 px-3.5 py-2.5 min-w-0">
                        <sem.Icon className={`w-3.5 h-3.5 ${sem.txt}`} aria-hidden />
                        <div className="flex flex-col min-w-0">
                            <span className="text-[9px] uppercase tracking-[0.2em] text-muted-foreground/60 font-mono">Status</span>
                            <span className={`text-[11px] font-bold tracking-wider ${sem.txt}`}>{sem.word} · {connectivity}</span>
                        </div>
                    </div>

                    {/* Cell — uptime */}
                    {status.available && (
                        <div className="flex flex-col justify-center px-3.5 py-2.5">
                            <span className="text-[9px] uppercase tracking-[0.2em] text-muted-foreground/60 font-mono">Uptime</span>
                            <span className="text-[11px] font-mono font-bold text-foreground tabular-nums">{formatUptime(status.uptime_s)}</span>
                        </div>
                    )}

                    {/* Cell — telemetry feed */}
                    <div className="flex flex-col justify-center px-3.5 py-2.5">
                        <span className="text-[9px] uppercase tracking-[0.2em] text-muted-foreground/60 font-mono flex items-center gap-1">
                            <RadioTower className="w-2.5 h-2.5" /> Feed
                        </span>
                        <span className="text-[11px] font-mono font-bold flex items-center gap-1.5 tabular-nums">
                            <FreshnessLED state={freshness} />
                            <span className={
                                freshness === "live" ? "text-emerald-300" :
                                freshness === "stale" ? "text-amber-300" : "text-red-300"
                            }>
                                {freshness === "error" ? "ERROR" : freshness === "live" ? "LIVE" : "STALE"}
                            </span>
                            <span className="text-muted-foreground/70 font-normal">
                                {dataUpdatedAt ? `· ${formatRelative(dataUpdatedAt, now)}` : ""}
                            </span>
                        </span>
                    </div>

                    {/* Cell — versions (collapses on mobile) */}
                    {status.available && (
                        <div className="hidden sm:flex flex-col justify-center px-3.5 py-2.5">
                            <span className="text-[9px] uppercase tracking-[0.2em] text-muted-foreground/60 font-mono">HW · SW</span>
                            <span className="text-[11px] font-mono text-muted-foreground tabular-nums">
                                {status.hardware_version || "—"} · {status.software_version || "—"}
                            </span>
                        </div>
                    )}

                    {/* Spacer */}
                    <div className="grow" />

                    {/* Cell — alerts (always reachable) */}
                    <button
                        onClick={onAlertsClick}
                        aria-label={`${alerts.length} active alerts. Open alerts panel.`}
                        className={`flex items-center gap-2 px-3.5 py-2.5 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                            alerts.length === 0
                                ? "hover:bg-emerald-500/10 text-emerald-300"
                                : hasCritical
                                ? "hover:bg-red-500/10 text-red-300"
                                : "hover:bg-amber-500/10 text-amber-300"
                        }`}
                    >
                        <Satellite className="w-3.5 h-3.5" />
                        <div className="flex flex-col items-start">
                            <span className="text-[9px] uppercase tracking-[0.2em] text-muted-foreground/60 font-mono">Alerts</span>
                            <span className="text-[11px] font-bold font-mono tabular-nums">
                                {alerts.length.toString().padStart(2, "0")}
                                {alerts.length > 0 && <span className="ml-1 text-muted-foreground/60 font-normal">{hasCritical ? "CRIT" : "WARN"}</span>}
                            </span>
                        </div>
                    </button>
                </div>

                {/* Software update — thin bleed bar at bottom edge */}
                {isUpdating && (
                    <div className="absolute inset-x-0 bottom-0 h-[3px] bg-blue-500/10">
                        <div
                            className="h-full bg-gradient-to-r from-blue-500 via-cyan-400 to-blue-500 transition-[width] duration-700"
                            style={{ width: `${updatePct}%` }}
                            role="progressbar"
                            aria-label={`Software update ${updatePct.toFixed(0)}%`}
                            aria-valuenow={updatePct}
                            aria-valuemin={0}
                            aria-valuemax={100}
                        />
                    </div>
                )}
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
                        <div className="flex flex-col min-w-0 grow">
                            <span className="text-[9px] uppercase tracking-[0.2em] text-red-400/70 font-mono">Active Outage</span>
                            <span className="text-xs font-bold text-red-300 truncate">
                                {status.outage_cause}
                                {status.outage_duration_ns > 0 && (
                                    <span className="ml-2 text-red-400/70 font-mono font-normal">
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

function FreshnessLED({ state }: { state: "live" | "stale" | "error" }) {
    const cls = state === "live" ? "bg-emerald-400 shadow-emerald-500/60 animate-pulse"
        : state === "stale" ? "bg-amber-400 shadow-amber-500/40"
        : "bg-red-500 shadow-red-500/60"
    return <span className={`w-1.5 h-1.5 rounded-full shadow-md ${cls}`} aria-hidden />
}
