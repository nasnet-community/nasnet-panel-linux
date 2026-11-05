import type { NodeDigest } from "@/lib/types"
import { cn } from "@/lib/utils"

type Day = NodeDigest["uptime"]["days"][number]

const DAYS = 30

function durationText(sec: number): string {
    if (sec < 60) return `${sec}s`
    if (sec < 3600) return `${Math.round(sec / 60)} min`
    return `${Math.floor(sec / 3600)}h ${Math.round((sec % 3600) / 60)}m`
}

function dayLabel(date: string): string {
    return new Date(`${date}T12:00:00Z`).toLocaleDateString("en-US", { month: "short", day: "numeric" })
}

/** Healthy days stay grey; color only marks time the node was down. */
function markClass(d: Day | undefined): string {
    if (!d || d.observed_sec + d.planned_sec === 0) return "ring-1 ring-inset ring-foreground/15"
    if (d.downtime_sec === 0) return d.planned_sec > 0 ? "bg-blue-500/75" : "bg-foreground/15"
    if (d.downtime_sec < 300) return "bg-amber-500/55"
    if (d.downtime_sec < 3600) return "bg-amber-500"
    return "bg-red-500"
}

function markTitle(date: string, d: Day | undefined, today: boolean): string {
    const head = `${dayLabel(date)}${today ? " so far" : ""}`
    if (!d || d.observed_sec + d.planned_sec === 0) return `${head}: no record`
    if (d.downtime_sec > 0) return `${head}: down ${durationText(d.downtime_sec)}, ${d.outages} outage${d.outages === 1 ? "" : "s"}`
    if (d.planned_sec > 0) return `${head}: ${durationText(d.planned_sec)} of planned maintenance`
    return `${head}: no outages`
}

/** The last 30 UTC days, oldest first, today last and outlined. */
export function UptimeStrip({ days, compact, className }: { days: Day[]; compact?: boolean; className?: string }) {
    const byDate = new Map(days.map((d) => [d.date, d]))
    const today = new Date()
    const dates = Array.from({ length: DAYS }, (_, i) => {
        const d = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - (DAYS - 1 - i)))
        return d.toISOString().slice(0, 10)
    })
    return (
        <span role="img" aria-label="Uptime, last 30 days" className={cn("flex items-stretch", compact ? "h-3 gap-px" : "h-3.5 gap-0.5", className)}>
            {dates.map((date, i) => {
                const d = byDate.get(date)
                const isToday = i === DAYS - 1
                return (
                    <i
                        key={date}
                        title={markTitle(date, d, isToday)}
                        className={cn(
                            "rounded-[1.5px]",
                            compact ? "w-[3px]" : "w-[5px]",
                            markClass(d),
                            isToday && "outline outline-1 outline-offset-1 outline-muted-foreground",
                        )}
                    />
                )
            })}
        </span>
    )
}
