import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { InterfaceTable } from "../interface-table"
import type { NetworkInterfaceView } from "@/lib/types/network"

const setInterfaceLabel = vi.fn()
const toastSuccess = vi.fn()
const toastError = vi.fn()

vi.mock("sonner", () => ({
    toast: {
        success: (m: string) => toastSuccess(m),
        error: (m: string) => toastError(m),
    },
}))

vi.mock("@/lib/api/network", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/api/network")>()
    return {
        ...actual,
        setInterfaceLabel: (...a: unknown[]) => setInterfaceLabel(...a),
    }
})

function iface(over: Partial<NetworkInterfaceView> = {}): NetworkInterfaceView {
    return {
        id: 1,
        if_name: "enp0s3",
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
        addrs: ["10.0.3.15/24"],
        role: "wan",
        slot: "secondary",
        label: "",
        present: true,
        healthy: true,
        ...over,
    }
}

// The component ships a desktop table and a mobile card list, so every control
// renders twice. These helpers always drive the first (desktop) one.
function renderIt(rows: NetworkInterfaceView[]) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <QueryClientProvider client={qc}>
            <InterfaceTable interfaces={rows} onAssign={() => {}} />
        </QueryClientProvider>,
    )
}

function first(label: string): HTMLElement {
    return screen.getAllByLabelText(label)[0]
}

describe("naming a port", () => {
    beforeEach(() => {
        setInterfaceLabel.mockReset()
        toastSuccess.mockReset()
        toastError.mockReset()
    })

    // An unnamed port still has to offer the control, or there is no way in.
    it("offers a name control whether or not the port is named", () => {
        renderIt([iface(), iface({ id: 2, if_name: "enp0s4", key: "k4", label: "LTE backup" })])
        expect(first("Name enp0s3")).toBeInTheDocument()
        expect(first("Rename enp0s4")).toBeInTheDocument()
        expect(screen.getAllByText("Unnamed").length).toBeGreaterThan(0)
        expect(screen.getAllByText("LTE backup").length).toBeGreaterThan(0)
    })

    it("saves the name against the interface key", async () => {
        setInterfaceLabel.mockResolvedValue({ success: true, data: null })
        renderIt([iface()])
        await userEvent.click(first("Name enp0s3"))
        await userEvent.type(first("Name for enp0s3"), "Starlink")
        await userEvent.click(first("Save the name"))
        await waitFor(() =>
            expect(setInterfaceLabel).toHaveBeenCalledWith("52:54:00:12:34:02", "Starlink"),
        )
        expect(toastSuccess).toHaveBeenCalledWith("Name saved")
    })

    // Clearing the field is how a name is removed, so it must not read as a failure.
    it("treats an emptied field as removing the name", async () => {
        setInterfaceLabel.mockResolvedValue({ success: true, data: null })
        renderIt([iface({ label: "Old name" })])
        await userEvent.click(first("Rename enp0s3"))
        await userEvent.clear(first("Name for enp0s3"))
        await userEvent.click(first("Save the name"))
        await waitFor(() => expect(setInterfaceLabel).toHaveBeenCalledWith("52:54:00:12:34:02", ""))
        expect(toastSuccess).toHaveBeenCalledWith("Name removed")
    })

    it("keeps the old name when the save fails", async () => {
        setInterfaceLabel.mockResolvedValue({ success: false, error: "database is locked" })
        renderIt([iface({ label: "Old name" })])
        await userEvent.click(first("Rename enp0s3"))
        await userEvent.type(first("Name for enp0s3"), "!")
        await userEvent.click(first("Save the name"))
        await waitFor(() => expect(toastError).toHaveBeenCalledWith("database is locked"))
        // Still editing, so the typing is not lost.
        expect(first("Name for enp0s3")).toBeInTheDocument()
    })

    it("discards the draft on cancel", async () => {
        renderIt([iface({ label: "Old name" })])
        await userEvent.click(first("Rename enp0s3"))
        await userEvent.type(first("Name for enp0s3"), "typed but abandoned")
        await userEvent.click(first("Discard"))
        expect(screen.getAllByText("Old name").length).toBeGreaterThan(0)
        expect(setInterfaceLabel).not.toHaveBeenCalled()
    })
})
