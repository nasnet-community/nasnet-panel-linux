import { LocalNodeBadge } from "@/components/node/local-node-badge"
import { isLiteNode, isLocalNode, nodeAddress } from "@/lib/node-kind"
import { MtprotoSheetSummary } from "@/components/mtproto/mtproto-node-summary"
import { useChartPalette } from "@/lib/design/palette"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { MetricBar } from "@/components/ui/metric-bar"
import { Sparkline } from "@/components/ui/sparkline"
import { Skeleton } from "@/components/ui/skeleton"
import { cn, formatBytes, formatSpeed, formatUptime } from "@/lib/utils"
import { Activity, Clock, Network, TrendingUp, TrendingDown, Users, ExternalLink } from "lucide-react"
import { HiOutlineTerminal } from "react-icons/hi"
import type { Node } from "@/lib/types"
import { useNodeStats, useNodeStatsHistory, useNodeHostInfo } from "@/lib/queries"
import { Link } from "react-router"

function StatItem({ icon: Icon, label, value, color }: { icon: any; label: string; value: string; color?: string }) {
    return (
        <div className="flex items-center gap-2">
            <Icon className={cn("w-3.5 h-3.5", color || "text-muted-foreground")} />
            <div>
                <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-medium">{label}</p>
                <p className="text-sm font-bold">{value}</p>
            </div>
        </div>
    )
}

interface NodeDetailSheetProps {
    node: Node | null
    open: boolean
    onOpenChange: (open: boolean) => void
    onDeploy?: () => void
}

export function NodeDetailSheet({ node, open, onOpenChange, onDeploy }: NodeDetailSheetProps) {
    // an MTProto node has no stats sweep or host info to ask for
    const agentNode = !!node?.is_online && !isLiteNode(node)
    const { data: stats, isLoading: statsLoading } = useNodeStats(node?.id ?? 0, agentNode)
    const { data: history } = useNodeStatsHistory(node?.id ?? 0, 30, agentNode)
    const { data: hostInfo } = useNodeHostInfo(node?.id ?? 0, agentNode)

    const palette = useChartPalette()
    const cpuHistory = (history || []).map((p) => p.cpu)
    const memHistory = (history || []).map((p) => p.memory)
    const diskHistory = (history || []).map((p) => p.disk)

    if (!node) return null

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent className="w-full sm:max-w-lg overflow-y-auto">
                <SheetHeader>
                    <div className="flex items-center gap-2">
                        <SheetTitle className="text-lg">{node.name}</SheetTitle>
                        <Badge
                            variant={node.is_online ? "success" : "danger"}
                            className="text-[10px] px-1.5 py-0"
                        >
                            {node.is_online ? "Online" : "Offline"}
                        </Badge>
                        {isLocalNode(node) && <LocalNodeBadge />}
                        {isLiteNode(node) && <Badge variant="outline" className="px-1.5 py-0 text-[10px] uppercase tracking-wider">MTProto</Badge>}
                    </div>
                    <SheetDescription className={cn("text-xs", !isLocalNode(node) && "font-mono")}>{nodeAddress(node)}</SheetDescription>
                </SheetHeader>

                <div className="space-y-6 px-4 pb-4">
                    {isLiteNode(node) && node.is_online && <MtprotoSheetSummary node={node} />}
                    {!isLiteNode(node) && node.is_online && (stats?.stats_unavailable || (!stats && !statsLoading)) && (
                        <p role="status" className="text-xs text-amber-500">Stats unavailable · {stats ? "showing last readings" : "retrying automatically"}</p>
                    )}
                    {/* Quick Stats */}
                    {isLiteNode(node) ? null : node.is_online && stats ? (
                        <>
                            <div className="grid grid-cols-2 gap-4">
                                <StatItem icon={Users} label="Accounts" value={`${stats.active_accounts ?? 0} / ${stats.total_accounts ?? 0}`} color="text-blue-500" />
                                <StatItem icon={Clock} label="Uptime" value={stats.process_uptime ? formatUptime(stats.process_uptime) : "—"} color="text-emerald-500" />
                                <StatItem icon={TrendingUp} label="Upload" value={formatSpeed(stats.up_speed ?? 0)} color="text-purple-500" />
                                <StatItem icon={TrendingDown} label="Download" value={formatSpeed(stats.down_speed ?? 0)} color="text-orange-500" />
                            </div>

                            {/* Gauges */}
                            <div className="space-y-4">
                                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Resource Usage</h4>
                                <div className="space-y-3">
                                    <div className="flex items-center gap-3">
                                        <div className="flex-1 min-w-0">
                                            <MetricBar value={stats.cpu_percent ?? 0} label="CPU" size="md" />
                                        </div>
                                        <div className="w-16 shrink-0 overflow-hidden">
                                            <Sparkline data={cpuHistory} color={palette.chart1} height={24} />
                                        </div>
                                    </div>
                                    <div className="flex items-center gap-3">
                                        <div className="flex-1 min-w-0">
                                            <MetricBar value={stats.memory_percent ?? 0} label="RAM" size="md" />
                                        </div>
                                        <div className="w-16 shrink-0 overflow-hidden">
                                            <Sparkline data={memHistory} color={palette.chart2} height={24} />
                                        </div>
                                    </div>
                                    <div className="flex items-center gap-3">
                                        <div className="flex-1 min-w-0">
                                            <MetricBar value={stats.disk_percent ?? 0} label="Disk" size="md" />
                                        </div>
                                        <div className="w-16 shrink-0 overflow-hidden">
                                            <Sparkline data={diskHistory} color={palette.chart3} height={24} />
                                        </div>
                                    </div>
                                </div>
                            </div>

                            {/* Network */}
                            {(stats.tcp_count !== undefined || stats.total_uplink !== undefined) && (
                                <div className="space-y-3">
                                    <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Network</h4>
                                    <div className="grid grid-cols-2 gap-4">
                                        {stats.tcp_count !== undefined && (
                                            <StatItem icon={Network} label="Connections" value={`TCP: ${stats.tcp_count}${stats.udp_count !== undefined ? ` UDP: ${stats.udp_count}` : ""}`} />
                                        )}
                                        {stats.total_uplink !== undefined && (
                                            <StatItem icon={Activity} label="Total Traffic" value={`↑${formatBytes(stats.total_uplink)} ↓${formatBytes(stats.total_downlink)}`} />
                                        )}
                                    </div>
                                </div>
                            )}

                            {/* Host Info */}
                            {hostInfo && (
                                <div className="space-y-3">
                                    <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">System</h4>
                                    <div className="grid grid-cols-2 gap-3 text-xs">
                                        <div>
                                            <p className="text-muted-foreground">OS</p>
                                            <p className="font-medium">{hostInfo.os} {hostInfo.arch}</p>
                                        </div>
                                        <div>
                                            <p className="text-muted-foreground">Kernel</p>
                                            <p className="font-medium truncate">{hostInfo.kernel_version}</p>
                                        </div>
                                        <div>
                                            <p className="text-muted-foreground">CPU</p>
                                            <p className="font-medium truncate">{hostInfo.cpu_model_name} ({hostInfo.cpu_cores} cores)</p>
                                        </div>
                                        <div>
                                            <p className="text-muted-foreground">Memory</p>
                                            <p className="font-medium">{formatBytes(hostInfo.total_memory || 0)}</p>
                                        </div>
                                    </div>
                                </div>
                            )}

                            {/* Versions */}
                            {stats.agent_version && (
                                <div className="space-y-3">
                                    <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Versions</h4>
                                    <div className="grid grid-cols-2 gap-3 text-xs">
                                        <div>
                                            <p className="text-muted-foreground">{isLocalNode(node) ? "Panel" : "Agent"}</p>
                                            <p className="font-medium">{isLocalNode(node) ? `v${stats.agent_version.replace(/^v/, "")}` : stats.agent_version}</p>
                                        </div>
                                        {stats.xray_version && (
                                            <div>
                                                <p className="text-muted-foreground">Xray</p>
                                                <p className="font-medium">{stats.xray_version}</p>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}
                        </>
                    ) : node.is_online && statsLoading ? (
                        <div className="space-y-4">
                            {[1, 2, 3, 4].map((i) => (
                                <Skeleton key={i} className="h-12 w-full" />
                            ))}
                        </div>
                    ) : null}

                    {/* Metadata */}
                    <div className="space-y-3">
                        <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Info</h4>
                        <div className="grid grid-cols-2 gap-3 text-xs">
                            <div>
                                <p className="text-muted-foreground">Location</p>
                                <p className="font-medium">{node.country_code || "Unknown"}</p>
                            </div>
                            <div>
                                <p className="text-muted-foreground">Datacenter</p>
                                <p className="font-medium">{node.datacenter || "—"}</p>
                            </div>
                            <div>
                                <p className="text-muted-foreground">Last Check</p>
                                <p className="font-medium">{new Date(node.last_check).toLocaleString()}</p>
                            </div>
                        </div>
                    </div>

                    {/* Actions */}
                    <div className="flex gap-2 pt-2 border-t">
                        <Button asChild variant="default" className="flex-1">
                            <Link to={`/nodes/${node.id}`}>
                                <ExternalLink className="w-4 h-4 mr-2" />
                                Open Full View
                            </Link>
                        </Button>
                        {!node.is_online && onDeploy && !isLocalNode(node) && (
                            <Button variant="outline" onClick={onDeploy}>
                                <HiOutlineTerminal className="w-4 h-4 mr-2" />
                                Deploy
                            </Button>
                        )}
                    </div>
                </div>
            </SheetContent>
        </Sheet>
    )
}
