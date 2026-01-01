import { Area, AreaChart, ResponsiveContainer, YAxis, Tooltip, XAxis, CartesianGrid } from "recharts"
import { formatSpeed } from "@/lib/utils"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"

interface NetworkChartProps {
    data: any[]
}

export function NetworkChart({ data }: NetworkChartProps) {
    const c = useChartPalette()
    // Add relative time for XAxis labels — stats are collected every 5s
    const INTERVAL_SEC = 5;
    const chartData = data.map((d, i) => {
        if (i === data.length - 1) return { ...d, displayTime: 'now' };
        const secsAgo = (data.length - 1 - i) * INTERVAL_SEC;
        const label = secsAgo >= 60 ? `-${(secsAgo / 60).toFixed(0)}m` : `-${secsAgo}s`;
        return { ...d, displayTime: label };
    });

    return (
        <div className="w-full h-full">
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                    <defs>
                        <linearGradient id="gradient-up" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={c.info} stopOpacity={0.4} />
                            <stop offset="50%" stopColor={c.info} stopOpacity={0.15} />
                            <stop offset="100%" stopColor={c.info} stopOpacity={0} />
                        </linearGradient>
                        <linearGradient id="gradient-down" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={c.success} stopOpacity={0.4} />
                            <stop offset="50%" stopColor={c.success} stopOpacity={0.15} />
                            <stop offset="100%" stopColor={c.success} stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid
                        vertical={false}
                        stroke={c.grid}
                        strokeDasharray="3 3"
                    />
                    <XAxis
                        dataKey="displayTime"
                        hide={false}
                        axisLine={false}
                        tickLine={false}
                        tick={{ fontSize: 10, fill: c.axis }}
                        interval={Math.floor(data.length / 5)}
                    />
                    <YAxis
                        tickFormatter={(value) => formatSpeed(value)}
                        width={80}
                        tick={{ fontSize: 11, fill: c.axis }}
                        axisLine={false}
                        tickLine={false}
                        orientation="right"
                    />
                    <Tooltip
                        contentStyle={{ ...tooltipContentStyle(c), backdropFilter: "blur(8px)" }}
                        itemStyle={{ fontSize: "12px", fontWeight: 600, padding: "2px 0" }}
                        labelStyle={{ color: c.tooltipLabel, fontSize: "10px", marginBottom: "8px", letterSpacing: "0.05em" }}
                        labelFormatter={(label) => {
                            const s = String(label);
                            return s === 'now' ? 'Now' : `${s.replace('-', '')} ago`;
                        }}
                        formatter={(value: any, name: string | undefined) => [formatSpeed(Number(value)), name || ""]}
                        cursor={{ stroke: "rgba(255,255,255,0.1)", strokeWidth: 1 }}
                    />
                    <Area
                        type="monotone"
                        dataKey="up_speed"
                        stroke={c.info}
                        fill="url(#gradient-up)"
                        strokeWidth={2}
                        isAnimationActive={true}
                        animationDuration={1000}
                        animationEasing="ease-out"
                        name="Upload"
                        style={{ filter: `drop-shadow(0 0 4px ${c.info})` }}
                    />
                    <Area
                        type="monotone"
                        dataKey="down_speed"
                        stroke={c.success}
                        fill="url(#gradient-down)"
                        strokeWidth={2}
                        isAnimationActive={true}
                        animationDuration={1000}
                        animationEasing="ease-out"
                        name="Download"
                        style={{ filter: `drop-shadow(0 0 4px ${c.success})` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
