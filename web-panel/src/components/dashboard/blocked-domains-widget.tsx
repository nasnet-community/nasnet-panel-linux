import { useMemo, useState } from "react"
import {
    Bar,
    BarChart,
    ResponsiveContainer,
    XAxis,
    YAxis,
    Tooltip,
} from "recharts"
import { ChartTooltip } from "@/components/ui/chart-tooltip"
import { WidgetWrapper } from "./widget-wrapper"
import { Skeleton } from "@/components/ui/skeleton"
import { ShieldX } from "lucide-react"
import { useBlockedDomainStats } from "@/lib/queries/use-analytics"
import { useChartPalette } from "@/lib/design/palette"

type Period = 7 | 14 | 30

function formatNumber(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + "M"
    if (n >= 1000) return (n / 1000).toFixed(1) + "K"
    return String(n)
}

interface BlockedDomainsWidgetProps {
    isEditMode?: boolean
}

export function BlockedDomainsWidget({ isEditMode }: BlockedDomainsWidgetProps) {
    const [period, setPeriod] = useState<Period>(7)
    const { data, isLoading } = useBlockedDomainStats({ days: period, top: 10 })
    const c = useChartPalette()

    const chartData = useMemo(() => {
        if (!data?.domains?.length) return []
        return data.domains.map((d) => ({
            domain: d.domain.length > 20 ? d.domain.slice(0, 18) + "..." : d.domain,
            fullDomain: d.domain,
            count: d.rejected_count,
            nodes: d.node_count,
        }))
    }, [data])

    return (
        <WidgetWrapper
            title="Blocked Domains"
            icon={<ShieldX className="w-4 h-4 text-red-500" />}
            isEditMode={isEditMode}
            headerRight={
                <div className="flex items-center gap-1">
                    {([7, 14, 30] as Period[]).map((p) => (
                        <button
                            key={p}
                            onClick={() => setPeriod(p)}
                            className={`px-2 py-0.5 text-[10px] font-medium rounded transition-colors ${
                                period === p
                                    ? "bg-primary text-primary-foreground"
                                    : "text-muted-foreground hover:text-foreground hover:bg-muted"
                            }`}
                        >
                            {p}d
                        </button>
                    ))}
                </div>
            }
        >
            {isLoading ? (
                <div className="space-y-3">
                    <div className="grid grid-cols-3 gap-3">
                        <Skeleton className="h-14" />
                        <Skeleton className="h-14" />
                        <Skeleton className="h-14" />
                    </div>
                    <Skeleton className="h-full min-h-[160px]" />
                </div>
            ) : (
                <div className="flex flex-col gap-3 h-full">
                    {/* Summary stats */}
                    <div className="grid grid-cols-3 gap-3">
                        <div className="rounded-lg border border-red-500/30 bg-red-500/5 px-3 py-2">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Rejected</p>
                            <p className="text-base font-bold mt-0.5 text-red-500">{formatNumber(data?.total_rejected ?? 0)}</p>
                        </div>
                        <div className="rounded-lg border border-border/50 bg-muted/20 px-3 py-2">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Accepted</p>
                            <p className="text-base font-bold mt-0.5">{formatNumber(data?.total_accepted ?? 0)}</p>
                        </div>
                        <div className="rounded-lg border border-border/50 bg-muted/20 px-3 py-2">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Block Rate</p>
                            <p className="text-base font-bold mt-0.5">{(data?.rejection_rate ?? 0).toFixed(1)}%</p>
                        </div>
                    </div>

                    {/* Horizontal Bar Chart */}
                    {chartData.length === 0 ? (
                        <div className="flex items-center justify-center flex-1 min-h-[120px] text-sm text-muted-foreground">
                            No blocked domains recorded
                        </div>
                    ) : (
                        <div className="flex-1 min-h-[140px] [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground">
                            <ResponsiveContainer width="100%" height="100%">
                                <BarChart
                                    data={chartData}
                                    layout="vertical"
                                    margin={{ top: 4, right: 8, left: 0, bottom: 0 }}
                                >
                                    <XAxis
                                        type="number"
                                        axisLine={false}
                                        tickLine={false}
                                        tick={{ fontSize: 10 }}
                                        tickFormatter={formatNumber}
                                    />
                                    <YAxis
                                        type="category"
                                        dataKey="domain"
                                        axisLine={false}
                                        tickLine={false}
                                        tick={{ fontSize: 9 }}
                                        width={120}
                                    />
                                    <Tooltip
                                        content={
                                            <ChartTooltip
                                                labelFormatter={(label) => String(label)}
                                                valueFormatter={(v) => formatNumber(v)}
                                            />
                                        }
                                    />
                                    <Bar
                                        dataKey="count"
                                        name="Rejections"
                                        fill={c.danger}
                                        radius={[0, 4, 4, 0]}
                                        barSize={16}
                                    />
                                </BarChart>
                            </ResponsiveContainer>
                        </div>
                    )}
                </div>
            )}
        </WidgetWrapper>
    )
}
