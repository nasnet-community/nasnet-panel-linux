import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { LanDevices } from "../lan-devices"
import type { LANDeviceList } from "@/lib/types/network"

const getLANDevices = vi.fn()

vi.mock("@/lib/api/network", () => ({
    getLANDevices: (...a: unknown[]) => getLANDevices(...a),
    setDeviceLabel: vi.fn(),
    getLAN: vi.fn(),
    updateLAN: vi.fn(),
    getNetworkInterfaces: vi.fn(),
    getNetworkState: vi.fn(),
    getPortForwards: vi.fn(),
    createPortForward: vi.fn(),
    updatePortForward: vi.fn(),
    deletePortForward: vi.fn(),
    planNetworkChange: vi.fn(),
    applyNetworkChange: vi.fn(),
    confirmNetworkApply: vi.fn(),
    rollbackNetworkApply: vi.fn(),
}))

function list(over: Partial<LANDeviceList> = {}): LANDeviceList {
    return {
        devices: [
            {
                mac: "b8:27:eb:11:00:01",
                ips: ["10.77.0.172"],
                hostname: "media-server",
                vendor: "Raspberry Pi Foundation",
                label: "",
                randomized: false,
                port: "enp0s4",
                online: true,
                last_seen_seconds: 3,
            },
            {
                mac: "b2:9a:44:11:00:03",
                ips: ["10.77.0.185"],
                hostname: "someones-phone",
                vendor: "",
                label: "",
                randomized: true,
                port: "enp0s4",
                online: true,
                last_seen_seconds: 5,
            },
        ],
        enabled: true,
        leases_ok: true,
        neighbours_ok: true,
        offline_after_seconds: 300,
        ...over,
    }
}

function renderIt(data: LANDeviceList) {
    getLANDevices.mockResolvedValue({ success: true, data })
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <QueryClientProvider client={qc}>
            <LanDevices lanEnabled />
        </QueryClientProvider>,
    )
}

describe("LanDevices", () => {
    beforeEach(() => getLANDevices.mockReset())

    // Every Tooltip needs a TooltipProvider above it. Without one the whole tab
    // throws, which no amount of pure-helper testing would have caught.
    it("renders without a tooltip provider error", async () => {
        renderIt(list())
        expect(await screen.findByText("media-server")).toBeInTheDocument()
        expect(screen.getByText("someones-phone")).toBeInTheDocument()
    })

    it("shows the vendor and the port", async () => {
        renderIt(list())
        expect(await screen.findByText("Raspberry Pi Foundation")).toBeInTheDocument()
        expect(screen.getAllByText("enp0s4").length).toBeGreaterThan(0)
    })

    // A randomized MAC cannot hold a name, so it must not offer a rename control.
    it("offers rename only for a stable MAC", async () => {
        renderIt(list())
        expect(await screen.findByLabelText(/Rename media-server/)).toBeInTheDocument()
        expect(screen.queryByLabelText(/Rename someones-phone/)).not.toBeInTheDocument()
    })

    it("summarises how many are connected", async () => {
        renderIt(list())
        expect(await screen.findByText("2")).toBeInTheDocument()
        expect(screen.getByText("connected")).toBeInTheDocument()
    })

    // The departure lag is surprising, so it must be explained somewhere — but
    // behind an affordance, not as three lines of body copy.
    it("explains the departure lag on demand", async () => {
        renderIt(list())
        await screen.findByText("media-server")
        await userEvent.click(screen.getByRole("button", { name: /more information/i }))
        expect(await screen.findByText(/up to 5 minutes/)).toBeInTheDocument()
    })

    it("explains a missing lease file instead of silently dropping names", async () => {
        renderIt(list({ leases_ok: false }))
        expect(await screen.findByText(/DHCP lease file could not be read/)).toBeInTheDocument()
    })

    it("invites action when nothing is connected", async () => {
        renderIt(list({ devices: [] }))
        expect(await screen.findByText("Nothing is connected yet")).toBeInTheDocument()
    })

    // The bridge is the only required source: failing it means unknown, not empty.
    it("reports a bridge read failure as unknown", async () => {
        getLANDevices.mockResolvedValue({ success: false, error: "bridge fdb: no such device" })
        const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
        render(
            <QueryClientProvider client={qc}>
                <LanDevices lanEnabled />
            </QueryClientProvider>,
        )
        expect(await screen.findByText(/not known what is connected/)).toBeInTheDocument()
    })
})
