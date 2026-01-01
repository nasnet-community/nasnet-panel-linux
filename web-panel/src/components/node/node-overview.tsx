import { useState, useMemo } from "react"
import { motion } from "framer-motion"
import { Card } from "@/components/ui/card"
import { Loader2 } from "lucide-react"
import {
    HiOutlineChip,
    HiOutlineDatabase,
    HiOutlineServer,
    HiOutlineSwitchHorizontal,
    HiOutlineCollection,
} from "react-icons/hi"
import { StatsChart } from "@/components/stats-chart"
import { NetworkChart } from "@/components/network-chart"
import { ConnectionChart } from "./connection-chart"
import { TrafficSummaryChart } from "./traffic-summary-chart"
import { NodeSystemInfo } from "./node-system-info"
import { NodeXrayInfo } from "./node-xray-info"
import { HealthGaugesRow } from "./health-gauges-row"
import { LoadAverageWidget, LoadAverageCompact } from "./load-average-widget"
import { BandwidthUtilization, BandwidthUtilizationCompact } from "./bandwidth-utilization"
import { UptimeTimeline } from "./uptime-timeline"
import { formatBytes } from "@/lib/utils"
import { StarlinkQuickStats } from "./starlink-quick-stats"
import { useChartPalette } from "@/lib/design/palette"
import type { Node, NodeStats, NodeDataPoint } from "@/lib/types"

interface NodeOverviewProps {
    node: Node
    stats?: NodeStats
    history?: NodeDataPoint[]
    isLoadingStats: boolean
    onRefreshStats?: () => void
    onNavigateTab?: (tab: string) => void
}

// Animation variants hoisted outside component to avoid re-creation on every render
const containerVariants = {
    hidden: { opacity: 0 },
    visible: {
        opacity: 1,
        transition: { staggerChildren: 0.06, delayChildren: 0.1 }
    }
} as const

const cardVariants = {
    hidden: { opacity: 0, y: 16, scale: 0.98 },
    visible: {
        opacity: 1,
        y: 0,
        scale: 1,
        transition: { type: "spring" as const, stiffness: 400, damping: 28 }
    }
}

const chartRevealVariants = {
    hidden: { opacity: 0, y: 20 },
    visible: {
        opacity: 1,
        y: 0,
        transition: { type: "spring" as const, stiffness: 300, damping: 30, delay: 0.35 }
    }
}

type ChartTab = "network" | "connections" | "traffic"

export function NodeOverview({ node, stats, history, isLoadingStats, onRefreshStats, onNavigateTab }: NodeOverviewProps) {
    const c = useChartPalette()
    const [chartTab, setChartTab] = useState<ChartTab>("network")

    const chartData = useMemo(() => {
        const base = history ? [...history].reverse() : []
        return base
    }, [history])

    // Build live chart data by appending current stats
    const liveChartData = useMemo(() => {
        if (!stats) return chartData
        return [
            ...chartData,
            {
                id: 0,
                node_id: node.id,
                cpu: stats.cpu_percent,
                memory: stats.memory_percent,
                disk: stats.disk_percent,
                up_speed: stats.up_speed || 0,
                down_speed: stats.down_speed || 0,
                tcp_count: stats.tcp_count || 0,
                udp_count: stats.udp_count || 0,
                fd_count: stats.fd_count || 0,
                load_avg_1: stats.load_avg_1 || 0,
                created_at: new Date().toISOString(),
            }
        ]
    }, [chartData, stats, node.id])

    if (!node.is_online) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground bg-muted/5 rounded-2xl border-2 border-dashed">
                <HiOutlineServer className="w-12 h-12 mb-4 opacity-50" />
                <h3 className="text-lg font-medium text-foreground">No Stats Available</h3>
                <p className="text-sm text-center px-4">Ensure the node is online and the agent is installed to view real-time statistics.</p>
            </div>
        )
    }

    const chartTabs: { key: ChartTab; label: string }[] = [
        { key: "network", label: "Network" },
        { key: "connections", label: "Connections" },
        { key: "traffic", label: "Traffic" },
    ]

    return (
        <motion.div
            className="space-y-3 md:space-y-5"
            initial="hidden"
            animate="visible"
            variants={containerVariants}
        >
            {/* ==================== MOBILE LAYOUT ==================== */}
            <div className="md:hidden space-y-3">
                {/* Health Gauges */}
                <motion.div variants={cardVariants}>
                    <HealthGaugesRow
                        cpu={stats?.cpu_percent}
                        memory={stats?.memory_percent}
                        disk={stats?.disk_percent}
                        chartData={liveChartData}
                        isLoading={isLoadingStats}
                    />
                </motion.div>

                {/* Quick stats 2x2 grid */}
                <motion.div className="grid grid-cols-2 gap-3" variants={containerVariants}>
                    <motion.div variants={cardVariants}>
                        <LoadAverageCompact nodeId={node.id} stats={stats} isLoading={isLoadingStats} />
                    </motion.div>
                    <motion.div variants={cardVariants}>
                        <BandwidthUtilizationCompact node={node} stats={stats} isLoading={isLoadingStats} />
                    </motion.div>

                    {/* Network Load - compact */}
                    <motion.div variants={cardVariants}>
                        <Card className="h-full relative overflow-hidden transition-shadow duration-300 rounded-2xl p-3 bg-card/50 backdrop-blur-sm border-white/5">
                            <div className="flex items-center justify-between mb-2 relative z-10">
                                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.12em]">Network Load</p>
                                <HiOutlineCollection className="w-3.5 h-3.5 text-muted-foreground/30" />
                            </div>
                            <div className="flex flex-col gap-2">
                                <div className="flex justify-between items-center text-xs">
                                    <span className="text-muted-foreground font-medium">TCP</span>
                                    <span className="font-mono font-bold text-foreground">{stats?.tcp_count?.toLocaleString() ?? "—"}</span>
                                </div>
                                <div className="flex justify-between items-center text-xs">
                                    <span className="text-muted-foreground font-medium">UDP</span>
                                    <span className="font-mono font-bold text-foreground">{stats?.udp_count?.toLocaleString() ?? "—"}</span>
                                </div>
                                <div className="flex justify-between items-center text-xs">
                                    <span className="text-muted-foreground font-medium">Users</span>
                                    <span className="font-mono font-bold text-foreground">{stats?.online_users?.toLocaleString() ?? "—"}</span>
                                </div>
                                <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2">
                                    <span className="text-muted-foreground font-medium">Files</span>
                                    <span className="font-mono font-bold text-foreground">{stats?.fd_count?.toLocaleString() ?? "—"}</span>
                                </div>
                            </div>
                        </Card>
                    </motion.div>

                    {/* Total Traffic - compact */}
                    <motion.div variants={cardVariants}>
                        <Card className="h-full relative overflow-hidden transition-shadow duration-300 rounded-2xl p-3 bg-card/50 backdrop-blur-sm border-white/5">
                            <div className="flex items-center justify-between mb-2 relative z-10">
                                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.12em]">Traffic</p>
                                <HiOutlineSwitchHorizontal className="w-3.5 h-3.5 text-muted-foreground/30" />
                            </div>
                            <div className="flex flex-col gap-2">
                                <div className="flex justify-between items-center text-xs">
                                    <span className="text-muted-foreground font-medium">Up</span>
                                    <span className="font-mono font-bold text-foreground text-[11px]">
                                        {stats?.total_uplink ? formatBytes(stats.total_uplink) : "0 B"}
                                    </span>
                                </div>
                                <div className="flex justify-between items-center text-xs">
                                    <span className="text-muted-foreground font-medium">Down</span>
                                    <span className="font-mono font-bold text-foreground text-[11px]">
                                        {stats?.total_downlink ? formatBytes(stats.total_downlink) : "0 B"}
                                    </span>
                                </div>
                                <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2">
                                    <span className="text-muted-foreground font-medium">Total</span>
                                    <span className="font-mono font-bold text-foreground text-[11px]">
                                        {stats ? formatBytes((stats.total_uplink || 0) + (stats.total_downlink || 0)) : "0 B"}
                                    </span>
                                </div>
                            </div>
                        </Card>
                    </motion.div>
                </motion.div>

                {/* Xray Process - full width */}
                <motion.div variants={cardVariants}>
                    <NodeXrayInfo node={node} stats={stats} isLoading={isLoadingStats} onRefresh={onRefreshStats} />
                </motion.div>

                {/* Tabbed Chart - mobile */}
                <motion.div variants={chartRevealVariants}>
                    <Card className="relative overflow-hidden transition-shadow duration-300 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-4">
                            <div className="flex items-center gap-1 bg-muted/30 rounded-lg p-0.5">
                                {chartTabs.map((tab) => (
                                    <button
                                        key={tab.key}
                                        onClick={() => setChartTab(tab.key)}
                                        className={`px-3 py-1.5 rounded-md text-[11px] font-bold transition-all ${
                                            chartTab === tab.key
                                                ? "bg-foreground text-background shadow-sm"
                                                : "text-muted-foreground hover:text-foreground"
                                        }`}
                                    >
                                        {tab.label}
                                    </button>
                                ))}
                            </div>
                        </div>
                        <div className="h-[200px] w-full">
                            {chartTab === "network" && <NetworkChart data={liveChartData} />}
                            {chartTab === "connections" && <ConnectionChart data={liveChartData} />}
                            {chartTab === "traffic" && <TrafficSummaryChart nodeId={node.id} />}
                        </div>
                    </Card>
                </motion.div>

                {/* System Info */}
                <motion.div variants={cardVariants}>
                    <NodeSystemInfo
                        nodeId={node.id}
                        countryCode={node.country_code}
                        datacenter={node.datacenter}
                        ip={node.ip}
                    />
                </motion.div>

                {/* Uptime Timeline */}
                <motion.div variants={cardVariants}>
                    <UptimeTimeline nodeId={node.id} isOnline={node.is_online} />
                </motion.div>
            </div>

            {/* ==================== DESKTOP LAYOUT ==================== */}

            {/* Row 1: Resource cards (4 columns) */}
            <motion.div
                className="hidden md:grid grid-cols-2 lg:grid-cols-4 gap-4"
                variants={containerVariants}
            >
                {/* CPU */}
                <motion.div variants={cardVariants}>
                    <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-blue-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-3 relative z-10">
                            <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Processor</p>
                            <HiOutlineChip className="w-4 h-4 text-muted-foreground/30 group-hover:text-blue-400 transition-colors" />
                        </div>
                        <div className="space-y-4">
                            <div className="flex items-end justify-between">
                                <div className="flex flex-col">
                                    <p className="text-3xl font-bold tracking-tighter">
                                        {stats?.cpu_percent !== undefined ? `${stats.cpu_percent.toFixed(0)}%` : "—"}
                                    </p>
                                    <span className="text-[11px] text-muted-foreground font-medium flex items-center gap-1 mt-0.5">
                                        Max {Math.max(...liveChartData.map(d => d.cpu || 0)).toFixed(0)}%
                                    </span>
                                </div>
                                {isLoadingStats && <Loader2 className="w-4 h-4 animate-spin text-blue-500/50 mb-1" />}
                            </div>
                            <StatsChart data={liveChartData} dataKey="cpu" color={c.info} height={45} />
                        </div>
                    </Card>
                </motion.div>

                {/* Memory */}
                <motion.div variants={cardVariants}>
                    <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-purple-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-3 relative z-10">
                            <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Memory</p>
                            <HiOutlineDatabase className="w-4 h-4 text-muted-foreground/30 group-hover:text-purple-400 transition-colors" />
                        </div>
                        <div className="space-y-4">
                            <div className="flex items-end justify-between">
                                <div className="flex flex-col">
                                    <p className="text-3xl font-bold tracking-tighter">
                                        {stats?.memory_percent !== undefined ? `${stats.memory_percent.toFixed(0)}%` : "—"}
                                    </p>
                                    <span className="text-[11px] text-muted-foreground font-medium flex items-center gap-1 mt-0.5">
                                        Max {Math.max(...liveChartData.map(d => d.memory || 0)).toFixed(0)}%
                                    </span>
                                </div>
                                {isLoadingStats && <Loader2 className="w-4 h-4 animate-spin text-purple-500/50 mb-1" />}
                            </div>
                            <StatsChart data={liveChartData} dataKey="memory" color={c.chart6} height={45} />
                        </div>
                    </Card>
                </motion.div>

                {/* Disk */}
                <motion.div variants={cardVariants}>
                    <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-amber-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-3 relative z-10">
                            <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Storage</p>
                            <HiOutlineServer className="w-4 h-4 text-muted-foreground/30 group-hover:text-amber-400 transition-colors" />
                        </div>
                        <div className="space-y-4">
                            <div className="flex items-end justify-between">
                                <div className="flex flex-col">
                                    <p className="text-3xl font-bold tracking-tighter">
                                        {stats?.disk_percent !== undefined ? `${stats.disk_percent.toFixed(0)}%` : "—"}
                                    </p>
                                    <span className="text-[11px] text-muted-foreground font-medium flex items-center gap-1 mt-0.5">
                                        Max {Math.max(...liveChartData.map(d => d.disk || 0)).toFixed(0)}%
                                    </span>
                                </div>
                                {isLoadingStats && <Loader2 className="w-4 h-4 animate-spin text-amber-500/50 mb-1" />}
                            </div>
                            <StatsChart data={liveChartData} dataKey="disk" color={c.warning} height={45} />
                        </div>
                    </Card>
                </motion.div>

                {/* Xray Process */}
                <motion.div variants={cardVariants}>
                    <NodeXrayInfo node={node} stats={stats} isLoading={isLoadingStats} onRefresh={onRefreshStats} />
                </motion.div>
            </motion.div>

            {/* Row 2: Quick Stats — 4 cols without Starlink, 3 cols (6 cards) with Starlink */}
            <motion.div
                className={`hidden md:grid grid-cols-2 gap-4 ${node.starlink_settings?.enabled ? "lg:grid-cols-3" : "lg:grid-cols-4"}`}
                variants={containerVariants}
            >
                {/* Load Average */}
                <motion.div variants={cardVariants}>
                    <LoadAverageWidget nodeId={node.id} stats={stats} isLoading={isLoadingStats} />
                </motion.div>

                {/* Network Load */}
                <motion.div variants={cardVariants}>
                    <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-emerald-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-3 relative z-10">
                            <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Network Load</p>
                            <HiOutlineCollection className="w-4 h-4 text-muted-foreground/30 group-hover:text-emerald-400 transition-colors" />
                        </div>
                        <div className="flex flex-col gap-2.5 mt-2">
                            <div className="flex justify-between items-center text-xs">
                                <span className="text-muted-foreground font-medium">TCP Conns</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">{stats?.tcp_count?.toLocaleString() ?? "—"}</span>
                            </div>
                            <div className="flex justify-between items-center text-xs">
                                <span className="text-muted-foreground font-medium">UDP Conns</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">{stats?.udp_count?.toLocaleString() ?? "—"}</span>
                            </div>
                            <div className="flex justify-between items-center text-xs">
                                <span className="text-muted-foreground font-medium">Online Users</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">{stats?.online_users?.toLocaleString() ?? "—"}</span>
                            </div>
                            <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2.5 mt-0.5">
                                <span className="text-muted-foreground font-medium">Open Files</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">{stats?.fd_count?.toLocaleString() ?? "—"}</span>
                            </div>
                        </div>
                        {isLoadingStats && (
                            <div className="absolute top-4 right-4">
                                <Loader2 className="w-4 h-4 animate-spin text-emerald-500/50" />
                            </div>
                        )}
                    </Card>
                </motion.div>

                {/* Total Traffic */}
                <motion.div variants={cardVariants}>
                    <Card className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-cyan-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5">
                        <div className="flex items-center justify-between mb-3 relative z-10">
                            <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Total Traffic</p>
                            <HiOutlineSwitchHorizontal className="w-4 h-4 text-muted-foreground/30 group-hover:text-cyan-400 transition-colors" />
                        </div>
                        <div className="flex flex-col gap-2.5 mt-2">
                            <div className="flex justify-between items-center text-xs">
                                <span className="text-muted-foreground font-medium">Upload</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">
                                    {stats?.total_uplink ? formatBytes(stats.total_uplink) : "0 B"}
                                </span>
                            </div>
                            <div className="flex justify-between items-center text-xs">
                                <span className="text-muted-foreground font-medium">Download</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">
                                    {stats?.total_downlink ? formatBytes(stats.total_downlink) : "0 B"}
                                </span>
                            </div>
                            <div className="flex justify-between items-center text-xs border-t border-white/5 pt-2.5 mt-0.5">
                                <span className="text-muted-foreground font-medium">Total</span>
                                <span className="font-mono font-bold text-foreground text-[13px]">
                                    {stats ? formatBytes((stats.total_uplink || 0) + (stats.total_downlink || 0)) : "0 B"}
                                </span>
                            </div>
                        </div>
                        {isLoadingStats && (
                            <div className="absolute top-4 right-4">
                                <Loader2 className="w-4 h-4 animate-spin text-cyan-500/50" />
                            </div>
                        )}
                    </Card>
                </motion.div>

                {/* Bandwidth Utilization */}
                <motion.div variants={cardVariants}>
                    <BandwidthUtilization node={node} stats={stats} isLoading={isLoadingStats} />
                </motion.div>

                {/* Starlink Signal + Throughput (conditional, 2 cards) */}
                {node.starlink_settings?.enabled && (
                    <>
                        <motion.div variants={cardVariants}>
                            <StarlinkQuickStats
                                nodeId={node.id}
                                isOnline={node.is_online}
                                variant="signal"
                                onNavigate={() => onNavigateTab?.("starlink")}
                            />
                        </motion.div>
                        <motion.div variants={cardVariants}>
                            <StarlinkQuickStats
                                nodeId={node.id}
                                isOnline={node.is_online}
                                variant="throughput"
                                onNavigate={() => onNavigateTab?.("starlink")}
                            />
                        </motion.div>
                    </>
                )}
            </motion.div>

            {/* Row 3: Tabbed Charts + System Info */}
            <motion.div
                className="hidden md:grid grid-cols-1 lg:grid-cols-3 gap-4 md:gap-5"
                variants={chartRevealVariants}
            >
                {/* Tabbed Chart Area */}
                <Card className="lg:col-span-2 relative overflow-hidden transition-shadow duration-300 hover:shadow-lg hover:shadow-blue-500/10 rounded-2xl p-4 md:p-6 bg-card/50 backdrop-blur-sm border-white/5">
                    <div className="flex items-center justify-between mb-4 md:mb-6 relative z-10">
                        <div className="flex items-center gap-1.5 bg-muted/30 rounded-lg p-0.5">
                            {chartTabs.map((tab) => (
                                <button
                                    key={tab.key}
                                    onClick={() => setChartTab(tab.key)}
                                    className={`px-3 py-1.5 rounded-md text-[11px] font-bold transition-all ${
                                        chartTab === tab.key
                                            ? "bg-foreground text-background shadow-sm"
                                            : "text-muted-foreground hover:text-foreground"
                                    }`}
                                >
                                    {tab.label}
                                </button>
                            ))}
                        </div>

                        {/* Legend for network/connections tabs */}
                        {chartTab === "network" && (
                            <div className="flex items-center gap-4">
                                <div className="flex items-center gap-2">
                                    <div className="w-2 h-2 rounded-full bg-blue-500" />
                                    <span className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-wider">Upload</span>
                                </div>
                                <div className="flex items-center gap-2">
                                    <div className="w-2 h-2 rounded-full bg-emerald-500" />
                                    <span className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-wider">Download</span>
                                </div>
                            </div>
                        )}
                        {chartTab === "connections" && (
                            <div className="flex items-center gap-3">
                                <div className="flex items-center gap-1.5">
                                    <div className="w-2 h-2 rounded-full bg-blue-500" />
                                    <span className="text-[10px] uppercase font-bold text-muted-foreground/70 tracking-wider">TCP</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <div className="w-2 h-2 rounded-full bg-purple-500" />
                                    <span className="text-[10px] uppercase font-bold text-muted-foreground/70 tracking-wider">UDP</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <div className="w-2 h-2 rounded-full bg-amber-500" />
                                    <span className="text-[10px] uppercase font-bold text-muted-foreground/70 tracking-wider">FD</span>
                                </div>
                            </div>
                        )}
                    </div>
                    <div className={chartTab === "traffic" ? "h-[320px] w-full" : "h-[280px] w-full"}>
                        {chartTab === "network" && <NetworkChart data={liveChartData} />}
                        {chartTab === "connections" && <ConnectionChart data={liveChartData} />}
                        {chartTab === "traffic" && <TrafficSummaryChart nodeId={node.id} />}
                    </div>
                </Card>

                {/* System Info */}
                <div className="lg:col-span-1">
                    <NodeSystemInfo
                        nodeId={node.id}
                        countryCode={node.country_code}
                        datacenter={node.datacenter}
                        ip={node.ip}
                    />
                </div>
            </motion.div>

            {/* Row 4: Uptime Timeline (full width) */}
            <motion.div
                className="hidden md:block"
                variants={cardVariants}
            >
                <UptimeTimeline nodeId={node.id} isOnline={node.is_online} />
            </motion.div>
        </motion.div>
    )
}
