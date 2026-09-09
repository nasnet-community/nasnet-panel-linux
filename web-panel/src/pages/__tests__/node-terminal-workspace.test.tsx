import { useEffect, useState } from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes, Link } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import NodeDetailPage from "../nodes/[id]"

const lifecycle = vi.hoisted(() => ({ mounted: vi.fn(), disposed: vi.fn() }))
vi.mock("@/lib/api/nodes", () => ({
    listNodes: vi.fn(async () => ({ success: true, data: [{ id: 1 }] })),
    getNode: vi.fn(async () => ({ success: true, data: { id: 1, name: "Local server", ip: "127.0.0.1", is_online: true } })),
    restartXrayProcess: vi.fn(),
}))
vi.mock("@/lib/queries", () => ({
    useNodeStats: () => ({ data: undefined, refetch: vi.fn() }),
    useNodeStatsHistory: () => ({ data: undefined }),
    queryKeys: { nodeStats: (id: number) => ["stats", id], nodeStatsHistory: (id: number) => ["history", id] },
}))
vi.mock("@/components/node/node-terminal", () => ({
    NodeTerminal: ({ isActive, nodeName }: { isActive: boolean; nodeName: string }) => {
        const [command, setCommand] = useState("")
        useEffect(() => { lifecycle.mounted(); return () => lifecycle.disposed() }, [])
        return <div data-testid="terminal" data-active={isActive}>
            <span>{nodeName} terminal</span>
            <input aria-label="Shell draft" value={command} onChange={e => setCommand(e.target.value)} />
        </div>
    },
}))
vi.mock("@/components/node/node-overview", () => ({ NodeOverview: () => <div>Overview content</div> }))
vi.mock("@/components/node/node-logs", () => ({ NodeLogs: () => <div>Logs content</div> }))
vi.mock("@/components/node/node-settings", () => ({ NodeSettings: ({ onDirtyChange }: { onDirtyChange: (dirty: boolean) => void }) => <button onClick={() => onDirtyChange(true)}>Edit settings</button> }))
vi.mock("@/components/node/node-network-config", () => ({ NodeNetworkConfig: () => <div>Network content</div> }))
vi.mock("@/components/node/node-access-logs", () => ({ NodeAccessLogs: () => null }))
vi.mock("@/components/node/starlink/starlink-dashboard", () => ({ StarlinkDashboard: () => null }))
vi.mock("@/components/node/geofiles-dialog", () => ({ GeofilesDialog: () => null }))
vi.mock("@/components/accounts/account-details-sheet", () => ({ AccountDetailsSheet: () => null }))

function mount(tab = "overview") {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/server?tab=${tab}`]}>
        <Link to="/dashboard">Leave server</Link>
        <Routes><Route path="/server" element={<NodeDetailPage />} /><Route path="/dashboard" element={<p>Dashboard</p>} /></Routes>
    </MemoryRouter></QueryClientProvider>)
}
beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => vi.unstubAllGlobals())

describe("local server workspace", () => {
    it("omits the retired Accounts section", async () => {
        mount()
        await screen.findByText("Overview content")
        expect(screen.queryByRole("tab", { name: /^Accounts/ })).not.toBeInTheDocument()
        expect(screen.getByRole("tab", { name: /^Network/ })).toBeVisible()
    })
    it.each(["users", "accounts"])("opens Network for the old %s tab", async tab => {
        mount(tab)
        expect(await screen.findByText("Network content")).toBeVisible()
        expect(screen.getByRole("tab", { name: /^Network/ })).toHaveAttribute("data-state", "active")
        expect(screen.queryByRole("tab", { name: /^Accounts/ })).not.toBeInTheDocument()
    })
    it("opens the terminal lazily and retains the same shell while visiting logs", async () => {
        const user = userEvent.setup()
        mount()
        await screen.findByText("Overview content")
        expect(lifecycle.mounted).not.toHaveBeenCalled()
        await user.click(screen.getByRole("tab", { name: /^Terminal/ }))
        await user.type(await screen.findByRole("textbox", { name: "Shell draft" }), "pwd")
        await user.click(screen.getByRole("tab", { name: /^Logs/ }))
        expect(screen.getByTestId("terminal")).not.toBeVisible()
        expect(screen.queryByRole("textbox", { name: "Shell draft" })).not.toBeInTheDocument()
        expect(lifecycle.disposed).not.toHaveBeenCalled()
        await user.click(screen.getByRole("tab", { name: /^Terminal/ }))
        expect(screen.getByRole("textbox", { name: "Shell draft" })).toHaveValue("pwd")
        expect(lifecycle.mounted).toHaveBeenCalledOnce()
        await user.click(screen.getByRole("link", { name: "Leave server" }))
        expect(lifecycle.disposed).toHaveBeenCalledOnce()
    })
    it("keeps dirty settings until the operator confirms leaving the section", async () => {
        const user = userEvent.setup()
        mount("settings")
        await user.click(await screen.findByRole("button", { name: "Edit settings" }))
        await user.click(screen.getByRole("tab", { name: /^Logs/ }))
        expect(screen.getByRole("dialog", { name: "Unsaved settings" })).toBeVisible()
        await user.click(screen.getByRole("button", { name: "Keep editing" }))
        expect(screen.getByRole("button", { name: "Edit settings" })).toBeVisible()
        await user.click(screen.getByRole("tab", { name: /^Logs/ }))
        await user.click(screen.getByRole("button", { name: "Discard and leave" }))
        expect(screen.getByText("Logs content")).toBeVisible()
    })
})
