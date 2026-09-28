import { useMemo } from "react"
import { Bar, BarChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { UsageHistoryPoint } from "@/lib/api/subscriptions"
import type { HourlyUsagePoint } from "@/lib/types"
import { cn } from "@/lib/utils"
import { tzName } from "@/components/access-history/format"
import { fmtAxisSize, fmtSize, localHourOffset } from "./format"

interface DayRow {
    day: string // YYYY-MM-DD (UTC)
    label: string
    download: number
    upload: number
    /** Days recorded before the hub split upload from download. */
    unsplit: number
}

// Theme tokens, so the bars follow light and dark like the rest of the panel.
const axis = { tick: "var(--text-tertiary)", label: "var(--text-secondary)" }
const chart = { emerald: "var(--chart-1)", blue: "var(--chart-2)", red: "var(--chart-4)" }

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

/** One row per UTC day for the last `days` days, today included. */
function fillDays(points: UsageHistoryPoint[], days: number): DayRow[] {
    const byDay = new Map(points.map(p => [p.date, p]))
    const rows: DayRow[] = []
    const today = new Date()
    const base = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())
    for (let i = days - 1; i >= 0; i--) {
        const d = new Date(base - i * 86_400_000)
        const day = d.toISOString().slice(0, 10)
        const p = byDay.get(day)
        const split = p?.data_upload != null && p?.data_download != null
        rows.push({
            day,
            label: `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}`,
            download: split ? p!.data_download! : 0,
            upload: split ? p!.data_upload! : 0,
            unsplit: p && !split ? p.data_used : 0,
        })
    }
    return rows
}

function TrafficTooltip({ active, payload }: { active?: boolean; payload?: { payload: DayRow }[] }) {
    if (!active || !payload?.length) return null
    const r = payload[0].payload
    const [y, m, d] = r.day.split("-").map(Number)
    const title = new Date(y, m - 1, d).toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })
    const line = (label: string, color: string, v: number) => (
        <div className="flex items-center justify-between gap-6">
            <span className="flex items-center gap-1.5 text-muted-foreground"><span className="size-2 rounded-sm" style={{ background: color }} />{label}</span>
            <span className="font-mono tabular-nums">{fmtSize(v)}</span>
        </div>
    )
    return (
        <div className="min-w-44 space-y-1 rounded-lg border border-border bg-popover px-3 py-2 text-xs shadow-lg">
            <div className="font-semibold">{title}</div>
            {r.unsplit > 0 ? line("Total", axis.label, r.unsplit) : (
                <>
                    {line("Download", chart.emerald, r.download)}
                    {line("Upload", chart.blue, r.upload)}
                </>
            )}
            <div className="flex justify-between gap-6 border-t border-border pt-1">
                <span className="text-muted-foreground">Total</span>
                <span className="font-mono font-semibold tabular-nums">{fmtSize(r.download + r.upload + r.unsplit)}</span>
            </div>
        </div>
    )
}

/** 0 and three or four round steps up to the day with most traffic. */
function niceTicks(max: number): number[] {
    const GB = 1024 ** 3
    if (max <= 0) return [0, GB]
    const steps = [0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 50, 100, 200, 500].map(g => g * GB)
    const step = steps.find(st => max / st <= 4) ?? steps[steps.length - 1]
    const top = Math.ceil(max / step) * step
    const out: number[] = []
    for (let v = 0; v <= top + 1; v += step) out.push(v)
    return out
}

/** Daily traffic, download under upload. `markDay` draws the day data ran out. */
export function TrafficChart({ points, days, markDay, className }: { points: UsageHistoryPoint[]; days: number; markDay?: string; className?: string }) {
    const rows = useMemo(() => fillDays(points, days), [points, days])
    const hasUnsplit = rows.some(r => r.unsplit > 0)
    const mark = markDay ? rows.find(r => r.day === markDay)?.label : undefined
    const ticks = niceTicks(Math.max(0, ...rows.map(r => r.download + r.upload + r.unsplit)))
    const empty = ticks.length <= 2 && rows.every(r => r.download + r.upload + r.unsplit === 0)
    return (
        <div className={cn("relative h-[150px] w-full", className)}>
            {empty && <span className="pointer-events-none absolute inset-0 flex items-center justify-center pb-5 text-[13px] text-muted-foreground">No traffic in the last {days} days</span>}
            <ResponsiveContainer width="100%" height="100%">
                <BarChart data={rows} margin={{ top: 6, right: 4, left: 0, bottom: 0 }} barCategoryGap="22%">
                    <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
                    <XAxis dataKey="label" tickLine={false} axisLine={false} tick={{ fontSize: 12, fill: axis.tick }} interval="preserveStartEnd" minTickGap={24} />
                    <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 12, fill: axis.tick }} tickFormatter={fmtAxisSize} width={52} ticks={ticks} domain={[0, ticks[ticks.length - 1]]} />
                    <Tooltip content={<TrafficTooltip />} cursor={{ fill: "var(--muted)", opacity: 0.5 }} />
                    <Bar dataKey="download" stackId="t" fill={chart.emerald} maxBarSize={28} />
                    <Bar dataKey="upload" stackId="t" fill={chart.blue} radius={hasUnsplit ? 0 : [3, 3, 0, 0]} maxBarSize={28} />
                    {hasUnsplit && <Bar dataKey="unsplit" stackId="t" fill={axis.label} radius={[3, 3, 0, 0]} maxBarSize={28} />}
                    {mark && <ReferenceLine x={mark} stroke={chart.red} strokeDasharray="4 3" />}
                </BarChart>
            </ResponsiveContainer>
        </div>
    )
}

export function TrafficLegend() {
    return (
        <span className="flex items-center gap-4 text-xs text-muted-foreground">
            <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm" style={{ background: chart.emerald }} />Download</span>
            <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm" style={{ background: chart.blue }} />Upload</span>
        </span>
    )
}

/** Traffic over the days the chart draws (today and the days before it). */
export function sumTraffic(points: UsageHistoryPoint[], days: number): number {
    return fillDays(points, days).reduce((s, r) => s + r.download + r.upload + r.unsplit, 0)
}

const pad = (h: number) => String(h).padStart(2, "0")

/** Local hour-of-day buckets from UTC ones. */
export function toLocalHours(points: HourlyUsagePoint[]): number[] {
    const off = localHourOffset()
    const out = new Array(24).fill(0)
    for (const p of points) out[(((p.hour + off) % 24) + 24) % 24] += p.count
    return out
}

function cellColor(ratio: number): string {
    if (ratio <= 0) return "var(--muted)"
    const a = ratio < 0.2 ? 0.14 : ratio < 0.4 ? 0.3 : ratio < 0.6 ? 0.5 : ratio < 0.8 ? 0.72 : 0.95
    return `rgba(16, 185, 129, ${a})`
}

/** 24 cells of connections by local hour, with the peak called out. */
export function HourStrip({ hours, compact }: { hours: number[]; compact?: boolean }) {
    const max = Math.max(...hours, 0)
    const peak = max > 0 ? hours.indexOf(max) : -1
    return (
        <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-3">
                <span className="flex items-center gap-3">
                    <h4 className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">By hour</h4>
                    <span className="text-xs text-muted-foreground">{compact ? tzName() : `Connections, ${tzName()}`}</span>
                </span>
                {peak >= 0 && (
                    <span className="flex items-baseline gap-2">
                        <span className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">Peak</span>
                        <span className="font-mono text-[13px] font-semibold tabular-nums">{pad(peak)}:00</span>
                    </span>
                )}
            </div>
            <div className={cn("flex", compact ? "gap-0.5" : "gap-1")}>
                {hours.map((v, h) => (
                    <span
                        key={h}
                        title={`${pad(h)}:00, ${v.toLocaleString("en-US")} connections`}
                        className={cn("flex-1", compact ? "h-5 rounded-[3px]" : "h-[22px] rounded")}
                        style={{ background: cellColor(max > 0 ? v / max : 0) }}
                    />
                ))}
            </div>
            <div className={cn("flex font-mono text-[12px] text-muted-foreground", compact ? "justify-between" : "gap-1")}>
                {compact
                    ? [0, 6, 12, 18, 23].map(h => <span key={h}>{pad(h)}</span>)
                    : hours.map((_, h) => <span key={h} className="flex-1">{h % 6 === 0 || h === 23 ? pad(h) : ""}</span>)}
            </div>
        </div>
    )
}
