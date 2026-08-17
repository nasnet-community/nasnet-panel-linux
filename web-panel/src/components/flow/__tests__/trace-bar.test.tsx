import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { TraceBar } from "@/components/flow/trace-bar"
import type { TraceView } from "@/lib/types/flow"

const postTrace = vi.fn()

vi.mock("@/lib/api/flow", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/api/flow")>()
    return {
        ...actual,
        postTrace: (...a: unknown[]) => postTrace(...a),
    }
})

function traceView(over: Partial<TraceView> = {}): TraceView {
    return {
        dest: "142.250.185.78",
        resolved_ip: "142.250.185.78",
        source: "lan",
        steps: [
            {
                title: "Classify",
                verdict: "ok",
                evidence: ["142.250.185.78 is not in @ir_v4"],
            },
            {
                title: "Policy walk",
                verdict: "ok",
                evidence: ["pref 150: table 203 → default dev nasnet-wg0"],
            },
        ],
        path_nodes: ["src-lan", "mark-foreign", "table-203"],
        path_edges: ["e-lan-for", "e-for-203"],
        final_verdict: "delivered-vpn",
        ...over,
    }
}

function renderBar(initialEntry = "/network/flow") {
    const onResult = vi.fn()
    const onClear = vi.fn()
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
        <QueryClientProvider client={qc}>
            <MemoryRouter initialEntries={[initialEntry]}>
                <TraceBar onResult={onResult} onClear={onClear} />
            </MemoryRouter>
        </QueryClientProvider>,
    )
    return { onResult, onClear }
}

beforeEach(() => {
    postTrace.mockReset()
})

describe("TraceBar", () => {
    it("posts the destination and the source kind", async () => {
        postTrace.mockResolvedValue({ success: true, data: traceView() })
        const { onResult } = renderBar()

        await userEvent.type(screen.getByLabelText("Trace destination"), "142.250.185.78")
        await userEvent.click(screen.getByRole("button", { name: /trace/i }))

        expect(postTrace).toHaveBeenCalledWith({ dest: "142.250.185.78", source: "lan" })
        expect(await screen.findByText(/Delivered through the VPN/i)).toBeInTheDocument()
        expect(onResult).toHaveBeenCalled()
    })

    it("shows each step's kernel evidence", async () => {
        postTrace.mockResolvedValue({ success: true, data: traceView() })
        renderBar()

        await userEvent.type(screen.getByLabelText("Trace destination"), "1.1.1.1")
        await userEvent.click(screen.getByRole("button", { name: /trace/i }))

        expect(await screen.findByText(/pref 150: table 203/)).toBeInTheDocument()
        expect(screen.getByText(/is not in @ir_v4/)).toBeInTheDocument()
    })

    it("names the drop when nothing routes", async () => {
        postTrace.mockResolvedValue({
            success: true,
            data: traceView({
                final_verdict: "dropped",
                steps: [
                    {
                        title: "Dropped",
                        verdict: "drop",
                        evidence: ["the blackhole rule at pref 199 has the last word"],
                    },
                ],
            }),
        })
        renderBar()

        await userEvent.type(screen.getByLabelText("Trace destination"), "1.1.1.1")
        await userEvent.click(screen.getByRole("button", { name: /trace/i }))

        expect(await screen.findByText(/never leaves the box/i)).toBeInTheDocument()
        expect(screen.getByText(/blackhole rule at pref 199/)).toBeInTheDocument()
    })

    it("surfaces a rejected input instead of swallowing it", async () => {
        postTrace.mockResolvedValue({
            success: false,
            error: "trace input: the destination must be an IP address or a hostname",
        })
        renderBar()

        await userEvent.type(screen.getByLabelText("Trace destination"), "nonsense")
        await userEvent.click(screen.getByRole("button", { name: /trace/i }))

        expect(await screen.findByText(/must be an IP address or a hostname/i)).toBeInTheDocument()
    })

    it("runs a shared trace from the URL without anyone pressing anything", async () => {
        postTrace.mockResolvedValue({ success: true, data: traceView() })
        renderBar("/network/flow?trace=142.250.185.78&source=lan")

        expect(await screen.findByText(/Delivered through the VPN/i)).toBeInTheDocument()
        expect(postTrace).toHaveBeenCalledWith({ dest: "142.250.185.78", source: "lan" })
    })

    it("clears the result on request", async () => {
        postTrace.mockResolvedValue({ success: true, data: traceView() })
        const { onClear } = renderBar()

        await userEvent.type(screen.getByLabelText("Trace destination"), "1.1.1.1")
        await userEvent.click(screen.getByRole("button", { name: /trace/i }))
        await screen.findByText(/Delivered through the VPN/i)

        await userEvent.click(screen.getByRole("button", { name: /clear/i }))
        expect(onClear).toHaveBeenCalled()
        expect(screen.queryByText(/Delivered through the VPN/i)).not.toBeInTheDocument()
    })
})
