import { useState, useCallback, useMemo, useRef, useEffect } from "react"
import { motion, AnimatePresence } from "framer-motion"
import { TrendingUp, TrendingDown, Minus } from "lucide-react"
import { Card } from "@/components/ui/card"
import { Area, AreaChart, ResponsiveContainer, YAxis, CartesianGrid, Tooltip, ReferenceArea } from "recharts"
import type { LucideIcon } from "lucide-react"
import { useChartPalette } from "@/lib/design/palette"

interface StarlinkMetricCardProps {
    label: string
    value: string
    unit: string
    subtitle: string
    valueColor: string
    sparklineData: { value: number }[]
    sparklineColor: string
    sparklineId: string
    icon: LucideIcon
    hoverShadow: string
    onClick: () => void
    formatHoverValue?: (v: number) => string
    threshold?: { warn?: number; crit?: number; direction?: "above" | "below" }
}

export function StarlinkMetricCard({
    label, value, unit, subtitle, valueColor,
    sparklineData, sparklineColor, sparklineId,
    icon: Icon, hoverShadow, onClick, formatHoverValue, threshold,
}: StarlinkMetricCardProps) {
    const [hoveredValue, setHoveredValue] = useState<number | null>(null)
    const onHoverValue = useCallback((v: number | null) => setHoveredValue(v), [])

    const displayValue = hoveredValue !== null
        ? (formatHoverValue ? formatHoverValue(hoveredValue) : hoveredValue.toFixed(0))
        : value

    const { trend, deltaPct, avg } = useMemo(() => {
        if (sparklineData.length < 2) return { trend: "flat" as const, deltaPct: 0, avg: 0 }
        const last = sparklineData[sparklineData.length - 1].value
        const a = sparklineData.reduce((s, d) => s + d.value, 0) / sparklineData.length
        const dp = a > 0 ? ((last - a) / a) * 100 : 0
        const t: "up" | "down" | "flat" = Math.abs(dp) < 2 ? "flat" : dp > 0 ? "up" : "down"
        return { trend: t, deltaPct: dp, avg: a }
    }, [sparklineData])

    const TrendIcon = trend === "up" ? TrendingUp : trend === "down" ? TrendingDown : Minus
    const trendColor = trend === "flat" ? "text-muted-foreground/60" : trend === "up" ? "text-amber-400/80" : "text-emerald-400/80"

    const edgeColor = useMemo(() => {
        if (!threshold) return null
        const lastVal = sparklineData[sparklineData.length - 1]?.value ?? 0
        const above = threshold.direction !== "below"
        if (threshold.crit !== undefined && (above ? lastVal >= threshold.crit : lastVal <= threshold.crit)) return "bg-red-500"
        if (threshold.warn !== undefined && (above ? lastVal >= threshold.warn : lastVal <= threshold.warn)) return "bg-amber-500"
        return "bg-emerald-500/60"
    }, [threshold, sparklineData])

    return (
        <Card
            role="button"
            tabIndex={0}
            aria-label={`${label}: ${value} ${unit}. ${subtitle}. Open detail.`}
            className={`relative h-full overflow-hidden group transition-shadow duration-300 hover:shadow-lg ${hoverShadow} rounded-2xl bg-card/50 backdrop-blur-sm border-white/5 cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring`}
            onClick={onClick}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick() } }}
        >
            {edgeColor && (
                <span className={`absolute left-0 top-3 bottom-3 w-[3px] rounded-r ${edgeColor} opacity-80`} aria-hidden />
            )}

            <div className="p-4">
                <div className="flex items-center justify-between mb-3">
                    <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">{label}</p>
                    <Icon className="w-4 h-4 text-muted-foreground/30 group-hover:text-current transition-colors" />
                </div>

                <p className={`text-3xl font-bold tracking-tighter tabular-nums ${valueColor}`}>
                    <SmoothValue value={displayValue} />
                    <span className="text-base font-medium ml-1">{unit}</span>
                </p>

                <div className="mt-1 flex items-center gap-2 text-[11px]">
                    <span className={`inline-flex items-center gap-0.5 font-mono font-semibold ${trendColor}`}>
                        <TrendIcon className="w-3 h-3" />
                        {trend === "flat" ? "—" : `${deltaPct > 0 ? "+" : ""}${deltaPct.toFixed(0)}%`}
                    </span>
                    <span className="text-muted-foreground/60 font-medium">
                        vs avg {formatHoverValue ? formatHoverValue(avg) : avg.toFixed(0)}{unit}
                    </span>
                </div>

                <span className="block mt-1 text-[10px] text-muted-foreground/70 font-medium">{subtitle}</span>

                <div className="mt-2 hidden sm:block">
                    <ThresholdSparkline
                        data={sparklineData}
                        color={sparklineColor}
                        id={sparklineId}
                        onHoverValue={onHoverValue}
                        threshold={threshold}
                    />
                </div>
            </div>
        </Card>
    )
}

function SmoothValue({ value }: { value: string }) {
    return (
        <span className="relative inline-block">
            <AnimatePresence mode="wait" initial={false}>
                <motion.span
                    key={value}
                    initial={{ opacity: 0, y: -4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.18, ease: "easeOut" }}
                    className="inline-block"
                >
                    {value}
                </motion.span>
            </AnimatePresence>
        </span>
    )
}

function ThresholdSparkline({
    data, color, id, onHoverValue, threshold,
}: {
    data: { value: number }[]
    color: string
    id: string
    onHoverValue?: (v: number | null) => void
    threshold?: { warn?: number; crit?: number; direction?: "above" | "below" }
}) {
    const c = useChartPalette()
    const animatedRef = useRef(false)
    useEffect(() => { if (data.length > 0) animatedRef.current = true }, [data])

    const max = data.reduce((m, d) => Math.max(m, d.value), 0) || 1
    const yMax = Math.max(max, threshold?.crit ?? 0, threshold?.warn ?? 0) * 1.1

    return (
        <div style={{ height: 45 }}>
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                    data={data}
                    margin={{ top: 2, right: 0, left: 0, bottom: 0 }}
                    // eslint-disable-next-line @typescript-eslint/no-explicit-any
                    onMouseMove={(state: any) => {
                        if (onHoverValue && state?.activeTooltipIndex != null) {
                            const idx = state.activeTooltipIndex as number
                            if (data[idx]) onHoverValue(data[idx].value)
                        }
                    }}
                    onMouseLeave={() => onHoverValue?.(null)}
                >
                    <defs>
                        <linearGradient id={`sl-spark-${id}`} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={color} stopOpacity={0.4} />
                            <stop offset="50%" stopColor={color} stopOpacity={0.1} />
                            <stop offset="100%" stopColor={color} stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                    <YAxis domain={[0, yMax]} hide />
                    <Tooltip content={() => null} cursor={false} />

                    {threshold?.crit !== undefined && (
                        <ReferenceArea
                            y1={threshold.direction === "below" ? 0 : threshold.crit}
                            y2={threshold.direction === "below" ? threshold.crit : yMax}
                            fill={c.danger} fillOpacity={0.06} stroke="none"
                        />
                    )}
                    {threshold?.warn !== undefined && threshold?.crit !== undefined && (
                        <ReferenceArea
                            y1={threshold.direction === "below" ? threshold.crit : threshold.warn}
                            y2={threshold.direction === "below" ? threshold.warn : threshold.crit}
                            fill={c.warning} fillOpacity={0.05} stroke="none"
                        />
                    )}

                    <Area
                        type="monotone"
                        dataKey="value"
                        stroke={color}
                        fill={`url(#sl-spark-${id})`}
                        strokeWidth={2}
                        dot={false}
                        activeDot={{ r: 4, fill: color, stroke: "#fff", strokeWidth: 2 }}
                        isAnimationActive={!animatedRef.current}
                        animationDuration={800}
                        animationEasing="ease-out"
                        style={{ filter: `drop-shadow(0 0 4px ${color}60)` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
