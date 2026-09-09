import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { api } from "@/lib/api"
import { useSubsExpiringWithin } from "../use-subscriptions"

vi.mock("@/lib/api", () => ({ api: { get: vi.fn() } }))
afterEach(cleanup)
beforeEach(() => vi.clearAllMocks())

function createWrapper() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    return function Wrapper({ children }: { children: ReactNode }) {
        return <QueryClientProvider client={client}>{children}</QueryClientProvider>
    }
}

describe("sidebar expiry count query", () => {
    it("uses one aggregate endpoint and returns counts above the old 1000-row cap", async () => {
        vi.mocked(api.get).mockResolvedValue({ success: true, data: { count: 1501 } })
        const { result } = renderHook(() => useSubsExpiringWithin(7), { wrapper: createWrapper() })
        await waitFor(() => expect(result.current.count).toBe(1501))
        expect(api.get).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/subscriptions/expiring-soon/count?days=7")
        expect(vi.mocked(api.get).mock.calls.some(([url]) => url.includes("per_page") || url.includes("status=active"))).toBe(false)
    })

    it("preserves route gating and keys each expiry window separately", async () => {
        vi.mocked(api.get).mockResolvedValue({ success: true, data: { count: 12 } })
        const { result, rerender } = renderHook(({ enabled, days }) => useSubsExpiringWithin(days, enabled), { initialProps: { enabled: false, days: 7 }, wrapper: createWrapper() })
        expect(api.get).not.toHaveBeenCalled()
        expect(result.current.isLoading).toBe(false)
        rerender({ enabled: true, days: 7 })
        await waitFor(() => expect(result.current.count).toBe(12))
        rerender({ enabled: true, days: 14 })
        await waitFor(() => expect(api.get).toHaveBeenCalledWith("/api/v1/admin/subscriptions/expiring-soon/count?days=14"))
        expect(api.get).toHaveBeenCalledTimes(2)
    })
})
