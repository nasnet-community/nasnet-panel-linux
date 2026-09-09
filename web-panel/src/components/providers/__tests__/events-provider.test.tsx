import { act, cleanup, render } from "@testing-library/react"
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { EventsProvider } from "../events-provider"
import { queryKeys } from "@/lib/queries/keys"

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), dismiss: vi.fn(), custom: vi.fn() } }))

class MockEventSource {
    static OPEN = 1
    static instances: MockEventSource[] = []
    readyState = 0
    onopen: (() => void) | null = null
    onerror: (() => void) | null = null
    listeners = new Map<string, (event: { data: string }) => void>()
    close = vi.fn(() => { this.readyState = 2 })
    constructor() { MockEventSource.instances.push(this) }
    addEventListener(type: string, callback: (event: { data: string }) => void) { this.listeners.set(type, callback) }
    open() { this.readyState = MockEventSource.OPEN; this.onopen?.() }
    emit(type: string, payload: unknown) {
        this.listeners.get(type)?.({ data: JSON.stringify({ type, payload }) })
    }
}

let client: QueryClient
let unsubscribe: Array<() => void>

function observe(key: readonly unknown[]) {
    client.setQueryData(key, { value: 1 })
    const fetch = vi.fn(async () => ({ value: 2 }))
    const observer = new QueryObserver(client, { queryKey: key, queryFn: fetch, staleTime: Infinity })
    unsubscribe.push(observer.subscribe(() => {}))
    return fetch
}

beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal("EventSource", MockEventSource)
    MockEventSource.instances = []
    client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    unsubscribe = []
})

afterEach(() => {
    cleanup()
    unsubscribe.forEach((stop) => stop())
    client.clear()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
})

function mount() {
    return render(<QueryClientProvider client={client}><EventsProvider><div /></EventsProvider></QueryClientProvider>)
}

const stats = { node_id: 1, online_users: 4, cpu_percent: 23, total_uplink: 100, total_downlink: 200 }

describe("telemetry event freshness", () => {
    it("patches node caches immediately and refreshes only live dashboard data once per burst", async () => {
        const dashboard = observe(queryKeys.dashboardStats())
        const online = observe(queryKeys.onlineUsers())
        const ips = observe(queryKeys.onlineUsersWithIPs())
        const history = observe(queryKeys.onlineUsersHistory(15))
        client.setQueryData(queryKeys.nodeStats(1), { online_users: 0 })
        client.setQueryData(queryKeys.nodeStatsBulk([1]), { "1": { stats: { online_users: 0 } } })
        mount()
        const stream = MockEventSource.instances[0]
        act(() => { stream.emit("node.stats_updated", stats); stream.emit("node.stats_updated", stats) })
        expect(client.getQueryData(queryKeys.nodeStats(1))).toMatchObject({ online_users: 4, cpu_percent: 23 })
        expect(client.getQueryData(queryKeys.nodeStatsBulk([1]))).toMatchObject({ "1": { stats: { online_users: 4 } } })
        expect(dashboard).not.toHaveBeenCalled()
        await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
        expect(dashboard).toHaveBeenCalledOnce()
        expect(online).toHaveBeenCalledOnce()
        expect(ips).toHaveBeenCalledOnce()
        expect(history).not.toHaveBeenCalled()
    })

    it("does not starve dashboard refresh during continuous node events", async () => {
        const dashboard = observe(queryKeys.dashboardStats())
        mount()
        const stream = MockEventSource.instances[0]
        for (let i = 0; i < 6; i++) {
            act(() => stream.emit("node.stats_updated", stats))
            await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
        }
        expect(dashboard).toHaveBeenCalledTimes(2)
    })

    it("catches up subscription and dashboard state after reconnect", async () => {
        const dashboard = observe(queryKeys.dashboardStats())
        const subs = observe(queryKeys.subscriptionCounts())
        mount()
        act(() => MockEventSource.instances[0].onerror?.())
        await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
        await act(async () => MockEventSource.instances[1].open())
        expect(dashboard).toHaveBeenCalledOnce()
        expect(subs).toHaveBeenCalledOnce()
    })

    it("refreshes live state for server online/offline transitions", async () => {
        const dashboard = observe(queryKeys.dashboardStats())
        mount()
        const stream = MockEventSource.instances[0]
        await act(async () => stream.emit("node.offline", { node_id: 1, node_name: "node" }))
        await act(async () => stream.emit("node.online", { node_id: 1, node_name: "node" }))
        expect(dashboard).toHaveBeenCalledTimes(2)
    })

    it("cancels a scheduled burst refresh on unmount", async () => {
        const dashboard = observe(queryKeys.dashboardStats())
        const view = mount()
        act(() => MockEventSource.instances[0].emit("node.stats_updated", stats))
        view.unmount()
        await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
        expect(dashboard).not.toHaveBeenCalled()
    })
})
