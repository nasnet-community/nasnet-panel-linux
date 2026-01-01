import { useMemo } from "react"
import { WidgetWrapper } from "./widget-wrapper"
import { Skeleton } from "@/components/ui/skeleton"
import { CategoryBar } from "@/components/ui/metric-bar"
import { Activity, ArrowUp, ArrowDown } from "lucide-react"
import { useNodeAggregateStats } from "@/lib/queries"
import { formatBytes, formatSpeed } from "@/lib/utils"

const NODE_COLORS = ["bg-blue-500", "bg-violet-500", "bg-amber-500", "bg-emerald-500", "bg-slate-400"]

interface NetworkTrafficWidgetProps {
    isEditMode?: boolean
}

export function NetworkTrafficWidget({ isEditMode }: NetworkTrafficWidgetProps) {
    const { data, isLoading } = useNodeAggregateStats()

    const onlineCount = useMemo(() => {
        if (!data?.nodes) return 0
        return data.nodes.filter((n) => n.isOnline).length
    }, [data])

    const totalUplink = useMemo(() => {
        if (!data?.nodes) return 0
        return data.nodes.reduce((sum, n) => sum + n.totalUplink, 0)
    }, [data])

    const totalDownlink = useMemo(() => {
        if (!data?.nodes) return 0
        return data.nodes.reduce((sum, n) => sum + n.totalDownlink, 0)
    }, [data])

    const totalTraffic = totalUplink + totalDownlink

    const liveUpSpeed = useMemo(() => {
        if (!data?.nodes) return 0
        return data.nodes.reduce((sum, n) => sum + n.upSpeed, 0)
    }, [data])

    const liveDownSpeed = useMemo(() => {
        if (!data?.nodes) return 0
        return data.nodes.reduce((sum, n) => sum + n.downSpeed, 0)
    }, [data])

    const liveBandwidth = liveUpSpeed + liveDownSpeed

    // Per-node breakdown sorted by total traffic descending
    const nodeBreakdown = useMemo(() => {
        if (!data?.nodes?.length) return []
        return [...data.nodes]
            .map((n) => ({
                ...n,
                totalBytes: n.totalUplink + n.totalDownlink,
            }))
            .filter((n) => n.totalBytes > 0 || n.isOnline)
            .sort((a, b) => b.totalBytes - a.totalBytes)
    }, [data])

    const maxNodeTraffic = nodeBreakdown.length > 0 ? nodeBreakdown[0].totalBytes : 1

    return (
        <WidgetWrapper
            title="Network Traffic"
            icon={<Activity className="w-4 h-4 text-blue-500" />}
            isEditMode={isEditMode}
            headerRight={
                <span className="text-[10px] text-muted-foreground">
                    {onlineCount} node{onlineCount !== 1 ? "s" : ""} online
                </span>
            }
        >
            {isLoading ? (
                <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-3">
                        <Skeleton className="h-20" />
                        <Skeleton className="h-20" />
                    </div>
                    <Skeleton className="h-4 w-32" />
                    <div className="space-y-2">
                        <Skeleton className="h-6" />
                        <Skeleton className="h-6" />
                        <Skeleton className="h-6" />
                    </div>
                </div>
            ) : (
                <div className="flex flex-col gap-3 h-full">
                    {/* Summary cards */}
                    <div className="grid grid-cols-2 gap-3">
                        {/* Total Traffic card */}
                        <div className="rounded-lg border border-border/50 bg-muted/20 px-3 py-2 space-y-1.5">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Total Traffic</p>
                            <p className="text-base font-bold">{formatBytes(totalTraffic)}</p>
                            <CategoryBar
                                values={[
                                    { value: totalUplink, color: "bg-violet-500", label: "Upload" },
                                    { value: totalDownlink, color: "bg-orange-500", label: "Download" },
                                ]}
                            />
                            <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
                                <span className="flex items-center gap-0.5">
                                    <ArrowUp className="w-2.5 h-2.5 text-violet-500" />
                                    {formatBytes(totalUplink)}
                                </span>
                                <span className="flex items-center gap-0.5">
                                    <ArrowDown className="w-2.5 h-2.5 text-orange-500" />
                                    {formatBytes(totalDownlink)}
                                </span>
                            </div>
                        </div>

                        {/* Live Bandwidth card */}
                        <div className="rounded-lg border border-border/50 bg-muted/20 px-3 py-2 space-y-1.5">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Live Bandwidth</p>
                            <p className="text-base font-bold">{formatSpeed(liveBandwidth)}</p>
                            <div className="flex items-center gap-3 text-[10px] text-muted-foreground mt-2">
                                <span className="flex items-center gap-0.5">
                                    <ArrowUp className="w-2.5 h-2.5 text-violet-500" />
                                    {formatSpeed(liveUpSpeed)}
                                </span>
                                <span className="flex items-center gap-0.5">
                                    <ArrowDown className="w-2.5 h-2.5 text-orange-500" />
                                    {formatSpeed(liveDownSpeed)}
                                </span>
                            </div>
                        </div>
                    </div>

                    {/* Per-node breakdown */}
                    {nodeBreakdown.length > 0 && (
                        <div className="flex-1 space-y-1.5 min-h-0 overflow-auto">
                            <p className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">Per-Node Breakdown</p>
                            <div className="space-y-1.5">
                                {nodeBreakdown.map((node, i) => {
                                    const pct = totalTraffic > 0 ? (node.totalBytes / totalTraffic) * 100 : 0
                                    const barWidth = maxNodeTraffic > 0 ? (node.totalBytes / maxNodeTraffic) * 100 : 0
                                    const colorClass = NODE_COLORS[i % NODE_COLORS.length]
                                    return (
                                        <div key={node.id} className="flex items-center gap-2 text-xs">
                                            <div className="flex items-center gap-1.5 w-20 shrink-0 min-w-0">
                                                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${node.isOnline ? "bg-emerald-500" : "bg-muted-foreground/40"}`} />
                                                <span className="truncate font-medium">{node.name}</span>
                                            </div>
                                            <div className="flex-1 h-2 rounded-full bg-muted/50 overflow-hidden">
                                                <div
                                                    className={`h-full rounded-full transition-all duration-500 ${colorClass}`}
                                                    style={{ width: `${barWidth}%` }}
                                                />
                                            </div>
                                            <span className="text-[10px] text-muted-foreground w-8 text-right shrink-0">{pct.toFixed(0)}%</span>
                                            <span className="text-[10px] font-medium w-16 text-right shrink-0">{formatBytes(node.totalBytes)}</span>
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    )}

                    {nodeBreakdown.length === 0 && (
                        <div className="flex items-center justify-center flex-1 text-sm text-muted-foreground">
                            No traffic data available
                        </div>
                    )}
                </div>
            )}
        </WidgetWrapper>
    )
}
