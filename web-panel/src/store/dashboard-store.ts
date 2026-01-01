import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { Layout, ResponsiveLayouts } from "react-grid-layout"

type Layouts = ResponsiveLayouts

export const WIDGET_IDS = {
    STATS_ROW: "stats-row",
    NETWORK_TRAFFIC: "network-traffic",
    SYSTEM_HEALTH: "system-health",
    ACTIVITY_FEED: "activity-feed",
    ACTIVITY_HEATMAP: "activity-heatmap",
    PEAK_HOURS: "peak-hours",
    BLOCKED_DOMAINS: "blocked-domains",
} as const

export type WidgetId = (typeof WIDGET_IDS)[keyof typeof WIDGET_IDS]

const DEFAULT_LAYOUTS: Layouts = {
    lg: [
        { i: WIDGET_IDS.STATS_ROW, x: 0, y: 0, w: 12, h: 3, minW: 6, minH: 3, isResizable: false },
        { i: WIDGET_IDS.NETWORK_TRAFFIC, x: 6, y: 3, w: 6, h: 12, minW: 4, minH: 8 },
        { i: WIDGET_IDS.SYSTEM_HEALTH, x: 0, y: 15, w: 6, h: 8, minW: 4, minH: 6 },
        { i: WIDGET_IDS.ACTIVITY_FEED, x: 6, y: 15, w: 6, h: 8, minW: 4, minH: 6 },
        { i: WIDGET_IDS.ACTIVITY_HEATMAP, x: 0, y: 23, w: 6, h: 6, minW: 4, minH: 5 },
        { i: WIDGET_IDS.PEAK_HOURS, x: 0, y: 29, w: 6, h: 10, minW: 4, minH: 8 },
        { i: WIDGET_IDS.BLOCKED_DOMAINS, x: 6, y: 29, w: 6, h: 10, minW: 4, minH: 8 },
    ],
    md: [
        { i: WIDGET_IDS.STATS_ROW, x: 0, y: 0, w: 6, h: 3, minW: 6, minH: 3, isResizable: false },
        { i: WIDGET_IDS.NETWORK_TRAFFIC, x: 0, y: 15, w: 6, h: 12, minW: 3, minH: 8 },
        { i: WIDGET_IDS.SYSTEM_HEALTH, x: 0, y: 27, w: 6, h: 8, minW: 3, minH: 6 },
        { i: WIDGET_IDS.ACTIVITY_FEED, x: 0, y: 35, w: 6, h: 8, minW: 3, minH: 6 },
        { i: WIDGET_IDS.ACTIVITY_HEATMAP, x: 0, y: 43, w: 6, h: 6, minW: 3, minH: 5 },
        { i: WIDGET_IDS.PEAK_HOURS, x: 0, y: 55, w: 6, h: 10, minW: 3, minH: 8 },
        { i: WIDGET_IDS.BLOCKED_DOMAINS, x: 0, y: 65, w: 6, h: 10, minW: 3, minH: 8 },
    ],
    sm: [
        { i: WIDGET_IDS.STATS_ROW, x: 0, y: 0, w: 1, h: 3, isResizable: false },
        { i: WIDGET_IDS.NETWORK_TRAFFIC, x: 0, y: 15, w: 1, h: 12 },
        { i: WIDGET_IDS.SYSTEM_HEALTH, x: 0, y: 27, w: 1, h: 8 },
        { i: WIDGET_IDS.ACTIVITY_FEED, x: 0, y: 35, w: 1, h: 8 },
        { i: WIDGET_IDS.ACTIVITY_HEATMAP, x: 0, y: 43, w: 1, h: 6 },
        { i: WIDGET_IDS.PEAK_HOURS, x: 0, y: 55, w: 1, h: 10 },
        { i: WIDGET_IDS.BLOCKED_DOMAINS, x: 0, y: 65, w: 1, h: 10 },
    ],
}

const defaultVisibility: Record<string, boolean> = Object.values(WIDGET_IDS).reduce(
    (acc, id) => ({ ...acc, [id]: true }),
    {} as Record<string, boolean>
)

interface DashboardStore {
    layouts: Layouts
    isEditMode: boolean
    widgetVisibility: Record<string, boolean>
    setLayouts: (layouts: Layouts) => void
    setEditMode: (editing: boolean) => void
    toggleWidget: (widgetId: string) => void
    resetLayouts: () => void
}

export const useDashboardStore = create<DashboardStore>()(
    persist(
        (set) => ({
            layouts: DEFAULT_LAYOUTS,
            isEditMode: false,
            widgetVisibility: defaultVisibility,
            setLayouts: (layouts) => set({ layouts }),
            setEditMode: (isEditMode) => set({ isEditMode }),
            toggleWidget: (widgetId) =>
                set((state) => ({
                    widgetVisibility: {
                        ...state.widgetVisibility,
                        [widgetId]: !state.widgetVisibility[widgetId],
                    },
                })),
            resetLayouts: () =>
                set({ layouts: DEFAULT_LAYOUTS, widgetVisibility: defaultVisibility }),
        }),
        {
            name: "dashboard-layout",
            partialize: (state) => ({
                layouts: state.layouts,
                widgetVisibility: state.widgetVisibility,
            }),
        }
    )
)
