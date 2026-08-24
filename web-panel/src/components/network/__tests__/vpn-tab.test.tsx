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
const setVPNProfileRole = vi.fn()
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
        setVPNProfileRole: (...a: unknown[]) => setVPNProfileRole(...a),
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
        priority: 0,
        weight: 1,
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
    return { tunnels, uplinks: [wan()], kill_switch: true, ...over }
}

function tunnelHealth(over: Partial<TunnelHealth> = {}): TunnelHealth {
    return {
        profile_id: 1,
        name: "frankfurt",
        if_name: "nasnet-wg0",
        priority: 0,
        weight: 1,
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
                      active_tier: 0,
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
        setVPNProfileRole.mockReset()
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
        expect(await screen.findByText("1 of 1 carrying in tier 0")).toBeInTheDocument()
        expect(screen.getAllByText("frankfurt").length).toBeGreaterThan(0)
    })

    // The kill switch is not a setting, so the UI has to state what it is doing.
    it("says why nothing is getting out when the pool is empty", async () => {
        renderIt([profile()], poolStatus([]))
        expect(await screen.findByText("No VPN in the pool")).toBeInTheDocument()
        expect(screen.getByText(/until a VPN is turned on/)).toBeInTheDocument()
    })

    // A tunnel that stops looks identical to a working one from the link's side.
    it("warns when no member of the pool is answering", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel({ connected: false, handshake_age_seconds: 900 })]),
        )
        expect(await screen.findByText(/tunnels are answering/)).toBeInTheDocument()
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
        expect(await screen.findByLabelText("Delete frankfurt")).toBeDisabled()
        expect(screen.getByLabelText("Edit frankfurt")).toBeDisabled()
        expect(screen.getByLabelText("Turn frankfurt off")).toBeEnabled()
    })

    it("shows each member's health and role in the table", async () => {
        renderIt(
            [
                profile({ enabled: true, wg_slot: 0, weight: 3 }),
                profile({ id: 2, name: "berlin", enabled: true, wg_slot: 1, priority: 1 }),
            ],
            poolStatus([
                tunnel({ weight: 3 }),
                tunnel({
                    profile_id: 2,
                    name: "berlin",
                    if_name: "nasnet-wg1",
                    priority: 1,
                    in_pool: false,
                }),
            ]),
            health([
                tunnelHealth({ weight: 3 }),
                tunnelHealth({
                    profile_id: 2,
                    name: "berlin",
                    if_name: "nasnet-wg1",
                    priority: 1,
                    in_pool: false,
                    verdict: "up",
                }),
            ]),
        )
        expect(await screen.findByText("online · standby")).toBeInTheDocument()
        expect(screen.getAllByText(/3% · 82ms/).length).toBe(2)
        expect(screen.getByLabelText("Turn berlin off")).toBeEnabled()
    })

    it("commits a weight edit through the role endpoint", async () => {
        setVPNProfileRole.mockResolvedValue({ success: true, data: null })
        renderIt(
            [profile({ enabled: true, wg_slot: 0, weight: 3 })],
            poolStatus([tunnel({ weight: 3 })]),
            health([tunnelHealth({ weight: 3 })]),
        )
        await screen.findByText("VPN pool")
        await userEvent.click(screen.getByLabelText("Change frankfurt weight"))
        const input = screen.getByLabelText("frankfurt weight")
        await userEvent.clear(input)
        await userEvent.type(input, "5{Enter}")
        expect(setVPNProfileRole).toHaveBeenCalledWith(1, { priority: 0, weight: 5 })
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
        expect(await screen.findByText(/This is the last tunnel/)).toBeInTheDocument()
    })

    // A rejected enable used to close the dialog and say nothing at all.
    it("says so when a change is refused", async () => {
        enableVPNProfile.mockRejectedValue(new Error("wg0 refused to come up"))
        renderIt([profile({ wg_slot: null })], poolStatus([]))
        await userEvent.click(await screen.findByLabelText("Turn frankfurt on"))
        await userEvent.click(await screen.findByRole("button", { name: /turn it on/i }))
        await waitFor(() => expect(toastError).toHaveBeenCalledWith("wg0 refused to come up"))
    })

    // Number("") is 0, and 0 is a valid tier: clearing the box must not promote
    // the tunnel to the top of the ladder.
    it("refuses a blank tier instead of reading it as tier 0", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0, priority: 3 })],
            poolStatus([tunnel({ priority: 3 })]),
            health([tunnelHealth({ priority: 3 })]),
        )
        await userEvent.click(await screen.findByLabelText("Change frankfurt tier"))
        await userEvent.clear(screen.getByLabelText("frankfurt tier"))
        await userEvent.click(screen.getByLabelText("Save frankfurt tier"))
        expect(setVPNProfileRole).not.toHaveBeenCalled()
        expect(toastError).toHaveBeenCalled()
    })

    it("separates a dead uplink from a dead pool", async () => {
        renderIt(
            [profile({ enabled: true, wg_slot: 0 })],
            poolStatus([tunnel({ connected: false })], { uplinks: [wan({ up: false })] }),
        )
        expect(await screen.findByText(/secondary uplink is down/)).toBeInTheDocument()
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
})
