import { useEffect, useMemo, useRef, useState } from "react"
import { motion, useReducedMotion } from "framer-motion"
import { AlertCircle, Loader2, RefreshCw, Satellite, WifiOff } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { useQueryClient } from "@tanstack/react-query"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { starlink as slPalette } from "@/lib/design/palette"
import {
    useStarlinkDevice, useStarlinkEvents, useStarlinkHistory, useStarlinkLive,
    useStarlinkObstructionMap, useStarlinkStatus,
} from "@/lib/queries/use-starlink"
import { queryKeys } from "@/lib/queries/keys"
import type { StarlinkAction } from "@/lib/types"

import { StarlinkStatusHeader } from "./starlink-status-header"
import { StarlinkActionsMenu } from "./starlink-actions-menu"
import { StarlinkNotices } from "./starlink-notices"
import { StarlinkMetricCard } from "./starlink-metric-card"
import { StarlinkSkyCard } from "./starlink-sky-card"
import { StarlinkCharts, tabsFor, type ChartTab } from "./starlink-charts"
import { StarlinkEventsCard } from "./starlink-events-card"
import { StarlinkDeviceCard } from "./starlink-device-card"
import { StarlinkDetailDrawer } from "./starlink-detail-drawer"
import { StarlinkSignalGauge } from "./starlink-signal-detail"
import { StarlinkObstructionDetail } from "./starlink-obstruction-detail"
import { StarlinkAlignment } from "./starlink-alignment"
import { StarlinkAlerts } from "./starlink-alerts"
import {
    type DrawerType, type EventRange, type TimeRange,
    TIME_RANGE_CONFIG, dropRateColor, formatMbps, isAgentOutdated, latencyColor,
} from "./starlink-helpers"
import { lastValue, outageBands, pointsFromHistory, pointsFromLive, sparkSeries } from "./starlink-series"

interface StarlinkDashboardProps {
    nodeId: number
    nodeName: string
    isOnline: boolean
}

// Actions after which the dish API itself goes away for a while.
const RESTARTING: StarlinkAction[] = ["reboot", "factory-reset"]
const WAIT_LIMIT_MS = 5 * 60_000

const containerVariants = {
    hidden: { opacity: 0 },
    visible: { opacity: 1, transition: { staggerChildren: 0.05, delayChildren: 0.05 } },
} as const
const cardVariants = {
    hidden: { opacity: 0, y: 12 },
    visible: { opacity: 1, y: 0, transition: { type: "spring" as const, stiffness: 400, damping: 30 } },
}
const noMotion = { hidden: { opacity: 1 }, visible: { opacity: 1 } } as const

const fmtMbps = (v: number) => (v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2))

export function StarlinkDashboard({ nodeId, nodeName, isOnline }: StarlinkDashboardProps) {
    const isMobile = useIsMobile()
    const reduceMotion = useReducedMotion()
    const containerVar = reduceMotion ? noMotion : containerVariants
    const cardVar = reduceMotion ? noMotion : cardVariants
    const queryClient = useQueryClient()
    const chartRef = useRef<HTMLDivElement>(null)

    const [drawer, setDrawer] = useState<DrawerType>(null)
    const [range, setRange] = useState<TimeRange | null>(null)
    const [tab, setTab] = useState<ChartTab>("throughput")
    const [eventRange, setEventRange] = useState<EventRange>("24h")
    const [waiting, setWaiting] = useState<{ action: StarlinkAction; since: number; sawDown: boolean } | null>(null)

    const statusQ = useStarlinkStatus(nodeId, isOnline, waiting ? 3_000 : 10_000)
    const status = statusQ.data
    const passthrough = !!status?.passthrough

    const liveQ = useStarlinkLive(nodeId, isOnline && passthrough)
    const deviceQ = useStarlinkDevice(nodeId, isOnline && passthrough)
    const mapQ = useStarlinkObstructionMap(nodeId, isOnline)
    const eventsQ = useStarlinkEvents(nodeId, isOnline, eventRange)

    const agentOutdated = !!status && (!passthrough || isAgentOutdated(liveQ.error))
    const liveAvailable = passthrough && !isAgentOutdated(liveQ.error)
    const timeRange: TimeRange = range ?? (liveAvailable ? "15m" : "1h")
    // 15m reads the dish's own buffer; the DB query stays on 1h so the KPI
    // sparklines still have data if the live query fails.
    const dbRange = timeRange === "15m" ? "1h" : timeRange
    const historyQ = useStarlinkHistory(nodeId, isOnline, dbRange, TIME_RANGE_CONFIG[dbRange].limit, TIME_RANGE_CONFIG[dbRange].refetchInterval)
    // Outage bands follow the chart's range, not the events card's.
    const bandRange: EventRange = timeRange === "7d" ? "7d" : "24h"
    const bandEventsQ = useStarlinkEvents(nodeId, isOnline && timeRange !== "15m", bandRange)

    // Clear the waiting state once the dish went down and came back, or give up after the limit.
    // A restarting dish shows up either as available=false or as a status
    // call that times out; both count as "went down".
    const statusDown = statusQ.isError || (!!status && !status.available)
    useEffect(() => {
        if (!waiting) return
        if (statusDown) {
            if (!waiting.sawDown) setWaiting({ ...waiting, sawDown: true })
            return
        }
        if (status && (waiting.sawDown || Date.now() - waiting.since > WAIT_LIMIT_MS)) setWaiting(null)
    }, [status, statusDown, waiting])
    useEffect(() => {
        if (!waiting) return
        const id = setTimeout(() => setWaiting(null), WAIT_LIMIT_MS)
        return () => clearTimeout(id)
    }, [waiting])

    const livePoints = useMemo(() => (liveQ.data ? pointsFromLive(liveQ.data) : []), [liveQ.data])
    const dbPoints = useMemo(() => pointsFromHistory(historyQ.data ?? []), [historyQ.data])
    const chartPoints = timeRange === "15m" ? livePoints : dbPoints
    const kpiPoints = liveAvailable && livePoints.length > 0 ? livePoints : dbPoints
    const bands = useMemo(
        () => outageBands(timeRange === "15m" ? (liveQ.data?.events ?? []) : (bandEventsQ.data ?? [])),
        [timeRange, liveQ.data, bandEventsQ.data],
    )
    const power = lastValue(livePoints, "power_w")

    const onRangeChange = (r: TimeRange) => {
        setRange(r)
        if (!tabsFor(r).includes(tab)) setTab("throughput")
    }
    const focusChart = (t: ChartTab) => {
        if (!tabsFor(timeRange).includes(t)) setRange(t === "power" ? "15m" : "1h")
        setTab(t)
        chartRef.current?.scrollIntoView({ behavior: reduceMotion ? "auto" : "smooth", block: "center" })
    }
    const onActionStarted = (a: StarlinkAction) => {
        if (RESTARTING.includes(a)) setWaiting({ action: a, since: Date.now(), sawDown: false })
    }

    // ─── Whole-tab states ───────────────────────────────────────────
    if (!isOnline) {
        return <EmptyState icon={WifiOff} title="Node offline" body="The node must be online to fetch Starlink metrics." />
    }

    if (statusQ.isLoading && !status) {
        return (
            <div className="space-y-4">
                <Skeleton className="h-11 w-full rounded-xl" />
                <div className="grid grid-cols-2 gap-3 md:gap-4 lg:grid-cols-5">
                    {[...Array(5)].map((_, i) => <Skeleton key={i} className="h-[170px] rounded-2xl" />)}
                </div>
                <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3">
                    <Skeleton className="h-[380px] rounded-2xl lg:col-span-2" />
                    <Skeleton className="h-[380px] rounded-2xl" />
                </div>
            </div>
        )
    }

    // Checked before the error branch: a restarting dish can also time the
    // status call out, and that must not read as "Failed to load".
    if (waiting && (statusQ.isError || (status && !status.available))) {
        return (
            <EmptyState icon={Loader2} spin title={waiting.action === "factory-reset" ? "Dish is resetting" : "Dish is restarting"}
                body="The dish API is down while it restarts. This page comes back by itself when the dish answers again." />
        )
    }

    if (statusQ.isError || !status) {
        const message = statusQ.error instanceof Error ? statusQ.error.message : "Failed to load Starlink status"
        return (
            <EmptyState icon={AlertCircle} tone="red" title="Failed to load" body={message}>
                <button
                    onClick={() => {
                        statusQ.refetch()
                        queryClient.invalidateQueries({ queryKey: queryKeys.starlinkMap(nodeId) })
                    }}
                    disabled={statusQ.isFetching}
                    className="mt-4 flex items-center gap-2 rounded-md border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-semibold text-red-400 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                >
                    <RefreshCw className={`h-3.5 w-3.5 ${statusQ.isFetching ? "animate-spin" : ""}`} />
                    Retry
                </button>
            </EmptyState>
        )
    }

    if (!status.available) {
        return (
            <EmptyState icon={Satellite} tone="red" title="Dish unreachable"
                body="Verify the dish address in node settings and ensure the dish is powered on." />
        )
    }

    const actions = (
        <StarlinkActionsMenu
            nodeId={nodeId}
            nodeName={nodeName}
            status={status}
            disabledReason={agentOutdated ? "Update the node agent to use dish actions" : undefined}
            onStarted={onActionStarted}
        />
    )

    return (
        <>
            <motion.div className="space-y-4" initial="hidden" animate="visible" variants={containerVar}>
                <motion.div variants={cardVar} className="space-y-2">
                    <StarlinkStatusHeader status={status} onAlertsClick={() => setDrawer("alerts")} dataUpdatedAt={statusQ.dataUpdatedAt} actions={actions} />
                    <StarlinkNotices status={status} agentOutdated={agentOutdated} waiting={waiting} />
                </motion.div>

                <motion.div className={`grid grid-cols-2 gap-3 md:gap-4 ${liveAvailable ? "lg:grid-cols-5" : "lg:grid-cols-4"}`} variants={containerVar}>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Latency" value={status.pop_ping_latency_ms.toFixed(0)} unit="ms"
                            valueColor={latencyColor(status.pop_ping_latency_ms)} sparklineData={sparkSeries(kpiPoints, "latency")}
                            sparklineColor={slPalette.latency} sparklineId="latency" onClick={() => focusChart("latency")}
                            format={(v) => v.toFixed(0)} polarity="lower-better" />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Drop rate" value={(status.pop_ping_drop_rate * 100).toFixed(1)} unit="%"
                            valueColor={dropRateColor(status.pop_ping_drop_rate)} sparklineData={sparkSeries(kpiPoints, "drop_pct")}
                            sparklineColor={slPalette.dropRate} sparklineId="drop" onClick={() => focusChart("loss")}
                            format={(v) => v.toFixed(1)} polarity="lower-better" deltaFloor={0.05} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Download" value={formatMbps(status.downlink_throughput_bps)} unit="Mbps"
                            valueColor="text-foreground" sparklineData={sparkSeries(kpiPoints, "dl_mbps")}
                            sparklineColor={slPalette.download} sparklineId="dl" onClick={() => focusChart("throughput")}
                            format={fmtMbps} polarity="higher-better" deltaFloor={0.01} />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkMetricCard label="Upload" value={formatMbps(status.uplink_throughput_bps)} unit="Mbps"
                            valueColor="text-foreground" sparklineData={sparkSeries(kpiPoints, "ul_mbps")}
                            sparklineColor={slPalette.upload} sparklineId="ul" onClick={() => focusChart("throughput")}
                            format={fmtMbps} polarity="higher-better" deltaFloor={0.01} />
                    </motion.div>
                    {liveAvailable && (
                        <motion.div variants={cardVar} className="col-span-2 lg:col-span-1">
                            <StarlinkMetricCard label="Power" value={power == null ? "—" : power.toFixed(0)} unit="W"
                                valueColor="text-foreground" sparklineData={sparkSeries(livePoints, "power_w")}
                                sparklineColor={slPalette.power} sparklineId="power" onClick={() => focusChart("power")}
                                format={(v) => v.toFixed(0)} polarity="lower-better" />
                        </motion.div>
                    )}
                </motion.div>

                <motion.div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3" variants={containerVar}>
                    <motion.div variants={cardVar} className="lg:col-span-2" ref={chartRef}>
                        <StarlinkCharts
                            points={chartPoints}
                            bands={bands}
                            isLoading={timeRange === "15m" ? liveQ.isLoading : historyQ.isLoading}
                            compact={isMobile}
                            timeRange={timeRange}
                            onTimeRangeChange={onRangeChange}
                            tab={tab}
                            onTabChange={setTab}
                            liveAvailable={liveAvailable}
                        />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkSkyCard status={status} mapData={mapQ.data} onClick={() => setDrawer("sky")} />
                    </motion.div>
                </motion.div>

                <motion.div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3" variants={containerVar}>
                    <motion.div variants={cardVar} className="lg:col-span-2">
                        <StarlinkEventsCard
                            events={eventsQ.data}
                            liveEvents={liveQ.data?.events}
                            isLoading={eventsQ.isLoading}
                            range={eventRange}
                            onRangeChange={setEventRange}
                            historyAvailable={liveAvailable}
                        />
                    </motion.div>
                    <motion.div variants={cardVar}>
                        <StarlinkDeviceCard status={status} device={deviceQ.data} />
                    </motion.div>
                </motion.div>
            </motion.div>

            <StarlinkDetailDrawer
                isOpen={drawer !== null}
                onClose={() => setDrawer(null)}
                title={drawer === "sky" ? "Sky & alignment" : "Alerts & events"}
            >
                {drawer === "sky" && (
                    <div className="space-y-5">
                        <StarlinkSignalGauge status={status} />
                        {mapQ.data ? (
                            <StarlinkObstructionDetail status={status} mapData={mapQ.data} />
                        ) : (
                            <div className="flex h-[280px] items-center justify-center rounded-2xl border-2 border-dashed border-border px-6 text-center text-sm text-muted-foreground">
                                {mapQ.isError ? "Failed to load the obstruction map." : mapQ.isLoading ? "Loading obstruction map…" : "No obstruction map available."}
                            </div>
                        )}
                        <StarlinkAlignment status={status} />
                    </div>
                )}
                {drawer === "alerts" && (
                    <StarlinkAlerts status={status} history={historyQ.data ?? []} events={eventsQ.data ?? liveQ.data?.events} />
                )}
            </StarlinkDetailDrawer>
        </>
    )
}

function EmptyState({ icon: Icon, title, body, tone, spin, children }: {
    icon: React.ComponentType<{ className?: string }>
    title: string
    body: string
    tone?: "red"
    spin?: boolean
    children?: React.ReactNode
}) {
    const red = tone === "red"
    return (
        <div className={`flex flex-col items-center justify-center rounded-2xl border-2 border-dashed py-12 text-muted-foreground ${red ? "border-red-500/20 bg-red-500/5" : ""}`}>
            <Icon className={`mb-4 h-12 w-12 opacity-50 ${red ? "text-red-400" : ""} ${spin ? "animate-spin" : ""}`} />
            <h3 className={`text-lg font-medium ${red ? "text-red-400" : "text-foreground"}`}>{title}</h3>
            <p className="mt-2 max-w-md px-4 text-center text-sm">{body}</p>
            {children}
        </div>
    )
}
