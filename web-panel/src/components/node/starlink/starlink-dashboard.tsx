import { useMemo, useState } from "react"
import { motion, useReducedMotion } from "framer-motion"
import { Activity, Shield, ArrowDown, ArrowUp, RefreshCw } from "lucide-react"
import { WifiOff, Satellite, AlertCircle } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { useQueryClient } from "@tanstack/react-query"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useStarlinkStatus, useStarlinkObstructionMap, useStarlinkHistory } from "@/lib/queries/use-starlink"
import { queryKeys } from "@/lib/queries/keys"

import { StarlinkStatusHeader } from "./starlink-status-header"
import { StarlinkMetricCard } from "./starlink-metric-card"
import { StarlinkSignalCard } from "./starlink-signal-card"
import { StarlinkObstructionCard } from "./starlink-obstruction-card"
import { StarlinkCharts } from "./starlink-charts"
import { StarlinkDetailDrawer } from "./starlink-detail-drawer"
import { StarlinkSignalDetail } from "./starlink-signal-detail"
import { StarlinkObstructionDetail } from "./starlink-obstruction-detail"
import { StarlinkMetricDetail } from "./starlink-metric-detail"
import { StarlinkAlerts } from "./starlink-alerts"
import { StarlinkAlignment } from "./starlink-alignment"
import {
    type DrawerType, type TimeRange,
    formatMbps, latencyColor, dropRateColor,
    TIME_RANGE_CONFIG,
} from "./starlink-helpers"

interface StarlinkDashboardProps {
    nodeId: number
    isOnline: boolean
}

const containerVariants = {
    hidden: { opacity: 0 },
    visible: { opacity: 1, transition: { staggerChildren: 0.06, delayChildren: 0.1 } },
} as const

const cardVariants = {
    hidden: { opacity: 0, y: 16, scale: 0.98 },
    visible: { opacity: 1, y: 0, scale: 1, transition: { type: "spring" as const, stiffness: 400, damping: 28 } },
}

const noMotionVariants = {
    hidden: { opacity: 1 },
    visible: { opacity: 1 },
} as const

export function StarlinkDashboard({ nodeId, isOnline }: StarlinkDashboardProps) {
    const [activeDrawer, setActiveDrawer] = useState<DrawerType>(null)
    const [timeRange, setTimeRange] = useState<TimeRange>("1h")
    const isMobile = useIsMobile()
    const reduceMotion = useReducedMotion()
    const containerVar = reduceMotion ? noMotionVariants : containerVariants
    const cardVar = reduceMotion ? noMotionVariants : cardVariants

    const config = TIME_RANGE_CONFIG[timeRange]
    const queryClient = useQueryClient()

    const { data: status, isLoading: statusLoading, isError: statusError, error: statusErr, refetch: refetchStatus, isFetching: statusFetching, dataUpdatedAt: statusUpdatedAt } = useStarlinkStatus(nodeId, isOnline)
    const { data: obstructionMap, isLoading: mapLoading, isError: mapError } = useStarlinkObstructionMap(nodeId, isOnline)
    const { data: history, isLoading: historyLoading } = useStarlinkHistory(nodeId, isOnline, timeRange, config.limit, config.refetchInterval)

    // Backend returns oldest→newest already; chart left-to-right matches.
    const historyAsc = history ?? []
    const latencyData = useMemo(() => historyAsc.map(d => ({ value: d.pop_ping_latency_ms })), [historyAsc])
    const dropData = useMemo(() => historyAsc.map(d => ({ value: d.pop_ping_drop_rate * 100 })), [historyAsc])
    const dlData = useMemo(() => historyAsc.map(d => ({ value: d.downlink_throughput_bps / 1e6 })), [historyAsc])
    const ulData = useMemo(() => historyAsc.map(d => ({ value: d.uplink_throughput_bps / 1e6 })), [historyAsc])

    const peakLatency = useMemo(() => latencyData.reduce((m, d) => Math.max(m, d.value), 0), [latencyData])
    const avgDrop = useMemo(() => dropData.length > 0 ? dropData.reduce((a, d) => a + d.value, 0) / dropData.length : 0, [dropData])
    const peakDl = useMemo(() => dlData.reduce((m, d) => Math.max(m, d.value), 0), [dlData])
    const peakUl = useMemo(() => ulData.reduce((m, d) => Math.max(m, d.value), 0), [ulData])

    // Drawer title
    const drawerTitles: Record<Exclude<DrawerType, null>, string> = {
        signal: "Signal & Dish Health", obstruction: "Obstruction Map",
        latency: "Latency Detail", dropRate: "Packet Loss Detail",
        download: "Download Detail", upload: "Upload Detail", alerts: "Alerts & Outages",
    }

    // ─── Loading / Error States ─────────────────────────────────────
    if (!isOnline) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground bg-muted/5 rounded-2xl border-2 border-dashed">
                <WifiOff className="w-12 h-12 mb-4 opacity-50" />
                <h3 className="text-lg font-medium text-foreground">Node Offline</h3>
                <p className="text-sm text-center px-4">The node must be online to fetch Starlink metrics.</p>
            </div>
        )
    }

    if (statusLoading && !status) {
        return (
            <div className="space-y-4">
                <Skeleton className="h-11 w-full rounded-xl" />
                <div className={`grid ${isMobile ? "grid-cols-2" : "grid-cols-4"} gap-3 md:gap-4`}>
                    {[...Array(4)].map((_, i) => <Skeleton key={i} className="h-[170px] rounded-2xl" />)}
                </div>
                <div className={`grid ${isMobile ? "grid-cols-1" : "grid-cols-2"} gap-3 md:gap-4`}>
                    <Skeleton className="h-[140px] rounded-2xl" />
                    <Skeleton className="h-[140px] rounded-2xl" />
                </div>
                <Skeleton className="h-[340px] rounded-2xl" />
            </div>
        )
    }

    if (statusError || (!statusLoading && !status)) {
        const message = statusErr instanceof Error ? statusErr.message : "Failed to load Starlink status"
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground bg-red-500/5 rounded-2xl border-2 border-dashed border-red-500/20">
                <AlertCircle className="w-12 h-12 mb-4 opacity-50 text-red-400" />
                <h3 className="text-lg font-medium text-red-400">Failed to Load</h3>
                <p className="text-sm text-center px-4 max-w-md mt-2 mb-4">{message}</p>
                <button
                    onClick={() => {
                        refetchStatus()
                        queryClient.invalidateQueries({ queryKey: queryKeys.starlinkMap(nodeId) })
                        queryClient.invalidateQueries({ queryKey: queryKeys.starlinkHistory(nodeId, timeRange) })
                    }}
                    disabled={statusFetching}
                    className="flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-semibold bg-red-500/10 text-red-400 border border-red-500/20 hover:bg-red-500/20 transition-colors disabled:opacity-50"
                >
                    <RefreshCw className={`w-3.5 h-3.5 ${statusFetching ? "animate-spin" : ""}`} />
                    Retry
                </button>
            </div>
        )
    }

    if (status && !status.available) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground bg-red-500/5 rounded-2xl border-2 border-dashed border-red-500/20">
                <Satellite className="w-12 h-12 mb-4 opacity-50 text-red-400" />
                <h3 className="text-lg font-medium text-red-400">Dish Unreachable</h3>
                <p className="text-sm text-center px-4 max-w-md mt-2">
                    Verify the dish address in node settings and ensure the dish is powered on.
                </p>
            </div>
        )
    }

    if (!status) return null

    return (
        <>
            <motion.div className="space-y-4" initial="hidden" animate="visible" variants={containerVar}>
                {/* Status Header */}
                <motion.div variants={cardVar}>
                    <StarlinkStatusHeader status={status} onAlertsClick={() => setActiveDrawer("alerts")} dataUpdatedAt={statusUpdatedAt} />
                </motion.div>

                {/* Hero Metric Cards */}
                <motion.div className={`grid ${isMobile ? "grid-cols-2" : "grid-cols-4"} gap-3 md:gap-4`} variants={containerVar}>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Latency" value={status.pop_ping_latency_ms.toFixed(0)} unit="ms" subtitle={`Peak ${peakLatency.toFixed(0)}ms`} valueColor={latencyColor(status.pop_ping_latency_ms)} sparklineData={latencyData} sparklineColor="#f59e0b" sparklineId="latency" icon={Activity} hoverShadow="hover:shadow-amber-500/10" onClick={() => setActiveDrawer("latency")} formatHoverValue={(v) => v.toFixed(0)} threshold={{ warn: 50, crit: 80, direction: "above" }} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Drop Rate" value={(status.pop_ping_drop_rate * 100).toFixed(1)} unit="%" subtitle={`Avg ${avgDrop.toFixed(1)}%`} valueColor={dropRateColor(status.pop_ping_drop_rate)} sparklineData={dropData} sparklineColor="#ef4444" sparklineId="drop" icon={Shield} hoverShadow="hover:shadow-red-500/10" onClick={() => setActiveDrawer("dropRate")} formatHoverValue={(v) => v.toFixed(1)} threshold={{ warn: 1, crit: 5, direction: "above" }} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Download" value={formatMbps(status.downlink_throughput_bps)} unit="Mbps" subtitle={`Peak ${peakDl.toFixed(1)} Mbps`} valueColor="text-emerald-400" sparklineData={dlData} sparklineColor="#10b981" sparklineId="dl" icon={ArrowDown} hoverShadow="hover:shadow-emerald-500/10" onClick={() => setActiveDrawer("download")} formatHoverValue={(v) => v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Upload" value={formatMbps(status.uplink_throughput_bps)} unit="Mbps" subtitle={`Peak ${peakUl.toFixed(1)} Mbps`} valueColor="text-blue-400" sparklineData={ulData} sparklineColor="#3b82f6" sparklineId="ul" icon={ArrowUp} hoverShadow="hover:shadow-blue-500/10" onClick={() => setActiveDrawer("upload")} formatHoverValue={(v) => v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)} />
                    </motion.div>
                </motion.div>

                {/* Signal & Obstruction Row */}
                <motion.div className={`grid ${isMobile ? "grid-cols-1" : "grid-cols-2"} gap-3 md:gap-4`} variants={containerVar}>
                    <motion.div variants={cardVar}>
                        <StarlinkSignalCard status={status} onClick={() => setActiveDrawer("signal")} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkObstructionCard status={status} onClick={() => setActiveDrawer("obstruction")} />
                    </motion.div>
                </motion.div>

                {/* Alignment — dish rotation & tilt */}
                <motion.div variants={cardVar}>
                    <StarlinkAlignment status={status} />
                </motion.div>

                {/* Performance Charts */}
                <motion.div variants={cardVar}>
                    <StarlinkCharts data={history || []} isLoading={historyLoading} compact={isMobile} timeRange={timeRange} onTimeRangeChange={setTimeRange} />
                </motion.div>
            </motion.div>

            {/* Detail Drawer */}
            <StarlinkDetailDrawer isOpen={activeDrawer !== null} onClose={() => setActiveDrawer(null)} title={activeDrawer ? drawerTitles[activeDrawer] : ""}>
                {activeDrawer === "signal" && <StarlinkSignalDetail status={status} />}
                {activeDrawer === "obstruction" && (
                    obstructionMap
                        ? <StarlinkObstructionDetail status={status} mapData={obstructionMap} />
                        : (
                            // Without this the drawer opened completely blank while
                            // the (60s-interval) map query was still in flight.
                            <div className="flex items-center justify-center h-[280px] text-sm text-muted-foreground border-2 border-dashed border-white/5 rounded-2xl px-6 text-center">
                                {mapError
                                    ? "Failed to load the obstruction map."
                                    : mapLoading
                                    ? "Loading obstruction map…"
                                    : "No obstruction map available."}
                            </div>
                        )
                )}
                {(activeDrawer === "latency" || activeDrawer === "dropRate" || activeDrawer === "download" || activeDrawer === "upload") && (
                    <StarlinkMetricDetail metricType={activeDrawer} data={history || []} timeRange={timeRange} onTimeRangeChange={setTimeRange} />
                )}
                {activeDrawer === "alerts" && <StarlinkAlerts status={status} history={history || []} />}
            </StarlinkDetailDrawer>
        </>
    )
}
