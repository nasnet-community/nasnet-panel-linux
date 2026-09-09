import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SidebarContextPanel } from "../sidebar-context-panel"
import * as dashboard from "@/lib/api/dashboard"
import * as nodes from "@/lib/api/nodes"
import * as subscriptions from "@/lib/api/subscriptions"
import { queryKeys } from "@/lib/queries/keys"

vi.mock("@/hooks/use-refresh-interval", () => ({ useRefreshInterval: () => 10_000 }))
vi.mock("@/lib/api/dashboard", async original => ({ ...await original<typeof import("@/lib/api/dashboard")>(), getDashboardStats: vi.fn(), getOnlineUsersHistory: vi.fn() }))
vi.mock("@/lib/api/nodes", async original => ({ ...await original<typeof import("@/lib/api/nodes")>(), listNodes: vi.fn(), getNodesStatsBulk: vi.fn() }))
vi.mock("@/lib/api/subscriptions", async original => ({ ...await original<typeof import("@/lib/api/subscriptions")>(), getSubscriptionCounts: vi.fn(), getExpiringSubscriptionCount: vi.fn(), listSubscriptions: vi.fn() }))
const tracked = () => [dashboard.getDashboardStats, dashboard.getOnlineUsersHistory, nodes.listNodes, nodes.getNodesStatsBulk, subscriptions.getSubscriptionCounts, subscriptions.getExpiringSubscriptionCount, subscriptions.listSubscriptions]
let client: QueryClient
function panel(path: string, collapsed = false, visible = true) {
    return <QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><SidebarContextPanel collapsed={collapsed} visible={visible} /></MemoryRouter></QueryClientProvider>
}
beforeEach(() => {
    localStorage.clear(); vi.clearAllMocks()
    client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    vi.mocked(dashboard.getDashboardStats).mockResolvedValue({ success: true, data: { online_users: 12, total_certificates: 5 } } as never)
    vi.mocked(dashboard.getOnlineUsersHistory).mockResolvedValue({ success: true, data: { points: [] } })
    vi.mocked(nodes.listNodes).mockResolvedValue({ success: true, data: [{ id: 1, is_online: true }] } as never)
    vi.mocked(nodes.getNodesStatsBulk).mockResolvedValue({ success: true, data: {} })
    vi.mocked(subscriptions.getSubscriptionCounts).mockResolvedValue({ success: true, data: { all: 500, active: 500 } } as never)
    vi.mocked(subscriptions.getExpiringSubscriptionCount).mockResolvedValue({ success: true, data: { count: 122 } })
})
afterEach(() => { cleanup(); client.clear(); vi.useRealTimers() })
describe("NasNet sidebar query scoping", () => {
    it("does not fetch hidden panels; certificate context only needs dashboard data", async () => {
        const view = render(panel("/certificates", false, false))
        await act(async () => {})
        for (const fn of tracked()) expect(fn).not.toHaveBeenCalled()
        view.rerender(panel("/certificates"))
        await waitFor(() => expect(dashboard.getDashboardStats).toHaveBeenCalledOnce())
        for (const fn of tracked().filter(fn => fn !== dashboard.getDashboardStats)) expect(fn).not.toHaveBeenCalled()
    })
    it("keeps the existing system fallback on settings without fetching portfolio lists", async () => {
        render(panel("/settings"))
        await waitFor(() => expect(dashboard.getDashboardStats).toHaveBeenCalledOnce())
        expect(subscriptions.getSubscriptionCounts).not.toHaveBeenCalled()
        expect(subscriptions.listSubscriptions).not.toHaveBeenCalled()
    })
    it("fetches portfolio counts only while expanded, using counts instead of subscription lists", async () => {
        render(panel("/users"))
        await waitFor(() => expect(nodes.listNodes).toHaveBeenCalledOnce())
        expect(nodes.getNodesStatsBulk).not.toHaveBeenCalled()
        expect(subscriptions.getSubscriptionCounts).not.toHaveBeenCalled()
        fireEvent.click(screen.getByRole("button", { name: "Expand CUSTOMERS details" }))
        await waitFor(() => expect(subscriptions.getExpiringSubscriptionCount).toHaveBeenCalledExactlyOnceWith(7))
        expect(subscriptions.getSubscriptionCounts).toHaveBeenCalledOnce()
        expect(subscriptions.listSubscriptions).not.toHaveBeenCalled()
        fireEvent.click(screen.getByRole("button", { name: "Collapse CUSTOMERS details" }))
        await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.subscriptions }) })
        expect(subscriptions.getExpiringSubscriptionCount).toHaveBeenCalledOnce()
    })
    it("loads rail details on hover and releases their observers on leaving", async () => {
        const view = render(panel("/server", true))
        await waitFor(() => expect(nodes.listNodes).toHaveBeenCalledOnce())
        expect(dashboard.getDashboardStats).not.toHaveBeenCalled()
        expect(nodes.getNodesStatsBulk).not.toHaveBeenCalled()
        fireEvent.mouseEnter(view.container.querySelector(".group")!)
        await waitFor(() => expect(nodes.getNodesStatsBulk).toHaveBeenCalledOnce())
        expect(dashboard.getDashboardStats).toHaveBeenCalledOnce()
        fireEvent.mouseLeave(view.container.querySelector(".group")!)
        await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.nodeStatsBulkAll() }) })
        expect(nodes.getNodesStatsBulk).toHaveBeenCalledOnce()
    })
    it("stops polling after a visible panel is hidden", async () => {
        vi.useFakeTimers()
        const view = render(panel("/certificates"))
        await act(async () => { await vi.advanceTimersByTimeAsync(1) })
        expect(dashboard.getDashboardStats).toHaveBeenCalledOnce()
        view.rerender(panel("/certificates", false, false))
        await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
        expect(dashboard.getDashboardStats).toHaveBeenCalledOnce()
    })
})
