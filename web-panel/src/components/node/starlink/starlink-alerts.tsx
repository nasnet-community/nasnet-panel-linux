import { AlertTriangle, CheckCircle2 } from "lucide-react"
import type { StarlinkStatus, StarlinkDataPoint } from "@/lib/types"
import { getActiveAlerts, buildAlertTimeline } from "./starlink-helpers"

interface StarlinkAlertsProps {
    status: StarlinkStatus
    history: StarlinkDataPoint[]
}

function formatTimeShort(dateStr: string): string {
    return new Date(dateStr).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

export function StarlinkAlerts({ status, history }: StarlinkAlertsProps) {
    const alerts = getActiveAlerts(status)
    const timeline = buildAlertTimeline(history)

    return (
        <div className="space-y-4">
            {/* Current Alerts */}
            <div>
                <p className="text-xs uppercase font-bold text-muted-foreground/60 tracking-[0.15em] mb-2">Current</p>
                {alerts.length === 0 ? (
                    <div className="flex items-center gap-2 text-sm text-emerald-400">
                        <CheckCircle2 className="w-4 h-4" />
                        All Systems Normal
                    </div>
                ) : (
                    <div className="space-y-1.5">
                        {alerts.map(alert => (
                            <div key={alert.label} className={`flex items-center gap-2 p-2.5 rounded-lg text-sm font-medium ${
                                alert.severity === "critical" ? "bg-red-500/10 border border-red-500/20 text-red-400" :
                                alert.severity === "warning" ? "bg-amber-500/10 border border-amber-500/20 text-amber-400" :
                                "bg-muted/30 border border-white/5 text-muted-foreground"
                            }`}>
                                {alert.severity === "critical" && <AlertTriangle className="w-3.5 h-3.5 shrink-0" />}
                                {alert.label}
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* Alert Timeline */}
            {timeline.length > 0 && (
                <div>
                    <p className="text-xs uppercase font-bold text-muted-foreground/60 tracking-[0.15em] mb-2">History</p>
                    <div className="space-y-1.5">
                        {timeline.slice(0, 20).map((entry, i) => (
                            <div key={i} className="flex items-center justify-between text-sm p-2.5 rounded-lg bg-muted/20 border border-white/5">
                                <span className="text-muted-foreground font-medium">{entry.name}</span>
                                <span className="font-mono text-xs text-muted-foreground">
                                    {formatTimeShort(entry.startTime)} – {entry.endTime ? formatTimeShort(entry.endTime) : "now"}
                                </span>
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    )
}
