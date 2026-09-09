import { useState, useCallback, useMemo, useRef, useEffect } from "react"
import { motion, AnimatePresence } from "framer-motion"
import { TrendingUp, TrendingDown, Minus } from "lucide-react"
import { Area, AreaChart, ResponsiveContainer, YAxis, CartesianGrid, Tooltip } from "recharts"
import { useChartPalette } from "@/lib/design/palette"
import {
    readTrend, trendToneClass, seriesAverage,
    type MetricPolarity,
} from "./starlink-helpers"
import { StarlinkCard, T } from "./starlink-ui"

interface StarlinkMetricCardProps {
    label: string
    value: string
    unit: string
    valueColor: string
    sparklineData: { value: number }[]
    sparklineColor: string
    sparklineId: string
    onClick: () => void
    /** Formats the summary numbers (average, peak) shown under the value. */
    format: (v: number) => string
    /** Which direction is good. Decides the trend colour, nothing else. */
    polarity: MetricPolarity
    /** Series average below which a delta percentage is noise, not signal. */
    deltaFloor?: number
}

export function StarlinkMetricCard({
    label, value, unit, valueColor,
    sparklineData, sparklineColor, sparklineId,
    onClick, format, polarity, deltaFloor = 0,
}: StarlinkMetricCardProps) {
    const [hoveredValue, setHoveredValue] = useState<number | null>(null)
    const onHoverValue = useCallback((v: number | null) => setHoveredValue(v), [])

    const displayValue = hoveredValue !== null ? format(hoveredValue) : value

    const values = useMemo(() => sparklineData.map(d => d.value), [sparklineData])
    const trend = useMemo(() => readTrend(values, polarity, deltaFloor), [values, polarity, deltaFloor])
    const avg = useMemo(() => seriesAverage(values), [values])
    const peak = useMemo(() => values.reduce((m, v) => Math.max(m, v), 0), [values])

    const TrendIcon = trend.tone === "flat" ? Minus
        : trend.deltaPct > 0 ? TrendingUp : TrendingDown

    return (
        <StarlinkCard
            title={label}
            onClick={onClick}
            ariaLabel={`${label}: ${value} ${unit}. Average ${format(avg)}, peak ${format(peak)}. Open detail.`}
        >
            <div>
                <p className="flex items-baseline gap-1.5">
                    <span className={`${T.value} ${valueColor}`}>
                        <SmoothValue value={displayValue} />
                    </span>
                    <span className={T.unit}>{unit}</span>
                </p>

                <div className={`mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 ${T.meta}`}>
                    <span className={`inline-flex items-center gap-0.5 font-semibold tabular-nums ${trendToneClass(trend.tone)}`}>
                        <TrendIcon className="h-3 w-3" aria-hidden />
                        {trend.steady
                            ? "steady"
                            : trend.tone === "flat"
                                ? "flat"
                                : `${trend.deltaPct > 0 ? "+" : ""}${trend.deltaPct.toFixed(0)}%`}
                    </span>
                    {sparklineData.length > 0 && (
                        <span className="whitespace-nowrap tabular-nums">
                            vs avg {format(avg)} · peak {format(peak)}
                        </span>
                    )}
                </div>
            </div>

            {/* Rendered at every width. These used to be `hidden sm:block`,
                which kept Recharts mounted into a zero-size box on phones —
                116 "width(0) and height(0)" warnings a minute, and no chart. */}
            <Sparkline
                data={sparklineData}
                color={sparklineColor}
                id={sparklineId}
                onHoverValue={onHoverValue}
            />
        </StarlinkCard>
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

function Sparkline({
    data, color, id, onHoverValue,
}: {
    data: { value: number }[]
    color: string
    id: string
    onHoverValue?: (v: number | null) => void
}) {
    const c = useChartPalette()
    const animatedRef = useRef(false)
    useEffect(() => { if (data.length > 0) animatedRef.current = true }, [data])

    const yMax = (data.reduce((m, d) => Math.max(m, d.value), 0) || 1) * 1.1

    return (
        <div style={{ height: 44 }}>
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                    data={data}
                    margin={{ top: 2, right: 0, left: 0, bottom: 0 }}
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
                    <Area
                        type="monotone"
                        dataKey="value"
                        stroke={color}
                        fill={`url(#sl-spark-${id})`}
                        strokeWidth={2}
                        dot={false}
                        activeDot={{ r: 4, fill: color, stroke: color, strokeWidth: 2 }}
                        isAnimationActive={!animatedRef.current}
                        animationDuration={800}
                        animationEasing="ease-out"
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
