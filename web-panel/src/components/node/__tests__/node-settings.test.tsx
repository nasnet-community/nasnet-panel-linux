import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NodeSettings } from "../node-settings"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { getXrayConfig, updateNode } from "@/lib/api/nodes"
import type { Node } from "@/lib/types"

vi.mock("@/lib/api/nodes", () => ({
    getXrayConfig: vi.fn(), getNodeSSHStatus: vi.fn(async () => ({ success: true, data: { enabled: true, port: 22 } })),
    getInstallCommand: vi.fn(), startXrayProcess: vi.fn(), stopXrayProcess: vi.fn(), restartXrayProcess: vi.fn(), restartNodeSSH: vi.fn(), clearNodeSSHLogs: vi.fn(),
    updateNode: vi.fn(async () => ({ success: true })), updateXrayConfig: vi.fn(), updateNodeSSHConfig: vi.fn(), switchNodeConnectMode: vi.fn(), deleteNode: vi.fn(),
}))
vi.mock("@/lib/api/maintenance", () => ({ setNodeMaintenance: vi.fn() }))
vi.mock("../danger-zone", () => ({ DangerZone: () => <div>Destructive operations</div> }))
const node = { id: 6, name: "London", ip: "192.0.2.6", is_online: true, is_active: true, updated_at: "1" } as Node
beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); vi.clearAllMocks(); vi.mocked(getXrayConfig).mockResolvedValue({ success: true, data: '{"log":{"loglevel":"warning"}}' }) })
function setup() { return render(<MemoryRouter><ConfirmDialogProvider><NodeSettings node={node} onRefresh={vi.fn()} /></ConfirmDialogProvider></MemoryRouter>) }
describe("node settings workspace", () => {
    it("clears the workspace navigation guard when dirty settings unmount", async () => {
        const onDirtyChange = vi.fn()
        const user = userEvent.setup()
        const view = render(<MemoryRouter><ConfirmDialogProvider><NodeSettings node={node} onRefresh={vi.fn()} onDirtyChange={onDirtyChange} /></ConfirmDialogProvider></MemoryRouter>)
        await user.type(screen.getByRole("textbox", { name: "Server Name" }), " draft")
        await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
        view.unmount()
        expect(onDirtyChange).toHaveBeenLastCalledWith(false)
    })
    it("preserves the service actions in an accessible menu", async () => {
        const user = userEvent.setup(); setup()
        await user.click(screen.getByRole("button", { name: "Settings actions" }))
        for (const name of ["Start Xray", "Stop Xray", "Restart Xray", "Restart SSH", "Clear SSH logs"]) {
            expect(screen.getByRole("menuitem", { name })).toBeVisible()
        }
        expect(screen.queryByRole("menuitem", { name: "Copy install command" })).not.toBeInTheDocument()
        expect(screen.queryByRole("menuitem", { name: "Update agent" })).not.toBeInTheDocument()
    })
    it("keeps edits across categories and reviews changes before saving", async () => {
        const user = userEvent.setup(); setup()
        const name = screen.getByRole("textbox", { name: "Server Name" })
        await user.clear(name); await user.type(name, "New name")
        await user.click(screen.getByRole("button", { name: "Automation" }))
        expect(screen.getByText("Starlink Monitoring")).toBeVisible()
        expect(screen.queryByText("Bandwidth Shaping")).not.toBeInTheDocument()
        expect(screen.queryByText("Destructive operations")).not.toBeInTheDocument()
        await user.click(screen.getByRole("button", { name: "General" }))
        expect(screen.getByRole("textbox", { name: "Server Name" })).toHaveValue("New name")
        await user.click(screen.getByRole("button", { name: /1 unsaved change/ }))
        const dialog = screen.getByRole("dialog")
        expect(within(dialog).getByText("London")).toBeVisible()
        expect(within(dialog).getByText("New name")).toBeVisible()
        expect(updateNode).not.toHaveBeenCalled()
        await user.click(within(dialog).getByRole("button", { name: "Save changes" }))
        await waitFor(() => expect(updateNode).toHaveBeenCalledWith(6, { name: "New name" }))
        await waitFor(() => expect(screen.queryByRole("region", { name: "Unsaved settings" })).not.toBeInTheDocument())
    })
    it("shows an actionable Xray retry state when loading fails", async () => {
        vi.mocked(getXrayConfig).mockResolvedValue({ success: false, error: "Agent timed out" })
        const user = userEvent.setup(); setup()
        await user.click(screen.getByRole("button", { name: "Logging" }))
        expect(await screen.findByText("Agent timed out")).toBeVisible()
        vi.mocked(getXrayConfig).mockResolvedValue({ success: true, data: '{"log":{"loglevel":"warning"}}' })
        await user.click(screen.getByRole("button", { name: "Retry Xray settings" }))
        expect(await screen.findByRole("combobox", { name: "Log Level" })).toBeVisible()
    })
    it("includes customer maintenance in the shared change review", async () => {
        const user = userEvent.setup(); setup()
        await user.click(screen.getByRole("switch", { name: "Maintenance mode" }))
        await user.type(screen.getByRole("textbox", { name: /User notice/ }), "Back soon")
        await user.click(screen.getByRole("button", { name: /2 unsaved changes/ }))
        expect(within(screen.getByRole("dialog")).getByText("Back soon")).toBeVisible()
    })
})
