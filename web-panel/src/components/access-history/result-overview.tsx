import { useMemo } from "react"
import {
    Area,
    AreaChart,
    CartesianGrid,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis,
} from "recharts"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { AlertCircle } from "lucide-react"
import { useSubscriptionAccessHistory } from "@/lib/queries"
import { useChartPalette, tooltipContentStyle } from "@/lib/design/palette"
import { cn } from "@/lib/utils"
import type { AccessHistoryGranularity } from "@/lib/types"

interface Props {
    subscriptionId: number
    from: string
    to: string
    nodeIDs?: number[]
    includeIPs: boolean
    granularity?: AccessHistoryGranularity | "auto"
}

function formatLargeNumber(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
    return n.toLocaleString()
}

function formatBucket(iso: string, granularity: AccessHistoryGranularity): string {
    const d = new Date(iso)
    if (granularity === "day") {
        return d.toLocaleDateString([], { month: "short", day: "numeric" })
    }
    return d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

export function ResultOverview({ subscriptionId, from, to, nodeIDs, includeIPs, granularity = "auto" }: Props) {
    const params = useMemo(() => ({
        from,
        to,
        node_ids: nodeIDs && nodeIDs.length > 0 ? nodeIDs : undefined,
        include_ips: includeIPs,
        granularity: granularity === "auto" ? undefined : granularity,
        top_n: 20,
    }), [from, to, nodeIDs, includeIPs, granularity])

    const { data, isLoading, isError, error } = useSubscriptionAccessHistory(
        subscriptionId,
        params,
        true,
    )

    if (isLoading && !data) {
        return (
            <div className="space-y-4 animate-in fade-in duration-300">
                <div className="grid grid-cols-2 md:grid-cols-4 gap-3 md:gap-4">
                    {[...Array(4)].map((_, i) => <Skeleton key={i} className="h-[78px] rounded-lg" />)}
                </div>
                <Skeleton className="h-[260px] rounded-lg" />
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 md:gap-4">
                    <Skeleton className="h-[240px] rounded-lg" />
                    <Skeleton className="h-[240px] rounded-lg" />
                </div>
            </div>
        )
    }
    if (isError) {
        return (
            <Card className="p-5 text-sm text-red-400 flex items-center gap-2.5">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>Failed to load overview: {error instanceof Error ? error.message : String(error)}</span>
            </Card>
        )
    }
    if (!data) return null
    const totals = data.totals
    if (totals.hour_buckets === 0) {
        return (
            <Card className="p-10 text-center space-y-2">
                <p className="text-sm font-medium">No activity for this subscription</p>
                <p className="text-xs text-muted-foreground">Try widening the date range.</p>
            </Card>
        )
    }
    const resolvedGranularity = data.granularity ?? "hour"

    return (
        <div className="space-y-4 animate-in fade-in duration-300">
            <TotalsRow totals={totals} />
            <ChartCard series={data.series ?? []} granularity={resolvedGranularity} />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 md:gap-4">
                <DomainList title="Top domains" rows={data.top_domains ?? []} accent="emerald" />
                <DomainList title="Rejected domains" rows={data.top_rejected ?? []} accent="red" />
            </div>
            {includeIPs && (data.top_source_ips?.length ?? 0) > 0 && (
                <IPList rows={data.top_source_ips ?? []} />
            )}
        </div>
    )
}

function TotalsRow({ totals }: { totals: { accepted_count: number; rejected_count: number; tcp_count: number; udp_count: number; hour_buckets: number } }) {
    const items = [
        { label: "Accepted", value: totals.accepted_count, color: "text-emerald-400" },
        { label: "Rejected", value: totals.rejected_count, color: "text-red-400" },
        { label: "TCP", value: totals.tcp_count, color: "text-sky-400" },
        { label: "UDP", value: totals.udp_count, color: "text-amber-400" },
    ]
    return (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 md:gap-4">
            {items.map(it => (
                <Card key={it.label} className="p-3 md:p-4 lg:p-5">
                    <p className="text-[11px] uppercase tracking-wider text-muted-foreground font-semibold">{it.label}</p>
                    <p className={cn("text-2xl md:text-3xl font-bold tabular-nums tracking-tight mt-1 font-mono", it.color)}>
                        {formatLargeNumber(it.value)}
                    </p>
                </Card>
            ))}
        </div>
    )
}

function ChartCard({ series, granularity }: { series: { bucket: string; accepted_count: number; rejected_count: number; tcp_count: number; udp_count: number }[]; granularity: AccessHistoryGranularity }) {
    const c = useChartPalette()
    const data = series.map(b => ({
        time: formatBucket(b.bucket, granularity),
        accepted: b.accepted_count,
        rejected: b.rejected_count,
    }))
    return (
        <Card className="p-4 md:p-5">
            <div className="flex items-center justify-between mb-3">
                <p className="text-xs uppercase font-semibold text-muted-foreground tracking-wider">
                    Requests over time
                </p>
                <span className="text-[10px] uppercase tracking-wider text-muted-foreground font-mono">
                    {granularity === "day" ? "daily" : "hourly"}
                </span>
            </div>
            <div className="h-[240px] w-full">
                <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={data} margin={{ top: 5, right: 5, left: 0, bottom: 5 }}>
                        <defs>
                            <linearGradient id="ro-acc" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={c.success} stopOpacity={0.45} />
                                <stop offset="100%" stopColor={c.success} stopOpacity={0} />
                            </linearGradient>
                            <linearGradient id="ro-rej" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="0%" stopColor={c.danger} stopOpacity={0.4} />
                                <stop offset="100%" stopColor={c.danger} stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid vertical={false} stroke={c.grid} strokeDasharray="3 3" />
                        <XAxis dataKey="time" tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} interval={Math.max(Math.floor(data.length / 8), 0)} />
                        <YAxis tick={{ fontSize: 11, fill: c.axis }} axisLine={false} tickLine={false} orientation="right" tickFormatter={formatLargeNumber} width={50} />
                        <Tooltip
                            contentStyle={{ ...tooltipContentStyle(c), fontSize: 12 }}
                            labelStyle={{ color: c.tooltipLabel, fontSize: 11 }}
                        />
                        <Area type="monotone" dataKey="accepted" stroke={c.success} strokeWidth={2} fill="url(#ro-acc)" dot={false} />
                        <Area type="monotone" dataKey="rejected" stroke={c.danger} strokeWidth={2} fill="url(#ro-rej)" dot={false} />
                    </AreaChart>
                </ResponsiveContainer>
            </div>
        </Card>
    )
}

function DomainList({ title, rows, accent }: { title: string; rows: { domain: string; count: number }[]; accent: "emerald" | "red" }) {
    const accentClass = accent === "emerald" ? "text-emerald-400" : "text-red-400"
    if (rows.length === 0) {
        return (
            <Card className="p-4 md:p-5">
                <p className="text-xs uppercase font-semibold text-muted-foreground tracking-wider mb-2">{title}</p>
                <p className="text-sm text-muted-foreground">None</p>
            </Card>
        )
    }
    return (
        <Card className="p-0 overflow-hidden">
            <div className="px-4 md:px-5 pt-4 pb-2">
                <p className="text-xs uppercase font-semibold text-muted-foreground tracking-wider">{title}</p>
            </div>
            <div className="max-h-[260px] overflow-y-auto">
                <table className="w-full text-sm">
                    <tbody>
                        {rows.slice(0, 20).map(r => (
                            <tr key={r.domain} className="border-t border-border/40 hover:bg-muted/30 transition-colors">
                                <td className="px-4 md:px-5 py-2 font-mono text-xs truncate max-w-[260px]">{r.domain}</td>
                                <td className={cn("px-4 md:px-5 py-2 text-right font-mono tabular-nums font-semibold", accentClass)}>{formatLargeNumber(r.count)}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </Card>
    )
}

function IPList({ rows }: { rows: { ip: string; count: number }[] }) {
    return (
        <Card className="p-0 overflow-hidden">
            <div className="px-4 md:px-5 pt-4 pb-2">
                <p className="text-xs uppercase font-semibold text-muted-foreground tracking-wider">Top source IPs</p>
            </div>
            <div className="max-h-[260px] overflow-y-auto">
                <table className="w-full text-sm">
                    <tbody>
                        {rows.slice(0, 20).map(r => (
                            <tr key={r.ip} className="border-t border-border/40 hover:bg-muted/30 transition-colors">
                                <td className="px-4 md:px-5 py-2 font-mono text-xs">{r.ip}</td>
                                <td className="px-4 md:px-5 py-2 text-right font-mono tabular-nums font-semibold">{formatLargeNumber(r.count)}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </Card>
    )
}
