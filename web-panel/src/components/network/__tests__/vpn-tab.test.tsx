import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { VpnTab } from "@/pages/router/vpn-tab"
import { detectFormat, formatBytes, handshakeLabel } from "@/lib/vpn-labels"
import type { VPNProfile, VPNStatus } from "@/lib/types/network"

const getVPNProfiles = vi.fn()
const getVPNStatus = vi.fn()
const deleteVPNProfile = vi.fn()

vi.mock("@/lib/api/network", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/api/network")>()
    return {
        ...actual,
        getVPNProfiles: (...a: unknown[]) => getVPNProfiles(...a),
        getVPNStatus: (...a: unknown[]) => getVPNStatus(...a),
        deleteVPNProfile: (...a: unknown[]) => deleteVPNProfile(...a),
        createVPNProfile: vi.fn(),
        updateVPNProfile: vi.fn(),
        parseVPNInput: vi.fn(),
        generateVPNKeypair: vi.fn(),
        activateVPN: vi.fn(),
        deactivateVPN: vi.fn(),
    }
})

function profile(over: Partial<VPNProfile> = {}): VPNProfile {
    return {
        id: 1,
        name: "frankfurt",
        type: "wireguard",
        active: false,
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

function status(over: Partial<VPNStatus> = {}): VPNStatus {
    return {
        active_profile_id: 1,
        name: "frankfurt",
        connected: true,
        handshake_age_seconds: 12,
        rx_bytes: 2048,
        tx_bytes: 1024,
        endpoint: "185.65.135.1:51820",
        mtu: 1420,
        keepalive_seconds: 25,
        secondary_uplink_up: true,
        kill_switch: true,
        ...over,
    }
}

function renderIt(profiles: VPNProfile[], st: VPNStatus) {
    getVPNProfiles.mockResolvedValue({ success: true, data: profiles })
    getVPNStatus.mockResolvedValue({ success: true, data: st })
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
        deleteVPNProfile.mockReset()
    })

    // Every popover and dialog needs its provider above it. Without one the
    // whole tab throws, which no amount of pure-helper testing would catch.
    it("renders a connected tunnel without a provider error", async () => {
        renderIt([profile({ active: true })], status())
        expect(await screen.findByText("Connected")).toBeInTheDocument()
        expect(screen.getByText(/Last handshake just now/)).toBeInTheDocument()
        expect(screen.getAllByText("frankfurt").length).toBeGreaterThan(0)
    })

    // The kill switch is not a setting, so the UI has to state what it is doing.
    it("says why nothing is getting out when no VPN is on", async () => {
        renderIt([profile()], status({ active_profile_id: null, connected: false, name: undefined }))
        expect(await screen.findByText("No VPN in use")).toBeInTheDocument()
        expect(screen.getByText(/until a VPN is turned on/)).toBeInTheDocument()
    })

    // A tunnel that stops looks identical to a working one from the link's side.
    it("warns when the tunnel is on but not answering", async () => {
        renderIt(
            [profile({ active: true })],
            status({ connected: false, handshake_age_seconds: 900 }),
        )
        expect(await screen.findByText(/tunnel is not answering/)).toBeInTheDocument()
        expect(screen.getByText("Not answering")).toBeInTheDocument()
    })

    it("invites a first VPN when there are none", async () => {
        renderIt([], status({ active_profile_id: null, connected: false, name: undefined }))
        expect(await screen.findByText("No VPN yet")).toBeInTheDocument()
    })

    // Two intents that must not collapse into one click: stop tunnelling, and
    // remove the config. Deleting the live one leaves nothing to turn it off.
    it("refuses to edit or delete the VPN in use", async () => {
        renderIt([profile({ active: true })], status())
        expect(await screen.findByLabelText("Delete frankfurt")).toBeDisabled()
        expect(screen.getByLabelText("Edit frankfurt")).toBeDisabled()
        expect(screen.getByRole("button", { name: "Turn off" })).toBeEnabled()
    })

    it("offers to switch to a VPN that is not in use", async () => {
        renderIt([profile(), profile({ id: 2, name: "berlin", active: true })], status())
        expect(await screen.findByRole("button", { name: "Use this one" })).toBeEnabled()
        expect(screen.getByLabelText("Delete frankfurt")).toBeEnabled()
    })

    // The defaults are applied for the operator, so the UI has to say so
    // somewhere rather than showing two numbers from nowhere.
    it("explains the applied defaults on demand", async () => {
        renderIt([profile({ active: true })], status())
        await screen.findByText("Connected")
        await userEvent.click(screen.getByRole("button", { name: /about these settings/i }))
        expect(await screen.findByText(/keepalive of 25 seconds/)).toBeInTheDocument()
    })

    it("separates a dead uplink from a dead tunnel", async () => {
        renderIt(
            [profile({ active: true })],
            status({ connected: false, secondary_uplink_up: false }),
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
        expect(handshakeLabel(status({ handshake_age_seconds: null }))).toBe("No handshake yet")
        expect(handshakeLabel(status({ handshake_age_seconds: 5 }))).toMatch(/just now/)
        expect(handshakeLabel(status({ handshake_age_seconds: 300 }))).toMatch(/5 min ago/)
        expect(handshakeLabel(status({ handshake_age_seconds: 7300 }))).toMatch(/2 h ago/)
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
