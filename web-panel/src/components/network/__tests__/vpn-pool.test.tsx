import { describe, expect, it, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { VpnPool } from "@/components/network/vpn-pool"
import { poolRowCondition } from "@/lib/vpn-labels"
import type { PoolStrategy, TunnelStatus, VPNProfile, VPNUplink } from "@/lib/types/network"
import type { TunnelHealth } from "@/lib/types/health"

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/lib/api/network", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/api/network")>()),
    setVPNPoolOrder: vi.fn().mockResolvedValue({ success: true, data: null }),
    setVPNProfileTransport: vi.fn().mockResolvedValue({ success: true, data: null }),
    deleteVPNProfile: vi.fn(),
}))

function profile(over: Partial<VPNProfile> = {}): VPNProfile {
    return {
        id: 1,
        name: "frankfurt",
        type: "wireguard",
        enabled: true,
        priority: 0,
        weight: 1,
        wg_slot: 0,
        public_key: "pub",
        created_at: "",
        updated_at: "",
        config: {
            private_key: "priv",
            address: "10.66.0.2/32",
            peer: { public_key: "peer", allowed_ips: ["0.0.0.0/0"], endpoint: "1.2.3.4:51820" },
        },
        ...over,
    }
}

function tunnel(over: Partial<TunnelStatus> = {}): TunnelStatus {
    return {
        profile_id: 1,
        name: "frankfurt",
        if_name: "nasnet-wg0",
        position: 0,
        connected: true,
        handshake_age_seconds: 30,
        rx_bytes: 2048,
        tx_bytes: 1024,
        mtu: 1420,
        keepalive_seconds: 25,
        in_pool: true,
        ...over,
    }
}

function health(over: Partial<TunnelHealth> = {}): TunnelHealth {
    return {
        profile_id: 1,
        name: "frankfurt",
        if_name: "nasnet-wg0",
        position: 0,
        in_pool: true,
        verdict: "up",
        degraded: false,
        loss_pct: 0,
        median_rtt_ms: 5,
        targets: [],
        history: [],
        ...over,
    }
}

const wan: VPNUplink = { slot: "secondary", if_name: "dish0", label: "Dish", key: "k-dish0", up: true }

function show(over: {
    profiles?: VPNProfile[]
    tunnels?: TunnelStatus[]
    health?: TunnelHealth[]
    strategy?: PoolStrategy
    carrier?: string
} = {}) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <QueryClientProvider client={qc}>
            <ConfirmDialogProvider>
                <VpnPool
                    profiles={over.profiles ?? [profile()]}
                    loading={false}
                    tunnels={over.tunnels ?? [tunnel()]}
                    health={over.health ?? [health()]}
                    poolLossPct={0}
                    poolRTTms={5}
                    uplinks={[wan]}
                    strategy={over.strategy ?? "spread"}
                    carrier={over.carrier}
                    armed={false}
                    busy={false}
                    onEnable={() => {}}
                    onDisable={() => {}}
                />
            </ConfirmDialogProvider>
        </QueryClientProvider>,
    )
}

const second = {
    profile: profile({ id: 2, name: "berlin", wg_slot: 1, priority: 1 }),
    tunnel: tunnel({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1, in_pool: false }),
    health: health({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1, in_pool: false, median_rtt_ms: 83 }),
}

beforeEach(() => vi.clearAllMocks())

describe("the pool states its condition once", () => {
    it("names the carrier in the header instead of a second card", () => {
        show({
            profiles: [profile(), second.profile],
            tunnels: [tunnel(), second.tunnel],
            health: [health(), second.health],
            strategy: "fastest",
            carrier: "nasnet-wg0",
        })
        expect(screen.getByText("frankfurt is carrying, fastest at 5 ms")).toBeInTheDocument()
        // One chip per row, and no duplicate of it anywhere else.
        expect(screen.getAllByText("carrying")).toHaveLength(1)
        expect(screen.getAllByText("standby")).toHaveLength(1)
    })

    it("counts the sharers under a spread", () => {
        show({
            profiles: [profile(), second.profile],
            tunnels: [tunnel(), { ...second.tunnel, in_pool: true }],
            health: [health(), { ...second.health, in_pool: true }],
        })
        expect(screen.getByText("2 of 2 sharing the traffic")).toBeInTheDocument()
    })

    it("calls a silent VPN not answering, whatever the routes say", () => {
        show({
            tunnels: [tunnel({ in_pool: true, connected: false })],
            health: [health({ verdict: "no-internet", loss_pct: 100, median_rtt_ms: 0 })],
        })
        expect(screen.getByText("not answering")).toBeInTheDocument()
        expect(screen.getByText("No VPN is carrying traffic")).toBeInTheDocument()
    })

    // The state helper is what stops the row and the header disagreeing.
    it("derives one condition per profile", () => {
        expect(poolRowCondition(profile({ enabled: false }), undefined, undefined, false).state)
            .toBe("off")
        expect(poolRowCondition(profile(), tunnel(), health({ verdict: "" }), false).state)
            .toBe("checking")
        expect(poolRowCondition(profile(), tunnel(), health(), false).state).toBe("carrying")
        expect(poolRowCondition(profile(), tunnel({ in_pool: false }), health(), true).state)
            .toBe("next-up")
        expect(poolRowCondition(profile(), tunnel({ in_pool: false }), health(), false).state)
            .toBe("standby")
        // Degraded is a tone, not a state: it is still carrying.
        const d = poolRowCondition(profile(), tunnel(), health({ degraded: true }), false)
        expect(d).toEqual({ state: "carrying", degraded: true })
    })
})

describe("the row's details", () => {
    it("keeps handshake, traffic and the destructive actions one click down", async () => {
        show()
        expect(screen.queryByRole("button", { name: "Delete" })).toBeNull()
        await userEvent.click(screen.getByLabelText("Details for frankfurt"))
        expect(screen.getByText("Last handshake")).toBeInTheDocument()
        expect(screen.getByText("Traffic")).toBeInTheDocument()
        // A running VPN cannot be edited or deleted, and the row says why.
        expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled()
        expect(screen.getByText(/Turn it off first/)).toBeInTheDocument()
    })

    it("shows the reason a VPN will not come up", async () => {
        show({ tunnels: [tunnel({ last_error: "resolve the endpoint: no such host" })] })
        await userEvent.click(screen.getByLabelText("Details for frankfurt"))
        expect(screen.getByText("resolve the endpoint: no such host")).toBeInTheDocument()
    })

    it("opens one row at a time", async () => {
        show({
            profiles: [profile(), second.profile],
            tunnels: [tunnel(), second.tunnel],
            health: [health(), second.health],
        })
        await userEvent.click(screen.getByLabelText("Details for frankfurt"))
        await userEvent.click(screen.getByLabelText("Details for berlin"))
        expect(screen.getByLabelText("Details for frankfurt")).toHaveAttribute(
            "aria-expanded",
            "false",
        )
        expect(screen.getByLabelText("Details for berlin")).toHaveAttribute("aria-expanded", "true")
    })
})

describe("VPNs that are not in use", () => {
    it("groups them away from the pool and counts them", () => {
        show({
            profiles: [profile(), profile({ id: 2, name: "berlin", enabled: false, wg_slot: null })],
        })
        expect(screen.getByText("Not in use (1)")).toBeInTheDocument()
    })

    // An empty pool should still show what there is to turn on.
    it("opens itself when nothing is running", () => {
        show({
            profiles: [profile({ enabled: false, wg_slot: null })],
            tunnels: [],
            health: [],
        })
        expect(screen.getByText("No VPN is turned on")).toBeInTheDocument()
        expect(screen.getByText("Not in use (1)").closest("details")).toHaveAttribute("open")
        // Off VPNs can be edited and deleted; running ones cannot.
        expect(screen.getByLabelText("Turn frankfurt on")).toBeEnabled()
    })

    it("invites a first VPN when there is nothing at all", () => {
        show({ profiles: [], tunnels: [], health: [] })
        expect(screen.getByText("No VPN yet")).toBeInTheDocument()
        expect(screen.getByText("Add your first VPN")).toBeInTheDocument()
    })
})
