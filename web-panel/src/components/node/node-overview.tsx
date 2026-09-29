import { isLocalNode } from "@/lib/node-kind"
import { useEffect, useMemo, useState } from "react"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useNodeHostInfo, useNodeOverview, useNodeStatsSeries } from "@/lib/queries"
import type { Node, NodeStatBucket, NodeStats } from "@/lib/types"
import { chart } from "@/lib/design/palette"
import { cn } from "@/lib/utils"
import { T } from "./node-ui"
import { DisconnectionHistory } from "./disconnection-history"
import { HealthGaugesRow } from "./health-gauges-row"
import { NodeSystemInfo } from "./node-system-info"
import { NodeXrayInfo } from "./node-xray-info"
import { StarlinkQuickStats } from "./starlink-quick-stats"
import { UptimeTimeline } from "./uptime-timeline"
import { OverviewCharts } from "./overview/overview-charts"
import { HOST_COLORS, isLiveReading, lastReadingLabel, usageTone } from "./overview/overview-helpers"
import { BandwidthCard, LoadAverageCard, NetworkLoadCard, TotalTrafficCard } from "./overview/overview-stat-cards"
import { ResourceCard } from "./overview/resource-card"
import type { SparkPoint } from "./overview/area-sparkline"

interface NodeOverviewProps {
    node: Node
    stats?: NodeStats
    isLoadingStats: boolean
    onNavigateTab?: (tab: string) => void
}

/** Re-render on an interval so "live" turns into "last reading" on its own. */
function useNow(intervalMs: number) {
    const [now, setNow] = useState(() => Date.now())
    useEffect(() => {
        const id = setInterval(() => setNow(Date.now()), intervalMs)
        return () => clearInterval(id)
    }, [intervalMs])
    return now
}

/** The last hour laid on its minute grid, so a missing minute is a gap. */
function hourValues(points: NodeStatBucket[], from: number, to: number, step: number, key: keyof NodeStatBucket): SparkPoint[] {
    const byT = new Map(points.map(p => [p.t, p]))
    const out: SparkPoint[] = []
    for (let t = Math.floor(from / step) * step; t <= to; t += step) {
        const p = byT.get(t)
        out.push({ t: t * 1000, v: p ? Number(p[key]) : null })
    }
    return out
}

const gb = (mb: number) => (mb / 1024).toFixed(1)

export function NodeOverview({ node, stats, isLoadingStats, onNavigateTab }: NodeOverviewProps) {
    const isMobile = useIsMobile()
    const now = useNow(5_000)
    const live = node.is_online && isLiveReading(stats, now)
    const stale = live ? null : stats ? lastReadingLabel(stats) : null

    const { data: overview } = useNodeOverview(node.id)
    const { data: hostInfo } = useNodeHostInfo(node.id)
    const { data: hour } = useNodeStatsSeries(node.id, "1h")
    const cores = hostInfo?.cpu_cores

    const points = hour?.points ?? []
    const spark = useMemo(() => {
        if (!hour) return { cpu: [], mem: [], disk: [] }
        const v = (k: keyof NodeStatBucket) => hourValues(hour.points, hour.from, hour.to, hour.step_sec, k)
        return { cpu: v("cpu_avg"), mem: v("mem_avg"), disk: v("disk_avg") }
    }, [hour])
    const peak = (k: keyof NodeStatBucket, current?: number) =>
        Math.max(current ?? 0, ...points.map(p => Number(p[k]) || 0))

    const starlink = !!node.starlink_settings?.enabled
    const openStarlink = () => onNavigateTab?.("starlink")

    const cpuSub = points.length || stats ? `Peak ${peak("cpu_max", stats?.cpu_percent).toFixed(0)}% in the last hour` : ""
    const memSub = stats?.memory_total_mb
        ? `${gb(stats.memory_used_mb)} GB used${points.length ? `, peak ${peak("mem_max", stats.memory_percent).toFixed(0)}%` : ""}`
        : ""
    const diskSub = stats?.disk_total_gb
        ? `${stats.disk_used_gb} GB used, ${Math.max(stats.disk_total_gb - stats.disk_used_gb, 0)} GB free`
        : ""

    if (isMobile) {
        return (
            <div className="space-y-3">
                <HealthGaugesRow stats={stats} points={points} live={live} isLoading={isLoadingStats} />
                <div className="grid grid-cols-2 gap-3">
                    <LoadAverageCard compact stats={stats} live={live} cores={cores} />
                    <BandwidthCard compact node={node} stats={stats} live={live} />
                    <NetworkLoadCard compact stats={stats} live={live} />
                    <TotalTrafficCard compact stats={stats} today={overview?.today} />
                    {starlink && (
                        <>
                            <StarlinkQuickStats compact nodeId={node.id} isOnline={node.is_online} variant="signal" onNavigate={openStarlink} />
                            <StarlinkQuickStats compact nodeId={node.id} isOnline={node.is_online} variant="throughput" onNavigate={openStarlink} />
                        </>
                    )}
                </div>
                <NodeXrayInfo compact node={node} stats={stats} configState={overview?.config.state} isLoading={isLoadingStats} />
                <OverviewCharts compact node={node} stats={stats} live={live} />
                <NodeSystemInfo compact node={node} overview={overview} />
                <UptimeTimeline nodeId={node.id} isOnline={node.is_online} local={isLocalNode(node)} createdAt={node.created_at} />
                <DisconnectionHistory nodeId={node.id} local={isLocalNode(node)} />
            </div>
        )
    }

    return (
        <div className="@container space-y-4">

            {/* Row 1: resources + Xray. Four across only once the content column
                itself is wide enough; with both sidebars open a 1280px window
                leaves ~680px, too narrow for four cards. */}
            <div className="grid grid-cols-2 gap-4 @4xl:grid-cols-4">
                <ResourceCard
                    title="Processor"
                    sparkLabel="CPU"
                    aside={cores ? <span className={T.meta}>{cores} cores</span> : undefined}
                    value={stats?.cpu_percent}
                    subline={cpuSub}
                    data={spark.cpu}
                    color={HOST_COLORS.cpu}
                    glow="blue"
                    tone={usageTone(stats?.cpu_percent, 85, 95)}
                    stale={stale}
                />
                <ResourceCard
                    title="Memory"
                    aside={stats?.memory_total_mb ? <span className={cn(T.meta, "tabular-nums")}>{gb(stats.memory_total_mb)} GB</span> : undefined}
                    value={stats?.memory_percent}
                    subline={memSub}
                    data={spark.mem}
                    color={HOST_COLORS.memory}
                    glow="purple"
                    tone={usageTone(stats?.memory_percent, 90, 97)}
                    stale={stale}
                />
                <ResourceCard
                    title="Storage"
                    aside={stats?.disk_total_gb ? <span className={cn(T.meta, "tabular-nums")}>{stats.disk_total_gb} GB</span> : undefined}
                    value={stats?.disk_percent}
                    subline={diskSub}
                    data={spark.disk}
                    color={HOST_COLORS.disk}
                    glow="teal"
                    tone={usageTone(stats?.disk_percent, 85, 95)}
                    stale={stale}
                    threshold={{ value: 85, color: chart.amber }}
                />
                <NodeXrayInfo node={node} stats={stats} configState={overview?.config.state} isLoading={isLoadingStats} />
            </div>

            {/* Row 2: quick stats, plus the two Starlink cards when the dish is monitored */}
            <div className={cn("grid grid-cols-2 gap-4", starlink ? "lg:grid-cols-3" : "lg:grid-cols-4")}>
                <LoadAverageCard stats={stats} live={live} cores={cores} />
                <NetworkLoadCard stats={stats} live={live} />
                <TotalTrafficCard stats={stats} today={overview?.today} />
                <BandwidthCard node={node} stats={stats} live={live} peakBps={overview?.today.peak_bps} />
                {starlink && (
                    <>
                        <StarlinkQuickStats nodeId={node.id} isOnline={node.is_online} variant="signal" onNavigate={openStarlink} />
                        <StarlinkQuickStats nodeId={node.id} isOnline={node.is_online} variant="throughput" onNavigate={openStarlink} />
                    </>
                )}
            </div>

            {/* Row 3: charts + system specifications */}
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
                <OverviewCharts node={node} stats={stats} live={live} className="lg:col-span-2" />
                <NodeSystemInfo node={node} overview={overview} />
            </div>

            <UptimeTimeline nodeId={node.id} isOnline={node.is_online} local={isLocalNode(node)} createdAt={node.created_at} />
            <DisconnectionHistory nodeId={node.id} local={isLocalNode(node)} />
        </div>
    )
}
