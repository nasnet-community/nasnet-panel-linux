import { useId, useMemo } from "react"
import { Area, AreaChart, ResponsiveContainer } from "recharts"
import { cn } from "@/lib/utils"
import { useChartPalette } from "@/lib/design/palette"

interface SparklineProps {
    data: number[]
    color?: string
    height?: number
    className?: string
    showGradient?: boolean
}

export function Sparkline({
    data,
    color,
    height = 32,
    className,
    showGradient = true,
}: SparklineProps) {
    const c = useChartPalette()
    const strokeColor = color ?? c.info

    const chartData = useMemo(
        () => data.map((value, index) => ({ index, value })),
        [data]
    )

    // useId gives a per-instance stable id; the old Math.random() suffix could
    // collide between two sparklines and make one steal the other's gradient.
    const gradientId = `spark-${useId().replace(/:/g, "")}`

    if (data.length < 2) return null

    return (
        <div className={cn("w-full", className)} style={{ height }}>
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 1, right: 1, left: 1, bottom: 1 }}>
                    {showGradient && (
                        <defs>
                            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={strokeColor} stopOpacity={0.3} />
                                <stop offset="100%" stopColor={strokeColor} stopOpacity={0} />
                            </linearGradient>
                        </defs>
                    )}
                    <Area
                        type="monotone"
                        dataKey="value"
                        stroke={strokeColor}
                        strokeWidth={1.5}
                        fill={showGradient ? `url(#${gradientId})` : "none"}
                        dot={false}
                        isAnimationActive={false}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
