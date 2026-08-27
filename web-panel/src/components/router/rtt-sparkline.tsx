import { cn } from "@/lib/utils"
import type { HealthSample } from "@/lib/types/health"

/** Probe RTT over the window: shape, not the reading beside it. Not the
 *  recharts one in ui/ — several of these render per page. */
export function RttSparkline({
    history,
    className,
    label = "Round-trip time, last 15 minutes",
}: {
    history: HealthSample[] | undefined
    className?: string
    label?: string
}) {
    const samples = (history ?? []).slice(-180)
    if (samples.length < 2) return null
    const max = Math.max(...samples.map((s) => s.rtt_ms), 1)
    const w = 112
    const h = 28
    const step = w / (samples.length - 1)
    const pts = samples.map(
        (s, i) => [(i * step).toFixed(1), (h - (s.rtt_ms / max) * (h - 3) - 1).toFixed(1)] as const,
    )
    const line = pts.map((p) => p.join(",")).join(" ")
    return (
        <svg
            viewBox={`0 0 ${w} ${h}`}
            preserveAspectRatio="none"
            className={cn("text-chart-2 h-7 w-28 shrink-0", className)}
            role="img"
            aria-label={label}
        >
            <polygon points={`0,${h} ${line} ${w},${h}`} fill="currentColor" opacity="0.12" />
            <polyline points={line} fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
    )
}
