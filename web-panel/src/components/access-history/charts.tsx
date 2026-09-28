import { useId } from "react"
import { Area, AreaChart, CartesianGrid, Line, ComposedChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { useChartPalette } from "@/lib/design/palette"
import { dayTicks, fmtCompact, fmtDayTick, fmtDayTime, fmtInt, fmtUTC } from "./format"

export interface HourPoint {
    t: number
    accepted: number
    rejected: number
    /** Source-IP matches, only when the search includes them. */
    sourceIp?: number
    nodes?: Record<string, number>
}

const total = (p: HourPoint) => p.accepted + p.rejected + (p.sourceIp ?? 0)

function useGradient(color: string) {
    const id = useId().replace(/:/g, "")
    const def = (
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.4} />
            <stop offset="50%" stopColor={color} stopOpacity={0.1} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
        </linearGradient>
    )
    return { id, def }
}

const tooltipBox = "rounded-lg border border-border bg-popover px-3 py-2 text-xs shadow-lg"

/**
 * Hits per hour under the summary figures. Clicking a point hands back the
 * start of that local day so the page can zoom the window to it.
 */
export function HitsSparkline({ points, onPickDay }: { points: HourPoint[]; onPickDay?: (t: number) => void }) {
    const c = useChartPalette()
    const g = useGradient(c.success)
    if (points.length === 0) return null
    const from = points[0].t
    const to = points[points.length - 1].t
    return (
        <div className="h-[72px] w-full">
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                    data={points}
                    margin={{ top: 4, right: 0, left: 0, bottom: 0 }}
                    onClick={(e) => {
                        const t = Number((e as { activeLabel?: number | string } | null)?.activeLabel)
                        if (onPickDay && Number.isFinite(t)) onPickDay(t)
                    }}
                    className={onPickDay ? "cursor-pointer" : undefined}
                >
                    <defs>{g.def}</defs>
                    <CartesianGrid horizontal={false} verticalValues={dayTicks(from, to, 31)} stroke="var(--border)" strokeDasharray="3 3" />
                    <XAxis
                        dataKey="t"
                        type="number"
                        domain={[from, to]}
                        ticks={dayTicks(from, to)}
                        tickFormatter={fmtDayTick}
                        tick={{ fontSize: 12, fill: c.axis }}
                        axisLine={false}
                        tickLine={false}
                        height={18}
                    />
                    <YAxis hide domain={[0, "dataMax"]} />
                    <Tooltip
                        cursor={{ stroke: c.success, strokeWidth: 1, strokeDasharray: "3 3", opacity: 0.5 }}
                        wrapperStyle={{ outline: "none", zIndex: 10 }}
                        content={({ active, payload }) => {
                            const p = payload?.[0]?.payload as HourPoint | undefined
                            if (!active || !p) return null
                            return (
                                <div className={tooltipBox}>
                                    <div className="mb-0.5 text-muted-foreground">{fmtDayTime(p.t)}</div>
                                    <div className="font-mono tabular-nums">{fmtInt(total(p))} hits</div>
                                </div>
                            )
                        }}
                    />
                    <Area
                        type="monotone"
                        dataKey={total}
                        stroke={c.success}
                        strokeWidth={1.75}
                        fill={`url(#${g.id})`}
                        isAnimationActive={false}
                        style={{ filter: `drop-shadow(0 0 4px color-mix(in oklab, ${c.success} 38%, transparent))` }}
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    )
}

/**
 * Accepted and rejected per bucket, with a per-node split in the tooltip
 * when the points carry one.
 */
export function HitsChart({
    points,
    nodeNames,
    height = 240,
    bucketLabel = fmtDayTime,
}: {
    points: HourPoint[]
    nodeNames?: Map<number, string>
    height?: number
    bucketLabel?: (t: number) => string
}) {
    const c = useChartPalette()
    const g = useGradient(c.success)
    if (points.length === 0) return null
    const from = points[0].t
    const to = points[points.length - 1].t
    const withIPs = points.some(p => (p.sourceIp ?? 0) > 0)
    return (
        <div className="w-full" style={{ height }}>
            <ResponsiveContainer width="100%" height="100%">
                <ComposedChart data={points} margin={{ top: 8, right: 0, left: 0, bottom: 0 }}>
                    <defs>{g.def}</defs>
                    <CartesianGrid stroke={c.grid} strokeDasharray="3 3" verticalValues={dayTicks(from, to, 31)} />
                    <XAxis
                        dataKey="t"
                        type="number"
                        domain={[from, to]}
                        ticks={dayTicks(from, to)}
                        tickFormatter={fmtDayTick}
                        tick={{ fontSize: 12, fill: c.axis }}
                        axisLine={false}
                        tickLine={false}
                    />
                    <YAxis
                        orientation="right"
                        tickFormatter={fmtCompact}
                        tick={{ fontSize: 12, fill: c.axis }}
                        axisLine={false}
                        tickLine={false}
                        width={44}
                    />
                    <Tooltip
                        cursor={{ stroke: c.success, strokeWidth: 1, strokeDasharray: "3 3", opacity: 0.5 }}
                        wrapperStyle={{ outline: "none", zIndex: 10 }}
                        content={({ active, payload }) => {
                            const p = payload?.[0]?.payload as HourPoint | undefined
                            if (!active || !p) return null
                            const nodes = Object.entries(p.nodes ?? {})
                            return (
                                <div className={`${tooltipBox} min-w-[190px]`}>
                                    <div className="font-medium text-foreground">{bucketLabel(p.t)}</div>
                                    <div className="mb-1.5 font-mono text-[12px] text-muted-foreground">{fmtUTC(p.t)}</div>
                                    <div className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5 tabular-nums">
                                        <span className="flex items-center gap-1.5"><span className="size-2 rounded-full" style={{ background: c.success }} />Accepted</span>
                                        <span className="text-right font-mono font-semibold">{fmtInt(p.accepted)}</span>
                                        <span className="flex items-center gap-1.5"><span className="size-2 rounded-full" style={{ background: c.danger }} />Rejected</span>
                                        <span className="text-right font-mono font-semibold">{fmtInt(p.rejected)}</span>
                                        {withIPs && (
                                            <>
                                                <span className="flex items-center gap-1.5"><span className="size-2 rounded-full" style={{ background: c.info }} />Source IP</span>
                                                <span className="text-right font-mono font-semibold">{fmtInt(p.sourceIp ?? 0)}</span>
                                            </>
                                        )}
                                    </div>
                                    {nodes.length > 0 && (
                                        <div className="mt-1.5 grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5 border-t border-border pt-1.5 text-muted-foreground tabular-nums">
                                            {nodes.map(([id, v]) => (
                                                <span key={id} className="contents">
                                                    <span>{nodeNames?.get(Number(id)) ?? `Node ${id}`}</span>
                                                    <span className="text-right font-mono">{fmtInt(v)}</span>
                                                </span>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            )
                        }}
                    />
                    <Area
                        type="monotone"
                        dataKey="accepted"
                        stroke={c.success}
                        strokeWidth={1.75}
                        fill={`url(#${g.id})`}
                        isAnimationActive={false}
                        style={{ filter: `drop-shadow(0 0 4px color-mix(in oklab, ${c.success} 38%, transparent))` }}
                    />
                    <Line type="monotone" dataKey="rejected" stroke={c.danger} strokeWidth={1.5} dot={false} isAnimationActive={false} />
                    {withIPs && <Line type="monotone" dataKey="sourceIp" stroke={c.info} strokeWidth={1.5} dot={false} isAnimationActive={false} />}
                </ComposedChart>
            </ResponsiveContainer>
        </div>
    )
}

/** Thin strip marking the buckets that had rejections; taller marks mean more. */
export function RejectionStrip({ points, total }: { points: HourPoint[]; total: number }) {
    if (points.length < 2) return null
    const from = points[0].t
    const span = points[points.length - 1].t - from || 1
    const max = Math.max(1, ...points.map(p => p.rejected))
    return (
        <div className="space-y-1">
            <div className="flex justify-between text-[12px] text-muted-foreground">
                <span>Hours with rejections</span>
                <span className="font-mono">{fmtInt(total)} rejected</span>
            </div>
            <div className="relative h-3 rounded-[3px] bg-muted/60" style={{ marginRight: 44 }}>
                {points.filter(p => p.rejected > 0).map(p => (
                    <span
                        key={p.t}
                        title={`${fmtDayTime(p.t)}: ${fmtInt(p.rejected)} rejected`}
                        className="absolute bottom-0 w-[3px] -translate-x-1/2 rounded-[1px] bg-red-500"
                        style={{ left: `${((p.t - from) / span) * 100}%`, height: `${4 + Math.round((8 * p.rejected) / max)}px` }}
                    />
                ))}
            </div>
        </div>
    )
}
