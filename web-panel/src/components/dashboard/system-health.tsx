import { WidgetWrapper } from "./widget-wrapper"
import { MetricBar } from "@/components/ui/metric-bar"
import { Skeleton } from "@/components/ui/skeleton"
import { cn, formatBytes } from "@/lib/utils"
import { Server } from "lucide-react"
import { useNodeAggregateStats } from "@/lib/queries"
import { Link } from "react-router"

interface SystemHealthProps {
    isEditMode?: boolean
}

export function SystemHealth({ isEditMode }: SystemHealthProps) {
    const { data, isLoading } = useNodeAggregateStats()

    return (
        <WidgetWrapper
            title="System Health"
            icon={<Server className="w-4 h-4 text-blue-500" />}
            isEditMode={isEditMode}
            headerRight={
                data && (
                    <span className="text-[10px] text-muted-foreground">
                        {data.nodes.filter((n) => n.isOnline).length}/{data.nodes.length} online
                    </span>
                )
            }
            noPadding
        >
            {isLoading ? (
                <div className="p-4 space-y-3">
                    {[1, 2, 3].map((i) => (
                        <Skeleton key={i} className="h-16 w-full" />
                    ))}
                </div>
            ) : !data?.nodes?.length ? (
                <div className="flex items-center justify-center h-full min-h-[200px] text-sm text-muted-foreground">
                    No nodes configured
                </div>
            ) : (
                <div className="divide-y divide-border/50">
                    {data.nodes.map((node) => (
                        <Link
                            key={node.id}
                            to={`/nodes/${node.id}`}
                            className="block px-4 py-3 hover:bg-muted/30 transition-colors"
                        >
                            <div className="flex items-center justify-between mb-2">
                                <div className="flex items-center gap-2 min-w-0">
                                    <div className={cn(
                                        "w-2 h-2 rounded-full shrink-0",
                                        node.isOnline ? "bg-emerald-500" : "bg-red-500"
                                    )} />
                                    <span className="text-sm font-medium truncate">{node.name}</span>
                                    {node.countryCode && (
                                        <span className="text-[9px] text-muted-foreground bg-muted px-1 rounded">
                                            {node.countryCode}
                                        </span>
                                    )}
                                </div>
                                <div className="flex items-center gap-2 text-[10px] text-muted-foreground shrink-0">
                                    {node.isOnline && (
                                        <>
                                            <span>&uarr;{formatBytes(node.totalUplink)}</span>
                                            <span>&darr;{formatBytes(node.totalDownlink)}</span>
                                        </>
                                    )}
                                </div>
                            </div>
                            {node.isOnline ? (
                                <div className="grid grid-cols-3 gap-3">
                                    <MetricBar value={node.cpuPercent} label="CPU" size="sm" />
                                    <MetricBar value={node.memoryPercent} label="RAM" size="sm" />
                                    <MetricBar value={node.diskPercent} label="Disk" size="sm" />
                                </div>
                            ) : (
                                <p className="text-[10px] text-muted-foreground">
                                    {node.isOnline ? "No agent stats" : "Offline"}
                                </p>
                            )}
                        </Link>
                    ))}
                </div>
            )}
        </WidgetWrapper>
    )
}
