import { useMemo, useState } from "react"
import { motion, useReducedMotion } from "framer-motion"
import { RefreshCw, WifiOff, Satellite, AlertCircle } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { useQueryClient } from "@tanstack/react-query"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useChartPalette } from "@/lib/design/palette"
import { useStarlinkStatus, useStarlinkObstructionMap, useStarlinkHistory } from "@/lib/queries/use-starlink"
import { queryKeys } from "@/lib/queries/keys"

import { StarlinkStatusHeader } from "./starlink-status-header"
import { StarlinkMetricCard } from "./starlink-metric-card"
import { StarlinkSkyCard } from "./starlink-sky-card"
import { StarlinkCharts } from "./starlink-charts"
import { StarlinkDetailDrawer } from "./starlink-detail-drawer"
import { StarlinkSignalGauge, StarlinkSignalFacts } from "./starlink-signal-detail"
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
    const c = useChartPalette()
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

    // Drawer title
    const drawerTitles: Record<Exclude<DrawerType, null>, string> = {
        sky: "Sky & signal",
        latency: "Latency detail", dropRate: "Packet loss detail",
        download: "Download detail", upload: "Upload detail", alerts: "Alerts & outages",
    }

    // ─── Loading / Error States ─────────────────────────────────────
    if (!isOnline) {
        return (
            <div className="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed py-12 text-muted-foreground">
                <WifiOff className="mb-4 h-12 w-12 opacity-50" />
                <h3 className="text-lg font-medium text-foreground">Node offline</h3>
                <p className="px-4 text-center text-sm">The node must be online to fetch Starlink metrics.</p>
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
            <div className="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-red-500/20 bg-red-500/5 py-12 text-muted-foreground">
                <AlertCircle className="mb-4 h-12 w-12 text-red-400 opacity-50" />
                <h3 className="text-lg font-medium text-red-400">Failed to load</h3>
                <p className="mt-2 mb-4 max-w-md px-4 text-center text-sm">{message}</p>
                <button
                    onClick={() => {
                        refetchStatus()
                        queryClient.invalidateQueries({ queryKey: queryKeys.starlinkMap(nodeId) })
                        queryClient.invalidateQueries({ queryKey: queryKeys.starlinkHistory(nodeId, timeRange) })
                    }}
                    disabled={statusFetching}
                    className="flex items-center gap-2 rounded-md border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-semibold text-red-400 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                >
                    <RefreshCw className={`h-3.5 w-3.5 ${statusFetching ? "animate-spin" : ""}`} />
                    Retry
                </button>
            </div>
        )
    }

    if (status && !status.available) {
        return (
            <div className="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-red-500/20 bg-red-500/5 py-12 text-muted-foreground">
                <Satellite className="mb-4 h-12 w-12 text-red-400 opacity-50" />
                <h3 className="text-lg font-medium text-red-400">Dish unreachable</h3>
                <p className="mt-2 max-w-md px-4 text-center text-sm">
                    Verify the dish address in node settings and ensure the dish is powered on.
                </p>
            </div>
        )
    }

    if (!status) return null

    return (
        <>
            <motion.div className="space-y-4" initial="hidden" animate="visible" variants={containerVar}>
                {/* Status row */}
                <motion.div variants={cardVar}>
                    <StarlinkStatusHeader status={status} onAlertsClick={() => setActiveDrawer("alerts")} dataUpdatedAt={statusUpdatedAt} />
                </motion.div>

                {/* Hero Metric Cards */}
                <motion.div className={`grid ${isMobile ? "grid-cols-2" : "grid-cols-4"} gap-3 md:gap-4`} variants={containerVar}>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard
                            label="Latency"
                            value={status.pop_ping_latency_ms.toFixed(0)}
                            unit="ms"
                            valueColor={latencyColor(status.pop_ping_latency_ms)}
                            sparklineData={latencyData}
                            sparklineColor={c.warning}
                            sparklineId="latency"
                            onClick={() => setActiveDrawer("latency")}
                            format={(v) => v.toFixed(0)}
                            polarity="lower-better"
                        />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard
                            label="Drop rate"
                            value={(status.pop_ping_drop_rate * 100).toFixed(1)}
                            unit="%"
                            valueColor={dropRateColor(status.pop_ping_drop_rate)}
                            sparklineData={dropData}
                            sparklineColor={c.danger}
                            sparklineId="drop"
                            onClick={() => setActiveDrawer("dropRate")}
                            format={(v) => v.toFixed(1)}
                            polarity="lower-better"
                            // A link dropping no packets averages 0.0%, and a
                            // percentage change against that is pure noise.
                            deltaFloor={0.05}
                        />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard
                            label="Download"
                            value={formatMbps(status.downlink_throughput_bps)}
                            unit="Mbps"
                            valueColor="text-foreground"
                            sparklineData={dlData}
                            sparklineColor={c.success}
                            sparklineId="dl"
                            onClick={() => setActiveDrawer("download")}
                            format={(v) => v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)}
                            polarity="higher-better"
                            deltaFloor={0.01}
                        />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard
                            label="Upload"
                            value={formatMbps(status.uplink_throughput_bps)}
                            unit="Mbps"
                            valueColor="text-foreground"
                            sparklineData={ulData}
                            sparklineColor={c.info}
                            sparklineId="ul"
                            onClick={() => setActiveDrawer("upload")}
                            format={(v) => v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)}
                            polarity="higher-better"
                            deltaFloor={0.01}
                        />
                    </motion.div>
                </motion.div>

                {/* Sky & Signal + Alignment */}
                <motion.div className={`grid ${isMobile ? "grid-cols-1" : "grid-cols-2"} gap-3 md:gap-4`} variants={containerVar}>
                    <motion.div variants={cardVar}>
                        <StarlinkSkyCard status={status} mapData={obstructionMap} onClick={() => setActiveDrawer("sky")} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkAlignment status={status} />
                    </motion.div>
                </motion.div>

                {/* Performance Charts */}
                <motion.div variants={cardVar}>
                    <StarlinkCharts data={history || []} isLoading={historyLoading} compact={isMobile} timeRange={timeRange} onTimeRangeChange={setTimeRange} />
                </motion.div>
            </motion.div>

            {/* Detail Drawer */}
            <StarlinkDetailDrawer
                isOpen={activeDrawer !== null}
                onClose={() => setActiveDrawer(null)}
                title={activeDrawer ? drawerTitles[activeDrawer] : ""}
            >
                {activeDrawer === "sky" && (
                    <div className="space-y-5">
                        <StarlinkSignalGauge status={status} />
                        {obstructionMap
                            ? <StarlinkObstructionDetail status={status} mapData={obstructionMap} />
                            : (
                                // Without this the drawer opened completely blank while
                                // the (60s-interval) map query was still in flight.
                                <div className="flex h-[280px] items-center justify-center rounded-2xl border-2 border-dashed border-border px-6 text-center text-sm text-muted-foreground">
                                    {mapError
                                        ? "Failed to load the obstruction map."
                                        : mapLoading
                                        ? "Loading obstruction map…"
                                        : "No obstruction map available."}
                                </div>
                            )}
                        <StarlinkSignalFacts status={status} />
                    </div>
                )}
                {(activeDrawer === "latency" || activeDrawer === "dropRate" || activeDrawer === "download" || activeDrawer === "upload") && (
                    <StarlinkMetricDetail metricType={activeDrawer} data={history || []} timeRange={timeRange} onTimeRangeChange={setTimeRange} />
                )}
                {activeDrawer === "alerts" && <StarlinkAlerts status={status} history={history || []} />}
            </StarlinkDetailDrawer>
        </>
    )
}
