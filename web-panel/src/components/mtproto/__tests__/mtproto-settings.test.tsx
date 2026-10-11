import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MtprotoNodeSettings } from "../mtproto-settings"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { updateNode } from "@/lib/api/nodes"
import {
    changeMtprotoAddress,
    changeMtprotoLinkPort,
    getMtprotoNode,
    removeMtprotoNode,
    rotateMtprotoKeys,
    streamMtprotoJob,
    updateMtprotoSettings,
    type MtprotoJob,
    type MtprotoJobCallbacks,
    type MtprotoNode,
} from "@/lib/api/mtproto"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import { toast } from "@/lib/toast"
import type { Node } from "@/lib/types"

vi.mock("@/lib/api/nodes", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/api/nodes")>()),
    updateNode: vi.fn(),
}))
vi.mock("@/lib/api/mtproto", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/api/mtproto")>()),
    getMtprotoNode: vi.fn(),
    updateMtprotoSettings: vi.fn(),
    updateMtprotoNode: vi.fn(),
    rotateMtprotoKeys: vi.fn(),
    changeMtprotoLinkPort: vi.fn(),
    changeMtprotoAddress: vi.fn(),
    repairMtprotoNode: vi.fn(),
    removeMtprotoNode: vi.fn(),
    restartMtprotoTelemt: vi.fn(),
    streamMtprotoJob: vi.fn(),
}))
vi.mock("@/lib/api/maintenance", () => ({ setNodeMaintenance: vi.fn(async () => ({ success: true })) }))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

const liteNode = (over: Partial<Node> = {}) => ({ id: 4, name: "VPS 1", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true, is_active: true, ...over }) as Node

function fixture(over: Partial<MtprotoNode> = {}): MtprotoNode {
    return {
        node_id: 4, agent_id: "16073014-22ac-4403-9862-ff988408d32b", hub_port: 8443, capture_port: 9443, ssh_port: 22, ssh_user: "root",
        host_key: "SHA256:e1cZ2/GTl1INNvNU4y", disguise: "www.example.com", max_streams: 2500, arch: "amd64",
        agent_version: "0.1.0", telemt_version: "3.4.25", running: true, healthy: true,
        link: { up: 2, conns: [], streams: 12, relayed: 0, dial_failures: 0, violations: 0, bytes_to_dc: 0, bytes_from_dc: 0 },
        latest: { agent: "0.2.0", telemt: "3.4.25" },
        ...over,
    }
}

let mtproto: MtprotoNode

function setup(node = liteNode(), extra: { onNavigateTab?: (t: string) => void } = {}) {
    const onSaved = vi.fn()
    const onDirtyChange = vi.fn()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    const view = render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/nodes/4"]}><ConfirmDialogProvider>
        <Routes>
            <Route path="/nodes/4" element={<MtprotoNodeSettings node={node} onSaved={onSaved} onDirtyChange={onDirtyChange} onNavigateTab={extra.onNavigateTab} />} />
            <Route path="/nodes" element={<p>the node list</p>} />
        </Routes>
    </ConfirmDialogProvider></MemoryRouter></QueryClientProvider>)
    return { onSaved, onDirtyChange, view, user: userEvent.setup() }
}

const group = (name: string) => screen.getByRole("region", { name })
const editGroup = async (user: ReturnType<typeof userEvent.setup>, name: string) => {
    await user.click(await screen.findByRole("button", { name: `Edit ${name}` }))
    return group(name)
}
const footerOf = (g: HTMLElement) => within(g).getByRole("region", { name: "Unsaved changes" })

beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    mtproto = fixture()
    vi.mocked(getMtprotoNode).mockImplementation(async () => ({ success: true, data: mtproto }))
    vi.mocked(updateNode).mockImplementation(async (id, data) => ({ success: true, data: { ...liteNode(), ...data, id } as Node }))
    vi.mocked(updateMtprotoSettings).mockImplementation(async (_id, body) => {
        mtproto = { ...mtproto, ...body }
        return { success: true, data: mtproto }
    })
})
afterEach(() => vi.unstubAllGlobals())

describe("MtprotoNodeSettings", () => {
    it("reads as one page of groups, with the sections listed beside it", async () => {
        setup()
        const list = screen.getByRole("navigation", { name: "On this page" })
        for (const s of ["General", "Connection", "Capacity", "Danger zone"]) expect(within(list).getByRole("button", { name: s })).toBeInTheDocument()
        expect(within(list).queryByRole("button", { name: "Software" })).not.toBeInTheDocument()
        expect(within(group("Identity")).getByText("VPS 1")).toBeInTheDocument()
        expect(within(group("Identity")).getByText("203.0.113.9")).toBeInTheDocument()
        expect(within(group("Identity")).getByText("#4")).toBeInTheDocument()
        expect(await within(group("Link")).findByText("8443")).toBeInTheDocument()
        expect(within(group("Link")).getByText("Up")).toBeInTheDocument()
        expect(within(group("Stream limit")).getByText("2,500")).toBeInTheDocument()
        // read first: nothing is an input until Edit
        expect(screen.queryByRole("textbox")).not.toBeInTheDocument()
    })

    it("renames the node from its group and toasts it", async () => {
        const { onSaved, onDirtyChange, user } = setup()
        const identity = await editGroup(user, "Identity")
        const input = within(identity).getByLabelText("Name")
        await user.clear(input)
        await user.type(input, "  VPS Tehran ")
        expect(onDirtyChange).toHaveBeenLastCalledWith(true)
        expect(footerOf(identity)).toHaveTextContent("1 unsaved change")
        await user.click(within(identity).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(toast.success).toHaveBeenCalledWith("MTProto node renamed"))
        expect(updateNode).toHaveBeenCalledWith(4, { name: "VPS Tehran" })
        expect(updateMtprotoSettings).not.toHaveBeenCalled()
        expect(onSaved).toHaveBeenCalled()
        await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
        // back to reading, showing the saved name until the page reloads the node
        expect(within(group("Identity")).getByText("VPS Tehran")).toBeInTheDocument()
        expect(within(group("Identity")).queryByRole("textbox")).not.toBeInTheDocument()
    })

    it("won't save an empty name", async () => {
        const { user } = setup()
        const identity = await editGroup(user, "Identity")
        await user.clear(within(identity).getByLabelText("Name"))
        await user.click(within(identity).getByRole("button", { name: "Save" }))
        expect(await within(identity).findByText("Enter a name.")).toBeInTheDocument()
        expect(footerOf(identity)).toHaveTextContent("Fix the highlighted fields to save.")
        expect(updateNode).not.toHaveBeenCalled()
    })

    it("asks before taking the node out of service", async () => {
        const { onSaved, user } = setup()
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "In service" }))
        expect(footerOf(g)).toHaveTextContent("Saving cuts this node's users off")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        const dialog = await screen.findByRole("dialog")
        expect(dialog).toHaveTextContent("Take VPS 1 out of service?")
        await user.click(within(dialog).getByRole("button", { name: "Take out of service" }))
        await waitFor(() => expect(toast.success).toHaveBeenCalledWith("MTProto node taken out of service"))
        expect(updateNode).toHaveBeenCalledWith(4, { is_active: false })
        expect(onSaved).toHaveBeenCalled()
    })

    it("keeps the node in service when that is cancelled", async () => {
        const { user } = setup()
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "In service" }))
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel" }))
        expect(updateNode).not.toHaveBeenCalled()
        expect(footerOf(group("Availability"))).toHaveTextContent("1 unsaved change")
    })

    it("brings a node back without asking", async () => {
        const { user } = setup(liteNode({ is_active: false, is_online: false }))
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "In service" }))
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(toast.success).toHaveBeenCalledWith("MTProto node is back in service"))
        expect(updateNode).toHaveBeenCalledWith(4, { is_active: true })
    })

    it("turns the maintenance notice on with its message", async () => {
        const { user } = setup()
        expect(within(group("Availability")).queryByText("Notice")).not.toBeInTheDocument()
        const g = await editGroup(user, "Availability")
        await user.click(within(g).getByRole("switch", { name: "Maintenance notice" }))
        await user.type(within(g).getByLabelText("Notice"), "Back soon")
        expect(footerOf(g)).toHaveTextContent("2 unsaved changes")
        expect(footerOf(g)).toHaveTextContent("Users see the notice right away")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(setNodeMaintenance).toHaveBeenCalledWith(4, { enabled: true, message: "Back soon" }))
        expect(toast.success).toHaveBeenCalledWith("Maintenance notice is on")
        expect(updateNode).not.toHaveBeenCalled()
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
        expect(await within(group("Availability")).findByText("Back soon")).toBeInTheDocument()
    })

    it("sends the SSH login in one PATCH from Repair access", async () => {
        const { user } = setup()
        expect(await within(group("Repair access")).findByText("SHA256:e1cZ2/GTl1INNvNU4y")).toBeInTheDocument()
        expect(within(group("Repair access")).getByText("Checked before every repair.")).toBeInTheDocument()
        const g = await editGroup(user, "Repair access")
        // the login sits under Advanced, which opens for the edit
        const port = within(g).getByLabelText("SSH port")
        await user.clear(port)
        await user.type(port, "2222")
        const login = within(g).getByLabelText("SSH user")
        await user.clear(login)
        await user.type(login, "admin")
        expect(footerOf(g)).toHaveTextContent("2 unsaved changes")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateMtprotoSettings).toHaveBeenCalledWith(4, { ssh_port: 2222, ssh_user: "admin" }))
        expect(updateMtprotoSettings).toHaveBeenCalledTimes(1)
        expect(updateNode).not.toHaveBeenCalled()
        await waitFor(() => expect(within(group("Repair access")).queryByRole("region", { name: "Unsaved changes" })).not.toBeInTheDocument())
    })

    it("shows a bad value instead of saving it", async () => {
        const { user } = setup()
        const g = await editGroup(user, "Repair access")
        const port = within(g).getByLabelText("SSH port")
        await user.clear(port)
        await user.type(port, "70000")
        const login = within(g).getByLabelText("SSH user")
        await user.clear(login)
        await user.type(login, "9bad user")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        expect(await within(g).findByText("Pick a port from 1 to 65535.")).toBeInTheDocument()
        expect(within(g).getByText("Up to 32 letters, digits, _ . or -, starting with a letter or _.")).toBeInTheDocument()
        expect(updateMtprotoSettings).not.toHaveBeenCalled()
    })

    it("sets the stream limit within the tested range", async () => {
        const { user } = setup()
        expect(await within(group("Stream limit")).findByText("Tested up to 5,000.")).toBeInTheDocument()
        const g = await editGroup(user, "Stream limit")
        const limit = within(g).getByLabelText("Max streams")
        expect(within(g).getByRole("slider", { name: "Stream limit" })).toHaveAttribute("aria-valuenow", "2500")
        await user.clear(limit)
        await user.type(limit, "50")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        expect(await within(g).findByText("Pick a limit from 100 to 5,000.")).toBeInTheDocument()
        expect(updateMtprotoSettings).not.toHaveBeenCalled()

        await user.clear(limit)
        await user.type(limit, "3000")
        expect(within(g).getByRole("slider", { name: "Stream limit" })).toHaveAttribute("aria-valuenow", "3000")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(updateMtprotoSettings).toHaveBeenCalledWith(4, { max_streams: 3000 }))
    })

    it("keeps a refused change, with the reason in its group", async () => {
        vi.mocked(updateMtprotoSettings).mockResolvedValue({ success: false, error: "ssh_user is not a valid login" })
        const { onSaved, user } = setup()
        const g = await editGroup(user, "Repair access")
        const login = within(g).getByLabelText("SSH user")
        await user.clear(login)
        await user.type(login, "deploy")
        await user.click(within(g).getByRole("button", { name: "Save" }))
        await waitFor(() => expect(footerOf(g)).toHaveTextContent("ssh_user is not a valid login"))
        expect(footerOf(g)).toHaveTextContent("Your edits are kept.")
        expect(onSaved).not.toHaveBeenCalled()
        expect(within(g).getByLabelText("SSH user")).toHaveValue("deploy")
    })

    it("cancel puts a group back as it was", async () => {
        const { onDirtyChange, user } = setup()
        const g = await editGroup(user, "Identity")
        await user.type(within(g).getByLabelText("Name"), " draft")
        await user.click(within(g).getByRole("button", { name: "Cancel" }))
        expect(within(group("Identity")).getByText("VPS 1")).toBeInTheDocument()
        expect(onDirtyChange).toHaveBeenLastCalledWith(false)
    })

    it("moves the link port through a job, refusing ports it can tell are wrong", async () => {
        vi.mocked(changeMtprotoLinkPort).mockResolvedValue({ success: true, data: { job_id: "j9" } })
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        const { user } = setup()
        await within(group("Link")).findByText("8443")
        await user.click(within(group("Link")).getByRole("button", { name: "Change" }))
        const dialog = await screen.findByRole("dialog")
        const input = within(dialog).getByLabelText("New port")
        for (const [port, why] of [["9443", "capture port"], ["22", "SSH port"], ["8443", "already uses"], ["70000", "1 to 65535"]]) {
            await user.clear(input)
            await user.type(input, port)
            await user.tab()
            expect(within(dialog).getByText(new RegExp(why))).toBeInTheDocument()
            expect(within(dialog).getByRole("button", { name: "Change port" })).toBeDisabled()
        }
        await user.clear(input)
        await user.type(input, "9000")
        await user.click(within(dialog).getByRole("button", { name: "Change port" }))
        await waitFor(() => expect(changeMtprotoLinkPort).toHaveBeenCalledWith(4, 9000))
        expect(await screen.findByText("Moving the link to port 9000")).toBeInTheDocument()
    })

    it("moves the address through a job that checks the link comes up there", async () => {
        vi.mocked(changeMtprotoAddress).mockResolvedValue({ success: true, data: { job_id: "j7" } })
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        const { user } = setup()
        await user.click(within(group("Identity")).getByRole("button", { name: "Change" }))
        const prompt = await screen.findByRole("dialog")
        expect(prompt).toHaveTextContent("Every user's proxy link contains this address")
        await user.type(within(prompt).getByLabelText("New address"), "vps.example.com")
        await user.click(within(prompt).getByRole("button", { name: "Change address" }))
        await waitFor(() => expect(changeMtprotoAddress).toHaveBeenCalledWith(4, "vps.example.com"))
        expect(await screen.findByText("Moving the link to vps.example.com")).toBeInTheDocument()
    })

    it("rotates the link keys on click", async () => {
        vi.mocked(rotateMtprotoKeys).mockResolvedValue({ success: true, data: { job_id: "j2" } })
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        const { user } = setup()
        await within(group("Link")).findByText("8443")
        await user.click(within(group("Link")).getByRole("button", { name: "Rotate" }))
        expect(await screen.findByText("Rotating the link keys")).toBeInTheDocument()
        await waitFor(() => expect(rotateMtprotoKeys).toHaveBeenCalledTimes(1))
    })

    it("says when the link keys last changed", async () => {
        mtproto = fixture({ installed_at: "2026-03-01T10:00:00Z" })
        const first = setup()
        expect(await within(group("Link")).findByText("From the install, March 1, 2026")).toBeInTheDocument()
        first.view.unmount()
        mtproto = fixture({ keys_rotated_at: "2026-09-20T10:00:00Z", installed_at: "2026-03-01T10:00:00Z" })
        setup()
        expect(await within(group("Link")).findByText("Rotated September 20, 2026")).toBeInTheDocument()
    })

    it("holds what needs the link while it is down", async () => {
        mtproto = fixture({ link: { ...fixture().link!, up: 0 } })
        setup()
        const link = group("Link")
        expect(await within(link).findByText("Down")).toBeInTheDocument()
        for (const name of ["Change", "Rotate"]) expect(within(link).getByRole("button", { name })).toBeDisabled()
        expect(within(link).getByText("Needs the link to the node")).toBeInTheDocument()
        expect(screen.getByText("The link is down, so removing asks for an SSH login.")).toBeInTheDocument()
    })

    it("needs no login to remove over a live link", async () => {
        const { user } = setup()
        await within(group("Link")).findByText("8443")
        await user.click(screen.getByRole("button", { name: "Remove…" }))
        const dialog = await screen.findByRole("dialog")
        expect(dialog).toHaveTextContent("No login is needed")
        expect(within(dialog).getByRole("button", { name: "Remove" })).toBeEnabled()
    })

    it("goes back to the node list once the node is removed", async () => {
        vi.mocked(removeMtprotoNode).mockResolvedValue({ success: true, data: { job_id: "j1" } })
        vi.mocked(streamMtprotoJob).mockImplementation(async (_id, c: MtprotoJobCallbacks) => {
            const j: MtprotoJob = { id: "j1", kind: "uninstall", state: "ok", steps: [], started_at: "" }
            c.onProgress(j)
            c.onDone(j)
        })
        const { user } = setup()
        await within(group("Link")).findByText("8443")
        await user.click(screen.getByRole("button", { name: "Remove…" }))
        await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Remove" }))
        await user.click((await screen.findAllByRole("button", { name: "Done" }))[0])
        expect(await screen.findByText("the node list")).toBeInTheDocument()
    })

    it("points to the Tunnels tab for the proxy's own settings", async () => {
        const onNavigateTab = vi.fn()
        const { user } = setup(liteNode(), { onNavigateTab })
        expect(within(group("Proxy")).getByText("Port, cover site and user limits are set on the Tunnels tab.")).toBeInTheDocument()
        await user.click(within(group("Proxy")).getByRole("button", { name: "Open Tunnels" }))
        expect(onNavigateTab).toHaveBeenCalledWith("tunnels")
    })

    it("says when the node's details can't load", async () => {
        vi.mocked(getMtprotoNode).mockResolvedValue({ success: false, error: "MTProto node not found" })
        setup()
        expect((await screen.findAllByText("MTProto node not found")).length).toBeGreaterThan(0)
        expect(screen.getAllByRole("button", { name: "Retry" }).length).toBeGreaterThan(0)
    })

    it("clears the page's unsaved guard on unmount", async () => {
        const { onDirtyChange, view, user } = setup()
        const g = await editGroup(user, "Identity")
        await user.type(within(g).getByLabelText("Name"), " x")
        expect(onDirtyChange).toHaveBeenLastCalledWith(true)
        view.unmount()
        expect(onDirtyChange).toHaveBeenLastCalledWith(false)
    })

    it("never says relay to the user", async () => {
        setup(liteNode({ name: "Tehran" }))
        await within(group("Link")).findByText("8443")
        expect(document.body.textContent).not.toMatch(/relay/i)
    })
})
