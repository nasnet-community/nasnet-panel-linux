import { Area, AreaChart, ResponsiveContainer, YAxis, Tooltip, CartesianGrid } from "recharts"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"

interface StatsChartProps {
    data: any[]
    dataKey: string
    color?: string
    height?: number
}

export function StatsChart({ data, dataKey, color, height = 60 }: StatsChartProps) {
    const c = useChartPalette()
    const strokeColor = color ?? c.success
    return (
        <div style={{ height }}>
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={data} margin={{ top: 5, right: 0, left: 0, bottom: 0 }}>
                    <defs>
                        <linearGradient id={`gradient-${dataKey}`} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={strokeColor} stopOpacity={0.4} />
                            <stop offset="50%" stopColor={strokeColor} stopOpacity={0.1} />
                            <stop offset="100%" stopColor={strokeColor} stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid
                        vertical={false}
                        stroke={c.grid}
                        strokeDasharray="3 3"
                    />
                    <YAxis domain={[0, 100]} hide />
                    <Tooltip
                        contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }}
                        itemStyle={{ color: c.tooltipText, fontSize: "12px", fontWeight: 600 }}
                        labelStyle={{ display: "none" }}
                        formatter={(value: any) => [`${Number(value).toFixed(1)}%`]}
                        separator=""
                        cursor={{ stroke: strokeColor, strokeWidth: 1, strokeDasharray: "3 3", opacity: 0.5 }}
                    />
                    <Area
                        type="monotone"
                        dataKey={dataKey}
                        stroke={strokeColor}
                        fill={`url(#gradient-${dataKey})`}
                        strokeWidth={2}
                        isAnimationActive={true}
                        animationDuration={1500}
                        animationEasing="ease-out"
                        style={{ filter: `drop-shadow(0 0 4px ${strokeColor})` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
