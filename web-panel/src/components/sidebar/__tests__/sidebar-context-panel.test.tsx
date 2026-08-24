import { render, screen } from "@testing-library/react"
import "@testing-library/jest-dom/vitest"
import { MemoryRouter } from "react-router"
import { describe, it, expect, vi } from "vitest"
import { SidebarContextPanel } from "../sidebar-context-panel"

vi.mock("@/lib/queries/use-dashboard", () => ({
    useDashboardStats: () => ({ data: { online_users: 3 }, isLoading: false }),
    useOnlineUsersHistory: () => ({ data: [] }),
}))
vi.mock("@/lib/queries/use-dashboard-widgets", () => ({
    useNodeAggregateStats: () => ({ data: { onlineCount: 1, totalCount: 2 } }),
}))
vi.mock("@/lib/queries/use-subscriptions", () => ({
    useSubscriptionCounts: () => ({ data: undefined }),
    useSubsExpiringWithin: () => ({ count: 0 }),
}))
vi.mock("@/lib/design/palette", () => ({
    useChartPalette: () => ({
        success: "#0f0",
        warning: "#ff0",
        danger: "#f00",
        chart6: "#00f",
        neutral: "#888",
        mutedForeground: "#999",
        info: "#0ff",
        primary: "#0f0",
    }),
}))

function renderAt(path: string) {
    return render(
        <MemoryRouter initialEntries={[path]}>
            <SidebarContextPanel collapsed={false} />
        </MemoryRouter>,
    )
}

describe("SidebarContextPanel", () => {
    // The panel used to vanish on any route without a config of its own, and
    // the nav below it jumped by the whole card height on the way in and out.
    it("renders on a route that has no panel of its own", () => {
        renderAt("/router")
        expect(screen.getByText("SYSTEM.STATUS")).toBeInTheDocument()
    })

    it("still renders its own panel where one is configured", () => {
        renderAt("/alerts")
        expect(screen.getByText("ALERTS")).toBeInTheDocument()
    })

    it("renders something for a route nobody claimed", () => {
        renderAt("/nothing-here")
        expect(screen.getByText("SYSTEM.STATUS")).toBeInTheDocument()
    })

    // A card with a sparkline and one without must occupy the same height, or
    // walking between those two pages still shifts the nav.
    it("keeps the same height whether or not there is a sparkline", () => {
        const withChart = renderAt("/dashboard")
        const a = withChart.container.querySelectorAll("div.h-8").length
        withChart.unmount()
        const without = renderAt("/alerts")
        const b = without.container.querySelectorAll("div.h-8").length
        expect(a).toBe(b)
    })
})
