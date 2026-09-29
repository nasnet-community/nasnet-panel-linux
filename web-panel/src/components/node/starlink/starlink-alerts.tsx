import { AlertTriangle, CheckCircle2 } from "lucide-react"
import type { StarlinkStatus, StarlinkDataPoint, StarlinkEvent } from "@/lib/types"
import { getActiveAlerts, buildAlertTimeline, eventReasonLabel, formatDurationSecs } from "./starlink-helpers"
import { T } from "./starlink-ui"

interface StarlinkAlertsProps {
    status: StarlinkStatus
    history: StarlinkDataPoint[]
    events?: StarlinkEvent[]
}

function formatTimeShort(dateStr: string): string {
    return new Date(dateStr).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

export function StarlinkAlerts({ status, history, events }: StarlinkAlertsProps) {
    const alerts = getActiveAlerts(status)
    const timeline = buildAlertTimeline(history)

    return (
        <div className="space-y-4">
            {/* Current Alerts */}
            <div>
                <p className={`${T.eyebrow} mb-2`}>Current</p>
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
                                "bg-muted/30 border border-border text-muted-foreground"
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
                    <p className={`${T.eyebrow} mb-2`}>History</p>
                    <div className="space-y-1.5">
                        {timeline.slice(0, 20).map((entry, i) => (
                            <div key={i} className="flex items-center justify-between text-sm p-2.5 rounded-lg bg-muted/20 border border-border">
                                <span className="text-muted-foreground font-medium">{entry.name}</span>
                                <span className="font-mono text-xs text-muted-foreground">
                                    {formatTimeShort(entry.startTime)} – {entry.endTime ? formatTimeShort(entry.endTime) : "now"}
                                </span>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {events && events.length > 0 && (
                <div>
                    <p className={`${T.eyebrow} mb-2`}>Recent dish events</p>
                    <div className="space-y-1.5">
                        {[...events]
                            .sort((a, b) => new Date(b.start_at).getTime() - new Date(a.start_at).getTime())
                            .slice(0, 10)
                            .map((e, i) => (
                                <div key={i} className="flex items-center justify-between rounded-lg border border-border bg-muted/20 p-2.5 text-sm">
                                    <span className="font-medium text-muted-foreground">{eventReasonLabel(e.reason)}</span>
                                    <span className="font-mono text-xs text-muted-foreground">
                                        {formatTimeShort(e.start_at)} · {formatDurationSecs(e.duration_ms / 1000)}
                                    </span>
                                </div>
                            ))}
                    </div>
                </div>
            )}
        </div>
    )
}
