import type { ReactNode } from "react"
import { Clock } from "lucide-react"
import { cn } from "@/lib/utils"
import { T } from "../node-ui"
import { AreaSparkline, type SparkPoint } from "./area-sparkline"
import { OverviewCard, type CardGlow } from "./overview-card"
import { toneText, type Tone } from "./overview-helpers"

/** "Last reading 14:29" under a value the node can't currently refresh. */
export function StaleStamp({ label, className }: { label: string; className?: string }) {
    return (
        <span className={cn(T.meta, "inline-flex items-center gap-1.5 text-muted-foreground/80", className)}>
            <Clock className="h-3 w-3 shrink-0" aria-hidden />
            {label}
        </span>
    )
}

interface ResourceCardProps {
    title: string
    aside?: ReactNode
    /** Percent in use; undefined while loading. */
    value?: number
    subline: ReactNode
    /** Last hour of readings, 0–100, oldest first; null is a gap. */
    data: SparkPoint[]
    color: string
    tone: Tone
    /** Set when the value isn't live: the stamp replaces the subline. */
    stale?: string | null
    threshold?: { value: number; color: string }
    glow?: CardGlow
    /** Tooltip series name; defaults to the title. */
    sparkLabel?: string
}

export function ResourceCard({ title, aside, value, subline, data, color, tone, stale, threshold, glow, sparkLabel }: ResourceCardProps) {
    const hasHistory = data.some(p => p.v !== null)
    return (
        <OverviewCard title={title} aside={aside} glow={glow}>
            <div>
                <div className={cn(stale && "text-muted-foreground")}>
                    <span className={cn(T.value, !stale && toneText[tone])}>{value !== undefined ? Math.round(value) : "—"}</span>
                    {value !== undefined && <span className={T.unit}>%</span>}
                </div>
                <div className="mt-0.5">
                    {stale ? <StaleStamp label={stale} /> : <p className={T.meta}>{subline}</p>}
                </div>
            </div>
            {hasHistory && <AreaSparkline grow data={data} color={color} threshold={threshold} label={sparkLabel ?? title} />}
        </OverviewCard>
    )
}
