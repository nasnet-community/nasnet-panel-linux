import { useState } from "react"
import { AnimatePresence, motion } from "framer-motion"
import { ChevronDown, Loader2 } from "lucide-react"
import { Card } from "@/components/ui/card"
import { CircularProgress } from "@/components/ui/circular-progress"
import type { NodeStatBucket, NodeStats } from "@/lib/types"
import { cn } from "@/lib/utils"
import { cardShell, T } from "./node-ui"
import { AreaSparkline } from "./overview/area-sparkline"
import { HOST_COLORS, lastReadingLabel } from "./overview/overview-helpers"
import { StaleStamp } from "./overview/resource-card"

type MetricKey = "cpu" | "memory" | "disk"

interface HealthGaugesRowProps {
    stats?: NodeStats
    /** Last hour, averaged per minute. */
    points: NodeStatBucket[]
    live: boolean
    isLoading: boolean
}

const METRICS: { key: MetricKey; label: string; ring: "blue" | "purple" | "teal"; color: string; avg: keyof NodeStatBucket; max: keyof NodeStatBucket }[] = [
    { key: "cpu", label: "CPU", ring: "blue", color: HOST_COLORS.cpu, avg: "cpu_avg", max: "cpu_max" },
    { key: "memory", label: "Memory", ring: "purple", color: HOST_COLORS.memory, avg: "mem_avg", max: "mem_max" },
    { key: "disk", label: "Storage", ring: "teal", color: HOST_COLORS.disk, avg: "disk_avg", max: "disk_avg" },
]

function gb(mb: number) {
    return (mb / 1024).toFixed(1)
}

/** Phone: the three resource gauges; tap one for its last hour. */
export function HealthGaugesRow({ stats, points, live, isLoading }: HealthGaugesRowProps) {
    const [expanded, setExpanded] = useState<MetricKey | null>(null)

    const value: Record<MetricKey, number | undefined> = {
        cpu: stats?.cpu_percent,
        memory: stats?.memory_percent,
        disk: stats?.disk_percent,
    }
    const peak = (k: keyof NodeStatBucket) => points.reduce((m, p) => Math.max(m, Number(p[k]) || 0), 0)
    const sub: Record<MetricKey, string> = {
        cpu: points.length ? `peak ${peak("cpu_max").toFixed(0)}%` : "",
        memory: stats?.memory_total_mb ? `${gb(stats.memory_used_mb)} of ${gb(stats.memory_total_mb)} GB` : "",
        disk: stats?.disk_total_gb ? `${Math.max(stats.disk_total_gb - stats.disk_used_gb, 0)} GB free` : "",
    }
    const open = METRICS.find(m => m.key === expanded)

    return (
        <Card className={cn(cardShell, "p-3")}>
            <div className="grid grid-cols-3">
                {METRICS.map(m => {
                    const v = value[m.key]
                    return (
                        <button
                            key={m.key}
                            type="button"
                            onClick={() => setExpanded(prev => (prev === m.key ? null : m.key))}
                            aria-expanded={expanded === m.key}
                            className="flex min-h-11 flex-col items-center gap-1 rounded-xl px-1 py-1.5 transition-colors active:bg-muted/50"
                        >
                            <div className={cn("relative", !live && "opacity-60")}>
                                <CircularProgress value={v ?? 0} size={72} strokeWidth={6} color={m.ring} showValue={false}>
                                    <span className="text-lg font-bold tracking-tight tabular-nums">{v !== undefined ? `${v.toFixed(0)}%` : "—"}</span>
                                </CircularProgress>
                                {isLoading && <Loader2 className="absolute -right-0.5 -top-0.5 h-3 w-3 animate-spin text-muted-foreground" />}
                            </div>
                            <span className="flex items-center gap-1">
                                <span className={T.eyebrow}>{m.label}</span>
                                <ChevronDown className={cn("h-3 w-3 text-muted-foreground/50 transition-transform duration-200", expanded === m.key && "rotate-180")} />
                            </span>
                            <span className={cn(T.meta, "tabular-nums")}>{live ? sub[m.key] : ""}</span>
                        </button>
                    )
                })}
            </div>
            {!live && stats && <StaleStamp label={lastReadingLabel(stats)} className="justify-center" />}

            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        key={open.key}
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: "easeInOut" }}
                        className="overflow-hidden"
                    >
                        <div className="mt-2 border-t border-border/60 pt-3">
                            {points.length ? (
                                <>
                                    <div className="mb-2 flex justify-between px-2 tabular-nums">
                                        {([
                                            ["Min", Math.min(...points.map(p => Number(p[open.avg]) || 0))],
                                            ["Avg", points.reduce((s, p) => s + (Number(p[open.avg]) || 0), 0) / points.length],
                                            ["Max", peak(open.max)],
                                        ] as const).map(([label, v]) => (
                                            <div key={label} className="flex flex-col items-center">
                                                <span className={T.meta}>{label}</span>
                                                <span className="text-xs font-bold">{v.toFixed(1)}%</span>
                                            </div>
                                        ))}
                                    </div>
                                    <AreaSparkline data={points.map(p => ({ t: p.t * 1000, v: Number(p[open.avg]) }))} color={open.color} height={90} label={open.label} />
                                    <p className={cn(T.meta, "mt-1 text-center")}>Last hour, one-minute averages</p>
                                </>
                            ) : (
                                <p className={cn(T.meta, "py-4 text-center")}>Nothing recorded in the last hour</p>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </Card>
    )
}
