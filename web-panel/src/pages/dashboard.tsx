import { useCallback, useEffect, useRef, useState } from "react"
import { Responsive, WidthProvider } from "react-grid-layout/legacy"

const ResponsiveGridLayout = WidthProvider(Responsive)
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import {
    HiOutlineRefresh,
    HiOutlineExclamation,
} from "react-icons/hi"
import { LayoutGrid, RotateCcw, Lock, Unlock } from "lucide-react"
import { useDashboardStats, useNodesSummary, queryKeys } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import { Card, CardContent } from "@/components/ui/card"
import { ErrorBoundary } from "@/components/ui/error-boundary"

// Widget error fallback
function WidgetErrorFallback({ name, onRetry }: { name: string, onRetry: () => void }) {
    return (
        <Card className="h-full border-dashed border-red-500/30 bg-red-500/5">
            <CardContent className="h-full flex flex-col items-center justify-center py-8 space-y-3">
                <HiOutlineExclamation className="w-8 h-8 text-red-400" />
                <div className="text-center space-y-1">
                    <p className="text-sm font-medium text-foreground">Failed to load {name}</p>
                    <p className="text-xs text-muted-foreground">An error occurred while rendering this widget</p>
                </div>
                <Button variant="outline" size="sm" onClick={onRetry}>
                    <HiOutlineRefresh className="w-3.5 h-3.5 mr-1.5" />
                    Retry
                </Button>
            </CardContent>
        </Card>
    )
}

// Dashboard widgets
import { StatsRow } from "@/components/dashboard/stats-row"
import { NetworkTrafficWidget } from "@/components/dashboard/network-traffic-widget"
import { SystemHealth } from "@/components/dashboard/system-health"
import { ActivityFeed } from "@/components/dashboard/activity-feed"
import { ActivityHeatmap } from "@/components/dashboard/activity-heatmap"
import { PeakHoursWidget } from "@/components/dashboard/peak-hours-widget"
import { BlockedDomainsWidget } from "@/components/dashboard/blocked-domains-widget"

// Store
import { useDashboardStore, WIDGET_IDS } from "@/store/dashboard-store"

// ─── Relative time helper ──────────────────────────────────────────────────────

function useRelativeTime(timestamp: number | undefined) {
    const [label, setLabel] = useState("")
    const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

    useEffect(() => {
        if (intervalRef.current) {
            clearInterval(intervalRef.current)
            intervalRef.current = null
        }
        if (!timestamp) { setLabel(""); return }

        const update = () => {
            const seconds = Math.floor((Date.now() - timestamp) / 1000)
            if (seconds < 5) setLabel("just now")
            else if (seconds < 60) setLabel(`${seconds}s ago`)
            else if (seconds < 3600) setLabel(`${Math.floor(seconds / 60)}m ago`)
            else setLabel(`${Math.floor(seconds / 3600)}h ago`)
        }

        update()
        intervalRef.current = setInterval(update, 5000)
        return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
    }, [timestamp])

    return label
}

// ─── Dashboard page ─────────────────────────────────────────────────────────────

export default function DashboardPage() {
    const queryClient = useQueryClient()
    const [mounted, setMounted] = useState(false)

    // Dashboard store
    const { layouts, isEditMode, widgetVisibility, setLayouts, setEditMode, resetLayouts } = useDashboardStore()

    // Data queries
    const { data: stats, isLoading: statsLoading, error: statsError, dataUpdatedAt: statsUpdatedAt } = useDashboardStats()
    const isLoading = statsLoading
    const error = statsError?.message
    const syncedLabel = useRelativeTime(statsUpdatedAt || undefined)

    const formattedDate = new Date().toLocaleDateString("en-US", {
        weekday: "long",
        year: "numeric",
        month: "long",
        day: "numeric",
    })

    const handleRefresh = useCallback(() => {
        queryClient.invalidateQueries({ queryKey: queryKeys.dashboard })
        queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
    }, [queryClient])

    useEffect(() => {
        setMounted(true)
    }, [])

    const handleLayoutChange = useCallback((_: any, allLayouts: any) => {
        setLayouts(allLayouts)
    }, [setLayouts])

    // Filter visible widgets for the grid
    const visibleWidgets = Object.entries(widgetVisibility)
        .filter(([_, visible]) => visible)
        .map(([id]) => id)

    return (
        <div className="space-y-4 animate-in fade-in duration-500">
            {/* ── Header ─────────────────────────────────────────────── */}
            <div className="flex flex-col sm:flex-row sm:items-end justify-between gap-4">
                <div>
                    <h1 className="text-2xl sm:text-3xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text">
                        Dashboard
                    </h1>
                    <div className="flex items-center gap-3 mt-1.5">
                        <p className="text-sm text-muted-foreground">{formattedDate}</p>
                        {syncedLabel && (
                            <>
                                <span className="text-muted-foreground/40">|</span>
                                <span className="flex items-center gap-1.5 text-xs text-muted-foreground/70">
                                    <span className="relative flex h-1.5 w-1.5">
                                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                                        <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500" />
                                    </span>
                                    Synced {syncedLabel}
                                </span>
                            </>
                        )}
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <AutoRefreshControl onRefresh={handleRefresh} isRefreshing={isLoading} />
                    {/* Edit mode controls - desktop only */}
                    <div className="hidden md:flex items-center gap-2">
                        {isEditMode && (
                            <Button variant="outline" size="sm" onClick={resetLayouts}>
                                <RotateCcw className="w-3.5 h-3.5 mr-1.5" />
                                Reset
                            </Button>
                        )}
                        <Button
                            variant={isEditMode ? "default" : "outline"}
                            size="sm"
                            onClick={() => setEditMode(!isEditMode)}
                        >
                            {isEditMode ? (
                                <><Lock className="w-3.5 h-3.5 mr-1.5" /> Lock Layout</>
                            ) : (
                                <><LayoutGrid className="w-3.5 h-3.5 mr-1.5" /> Edit Layout</>
                            )}
                        </Button>
                    </div>
                    <Button variant="outline" size="sm" onClick={handleRefresh}>
                        <HiOutlineRefresh className={cn("w-3.5 h-3.5 mr-1.5", isLoading && "animate-spin")} />
                        Refresh
                    </Button>
                </div>
            </div>

            {/* ── Edit mode indicator ─────────────────────────────── */}
            {isEditMode && (
                <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-primary/5 border border-primary/20 text-sm">
                    <Unlock className="w-4 h-4 text-primary" />
                    <span className="text-primary font-medium">Layout editing mode</span>
                    <span className="text-muted-foreground">— Drag widgets to rearrange, click Lock Layout when done</span>
                </div>
            )}

            {/* ── Error state ────────────────────────────────────────── */}
            {error && (
                <Card className="border-red-500/50 bg-red-500/10">
                    <CardContent className="flex items-center gap-3 py-4">
                        <HiOutlineExclamation className="w-5 h-5 text-red-500" />
                        <p className="text-sm text-red-500">{error}</p>
                    </CardContent>
                </Card>
            )}

            {/* ── Stats Row (always on top, not in grid) ─────────────── */}
            {visibleWidgets.includes(WIDGET_IDS.STATS_ROW) && (
                <ErrorBoundary fallback={<WidgetErrorFallback name="Stats" onRetry={() => handleRefresh()} />}>
                    <StatsRow stats={stats} isLoading={statsLoading} />
                </ErrorBoundary>
            )}

            {/* ── Widget Grid ──────────────────────────────────────── */}
            {mounted && (
                <ResponsiveGridLayout
                    className="layout"
                    layouts={layouts}
                    breakpoints={{ lg: 1024, md: 768, sm: 0 }}
                    cols={{ lg: 12, md: 6, sm: 1 }}
                    rowHeight={30}
                    isDraggable={isEditMode}
                    isResizable={isEditMode}
                    onLayoutChange={handleLayoutChange}
                    draggableHandle=".drag-handle"
                    containerPadding={[0, 0]}
                    margin={[16, 16]}
                    useCSSTransforms
                >
                    {visibleWidgets.includes(WIDGET_IDS.NETWORK_TRAFFIC) && (
                        <div key={WIDGET_IDS.NETWORK_TRAFFIC}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="Network Traffic" onRetry={() => handleRefresh()} />}>
                                <NetworkTrafficWidget isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}

                    {visibleWidgets.includes(WIDGET_IDS.SYSTEM_HEALTH) && (
                        <div key={WIDGET_IDS.SYSTEM_HEALTH}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="System Health" onRetry={() => handleRefresh()} />}>
                                <SystemHealth isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}

                    {visibleWidgets.includes(WIDGET_IDS.ACTIVITY_FEED) && (
                        <div key={WIDGET_IDS.ACTIVITY_FEED}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="Activity Feed" onRetry={() => handleRefresh()} />}>
                                <ActivityFeed isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}

                    {visibleWidgets.includes(WIDGET_IDS.ACTIVITY_HEATMAP) && (
                        <div key={WIDGET_IDS.ACTIVITY_HEATMAP}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="Activity Heatmap" onRetry={() => handleRefresh()} />}>
                                <ActivityHeatmap isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}

                    {visibleWidgets.includes(WIDGET_IDS.PEAK_HOURS) && (
                        <div key={WIDGET_IDS.PEAK_HOURS}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="Peak Hours" onRetry={() => handleRefresh()} />}>
                                <PeakHoursWidget isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}

                    {visibleWidgets.includes(WIDGET_IDS.BLOCKED_DOMAINS) && (
                        <div key={WIDGET_IDS.BLOCKED_DOMAINS}>
                            <ErrorBoundary fallback={<WidgetErrorFallback name="Blocked Domains" onRetry={() => handleRefresh()} />}>
                                <BlockedDomainsWidget isEditMode={isEditMode} />
                            </ErrorBoundary>
                        </div>
                    )}
                </ResponsiveGridLayout>
            )}

            {/* ── Mobile fallback (no grid layout, stacked) ──────── */}
            {!mounted && (
                <div className="space-y-4 pb-6">
                    <ErrorBoundary fallback={<WidgetErrorFallback name="Network Traffic" onRetry={() => handleRefresh()} />}>
                        <NetworkTrafficWidget />
                    </ErrorBoundary>
                    <ErrorBoundary fallback={<WidgetErrorFallback name="System Health" onRetry={() => handleRefresh()} />}>
                        <SystemHealth />
                    </ErrorBoundary>
                    <ErrorBoundary fallback={<WidgetErrorFallback name="Activity Feed" onRetry={() => handleRefresh()} />}>
                        <ActivityFeed />
                    </ErrorBoundary>
                    <ErrorBoundary fallback={<WidgetErrorFallback name="Activity Heatmap" onRetry={() => handleRefresh()} />}>
                        <ActivityHeatmap />
                    </ErrorBoundary>
                    <ErrorBoundary fallback={<WidgetErrorFallback name="Peak Hours" onRetry={() => handleRefresh()} />}>
                        <PeakHoursWidget />
                    </ErrorBoundary>
                    <ErrorBoundary fallback={<WidgetErrorFallback name="Blocked Domains" onRetry={() => handleRefresh()} />}>
                        <BlockedDomainsWidget />
                    </ErrorBoundary>
                </div>
            )}
        </div>
    )
}
