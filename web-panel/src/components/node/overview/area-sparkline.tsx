import { useId } from "react"
import { Area, AreaChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, YAxis } from "recharts"
import { cn } from "@/lib/utils"
import { T } from "../node-ui"
import { clockTime } from "./overview-helpers"

export interface SparkPoint {
    /** Unix ms. */
    t: number
    /** Null is a gap: nothing recorded then. */
    v: number | null
}

interface AreaSparklineProps {
    data: SparkPoint[]
    color: string
    max?: number
    height?: number
    /** Dashed marker line at this value, e.g. a warning threshold. */
    threshold?: { value: number; color: string }
    format?: (v: number) => string
    /** Series name in the tooltip, e.g. "CPU". */
    label?: string
    /** Fill the space left in the card (height becomes the minimum). */
    grow?: boolean
    className?: string
}

/**
 * The resource cards' sparkline: glowing line over a fading fill on a fixed
 * 0–max scale, with a dashed cursor and the reading on hover.
 */
export function AreaSparkline({ data, color, max = 100, height = 45, threshold, format = (v) => `${v.toFixed(1)}%`, label, grow, className }: AreaSparklineProps) {
    const gradientId = useId().replace(/:/g, "")
    return (
        <div className={cn("relative w-full", grow && "flex-1", className)} style={grow ? { minHeight: height } : { height }}>
            <ResponsiveContainer width="100%" height="100%" className={grow ? "absolute inset-0" : undefined}>
                <AreaChart data={data} margin={{ top: 5, right: 0, left: 0, bottom: 0 }}>
                    <defs>
                        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={color} stopOpacity={0.4} />
                            <stop offset="50%" stopColor={color} stopOpacity={0.1} />
                            <stop offset="100%" stopColor={color} stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid vertical={false} stroke="rgba(255,255,255,0.05)" strokeDasharray="3 3" />
                    <YAxis domain={[0, max]} hide allowDataOverflow />
                    {threshold && (
                        <ReferenceLine y={threshold.value} stroke={threshold.color} strokeOpacity={0.45} strokeDasharray="3 3" />
                    )}
                    <Tooltip
                        cursor={{ stroke: color, strokeWidth: 1, strokeDasharray: "3 3", opacity: 0.5 }}
                        wrapperStyle={{ outline: "none", zIndex: 10 }}
                        // Pinned to the top of the chart: the card clips anything below it.
                        position={{ y: 0 }}
                        allowEscapeViewBox={{ x: false, y: true }}
                        content={({ active, payload }) => {
                            const p = payload?.[0]?.payload as SparkPoint | undefined
                            if (!active || !p || p.v === null) return null
                            return (
                                <div className="whitespace-nowrap rounded-lg border border-border bg-popover px-3 py-2 text-xs shadow-lg">
                                    <div className={cn(T.meta, "mb-1")}>{clockTime(p.t)} (average)</div>
                                    <div className="tabular-nums">
                                        {label && <span style={{ color }}>{label} </span>}
                                        {format(p.v)}
                                    </div>
                                </div>
                            )
                        }}
                    />
                    <Area
                        type="monotone"
                        dataKey="v"
                        stroke={color}
                        fill={`url(#${gradientId})`}
                        strokeWidth={2}
                        connectNulls={false}
                        isAnimationActive
                        animationDuration={1500}
                        animationEasing="ease-out"
                        style={{ filter: `drop-shadow(0 0 4px ${color}60)` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
