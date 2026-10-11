import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { checkMtprotoDisguise } from "@/lib/api/mtproto"
import { toast } from "@/lib/toast"
import { useCheckMtprotoDisguise } from "../use-mtproto"

vi.mock("@/lib/api/mtproto", () => ({ checkMtprotoDisguise: vi.fn() }))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))
afterEach(cleanup)
beforeEach(() => vi.clearAllMocks())

function wrapper({ children }: { children: ReactNode }) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

const result = { tls13: true, x25519: true, h2: true, cert_ok: true, latency_ms: 30 }

describe("cover site check", () => {
    it("toasts a site that works", async () => {
        vi.mocked(checkMtprotoDisguise).mockResolvedValue({ success: true, data: { ...result, ok: true } })
        const { result: hook } = renderHook(() => useCheckMtprotoDisguise(4), { wrapper })
        act(() => hook.current.mutate("www.example.ir"))
        await waitFor(() => expect(toast.success).toHaveBeenCalled())
        expect(vi.mocked(toast.success).mock.calls[0][0]).toMatch(/www\.example\.ir/)
    })

    it("toasts a site that does not work, with the reason", async () => {
        vi.mocked(checkMtprotoDisguise).mockResolvedValue({ success: true, data: { ...result, ok: false, h2: false, err: "no h2" } })
        const { result: hook } = renderHook(() => useCheckMtprotoDisguise(4), { wrapper })
        act(() => hook.current.mutate("www.example.ir"))
        await waitFor(() => expect(toast.warning).toHaveBeenCalled())
        expect(vi.mocked(toast.warning).mock.calls[0][1]).toEqual(expect.objectContaining({ description: "no h2" }))
        expect(toast.success).not.toHaveBeenCalled()
    })
})
