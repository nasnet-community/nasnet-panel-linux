import { Area, AreaChart, ResponsiveContainer, YAxis, Tooltip, XAxis, CartesianGrid, Legend } from "recharts"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"

interface ConnectionChartProps {
    data: any[]
}

export function ConnectionChart({ data }: ConnectionChartProps) {
    const c = useChartPalette()
    // Add relative time labels — stats are collected every ~5s
    const INTERVAL_SEC = 5
    const chartData = data.map((d, i) => {
        if (i === data.length - 1) return { ...d, displayTime: "now" }
        const secsAgo = (data.length - 1 - i) * INTERVAL_SEC
        const label = secsAgo >= 60 ? `-${(secsAgo / 60).toFixed(0)}m` : `-${secsAgo}s`
        return { ...d, displayTime: label }
    })

    const formatCount = (value: number) => {
        if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
        if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
        return `${value}`
    }

    return (
        <div className="w-full h-full">
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 5 }}>
                    <defs>
                        <linearGradient id="gradient-tcp" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={c.info} stopOpacity={0.3} />
                            <stop offset="100%" stopColor={c.info} stopOpacity={0} />
                        </linearGradient>
                        <linearGradient id="gradient-udp" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={c.chart6} stopOpacity={0.3} />
                            <stop offset="100%" stopColor={c.chart6} stopOpacity={0} />
                        </linearGradient>
                        <linearGradient id="gradient-fd" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={c.warning} stopOpacity={0.2} />
                            <stop offset="100%" stopColor={c.warning} stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid
                        vertical={false}
                        stroke={c.grid}
                        strokeDasharray="3 3"
                    />
                    <XAxis
                        dataKey="displayTime"
                        axisLine={false}
                        tickLine={false}
                        tick={{ fontSize: 10, fill: c.axis }}
                        interval={Math.floor(data.length / 5)}
                    />
                    <YAxis
                        tickFormatter={formatCount}
                        width={55}
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
                            const s = String(label)
                            return s === "now" ? "Now" : `${s.replace("-", "")} ago`
                        }}
                        formatter={(value: any, name: string | undefined) => [formatCount(Number(value)), name || ""]}
                        cursor={{ stroke: "rgba(255,255,255,0.1)", strokeWidth: 1 }}
                    />
                    <Area
                        type="monotone"
                        dataKey="tcp_count"
                        stroke={c.info}
                        fill="url(#gradient-tcp)"
                        strokeWidth={2}
                        isAnimationActive={true}
                        animationDuration={1000}
                        animationEasing="ease-out"
                        name="TCP"
                        style={{ filter: `drop-shadow(0 0 3px ${c.info})` }}
                    />
                    <Area
                        type="monotone"
                        dataKey="udp_count"
                        stroke={c.chart6}
                        fill="url(#gradient-udp)"
                        strokeWidth={2}
                        isAnimationActive={true}
                        animationDuration={1000}
                        animationEasing="ease-out"
                        name="UDP"
                        style={{ filter: `drop-shadow(0 0 3px ${c.chart6})` }}
                    />
                    <Area
                        type="monotone"
                        dataKey="fd_count"
                        stroke={c.warning}
                        fill="url(#gradient-fd)"
                        strokeWidth={1.5}
                        strokeDasharray="4 2"
                        isAnimationActive={true}
                        animationDuration={1000}
                        animationEasing="ease-out"
                        name="File Descriptors"
                        style={{ filter: `drop-shadow(0 0 3px ${c.warning})` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}
