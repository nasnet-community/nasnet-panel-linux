import { render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest"
import NodeDetailPage from "@/pages/nodes/[id]"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"

vi.mock("@/lib/api/nodes", () => ({
    getNode: vi.fn(async () => ({ success: true, data: { id: 4, name: "VPS 1", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true, is_active: true } })),
    restartXrayProcess: vi.fn(),
    updateNode: vi.fn(),
}))
vi.mock("@/lib/queries", () => ({
    useNodes: () => ({ data: [], isLoading: false }),
    useNodeStats: () => ({ data: undefined, refetch: vi.fn() }),
    useNodeOverview: () => ({ data: undefined }),
    queryKeys: { nodeStats: (id: number) => ["stats", id], nodeOverview: (id: number) => ["overview", id] },
}))
vi.mock("@/components/mtproto/mtproto-overview", () => ({ MtprotoOverview: () => <div>MTProto overview</div> }))
vi.mock("@/components/mtproto/mtproto-logs", () => ({ MtprotoLogs: () => null }))
vi.mock("@/components/mtproto/mtproto-settings", () => ({ MtprotoNodeSettings: () => <div>MTProto node settings</div> }))
vi.mock("@/components/mtproto/mtproto-header-actions", () => ({ MtprotoHeaderActions: () => <button type="button">Restart proxy</button> }))
vi.mock("@/components/node/node-settings", () => ({ NodeSettings: () => <div>Xray node settings</div> }))
vi.mock("@/components/node/node-cores-tab", () => ({ NodeCoresTab: () => null }))
vi.mock("@/components/node/geofiles-dialog", () => ({ GeofilesDialog: () => null }))
vi.mock("@/components/accounts/account-details-sheet", () => ({ AccountDetailsSheet: () => null }))

beforeEach(() => vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} }))
afterEach(() => vi.unstubAllGlobals())

function open(tab: string) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><ConfirmDialogProvider><MemoryRouter initialEntries={[`/nodes/4?tab=${tab}`]}>
        <Routes><Route path="/nodes/:id" element={<NodeDetailPage />} /></Routes>
    </MemoryRouter></ConfirmDialogProvider></QueryClientProvider>)
}

describe("an MTProto node's page", () => {
    it("shows its own settings, not a full node's", async () => {
        open("settings")
        expect(await screen.findByText("MTProto node settings")).toBeInTheDocument()
        expect(screen.getByRole("tab", { name: /^Settings/ })).toHaveAttribute("data-state", "active")
        expect(screen.queryByText("Xray node settings")).not.toBeInTheDocument()
    })

    it("has only the tabs that apply, and its own header actions", async () => {
        open("overview")
        expect(await screen.findByText("MTProto overview")).toBeInTheDocument()
        expect(screen.getByText("MTProto node")).toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Restart proxy" })).toBeInTheDocument()
        expect(screen.queryByRole("button", { name: /Restart Xray/ })).not.toBeInTheDocument()
        for (const tab of ["Terminal", "Network", "Access Logs"]) {
            expect(screen.queryByRole("tab", { name: new RegExp(`^${tab}`) })).not.toBeInTheDocument()
        }
    })

    it("lands on Overview from a deep link to a tab it doesn't have", async () => {
        open("terminal")
        expect(await screen.findByText("MTProto overview")).toBeInTheDocument()
    })
})
