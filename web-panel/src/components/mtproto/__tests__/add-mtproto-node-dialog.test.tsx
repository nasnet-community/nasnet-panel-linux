import { StrictMode } from "react"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { AddMtprotoNodeDialog } from "../add-mtproto-node-dialog"
import { installMtprotoNode, probeMtprotoHost, streamMtprotoJob, type MtprotoJobCallbacks } from "@/lib/api/mtproto"
import { ApiError } from "@/lib/api"

vi.mock("@/lib/api/mtproto", () => ({
    probeMtprotoHost: vi.fn(),
    installMtprotoNode: vi.fn(),
    streamMtprotoJob: vi.fn(),
}))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(probeMtprotoHost).mockResolvedValue({ success: true, data: { fingerprint: "SHA256:abcDEF123" } })
    vi.mocked(installMtprotoNode).mockResolvedValue({ success: true, data: { job_id: "j1" } })
})

async function reachDetails(user: ReturnType<typeof userEvent.setup>) {
    await user.type(screen.getByLabelText("Address"), "203.0.113.9")
    await user.click(screen.getByRole("button", { name: "Check node" }))
    await screen.findByLabelText("Cover site")
}

describe("AddMtprotoNodeDialog", () => {
    it("checks the node, then goes straight to signing in", async () => {
        const user = userEvent.setup()
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        expect(screen.getByRole("button", { name: "Check node" })).toBeDisabled()
        await user.type(screen.getByLabelText("Address"), "203.0.113.9")
        await user.click(screen.getByRole("button", { name: "Check node" }))

        expect(await screen.findByLabelText("Cover site")).toBeInTheDocument()
        expect(probeMtprotoHost).toHaveBeenCalledWith({ host: "203.0.113.9", ssh_port: 22, ssh_user: "root" })
        // no fingerprint step, and nothing is installed until asked
        expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
        expect(screen.queryByText(/fingerprint/i)).not.toBeInTheDocument()
        expect(installMtprotoNode).not.toHaveBeenCalled()
    })

    it("goes back to the node kind picker from its first step", async () => {
        const user = userEvent.setup()
        const onBack = vi.fn()
        const { rerender } = render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument()
        rerender(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} onBack={onBack} />)
        await user.click(screen.getByRole("button", { name: "Back" }))
        expect(onBack).toHaveBeenCalledTimes(1)
        expect(probeMtprotoHost).not.toHaveBeenCalled()
    })

    it("reports a node that does not answer, and stays on the first step", async () => {
        vi.mocked(probeMtprotoHost).mockResolvedValue({ success: false, error: "installer: connect: connection refused" })
        const user = userEvent.setup()
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await user.type(screen.getByLabelText("Address"), "203.0.113.9")
        await user.click(screen.getByRole("button", { name: "Check node" }))
        expect(await screen.findByRole("alert")).toHaveTextContent("connection refused")
        expect(screen.queryByLabelText("Cover site")).not.toBeInTheDocument()
    })

    it("needs a name, a real domain and a credential before it can install", async () => {
        const user = userEvent.setup()
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await reachDetails(user)
        const install = screen.getByRole("button", { name: "Install" })
        expect(install).toBeDisabled()
        await user.type(screen.getByLabelText("Cover site"), "not a domain")
        await user.type(screen.getByLabelText("Password"), "pw")
        expect(install).toBeDisabled()
        await user.clear(screen.getByLabelText("Cover site"))
        await user.type(screen.getByLabelText("Cover site"), "www.example.ir")
        expect(install).toBeEnabled()
    })

    it("pins the host key it saw, then hands over the new node", async () => {
        const user = userEvent.setup()
        const onInstalled = vi.fn()
        let cbs!: MtprotoJobCallbacks
        vi.mocked(streamMtprotoJob).mockImplementation((_id, c) => new Promise<void>((resolve) => { cbs = c; setTimeout(resolve, 0) }))
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={onInstalled} />)
        await reachDetails(user)
        await user.clear(screen.getByLabelText("Name"))
        await user.type(screen.getByLabelText("Name"), "VPS 1")
        await user.type(screen.getByLabelText("Cover site"), "WWW.Example.IR")
        await user.type(screen.getByLabelText("Password"), "s3cret")
        await user.click(screen.getByRole("button", { name: "Install" }))

        await waitFor(() => expect(installMtprotoNode).toHaveBeenCalled())
        expect(installMtprotoNode).toHaveBeenCalledWith({
            name: "VPS 1", host: "203.0.113.9", ssh_port: 22, ssh_user: "root",
            host_key: "SHA256:abcDEF123", disguise: "www.example.ir", ssh_password: "s3cret",
        })
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        const ok = { id: "j1", kind: "install", state: "ok" as const, steps: [], node_id: 9, started_at: "" }
        cbs.onProgress(ok)
        cbs.onDone(ok)
        await waitFor(() => expect(onInstalled).toHaveBeenCalledWith(9))
    })

    it("sends a private key and its passphrase instead of a password", async () => {
        const user = userEvent.setup()
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await reachDetails(user)
        await user.type(screen.getByLabelText("Cover site"), "www.example.ir")
        await user.click(screen.getByRole("button", { name: "Private key" }))
        await user.type(screen.getByLabelText("Private key"), "-----BEGIN KEY-----")
        await user.type(screen.getByLabelText(/Passphrase/), "pp")
        await user.click(screen.getByRole("button", { name: "Install" }))
        await waitFor(() => expect(installMtprotoNode).toHaveBeenCalled())
        const body = vi.mocked(installMtprotoNode).mock.calls[0][0]
        expect(body.ssh_key).toBe("-----BEGIN KEY-----")
        expect(body.ssh_passphrase).toBe("pp")
        expect(body.ssh_password).toBeUndefined()
    })

    it("passes advanced ports only when they were filled in", async () => {
        const user = userEvent.setup()
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await reachDetails(user)
        await user.type(screen.getByLabelText("Cover site"), "www.example.ir")
        await user.type(screen.getByLabelText("Password"), "pw")
        await user.click(screen.getByRole("button", { name: /Show advanced/ }))
        await user.type(screen.getByLabelText("Link port"), "8444")
        await user.click(screen.getByRole("button", { name: "Install" }))
        await waitFor(() => expect(installMtprotoNode).toHaveBeenCalled())
        const body = vi.mocked(installMtprotoNode).mock.calls[0][0]
        expect(body.hub_port).toBe(8444)
        expect(body.capture_port).toBeUndefined()
        expect(body.max_streams).toBeUndefined()
    })
})

describe("AddMtprotoNodeDialog cover test", () => {
    async function fillAndInstall(user: ReturnType<typeof userEvent.setup>) {
        await reachDetails(user)
        await user.type(screen.getByLabelText("Cover site"), "bad.example.ir")
        await user.type(screen.getByLabelText("Password"), "s3cret")
        await user.click(screen.getByRole("button", { name: "Install" }))
    }

    it("keeps the form, login included, when the cover site fails its test", async () => {
        const cover = { ok: false, tls13: true, x25519: true, h2: false, cert_ok: true, latency_ms: 0, err: "the site does not offer h2" }
        vi.mocked(installMtprotoNode).mockRejectedValueOnce(new ApiError("bad.example.ir does not work as a cover site from this VPS: the site does not offer h2", undefined, 422, { success: false, data: { cover } }))
        const user = userEvent.setup()
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await fillAndInstall(user)

        expect(await screen.findByRole("alert")).toHaveTextContent("bad.example.ir doesn't work as a cover site from this node")
        expect(screen.getByText("HTTP/2")).toBeInTheDocument()
        expect(screen.getByText("the site does not offer h2")).toBeInTheDocument()
        expect(screen.getByLabelText("Password")).toHaveValue("s3cret")
        expect(streamMtprotoJob).not.toHaveBeenCalled()

        // a new site clears it, and the next try goes ahead
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        await user.clear(screen.getByLabelText("Cover site"))
        await user.type(screen.getByLabelText("Cover site"), "www.example.ir")
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
        await user.click(screen.getByRole("button", { name: "Install" }))
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        expect(vi.mocked(installMtprotoNode).mock.calls[1][0]).toMatchObject({ disguise: "www.example.ir", ssh_password: "s3cret" })
    })

    it("shows a failed sign-in on the form", async () => {
        vi.mocked(installMtprotoNode).mockRejectedValueOnce(new ApiError("installer: login: ssh: unable to authenticate", undefined, 502, { success: false }))
        const user = userEvent.setup()
        render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        await fillAndInstall(user)
        expect(await screen.findByRole("alert")).toHaveTextContent("unable to authenticate")
        expect(screen.getByLabelText("Cover site")).toHaveValue("bad.example.ir")
        expect(streamMtprotoJob).not.toHaveBeenCalled()
    })
})

describe("AddMtprotoNodeDialog copy", () => {
    it("talks about the node and its cover site, never a server or FakeTLS", async () => {
        const user = userEvent.setup()
        const { baseElement } = render(<AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} />)
        const seen: string[] = [baseElement.textContent ?? ""]
        await reachDetails(user)
        seen.push(baseElement.textContent ?? "")
        for (const text of seen) {
            expect(text).not.toMatch(/server/i)
            expect(text).not.toMatch(/faketls/i)
        }
    })
})

describe("AddMtprotoNodeDialog in development", () => {
    it("starts one install under StrictMode", async () => {
        const user = userEvent.setup()
        vi.mocked(streamMtprotoJob).mockImplementation(() => new Promise<void>(() => {}))
        render(<StrictMode><AddMtprotoNodeDialog open onOpenChange={() => {}} onInstalled={() => {}} /></StrictMode>)
        await reachDetails(user)
        await user.type(screen.getByLabelText("Cover site"), "www.example.ir")
        await user.type(screen.getByLabelText("Password"), "pw")
        await user.click(screen.getByRole("button", { name: "Install" }))
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        await new Promise((r) => setTimeout(r, 50))
        expect(installMtprotoNode).toHaveBeenCalledTimes(1)
    })
})
