import { describe, expect, it, vi, beforeEach } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { HealthStrip } from "@/components/router/health-strip"
import type { RouterHealth, TunnelHealth, UplinkHealth } from "@/lib/types/health"
import type { NetworkInterfaceView } from "@/lib/types/network"

const mutate = vi.fn()
const toastSuccess = vi.fn()
const toastError = vi.fn()
const setInterfaceLabel = vi.fn()

vi.mock("sonner", () => ({
    toast: {
        success: (m: string) => toastSuccess(m),
        error: (m: string) => toastError(m),
    },
}))

vi.mock("@/lib/queries/use-router-health", () => ({
    useRouterHealth: () => mockQuery,
    useSetUplinkForce: () => ({ mutate, isPending: false }),
}))

vi.mock("@/lib/api/network", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/api/network")>()
    return {
        ...actual,
        setInterfaceLabel: (...a: unknown[]) => setInterfaceLabel(...a),
    }
})

let mockQuery: { data?: RouterHealth; isLoading: boolean; isError: boolean }

// The strip asks before cutting an uplink, so it needs the same provider the
// dashboard layout wraps every page in.
function renderStrip(interfaces: NetworkInterfaceView[] = []) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <QueryClientProvider client={qc}>
            <ConfirmDialogProvider>
                <HealthStrip interfaces={interfaces} />
            </ConfirmDialogProvider>
        </QueryClientProvider>,
    )
}

function iface(over: Partial<NetworkInterfaceView>): NetworkInterfaceView {
    return {
        id: 1,
        if_name: "eth0",
        phy: "",
        perm_mac: "52:54:00:12:34:02",
        id_path: "",
        key: "52:54:00:12:34:02",
        key_kind: "permaddr",
        source: "eth_pci",
        confidence: 100,
        driver: "virtio_net",
        carrier: true,
        oper_state: "up",
        speed_mbit: 1000,
        mtu: 1500,
        usb_speed_mbit: 0,
        assignable: true,
        addrs: [],
        role: "wan",
        slot: "domestic",
        label: "",
        present: true,
        healthy: true,
        ...over,
    }
}

function card(ifName: string): HTMLElement {
    return document.querySelector(`[data-uplink="${ifName}"]`) as HTMLElement
}

function uplink(over: Partial<UplinkHealth>): UplinkHealth {
    return {
        slot: "domestic",
        if_name: "eth0",
        carrier: "up",
        gateway: "up",
        internet: "up",
        verdict: "up",
        force_state: "",
        degraded: false,
        loss_pct: 0,
        median_rtt_ms: 40,
        targets: [{ address: "1.1.1.1:443", proto: "tcp", ok: true, rtt_ms: 40 }],
        history: [
            { unix: 1, ok_ratio: 1, rtt_ms: 40 },
            { unix: 2, ok_ratio: 1, rtt_ms: 42 },
        ],
        ...over,
    }
}

function health(over: Partial<RouterHealth>): RouterHealth {
    return {
        generated_unix: 100,
        failover_active: false,
        uplinks: [uplink({}), uplink({ slot: "secondary", if_name: "eth1" })],
        vpn: null,
        ...over,
    }
}

beforeEach(() => {
    mutate.mockClear()
    setInterfaceLabel.mockReset()
    mockQuery = { data: health({}), isLoading: false, isError: false }
})

// The card is now the one place an uplink is described: health, name, address
// and the rename all live together, so nothing repeats it further down the page.
describe("the uplink card as the uplink's own card", () => {
    const NAMED = iface({ if_name: "eth0", label: "Main", addrs: ["10.0.2.15/24"] })
    const BARE = iface({ id: 2, if_name: "eth1", key: "k1", slot: "secondary" })

    it("titles the card with the port's name and keeps the interface name beneath", () => {
        renderStrip([NAMED, BARE])
        const c = within(card("eth0"))
        expect(c.getByText("Main")).toBeInTheDocument()
        expect(c.getByText(/eth0 · domestic WAN/)).toBeInTheDocument()
    })

    // An unnamed port falls back to its interface name, and repeating it in the
    // subtitle would say the same thing twice.
    it("does not repeat the interface name when the port has no name", () => {
        renderStrip([NAMED, BARE])
        const c = within(card("eth1"))
        expect(c.getByText("eth1")).toBeInTheDocument()
        expect(c.getByText("secondary WAN")).toBeInTheDocument()
        expect(c.queryByText(/eth1 ·/)).toBeNull()
    })

    it("shows the address the uplink answers on", () => {
        renderStrip([NAMED, BARE])
        expect(within(card("eth0")).getByText("10.0.2.15/24")).toBeInTheDocument()
        expect(within(card("eth1")).getByText("no address")).toBeInTheDocument()
    })

    it("renames the port from its own card", async () => {
        setInterfaceLabel.mockResolvedValue({ success: true, data: null })
        renderStrip([NAMED, BARE])
        const c = within(card("eth1"))
        await userEvent.click(c.getByLabelText("Name eth1"))
        await userEvent.type(c.getByLabelText("Name for eth1"), "LTE backup")
        await userEvent.click(c.getByLabelText("Save the name"))
        await waitFor(() => expect(setInterfaceLabel).toHaveBeenCalledWith("k1", "LTE backup"))
    })

    // Health arrives from the probe loop, interfaces from the state query; a
    // card must still render when the two disagree about what exists.
    it("still renders an uplink no interface row matches", () => {
        renderStrip([])
        expect(within(card("eth0")).getByText("eth0")).toBeInTheDocument()
    })
})

describe("HealthStrip", () => {
    it("renders one card per uplink with its verdict", () => {
        renderStrip()
        expect(screen.getByText("eth0")).toBeInTheDocument()
        expect(screen.getByText("eth1")).toBeInTheDocument()
        expect(document.querySelectorAll('[data-verdict="up"]').length).toBe(2)
    })

    it("shows the loss figure on a degraded uplink", () => {
        mockQuery.data = health({
            uplinks: [uplink({ verdict: "degraded", degraded: true, loss_pct: 25 })],
        })
        renderStrip()
        expect(screen.getByText(/25% loss/)).toBeInTheDocument()
        expect(document.querySelector('[data-verdict="degraded"]')).not.toBeNull()
    })

    it("marks the internet segment down while the gateway stays ok", () => {
        mockQuery.data = health({
            uplinks: [uplink({ verdict: "no-internet", internet: "down" })],
        })
        renderStrip()
        const segs = document.querySelectorAll("[data-layer]")
        const byLayer: Record<string, string> = {}
        segs.forEach((s) => {
            byLayer[s.getAttribute("data-layer")!] = s.getAttribute("data-layer-status")!
        })
        expect(byLayer["gateway"]).toBe("up")
        expect(byLayer["internet"]).toBe("down")
    })

    it("announces an active failover", () => {
        mockQuery.data = health({ failover_active: true })
        renderStrip()
        expect(screen.getByText(/riding the VPN/i)).toBeInTheDocument()
    })

    it("fires the force mutation with the chosen state", async () => {
        mockQuery.data = health({ uplinks: [uplink({})] })
        renderStrip()
        await userEvent.click(screen.getByRole("button", { name: /force down/i }))
        await userEvent.click(await screen.findByRole("button", { name: "Force it down" }))
        await waitFor(() =>
            expect(mutate).toHaveBeenCalledWith(
                { ifName: "eth0", state: "down" },
                expect.anything(),
            ),
        )
    })

    // Per-member detail belongs to the VPN tab now.
    it("says what the pool is doing in one line and counts an ejected member", () => {
        const member = (over: Partial<TunnelHealth>): TunnelHealth => ({
            profile_id: 1,
            name: "frankfurt",
            if_name: "nasnet-wg0",
            position: 0,
            in_pool: true,
            verdict: "up",
            degraded: false,
            loss_pct: 2,
            median_rtt_ms: 80,
            targets: [],
            history: [],
            ...over,
        })
        mockQuery.data = health({
            vpn: {
                present: true,
                strategy: "spread" as const,
                loss_pct: 2,
                median_rtt_ms: 80,
                pool_history: [
                    { unix: 1, ok_ratio: 1, rtt_ms: 80 },
                    { unix: 2, ok_ratio: 1, rtt_ms: 82 },
                ],
                tunnels: [
                    member({}),
                    member({
                        profile_id: 2,
                        name: "vienna",
                        if_name: "nasnet-wg1",
                        in_pool: false,
                        verdict: "no-internet",
                    }),
                ],
            },
        })
        renderStrip()
        expect(screen.getByText("VPN")).toBeInTheDocument()
        expect(screen.getByText("1 of 2 sharing the traffic")).toBeInTheDocument()
        // One member was ejected, so the badge counts instead of claiming green.
        expect(document.querySelector('[data-pool-state="1 of 2"]')).not.toBeNull()
        // No per-member dots: the tab owns that, and this card repeated it.
        expect(document.querySelectorAll("[data-member]").length).toBe(0)
    })

    it("refuses to call a routed but silent pool online", () => {
        // The best tier's last member is never ejected, so in_pool stays true
        // while it answers nothing.
        mockQuery.data = health({
            vpn: {
                present: true,
                strategy: "spread" as const,
                loss_pct: 100,
                median_rtt_ms: 0,
                pool_history: [],
                tunnels: [
                    {
                        profile_id: 1,
                        name: "frankfurt",
                        if_name: "nasnet-wg0",
                        position: 0,
                        in_pool: true,
                        verdict: "no-internet",
                        degraded: true,
                        loss_pct: 100,
                        median_rtt_ms: 0,
                        targets: [],
                        history: [],
                    },
                ],
            },
        })
        renderStrip()
        expect(document.querySelector('[data-pool-state="down"]')).not.toBeNull()
        expect(screen.getByText("nothing carrying")).toBeInTheDocument()
    })

    it("says waiting while no tunnel has answered yet", () => {
        mockQuery.data = health({
            vpn: {
                present: true,
                strategy: "spread" as const,
                loss_pct: 0,
                median_rtt_ms: 0,
                pool_history: [],
                tunnels: [
                    {
                        profile_id: 1,
                        name: "frankfurt",
                        if_name: "nasnet-wg0",
                        position: 0,
                        in_pool: true,
                        verdict: "",
                        degraded: false,
                        loss_pct: 0,
                        median_rtt_ms: 0,
                        targets: [],
                        history: [],
                    },
                ],
            },
        })
        renderStrip()
        expect(document.querySelector('[data-pool-state="waiting"]')).not.toBeNull()
    })

    it("stays quiet on loading and error", () => {
        mockQuery = { isLoading: true, isError: false }
        const { unmount } = renderStrip()
        expect(screen.queryByText("eth0")).toBeNull()
        unmount()
        mockQuery = { isLoading: false, isError: true }
        renderStrip()
        expect(screen.getByText(/health is unavailable/i)).toBeInTheDocument()
    })

    // The override is a manual act with real consequences — it has to say back
    // what it did, or nothing on screen changes until the next 5s probe.
    describe("says what the override did", () => {
        beforeEach(() => {
            toastSuccess.mockReset()
            toastError.mockReset()
            mutate.mockReset()
            // Drive the caller's own onSuccess, the way the real mutation does.
            mutate.mockImplementation((_vars, opts) => opts?.onSuccess?.())
            mockQuery = { data: health({}), isLoading: false, isError: false }
        })

        it("confirms forcing an uplink down", async () => {
            renderStrip()
            await userEvent.click(screen.getAllByText("force down")[0])
            await userEvent.click(await screen.findByRole("button", { name: "Force it down" }))
            await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("eth0 forced down"))
        })

        // Cutting an uplink is the one override that can take the operator off
        // the box, so it asks first and a refusal must change nothing.
        it("asks before forcing down, and backing out does nothing", async () => {
            renderStrip()
            await userEvent.click(screen.getAllByText("force down")[0])
            await userEvent.click(await screen.findByRole("button", { name: "Cancel" }))
            expect(mutate).not.toHaveBeenCalled()
            expect(toastSuccess).not.toHaveBeenCalled()
        })

        // The domestic uplink is the one carrying the panel session.
        it("names the real consequence for the domestic uplink", async () => {
            renderStrip()
            await userEvent.click(screen.getAllByText("force down")[0])
            expect(await screen.findByText(/lose access to this panel/i)).toBeInTheDocument()
        })

        it("does not ask for force up or auto", async () => {
            renderStrip()
            await userEvent.click(screen.getAllByText("force up")[0])
            expect(toastSuccess).toHaveBeenCalledWith("eth0 forced up")
            await userEvent.click(screen.getAllByText("auto")[0])
            expect(toastSuccess).toHaveBeenCalledWith("eth0 back on auto")
        })

        it("says so when the override fails", async () => {
            mutate.mockImplementation((_vars, opts) =>
                opts?.onError?.(new Error("no such uplink")),
            )
            renderStrip()
            await userEvent.click(screen.getAllByText("force down")[0])
            await userEvent.click(await screen.findByRole("button", { name: "Force it down" }))
            await waitFor(() => expect(toastError).toHaveBeenCalledWith("no such uplink"))
        })
    })
})
