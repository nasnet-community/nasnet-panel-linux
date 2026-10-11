import { StrictMode } from "react"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { MtprotoHeaderActions } from "../mtproto-header-actions"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { streamMtprotoJob, updateMtprotoNode, type MtprotoNode } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"

const state = vi.hoisted(() => ({ node: undefined as unknown, restart: vi.fn(), setStopped: vi.fn(), refreshNodes: vi.fn() }))

vi.mock("@/lib/queries", () => ({
    useMtprotoNode: () => ({ data: state.node, refetch: vi.fn() }),
    useRestartMtprotoTelemt: () => ({ mutate: state.restart, isPending: false }),
    useSetMtprotoProxyStopped: () => ({ mutate: state.setStopped, isPending: false }),
    useRefreshNodes: () => state.refreshNodes,
}))
vi.mock("@/lib/api/mtproto", () => ({
    updateMtprotoNode: vi.fn(), rotateMtprotoKeys: vi.fn(), repairMtprotoNode: vi.fn(), removeMtprotoNode: vi.fn(), streamMtprotoJob: vi.fn(),
}))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

const liteNode = { id: 4, name: "Tehran 1", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true } as Node

function fixture(up = 2): MtprotoNode {
    return {
        node_id: 4, agent_id: "a", hub_port: 8443, capture_port: 9443, ssh_port: 22, ssh_user: "root", host_key: "SHA256:x",
        disguise: "www.example.ir", max_streams: 5000, arch: "amd64", agent_version: "0.1.0", telemt_version: "3.4.25",
        running: true, healthy: true, latest: { agent: "0.2.0", telemt: "3.4.25" }, updates: { agent: true, telemt: false },
        link: { up, conns: [], streams: 0, relayed: 0, dial_failures: 0, violations: 0, bytes_to_dc: 0, bytes_from_dc: 0 },
    }
}

function setup(strict = false) {
    const tree = (
        <MemoryRouter initialEntries={["/nodes/4"]}>
            <ConfirmDialogProvider>
                <Routes>
                    <Route path="/nodes/4" element={<MtprotoHeaderActions node={liteNode} />} />
                    <Route path="/nodes" element={<p>the node list</p>} />
                </Routes>
            </ConfirmDialogProvider>
        </MemoryRouter>
    )
    return render(strict ? <StrictMode>{tree}</StrictMode> : tree)
}

async function openMenu() {
    await userEvent.setup().click(screen.getByRole("button", { name: "Node actions" }))
}

beforeEach(() => {
    vi.clearAllMocks()
    state.node = fixture()
})

describe("MtprotoHeaderActions", () => {
    it("flags an agent update on the button", () => {
        setup()
        expect(screen.getByRole("button", { name: /Update agent/ })).toContainElement(screen.getByLabelText("update available"))
    })

    it("shows no update that would change nothing", async () => {
        state.node = { ...fixture(), updates: { agent: false, telemt: false } }
        setup()
        expect(screen.queryByRole("button", { name: /Update agent/ })).not.toBeInTheDocument()
        await openMenu()
        expect(screen.queryByRole("menuitem", { name: /Update/ })).not.toBeInTheDocument()
    })

    it("turns off what needs the link while it is down, but keeps repair", async () => {
        state.node = fixture(0)
        setup()
        expect(screen.getByRole("button", { name: "Restart proxy" })).toBeDisabled()
        await openMenu()
        expect(screen.getByRole("menuitem", { name: /Stop proxy/ })).toHaveAttribute("data-disabled")
        expect(screen.getByRole("menuitem", { name: /Repair/ })).not.toHaveAttribute("data-disabled")
        // removal and key rotation live in Settings now
        expect(screen.queryByRole("menuitem", { name: /Remove/ })).not.toBeInTheDocument()
        expect(screen.queryByRole("menuitem", { name: /Rotate/ })).not.toBeInTheDocument()
    })

    it("restarts the proxy on click, without asking", async () => {
        const user = userEvent.setup()
        setup()
        await user.click(screen.getByRole("button", { name: "Restart proxy" }))
        expect(state.restart).toHaveBeenCalled()
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })

    it("starts one update per click, even under StrictMode", async () => {
        const user = userEvent.setup()
        vi.mocked(updateMtprotoNode).mockResolvedValue({ success: true, data: { job_id: "j1" } })
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        setup(true)
        await user.click(screen.getByRole("button", { name: /Update agent/ }))
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        await new Promise((r) => setTimeout(r, 50))
        expect(updateMtprotoNode).toHaveBeenCalledTimes(1)
        expect(updateMtprotoNode).toHaveBeenCalledWith(4, "agent")
    })

    it("asks before stopping the proxy", async () => {
        const user = userEvent.setup()
        setup()
        await openMenu()
        await user.click(screen.getByRole("menuitem", { name: /Stop proxy/ }))
        const dialog = await screen.findByRole("dialog")
        expect(dialog).toHaveTextContent("stays stopped across restarts")
        await user.click(within(dialog).getByRole("button", { name: "Stop proxy" }))
        expect(state.setStopped).toHaveBeenCalledWith(true)
    })

    it("offers Start once the proxy is stopped", async () => {
        const user = userEvent.setup()
        state.node = { ...fixture(), proxy_stopped: true }
        setup()
        expect(screen.getByRole("button", { name: "Restart proxy" })).toBeDisabled()
        await openMenu()
        await user.click(screen.getByRole("menuitem", { name: /Start proxy/ }))
        expect(state.setStopped).toHaveBeenCalledWith(false)
    })
})
