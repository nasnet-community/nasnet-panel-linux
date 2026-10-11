import { StrictMode } from "react"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { MtprotoJobDialog } from "../mtproto-job-dialog"
import { startMtprotoJob } from "../mtproto-labels"
import { streamMtprotoJob, type MtprotoJob, type MtprotoJobCallbacks } from "@/lib/api/mtproto"
import { toast } from "@/lib/toast"

vi.mock("@/lib/api/mtproto", () => ({ streamMtprotoJob: vi.fn() }))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

const job = (state: MtprotoJob["state"], steps: MtprotoJob["steps"], extra: Partial<MtprotoJob> = {}): MtprotoJob => ({
    id: "j1", kind: "install", state, steps, started_at: "2026-10-01T00:00:00Z", ...extra,
})

/** Lets a test drive the stream by hand. */
function controllable() {
    let cbs!: MtprotoJobCallbacks
    let finish!: () => void
    vi.mocked(streamMtprotoJob).mockImplementation(
        (_id, c) => new Promise<void>((resolve) => { cbs = c; finish = resolve }),
    )
    return { cbs: () => cbs, end: () => finish() }
}

beforeEach(() => vi.clearAllMocks())

describe("MtprotoJobDialog", () => {
    it("follows the job it is given and shows each step in words", async () => {
        const c = controllable()
        render(<MtprotoJobDialog open onOpenChange={() => {}} title="Installing" job={Promise.resolve("j1")} />)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        expect(vi.mocked(streamMtprotoJob).mock.calls[0][0]).toBe("j1")

        c.cbs().onProgress(job("running", [
            { name: "check", state: "ok" },
            { name: "upload", state: "running" },
        ]))
        const list = await screen.findByRole("list", { name: "Progress" })
        expect(list).toHaveTextContent("Checking the node")
        expect(list).toHaveTextContent("Uploading")
    })

    it("names a rotation's verify step for what it checks", async () => {
        const c = controllable()
        render(<MtprotoJobDialog open onOpenChange={() => {}} title="Rotating keys" job={Promise.resolve("j1")} />)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        c.cbs().onProgress(job("running", [{ name: "rotate", state: "ok" }, { name: "verify", state: "running" }], { kind: "rotate" }))
        const list = await screen.findByRole("list", { name: "Progress" })
        expect(list).toHaveTextContent("Checking the new keys work")
        expect(list).not.toHaveTextContent("Verifying what will be uploaded")
    })

    it("cannot be closed while the job runs", async () => {
        controllable()
        const onOpenChange = vi.fn()
        render(<MtprotoJobDialog open onOpenChange={onOpenChange} title="Installing" job={Promise.resolve("j1")} />)
        const button = await screen.findByRole("button", { name: "Working…" })
        expect(button).toBeDisabled()
        await userEvent.setup().keyboard("{Escape}")
        expect(onOpenChange).not.toHaveBeenCalled()
    })

    it("reports success: toast, Done button, and the finished callback with the node", async () => {
        const c = controllable()
        const onFinished = vi.fn()
        const onOpenChange = vi.fn()
        render(<MtprotoJobDialog open onOpenChange={onOpenChange} title="Installing" job={Promise.resolve("j1")} onFinished={onFinished} />)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        const done = job("ok", [{ name: "link", state: "ok" }], { node_id: 7 })
        c.cbs().onProgress(done)
        c.cbs().onDone(done)
        c.end()
        const btn = await screen.findByRole("button", { name: "Done" })
        expect(btn).toBeEnabled()
        expect(toast.success).toHaveBeenCalledWith("Installing finished")
        expect(onFinished).toHaveBeenCalledWith(expect.objectContaining({ node_id: 7 }))
        await userEvent.setup().click(btn)
        expect(onOpenChange).toHaveBeenCalledWith(false)
    })

    it("shows why it failed, on the step and overall, and toasts the error", async () => {
        const c = controllable()
        render(<MtprotoJobDialog open onOpenChange={() => {}} title="Installing" job={Promise.resolve("j1")} />)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        const failed = job("failed", [{ name: "check", state: "failed", detail: "port 443 is already in use on the VPS" }], { error: "port 443 is already in use on the VPS" })
        c.cbs().onProgress(failed)
        c.cbs().onDone(failed)
        c.end()
        const alert = await screen.findByRole("alert")
        expect(alert).toHaveTextContent("port 443 is already in use on the VPS")
        expect(toast.error).toHaveBeenCalledWith("port 443 is already in use on the VPS")
        // the footer button and the dialog's own X are both "Close", and both work again
        const closers = screen.getAllByRole("button", { name: "Close" })
        expect(closers.length).toBeGreaterThanOrEqual(1)
        for (const b of closers) expect(b).toBeEnabled()
    })

    it("surfaces a job that could not even be started", async () => {
        render(<MtprotoJobDialog open onOpenChange={() => {}} title="Installing" job={startMtprotoJob(async () => { throw new Error("confirm the host key first") })} />)
        expect(await screen.findByRole("alert")).toHaveTextContent("confirm the host key first")
        expect(streamMtprotoJob).not.toHaveBeenCalled()
        expect(toast.error).toHaveBeenCalledWith("confirm the host key first")
    })

    it("says so when the progress connection drops before the job ends", async () => {
        const c = controllable()
        render(<MtprotoJobDialog open onOpenChange={() => {}} title="Installing" job={Promise.resolve("j1")} />)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        c.end() // the stream closes without a final frame
        expect(await screen.findByRole("alert")).toHaveTextContent(/progress connection ended/i)
    })
})

describe("MtprotoJobDialog in development", () => {
    it("starts nothing itself: a StrictMode remount only follows the same job again", async () => {
        controllable()
        const begin = vi.fn(async () => "j1")
        const job = startMtprotoJob(begin)
        render(<StrictMode><MtprotoJobDialog open onOpenChange={() => {}} title="Rotating" job={job} /></StrictMode>)
        await waitFor(() => expect(streamMtprotoJob).toHaveBeenCalled())
        expect(begin).toHaveBeenCalledTimes(1)
        for (const [id] of vi.mocked(streamMtprotoJob).mock.calls) expect(id).toBe("j1")
    })
})
