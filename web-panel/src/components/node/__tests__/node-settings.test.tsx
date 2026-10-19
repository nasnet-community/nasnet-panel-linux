import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NodeSettings } from "../node-settings"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { getXrayConfig, moveAgentPort, moveNodeAddress, switchNodeConnectMode, updateNode, updateXrayConfig } from "@/lib/api/nodes"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import { toast } from "@/lib/toast"
import type { Node } from "@/lib/types"

vi.mock("@/lib/api/nodes", () => ({
    getXrayConfig: vi.fn(), getNodeSSHStatus: vi.fn(async () => ({ success: true, data: { enabled: true, port: 22, is_active: true } })),
    updateNode: vi.fn(async () => ({ success: true })), updateXrayConfig: vi.fn(async () => ({ success: true })), updateNodeSSHConfig: vi.fn(),
    switchNodeConnectMode: vi.fn(), moveAgentPort: vi.fn(), moveNodeAddress: vi.fn(), deleteNode: vi.fn(),
}))
vi.mock("@/lib/api/maintenance", () => ({ setNodeMaintenance: vi.fn(async () => ({ success: true })) }))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))
vi.mock("../danger-zone", () => ({ DangerZone: () => <div>Destructive operations</div> }))

const localNode = { id: 6, name: "London", ip: "192.0.2.6", connect_mode: "local", is_online: true, is_active: true, updated_at: "1" } as Node
const remoteNode = (over: Partial<Node> = {}) => ({ id: 7, name: "fra-1", ip: "203.0.113.24", connect_mode: "direct", agent_port: 9090, api_port: 10085, is_online: true, is_active: true, updated_at: "1", ...over }) as Node

beforeEach(() => {
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    vi.clearAllMocks()
    vi.mocked(getXrayConfig).mockResolvedValue({ success: true, data: '{"log":{"loglevel":"warning","access":"/var/log/xray/access.log"}}' })
})

function setup(node: Node = localNode) {
    const onDirtyChange = vi.fn()
    const onRefresh = vi.fn()
    const view = render(<MemoryRouter><ConfirmDialogProvider><NodeSettings node={node} onRefresh={onRefresh} onDirtyChange={onDirtyChange} /></ConfirmDialogProvider></MemoryRouter>)
    return { onDirtyChange, onRefresh, view, user: userEvent.setup() }
}
const group = (name: string) => screen.getByRole("region", { name })
const editGroup = async (user: ReturnType<typeof userEvent.setup>, name: string) => {
    await user.click(await screen.findByRole("button", { name: `Edit ${name}` }))
    return group(name)
}
const footerOf = (g: HTMLElement) => within(g).getByRole("region", { name: "Unsaved changes" })

describe("node settings page", () => {
    it("reads as one page of sections and groups", async () => {
        setup(remoteNode())
        const list = screen.getByRole("navigation", { name: "On this page" })
        for (const s of ["General", "Connection", "Logging", "Monitoring", "Danger zone"]) expect(within(list).getByRole("button", { name: s })).toBeInTheDocument()
        expect(within(list).queryByRole("button", { name: "Automation" })).not.toBeInTheDocument()
        expect(screen.queryByText(/crash recovery/i)).not.toBeInTheDocument()
        expect(within(group("Identity")).getByText("fra-1")).toBeInTheDocument()
        expect(within(group("Identity")).getByText("#7")).toBeInTheDocument()
        expect(within(group("Agent")).getByText("Direct")).toBeInTheDocument()
        expect(within(group("Agent")).getByRole("button", { name: "Switch to reverse" })).toBeEnabled()
        expect(await within(group("Xray logging")).findByText("Warning")).toBeInTheDocument()
        expect(screen.queryByRole("textbox")).not.toBeInTheDocument()
    })

    it("saves only the group that changed", async () => {
        const { onDirtyChange, user } = setup()
        const identity = await editGroup(user, "Identity")
        const name = within(identity).getByLabelText("Name")
        await user.clear(name)
        await user.type(name, "New name")
        await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
        expect(footerOf(identity)).toHaveTextContent("1 unsaved change")
        expect(footerOf(identity)).toHaveTextContent("Users see it after their app refreshes")
        await user.click(within(identity).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateNode).toHaveBeenCalledWith(6, { name: "New name" }))
        expect(toast.success).toHaveBeenCalledWith("Identity saved")
        await waitFor(() => expect(within(group("Identity")).queryByRole("textbox")).not.toBeInTheDocument())
        expect(updateXrayConfig).not.toHaveBeenCalled()
        expect(setNodeMaintenance).not.toHaveBeenCalled()
    })

    it("keeps a refused save open with the reason", async () => {
        vi.mocked(updateNode).mockResolvedValue({ success: false, error: "That name is taken" })
        const { user } = setup()
        const identity = await editGroup(user, "Identity")
        await user.type(within(identity).getByLabelText("Name"), " 2")
        await user.click(within(identity).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(footerOf(identity)).toHaveTextContent("That name is taken"))
        expect(footerOf(identity)).toHaveTextContent("Your edits are kept.")
        expect(within(identity).getByLabelText("Name")).toHaveValue("London 2")
    })

    it("cancel puts a group back as it was", async () => {
        const { onDirtyChange, user } = setup()
        const identity = await editGroup(user, "Identity")
        await user.type(within(identity).getByLabelText("Name"), " draft")
        await user.click(within(identity).getByRole("button", { name: "Cancel" }))
        expect(within(group("Identity")).getByText("London")).toBeInTheDocument()
        await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    })

    it("clears the workspace navigation guard when dirty settings unmount", async () => {
        const { onDirtyChange, view, user } = setup()
        const identity = await editGroup(user, "Identity")
        await user.type(within(identity).getByLabelText("Name"), " draft")
        await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
        view.unmount()
        expect(onDirtyChange).toHaveBeenLastCalledWith(false)
    })

    it("saves the maintenance notice with its message", async () => {
        const { user } = setup()
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "Maintenance notice" }))
        await user.type(within(g).getByLabelText("Notice"), "Back soon")
        expect(footerOf(g)).toHaveTextContent("2 unsaved changes")
        expect(footerOf(g)).toHaveTextContent("Users see the notice right away")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(setNodeMaintenance).toHaveBeenCalledWith(6, { enabled: true, message: "Back soon" }))
        expect(updateNode).not.toHaveBeenCalled()
    })

    it("asks before taking a node out of service and says when users are still on it", async () => {
        vi.mocked(updateNode).mockResolvedValue({ success: true, data: remoteNode({ is_active: false }), warning: "The node didn't answer, so its users can still connect until it does." })
        const { user } = setup(remoteNode())
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "In service" }))
        expect(footerOf(g)).toHaveTextContent("Its users are cut off right away")
        expect(within(g).getByText(/Nobody can connect and its links are hidden/)).toBeInTheDocument()

        // backing out of the question saves nothing
        await user.click(within(g).getByRole("button", { name: "Save" }))
        const ask = await screen.findByRole("dialog")
        expect(ask).toHaveTextContent("Take fra-1 out of service?")
        expect(ask).toHaveTextContent("Its users are removed from Xray right away")
        await user.click(within(ask).getByRole("button", { name: "Cancel" }))
        expect(updateNode).not.toHaveBeenCalled()

        await user.click(within(g).getByRole("button", { name: "Save" }))
        await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Take out of service" }))
        await waitFor(() => expect(updateNode).toHaveBeenCalledWith(7, { is_active: false }))
        expect(toast.warning).toHaveBeenCalledWith("Availability saved", expect.objectContaining({ description: expect.stringContaining("still connect") }))
    })

    it("puts a node back in service without asking", async () => {
        vi.mocked(updateNode).mockResolvedValue({ success: true, data: remoteNode() })
        const { user } = setup(remoteNode({ is_active: false }))
        expect(within(group("Availability")).getByText("Nobody can connect and its links are hidden.")).toBeInTheDocument()
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "In service" }))
        expect(footerOf(g)).toHaveTextContent("Its users can connect again")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateNode).toHaveBeenCalledWith(7, { is_active: true }))
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
        expect(toast.success).toHaveBeenCalledWith("Availability saved")
    })

    it("points the built-in node's Wipe and Nuke to the panel's settings", () => {
        setup()
        const list = screen.getByRole("navigation", { name: "On this page" })
        expect(within(list).queryByRole("button", { name: "Danger zone" })).not.toBeInTheDocument()
        expect(screen.queryByText("Destructive operations")).not.toBeInTheDocument()
        expect(screen.getByRole("link", { name: "Settings → Danger zone" })).toHaveAttribute("href", "/settings/danger")
    })

    it("leaves the in-service switch off the built-in node", async () => {
        const { user } = setup()
        const g = await editGroup(user, "Availability")
        expect(within(g).queryByRole("switch", { name: "In service" })).not.toBeInTheDocument()
        expect(screen.queryByRole("region", { name: "Agent" })).not.toBeInTheDocument()
    })

    it("moves the agent port through a guarded change", async () => {
        vi.mocked(moveAgentPort).mockResolvedValue({ success: true, data: { moved: true, message: "Now on port 9443. Port 9090 is closed." } })
        const { onRefresh, user } = setup(remoteNode())
        await user.click(within(group("Agent")).getByRole("button", { name: "Change" }))
        const dialog = await screen.findByRole("dialog")
        const input = within(dialog).getByLabelText("New port")
        for (const [port, why] of [["10085", "Xray's API port"], ["9090", "already uses"], ["70000", "1 to 65535"]]) {
            await user.clear(input)
            await user.type(input, port)
            await user.tab()
            expect(within(dialog).getByText(new RegExp(why))).toBeInTheDocument()
            expect(within(dialog).getByRole("button", { name: "Change port" })).toBeDisabled()
        }
        await user.clear(input)
        await user.type(input, "9443")
        await user.click(within(dialog).getByRole("button", { name: "Change port" }))
        await waitFor(() => expect(moveAgentPort).toHaveBeenCalledWith(7, 9443))
        expect(await within(dialog).findByText("Now on port 9443. Port 9090 is closed.")).toBeInTheDocument()
        expect(onRefresh).toHaveBeenCalled()
        expect(toast.success).toHaveBeenCalledWith("Now on port 9443. Port 9090 is closed.")
    })

    it("says where a port move stopped when the panel can't get in", async () => {
        vi.mocked(moveAgentPort).mockResolvedValue({ success: true, data: { moved: false, step: "switch", message: "The panel couldn't reach the node on port 9443, so it stays on 9090." } })
        const { onRefresh, user } = setup(remoteNode())
        await user.click(within(group("Agent")).getByRole("button", { name: "Change" }))
        const dialog = await screen.findByRole("dialog")
        await user.type(within(dialog).getByLabelText("New port"), "9443")
        await user.click(within(dialog).getByRole("button", { name: "Change port" }))
        expect(await within(dialog).findByText(/stays on 9090/)).toBeInTheDocument()
        expect(onRefresh).not.toHaveBeenCalled()
        expect(toast.error).toHaveBeenCalled()
    })

    it("switches the connection mode and says how it ended", async () => {
        vi.mocked(switchNodeConnectMode).mockResolvedValue({ success: true, data: { moved: true, message: "Now in reverse mode." } })
        const { onRefresh, user } = setup(remoteNode())
        await user.click(within(group("Agent")).getByRole("button", { name: "Switch to reverse" }))
        const dialog = await screen.findByRole("dialog")
        expect(dialog).toHaveTextContent("goes back to direct mode by itself")
        await user.click(within(dialog).getByRole("button", { name: "Switch to reverse" }))
        await waitFor(() => expect(switchNodeConnectMode).toHaveBeenCalledWith(7, "reverse"))
        expect(await within(dialog).findByText("Now in reverse mode.")).toBeInTheDocument()
        expect(onRefresh).toHaveBeenCalled()
    })

    it("picks No flag, which the address can't fill back in", async () => {
        vi.mocked(updateNode).mockResolvedValue({ success: true, data: remoteNode({ country_code: "", no_flag: true }) })
        const { user } = setup(remoteNode({ country_code: "DE" }))
        const identity = await editGroup(user, "Identity")
        await user.click(within(identity).getByRole("button", { name: "Country" }))
        await user.click(await screen.findByRole("option", { name: "No flag" }))
        expect(within(identity).getByRole("button", { name: "Country" })).toHaveTextContent("No flag")
        await user.click(within(identity).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateNode).toHaveBeenCalledWith(7, { country_code: "", no_flag: true }))
    })

    it("shows No flag while reading", () => {
        setup(remoteNode({ country_code: "", no_flag: true }))
        expect(within(group("Identity")).getByText("No flag")).toBeInTheDocument()
    })

    it("moves the address only through a guarded change", async () => {
        vi.mocked(moveNodeAddress).mockResolvedValue({ success: true, data: { moved: true, message: "Moved to 198.51.100.9. Users get the new links when their app refreshes." } })
        const { onRefresh, user } = setup(remoteNode())
        // the address isn't a plain field any more
        const identity = await editGroup(user, "Identity")
        expect(within(identity).queryByLabelText("Address")).not.toBeInTheDocument()
        await user.click(within(identity).getByRole("button", { name: "Cancel" }))
        await user.click(within(group("Identity")).getByRole("button", { name: "Change" }))
        const prompt = await screen.findByRole("dialog")
        expect(prompt).toHaveTextContent("Users' links use this address")
        const input = within(prompt).getByLabelText("New address")
        for (const [v, why] of [["203.0.113.24", "current address"], ["bad host", "IP address or a host name"]]) {
            await user.clear(input)
            await user.type(input, v)
            await user.tab()
            expect(within(prompt).getByText(new RegExp(why))).toBeInTheDocument()
        }
        await user.clear(input)
        await user.type(input, "198.51.100.9")
        await user.click(within(prompt).getByRole("button", { name: "Change address" }))
        await waitFor(() => expect(moveNodeAddress).toHaveBeenCalledWith(7, "198.51.100.9"))
        expect(await screen.findByText(/Moved to 198.51.100.9/, { selector: "p" })).toBeInTheDocument()
        expect(onRefresh).toHaveBeenCalled()
    })

    it("restarts Xray only when the logging that Xray reads changes", async () => {
        const { user } = setup()
        await within(group("Xray logging")).findByText("Warning")
        const g = await editGroup(user, "Xray logging")
        await user.click(within(g).getByRole("switch", { name: "DNS logging" }))
        expect(footerOf(g)).toHaveTextContent("Saving restarts Xray")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateXrayConfig).toHaveBeenCalled())
        const sent = JSON.parse(vi.mocked(updateXrayConfig).mock.calls[0][1])
        expect(sent.log).toMatchObject({ loglevel: "warning", dnsLog: true, access: "/var/log/xray/access.log" })
        expect(toast.success).toHaveBeenCalledWith("Xray logging saved")
    })

    it("shows an actionable Xray retry when loading fails", async () => {
        vi.mocked(getXrayConfig).mockResolvedValue({ success: false, error: "Agent timed out" })
        const { user } = setup()
        expect(await within(group("Xray logging")).findByText("Agent timed out")).toBeInTheDocument()
        expect(within(group("Xray logging")).getByRole("button", { name: "Edit Xray logging" })).toBeDisabled()
        vi.mocked(getXrayConfig).mockResolvedValue({ success: true, data: '{"log":{"loglevel":"warning"}}' })
        await user.click(within(group("Xray logging")).getByRole("button", { name: "Retry Xray settings" }))
        await waitFor(() => expect(within(group("Xray logging")).getByRole("button", { name: "Edit Xray logging" })).toBeEnabled())
    })

    it("greys out what the node must answer while it is offline", async () => {
        setup(remoteNode({ is_online: false }))
        expect(within(group("SSH")).getByText(/Can't read SSH while the node is offline/)).toBeInTheDocument()
        expect(within(group("Xray logging")).getByText(/Can't read Xray's settings/)).toBeInTheDocument()
        expect(within(group("Agent")).getByRole("button", { name: "Edit Agent" })).toBeDisabled()
        expect(within(group("Agent")).getByRole("button", { name: "Switch to reverse" })).toBeDisabled()
        // what the panel keeps itself still edits
        expect(within(group("Identity")).getByRole("button", { name: "Edit Identity" })).toBeEnabled()
    })
})
