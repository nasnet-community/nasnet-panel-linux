import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { VpnTab } from "@/pages/router/vpn-tab"
import { detectFormat, formatBytes, handshakeLabel, handshakeShort } from "@/lib/vpn-labels"
import type { TunnelStatus, VPNPoolStatus, VPNProfile, VPNUplink } from "@/lib/types/network"
import type { RouterHealth, TunnelHealth } from "@/lib/types/health"

const getVPNProfiles = vi.fn()
const getVPNStatus = vi.fn()
const getRouterHealth = vi.fn()
const deleteVPNProfile = vi.fn()
const setVPNPoolStrategy = vi.fn()
const setVPNPoolOrder = vi.fn()
const setVPNProfileTransport = vi.fn()
const toastError = vi.fn()
const toastSuccess = vi.fn()
vi.mock("sonner", () => ({
    toast: {
        error: (m: string) => toastError(m),
        success: (m: string) => toastSuccess(m),
    },
}))

const enableVPNProfile = vi.fn()
const disableVPNProfile = vi.fn()

vi.mock("@/lib/api/network", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/api/network")>()
    return {
        ...actual,
        getVPNProfiles: (...a: unknown[]) => getVPNProfiles(...a),
        getVPNStatus: (...a: unknown[]) => getVPNStatus(...a),
        getRouterHealth: (...a: unknown[]) => getRouterHealth(...a),
        deleteVPNProfile: (...a: unknown[]) => deleteVPNProfile(...a),
        setVPNPoolStrategy: (...a: unknown[]) => setVPNPoolStrategy(...a),
        setVPNPoolOrder: (...a: unknown[]) => setVPNPoolOrder(...a),
        setVPNProfileTransport: (...a: unknown[]) => setVPNProfileTransport(...a),
        enableVPNProfile: (...a: unknown[]) => enableVPNProfile(...a),
        disableVPNProfile: (...a: unknown[]) => disableVPNProfile(...a),
        createVPNProfile: vi.fn(),
        updateVPNProfile: vi.fn(),
        parseVPNInput: vi.fn(),
        generateVPNKeypair: vi.fn(),
    }
})

function profile(over: Partial<VPNProfile> = {}): VPNProfile {
    return {
        id: 1,
        name: "frankfurt",
        type: "wireguard",
        enabled: false,
        priority: 0,
        weight: 1,
        wg_slot: null,
        public_key: "pub",
        created_at: "",
        updated_at: "",
        config: {
            private_key: "priv",
            address: "10.66.0.2/32",
            peer: {
                public_key: "peerpub",
                allowed_ips: ["0.0.0.0/0"],
                endpoint: "vpn.example.com:51820",
            },
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
        handshake_age_seconds: 12,
        rx_bytes: 2048,
        tx_bytes: 1024,
        endpoint: "185.65.135.1:51820",
        mtu: 1420,
        keepalive_seconds: 25,
        in_pool: true,
        ...over,
    }
}

function wan(over: Partial<VPNUplink> = {}): VPNUplink {
    return { slot: "secondary", if_name: "dish0", label: "Dish", key: "k-dish0", up: true, ...over }
}

function poolStatus(tunnels: TunnelStatus[], over: Partial<VPNPoolStatus> = {}): VPNPoolStatus {
    return { tunnels, uplinks: [wan()], kill_switch: true, strategy: "spread", ...over }
}

function tunnelHealth(over: Partial<TunnelHealth> = {}): TunnelHealth {
    return {
        profile_id: 1,
        name: "frankfurt",
        if_name: "nasnet-wg0",
        position: 0,
        in_pool: true,
        verdict: "up",
        degraded: false,
        loss_pct: 3,
        median_rtt_ms: 82,
        targets: [],
        history: [],
        ...over,
    }
}

function health(tunnels: TunnelHealth[]): RouterHealth {
    return {
        generated_unix: 0,
        failover_active: false,
        uplinks: [],
        vpn:
            tunnels.length === 0
                ? null
                : {
                      present: true,
                      strategy: "spread" as const,
                      loss_pct: 3,
                      median_rtt_ms: 82,
                      pool_history: [],
                      tunnels,
                  },
    }
}

function renderIt(profiles: VPNProfile[], st: VPNPoolStatus, h?: RouterHealth) {
    getVPNProfiles.mockResolvedValue({ success: true, data: profiles })
    getVPNStatus.mockResolvedValue({ success: true, data: st })
    getRouterHealth.mockResolvedValue({ success: true, data: h ?? health([]) })
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    // The same providers the dashboard layout supplies in the real app.
    return render(
        <QueryClientProvider client={qc}>
            <ConfirmDialogProvider>
                <VpnTab armed={false} onApplied={() => {}} />
            </ConfirmDialogProvider>
        </QueryClientProvider>,
    )
}

describe("VpnTab", () => {
    beforeEach(() => {
        getVPNProfiles.mockReset()
        getVPNStatus.mockReset()
        getRouterHealth.mockReset()
        deleteVPNProfile.mockReset()
        setVPNPoolStrategy.mockReset()
        setVPNPoolOrder.mockReset()
        setVPNProfileTransport.mockReset()
        enableVPNProfile.mockReset()
        disableVPNProfile.mockReset()
    })

    // Every popover and dialog needs its provider above it. Without one the
    // whole tab throws, which no amount of pure-helper testing would catch.
    it("renders a carrying pool without a provider error", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel()]),
            health([tunnelHealth()]),
        )
        expect(await screen.findByText("1 of 1 sharing the traffic")).toBeInTheDocument()
        expect(screen.getAllByText("frankfurt").length).toBeGreaterThan(0)
    })

    // The kill switch is not a setting, so the UI has to state what it is doing.
    it("says why nothing is getting out when the pool is empty", async () => {
        renderIt([profile()], poolStatus([]))
        expect(await screen.findByText("No VPN is turned on")).toBeInTheDocument()
        expect(screen.getByText(/until a VPN is turned on/)).toBeInTheDocument()
    })

    // A tunnel that stops looks identical to a working one from the link's side.
    it("warns when no member of the pool is answering", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel({ connected: false, handshake_age_seconds: 900 })]),
        )
        expect(await screen.findByText(/VPNs are answering/)).toBeInTheDocument()
    })

    it("invites a first VPN when there are none", async () => {
        renderIt([], poolStatus([]))
        expect(await screen.findByText("No VPN yet")).toBeInTheDocument()
    })

    // Two intents that must not collapse into one click: leave the pool, and
    // remove the config. Deleting a live member leaves nothing to turn it off.
    it("refuses to edit or delete a pool member", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel()]),
            health([tunnelHealth()]),
        )
        await userEvent.click(await screen.findByLabelText("Details for frankfurt"))
        expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled()
        expect(screen.getByRole("button", { name: "Edit" })).toBeDisabled()
        expect(screen.getByLabelText("Turn frankfurt off")).toBeEnabled()
    })

    it("shows each member's health and whether it is carrying", async () => {
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus([
                tunnel(),
                tunnel({
                    profile_id: 2,
                    name: "berlin",
                    if_name: "nasnet-wg1",
                    position: 1,
                    in_pool: false,
                }),
            ]),
            health([
                tunnelHealth(),
                tunnelHealth({
                    profile_id: 2,
                    name: "berlin",
                    if_name: "nasnet-wg1",
                    position: 1,
                    in_pool: false,
                    verdict: "up",
                }),
            ]),
        )
        expect(await screen.findByText("carrying")).toBeInTheDocument()
        expect(screen.getByText("standby")).toBeInTheDocument()
        // One condition per row: no second chip repeating it.
        expect(screen.queryByText("online · standby")).toBeNull()
        expect(screen.getAllByText("82 ms").length).toBe(2)
        expect(screen.getByLabelText("Turn berlin off")).toBeEnabled()
    })

    // Tier and weight were numbers only the router understood.
    it("never asks for a tier or a weight", async () => {
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus([tunnel(), tunnel({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1 })]),
        )
        await screen.findByRole("button", { name: /add a vpn/i })
        expect(screen.queryByText(/tier/i)).toBeNull()
        expect(screen.queryByText(/weight/i)).toBeNull()
        expect(screen.queryByLabelText(/Change frankfurt tier/)).toBeNull()
    })

    it("sends the chosen strategy and nothing else", async () => {
        setVPNPoolStrategy.mockResolvedValue({ success: true, data: null })
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus([tunnel(), tunnel({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1 })]),
        )
        await screen.findByRole("button", { name: /add a vpn/i })
        await userEvent.click(screen.getByRole("button", { name: "Fastest first" }))
        expect(setVPNPoolStrategy).toHaveBeenCalledWith("fastest")
    })

    // A pool of one has nothing to choose between.
    it("hides the strategy control until there are two tunnels", async () => {
        renderIt([profile({ enabled: true, wg_slot: 0 })], poolStatus([tunnel()]))
        await screen.findByRole("button", { name: /add a vpn/i })
        expect(screen.queryByRole("group", { name: /How traffic uses/ })).toBeNull()
    })

    // The last member's removal blackholes foreign traffic; the dialog says so.
    it("warns hardest when removing the last member", async () => {
        disableVPNProfile.mockResolvedValue({ success: true, data: {} })
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel()]),
            health([tunnelHealth()]),
        )
        await userEvent.click(await screen.findByLabelText("Turn frankfurt off"))
        expect(await screen.findByText(/This is the last VPN/)).toBeInTheDocument()
    })

    // A rejected enable used to close the dialog and say nothing at all.
    it("says so when a change is refused", async () => {
        enableVPNProfile.mockRejectedValue(new Error("wg0 refused to come up"))
        renderIt([profile({ wg_slot: null })], poolStatus([]))
        await userEvent.click(await screen.findByLabelText("Turn frankfurt on"))
        await userEvent.click(await screen.findByRole("button", { name: /turn it on/i }))
        await waitFor(() => expect(toastError).toHaveBeenCalledWith("wg0 refused to come up"))
    })

    // The order is only a control where it decides something.
    it("offers the order only under the chain", async () => {
        const two = [
            profile({ enabled: true, wg_slot: 0 }),
            profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
        ]
        const status = (strategy: "spread" | "order") =>
            poolStatus(
                [tunnel(), tunnel({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1 })],
                { strategy, carrier: strategy === "order" ? "nasnet-wg0" : undefined },
            )

        const { unmount } = renderIt(two, status("spread"))
        await screen.findByRole("button", { name: /add a vpn/i })
        expect(screen.queryByLabelText("Move frankfurt down")).toBeNull()
        unmount()

        renderIt(two, status("order"))
        await screen.findByRole("button", { name: /add a vpn/i })
        expect(screen.getByLabelText("Move frankfurt down")).toBeEnabled()
        expect(screen.getByLabelText("Move berlin up")).toBeEnabled()
        // First in the chain cannot climb, last cannot fall.
        expect(screen.getByLabelText("Move frankfurt up")).toBeDisabled()
        expect(screen.getByLabelText("Move berlin down")).toBeDisabled()
    })

    it("saves the whole order the moment a tunnel moves", async () => {
        setVPNPoolOrder.mockResolvedValue({ success: true, data: null })
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus(
                [tunnel(), tunnel({ profile_id: 2, name: "berlin", if_name: "nasnet-wg1", position: 1 })],
                { strategy: "order", carrier: "nasnet-wg0" },
            ),
        )
        await screen.findByRole("button", { name: /add a vpn/i })
        await userEvent.click(screen.getByLabelText("Move berlin up"))
        expect(setVPNPoolOrder).toHaveBeenCalledWith([2, 1])
    })

    // The whole point of the chain: what happens when this one dies.
    it("names the tunnel that would take over", async () => {
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus(
                [
                    tunnel(),
                    tunnel({
                        profile_id: 2,
                        name: "berlin",
                        if_name: "nasnet-wg1",
                        position: 1,
                        in_pool: false,
                    }),
                ],
                { strategy: "order", carrier: "nasnet-wg0" },
            ),
            health([
                tunnelHealth(),
                tunnelHealth({
                    profile_id: 2,
                    name: "berlin",
                    if_name: "nasnet-wg1",
                    position: 1,
                    in_pool: false,
                }),
            ]),
        )
        expect(await screen.findByText("next up")).toBeInTheDocument()
    })

    it("separates a dead uplink from a dead pool", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel({ connected: false })], { uplinks: [wan({ up: false })] }),
        )
        expect(await screen.findByText(/Every secondary uplink is down/)).toBeInTheDocument()
    })
})

describe("detectFormat", () => {
    // The two forms cannot be confused, so the operator never has to say which
    // one they have.
    it("tells a link from a config file", () => {
        expect(detectFormat("wireguard://key@host:51820")).toBe("uri")
        expect(detectFormat("  WIREGUARD://key@host:51820  ")).toBe("uri")
        expect(detectFormat("[Interface]\nPrivateKey = x")).toBe("conf")
        expect(detectFormat("\n [interface] \n")).toBe("conf")
        expect(detectFormat("hello")).toBe("")
        expect(detectFormat("")).toBe("")
    })
})

describe("handshakeLabel", () => {
    it("reads as time, not as a number", () => {
        expect(handshakeLabel({ handshake_age_seconds: null })).toBe("No handshake yet")
        expect(handshakeLabel({ handshake_age_seconds: 5 })).toMatch(/just now/)
        expect(handshakeLabel({ handshake_age_seconds: 300 })).toMatch(/5 min ago/)
        expect(handshakeLabel({ handshake_age_seconds: 7300 })).toMatch(/2 h ago/)
    })
})

describe("handshakeShort", () => {
    it("compresses the same signal for a table cell", () => {
        expect(handshakeShort(null)).toBe("never")
        expect(handshakeShort(5)).toBe("just now")
        expect(handshakeShort(300)).toBe("5m ago")
        expect(handshakeShort(7300)).toBe("2h ago")
    })
})

describe("formatBytes", () => {
    it("scales", () => {
        expect(formatBytes(512)).toBe("512 B")
        expect(formatBytes(2048)).toBe("2.0 KB")
        expect(formatBytes(5 * 1024 * 1024)).toBe("5.0 MB")
        expect(formatBytes(50 * 1024 * 1024 * 1024)).toBe("50 GB")
    })
})

describe("the via column", () => {
    beforeEach(() => {
        getVPNProfiles.mockReset()
        getVPNStatus.mockReset()
        getRouterHealth.mockReset()
        setVPNProfileTransport.mockReset()
    })

    const twoWans = [
        wan(),
        wan({ slot: "secondary2", if_name: "lte0", label: "LTE", key: "k-lte0" }),
    ]

    it("dims a ride the pool chose and leaves a pin solid", async () => {
        renderIt(
            [
                profile({ id: 1, name: "a", enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "b", enabled: true, wg_slot: 1, transport_uplink: "k-lte0" }),
            ],
            poolStatus(
                [
                    tunnel({ profile_id: 1, name: "a", via: { if_name: "dish0", label: "Dish", key: "k-dish0", pinned: false } }),
                    tunnel({ profile_id: 2, name: "b", if_name: "nasnet-wg1", via: { if_name: "lte0", label: "LTE", key: "k-lte0", pinned: true } }),
                ],
                { uplinks: twoWans },
            ),
        )
        const auto = await screen.findByText("auto · Dish")
        expect(auto.className).toContain("text-text-tertiary")
        expect(screen.getByText("LTE").className).not.toContain("text-text-tertiary")
    })

    it("pins through the dropdown, named per row", async () => {
        setVPNProfileTransport.mockResolvedValue({ success: true, data: null })
        renderIt(
            [profile({ id: 1, name: "a", enabled: true, wg_slot: 0 })],
            poolStatus(
                [tunnel({ profile_id: 1, name: "a", via: { if_name: "dish0", label: "Dish", key: "k-dish0", pinned: false } })],
                { uplinks: twoWans },
            ),
        )
        await userEvent.click(await screen.findByLabelText("Change a uplink"))
        await userEvent.click(await screen.findByRole("option", { name: "LTE" }))
        await waitFor(() => expect(setVPNProfileTransport).toHaveBeenCalledWith(1, "k-lte0"))
    })

    it("offers Automatic first, and it clears the pin", async () => {
        setVPNProfileTransport.mockResolvedValue({ success: true, data: null })
        renderIt(
            [profile({ id: 1, name: "a", enabled: true, wg_slot: 0, transport_uplink: "k-lte0" })],
            poolStatus(
                [tunnel({ profile_id: 1, name: "a", via: { if_name: "lte0", label: "LTE", key: "k-lte0", pinned: true } })],
                { uplinks: twoWans },
            ),
        )
        await userEvent.click(await screen.findByLabelText("Change a uplink"))
        const options = await screen.findAllByRole("option")
        expect(options[0]).toHaveTextContent("Automatic")
        await userEvent.click(options[0])
        await waitFor(() => expect(setVPNProfileTransport).toHaveBeenCalledWith(1, ""))
    })

    // Repro: two rows, the second one dead. Both must open.
    it("opens the dropdown on every row, not just the first", async () => {
        renderIt(
            [
                profile({ id: 1, name: "a", enabled: true, wg_slot: 0 }),
                profile({ id: 2, name: "b", enabled: true, wg_slot: 1 }),
            ],
            poolStatus(
                [
                    tunnel({ profile_id: 1, name: "a", via: { if_name: "dish0", label: "Dish", key: "k-dish0", pinned: false } }),
                    tunnel({
                        profile_id: 2, name: "b", if_name: "nasnet-wg1", position: 1, in_pool: false,
                        connected: false, handshake_age_seconds: null,
                        via: { if_name: "lte0", label: "LTE", key: "k-lte0", pinned: false },
                    }),
                ],
                { uplinks: twoWans, strategy: "fastest", carrier: "nasnet-wg0" },
            ),
            health([
                tunnelHealth({ profile_id: 1, name: "a" }),
                tunnelHealth({
                    profile_id: 2, name: "b", if_name: "nasnet-wg1", position: 1,
                    in_pool: false, verdict: "no-internet", loss_pct: 100, median_rtt_ms: 0,
                }),
            ]),
        )
        await userEvent.click(await screen.findByLabelText("Change b uplink"))
        const options = await screen.findAllByRole("option")
        expect(options.length).toBe(3)
        // Popper: the last row's menu flips up instead of clamping at the page edge.
        expect(document.querySelector("[data-radix-popper-content-wrapper]")).not.toBeNull()
    })
})
